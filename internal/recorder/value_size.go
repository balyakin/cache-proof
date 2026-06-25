package recorder

import "cacheproof/internal/resp"

func valueSize(cmd *resp.Command) (int, bool) {
	switch cmd.Name {
	case "SET":
		if len(cmd.Args) >= 3 {
			return len(cmd.Args[2]), true
		}
	case "SETEX", "PSETEX":
		if len(cmd.Args) >= 4 {
			return len(cmd.Args[3]), true
		}
	case "HSET":
		maxSize := 0
		found := false
		for index := 3; index < len(cmd.Args); index += 2 {
			size := len(cmd.Args[index])
			if size > maxSize {
				maxSize = size
			}
			found = true
		}
		return maxSize, found
	case "APPEND":
		if len(cmd.Args) >= 3 {
			return len(cmd.Args[2]), true
		}
	}
	return 0, false
}
