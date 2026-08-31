package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func newAdmissionTestContext(req *http.Request) *gin.Context {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = req
	return c
}

func newAdmissionTestRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewBufferString(body))
	// 默认使用普通二进制类型；JSON 专用的预留倍率在专门测试中覆盖。
	req.Header.Set("Content-Type", "application/octet-stream")
	return req
}

type budgetTypedNilBody struct{}

func (*budgetTypedNilBody) Read([]byte) (int, error) { return 0, io.EOF }
func (*budgetTypedNilBody) Close() error             { return nil }

func TestAcquireRequestBodyAdmissionReleasesReadAtEOFButRetainsMemoryUntilRelease(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	first := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	lease, reason, err := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	_, err = io.ReadAll(first.Request.Body)
	require.NoError(t, err)

	// EOF 后读取槽已经归还，但 8 字节的内存租约仍在持有，第二个请求
	// 应该因内存预算不足而失败。
	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	t.Logf("second content length=%d reservation=%d", second.Request.ContentLength, budget.reservationBytes(second.Request.ContentLength, 16, second.GetHeader("Content-Encoding")))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, secondErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, secondReason)
	require.Nil(t, secondLease)

	lease.Release()
	third := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	thirdLease, thirdReason, thirdErr := AcquireRequestBodyAdmission(third, budget, 16)
	require.NoError(t, thirdErr)
	require.Empty(t, thirdReason)
	require.NotNil(t, thirdLease)
	thirdLease.Release()
}

func TestAcquireRequestBodyAdmissionRejectsAndThenReusesReadSlot(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	first := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	firstLease, reason, err := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, firstLease)

	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, secondErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectRead, secondReason)
	require.Nil(t, secondLease)

	// 关闭请求体即使没有读到 EOF，也必须归还读取槽。
	require.NoError(t, first.Request.Body.Close())
	third := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	thirdLease, thirdReason, thirdErr := AcquireRequestBodyAdmission(third, budget, 16)
	require.NoError(t, thirdErr)
	require.Empty(t, thirdReason)
	require.NotNil(t, thirdLease)
	thirdLease.Release()
	firstLease.Release()
}

func TestCachedBodyDoesNotOverReleaseReadSlot(t *testing.T) {
	budget := NewBodyMemoryBudget(64, 0, 1)
	cachedReq := newAdmissionTestRequest("ignored")
	cachedReq = pkghttputil.WithCachedRequestBody(cachedReq, []byte("1234"))
	cached := newAdmissionTestContext(cachedReq)
	cachedLease, reason, err := AcquireRequestBodyAdmission(cached, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, cachedLease)
	defer cachedLease.Release()

	// 缓存体没有网络 reader，不应获取或释放读取槽。第一个真实 body
	// 占满唯一读取槽后，第二个真实 body 必须被拒绝。
	first := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	firstLease, firstReason, firstErr := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, firstErr)
	require.Empty(t, firstReason)
	require.NotNil(t, firstLease)
	defer firstLease.Release()

	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, secondErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectRead, secondReason)
	require.Nil(t, secondLease)
}

type admissionErrorBody struct{}

func (admissionErrorBody) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (admissionErrorBody) Close() error             { return nil }

func TestAdmissionReadErrorReleasesReadSlot(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	req := newAdmissionTestRequest("ignored")
	req.Body = admissionErrorBody{}
	first := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	_, readErr := io.ReadAll(first.Request.Body)
	require.Error(t, readErr)

	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.NoError(t, secondErr)
	require.Empty(t, secondReason)
	require.NotNil(t, secondLease)
	secondLease.Release()
	lease.Release()
}

func TestAdmissionContextCancellationReleasesLeaseExactlyOnce(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	ctx, cancel := context.WithCancel(context.Background())
	req := newAdmissionTestRequest("1234").WithContext(ctx)
	first := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	cancel()
	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	_, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, secondErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, secondReason)

	// AfterFunc 与请求链 defer 可能同时调用 Release，重复释放不能让信号量
	// 超过容量；再次申请应仍然只能成功一次。
	lease.Release()
	lease.Release()
	third := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	thirdLease, _, thirdErr := AcquireRequestBodyAdmission(third, budget, 16)
	require.NoError(t, thirdErr)
	require.NotNil(t, thirdLease)
	thirdLease.Release()
}

type nonComparableAdmissionBody struct {
	data []byte
}

func (b nonComparableAdmissionBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (nonComparableAdmissionBody) Close() error { return nil }

func TestAcquireRequestBodyAdmissionHandlesNonComparableBody(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	req := newAdmissionTestRequest("ignored")
	// 该值类型含 slice，作为 interface 比较会 panic；准入只应按 nil/
	// http.NoBody 类型安全判断。
	req.Body = nonComparableAdmissionBody{data: []byte("1234")}
	c := newAdmissionTestContext(req)

	var lease *RequestBodyAdmissionLease
	var reason BodyAdmissionRejectReason
	var err error
	require.NotPanics(t, func() {
		lease, reason, err = AcquireRequestBodyAdmission(c, budget, 16)
	})
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	lease.Release()
}

type nonIdempotentCloseBody struct {
	closes atomic.Int32
}

func (b *nonIdempotentCloseBody) Read([]byte) (int, error) { return 0, io.EOF }

func (b *nonIdempotentCloseBody) Close() error {
	if b.closes.Add(1) > 1 {
		return errors.New("body closed more than once")
	}
	return nil
}

func TestAdmissionBodyCloseIsIdempotentAcrossCacheReplacementAndCancellation(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := &nonIdempotentCloseBody{}
	req := newAdmissionTestRequest("1234").WithContext(ctx)
	req.Body = original
	c := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	// 安装缓存会关闭旧的准入包装；随后取消触发 AfterFunc，底层 body
	// 仍应只收到一次 Close。
	_ = pkghttputil.WithCachedRequestBody(req, []byte("1234"))
	cancel()
	require.Eventually(t, func() bool {
		return original.closes.Load() == 1
	}, time.Second, time.Millisecond)
	lease.Release()
	require.Equal(t, int32(1), original.closes.Load())
}

type blockingAdmissionBody struct {
	closed chan struct{}
	once   sync.Once
}

type admissionDoneNotifyContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *admissionDoneNotifyContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func newBlockingAdmissionBody() *blockingAdmissionBody {
	return &blockingAdmissionBody{closed: make(chan struct{})}
}

func (b *blockingAdmissionBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, errors.New("body closed")
}

func (b *blockingAdmissionBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestAdmissionCancellationClosesBlockingBodyRead(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	ctx, cancel := context.WithCancel(context.Background())
	body := newBlockingAdmissionBody()
	req := newAdmissionTestRequest("").WithContext(ctx)
	req.Body = body
	req.ContentLength = -1
	first := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(first, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(first.Request.Body)
		readDone <- readErr
	}()
	cancel()

	select {
	case readErr := <-readDone:
		require.Error(t, readErr)
	case <-time.After(time.Second):
		t.Fatal("blocking body read did not exit after request cancellation")
	}
	lease.Release()
}

func TestAdmissionWaitHonorsRequestCancellation(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 5, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notifyCtx := &admissionDoneNotifyContext{Context: ctx, observed: make(chan struct{})}
	waiting := newAdmissionTestContext(newAdmissionTestRequest("1234").WithContext(notifyCtx))
	result := make(chan error, 1)
	go func() {
		_, _, acquireErr := AcquireRequestBodyAdmission(waiting, budget, 16)
		result <- acquireErr
	}()

	// 等待 semaphore 读取自定义 context 的 Done 通道，确认请求已经进入
	// 等待分支后再取消，避免竞争导致偶发成功。
	select {
	case <-notifyCtx.observed:
	case <-time.After(time.Second):
		t.Fatal("admission wait did not observe request context")
	}
	cancel()
	select {
	case acquireErr := <-result:
		require.ErrorIs(t, acquireErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("admission wait did not honor request cancellation")
	}
}

func TestBodyMemoryAdmissionLetsSmallRequestBypassLargeWaiter(t *testing.T) {
	budget := NewBodyMemoryBudget(100, 2)
	holder := newAdmissionTestContext(newAdmissionTestRequest(string(bytes.Repeat([]byte("h"), 30))))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 100)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	type admissionResult struct {
		lease  *RequestBodyAdmissionLease
		reason BodyAdmissionRejectReason
		err    error
	}
	largeCtx, cancelLarge := context.WithCancel(context.Background())
	defer cancelLarge()
	largeResult := make(chan admissionResult, 1)
	go func() {
		req := newAdmissionTestRequest(string(bytes.Repeat([]byte("l"), 30))).WithContext(largeCtx)
		lease, reason, acquireErr := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
		largeResult <- admissionResult{lease: lease, reason: reason, err: acquireErr}
	}()
	require.Eventually(t, func() bool {
		return budget.memoryAvailable.waiters.Load() > 0
	}, time.Second, time.Millisecond, "大请求应先进入内存等待队列")

	// holder 占用 60，large 还需要 60，剩余 40 足够 small 的 20。
	// 小请求不应被排在前面但暂时无法满足的大请求阻塞。
	smallCtx, cancelSmall := context.WithCancel(context.Background())
	defer cancelSmall()
	smallResult := make(chan admissionResult, 1)
	go func() {
		req := newAdmissionTestRequest(string(bytes.Repeat([]byte("s"), 10))).WithContext(smallCtx)
		lease, reason, acquireErr := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
		smallResult <- admissionResult{lease: lease, reason: reason, err: acquireErr}
	}()
	select {
	case result := <-smallResult:
		require.NoError(t, result.err)
		require.Empty(t, result.reason)
		require.NotNil(t, result.lease)
		result.lease.Release()
	case <-time.After(250 * time.Millisecond):
		t.Fatal("小请求被前方无法满足的大请求阻塞")
	}

	select {
	case result := <-largeResult:
		if result.lease != nil {
			result.lease.Release()
		}
		t.Fatalf("holder 释放前大请求不应完成准入: reason=%s err=%v", result.reason, result.err)
	default:
	}

	holderLease.Release()
	select {
	case result := <-largeResult:
		require.NoError(t, result.err)
		require.Empty(t, result.reason)
		require.NotNil(t, result.lease)
		result.lease.Release()
	case <-time.After(time.Second):
		t.Fatal("内存释放后大请求未被唤醒")
	}
}

func TestUnknownBodyBudgetLetsSmallRequestBypassLargeWaiter(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 2)
	newUnknown := func() *gin.Context {
		req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		req.Body = io.NopCloser(strings.NewReader("x"))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/octet-stream")
		return newAdmissionTestContext(req)
	}

	holders := make([]*RequestBodyAdmissionLease, 0, 4)
	for _, limit := range []int64{50, 50, 50, 25} {
		lease, reason, err := AcquireRequestBodyAdmission(newUnknown(), budget, limit)
		require.NoError(t, err)
		require.Empty(t, reason)
		require.NotNil(t, lease)
		holders = append(holders, lease)
	}
	defer func() {
		for _, lease := range holders {
			lease.Release()
		}
	}()

	type admissionResult struct {
		lease  *RequestBodyAdmissionLease
		reason BodyAdmissionRejectReason
		err    error
	}
	largeResult := make(chan admissionResult, 1)
	go func() {
		lease, reason, err := AcquireRequestBodyAdmission(newUnknown(), budget, 50)
		largeResult <- admissionResult{lease: lease, reason: reason, err: err}
	}()
	require.Eventually(t, func() bool {
		return budget.unknownMemoryAvailable.waiters.Load() > 0
	}, time.Second, time.Millisecond, "大 unknown 请求应先等待子预算")

	// holders 占用 350，剩余 50 放不下 reservation=100 的 large，
	// 但足够 reservation=16 的 small；small 不应被 FIFO 队头阻塞。
	smallResult := make(chan admissionResult, 1)
	go func() {
		lease, reason, err := AcquireRequestBodyAdmission(newUnknown(), budget, 8)
		smallResult <- admissionResult{lease: lease, reason: reason, err: err}
	}()
	select {
	case result := <-smallResult:
		require.NoError(t, result.err)
		require.Empty(t, result.reason)
		require.NotNil(t, result.lease)
		result.lease.Release()
	case <-time.After(250 * time.Millisecond):
		t.Fatal("小 unknown 请求被前方无法满足的大请求阻塞")
	}

	select {
	case result := <-largeResult:
		if result.lease != nil {
			result.lease.Release()
		}
		t.Fatalf("释放 holder 前大 unknown 请求不应完成准入: reason=%s err=%v", result.reason, result.err)
	default:
	}

	holders[0].Release()
	select {
	case result := <-largeResult:
		require.NoError(t, result.err)
		require.Empty(t, result.reason)
		require.NotNil(t, result.lease)
		result.lease.Release()
	case <-time.After(time.Second):
		t.Fatal("unknown 子预算释放后大请求未被唤醒")
	}
}

func TestBodyMemoryAvailabilitySkipsBroadcastWithoutWaiters(t *testing.T) {
	availability := newBodyMemoryAvailability()
	ready := availability.ready

	availability.notify()

	require.Equal(t, ready, availability.ready)
}

func TestBodyMemoryAvailabilityBroadcastsToSubscribers(t *testing.T) {
	availability := newBodyMemoryAvailability()
	first := availability.subscribe()
	second := availability.subscribe()
	defer availability.unsubscribe()
	defer availability.unsubscribe()

	availability.notify()

	for _, ready := range []<-chan struct{}{first, second} {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("内存释放通知未广播给全部等待者")
		}
	}
}

func TestMemoryAcquireReturnsTokenWhenReleaseRacesWithCancellation(t *testing.T) {
	for range 50 {
		sem := semaphore.NewWeighted(1)
		require.True(t, sem.TryAcquire(1))
		available := newBodyMemoryAvailability()
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			result <- acquireMemoryWithoutHeadOfLineBlocking(ctx, sem, available, 1, false)
		}()
		require.Eventually(t, func() bool {
			return available.waiters.Load() > 0
		}, time.Second, time.Millisecond)

		// 先归还 token，但暂不广播；随后让取消和资源通知同时可见，覆盖
		// waiter 选择 ready 分支后 TryAcquire 成功的竞态。
		sem.Release(1)
		cancel()
		available.notify()
		require.ErrorIs(t, <-result, context.Canceled)
		require.True(t, sem.TryAcquire(1), "取消请求抢到的 token 必须立即归还")
		sem.Release(1)
	}
}

func TestImmediateReadSlotAcquireHonorsCancellation(t *testing.T) {
	sem := semaphore.NewWeighted(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, acquireSemaphore(ctx, sem, 1, true), context.Canceled)
	require.True(t, sem.TryAcquire(1), "取消请求不能占用读取槽")
	sem.Release(1)
}

func TestRequestBodyAdmissionMiddlewareRejectsWithoutCallingHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)

	handlerCalled := false
	router := gin.New()
	router.POST("/v1/responses", RequestBodyAdmission(budget, 16), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := newAdmissionTestRequest("1234")
	req.URL.Path = "/v1/responses"
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.False(t, handlerCalled)
	require.Contains(t, w.Body.String(), "request_body_admission_exhausted")

	holderLease.Release()
}

func TestDeferredBodyAdmissionReusesControllerWithoutDoubleAcquire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"async":true,"client_request_id":"request_once","model":"gpt-image-2"}`
	budget := NewBodyMemoryBudget(int64(len(body))*jsonBodyMemoryReservationMultiplier, 0, 1)
	var triggerCalls, handlerCalls int

	router := gin.New()
	router.Use(DeferredRequestBodyAdmissionForGateway(budget, 1024, 1024))
	router.Use(func(c *gin.Context) {
		triggerCalls++
		cached, ok := readAndCacheDeferredRequestBody(c)
		require.True(t, ok)
		require.Equal(t, body, string(cached))
		c.Next()
	})
	router.Use(RequestBodyAdmissionForGateway(budget, 1024, 1024))
	router.POST("/v1/images/generations", func(c *gin.Context) {
		handlerCalls++
		cached, ok := pkghttputil.CachedRequestBody(c.Request)
		require.True(t, ok)
		require.Equal(t, body, string(cached))
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, 1, triggerCalls)
	require.Equal(t, 1, handlerCalls)

	// 外层 controller 在请求结束后应归还完整预算。
	probe := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
	probe.Header.Set("Content-Type", "application/json")
	probeLease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(probe), budget, 1024)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, probeLease)
	probeLease.Release()
}

func TestDeferredBodyAdmissionFailureStopsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	var downstreamCalled bool
	router := gin.New()
	router.Use(DeferredRequestBodyAdmissionForGateway(budget, 16, 16))
	router.Use(func(c *gin.Context) {
		_, _ = readAndCacheDeferredRequestBody(c)
		if c.IsAborted() {
			return
		}
		c.Next()
	})
	router.Use(RequestBodyAdmissionForGateway(budget, 16, 16))
	router.POST("/v1/images/generations", func(c *gin.Context) {
		downstreamCalled = true
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"async":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.False(t, downstreamCalled)
	require.Contains(t, recorder.Body.String(), "request_body_admission_exhausted")
}

func TestDeferredBodyAdmissionDeclaredOversizeStopsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var downstreamCalled bool
	router := gin.New()
	router.Use(DeferredRequestBodyAdmissionForGateway(nil, 4, 4))
	router.Use(func(c *gin.Context) {
		_, _ = readAndCacheDeferredRequestBody(c)
		if c.IsAborted() {
			return
		}
		c.Next()
	})
	router.Use(RequestBodyAdmissionForGateway(nil, 4, 4))
	router.POST("/v1/images/generations", func(c *gin.Context) {
		downstreamCalled = true
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString("12345"))
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.False(t, downstreamCalled)
}

func TestDeferredBodyAdmissionPreservesDetachedLeaseOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"async":true,"client_request_id":"request_handoff"}`
	budget := NewBodyMemoryBudget(int64(len(body))*jsonBodyMemoryReservationMultiplier, 0, 1)
	var handedOff *RequestBodyAdmissionLease

	router := gin.New()
	router.Use(DeferredRequestBodyAdmissionForGateway(budget, 1024, 1024))
	router.Use(func(c *gin.Context) {
		_, ok := readAndCacheDeferredRequestBody(c)
		require.True(t, ok)
		c.Next()
	})
	router.Use(RequestBodyAdmissionForGateway(budget, 1024, 1024))
	router.POST("/v1/images/generations", func(c *gin.Context) {
		handedOff = TakeRequestBodyAdmissionLease(c)
		require.NotNil(t, handedOff)
		c.Status(http.StatusAccepted)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.NotNil(t, handedOff)

	probe := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
	probe.Header.Set("Content-Type", "application/json")
	probeLease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(probe), budget, 1024)
	require.ErrorIs(t, err, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, reason)
	require.Nil(t, probeLease)

	handedOff.Release()
	releasedProbe := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
	releasedProbe.Header.Set("Content-Type", "application/json")
	releasedLease, releasedReason, releasedErr := AcquireRequestBodyAdmission(newAdmissionTestContext(releasedProbe), budget, 1024)
	require.NoError(t, releasedErr)
	require.Empty(t, releasedReason)
	require.NotNil(t, releasedLease)
	releasedLease.Release()
}

func TestBodyAdmissionRejectsSlowPartialUploadWithoutDraining(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	router := gin.New()
	router.POST("/v1/responses", RequestBodyAdmission(budget, 16), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = io.WriteString(conn,
		"POST /v1/responses HTTP/1.1\r\n"+
			"Host: example.test\r\n"+
			"Content-Type: application/octet-stream\r\n"+
			"Content-Length: 4\r\n\r\n1",
	)
	require.NoError(t, err)

	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	require.NoError(t, err, "拒绝响应不应等待客户端补完剩余请求体")
	defer response.Body.Close()
	require.Equal(t, http.StatusTooManyRequests, response.StatusCode)
}

func TestTakeRequestBodyAdmissionLeaseTransfersOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0, 1)
	var handedOff *RequestBodyAdmissionLease

	router := gin.New()
	router.POST("/v1/images/generations/async", RequestBodyAdmission(budget, 16), func(c *gin.Context) {
		handedOff = TakeRequestBodyAdmissionLease(c)
		require.NotNil(t, handedOff)
		c.Status(http.StatusAccepted)
	})
	recorder := httptest.NewRecorder()
	req := newAdmissionTestRequest("1234")
	req.URL.Path = "/v1/images/generations/async"
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.NotNil(t, handedOff)

	// handler 返回后租约仍由后台任务持有，内存预算不能提前被下一个请求占用。
	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, reason, err := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, err, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, reason)
	require.Nil(t, secondLease)

	handedOff.Release()
	third := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	thirdLease, thirdReason, thirdErr := AcquireRequestBodyAdmission(third, budget, 16)
	require.NoError(t, thirdErr)
	require.Empty(t, thirdReason)
	require.NotNil(t, thirdLease)
	thirdLease.Release()
}

func TestTakeRequestBodyAdmissionLeaseDropsConsumedBodyReferences(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	req := newAdmissionTestRequest("1234")
	c := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	require.NotNil(t, lease.body)
	require.NotNil(t, lease.stopCancel)

	_, err = io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.True(t, lease.readDone.Load())
	c.Set(requestBodyAdmissionLeaseKey, lease)

	handedOff := TakeRequestBodyAdmissionLease(c)
	require.Same(t, lease, handedOff)
	require.Nil(t, handedOff.body, "已物化的异步 body 不应继续持有 MaxBytesReader/ResponseWriter")
	require.Nil(t, handedOff.stopCancel, "已物化的 body 不需要继续注册请求取消回调")

	// memory token 仍由后台 owner 持有，直到显式 Release。
	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.ErrorIs(t, secondErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, secondReason)
	require.Nil(t, secondLease)

	handedOff.Release()
}

func TestTakeRequestBodyAdmissionLeaseKeepsUnreadBodyForFinalRelease(t *testing.T) {
	budget := NewBodyMemoryBudget(32, 0, 1)
	body := &admissionCloseTrackingBody{Reader: bytes.NewReader([]byte("1234"))}
	req := newAdmissionTestRequest("")
	req.Body = body
	req.ContentLength = 4
	c := newAdmissionTestContext(req)
	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	c.Set(requestBodyAdmissionLeaseKey, lease)

	handedOff := TakeRequestBodyAdmissionLease(c)
	require.Same(t, lease, handedOff)
	require.NotNil(t, handedOff.body, "未读完 body 仍需由最终 owner 关闭")
	handedOff.Release()
	require.Equal(t, int32(1), body.closed.Load())
}

func TestTakeRequestBodyAdmissionLeaseIsSingleUse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	lease := &RequestBodyAdmissionLease{}
	c.Set(requestBodyAdmissionLeaseKey, lease)
	require.Same(t, lease, TakeRequestBodyAdmissionLease(c))
	require.Nil(t, TakeRequestBodyAdmissionLease(c))
	lease.Release()
}

func TestUnknownBodyHandoffReleasesBothBudgetsExactlyOnce(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 0, 1)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader("x"))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	c := newAdmissionTestContext(req)

	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 100)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	require.True(t, lease.unknownMemoryAcquired)
	_, err = io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	c.Set(requestBodyAdmissionLeaseKey, lease)

	handedOff := TakeRequestBodyAdmissionLease(c)
	require.Same(t, lease, handedOff)
	require.True(t, budget.memory.TryAcquire(budget.capacity-handedOff.reservation))
	require.False(t, budget.memory.TryAcquire(1), "handoff 期间仍应持有主内存预算")
	budget.memory.Release(budget.capacity - handedOff.reservation)
	require.True(t, budget.unknownMemory.TryAcquire(budget.unknownCapacity-handedOff.unknownReservation))
	require.False(t, budget.unknownMemory.TryAcquire(1), "handoff 期间仍应持有 unknown 子预算")
	budget.unknownMemory.Release(budget.unknownCapacity - handedOff.unknownReservation)

	handedOff.Release()
	handedOff.Release()
	require.True(t, budget.memory.TryAcquire(budget.capacity))
	budget.memory.Release(budget.capacity)
	require.True(t, budget.unknownMemory.TryAcquire(budget.unknownCapacity))
	budget.unknownMemory.Release(budget.unknownCapacity)
}

func TestUnknownBodyReadSlotFailureReleasesBothBudgets(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 0, 1)
	holder, _, err := AcquireRequestBodyAdmission(
		newAdmissionTestContext(newAdmissionTestRequest("x")),
		budget,
		100,
	)
	require.NoError(t, err)
	require.NotNil(t, holder)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader("x"))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	failed, reason, acquireErr := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
	require.ErrorIs(t, acquireErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectRead, reason)
	require.Nil(t, failed)

	holder.Release()
	require.True(t, budget.memory.TryAcquire(budget.capacity), "读取槽失败后主预算必须完整归还")
	budget.memory.Release(budget.capacity)
	require.True(t, budget.unknownMemory.TryAcquire(budget.unknownCapacity), "读取槽失败后 unknown 子预算必须完整归还")
	budget.unknownMemory.Release(budget.unknownCapacity)
}

func TestGatewayBodyAdmissionProtectsBodyRoutesAndSkipsNoBodyMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		wantStatus  int
		wantCalled  bool
	}{
		{
			name:        "responses websocket",
			method:      http.MethodGet,
			path:        "/v1/responses",
			contentType: "application/json",
			wantStatus:  http.StatusNoContent,
			wantCalled:  true,
		},
		{
			name:        "multipart image",
			method:      http.MethodPost,
			path:        "/v1/images/edits",
			contentType: "multipart/form-data; boundary=test",
			wantStatus:  http.StatusTooManyRequests,
			wantCalled:  false,
		},
		{
			name:        "native gemini",
			method:      http.MethodPost,
			path:        "/v1beta/models/gemini-2.5-pro:generateContent",
			contentType: "application/json",
			wantStatus:  http.StatusTooManyRequests,
			wantCalled:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := NewBodyMemoryBudget(8, 0, 1)
			holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
			holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
			require.NoError(t, err)
			require.NotNil(t, holderLease)
			defer holderLease.Release()

			handlerCalled := false
			router := gin.New()
			router.Any("/*path", RequestBodyAdmissionForGateway(budget, 16, 8), func(c *gin.Context) {
				handlerCalled = true
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString("body"))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code)
			require.Equal(t, tt.wantCalled, handlerCalled)
		})
	}
}

func TestGatewayBodyAdmissionTreatsZeroLengthBodyAsUnknown(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	req := newAdmissionTestRequest("1234")
	req.ContentLength = 0
	req.Header.Set("Content-Length", "0")
	c := newAdmissionTestContext(req)
	lease, reason, acquireErr := AcquireRequestBodyAdmission(c, budget, 16)
	require.ErrorIs(t, acquireErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, reason)
	require.Nil(t, lease)
}

func TestRequestBodyAdmissionRejectsDeclaredOversizeBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlerCalled := false
	router.POST("/v1/responses", RequestBodyAdmission(nil, 4), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString("12345"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.False(t, handlerCalled)
}

func TestGatewayBodyRouteLimitUsesTextLimitForTextEndpoints(t *testing.T) {
	const defaultLimit = int64(256)
	const textLimit = int64(32)
	for _, path := range []string{
		"/v1/alpha/search",
		"/alpha/search/",
		"/backend-api/codex/alpha/search",
		"/v1/embeddings",
		"/embeddings/",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		require.Equal(t, textLimit, GatewayBodyRouteLimit(req, defaultLimit, textLimit), path)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.Equal(t, defaultLimit, GatewayBodyRouteLimit(req, defaultLimit, textLimit))
}

func TestGatewayBodyAdmissionAppliesRouteLimitBeforeDownstreamRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/alpha/search", RequestBodyAdmissionForGateway(nil, 64, 4), func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.String(http.StatusOK, string(body))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewBufferString("12345"))
	// 模拟 chunked/代理未转发 Content-Length：声明长度检查无法帮助，
	// 仍必须由准入层安装的 route-specific MaxBytesReader 拦截。
	req.ContentLength = -1
	req.Header.Del("Content-Length")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestGatewayBodyAdmissionAppliesRouteLimitAfterDecompression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write(bytes.Repeat([]byte("x"), 128))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	router := gin.New()
	router.POST("/v1/alpha/search", RequestBodyAdmissionForGateway(nil, 256, 64), func(c *gin.Context) {
		_, readErr := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
			c.Header("X-Decoded-Limit", strconv.FormatInt(maxErr.Limit, 10))
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Equal(t, "64", recorder.Header().Get("X-Decoded-Limit"))
}

func TestBodyMemoryReservationAddsJSONNormalizationHeadroom(t *testing.T) {
	budget := NewBodyMemoryBudget(1024, 0)

	// 已知长度的 JSON 按 7 倍预留，覆盖原始 body 与最坏六倍转义副本共存。
	require.Equal(t, int64(70), budget.reservationBytes(10, 100, "", "application/json; charset=utf-8"))
	require.Equal(t, int64(70), budget.reservationBytes(10, 100, "", "application/problem+json"))

	// 未知长度使用已经收紧的 effective limit，并按 JSON 最坏七倍足额预留。
	require.Equal(t, int64(700), budget.reservationBytes(-1, 100, "", "application/json"))
	require.Equal(t, int64(20), budget.reservationBytes(10, 100, "", "text/plain"))
}

func TestUnknownBodyAdmissionLimitCapsByRouteAndBudget(t *testing.T) {
	newUnknownRequest := func(path, contentType, encoding string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Body = io.NopCloser(strings.NewReader("x"))
		req.ContentLength = -1
		req.Header.Del("Content-Length")
		req.Header.Set("Content-Type", contentType)
		if encoding != "" {
			req.Header.Set("Content-Encoding", encoding)
		}
		return req
	}

	defaultBudget := NewBodyMemoryBudget(512<<20, 0)
	jsonRequest := newUnknownRequest("/v1/responses", "application/octet-stream", "")
	require.Equal(t, unknownBodyMaxBytes, requestBodyAdmissionLimit(defaultBudget, jsonRequest, 256<<20))

	compressedJSON := newUnknownRequest("/v1/responses", "application/json", "gzip")
	require.Equal(t, unknownBodyMaxBytes, requestBodyAdmissionLimit(defaultBudget, compressedJSON, 256<<20))

	smallBudget := NewBodyMemoryBudget(48<<20, 0)
	require.Equal(t, int64((48<<20)/unknownBodyPerRequestBudgetDivisor/jsonBodyMemoryReservationMultiplier), requestBodyAdmissionLimit(smallBudget, jsonRequest, 256<<20))
	binaryRequest := newUnknownRequest("/v1/images/edits", "application/octet-stream", "")
	require.Equal(t, int64(3<<20), requestBodyAdmissionLimit(smallBudget, binaryRequest, 256<<20))
	require.Equal(t, int64(512), requestBodyAdmissionLimit(defaultBudget, jsonRequest, 512))
}

func TestAcquireRequestBodyAdmissionInstallsUnknownLengthHardLimit(t *testing.T) {
	budget := NewBodyMemoryBudget(64, 0, 1)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader("{}"))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	c := newAdmissionTestContext(req)

	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 100)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	defer lease.Release()

	_, readErr := io.ReadAll(c.Request.Body)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, readErr, &maxErr)
	require.Equal(t, int64(1), maxErr.Limit)
	require.Equal(t, int64(1), pkghttputil.DecompressedBodyLimit(c.Request))
}

func TestUnknownBodyAdmissionRejectsWhenBudgetCannotHonorPerRequestShare(t *testing.T) {
	budget := NewBodyMemoryBudget(16, 0, 1)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader("x"))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")

	lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
	require.ErrorIs(t, err, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, reason)
	require.Nil(t, lease)
	require.True(t, budget.memory.TryAcquire(budget.capacity))
	budget.memory.Release(budget.capacity)
	require.True(t, budget.unknownMemory.TryAcquire(budget.unknownCapacity))
	budget.unknownMemory.Release(budget.unknownCapacity)
}

func TestUnknownBodyAdmissionDoesNotExhaustKnownLengthBudget(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 0)
	unknownRequest := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	unknownRequest.Body = io.NopCloser(strings.NewReader("x"))
	unknownRequest.ContentLength = -1
	unknownRequest.Header.Set("Content-Type", "application/json")
	unknown := newAdmissionTestContext(unknownRequest)
	unknownLease, reason, err := AcquireRequestBodyAdmission(unknown, budget, 100)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, unknownLease)
	defer unknownLease.Release()
	require.LessOrEqual(t, unknownLease.reservation, int64(100))

	knownRequest := newAdmissionTestRequest("1234567890")
	known := newAdmissionTestContext(knownRequest)
	knownLease, knownReason, knownErr := AcquireRequestBodyAdmission(known, budget, 100)
	require.NoError(t, knownErr)
	require.Empty(t, knownReason)
	require.NotNil(t, knownLease)
	knownLease.Release()
}

func TestUnknownBodyAggregateBudgetLeavesCapacityForKnownRequests(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 0)
	leases := make([]*RequestBodyAdmissionLease, 0, 4)
	for range 4 {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		req.Body = io.NopCloser(strings.NewReader("x"))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
		require.NoError(t, err)
		require.Empty(t, reason)
		require.NotNil(t, lease)
		leases = append(leases, lease)
	}
	defer func() {
		for _, lease := range leases {
			lease.Release()
		}
	}()

	blockedReq := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	blockedReq.Body = io.NopCloser(strings.NewReader("x"))
	blockedReq.ContentLength = -1
	blockedReq.Header.Set("Content-Type", "application/json")
	blockedLease, blockedReason, blockedErr := AcquireRequestBodyAdmission(newAdmissionTestContext(blockedReq), budget, 100)
	require.ErrorIs(t, blockedErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, blockedReason)
	require.Nil(t, blockedLease)

	knownLease, knownReason, knownErr := AcquireRequestBodyAdmission(
		newAdmissionTestContext(newAdmissionTestRequest("1234567890")),
		budget,
		100,
	)
	require.NoError(t, knownErr)
	require.Empty(t, knownReason)
	require.NotNil(t, knownLease)
	knownLease.Release()
}

func TestUnknownBodyAdmissionReleasesSubBudgetAfterGlobalAcquireFailure(t *testing.T) {
	budget := NewBodyMemoryBudget(800, 0)
	holderRequest := newAdmissionTestRequest(strings.Repeat("x", 400))
	holderLease, _, err := AcquireRequestBodyAdmission(newAdmissionTestContext(holderRequest), budget, 400)
	require.NoError(t, err)
	require.NotNil(t, holderLease)

	newUnknown := func() *gin.Context {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		req.Body = io.NopCloser(strings.NewReader("x"))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		return newAdmissionTestContext(req)
	}
	failedLease, failedReason, failedErr := AcquireRequestBodyAdmission(newUnknown(), budget, 100)
	require.ErrorIs(t, failedErr, errBodyAdmissionUnavailable)
	require.Equal(t, BodyAdmissionRejectMemory, failedReason)
	require.Nil(t, failedLease)
	holderLease.Release()

	leases := make([]*RequestBodyAdmissionLease, 0, 4)
	for range 4 {
		lease, reason, acquireErr := AcquireRequestBodyAdmission(newUnknown(), budget, 100)
		require.NoError(t, acquireErr)
		require.Empty(t, reason)
		require.NotNil(t, lease)
		leases = append(leases, lease)
	}
	for _, lease := range leases {
		lease.Release()
	}
}

func TestUnknownCompressedJSONUsesEffectiveLimitForRawAndDecodedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const effectiveLimit = int64(1024)
	budget := NewBodyMemoryBudget(effectiveLimit*(jsonBodyMemoryReservationMultiplier+1)*unknownBodyPerRequestBudgetDivisor, 0, 4)

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, err := gz.Write(bytes.Repeat([]byte("x"), int(effectiveLimit+1)))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	router := gin.New()
	router.POST("/v1/responses", RequestBodyAdmissionForGateway(budget, 16<<10, 16<<10), func(c *gin.Context) {
		_, readErr := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
			AbortBodyTooLarge(c, maxErr.Limit)
			return
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed.Bytes()))
	req.ContentLength = -1
	req.Header.Del("Content-Length")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Equal(t, strconv.FormatInt(effectiveLimit, 10), recorder.Header().Get("X-Request-Body-Limit"))
}

func TestBodyAdmissionUsesRouteJSONHeadroomWhenContentTypeIsMisleading(t *testing.T) {
	budget := NewBodyMemoryBudget(50, 0)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234567890"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 100)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	for _, path := range []string{"/v1/responses", "/v1/images/generations", "/images/generations"} {
		req := newAdmissionTestRequest("1234567890")
		req.URL.Path = path
		// 客户端声明为二进制不能绕过 JSON 路由的扩容预算：10 字节
		// 按七倍估算并封顶为完整 50 字节预算；holder 已占 20 字节，
		// 因此请求应在读取前拒绝。
		req.Header.Set("Content-Type", "application/octet-stream")
		lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
		require.ErrorIs(t, err, errBodyAdmissionUnavailable, path)
		require.Equal(t, BodyAdmissionRejectMemory, reason, path)
		require.Nil(t, lease, path)
	}
}

func TestBodyAdmissionKeepsBinaryReservationForMultipartRoutes(t *testing.T) {
	budget := NewBodyMemoryBudget(50, 0)
	req := newAdmissionTestRequest("1234567890")
	req.URL.Path = "/v1/images/edits"
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 100)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)
	lease.Release()
}

func TestBodyMemoryReservationAccountsForCompressedJSON(t *testing.T) {
	budget := NewBodyMemoryBudget(2048, 0)
	// 压缩体估算同时包含压缩原文、解压原文和最坏六倍转义副本。
	require.Equal(t, int64(710), budget.reservationBytes(10, 100, "gzip", "application/json"))
	require.Equal(t, int64(800), budget.reservationBytes(-1, 100, "gzip", "application/json"))
}

func TestBodyMemoryReservationCapsSingleRequestAtCapacity(t *testing.T) {
	budget := NewBodyMemoryBudget(50, 0)
	require.Equal(t, int64(50), budget.reservationBytes(10, 100, "", "application/json"))
	require.Equal(t, int64(50), budget.reservationBytes(-1, 100, "gzip", "application/json"))
}

func TestBodyAdmissionAnthropicRateLimitErrorUsesRateLimitType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	budget := NewBodyMemoryBudget(8, 0, 1)
	holder := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	holderLease, _, err := AcquireRequestBodyAdmission(holder, budget, 16)
	require.NoError(t, err)
	require.NotNil(t, holderLease)
	defer holderLease.Release()

	router := gin.New()
	router.POST("/v1/messages", RequestBodyAdmission(budget, 16), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	req := newAdmissionTestRequest("1234")
	req.URL.Path = "/v1/messages"
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"rate_limit_error"`)
}

type admissionCloseTrackingBody struct {
	*bytes.Reader
	closed atomic.Int32
}

func (b *admissionCloseTrackingBody) Close() error {
	b.closed.Add(1)
	return nil
}

func TestBodyAdmissionClosesBodyWhenRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &admissionCloseTrackingBody{Reader: bytes.NewReader([]byte("12345"))}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = body
	req.ContentLength = 5
	router := gin.New()
	router.POST("/v1/responses", RequestBodyAdmission(nil, 4), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Equal(t, int32(1), body.closed.Load())
}

func TestBodyAdmissionReleaseClosesUnreadBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &admissionCloseTrackingBody{Reader: bytes.NewReader([]byte("1234"))}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = body
	req.ContentLength = 4
	c := newAdmissionTestContext(req)

	budget := NewBodyMemoryBudget(32, 0, 1)
	lease, reason, err := AcquireRequestBodyAdmission(c, budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotNil(t, lease)

	lease.Release()
	require.Equal(t, int32(1), body.closed.Load())

	// 读取槽必须在未消费完 body 的租约释放后立即可复用。
	second := newAdmissionTestContext(newAdmissionTestRequest("1234"))
	secondLease, secondReason, secondErr := AcquireRequestBodyAdmission(second, budget, 16)
	require.NoError(t, secondErr)
	require.Empty(t, secondReason)
	require.NotNil(t, secondLease)
	secondLease.Release()
}

func TestBodyAdmissionOversizeAnthropicUsesInvalidRequestType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/messages", RequestBodyAdmission(nil, 4), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString("12345"))
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"invalid_request_error"`)
}

func TestAcquireRequestBodyAdmissionSkipsNoBody(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/models", nil),
		httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody),
	} {
		lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 16)
		require.NoError(t, err)
		require.Empty(t, reason)
		require.Nil(t, lease)
	}
}

func TestAcquireRequestBodyAdmissionWithoutBudgetStillInstallsHardLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = io.NopCloser(strings.NewReader("{}"))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	c := newAdmissionTestContext(req)

	lease, reason, err := AcquireRequestBodyAdmission(c, nil, 1)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Nil(t, lease)
	require.Equal(t, int64(1), pkghttputil.DecompressedBodyLimit(c.Request))
	_, readErr := io.ReadAll(c.Request.Body)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, readErr, &maxErr)
	require.Equal(t, int64(1), maxErr.Limit)
}

func TestAcquireRequestBodyAdmissionSkipsTypedNilBody(t *testing.T) {
	budget := NewBodyMemoryBudget(8, 0, 1)
	req := newAdmissionTestRequest("ignored")
	var body *budgetTypedNilBody
	req.Body = body

	lease, reason, err := AcquireRequestBodyAdmission(newAdmissionTestContext(req), budget, 16)
	require.NoError(t, err)
	require.Empty(t, reason)
	require.Nil(t, lease)
}

func TestNewBodyMemoryBudgetSaturatesHugeWaitSeconds(t *testing.T) {
	budget := NewBodyMemoryBudget(8, int(^uint(0)>>1), 1)
	require.NotNil(t, budget)
	require.Greater(t, budget.wait, time.Duration(0))
	require.LessOrEqual(t, budget.wait, time.Duration(math.MaxInt64))
}
