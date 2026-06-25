package resp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxReplyDepth = 128

func ReadReplyRaw(reader *bufio.Reader) ([]byte, byte, error) {
	var raw bytes.Buffer
	kind, err := readReplyRawDepth(reader, &raw, 0)
	if err != nil {
		return nil, 0, err
	}
	return raw.Bytes(), kind, nil
}

func readReplyRawDepth(reader *bufio.Reader, raw *bytes.Buffer, depth int) (byte, error) {
	if depth > maxReplyDepth {
		return 0, fmt.Errorf("RESP reply exceeds max depth %d", maxReplyDepth)
	}
	kind, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	raw.WriteByte(kind)
	line, err := readRESPLine(reader)
	if err != nil {
		return 0, fmt.Errorf("read reply line: %w", err)
	}
	raw.WriteString(line)

	switch kind {
	case '+', '-', ':', '_', ',', '#', '(':
		return kind, nil
	case '$', '=', '!':
		length, err := parseReplyInt(line)
		if err != nil {
			return 0, fmt.Errorf("parse bulk reply length: %w", err)
		}
		if length < 0 {
			return kind, nil
		}
		if err := validateBulkLength(length); err != nil {
			return 0, err
		}
		payload := make([]byte, length+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return 0, fmt.Errorf("read bulk reply payload: %w", err)
		}
		raw.Write(payload)
		return kind, nil
	case '*', '~', '>':
		count, err := parseReplyInt(line)
		if err != nil {
			return 0, fmt.Errorf("parse aggregate length: %w", err)
		}
		if count < 0 {
			return kind, nil
		}
		if err := validateAggregateCount("aggregate", count); err != nil {
			return 0, err
		}
		for index := 0; index < count; index++ {
			if _, err := readReplyRawDepth(reader, raw, depth+1); err != nil {
				return 0, err
			}
		}
		return kind, nil
	case '%':
		count, err := parseReplyInt(line)
		if err != nil {
			return 0, fmt.Errorf("parse map length: %w", err)
		}
		if count < 0 {
			return kind, nil
		}
		if err := validateAggregatePairCount("map", count); err != nil {
			return 0, err
		}
		for index := 0; index < count*2; index++ {
			if _, err := readReplyRawDepth(reader, raw, depth+1); err != nil {
				return 0, err
			}
		}
		return kind, nil
	case '|':
		count, err := parseReplyInt(line)
		if err != nil {
			return 0, fmt.Errorf("parse attribute length: %w", err)
		}
		if count < 0 {
			return kind, nil
		}
		if err := validateAggregatePairCount("attribute", count); err != nil {
			return 0, err
		}
		for index := 0; index < count*2; index++ {
			if _, err := readReplyRawDepth(reader, raw, depth+1); err != nil {
				return 0, err
			}
		}
		return readReplyRawDepth(reader, raw, depth+1)
	default:
		return 0, fmt.Errorf("unsupported RESP reply type %q", kind)
	}
}

func parseReplyInt(line string) (int, error) {
	return strconv.Atoi(strings.TrimRight(line, "\r\n"))
}

func NilReply(resp3 bool) []byte {
	if resp3 {
		return []byte("_\r\n")
	}
	return []byte("$-1\r\n")
}

func ErrorReply(message string) []byte {
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	return []byte("-ERR " + message + "\r\n")
}
