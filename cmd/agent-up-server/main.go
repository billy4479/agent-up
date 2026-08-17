package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/billy4479/agent-up/internal/server"
)

func main() {
	listen := envOrDefault("AGENTUP_LISTEN", ":8080")
	dataDir := envOrDefault("AGENTUP_DATA_DIR", "./data")
	maxUploadSize, err := parseMaxUploadSize(os.Getenv("AGENTUP_MAX_UPLOAD_SIZE"))
	if err != nil {
		log.Fatal(err)
	}
	uploadTTL, err := parseUploadTTL(os.Getenv("AGENTUP_UPLOAD_TTL"))
	if err != nil {
		log.Fatal(err)
	}
	service, err := server.New(dataDir, maxUploadSize, uploadTTL)
	if err != nil {
		log.Fatal(err)
	}
	if err := service.CleanupExpired(); err != nil {
		log.Printf("cleanup expired uploads: %v", err)
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := service.CleanupExpired(); err != nil {
				log.Printf("cleanup expired uploads: %v", err)
			}
		}
	}()
	log.Printf("listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, service.Handler()))
}

func parseUploadTTL(value string) (time.Duration, error) {
	if value == "" {
		return 24 * time.Hour, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl <= 0 {
		return 0, fmt.Errorf("AGENTUP_UPLOAD_TTL must be a positive duration such as 24h")
	}
	return ttl, nil
}

func parseMaxUploadSize(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("AGENTUP_MAX_UPLOAD_SIZE must be a positive byte count")
	}
	return size, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
