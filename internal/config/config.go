package config

import (
	"fmt"
	"os"
)

type Config struct {
	Domain                  string
	DatabaseURL             string
	NatsURL                 string
	S3Endpoint              string
	S3Bucket                string
	S3AccessKey             string
	S3SecretKey             string
	S3Region                string
	InternalToken           string
	CookieSecret            string
	AnthropicAPIKey         string
	FakeLLM                 bool
	TLS                     string
	MetricsAddr             string
	Runtime                 string
}

func LoadConfig() *Config {
	cfg := &Config{
		Domain:          requireEnv("DOMAIN"),
		DatabaseURL:     requireEnv("FORGE_DATABASE_URL"),
		NatsURL:         requireEnv("FORGE_NATS_URL"),
		S3Endpoint:      requireEnv("FORGE_S3_ENDPOINT"),
		S3Bucket:        requireEnv("FORGE_S3_BUCKET"),
		S3AccessKey:     requireEnv("FORGE_S3_ACCESS_KEY"),
		S3SecretKey:     requireEnv("FORGE_S3_SECRET_KEY"),
		S3Region:        requireEnv("FORGE_S3_REGION"),
		InternalToken:   requireEnv("FORGE_INTERNAL_TOKEN"),
		CookieSecret:    requireEnv("FORGE_COOKIE_SECRET"),
		TLS:             getEnvOrDefault("FORGE_TLS", "on"),
		MetricsAddr:     getEnvOrDefault("FORGE_METRICS_ADDR", "localhost:9090"),
		Runtime:         getEnvOrDefault("FORGE_RUNTIME", "runsc"),
	}

	fakeLlm := os.Getenv("FORGE_FAKE_LLM")
	if fakeLlm == "1" {
		cfg.FakeLLM = true
	} else {
		cfg.AnthropicAPIKey = requireEnv("FORGE_ANTHROPIC_API_KEY")
	}

	return cfg
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fmt.Printf("fatal: missing required config variable %s\n", key)
		os.Exit(1)
	}
	return val
}

func getEnvOrDefault(key, def string) string {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	return val
}
