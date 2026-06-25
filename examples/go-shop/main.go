package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

type product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

var products = map[int]product{
	42: {ID: 42, Name: "Demo Product", Price: 19.99},
}

func main() {
	redisAddr := getenv("REDIS_ADDR", "127.0.0.1:6380")
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("close redis: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler())
	mux.HandleFunc("POST /test/reset", resetHandler(client))
	mux.HandleFunc("GET /catalog/42", catalogHandler(client))
	mux.HandleFunc("POST /checkout", checkoutHandler(client))

	addr := getenv("SHOP_ADDR", "127.0.0.1:8000")
	log.Printf("go-shop listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func resetHandler(client *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := client.Set(r.Context(), "cart:7", `{"items":[1]}`, time.Hour).Err(); err != nil {
			http.Error(w, "reset failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func catalogHandler(client *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		const key = "product:42"
		raw, err := client.Get(ctx, key).Result()
		if err == nil {
			writeJSON(w, []byte(raw))
			return
		}
		if !errors.Is(err, redis.Nil) {
			http.Error(w, "catalog unavailable", http.StatusInternalServerError)
			return
		}
		item := products[42]
		encoded, err := json.Marshal(item)
		if err != nil {
			http.Error(w, "catalog unavailable", http.StatusInternalServerError)
			return
		}
		cacheCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		if err := client.Set(cacheCtx, key, encoded, time.Hour).Err(); err != nil {
			log.Printf("cache set failed: %v", err)
		}
		writeJSON(w, encoded)
	}
}

func checkoutHandler(client *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cart, err := client.Get(r.Context(), "cart:7").Result()
		if err != nil || cart == "" {
			http.Error(w, "cart missing from cache", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
