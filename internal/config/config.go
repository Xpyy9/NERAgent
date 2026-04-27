package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration.
type Config struct {
	LLM  LLMConfig
	JADX JADXConfig
	Web  WebConfig
}

// LLMConfig holds LLM-related configuration.
type LLMConfig struct {
	BaseURL          string
	APIKey           string
	AvailableModels  []string
	DefaultPlanner   string
	DefaultExecutor  string
	DefaultReplanner string
	Timeout          time.Duration
}

// JADXConfig holds JADX service configuration.
type JADXConfig struct {
	BaseURL     string
	CacheSize   int
	CacheTTL    time.Duration
	HTTPTimeout time.Duration
}

// WebConfig holds HTTP server configuration.
type WebConfig struct {
	Host string
	Port string
}

// Load reads .env and returns a typed Config.
func Load() (*Config, error) {
	_ = godotenv.Load()

	return &Config{
		LLM: LLMConfig{
			BaseURL:          envStr("One_BASE_URL", ""),
			APIKey:           envStr("One_API_KEY", ""),
			AvailableModels:  parseModels(),
			DefaultPlanner:   envStr("GPT_MODEL", "GPT-4.1"),
			DefaultExecutor:  envStr("GLM_MODEL", "GLM-5"),
			DefaultReplanner: envStr("GPT_MODEL", "GPT-4.1"),
			Timeout:          time.Duration(envInt("LLM_TIMEOUT", 30)) * time.Second,
		},
		JADX: JADXConfig{
			BaseURL:     envStr("JADX_BASE_URL", "http://localhost:13997"),
			CacheSize:   envInt("JADX_CACHE_SIZE", 500),
			CacheTTL:    10 * time.Minute,
			HTTPTimeout: 90 * time.Second,
		},
		Web: WebConfig{
			Host: envStr("WEB_HOST", "127.0.0.1"),
			Port: envStr("WEB_PORT", "13998"),
		},
	}, nil
}

// ListenAddr returns the formatted listen address.
func (c *WebConfig) ListenAddr() string {
	return c.Host + ":" + c.Port
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func parseModels() []string {
	raw := os.Getenv("LLM_AVAILABLE_MODELS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		if m := strings.TrimSpace(p); m != "" && !seen[m] {
			models = append(models, m)
			seen[m] = true
		}
	}
	// supplement models defined in individual env vars
	envKeys := []string{"GPT_MODEL", "GPT4o_MODEL", "CLAUDE_MODEL", "GLM_MODEL",
		"GEMINI_MODEL", "Qwen3_Coder_MODEL", "DeepSeek_MODEL", "DeepSeekR1_MODEL"}
	for _, key := range envKeys {
		if v := os.Getenv(key); v != "" && !seen[v] {
			models = append(models, v)
			seen[v] = true
		}
	}
	return models
}
