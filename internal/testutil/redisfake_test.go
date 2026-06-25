package testutil

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"cacheproof/internal/resp"
)

func TestJSONServerAndRedisFake(t *testing.T) {
	server := JSONServer(http.StatusAccepted, `{"ok":true}`)
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get json server: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if response.StatusCode != http.StatusAccepted || string(body) != `{"ok":true}` {
		t.Fatalf("unexpected response: %d %s", response.StatusCode, body)
	}

	fake := StartRedisFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := resp.DialContext(ctx, fake.Addr, resp.Auth{})
	if err != nil {
		t.Fatalf("dial fake redis: %v", err)
	}
	defer client.Close()
	if value, err := client.Do(ctx, "GET", "k"); err != nil || value != "value" {
		t.Fatalf("unexpected get: %v %v", value, err)
	}
	if _, err := client.Do(ctx, "SET", "k", "v"); err != nil {
		t.Fatalf("set fake redis: %v", err)
	}
	if _, err := client.Do(ctx, "SCAN", "0", "MATCH", "k", "COUNT", "100"); err != nil {
		t.Fatalf("scan fake redis: %v", err)
	}
	if len(fake.Commands()) == 0 {
		t.Fatal("expected recorded commands")
	}
}
