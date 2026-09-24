package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr               string
	PublicHost               string
	DataDir                  string
	DatabaseURL              string
	CaptureTimeout           time.Duration
	ModelMode                string
	SessionSecure            bool
	LocalEmbeddingURL        string
	LocalEmbeddingModel      string
	LocalEmbeddingDimensions int
	OpenAIKeyFile            string
}

func Load() (Config, error) {
	dataDir := os.Getenv("APP_DATA_DIR")
	if dataDir == "" {
		userConfig, err := os.UserConfigDir()
		if err != nil {
			return Config{}, errors.New("cannot determine application data directory")
		}
		dataDir = filepath.Join(userConfig, "OnboardMePlease")
	}
	absoluteData, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, errors.New("invalid application data directory")
	}
	listen := os.Getenv("APP_LISTEN_ADDR")
	if listen == "" {
		listen = "127.0.0.1:8765"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return Config{}, errors.New("invalid APP_LISTEN_ADDR")
	}
	containerMode := os.Getenv("APP_CONTAINER_MODE") == "1"
	publicHost := listen
	if host == "0.0.0.0" && containerMode {
		publicHost = net.JoinHostPort("127.0.0.1", port)
	} else if net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return Config{}, errors.New("Phase 1 requires loopback, or APP_CONTAINER_MODE=1 for a loopback-published container")
	}
	mode := os.Getenv("MODEL_MODE")
	if mode == "" {
		mode = "strict_local"
	}
	if mode != "strict_local" && mode != "cloud_opt_in" {
		return Config{}, errors.New("MODEL_MODE must be strict_local or cloud_opt_in")
	}
	localURL := strings.TrimSpace(os.Getenv("LOCAL_EMBEDDING_URL"))
	localModel := strings.TrimSpace(os.Getenv("LOCAL_EMBEDDING_MODEL"))
	localDimensions := 0
	if raw := strings.TrimSpace(os.Getenv("LOCAL_EMBEDDING_DIMENSIONS")); raw != "" {
		localDimensions, err = strconv.Atoi(raw)
		if err != nil || localDimensions < 1 || localDimensions > 2000 {
			return Config{}, errors.New("LOCAL_EMBEDDING_DIMENSIONS must be 1-2000")
		}
	}
	if localURL != "" && (localModel == "" || localDimensions == 0) {
		return Config{}, errors.New("local embedding model and dimensions are required with LOCAL_EMBEDDING_URL")
	}
	timeoutText := os.Getenv("APP_CAPTURE_TIMEOUT")
	if timeoutText == "" {
		timeoutText = "2h"
	}
	captureTimeout, err := time.ParseDuration(timeoutText)
	if err != nil || captureTimeout < time.Minute || captureTimeout > 24*time.Hour {
		return Config{}, errors.New("APP_CAPTURE_TIMEOUT must be between 1m and 24h")
	}
	var databaseURL string
	if secretFile := os.Getenv("DATABASE_URL_FILE"); secretFile != "" {
		content, err := os.ReadFile(secretFile)
		if err != nil {
			return Config{}, errors.New("DATABASE_URL_FILE cannot be read")
		}
		databaseURL = strings.TrimSpace(string(content))
	} else {
		databaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if passwordFile := os.Getenv("DATABASE_PASSWORD_FILE"); passwordFile != "" {
		if databaseURL == "" {
			return Config{}, errors.New("DATABASE_URL is required with DATABASE_PASSWORD_FILE")
		}
		passwordBytes, err := os.ReadFile(passwordFile)
		if err != nil {
			return Config{}, errors.New("DATABASE_PASSWORD_FILE cannot be read")
		}
		parsed, err := url.Parse(databaseURL)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil {
			return Config{}, errors.New("DATABASE_URL must be a PostgreSQL URL with a user")
		}
		parsed.User = url.UserPassword(parsed.User.Username(), strings.TrimSpace(string(passwordBytes)))
		databaseURL = parsed.String()
	}
	return Config{ListenAddr: listen, PublicHost: publicHost, DataDir: absoluteData, DatabaseURL: databaseURL,
		CaptureTimeout: captureTimeout, ModelMode: mode,
		LocalEmbeddingURL: localURL, LocalEmbeddingModel: localModel, LocalEmbeddingDimensions: localDimensions,
		OpenAIKeyFile: strings.TrimSpace(os.Getenv("OPENAI_API_KEY_FILE"))}, nil
}
