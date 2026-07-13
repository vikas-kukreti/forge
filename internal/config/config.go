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
	SignupGrantCredits      int64
	MaxProjectsPerUser      int
	AdminEmails             string
	Signups                 string
	NodeName                string
	NodeInternalAddr        string
	WSRoot                  string
	SbxSubnet               string
	SbxBridge               string
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
		CookieSecret:       requireEnv("FORGE_COOKIE_SECRET"),
		TLS:                getEnvOrDefault("FORGE_TLS", "on"),
		MetricsAddr:        getEnvOrDefault("FORGE_METRICS_ADDR", "localhost:9090"),
		Runtime:            getEnvOrDefault("FORGE_RUNTIME", "runsc"),
		SignupGrantCredits: getEnvOrDefaultInt64("FORGE_SIGNUP_GRANT_CREDITS", 50),
		MaxProjectsPerUser: getEnvOrDefaultInt("FORGE_MAX_PROJECTS_PER_USER", 10),
		AdminEmails:        getEnvOrDefault("FORGE_ADMIN_EMAILS", ""),
		Signups:            getEnvOrDefault("FORGE_SIGNUPS", "open"),
		NodeName:           getEnvOrDefault("FORGE_NODE_NAME", "worker-1"),
		NodeInternalAddr:   getEnvOrDefault("FORGE_NODE_INTERNAL_ADDR", "localhost:7443"),
		WSRoot:             getEnvOrDefault("WS_ROOT", "/var/lib/forge/workspaces"),
		SbxSubnet:          getEnvOrDefault("FORGE_SBX_SUBNET", "10.66.0.0/16"),
		SbxBridge:          getEnvOrDefault("FORGE_SBX_BRIDGE", "forge-sbx"),
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

func getEnvOrDefaultInt(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	var i int
	_, err := fmt.Sscanf(val, "%d", &i)
	if err != nil {
		return def
	}
	return i
}

func getEnvOrDefaultInt64(key string, def int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	var i int64
	_, err := fmt.Sscanf(val, "%d", &i)
	if err != nil {
		return def
	}
	return i
}

func getEnvOrDefault(key, def string) string {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	return val
}
