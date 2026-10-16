package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	MySQLDSN          string
	MySQLMaxOpenConns int
	MySQLMaxIdleConns int
	MySQLConnMaxLife  time.Duration
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	RedisQueue        string
	RedisConcurrency  int
	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	ImageBaseURL      string
	ImageAPIKey       string
	ImageModel        string
	AuthSecret        string
	AuthTTL           time.Duration
}

func Load() Config {
	loadDotEnv(".env")
	return Config{
		HTTPAddr:          env("HTTP_ADDR", ":8080"),
		MySQLDSN:          env("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/packaging_ai?parseTime=true&charset=utf8mb4&loc=Local"),
		MySQLMaxOpenConns: envInt("MYSQL_MAX_OPEN_CONNS", 20),
		MySQLMaxIdleConns: envInt("MYSQL_MAX_IDLE_CONNS", 10),
		MySQLConnMaxLife:  time.Duration(envInt("MYSQL_CONN_MAX_LIFETIME_MIN", 30)) * time.Minute,
		RedisAddr:         env("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		RedisDB:           envInt("REDIS_DB", 0),
		RedisQueue:        env("REDIS_QUEUE", "packaging-ai"),
		RedisConcurrency:  envInt("REDIS_CONCURRENCY", 4),
		LLMBaseURL:        env("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:         os.Getenv("LLM_API_KEY"),
		LLMModel:          env("LLM_MODEL", "gpt-4o-mini"),
		ImageBaseURL:      env("IMAGE_BASE_URL", env("LLM_BASE_URL", "https://api.openai.com/v1")),
		ImageAPIKey:       env("IMAGE_API_KEY", os.Getenv("LLM_API_KEY")),
		ImageModel:        env("IMAGE_MODEL", "gpt-image-1"),
		AuthSecret:        env("AUTH_SECRET", "local-development-secret-change-me"),
		AuthTTL:           time.Duration(envInt("AUTH_TTL_HOURS", 24)) * time.Hour,
	}
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, value)
			}
		}
	}
	if scanner.Err() != nil && scanner.Err() != io.EOF {
		return
	}
}

func (c Config) Validate() error {
	if c.MySQLDSN == "" {
		return fmt.Errorf("MYSQL_DSN is required")
	}
	if c.MySQLMaxOpenConns < 1 || c.MySQLMaxIdleConns < 0 {
		return fmt.Errorf("invalid MySQL pool settings")
	}
	if c.RedisAddr == "" || c.RedisQueue == "" || c.RedisConcurrency < 1 {
		return fmt.Errorf("invalid Redis queue settings")
	}
	if c.LLMBaseURL == "" || c.LLMAPIKey == "" || c.LLMModel == "" {
		return fmt.Errorf("LLM_BASE_URL, LLM_API_KEY and LLM_MODEL are required")
	}
	if c.ImageBaseURL == "" || c.ImageAPIKey == "" || c.ImageModel == "" {
		return fmt.Errorf("IMAGE_BASE_URL, IMAGE_API_KEY and IMAGE_MODEL are required")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}
