package config

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Environment, HTTPAddr, StaticDir, Namespace string
	Origins                                     []string
	TrustedProxies                              []netip.Prefix
	Rate, Burst                                 float64
	Redis                                       *redis.Options
}

var namespacePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func Load(getenv func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	c := Config{
		Environment: value("APP_ENV", "development"),
		HTTPAddr:    value("HTTP_ADDR", "127.0.0.1:8080"),
		StaticDir:   value("STATIC_DIR", "../frontend/dist"),
		Namespace:   value("REDIS_NAMESPACE", "vocab-practice"),
	}
	if c.Environment != "development" && c.Environment != "production" {
		return c, fmt.Errorf("APP_ENV must be development or production")
	}
	if _, _, err := listenAddress(c.HTTPAddr); err != nil {
		return c, err
	}
	if !namespacePattern.MatchString(c.Namespace) {
		return c, fmt.Errorf("REDIS_NAMESPACE must contain 1-64 letters, digits, underscores or hyphens")
	}
	var err error
	if c.Rate, err = number(value("RATE_LIMIT_PER_SECOND", "10"), "RATE_LIMIT_PER_SECOND", 10000); err != nil {
		return c, err
	}
	if c.Burst, err = number(value("RATE_LIMIT_BURST", "40"), "RATE_LIMIT_BURST", 20000); err != nil {
		return c, err
	}
	origins := value("ALLOWED_ORIGINS", "http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173")
	seen := map[string]bool{}
	for _, raw := range strings.Split(origins, ",") {
		origin := strings.TrimSpace(raw)
		u, parseErr := url.Parse(origin)
		if parseErr != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(origin, "*\\#") || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("ALLOWED_ORIGINS must be exact http(s) origins without paths, credentials or wildcards")
		}
		if u.Port() != "" {
			if _, err := port(u.Port()); err != nil {
				return c, fmt.Errorf("ALLOWED_ORIGINS contains an invalid port")
			}
		} else if strings.HasSuffix(u.Host, ":") {
			return c, fmt.Errorf("ALLOWED_ORIGINS contains an empty port")
		}
		if c.Production() && u.Scheme != "https" {
			return c, fmt.Errorf("production requires exact HTTPS ALLOWED_ORIGINS")
		}
		if !seen[origin] {
			c.Origins = append(c.Origins, origin)
			seen[origin] = true
		}
	}
	if proxies := strings.TrimSpace(getenv("TRUSTED_PROXY_CIDRS")); proxies != "" {
		for _, raw := range strings.Split(proxies, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
			if err != nil || prefix.Bits() == 0 {
				return c, fmt.Errorf("TRUSTED_PROXY_CIDRS requires explicit proxy CIDRs; trusting every address is not allowed")
			}
			c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
		}
	}
	c.Redis, err = redis.ParseURL(value("REDIS_URL", "redis://127.0.0.1:6379/0"))
	if err != nil {
		return c, fmt.Errorf("REDIS_URL is invalid")
	}
	c.Redis.DialTimeout = 700 * time.Millisecond
	c.Redis.ReadTimeout = 700 * time.Millisecond
	c.Redis.WriteTimeout = 700 * time.Millisecond
	c.Redis.PoolTimeout = 800 * time.Millisecond
	c.Redis.ContextTimeoutEnabled = true
	c.Redis.MaxRetries = -1
	c.Redis.PoolSize = 32
	return c, nil
}

func (c Config) Production() bool { return c.Environment == "production" }

func (c Config) HealthURL() string {
	host, port, _ := listenAddress(c.HTTPAddr)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz"
}

func listenAddress(address string) (string, string, error) {
	host, p, err := net.SplitHostPort(address)
	if err != nil {
		return "", "", fmt.Errorf("HTTP_ADDR must be a host:port address")
	}
	if _, err := port(p); err != nil {
		return "", "", fmt.Errorf("HTTP_ADDR requires a port between 1 and 65535")
	}
	return host, p, nil
}

func port(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid port")
	}
	return n, nil
}

func number(value, name string, maximum float64) (float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > maximum {
		return 0, fmt.Errorf("%s must be a finite number between 1 and %.0f", name, maximum)
	}
	return n, nil
}
