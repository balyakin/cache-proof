package report

import (
	"encoding/json"
	"io"
)

func WriteJSON(w io.Writer, result RunResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
