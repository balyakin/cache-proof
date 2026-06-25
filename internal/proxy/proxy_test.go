package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"cacheproof/internal/fault"
	"cacheproof/internal/recorder"
	"cacheproof/internal/resp"
	"cacheproof/internal/testutil"

	"github.com/stretchr/testify/require"
)

func TestProxyPassThroughPreservesReplyBytes(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		if cmd.Name == "PING" {
			return []byte("+PONG\r\n")
		}
		return []byte("$3\r\nbar\r\n")
	}
	p := startTestProxy(t, fake.Addr)
	conn := dialProxy(t, p.Listen)
	defer conn.Close()

	_, err := conn.Write(resp.EncodeCommand("GET", "foo"))
	require.NoError(t, err)
	reply, _, err := resp.ReadReplyRaw(bufio.NewReader(conn))
	require.NoError(t, err)
	require.Equal(t, []byte("$3\r\nbar\r\n"), reply)
}

func TestProxyPipeliningOrdersReplies(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		if cmd.Name == "PING" {
			return []byte("+PONG\r\n")
		}
		return []byte("+" + cmd.Args[1] + "\r\n")
	}
	p := startTestProxy(t, fake.Addr)
	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	_, err := conn.Write(append(resp.EncodeCommand("GET", "one"), resp.EncodeCommand("GET", "two")...))
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	first, _, err := resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	second, _, err := resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("+one\r\n"), first)
	require.Equal(t, []byte("+two\r\n"), second)
}

func TestProxyRandomMissReturnsNil(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	p := startTestProxy(t, fake.Addr)
	p.SetEngine(fault.RandomMiss{Probability: 1})
	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	_, err := conn.Write(resp.EncodeCommand("GET", "foo"))
	require.NoError(t, err)
	reply, _, err := resp.ReadReplyRaw(bufio.NewReader(conn))
	require.NoError(t, err)
	require.Equal(t, []byte("$-1\r\n"), reply)
}

func TestProxyRandomMissDoesNotForwardMutatingReads(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	p := startTestProxy(t, fake.Addr)
	p.SetEngine(fault.RandomMiss{Probability: 1})
	conn := dialProxy(t, p.Listen)
	defer conn.Close()

	_, err := conn.Write(resp.EncodeCommand("GETDEL", "foo"))
	require.NoError(t, err)
	reply, _, err := resp.ReadReplyRaw(bufio.NewReader(conn))
	require.NoError(t, err)
	require.Equal(t, []byte("$-1\r\n"), reply)
	require.NotContains(t, fake.Commands(), "GETDEL")
}

func TestProxyShutdownClosesBlockedUpstream(t *testing.T) {
	upstream := startBlockingRedis(t)
	p := New("127.0.0.1:0", upstream.addr, resp.Auth{}, recorder.New(1024), nil)
	require.NoError(t, p.Start(context.Background()))

	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	_, err := conn.Write(resp.EncodeCommand("GET", "foo"))
	require.NoError(t, err)

	select {
	case <-upstream.blocked:
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive blocking command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.NoError(t, p.Shutdown(ctx))
}

func TestProxyUnavailableClosesAndRefuses(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	p := startTestProxy(t, fake.Addr)
	conn := dialProxy(t, p.Listen)
	p.SetEngine(fault.Unavailable{})
	p.CloseAllConns()
	_, _ = conn.Write(resp.EncodeCommand("GET", "foo"))
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err := bufio.NewReader(conn).ReadByte()
	require.Error(t, err)
	_ = conn.Close()

	newConn := dialProxy(t, p.Listen)
	defer newConn.Close()
	_, _ = newConn.Write(resp.EncodeCommand("GET", "foo"))
	_ = newConn.SetReadDeadline(time.Now().Add(time.Second))
	_, err = bufio.NewReader(newConn).ReadByte()
	require.Error(t, err)
}

func TestProxyUnsupportedUnderFault(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	p := startTestProxy(t, fake.Addr)
	p.SetEngine(fault.RandomMiss{Probability: 1})
	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	_, err := conn.Write(resp.EncodeCommand("SUBSCRIBE", "events"))
	require.NoError(t, err)
	reply, _, err := resp.ReadReplyRaw(bufio.NewReader(conn))
	require.NoError(t, err)
	require.Equal(t, []byte("-ERR cacheproof unsupported command under fault injection: SUBSCRIBE\r\n"), reply)
}

func TestProxyRESP3SyntheticNilAndSetScenario(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	p := startTestProxy(t, fake.Addr)
	p.SetScenario("random")
	p.SetEngine(fault.RandomMiss{Probability: 1})
	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_, err := conn.Write(append(resp.EncodeCommand("HELLO", "3"), resp.EncodeCommand("GET", "foo")...))
	require.NoError(t, err)
	_, _, err = resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	reply, _, err := resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("_\r\n"), reply)
	require.Equal(t, 1, p.recorder.Snapshot().CommandByScenario["random"]["GET"])
}

func TestProxyKeepsRESP2WhenHELLO3Rejected(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	fake.Handler = func(cmd *resp.Command) []byte {
		if cmd.Name == "HELLO" {
			return []byte("-ERR unknown command 'HELLO'\r\n")
		}
		return []byte("$5\r\nvalue\r\n")
	}
	p := startTestProxy(t, fake.Addr)
	p.SetEngine(fault.RandomMiss{Probability: 1})
	conn := dialProxy(t, p.Listen)
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_, err := conn.Write(append(resp.EncodeCommand("HELLO", "3"), resp.EncodeCommand("GET", "foo")...))
	require.NoError(t, err)
	first, _, err := resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("-ERR unknown command 'HELLO'\r\n"), first)
	reply, _, err := resp.ReadReplyRaw(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("$-1\r\n"), reply)
}

func TestProxyReadCommandLogDoesNotLeakCommandBytes(t *testing.T) {
	fake := testutil.StartRedisFake(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := New("127.0.0.1:0", fake.Addr, resp.Auth{}, recorder.New(1024), logger)
	require.NoError(t, p.Start(context.Background()))

	conn := dialProxy(t, p.Listen)
	const secret = "SECRET_VALUE_SHOULD_NOT_LEAK"
	_, err := conn.Write([]byte("*2\r\n$3\r\nGET\r\n$100\r\n" + secret))
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(ctx))
	require.NotContains(t, logs.String(), secret)
}

func TestProxyStartInvalidAddress(t *testing.T) {
	p := New("bad-address", "127.0.0.1:1", resp.Auth{}, recorder.New(1), nil)
	require.Error(t, p.Start(context.Background()))
}

func startTestProxy(t *testing.T, upstream string) *Proxy {
	t.Helper()
	p := New("127.0.0.1:0", upstream, resp.Auth{}, recorder.New(1024), nil)
	require.NoError(t, p.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, p.Shutdown(ctx))
	})
	return p
}

func dialProxy(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	return conn
}

type blockingRedis struct {
	addr      string
	listener  net.Listener
	blocked   chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	blockOnce sync.Once
	wg        sync.WaitGroup
	mu        sync.Mutex
	conns     []net.Conn
}

func startBlockingRedis(t *testing.T) *blockingRedis {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &blockingRedis{
		addr:     listener.Addr().String(),
		listener: listener,
		blocked:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	server.wg.Add(1)
	go server.acceptLoop()
	t.Cleanup(server.Close)
	return server
}

func (s *blockingRedis) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *blockingRedis) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		cmd, err := resp.ReadCommand(reader)
		if err != nil {
			return
		}
		if cmd.Name == "PING" {
			_, _ = conn.Write([]byte("+PONG\r\n"))
			continue
		}
		s.blockOnce.Do(func() {
			close(s.blocked)
		})
		<-s.done
		return
	}
}

func (s *blockingRedis) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.listener.Close()
		s.mu.Lock()
		for _, conn := range s.conns {
			_ = conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}

func TestUseClosedNetworkConnDetection(t *testing.T) {
	require.True(t, isUseClosedNetworkConn(errors.New("use of closed network connection")))
}
