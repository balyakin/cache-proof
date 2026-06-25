package resp

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzReadCommand(f *testing.F) {
	f.Add([]byte("*1\r\n$4\r\nPING\r\n"))
	f.Add([]byte("PING\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadCommand(bufio.NewReader(bytes.NewReader(data)))
	})
}
