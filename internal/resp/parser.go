package resp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	maxRESPLineBytes     = 1024 * 1024
	maxBulkBytes         = 64 * 1024 * 1024
	maxAggregateElements = 1 << 20
)

func ReadCommand(reader *bufio.Reader) (*Command, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if first != '*' {
		if err := reader.UnreadByte(); err != nil {
			return nil, fmt.Errorf("unread inline command byte: %w", err)
		}
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, fmt.Errorf("read inline command: %w", err)
		}
		args := strings.Fields(strings.TrimRight(line, "\r\n"))
		if len(args) == 0 {
			return nil, fmt.Errorf("empty inline command")
		}
		return NewCommand(args, []byte(line)), nil
	}

	var raw bytes.Buffer
	raw.WriteByte(first)
	line, err := readRESPLine(reader)
	if err != nil {
		return nil, fmt.Errorf("read array length: %w", err)
	}
	raw.WriteString(line)
	count, err := parseLineInt(line)
	if err != nil {
		return nil, fmt.Errorf("parse array length: %w", err)
	}
	if count <= 0 {
		return nil, fmt.Errorf("command array length must be positive")
	}
	if err := validateAggregateCount("command array", count); err != nil {
		return nil, err
	}

	args := make([]string, 0, count)
	for index := 0; index < count; index++ {
		prefix, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read bulk prefix: %w", err)
		}
		raw.WriteByte(prefix)
		if prefix != '$' {
			return nil, fmt.Errorf("command array argument must be bulk string")
		}
		lengthLine, err := readRESPLine(reader)
		if err != nil {
			return nil, fmt.Errorf("read bulk length: %w", err)
		}
		raw.WriteString(lengthLine)
		length, err := parseLineInt(lengthLine)
		if err != nil {
			return nil, fmt.Errorf("parse bulk length: %w", err)
		}
		if err := validateBulkLength(length); err != nil {
			return nil, err
		}
		payload := make([]byte, length+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, fmt.Errorf("read bulk payload: %w", err)
		}
		if payload[length] != '\r' || payload[length+1] != '\n' {
			return nil, fmt.Errorf("bulk payload missing CRLF")
		}
		raw.Write(payload)
		args = append(args, string(payload[:length]))
	}
	return NewCommand(args, raw.Bytes()), nil
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > maxRESPLineBytes {
			return "", fmt.Errorf("RESP line exceeds %d bytes", maxRESPLineBytes)
		}
		if err == nil {
			return string(line), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return "", err
		}
	}
}

func validateBulkLength(length int) error {
	if length < 0 {
		return fmt.Errorf("bulk length must be non-negative")
	}
	if length > maxBulkBytes {
		return fmt.Errorf("bulk length %d exceeds %d", length, maxBulkBytes)
	}
	return nil
}

func validateAggregateCount(name string, count int) error {
	if count < 0 {
		return nil
	}
	if count > maxAggregateElements {
		return fmt.Errorf("%s length %d exceeds %d", name, count, maxAggregateElements)
	}
	return nil
}

func validateAggregatePairCount(name string, count int) error {
	if count < 0 {
		return nil
	}
	if count > maxAggregateElements/2 {
		return fmt.Errorf("%s length %d exceeds %d", name, count, maxAggregateElements/2)
	}
	return nil
}

func parseLineInt(line string) (int, error) {
	line = strings.TrimRight(line, "\r\n")
	value, err := strconv.Atoi(line)
	if err != nil {
		return 0, err
	}
	return value, nil
}
