// Package config loads and validates CloudGate configuration from file and env vars.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration structure.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Cassandra CassandraConfig `mapstructure:"cassandra"`
	Admin     AdminConfig     `mapstructure:"admin"`
	Tenants   []TenantConfig  `mapstructure:"tenants"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Addr        string        `mapstructure:"addr"`
	MetricsAddr string        `mapstructure:"metrics_addr"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addrs    []string `mapstructure:"addrs"`
	Password string   `mapstructure:"password"`
	DB       int      `mapstructure:"db"`
}

// CassandraConfig holds Cassandra connection settings.
type CassandraConfig struct {
	Hosts    []string `mapstructure:"hosts"`
	Keyspace string   `mapstructure:"keyspace"`
	Username string   `mapstructure:"username"`
	Password string   `mapstructure:"password"`
}

// AdminConfig holds gRPC admin server settings.
type AdminConfig struct {
	GRPCAddr string `mapstructure:"grpc_addr"`
}

// TenantConfig defines upstream routing and rate-limit policy for a tenant.
type TenantConfig struct {
	ID              string         `mapstructure:"id"`
	Upstreams       []string       `mapstructure:"upstreams"`
	RateLimit       RateLimitConf  `mapstructure:"rate_limit"`
	CircuitBreaker  CBConf         `mapstructure:"circuit_breaker"`
	BulkheadMax     int            `mapstructure:"bulkhead_max"`
}

// RateLimitConf holds rate limiting configuration for a tenant.
type RateLimitConf struct {
	// Requests per second for token bucket
	RequestsPerSecond int `mapstructure:"requests_per_second"`
	// Burst size (token bucket capacity)
	Burst int `mapstructure:"burst"`
	// Window in seconds for sliding window counter
	WindowSeconds int `mapstructure:"window_seconds"`
	// Max requests in window
	MaxInWindow int `mapstructure:"max_in_window"`
}

// CBConf holds circuit breaker configuration.
type CBConf struct {
	// Consecutive failures before opening
	FailureThreshold int           `mapstructure:"failure_threshold"`
	// How long to stay open before trying half-open
	OpenTimeout      time.Duration `mapstructure:"open_timeout"`
	// Consecutive successes in half-open to close
	SuccessThreshold int           `mapstructure:"success_threshold"`
}

// Load reads configuration from cloudgate.yaml and environment variables.
// Environment variables are prefixed with CLOUDGATE_ and override file values.
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.addr", ":8080")
	v.SetDefault("server.metrics_addr", ":9090")
	v.SetDefault("redis.addrs", []string{"localhost:6379"})
	v.SetDefault("redis.db", 0)
	v.SetDefault("cassandra.hosts", []string{"localhost"})
	v.SetDefault("cassandra.keyspace", "cloudgate")
	v.SetDefault("admin.grpc_addr", ":50051")

	// Config file
	v.SetConfigName("cloudgate")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/cloudgate")

	// Env override
	v.SetEnvPrefix("CLOUDGATE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		// Config file is optional; env vars alone are sufficient
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}
