package config

import "time"

func DefaultSuspicious() []string {
	return []string{
		"XADD", "XREAD", "XRANGE",
		"LPUSH", "RPUSH", "BLPOP", "BRPOP",
		"PUBLISH", "SUBSCRIBE", "PSUBSCRIBE",
		"PERSIST",
	}
}

func DefaultConfig() Config {
	statusOK := 200
	return Config{
		Version:       1,
		Seed:          1,
		ProbeTimeout:  Duration(30 * time.Second),
		WarmupRetries: 1,
		WarmupDelay:   Duration(500 * time.Millisecond),
		Proxy: ProxyConfig{
			Listen:              "127.0.0.1:6380",
			Upstream:            "127.0.0.1:6379",
			UpstreamUsernameEnv: "",
			UpstreamPasswordEnv: "",
		},
		Safety: SafetyConfig{
			AllowKeyDeletes: false,
			MaxKeysDelete:   10000,
			MinDeletePrefix: 3,
		},
		Profiles: []Profile{
			{
				Name:        "cache",
				KeyPatterns: []string{"product:*", "cache:*"},
				MustExpire:  true,
				MaxTTL:      Duration(24 * time.Hour),
				Disposable:  true,
			},
		},
		SuspiciousCommands: DefaultSuspicious(),
		MaxValueBytes:      1048576,
		MaxKeysScanned:     5000,
		Scenarios: []Scenario{
			{Name: "random-miss", Type: "random_miss", Probability: 0.30},
			{Name: "redis-down", Type: "redis_unavailable"},
		},
		Probes: []Probe{
			{
				Name: "catalog",
				HTTP: &HTTPProbe{
					Method:  "GET",
					URL:     "http://127.0.0.1:8000/catalog/42",
					Headers: map[string]string{},
					Body:    "",
				},
				Assert: Assertion{
					Expect:             "pass",
					Status:             statusOK,
					JSONSchema:         "schemas/product.json",
					JSONEqualsBaseline: []string{"id", "name", "price"},
					MaxLatencyIncrease: 1.00,
				},
			},
			{
				Name: "checkout",
				HTTP: &HTTPProbe{
					Method: "POST",
					URL:    "http://127.0.0.1:8000/checkout",
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
					Body: `{"cart_id":7}`,
				},
				Assert: Assertion{
					Expect: "pass",
					Status: statusOK,
				},
			},
		},
	}
}

const StarterYAML = `version: 1

proxy:
  listen: "127.0.0.1:6380"
  upstream: "127.0.0.1:6379"
  upstream_username_env: ""
  upstream_password_env: ""

seed: 1
probe_timeout: "30s"
warmup_retries: 1
warmup_delay: "500ms"

safety:
  allow_key_deletes: false
  max_keys_delete: 10000
  min_delete_prefix: 3

profiles:
  - name: cache
    key_patterns: ["product:*", "cache:*"]
    must_expire: true
    max_ttl: "24h"
    disposable: true

suspicious_commands:
  - XADD
  - XREAD
  - XRANGE
  - LPUSH
  - RPUSH
  - BLPOP
  - BRPOP
  - PUBLISH
  - SUBSCRIBE
  - PSUBSCRIBE
  - PERSIST

max_value_bytes: 1048576
max_keys_scanned: 5000

scenarios:
  - name: random-miss
    type: random_miss
    probability: 0.30

  - name: redis-down
    type: redis_unavailable

probes:
  - name: catalog
    http:
      method: GET
      url: "http://127.0.0.1:8000/catalog/42"
      headers: {}
      body: ""
    assert:
      expect: pass
      status: 200
      json_schema: "schemas/product.json"
      json_equals_baseline: ["id", "name", "price"]
      max_latency_increase: 1.00

  - name: checkout
    http:
      method: POST
      url: "http://127.0.0.1:8000/checkout"
      headers:
        Content-Type: "application/json"
      body: '{"cart_id":7}'
    assert:
      expect: pass
      status: 200
`
