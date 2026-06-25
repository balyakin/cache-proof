package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"cacheproof/internal/fault"
	"cacheproof/internal/resp"
)

func (p *Proxy) handleConn(ctx context.Context, appConn net.Conn) {
	defer p.wg.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			p.logger.Error("proxy connection panic", "error", recovered)
		}
	}()
	p.registerConn(appConn)
	defer p.unregisterConn(appConn)
	defer func() {
		if err := appConn.Close(); err != nil {
			p.logger.Debug("close app connection", "error", err)
		}
	}()

	if p.currentEngine().RefuseConnections() {
		return
	}
	client, err := resp.DialContext(ctx, p.Upstream, p.Auth)
	if err != nil {
		p.logger.Error("dial upstream", "error", err)
		return
	}
	defer func() {
		if err := client.Close(); err != nil {
			p.logger.Debug("close upstream connection", "error", err)
		}
	}()
	upstreamConn, upstreamReader := client.Raw()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = appConn.Close()
			_ = upstreamConn.Close()
		case <-stopWatch:
		}
	}()

	appReader := bufio.NewReader(appConn)
	resp3 := false
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		cmd, err := resp.ReadCommand(appReader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !isUseClosedNetworkConn(err) {
				p.logger.Debug("read app redis command", "error", "parse failed")
			}
			return
		}
		if p.recorder != nil {
			p.recorder.Observe(cmd)
		}

		engine := p.currentEngine()
		if engine.RefuseConnections() {
			return
		}
		if engine.Name() != "pass-through" && unsupportedUnderFault[cmd.Name] {
			reply := resp.ErrorReply("cacheproof unsupported command under fault injection: " + cmd.Name)
			if _, err := appConn.Write(reply); err != nil {
				p.logger.Debug("write unsupported command error", "cmd", cmd.Name, "error", err)
			}
			continue
		}
		action := engine.Decide(cmd)
		switch action.Kind {
		case fault.ActionForward:
			replyKind, err := forwardCommand(appConn, upstreamConn, upstreamReader, cmd.Raw)
			if err != nil {
				p.logger.Debug("forward redis command", "cmd", cmd.Name, "error", err)
				return
			}
			if protocol, ok := helloProtocol(cmd); ok && replyKind != '-' {
				resp3 = protocol == "3"
			}
		case fault.ActionReplaceWithMiss:
			if _, err := appConn.Write(resp.NilReply(resp3)); err != nil {
				p.logger.Debug("write synthetic nil reply", "cmd", cmd.Name, "error", err)
				return
			}
		case fault.ActionDropConnection:
			return
		default:
			p.logger.Error("unknown fault action", "cmd", cmd.Name, "error", fmt.Sprintf("%d", action.Kind))
			return
		}
	}
}

func forwardCommand(appConn net.Conn, upstreamConn net.Conn, upstreamReader *bufio.Reader, raw []byte) (byte, error) {
	if _, err := upstreamConn.Write(raw); err != nil {
		return 0, fmt.Errorf("write upstream: %w", err)
	}
	reply, kind, err := resp.ReadReplyRaw(upstreamReader)
	if err != nil {
		return 0, fmt.Errorf("read upstream reply: %w", err)
	}
	if _, err := appConn.Write(reply); err != nil {
		return 0, fmt.Errorf("write app reply: %w", err)
	}
	return kind, nil
}

func helloProtocol(cmd *resp.Command) (string, bool) {
	if cmd.Name != "HELLO" || len(cmd.Args) < 2 {
		return "", false
	}
	switch cmd.Args[1] {
	case "2", "3":
		return cmd.Args[1], true
	default:
		return "", false
	}
}

func isUseClosedNetworkConn(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}
