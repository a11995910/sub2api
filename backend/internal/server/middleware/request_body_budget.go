package middleware

import (
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

const (
	bodyMemoryReservationMultiplier       int64 = 2
	jsonBodyMemoryReservationMultiplier   int64 = 7
	stableBodyMemoryReservationMultiplier int64 = 4
	unknownBodyMaxBytes                   int64 = 8 << 20
	unknownBodyPerRequestBudgetDivisor    int64 = 8
	unknownBodyAggregateBudgetDivisor     int64 = 2
	requestBodyAdmissionLeaseKey                = "middleware.request_body_admission_lease"
	requestBodyAdmissionControllerKey           = "middleware.request_body_admission_controller"
)

var errBodyAdmissionUnavailable = errors.New("request body admission unavailable")

// BodyAdmissionRejectReason 表示请求体准入失败的内部原因。
type BodyAdmissionRejectReason string

const (
	BodyAdmissionRejectMemory BodyAdmissionRejectReason = "body_memory_budget"
	BodyAdmissionRejectRead   BodyAdmissionRejectReason = "body_read_slots"
)

type requestBodyAdmissionState uint8

const (
	requestBodyAdmissionPending requestBodyAdmissionState = iota
	requestBodyAdmissionAdmitted
	requestBodyAdmissionFailed
)

// requestBodyAdmissionController 允许合法身份在计费判断前按需触发请求体
// 准入，同时把租约的生命周期统一交给最外层中间件管理。
type requestBodyAdmissionController struct {
	budget *BodyMemoryBudget
	limit  int64
	state  requestBodyAdmissionState
	lease  *RequestBodyAdmissionLease
}

// BodyMemoryBudget 管理进程内请求体内存和上传读取并发。
// 读取槽位在 body 到 EOF、出错或 Close 后立即释放；主内存和 unknown
// 子租约在 body 物化后按实际大小同步收缩，并持有到当前请求链结束。
type BodyMemoryBudget struct {
	memory        *semaphore.Weighted
	unknownMemory *semaphore.Weighted
	reads         *semaphore.Weighted
	// memoryAvailable 使用广播唤醒内存等待者。semaphore.Weighted 的阻塞
	// Acquire 是严格队头语义，大请求排在前面时会阻塞本可使用剩余预算的
	// 小请求；网关更需要保护小请求的首字延迟。
	memoryAvailable *bodyMemoryAvailability
	// unknownMemoryAvailable 对 unknown 子预算采用相同的非 FIFO 唤醒，
	// 避免队头大请求阻塞仍可使用剩余子预算的小请求。
	unknownMemoryAvailable *bodyMemoryAvailability

	capacity        int64
	unknownCapacity int64
	maxReads        int
	wait            time.Duration
}

type bodyMemoryAvailability struct {
	mu      sync.Mutex
	ready   chan struct{}
	waiters atomic.Int64
}

func newBodyMemoryAvailability() *bodyMemoryAvailability {
	return &bodyMemoryAvailability{ready: make(chan struct{})}
}

func (a *bodyMemoryAvailability) subscribe() <-chan struct{} {
	if a == nil {
		return nil
	}
	a.waiters.Add(1)
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	return ready
}

func (a *bodyMemoryAvailability) unsubscribe() {
	if a != nil {
		a.waiters.Add(-1)
	}
}

func (a *bodyMemoryAvailability) notify() {
	if a == nil || a.waiters.Load() <= 0 {
		return
	}
	a.mu.Lock()
	if a.waiters.Load() <= 0 {
		a.mu.Unlock()
		return
	}
	close(a.ready)
	a.ready = make(chan struct{})
	a.mu.Unlock()
}

// NewBodyMemoryBudget 创建进程级请求体准入预算。
// waitSeconds <= 0 表示预算耗尽时立即拒绝；maxReadSlots <= 0 表示不限制读取槽位。
// 变长参数用于兼容旧的两参数构造调用。
func NewBodyMemoryBudget(capacityBytes int64, waitSeconds int, maxReadSlots ...int) *BodyMemoryBudget {
	readSlots := 0
	if len(maxReadSlots) > 0 {
		readSlots = maxReadSlots[0]
	}
	if capacityBytes <= 0 && readSlots <= 0 {
		return nil
	}
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	b := &BodyMemoryBudget{
		capacity: capacityBytes,
		maxReads: readSlots,
		wait:     durationFromSeconds(waitSeconds),
	}
	if capacityBytes > 0 {
		b.memory = semaphore.NewWeighted(capacityBytes)
		b.memoryAvailable = newBodyMemoryAvailability()
		b.unknownCapacity = capacityBytes / unknownBodyAggregateBudgetDivisor
		if b.unknownCapacity <= 0 {
			b.unknownCapacity = 1
		}
		b.unknownMemory = semaphore.NewWeighted(b.unknownCapacity)
		b.unknownMemoryAvailable = newBodyMemoryAvailability()
	}
	if readSlots > 0 {
		b.reads = semaphore.NewWeighted(int64(readSlots))
	}
	return b
}

func durationFromSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	maxSeconds := int64(math.MaxInt64 / int64(time.Second))
	if int64(seconds) > maxSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds) * time.Second
}

// RequestBodyAdmissionLease 是一次请求体准入租约。
type RequestBodyAdmissionLease struct {
	budget                *BodyMemoryBudget
	reservation           int64
	memoryAcquired        bool
	unknownReservation    int64
	unknownMemoryAcquired bool
	jsonBody              bool
	compressedBody        bool
	body                  *admissionReadCloser

	readAcquired  bool
	readOnce      sync.Once
	readDone      atomic.Bool
	stopOnce      sync.Once
	stopCancel    func() bool
	interruptRead func()
	memoryMu      sync.Mutex
	unknownMu     sync.Mutex
	bodyMu        sync.Mutex
	ownershipMu   sync.Mutex
	detached      bool
	released      bool
}

func (l *RequestBodyAdmissionLease) releaseRead() {
	if l == nil {
		return
	}
	if !l.readAcquired {
		l.readDone.Store(true)
		return
	}
	l.readOnce.Do(func() {
		if l.budget != nil && l.budget.reads != nil {
			l.budget.reads.Release(1)
		}
		l.readDone.Store(true)
	})
}

func (l *RequestBodyAdmissionLease) releaseMemory() {
	if l == nil || l.budget == nil || l.budget.memory == nil {
		return
	}
	l.memoryMu.Lock()
	if !l.memoryAcquired || l.reservation <= 0 {
		l.memoryAcquired = false
		l.reservation = 0
		l.memoryMu.Unlock()
		return
	}
	released := l.reservation
	l.reservation = 0
	l.memoryAcquired = false
	l.memoryMu.Unlock()

	l.budget.memory.Release(released)
	l.budget.memoryAvailable.notify()
}

func (l *RequestBodyAdmissionLease) shrinkMemory(target int64) {
	if l == nil || l.budget == nil || l.budget.memory == nil {
		return
	}
	if target < 0 {
		target = 0
	}
	if target > l.budget.capacity {
		target = l.budget.capacity
	}

	l.memoryMu.Lock()
	if !l.memoryAcquired || target >= l.reservation {
		l.memoryMu.Unlock()
		return
	}
	released := l.reservation - target
	l.reservation = target
	l.memoryMu.Unlock()

	l.budget.memory.Release(released)
	l.budget.memoryAvailable.notify()
}

func (l *RequestBodyAdmissionLease) retainedMemory() int64 {
	if l == nil {
		return 0
	}
	l.memoryMu.Lock()
	defer l.memoryMu.Unlock()
	return l.reservation
}

func (l *RequestBodyAdmissionLease) observeBodyMaterialized(
	stage pkghttputil.RequestBodyMaterializationStage,
	bodyBytes int64,
) {
	if l == nil || bodyBytes < 0 {
		return
	}
	multiplier := bodyMemoryReservationMultiplier
	if stage == pkghttputil.RequestBodyMaterializedStable {
		// 最终入口 body 后续仍可能同时存在会话哈希、渠道映射和上游转发
		// 副本。保留四倍覆盖常见转发链，又避免七倍解析余量占满整个长流。
		multiplier = stableBodyMemoryReservationMultiplier
	} else if stage == pkghttputil.RequestBodyMaterializedDecoded && l.jsonBody {
		multiplier = jsonBodyMemoryReservationMultiplier
	}
	target := saturatingMul(bodyBytes, multiplier)
	l.shrinkMemory(target)
	l.shrinkUnknownMemory(target)
}

func (l *RequestBodyAdmissionLease) finishRead(bodyBytes int64, complete bool) {
	if l == nil {
		return
	}
	l.releaseRead()
	if complete && !l.compressedBody {
		l.observeBodyMaterialized(pkghttputil.RequestBodyMaterializedDecoded, bodyBytes)
	}
}

func (l *RequestBodyAdmissionLease) releaseUnknownMemory() {
	if l == nil || l.budget == nil || l.budget.unknownMemory == nil {
		return
	}
	l.unknownMu.Lock()
	if !l.unknownMemoryAcquired || l.unknownReservation <= 0 {
		l.unknownMemoryAcquired = false
		l.unknownReservation = 0
		l.unknownMu.Unlock()
		return
	}
	released := l.unknownReservation
	l.unknownReservation = 0
	l.unknownMemoryAcquired = false
	l.unknownMu.Unlock()

	l.budget.unknownMemory.Release(released)
	l.budget.unknownMemoryAvailable.notify()
}

func (l *RequestBodyAdmissionLease) shrinkUnknownMemory(target int64) {
	if l == nil || l.budget == nil || l.budget.unknownMemory == nil {
		return
	}
	if target < 0 {
		target = 0
	}
	if target > l.budget.unknownCapacity {
		target = l.budget.unknownCapacity
	}

	l.unknownMu.Lock()
	if !l.unknownMemoryAcquired || target >= l.unknownReservation {
		l.unknownMu.Unlock()
		return
	}
	released := l.unknownReservation - target
	l.unknownReservation = target
	l.unknownMu.Unlock()

	l.budget.unknownMemory.Release(released)
	l.budget.unknownMemoryAvailable.notify()
}

func (l *RequestBodyAdmissionLease) retainedUnknownMemory() int64 {
	if l == nil {
		return 0
	}
	l.unknownMu.Lock()
	defer l.unknownMu.Unlock()
	return l.unknownReservation
}

func (l *RequestBodyAdmissionLease) releaseResources() {
	if l == nil {
		return
	}
	l.bodyMu.Lock()
	body := l.body
	stopCancel := l.stopCancel
	interruptRead := l.interruptRead
	l.body = nil
	l.stopCancel = nil
	l.interruptRead = nil
	l.bodyMu.Unlock()
	// 下游可能在尚未消费完请求体时提前返回。主动关闭包装体，确保底层
	// 连接停止继续排空后再释放读取槽位，避免把仍在上传的请求漏计为闲置。
	if body != nil {
		if !l.readDone.Load() && interruptRead != nil {
			interruptRead()
		}
		_ = body.Close()
	}
	if stopCancel != nil {
		l.stopOnce.Do(func() { _ = stopCancel() })
	}
	l.releaseRead()
	l.releaseMemory()
	l.releaseUnknownMemory()
}

// releaseConsumedBodyReference 在请求体已经读完（或读错）后解除入口 body
// 包装器的引用。异步 handoff 只需要继续持有收缩后的内存租约；若继续保留
// MaxBytesReader，它会反向持有已结束请求的 ResponseWriter 和连接对象。
// 未完成读取时不做处理，仍由最终 Release 关闭底层 body。
func (l *RequestBodyAdmissionLease) releaseConsumedBodyReference() {
	if l == nil || !l.readDone.Load() {
		return
	}
	l.bodyMu.Lock()
	stopCancel := l.stopCancel
	l.body = nil
	l.stopCancel = nil
	l.interruptRead = nil
	l.bodyMu.Unlock()
	if stopCancel != nil {
		l.stopOnce.Do(func() { _ = stopCancel() })
	}
	// body 已经到 EOF/错误，底层请求不会再被读取；不重复调用 Close，
	// 让 net/http 在 ServeHTTP 结束时执行标准的 request-body Close。
}

// Release 释放租约持有的全部资源，可安全重复调用。
func (l *RequestBodyAdmissionLease) Release() {
	if l == nil {
		return
	}
	l.ownershipMu.Lock()
	if l.released {
		l.ownershipMu.Unlock()
		return
	}
	l.released = true
	l.ownershipMu.Unlock()
	l.releaseResources()
}

// releaseIfAttached 由请求中间件在请求链结束时调用。若租约已经转交给
// 后台任务，则由后台任务负责释放，避免异步提交在 handler 返回时提前归还预算。
func (l *RequestBodyAdmissionLease) releaseIfAttached() {
	if l == nil {
		return
	}
	l.ownershipMu.Lock()
	if l.released || l.detached {
		l.ownershipMu.Unlock()
		return
	}
	l.released = true
	l.ownershipMu.Unlock()
	l.releaseResources()
}

func (l *RequestBodyAdmissionLease) detach() bool {
	if l == nil {
		return false
	}
	l.ownershipMu.Lock()
	defer l.ownershipMu.Unlock()
	if l.released || l.detached {
		return false
	}
	l.detached = true
	return true
}

// TakeRequestBodyAdmissionLease 将当前请求的请求体租约转交给后台任务。
// 调用方取得租约后必须在后台任务结束时调用 Release；没有租约时返回 nil。
func TakeRequestBodyAdmissionLease(c *gin.Context) *RequestBodyAdmissionLease {
	if c == nil {
		return nil
	}
	value, exists := c.Get(requestBodyAdmissionLeaseKey)
	if !exists {
		return nil
	}
	lease, ok := value.(*RequestBodyAdmissionLease)
	if !ok || lease == nil || !lease.detach() {
		return nil
	}
	lease.releaseConsumedBodyReference()
	// 防止同一请求链重复转交同一个租约。
	c.Set(requestBodyAdmissionLeaseKey, nil)
	return lease
}

// admissionReadCloser 在 body 读完、读错或关闭时释放读取阶段资源。
type admissionReadCloser struct {
	io.ReadCloser
	finish    func(int64, bool)
	readBytes atomic.Int64
	readOnce  sync.Once
	closeOnce sync.Once
	closeErr  error
}

func (r *admissionReadCloser) done(complete bool) {
	if r == nil || r.finish == nil {
		return
	}
	r.readOnce.Do(func() { r.finish(r.readBytes.Load(), complete) })
}

func (r *admissionReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.ReadCloser == nil {
		if r != nil {
			r.done(false)
		}
		return 0, io.ErrClosedPipe
	}
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.readBytes.Add(int64(n))
	}
	if err != nil {
		r.done(errors.Is(err, io.EOF))
	}
	return n, err
}

func (r *admissionReadCloser) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.ReadCloser != nil {
			r.closeErr = r.ReadCloser.Close()
		}
		r.done(false)
	})
	return r.closeErr
}

func requestBodyAdmissionContentType(req *http.Request) string {
	if req == nil {
		return ""
	}
	contentType := req.Header.Get("Content-Type")
	if gatewayRouteUsesLenientJSON(req) {
		// 这些端点无论客户端如何声明 Content-Type，都会进入宽松 JSON
		// 规范化。按路由语义估算，不能通过伪造类型降低准入成本。
		return "application/json"
	}
	return contentType
}

// requestBodyAdmissionLimit 为未知长度请求设置可验证的硬上限。若继续按
// 整条路由上限预留，一个很小的 chunked 请求也会长期占满全局预算；若只
// 预留固定小值又会让大请求绕过内存保护。这里按最坏内存倍率反推上限，
// 确保单个未知长度请求最多占全局预算的八分之一。
func requestBodyAdmissionLimit(budget *BodyMemoryBudget, req *http.Request, routeLimit int64) int64 {
	if routeLimit <= 0 || req == nil || req.ContentLength > 0 || requestBodyIsEmpty(req.Body) {
		return routeLimit
	}
	limit := routeLimit
	if limit > unknownBodyMaxBytes {
		limit = unknownBodyMaxBytes
	}
	if budget == nil || budget.capacity <= 0 {
		return limit
	}

	maxReservation := budget.capacity / unknownBodyPerRequestBudgetDivisor
	if maxReservation <= 0 {
		maxReservation = 1
	}
	multiplier := bodyMemoryReservationMultiplier
	if isJSONContentType(requestBodyAdmissionContentType(req)) {
		multiplier = jsonBodyMemoryReservationMultiplier
	}
	if isRequestBodyCompressed(req.Header.Get("Content-Encoding")) {
		// 压缩输入在解码时会同时保留原文和解压后的解析/改写副本。
		multiplier++
	}
	byBudget := maxReservation / multiplier
	if byBudget <= 0 {
		byBudget = 1
	}
	if byBudget < limit {
		limit = byBudget
	}
	return limit
}

// AcquireRequestBodyAdmission 在下游第一次读取 body 前安装有效硬上限并
// 预留准入资源。调用方负责在请求链结束时调用 lease.Release；声明长度
// 已超限时返回 *http.MaxBytesError。
func AcquireRequestBodyAdmission(c *gin.Context, budget *BodyMemoryBudget, routeLimit int64) (*RequestBodyAdmissionLease, BodyAdmissionRejectReason, error) {
	if c == nil || c.Request == nil || requestBodyIsEmpty(c.Request.Body) {
		return nil, "", nil
	}
	if routeLimit <= 0 {
		return nil, "", nil
	}
	if err := c.Request.Context().Err(); err != nil {
		return nil, "", err
	}

	routeLimit = requestBodyAdmissionLimit(budget, c.Request, routeLimit)
	installRequestBodyLimits(c, routeLimit)
	if routeLimit > 0 && c.Request.ContentLength > routeLimit {
		return nil, "", &http.MaxBytesError{Limit: routeLimit}
	}
	if budget == nil {
		return nil, "", nil
	}
	return acquireRequestBodyAdmissionWithLimit(c, budget, routeLimit)
}

// acquireRequestBodyAdmissionWithLimit 只负责获取租约。调用方必须已经安装
// routeLimit 对应的原始和解压后硬上限，避免预算估算与实际可读字节数脱节。
func acquireRequestBodyAdmissionWithLimit(c *gin.Context, budget *BodyMemoryBudget, routeLimit int64) (*RequestBodyAdmissionLease, BodyAdmissionRejectReason, error) {
	if budget == nil || c == nil || c.Request == nil || requestBodyIsEmpty(c.Request.Body) || routeLimit <= 0 {
		return nil, "", nil
	}
	if err := c.Request.Context().Err(); err != nil {
		return nil, "", err
	}

	unknownLength := c.Request.ContentLength <= 0
	_, cached := pkghttputil.CachedRequestBody(c.Request)
	contentType := requestBodyAdmissionContentType(c.Request)
	contentEncoding := c.GetHeader("Content-Encoding")
	reservation := budget.reservationBytes(c.Request.ContentLength, routeLimit, contentEncoding, contentType)
	if reservation <= 0 && (budget.reads == nil || cached) {
		return nil, "", nil
	}

	lease := &RequestBodyAdmissionLease{
		budget:         budget,
		reservation:    reservation,
		jsonBody:       isJSONContentType(contentType),
		compressedBody: isRequestBodyCompressed(contentEncoding),
	}
	if reservation > budget.capacity && budget.memory != nil {
		return nil, BodyAdmissionRejectMemory, errBodyAdmissionUnavailable
	}
	if unknownLength && reservation > 0 && budget.memory != nil {
		maxUnknownReservation := budget.capacity / unknownBodyPerRequestBudgetDivisor
		if maxUnknownReservation <= 0 || reservation > maxUnknownReservation || reservation > budget.unknownCapacity {
			// 极小预算可能连一个字节的未知长度 JSON 都无法在 1/8 单请求
			// 约束内足额预留。此时拒绝请求，不能通过向上取整或截断 token
			// 破坏单请求与 unknown 聚合预算保证。
			return nil, BodyAdmissionRejectMemory, errBodyAdmissionUnavailable
		}
	}
	waitCtx := c.Request.Context()
	var cancel context.CancelFunc
	if budget.wait > 0 {
		waitCtx, cancel = context.WithTimeout(waitCtx, budget.wait)
		defer cancel()
	}

	if unknownLength && reservation > 0 && budget.unknownMemory != nil {
		unknownReservation := reservation
		if err := acquireUnknownBodyMemory(waitCtx, budget, unknownReservation, budget.wait <= 0); err != nil {
			if c.Request.Context().Err() != nil {
				return nil, "", c.Request.Context().Err()
			}
			return nil, BodyAdmissionRejectMemory, errBodyAdmissionUnavailable
		}
		lease.unknownMu.Lock()
		lease.unknownReservation = unknownReservation
		lease.unknownMemoryAcquired = true
		lease.unknownMu.Unlock()
	}
	if reservation > 0 && budget.memory != nil {
		if err := acquireBodyMemory(waitCtx, budget, reservation, budget.wait <= 0); err != nil {
			lease.releaseUnknownMemory()
			if c.Request.Context().Err() != nil {
				return nil, "", c.Request.Context().Err()
			}
			return nil, BodyAdmissionRejectMemory, errBodyAdmissionUnavailable
		}
		lease.memoryMu.Lock()
		lease.memoryAcquired = true
		lease.memoryMu.Unlock()
	}
	if budget.reads != nil && !cached {
		if err := acquireSemaphore(waitCtx, budget.reads, 1, budget.wait <= 0); err != nil {
			// 读取槽位等待失败时归还已经预留的内存，避免部分准入泄漏。
			lease.releaseMemory()
			lease.releaseUnknownMemory()
			if c.Request.Context().Err() != nil {
				return nil, "", c.Request.Context().Err()
			}
			return nil, BodyAdmissionRejectRead, errBodyAdmissionUnavailable
		}
		lease.readAcquired = true
	}

	updatedRequest := pkghttputil.WithRequestBodyMaterializationObserver(c.Request, lease.observeBodyMaterialized)
	if updatedRequest != c.Request {
		*c.Request = *updatedRequest
	}
	if cached {
		if body, ok := pkghttputil.CachedRequestBody(c.Request); ok {
			lease.observeBodyMaterialized(pkghttputil.RequestBodyMaterializedDecoded, int64(len(body)))
		}
		return lease, "", nil
	}
	underlyingBody := c.Request.Body
	admissionBody := &admissionReadCloser{
		ReadCloser: underlyingBody,
		finish:     lease.finishRead,
	}
	lease.body = admissionBody
	lease.interruptRead = requestBodyReadInterrupter(c)
	c.Request.Body = admissionBody
	// 请求取消时只关闭准入包装并释放读取槽；内存租约仍由请求链结束时
	// Release，避免 handler 仍持有已读 body 时预算提前归还。
	interruptRead := lease.interruptRead
	lease.stopCancel = context.AfterFunc(c.Request.Context(), func() {
		if interruptRead != nil {
			interruptRead()
		}
		_ = admissionBody.Close()
	})
	return lease, "", nil
}

func (d *requestBodyAdmissionController) ensure(c *gin.Context) bool {
	if d == nil || c == nil {
		return false
	}
	if d.state == requestBodyAdmissionAdmitted {
		return !c.IsAborted()
	}
	if d.state == requestBodyAdmissionFailed {
		return false
	}
	if c.Request == nil {
		d.state = requestBodyAdmissionFailed
		c.Abort()
		return false
	}
	if c.Request.Context().Err() != nil {
		d.state = requestBodyAdmissionFailed
		closeRequestBody(c)
		c.Abort()
		return false
	}

	d.limit = requestBodyAdmissionLimit(d.budget, c.Request, d.limit)
	installRequestBodyLimits(c, d.limit)
	if d.limit > 0 && c.Request.ContentLength > d.limit {
		d.state = requestBodyAdmissionFailed
		AbortBodyTooLarge(c, d.limit)
		return false
	}

	lease, reason, err := acquireRequestBodyAdmissionWithLimit(c, d.budget, d.limit)
	if err != nil {
		d.state = requestBodyAdmissionFailed
		if c.Request.Context().Err() != nil {
			closeRequestBody(c)
			c.Abort()
			return false
		}
		AbortBodyAdmission(c, reason, d.budget)
		return false
	}
	if lease != nil {
		d.lease = lease
		c.Set(requestBodyAdmissionLeaseKey, lease)
	}
	d.state = requestBodyAdmissionAdmitted
	enableRequestFullDuplex(c)
	return true
}

func (d *requestBodyAdmissionController) releaseIfAttached() {
	if d == nil || d.lease == nil {
		return
	}
	d.lease.releaseIfAttached()
}

func requestBodyController(c *gin.Context) (*requestBodyAdmissionController, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(requestBodyAdmissionControllerKey)
	if !exists {
		return nil, false
	}
	controller, ok := value.(*requestBodyAdmissionController)
	return controller, ok && controller != nil
}

// readAndCacheDeferredRequestBody 在鉴权完成基础身份检查后按需触发准入，
// 并缓存完整的解压后原始请求体，供计费判断和下游 handler 重复读取。
func readAndCacheDeferredRequestBody(c *gin.Context) ([]byte, bool) {
	if c == nil || c.Request == nil {
		return nil, false
	}
	controller, ok := requestBodyController(c)
	if !ok || !controller.ensure(c) {
		return nil, false
	}
	if body, cached := pkghttputil.CachedRequestBody(c.Request); cached {
		return body, true
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		controller.state = requestBodyAdmissionFailed
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			AbortBodyTooLarge(c, maxErr.Limit)
		} else if c.Request.Context().Err() != nil {
			closeRequestBody(c)
			c.Abort()
		} else {
			abortBodyReadFailure(c)
		}
		return nil, false
	}
	c.Request = pkghttputil.WithCachedRequestBody(c.Request, body)
	return body, true
}

func acquireBodyMemory(ctx context.Context, budget *BodyMemoryBudget, n int64, immediate bool) error {
	if budget == nil || budget.memory == nil || n <= 0 {
		return nil
	}
	return acquireMemoryWithoutHeadOfLineBlocking(ctx, budget.memory, budget.memoryAvailable, n, immediate)
}

func acquireUnknownBodyMemory(ctx context.Context, budget *BodyMemoryBudget, n int64, immediate bool) error {
	if budget == nil || budget.unknownMemory == nil || n <= 0 {
		return nil
	}
	return acquireMemoryWithoutHeadOfLineBlocking(ctx, budget.unknownMemory, budget.unknownMemoryAvailable, n, immediate)
}

func acquireMemoryWithoutHeadOfLineBlocking(
	ctx context.Context,
	sem *semaphore.Weighted,
	available *bodyMemoryAvailability,
	n int64,
	immediate bool,
) error {
	if sem == nil || n <= 0 {
		return nil
	}
	acquired, err := tryAcquireMemory(ctx, sem, available, n)
	if err != nil {
		return err
	}
	if acquired {
		return nil
	}
	if immediate {
		return errBodyAdmissionUnavailable
	}
	if available == nil {
		return sem.Acquire(ctx, n)
	}

	for {
		// 先订阅再二次尝试，避免资源恰好在首次 TryAcquire 与订阅之间
		// 释放而丢失通知。释放时广播给所有等待者，能满足的小请求先行；
		// 无法满足的大请求继续等到下一次释放或自身超时。
		ready := available.subscribe()
		acquired, err := tryAcquireMemory(ctx, sem, available, n)
		if err != nil {
			available.unsubscribe()
			return err
		}
		if acquired {
			available.unsubscribe()
			return nil
		}
		select {
		case <-ctx.Done():
			available.unsubscribe()
			return ctx.Err()
		case <-ready:
			available.unsubscribe()
		}
	}
}

func tryAcquireMemory(
	ctx context.Context,
	sem *semaphore.Weighted,
	available *bodyMemoryAvailability,
	n int64,
) (bool, error) {
	if !sem.TryAcquire(n) {
		return false, nil
	}
	select {
	case <-ctx.Done():
		// 资源释放与请求取消可能同时唤醒等待者。若 token 已经抢到但取消
		// 也已发生，立即原样归还并唤醒其他请求，避免断开的客户端短暂
		// 挤占活跃请求预算。
		sem.Release(n)
		available.notify()
		return false, ctx.Err()
	default:
		return true, nil
	}
}

func acquireSemaphore(ctx context.Context, sem *semaphore.Weighted, n int64, immediate bool) error {
	if sem == nil || n <= 0 {
		return nil
	}
	if immediate {
		acquired, err := tryAcquireMemory(ctx, sem, nil, n)
		if err != nil {
			return err
		}
		if acquired {
			return nil
		}
		return errBodyAdmissionUnavailable
	}
	return sem.Acquire(ctx, n)
}

// RequestBodyAdmission 是标准 Gin 中间件版本，适用于鉴权和分组校验之后。
func RequestBodyAdmission(budget *BodyMemoryBudget, routeLimit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil {
			return
		}
		if c.Request == nil {
			c.Abort()
			return
		}
		if c.Request.Context().Err() != nil {
			closeRequestBody(c)
			c.Abort()
			return
		}
		effectiveLimit := requestBodyAdmissionLimit(budget, c.Request, routeLimit)
		installRequestBodyLimits(c, effectiveLimit)
		if effectiveLimit > 0 && c.Request.ContentLength > effectiveLimit {
			AbortBodyTooLarge(c, effectiveLimit)
			return
		}
		lease, reason, err := acquireRequestBodyAdmissionWithLimit(c, budget, effectiveLimit)
		if err != nil {
			if c.Request.Context().Err() != nil {
				closeRequestBody(c)
				c.Abort()
				return
			}
			AbortBodyAdmission(c, reason, budget)
			return
		}
		if lease != nil {
			c.Set(requestBodyAdmissionLeaseKey, lease)
			defer lease.releaseIfAttached()
		}
		enableRequestFullDuplex(c)
		c.Next()
	}
}

// AbortBodyAdmission 返回协议对应的 429 响应，并记录入口拒绝指标。
func AbortBodyAdmission(c *gin.Context, reason BodyAdmissionRejectReason, budget *BodyMemoryBudget) {
	closeRequestBody(c)
	markBodyAdmissionRejected(c, reason)
	writeBodyAdmissionError(c, budgetWaitSeconds(budget))
}

// GatewayBodyRouteLimit 按网关路径选择实际请求体上限，覆盖嵌套的 textBodyLimit。
func GatewayBodyRouteLimit(req *http.Request, defaultLimit, textLimit int64) int64 {
	if req == nil || req.URL == nil {
		return defaultLimit
	}
	path := strings.TrimRight(req.URL.Path, "/")
	if textLimit > 0 {
		switch path {
		case "/v1/alpha/search", "/alpha/search", "/backend-api/codex/alpha/search",
			"/v1/embeddings", "/embeddings":
			return textLimit
		}
	}
	return defaultLimit
}

// RequestBodyAdmissionForGateway 为所有网关入口选择正确的路由上限。
func RequestBodyAdmissionForGateway(budget *BodyMemoryBudget, defaultLimit, textLimit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil {
			return
		}
		if controller, ok := requestBodyController(c); ok {
			if !controller.ensure(c) {
				return
			}
			c.Next()
			return
		}
		if c.Request == nil {
			c.Abort()
			return
		}
		if c.Request.Context().Err() != nil {
			closeRequestBody(c)
			c.Abort()
			return
		}
		limit := GatewayBodyRouteLimit(c.Request, defaultLimit, textLimit)
		if !gatewayRequestMayHaveBody(c.Request) {
			c.Next()
			return
		}
		limit = requestBodyAdmissionLimit(budget, c.Request, limit)
		installRequestBodyLimits(c, limit)
		if limit > 0 && c.Request.ContentLength > limit {
			AbortBodyTooLarge(c, limit)
			return
		}
		lease, reason, err := acquireRequestBodyAdmissionWithLimit(c, budget, limit)
		if err != nil {
			if c.Request.Context().Err() != nil {
				closeRequestBody(c)
				c.Abort()
				return
			}
			AbortBodyAdmission(c, reason, budget)
			return
		}
		if lease != nil {
			c.Set(requestBodyAdmissionLeaseKey, lease)
			defer lease.releaseIfAttached()
		}
		enableRequestFullDuplex(c)
		c.Next()
	}
}

// DeferredRequestBodyAdmissionForGateway 在鉴权前安装轻量 controller，但不
// 读取请求体。API Key 鉴权可在基础身份检查后按需触发它，后置的标准准入
// 中间件也会复用同一个 controller；租约只由这一层负责最终释放。
func DeferredRequestBodyAdmissionForGateway(budget *BodyMemoryBudget, defaultLimit, textLimit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || !gatewayRequestMayHaveBody(c.Request) {
			if c != nil {
				c.Next()
			}
			return
		}
		if _, exists := requestBodyController(c); exists {
			c.Next()
			return
		}
		controller := &requestBodyAdmissionController{
			budget: budget,
			limit:  GatewayBodyRouteLimit(c.Request, defaultLimit, textLimit),
			state:  requestBodyAdmissionPending,
		}
		c.Set(requestBodyAdmissionControllerKey, controller)
		defer func() {
			controller.releaseIfAttached()
			if controller.state == requestBodyAdmissionPending && c.IsAborted() {
				closeRequestBody(c)
			}
		}()
		c.Next()
	}
}

func gatewayRequestMayHaveBody(req *http.Request) bool {
	if req == nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodConnect:
		return false
	default:
		return true
	}
}

func gatewayRouteUsesLenientJSON(req *http.Request) bool {
	if req == nil || req.URL == nil || !gatewayRequestMayHaveBody(req) {
		return false
	}
	path := strings.ToLower(strings.TrimRight(req.URL.Path, "/"))
	return path == "/v1/images/generations" || path == "/images/generations" ||
		strings.Contains(path, "/messages") ||
		strings.Contains(path, "/responses") ||
		strings.Contains(path, "/chat/completions")
}

func requestBodyIsEmpty(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	value := reflect.ValueOf(body)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return true
		}
	}
	// 不能直接比较两个 interface：自定义的不可比较 ReadCloser 值（例如
	// 含 slice 字段的值类型）会在接口比较时触发 panic。http.NoBody 的
	// 动态类型是包内唯一的 noBody 类型，比较反射类型即可安全识别。
	return reflect.TypeOf(body) == reflect.TypeOf(http.NoBody)
}

func installRequestBodyLimits(c *gin.Context, limit int64) {
	if c == nil || c.Request == nil || limit <= 0 {
		return
	}
	updated := pkghttputil.WithDecompressedBodyLimit(c.Request, limit)
	if updated != c.Request {
		// 保持 *http.Request 指针身份不变，避免同一请求链中已持有该指针的
		// 调用方继续操作旧 body，并在缓存替换或取消时重复关闭底层 reader。
		*c.Request = *updated
	}
	enforceRequestBodyLimit(c, limit)
}

// enforceRequestBodyLimit 在下游中间件读取 body 前安装路由级上限。
// 组级上限会更早安装，因此这里用于覆盖声明了更小文本上限的路由。
func enforceRequestBodyLimit(c *gin.Context, limit int64) {
	if c == nil || c.Request == nil || limit <= 0 || requestBodyIsEmpty(c.Request.Body) || c.Writer == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
}

func closeRequestBody(c *gin.Context) {
	if c == nil || c.Request == nil || requestBodyIsEmpty(c.Request.Body) {
		return
	}
	if interrupt := requestBodyReadInterrupter(c); interrupt != nil {
		interrupt()
	}
	_ = c.Request.Body.Close()
}

func requestBodyReadInterrupter(c *gin.Context) func() {
	if c == nil || c.Writer == nil {
		return nil
	}
	controller := http.NewResponseController(c.Writer)
	return func() {
		// Go 的服务端 Request.Body.Close 会尝试排空一小段未读 body 以复用
		// HTTP/1 连接。把读取截止时间推进到当前时刻，使慢上传在拒绝或
		// handler 提前返回时立即中断；写方向不受影响。
		_ = controller.SetReadDeadline(time.Now())
	}
}

func enableRequestFullDuplex(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	// 防止 net/http 在 handler 写出提前响应前同步排空未读请求体。正常读到
	// EOF 的请求不受影响；提前返回的请求由租约释放路径中断读取并关闭。
	_ = http.NewResponseController(c.Writer).EnableFullDuplex()
}

func (b *BodyMemoryBudget) reservationBytes(contentLength, routeLimit int64, contentEncoding string, contentTypes ...string) int64 {
	if b == nil || b.memory == nil || b.capacity <= 0 || routeLimit <= 0 {
		return 0
	}
	bodyBytes := contentLength
	if bodyBytes <= 0 {
		// 未知长度已经由 requestBodyAdmissionLimit 收紧到可足额预留的
		// effective limit，因此这里按该上限计算最坏内存占用。
		bodyBytes = routeLimit
	}
	if bodyBytes > routeLimit {
		bodyBytes = routeLimit
	}
	if bodyBytes < 0 {
		bodyBytes = 0
	}

	var estimate int64
	if isRequestBodyCompressed(contentEncoding) {
		decodedLimit := routeLimit
		if decodedLimit > pkghttputil.MaxDecompressedBodySize {
			decodedLimit = pkghttputil.MaxDecompressedBodySize
		}
		// 解压阶段同时存在压缩原文、解压结果和最坏六倍的规范化副本。
		decodedMultiplier := bodyMemoryReservationMultiplier
		if isJSONContentType(contentTypes...) {
			decodedMultiplier = jsonBodyMemoryReservationMultiplier
		}
		estimate = saturatingAdd(bodyBytes, saturatingMul(decodedLimit, decodedMultiplier))
	} else {
		multiplier := bodyMemoryReservationMultiplier
		// JSON 可能在宽松规范化时把一个控制字符扩展为六字节转义；
		// 原始 body 与规范化副本会短时共存，因此按七倍足额预留。
		if isJSONContentType(contentTypes...) {
			multiplier = jsonBodyMemoryReservationMultiplier
		}
		estimate = saturatingMul(bodyBytes, multiplier)
	}
	if estimate <= 0 {
		return 0
	}
	if estimate > b.capacity {
		// 单请求仍受 routeLimit 限制。估算值超过进程预算时让它独占整个
		// 预算，而不是返回永远无法通过重试恢复的 429。
		return b.capacity
	}
	return estimate
}

func isRequestBodyCompressed(contentEncoding string) bool {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "gzip", "x-gzip", "zstd", "deflate":
		return true
	default:
		return false
	}
}

func isJSONContentType(contentTypes ...string) bool {
	for _, raw := range contentTypes {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
			return true
		}
	}
	return false
}

func saturatingMul(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

func saturatingAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func markBodyAdmissionRejected(c *gin.Context, reason BodyAdmissionRejectReason) {
	if reason == BodyAdmissionRejectRead {
		MarkIngressRejected(c, IngressRejectBodyReadSlots)
		return
	}
	MarkIngressRejected(c, IngressRejectBodyMemoryBudget)
}

func writeBodyAdmissionError(c *gin.Context, retryAfterSeconds int) {
	message := "Too many request bodies are being processed; please retry later"
	if c == nil || c.Request == nil {
		return
	}
	path := ""
	if c.Request.URL != nil {
		path = strings.ToLower(c.Request.URL.Path)
	}
	if retryAfterSeconds <= 0 {
		// 429 响应仍给出最小重试提示，避免客户端立即忙循环。
		retryAfterSeconds = 1
	}
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
	}
	switch {
	case strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta"):
		GoogleErrorWriter(c, http.StatusTooManyRequests, message)
	case strings.Contains(path, "/messages") || strings.HasPrefix(path, "/antigravity"):
		AnthropicRateLimitErrorWriter(c, http.StatusTooManyRequests, message)
	default:
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"type":    "rate_limit_error",
				"code":    "request_body_admission_exhausted",
				"message": message,
			},
		})
		return
	}
	c.Abort()
}

func abortBodyReadFailure(c *gin.Context) {
	if c == nil {
		return
	}
	closeRequestBody(c)
	message := "Failed to read request body"
	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = strings.ToLower(c.Request.URL.Path)
	}
	switch {
	case strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta"):
		GoogleErrorWriter(c, http.StatusBadRequest, message)
	case strings.Contains(path, "/messages") || strings.HasPrefix(path, "/antigravity"):
		AnthropicInvalidRequestErrorWriter(c, http.StatusBadRequest, message)
	default:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": message,
			},
		})
		return
	}
	c.Abort()
}

// AbortBodyTooLarge 在读取或预留请求体资源前拒绝已声明超限的请求。
func AbortBodyTooLarge(c *gin.Context, limit int64) {
	if c == nil {
		return
	}
	closeRequestBody(c)
	message := "Request body too large"
	if limit > 0 {
		c.Header("X-Request-Body-Limit", strconv.FormatInt(limit, 10))
	}
	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = strings.ToLower(c.Request.URL.Path)
	}
	switch {
	case strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta"):
		GoogleErrorWriter(c, http.StatusRequestEntityTooLarge, message)
	case strings.Contains(path, "/messages") || strings.HasPrefix(path, "/antigravity"):
		AnthropicInvalidRequestErrorWriter(c, http.StatusRequestEntityTooLarge, message)
	default:
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": message,
				"param":   nil,
				"code":    "request_body_too_large",
			},
		})
		return
	}
	c.Abort()
}

func budgetWaitSeconds(budget *BodyMemoryBudget) int {
	if budget == nil || budget.wait <= 0 {
		return 0
	}
	return int(budget.wait / time.Second)
}
