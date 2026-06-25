package testutil

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"

	"cacheproof/internal/resp"
)

type RedisFake struct {
	Addr string

	listener net.Listener
	wg       sync.WaitGroup
	mu       sync.Mutex
	commands []string
	Handler  func(*resp.Command) []byte
}

func StartRedisFake(t *testing.T) *RedisFake {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake redis: %v", err)
	}
	fake := &RedisFake{Addr: listener.Addr().String(), listener: listener}
	fake.wg.Add(1)
	go fake.acceptLoop()
	t.Cleanup(fake.Close)
	return fake
}

func (f *RedisFake) Close() {
	_ = f.listener.Close()
	f.wg.Wait()
}

func (f *RedisFake) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func (f *RedisFake) acceptLoop() {
	defer f.wg.Done()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go f.handleConn(conn)
	}
}

func (f *RedisFake) handleConn(conn net.Conn) {
	defer f.wg.Done()
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		cmd, err := resp.ReadCommand(reader)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.commands = append(f.commands, cmd.Name)
		f.mu.Unlock()
		reply := f.defaultReply(cmd)
		if f.Handler != nil {
			reply = f.Handler(cmd)
		}
		if _, err := conn.Write(reply); err != nil {
			return
		}
	}
}

func (f *RedisFake) defaultReply(cmd *resp.Command) []byte {
	switch cmd.Name {
	case "PING":
		return []byte("+PONG\r\n")
	case "AUTH":
		return []byte("+OK\r\n")
	case "GET", "HGET":
		return []byte("$5\r\nvalue\r\n")
	case "SET", "SETEX", "PSETEX", "HSET", "APPEND":
		return []byte("+OK\r\n")
	case "SCAN":
		return []byte("*2\r\n$1\r\n0\r\n*0\r\n")
	case "TTL":
		return []byte(":-2\r\n")
	case "UNLINK", "DEL":
		return []byte(":1\r\n")
	default:
		if strings.HasPrefix(cmd.Name, "X") {
			return []byte("+OK\r\n")
		}
		return []byte("+OK\r\n")
	}
}
