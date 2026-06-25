package resp

import "strings"

type Command struct {
	Args []string
	Name string
	Raw  []byte
}

var keyPosition = map[string]int{
	"GET": 1, "SET": 1, "SETEX": 1, "SETNX": 1, "PSETEX": 1, "GETEX": 1, "GETDEL": 1,
	"DEL": 1, "UNLINK": 1, "EXPIRE": 1, "PEXPIRE": 1, "TTL": 1, "PTTL": 1, "PERSIST": 1,
	"INCR": 1, "DECR": 1, "INCRBY": 1, "APPEND": 1, "STRLEN": 1,
	"HGET": 1, "HSET": 1, "HDEL": 1, "HGETALL": 1, "HMGET": 1,
	"LPUSH": 1, "RPUSH": 1, "LPOP": 1, "RPOP": 1, "LRANGE": 1, "LLEN": 1,
	"SADD": 1, "SREM": 1, "SMEMBERS": 1,
	"ZADD": 1, "ZRANGE": 1, "ZSCORE": 1,
	"XADD":   1,
	"EXISTS": 1, "TYPE": 1, "DUMP": 1,
}

func NewCommand(args []string, raw []byte) *Command {
	name := ""
	if len(args) > 0 {
		name = strings.ToUpper(args[0])
	}
	return &Command{Args: args, Name: name, Raw: raw}
}

func (c *Command) Key() (string, bool) {
	position, ok := keyPosition[c.Name]
	if !ok || position >= len(c.Args) {
		return "", false
	}
	return c.Args[position], true
}
