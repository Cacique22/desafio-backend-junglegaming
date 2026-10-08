package config

import (
	"os"
	"strconv"
)

type AppConfig struct {
	Port               int
	DBHost             string
	DBPort             int
	DBUser             string
	DBPassword         string
	DBName             string
	DBSSLMode          string
	KeycloakIssuer     string
	JWTSecret          string
	AWSEndpoint        string
	AWSRegion          string
	SQSQueueURL        string
	SQSOutboxQueueURL  string
	SQSCommandQueueURL string
	AllowDevTokens     bool
}

func Load() AppConfig {
	port, _ := strconv.Atoi(getEnv("PORT", "8080"))
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))

	sqsQueueURL := getEnv("SQS_QUEUE_URL", "http://localhost:4566/000000000000/wager-transactions.fifo")
	sqsOutboxQueueURL := getEnv("SQS_OUTBOX_QUEUE_URL", "http://localhost:4566/000000000000/wager-events.fifo")
	sqsCommandQueueURL := getEnv("SQS_COMMAND_QUEUE_URL", sqsQueueURL)
	allowDevTokens := getEnv("ALLOW_DEV_TOKENS", "true") == "true"

	return AppConfig{
		Port:               port,
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             dbPort,
		DBUser:             getEnv("DB_USER", "postgres"),
		DBPassword:         getEnv("DB_PASSWORD", "postgres"),
		DBName:             getEnv("DB_NAME", "wager_db"),
		DBSSLMode:          getEnv("DB_SSLMODE", "disable"),
		KeycloakIssuer:     getEnv("KEYCLOAK_ISSUER", "http://localhost:8080/realms/jungle"),
		JWTSecret:          getEnv("JWT_SECRET", "super-secret-development-key-32bytes!!"),
		AWSEndpoint:        getEnv("AWS_ENDPOINT", "http://localhost:4566"),
		AWSRegion:          getEnv("AWS_REGION", "us-east-1"),
		SQSQueueURL:        sqsQueueURL,
		SQSOutboxQueueURL:  sqsOutboxQueueURL,
		SQSCommandQueueURL: sqsCommandQueueURL,
		AllowDevTokens:     allowDevTokens,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
