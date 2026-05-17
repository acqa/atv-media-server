package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	MediaPath           string
	HTTPPort            string
	HTTPSPort           string
	CertDir             string
	BaseHost            string
	DataDir             string
	TMDbAPIKey          string
	TranscodeCacheMaxGB int
	AdminPort           string
	AdminUser           string
	AdminPass           string
	LogToFile           bool
	LoggingPath         string

	// DohURL is the optional comma-separated list of DNS-over-HTTPS endpoint
	// URLs used to resolve TMDb hosts (image.tmdb.org, api.themoviedb.org) on
	// networks that DNS-sinkhole them. Empty → dnsdoh.DefaultProviders
	// (Cloudflare primary, Quad9 fallback).
	DohURL []string
}

// Version is set via -ldflags at build time.
var Version = "dev"

// Load builds Config from environment variables, falling back to sensible defaults.
// Returns an error if required values are missing or invalid.
func Load(getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	c := &Config{
		MediaPath:   getenv("MEDIA_PATH"),
		HTTPPort:    valueOr(getenv("HTTP_PORT"), "80"),
		HTTPSPort:   valueOr(getenv("HTTPS_PORT"), "443"),
		CertDir:     valueOr(getenv("CERT_DIR"), "/certs"),
		BaseHost:    valueOr(getenv("BASE_HOST"), "appletv.redbull.tv"),
		DataDir:     valueOr(getenv("DATA_DIR"), "/data"),
		TMDbAPIKey:  getenv("TMDB_API_KEY"),
		AdminPort:   valueOr(getenv("ADMIN_PORT"), "8080"),
		AdminUser:   getenv("ADMIN_USER"),
		AdminPass:   getenv("ADMIN_PASS"),
		LoggingPath: valueOr(getenv("LOGGING_PATH"), "/data/logs"),
	}
	if v := getenv("LOG_TO_FILE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, errors.New("LOG_TO_FILE must be a boolean: " + err.Error())
		}
		c.LogToFile = b
	}
	c.TranscodeCacheMaxGB = 50
	if v := getenv("TRANSCODE_CACHE_MAX_GB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("TRANSCODE_CACHE_MAX_GB must be a positive integer")
		}
		c.TranscodeCacheMaxGB = n
	}
	if v := getenv("DOH_URL"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.DohURL = append(c.DohURL, p)
			}
		}
	}
	if c.MediaPath == "" {
		return nil, errors.New("MEDIA_PATH is required")
	}
	return c, nil
}

// CertPath returns the absolute file path for a certificate artefact.
func (c *Config) CertPath(name string) string {
	return c.CertDir + "/" + name
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
