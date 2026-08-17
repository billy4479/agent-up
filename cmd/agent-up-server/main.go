package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/billy4479/agent-up/internal/server"
)

func main() {
	listen := envOrDefault("AGENTUP_LISTEN", ":8080")
	dataDir := envOrDefault("AGENTUP_DATA_DIR", "./data")
	maxUploadSize, err := parseMaxUploadSize(os.Getenv("AGENTUP_MAX_UPLOAD_SIZE"))
	if err != nil {
		log.Fatal(err)
	}
	service, err := server.New(dataDir, maxUploadSize)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, service.Handler()))
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
