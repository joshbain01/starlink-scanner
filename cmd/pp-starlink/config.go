package main

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration. Load once at startup; pass by value.
type Config struct {
	DishAddr        string
	DBPath          string
	TLECacheFile    string
	GrpcurlPath     string
	EnableDishGPS   bool
	LocationCommand string
	Interval        time.Duration
	LossThreshold   float64
	SNRDelta        float64
	NoiseDelta      float64
}

// Load reads configuration from environment variables, falling back to
// production defaults. Never returns an error — bad env values are ignored
// and the safe default is used instead.
func Load() Config {
	return Config{
		DishAddr:        envStr("STARLINK_DISH_ADDR", "192.168.100.1:9200"),
		DBPath:          envStr("DB_PATH", "/data/starlink_telemetry.db"),
		TLECacheFile:    envStr("STARLINK_TLE_CACHE", "/tmp/starlink_current.tle"),
		GrpcurlPath:     envStr("GRPCURL_PATH", detectGrpcurl()),
		EnableDishGPS:   boolEnv("STARLINK_ENABLE_DISH_LOCATION", false),
		LocationCommand: envStr("STARLINK_LOCATION_COMMAND", ""),
		Interval:        15 * time.Second,
		LossThreshold:   floatEnv("STARLINK_LOSS_THRESHOLD", 0.05),
		SNRDelta:        floatEnv("STARLINK_SNR_DELTA", 3.0),
		NoiseDelta:      floatEnv("STARLINK_NOISE_DELTA", 3.0),
	}
}

func detectGrpcurl() string {
	for _, candidate := range []string{"/usr/local/bin/grpcurl", "/usr/bin/grpcurl", "/home/josh/go/bin/grpcurl"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func floatEnv(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			return f
		}
	}
	return def
}

func boolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return def
	}
}
