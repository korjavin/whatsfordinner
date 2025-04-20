package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	// Telegram Bot configuration
	BotToken string

	// OpenAI configuration
	OpenAIAPIBase string
	OpenAIAPIKey  string
	OpenAIModel   string

	// Application configuration
	Cuisines []string
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	// Load .env file if it exists
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	cfg := &Config{}

	// Required configurations
	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("BOT_TOKEN environment variable is required")
	}
	cfg.BotToken = botToken

	openAIAPIKey := os.Getenv("OPENAI_API_KEY")
	if openAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}
	cfg.OpenAIAPIKey = openAIAPIKey

	// Optional configurations with defaults
	cfg.OpenAIAPIBase = getEnvWithDefault("OPENAI_API_BASE", "https://api.openai.com/v1")
	cfg.OpenAIModel = getEnvWithDefault("OPENAI_MODEL", "gpt-3.5-turbo")

	// Parse cuisines
	cuisinesStr := getEnvWithDefault("CUISINES", "European,Russian,Italian")
	cfg.Cuisines = strings.Split(cuisinesStr, ",")

	// Log configuration with sensitive data redacted
	logCfg := *cfg
	if len(logCfg.BotToken) > 8 {
		logCfg.BotToken = logCfg.BotToken[:8] + "...REDACTED..."
	}
	if len(logCfg.OpenAIAPIKey) > 8 {
		logCfg.OpenAIAPIKey = logCfg.OpenAIAPIKey[:8] + "...REDACTED..."
	}
	log.Printf("Configuration loaded: %+v", logCfg)
	return cfg, nil
}

// getEnvWithDefault returns the value of the environment variable or the default value
func getEnvWithDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
