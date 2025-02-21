package config

import (
	"log"
	"os"
)

func checkEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("WARNING: %s environment variable is required!", key)
	}
	return value
}
