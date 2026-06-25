package resp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Auth struct {
	Username string
	Password string
}

type RedisError string

func (e RedisError) Error() string {
	return string(e)
}

type Client struct {
	conn   net.Conn
	reader *bufio.Reader
}

func DialContext(ctx context.Context, addr string, auth Auth) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial redis %s: %w", addr, err)
	}
	client := &Client{conn: conn, reader: bufio.NewReader(conn)}
	if auth.Password != "" {
		var authErr error
		if auth.Username == "" {
			_, authErr = client.Do(ctx, "AUTH", auth.Password)
		} else {
			_, authErr = client.Do(ctx, "AUTH", auth.Username, auth.Password)
		}
		if authErr != nil {
			_ = client.Close()
			return nil, fmt.Errorf("auth redis: %w", authErr)
		}
	}
	if _, err := client.Do(ctx, "PING"); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) Raw() (net.Conn, *bufio.Reader) {
	return c.conn, c.reader
}

func (c *Client) Do(ctx context.Context, args ...string) (value interface{}, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("redis command args must not be empty")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("set redis deadline: %w", err)
		}
	} else {
		if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return nil, fmt.Errorf("set redis default deadline: %w", err)
		}
	}
	defer func() {
		if clearErr := c.conn.SetDeadline(time.Time{}); clearErr != nil && err == nil {
			err = fmt.Errorf("clear redis deadline: %w", clearErr)
		}
	}()
	if _, err := c.conn.Write(EncodeCommand(args...)); err != nil {
		return nil, fmt.Errorf("write redis command %s: %w", strings.ToUpper(args[0]), err)
	}
	value, err = readValue(c.reader, 0)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read redis reply %s: %w", strings.ToUpper(args[0]), err)
	}
	return value, nil
}

func EncodeCommand(args ...string) []byte {
	var b strings.Builder
	b.WriteByte('*')
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteString("\r\n")
	for _, arg := range args {
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteString("\r\n")
		b.WriteString(arg)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func readValue(reader *bufio.Reader, depth int) (interface{}, error) {
	if depth > maxReplyDepth {
		return nil, fmt.Errorf("RESP reply exceeds max depth %d", maxReplyDepth)
	}
	kind, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readRESPLine(reader)
	if err != nil {
		return nil, fmt.Errorf("read reply line: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	switch kind {
	case '+':
		return line, nil
	case '-':
		return nil, RedisError(line)
	case ':':
		value, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse integer reply: %w", err)
		}
		return value, nil
	case '_':
		return nil, nil
	case '$', '=':
		length, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("parse bulk length: %w", err)
		}
		if length < 0 {
			return nil, nil
		}
		if err := validateBulkLength(length); err != nil {
			return nil, err
		}
		payload := make([]byte, length+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, fmt.Errorf("read bulk payload: %w", err)
		}
		return string(payload[:length]), nil
	case '*', '~', '>':
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("parse array length: %w", err)
		}
		if count < 0 {
			return nil, nil
		}
		if err := validateAggregateCount("array", count); err != nil {
			return nil, err
		}
		values := make([]interface{}, 0, count)
		for index := 0; index < count; index++ {
			value, err := readValue(reader, depth+1)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case '%':
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("parse map length: %w", err)
		}
		if count < 0 {
			return nil, nil
		}
		if err := validateAggregatePairCount("map", count); err != nil {
			return nil, err
		}
		values := make(map[interface{}]interface{}, count)
		for index := 0; index < count; index++ {
			key, err := readValue(reader, depth+1)
			if err != nil {
				return nil, err
			}
			if err := validateMapKey(key); err != nil {
				return nil, err
			}
			value, err := readValue(reader, depth+1)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported RESP reply type %q", kind)
	}
}

func validateMapKey(key interface{}) error {
	keyType := reflect.TypeOf(key)
	if keyType == nil || !keyType.Comparable() {
		return fmt.Errorf("RESP map key is not comparable: %T", key)
	}
	return nil
}

func IsUnknownCommand(err error) bool {
	var redisErr RedisError
	if errors.As(err, &redisErr) {
		return strings.Contains(strings.ToLower(redisErr.Error()), "unknown command")
	}
	return strings.Contains(strings.ToLower(err.Error()), "unknown command")
}
