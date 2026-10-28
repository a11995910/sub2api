package service

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type accountProtectionWSKey struct{}

// 单一读循环负责控制帧与断线检测；等待额度时仍可应答 ping。
type accountProtectionWSClient struct {
	conn      *websocket.Conn
	startOnce sync.Once
	started   atomic.Bool
	pumpCtx   context.Context
	frames    chan openAIWSClientReadResult
	done      chan struct{}
	mu        sync.Mutex
	cancel    context.CancelFunc
	waiting   bool
	closed    bool
}

func WithAccountProtectionWSClient(ctx context.Context, conn *websocket.Conn) (context.Context, func()) {
	p := &accountProtectionWSClient{conn: conn, frames: make(chan openAIWSClientReadResult, 1), done: make(chan struct{})}
	pumpCtx, stop := context.WithCancel(ctx)
	p.pumpCtx = pumpCtx
	return context.WithValue(ctx, accountProtectionWSKey{}, p), func() {
		stop()
		_ = conn.CloseNow()
		p.startOnce.Do(func() { close(p.done) })
		<-p.done
	}
}

// 只有实际启用账号限速后才启动，避免改变未受限连接的读取方式。
func (p *accountProtectionWSClient) start() {
	p.startOnce.Do(func() {
		p.started.Store(true)
		go func() {
			defer close(p.done)
			defer func() {
				p.mu.Lock()
				p.closed = true
				if p.cancel != nil {
					p.cancel()
				}
				p.mu.Unlock()
			}()
			for {
				typ, payload, err := p.conn.Read(context.Background())
				p.mu.Lock()
				waiting := p.waiting
				var event struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(payload, &event)
				cancelTurn := waiting && event.Type == "response.cancel"
				if (err != nil || cancelTurn) && p.cancel != nil {
					p.cancel()
				}
				p.mu.Unlock()
				if cancelTurn {
					continue
				}
				select {
				case p.frames <- openAIWSClientReadResult{messageType: typ, payload: payload, err: err}:
				case <-p.pumpCtx.Done():
					return
				default:
					// 不无限缓存客户端流水请求；过载关闭也会取消原账号排队。
					p.mu.Lock()
					p.closed = true
					if p.cancel != nil {
						p.cancel()
					}
					p.mu.Unlock()
					_ = p.conn.Close(websocket.StatusPolicyViolation, "客户端待处理消息过多")
					return
				}
				if err != nil {
					return
				}
			}
		}()
	})
}

func accountProtectionClientWaitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	p, _ := ctx.Value(accountProtectionWSKey{}).(*accountProtectionWSClient)
	if p == nil {
		return ctx, func() {}
	}
	waitCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel, p.waiting = cancel, true
	if p.closed {
		cancel()
	}
	p.mu.Unlock()
	p.start()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-waitCtx.Done():
				return
			case <-ticker.C:
				// 正常结束排队不能取消正在写出的控制帧，避免误关健康连接。
				pingCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := p.conn.Ping(pingCtx)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return waitCtx, func() {
		cancel()
		<-done
		p.mu.Lock()
		p.cancel, p.waiting = nil, false
		p.mu.Unlock()
	}
}
