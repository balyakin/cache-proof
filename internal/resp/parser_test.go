package resp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadCommandRESPArray(t *testing.T) {
	raw := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	cmd, err := ReadCommand(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	require.Equal(t, "SET", cmd.Name)
	require.Equal(t, []string{"SET", "foo", "bar"}, cmd.Args)
	require.Equal(t, []byte(raw), cmd.Raw)
	key, ok := cmd.Key()
	require.True(t, ok)
	require.Equal(t, "foo", key)
}

func TestReadCommandInline(t *testing.T) {
	cmd, err := ReadCommand(bufio.NewReader(strings.NewReader("PING\r\n")))
	require.NoError(t, err)
	require.Equal(t, "PING", cmd.Name)
	require.Equal(t, []byte("PING\r\n"), cmd.Raw)
}

func TestReadCommandRejectsMalformed(t *testing.T) {
	tests := []string{
		"*0\r\n",
		"*x\r\n",
		"*1\r\n+OK\r\n",
		"*1\r\n$x\r\n",
		"*1\r\n$-1\r\n",
		"*1\r\n$3\r\nab\r\n",
	}
	for _, raw := range tests {
		_, err := ReadCommand(bufio.NewReader(strings.NewReader(raw)))
		require.Error(t, err, raw)
	}
}

func TestReadCommandRejectsOversizedRESP(t *testing.T) {
	_, err := ReadCommand(bufio.NewReader(strings.NewReader(strings.Repeat("A", 1024*1024+1))))
	require.ErrorContains(t, err, "exceeds")

	var bulkErr error
	require.NotPanics(t, func() {
		_, bulkErr = ReadCommand(bufio.NewReader(strings.NewReader("*1\r\n$9223372036854775807\r\n")))
	})
	require.Error(t, bulkErr)

	_, err = ReadCommand(bufio.NewReader(strings.NewReader("*1048577\r\n")))
	require.ErrorContains(t, err, "exceeds")
}

func TestReadReplyRawPreservesNestedArrays(t *testing.T) {
	raw := "*2\r\n$3\r\nfoo\r\n*2\r\n:1\r\n$3\r\nbar\r\n"
	got, kind, err := ReadReplyRaw(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	require.Equal(t, byte('*'), kind)
	require.Equal(t, []byte(raw), got)
}

func TestReadReplyRawDepthLimit(t *testing.T) {
	var b bytes.Buffer
	for i := 0; i < 130; i++ {
		b.WriteString("*1\r\n")
	}
	b.WriteString("+OK\r\n")
	_, _, err := ReadReplyRaw(bufio.NewReader(bytes.NewReader(b.Bytes())))
	require.Error(t, err)
}

func TestReadReplyRawRejectsOversizedRESP(t *testing.T) {
	_, _, err := ReadReplyRaw(bufio.NewReader(strings.NewReader("+" + strings.Repeat("A", 1024*1024+1) + "\r\n")))
	require.ErrorContains(t, err, "exceeds")

	var bulkErr error
	require.NotPanics(t, func() {
		_, _, bulkErr = ReadReplyRaw(bufio.NewReader(strings.NewReader("$9223372036854775807\r\n")))
	})
	require.Error(t, bulkErr)

	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("*1048577\r\n")))
	require.ErrorContains(t, err, "exceeds")

	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("%524289\r\n")))
	require.ErrorContains(t, err, "exceeds")
}

func TestClientDecodeScanAndRedisError(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("*2\r\n$1\r\n0\r\n*1\r\n$3\r\nfoo\r\n"))
	value, err := readValue(reader, 0)
	require.NoError(t, err)
	require.Equal(t, []interface{}{"0", []interface{}{"foo"}}, value)

	_, err = readValue(bufio.NewReader(strings.NewReader("-ERR unknown command 'UNLINK'\r\n")), 0)
	require.Error(t, err)
	require.True(t, IsUnknownCommand(err))
	require.True(t, IsUnknownCommand(errors.New("ERR unknown command 'UNLINK'")))
}

func TestClientDialDoAndRaw(t *testing.T) {
	addr, closeServer := startRESPServer(t)
	defer closeServer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := DialContext(ctx, addr, Auth{Password: "secret"})
	require.NoError(t, err)
	defer client.Close()
	conn, reader := client.Raw()
	require.NotNil(t, conn)
	require.NotNil(t, reader)
	value, err := client.Do(ctx, "GET", "product:42")
	require.NoError(t, err)
	require.Equal(t, "cached", value)
	_, err = client.Do(ctx, "FAIL")
	require.Error(t, err)
	require.False(t, IsUnknownCommand(err))
}

func TestClientDoClearsDeadline(t *testing.T) {
	conn := &deadlineRecordingConn{}
	client := &Client{conn: conn, reader: bufio.NewReader(strings.NewReader("+OK\r\n"))}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := client.Do(ctx, "PING")
	require.NoError(t, err)
	require.Equal(t, "OK", value)
	require.Len(t, conn.deadlines, 2)
	require.False(t, conn.deadlines[0].IsZero())
	require.True(t, conn.deadlines[1].IsZero())
}

func TestClientErrorBranches(t *testing.T) {
	addr, closeServer := startRESPServer(t)
	defer closeServer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := DialContext(ctx, addr, Auth{Username: "user", Password: "secret"})
	require.NoError(t, err)
	defer client.Close()
	_, err = client.Do(ctx)
	require.Error(t, err)

	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, err = DialContext(canceled, addr, Auth{})
	require.Error(t, err)
	_, err = DialContext(ctx, "127.0.0.1:1", Auth{})
	require.Error(t, err)

	authAddr, closeAuthServer := startStaticRESPServer(t, []byte("-ERR bad auth\r\n"))
	defer closeAuthServer()
	_, err = DialContext(ctx, authAddr, Auth{Password: "bad"})
	require.Error(t, err)

	pingAddr, closePingServer := startStaticRESPServer(t, []byte("-ERR ping failed\r\n"))
	defer closePingServer()
	_, err = DialContext(ctx, pingAddr, Auth{})
	require.Error(t, err)

	_, err = (&Client{}).Do(canceled, "PING")
	require.Error(t, err)
	_, err = (&Client{conn: fakeConn{deadlineErr: errors.New("deadline")}, reader: bufio.NewReader(strings.NewReader(""))}).Do(ctx, "PING")
	require.Error(t, err)
	_, err = (&Client{conn: fakeConn{writeErr: errors.New("write")}, reader: bufio.NewReader(strings.NewReader(""))}).Do(ctx, "PING")
	require.Error(t, err)
	_, err = (&Client{conn: fakeConn{}, reader: bufio.NewReader(strings.NewReader(""))}).Do(ctx, "PING")
	require.Error(t, err)
}

func TestEncodeNilAndErrorReplies(t *testing.T) {
	require.Equal(t, []byte("*2\r\n$3\r\nGET\r\n$1\r\nk\r\n"), EncodeCommand("GET", "k"))
	require.Equal(t, []byte("$-1\r\n"), NilReply(false))
	require.Equal(t, []byte("_\r\n"), NilReply(true))
	require.Equal(t, []byte("-ERR bad  message\r\n"), ErrorReply("bad\r\nmessage"))
}

func TestReadReplyRawSupportedTypes(t *testing.T) {
	replies := []string{
		"+OK\r\n",
		"-ERR bad\r\n",
		":1\r\n",
		"_\r\n",
		",1.5\r\n",
		"#t\r\n",
		"(123\r\n",
		"$3\r\nfoo\r\n",
		"=7\r\ntxt:foo\r\n",
		"!3\r\nbad\r\n",
		"~1\r\n+OK\r\n",
		">1\r\n+OK\r\n",
		"%1\r\n$1\r\na\r\n$1\r\nb\r\n",
		"%-1\r\n",
		"|1\r\n$1\r\na\r\n$1\r\nb\r\n+OK\r\n",
		"|-1\r\n",
	}
	for _, raw := range replies {
		got, _, err := ReadReplyRaw(bufio.NewReader(strings.NewReader(raw)))
		require.NoError(t, err, raw)
		require.Equal(t, []byte(raw), got)
	}
}

func TestReadValueAndCommandEdgeCases(t *testing.T) {
	require.Equal(t, []byte("*0\r\n"), EncodeCommand())
	values := map[string]interface{}{
		"+OK\r\n":                  "OK",
		":42\r\n":                  int64(42),
		"$-1\r\n":                  nil,
		"_\r\n":                    nil,
		"*2\r\n+OK\r\n:1\r\n":      []interface{}{"OK", int64(1)},
		"%1\r\n+key\r\n+value\r\n": map[interface{}]interface{}{"key": "value"},
		"%-1\r\n":                  nil,
	}
	for raw, want := range values {
		got, err := readValue(bufio.NewReader(strings.NewReader(raw)), 0)
		require.NoError(t, err, raw)
		require.Equal(t, want, got)
	}
	_, err := readValue(bufio.NewReader(strings.NewReader("?bad\r\n")), 0)
	require.Error(t, err)
	_, err = readValue(bufio.NewReader(strings.NewReader(":x\r\n")), 0)
	require.Error(t, err)
	_, err = readValue(bufio.NewReader(strings.NewReader("$x\r\n")), 0)
	require.Error(t, err)
	_, err = readValue(bufio.NewReader(strings.NewReader("*x\r\n")), 0)
	require.Error(t, err)
	_, err = readValue(bufio.NewReader(strings.NewReader("%x\r\n")), 0)
	require.Error(t, err)
	var deep strings.Builder
	for i := 0; i < 130; i++ {
		deep.WriteString("*1\r\n")
	}
	deep.WriteString("+OK\r\n")
	_, err = readValue(bufio.NewReader(strings.NewReader(deep.String())), 0)
	require.Error(t, err)
	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("?bad\r\n")))
	require.Error(t, err)
	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("$x\r\n")))
	require.Error(t, err)
	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("*x\r\n")))
	require.Error(t, err)
	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("%x\r\n")))
	require.Error(t, err)
	_, _, err = ReadReplyRaw(bufio.NewReader(strings.NewReader("|x\r\n")))
	require.Error(t, err)
	_, ok := NewCommand([]string{"GET"}, nil).Key()
	require.False(t, ok)
}

func TestReadValueRejectsOversizedRESPAndBadMapKeys(t *testing.T) {
	_, err := readValue(bufio.NewReader(strings.NewReader("+"+strings.Repeat("A", 1024*1024+1)+"\r\n")), 0)
	require.ErrorContains(t, err, "exceeds")

	var bulkErr error
	require.NotPanics(t, func() {
		_, bulkErr = readValue(bufio.NewReader(strings.NewReader("$9223372036854775807\r\n")), 0)
	})
	require.Error(t, bulkErr)

	var mapErr error
	require.NotPanics(t, func() {
		_, mapErr = readValue(bufio.NewReader(strings.NewReader("%1\r\n*1\r\n+key\r\n+value\r\n")), 0)
	})
	require.ErrorContains(t, mapErr, "not comparable")
}

type fakeConn struct {
	deadlineErr error
	writeErr    error
}

func (c fakeConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c fakeConn) Write([]byte) (int, error)        { return 0, c.writeErr }
func (c fakeConn) Close() error                     { return nil }
func (c fakeConn) LocalAddr() net.Addr              { return nil }
func (c fakeConn) RemoteAddr() net.Addr             { return nil }
func (c fakeConn) SetDeadline(time.Time) error      { return c.deadlineErr }
func (c fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c fakeConn) SetWriteDeadline(time.Time) error { return nil }

type deadlineRecordingConn struct {
	deadlines []time.Time
	writes    bytes.Buffer
}

func (c *deadlineRecordingConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *deadlineRecordingConn) Write(p []byte) (int, error) {
	return c.writes.Write(p)
}
func (c *deadlineRecordingConn) Close() error                     { return nil }
func (c *deadlineRecordingConn) LocalAddr() net.Addr              { return nil }
func (c *deadlineRecordingConn) RemoteAddr() net.Addr             { return nil }
func (c *deadlineRecordingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *deadlineRecordingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	c.deadlines = append(c.deadlines, deadline)
	return nil
}

func startRESPServer(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					cmd, err := ReadCommand(reader)
					if err != nil {
						return
					}
					switch cmd.Name {
					case "AUTH":
						_, _ = conn.Write([]byte("+OK\r\n"))
					case "PING":
						_, _ = conn.Write([]byte("+PONG\r\n"))
					case "GET":
						_, _ = conn.Write([]byte("$6\r\ncached\r\n"))
					case "FAIL":
						_, _ = conn.Write([]byte("-ERR failed\r\n"))
					default:
						_, _ = conn.Write([]byte("+OK\r\n"))
					}
				}
			}(conn)
		}
	}()
	closeFn := func() {
		close(done)
		_ = listener.Close()
		wg.Wait()
	}
	return listener.Addr().String(), closeFn
}

func startStaticRESPServer(t *testing.T, reply []byte) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_, _ = ReadCommand(reader)
		_, _ = conn.Write(reply)
	}()
	return listener.Addr().String(), func() {
		_ = listener.Close()
		wg.Wait()
	}
}
