package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"cacheproof/internal/appx"
	"cacheproof/internal/fault"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"
)

type Proxy struct {
	Listen   string
	Upstream string
	Auth     resp.Auth

	recorder *recorder.Recorder
	logger   *slog.Logger

	mu       sync.RWMutex
	engine   fault.Engine
	scenario string
	listener net.Listener
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
	cancel   context.CancelFunc
}

func New(listen string, upstream string, auth resp.Auth, rec *recorder.Recorder, logger *slog.Logger) *Proxy {
	if logger == nil {
		logger = slog.Default()
	}
	return &Proxy{
		Listen:   listen,
		Upstream: upstream,
		Auth:     auth,
		recorder: rec,
		logger:   logger,
		engine:   fault.PassThrough{},
		scenario: "baseline",
		conns:    make(map[net.Conn]struct{}),
	}
}

func (p *Proxy) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: proxy start context: %v", appx.ErrInfrastructure, err)
	}
	listener, err := net.Listen("tcp", p.Listen)
	if err != nil {
		return fmt.Errorf("%w: listen proxy %s: %v", appx.ErrInfrastructure, p.Listen, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.listener = listener
	p.Listen = listener.Addr().String()
	p.cancel = cancel
	p.mu.Unlock()

	p.wg.Add(1)
	go p.acceptLoop(runCtx, listener)
	return nil
}

func (p *Proxy) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	listener := p.listener
	cancel := p.cancel
	p.listener = nil
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("close proxy listener: %w", err)
		}
	}
	p.CloseAllConns()
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.wg.Wait()
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("shutdown proxy: %w", ctx.Err())
	case <-done:
		return nil
	}
}

func (p *Proxy) SetEngine(engine fault.Engine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if engine == nil {
		engine = fault.PassThrough{}
	}
	p.engine = engine
}

func (p *Proxy) SetScenario(name string) {
	p.mu.Lock()
	p.scenario = name
	p.mu.Unlock()
	if p.recorder != nil {
		p.recorder.SetScenario(name)
	}
}

func (p *Proxy) CloseAllConns() {
	p.mu.RLock()
	conns := make([]net.Conn, 0, len(p.conns))
	for conn := range p.conns {
		conns = append(conns, conn)
	}
	p.mu.RUnlock()
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			p.logger.Debug("close app connection", "error", err)
		}
	}
}

func (p *Proxy) currentEngine() fault.Engine {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.engine
}

func (p *Proxy) registerConn(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns[conn] = struct{}{}
}

func (p *Proxy) unregisterConn(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.conns, conn)
}

func (p *Proxy) acceptLoop(ctx context.Context, listener net.Listener) {
	defer p.wg.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			p.logger.Error("proxy accept panic", "error", recovered)
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			p.logger.Error("accept proxy connection", "error", err)
			continue
		}
		p.wg.Add(1)
		go p.handleConn(ctx, conn)
	}
}
