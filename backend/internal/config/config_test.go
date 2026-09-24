package config

import (
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func load(values map[string]string) (Config, error) {
	return Load(func(key string) string { return values[key] })
}

func TestDefaultsAndRedisDeadlines(t *testing.T) {
	c, err := load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Production() || c.HealthURL() != "http://127.0.0.1:8080/readyz" || len(c.TrustedProxies) != 0 {
		t.Fatalf("unsafe defaults: %#v", c)
	}
	client := redis.NewClient(c.Redis)
	defer client.Close()
	if client.Options().MaxRetries != 0 || !client.Options().ContextTimeoutEnabled {
		t.Fatal("Redis must honor deadlines without implicit command retries")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"APP_ENV", "prod"}, {"HTTP_ADDR", "localhost"}, {"HTTP_ADDR", ":0"}, {"HTTP_ADDR", ":65536"},
		{"REDIS_NAMESPACE", "wrong:{namespace}"}, {"REDIS_URL", "https://secret:password@redis.invalid"},
		{"RATE_LIMIT_PER_SECOND", "NaN"}, {"RATE_LIMIT_BURST", "NaN"}, {"RATE_LIMIT_PER_SECOND", "+Inf"}, {"RATE_LIMIT_BURST", "0"},
		{"ALLOWED_ORIGINS", "https://*.example.com"}, {"ALLOWED_ORIGINS", "https://example.com/"},
		{"ALLOWED_ORIGINS", "https://user:secret@example.com"}, {"ALLOWED_ORIGINS", "https://example.com?x=1"},
		{"ALLOWED_ORIGINS", "https://example.com#"}, {"ALLOWED_ORIGINS", "https://example.com:"},
		{"ALLOWED_ORIGINS", "https://example.com:65536"}, {"ALLOWED_ORIGINS", "null"}, {"ALLOWED_ORIGINS", "https://example.com,"},
		{"TRUSTED_PROXY_CIDRS", "0.0.0.0/0"}, {"TRUSTED_PROXY_CIDRS", "::/0"}, {"TRUSTED_PROXY_CIDRS", "localhost"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			_, err := load(map[string]string{tc.key: tc.value})
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected useful configuration error, got %v", err)
			}
			if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "secret") {
				t.Fatal("configuration errors must not expose credentials")
			}
		})
	}
}

func TestProductionAndCustomHealthAddress(t *testing.T) {
	if _, err := load(map[string]string{"APP_ENV": "production"}); err == nil {
		t.Fatal("production accepted local HTTP origins")
	}
	for address, expected := range map[string]string{
		"0.0.0.0:18081": "http://127.0.0.1:18081/readyz", "[::]:18082": "http://[::1]:18082/readyz", "localhost:8085": "http://localhost:8085/readyz",
	} {
		c, err := load(map[string]string{"APP_ENV": "production", "ALLOWED_ORIGINS": " https://quiz.example.com , https://quiz.example.com ", "HTTP_ADDR": address, "TRUSTED_PROXY_CIDRS": "127.0.0.1/32, ::1/128"})
		if err != nil || len(c.Origins) != 1 || len(c.TrustedProxies) != 2 || !c.Production() || c.HealthURL() != expected {
			t.Fatalf("invalid production configuration: %v %#v", err, c)
		}
	}
}
