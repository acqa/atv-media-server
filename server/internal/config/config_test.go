package config

import (
	"testing"
)

func envFunc(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

func TestLoad_RequiresMediaPath(t *testing.T) {
	_, err := Load(envFunc(map[string]string{}))
	if err == nil {
		t.Fatal("expected error when MEDIA_PATH missing, got nil")
	}
}

func TestLoad_Defaults(t *testing.T) {
	c, err := Load(envFunc(map[string]string{"MEDIA_PATH": "/media"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.HTTPPort != "80" {
		t.Errorf("HTTPPort default: want 80, got %q", c.HTTPPort)
	}
	if c.HTTPSPort != "443" {
		t.Errorf("HTTPSPort default: want 443, got %q", c.HTTPSPort)
	}
	if c.BaseHost != "appletv.redbull.tv" {
		t.Errorf("BaseHost default: want appletv.redbull.tv, got %q", c.BaseHost)
	}
	if c.CertDir != "/certs" {
		t.Errorf("CertDir default: want /certs, got %q", c.CertDir)
	}
	if c.DataDir != "/data" {
		t.Errorf("DataDir default: want /data, got %q", c.DataDir)
	}
	if c.LogToFile {
		t.Error("LogToFile default should be false")
	}
}

func TestLoad_Overrides(t *testing.T) {
	c, err := Load(envFunc(map[string]string{
		"MEDIA_PATH":   "/m",
		"HTTP_PORT":    "8080",
		"HTTPS_PORT":   "8443",
		"BASE_HOST":    "atv.local",
		"CERT_DIR":     "/c",
		"DATA_DIR":     "/d",
		"TMDB_API_KEY": "topsecret",
		"LOG_TO_FILE":  "true",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.HTTPPort != "8080" || c.HTTPSPort != "8443" {
		t.Errorf("ports not overridden: %+v", c)
	}
	if c.BaseHost != "atv.local" || c.CertDir != "/c" || c.DataDir != "/d" {
		t.Errorf("string overrides not applied: %+v", c)
	}
	if c.TMDbAPIKey != "topsecret" {
		t.Errorf("TMDbAPIKey: want topsecret, got %q", c.TMDbAPIKey)
	}
	if !c.LogToFile {
		t.Errorf("LogToFile not parsed as true")
	}
}

func TestLoad_TMDbAPIKey_DefaultsEmpty(t *testing.T) {
	c, err := Load(envFunc(map[string]string{"MEDIA_PATH": "/m"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDbAPIKey != "" {
		t.Errorf("want empty TMDbAPIKey by default, got %q", c.TMDbAPIKey)
	}
}

func TestLoad_LogToFile_Invalid(t *testing.T) {
	_, err := Load(envFunc(map[string]string{
		"MEDIA_PATH":  "/m",
		"LOG_TO_FILE": "not-a-bool",
	}))
	if err == nil {
		t.Fatal("expected error for invalid LOG_TO_FILE, got nil")
	}
}

func TestLoad_TranscodeCacheMaxGB(t *testing.T) {
	c, err := Load(envFunc(map[string]string{"MEDIA_PATH": "/m"}))
	if err != nil {
		t.Fatalf("default TranscodeCacheMaxGB: err %v", err)
	}
	if c.TranscodeCacheMaxGB != 50 {
		t.Errorf("default TranscodeCacheMaxGB: got %d", c.TranscodeCacheMaxGB)
	}
	c, err = Load(envFunc(map[string]string{"MEDIA_PATH": "/m", "TRANSCODE_CACHE_MAX_GB": "100"}))
	if err != nil {
		t.Fatalf("override: err %v", err)
	}
	if c.TranscodeCacheMaxGB != 100 {
		t.Errorf("override: got %d", c.TranscodeCacheMaxGB)
	}
	if _, err := Load(envFunc(map[string]string{"MEDIA_PATH": "/m", "TRANSCODE_CACHE_MAX_GB": "0"})); err == nil {
		t.Error("expected error for zero")
	}
	if _, err := Load(envFunc(map[string]string{"MEDIA_PATH": "/m", "TRANSCODE_CACHE_MAX_GB": "nope"})); err == nil {
		t.Error("expected error for non-int")
	}
}

func TestCertPath(t *testing.T) {
	c := &Config{CertDir: "/certs"}
	if got := c.CertPath("redbulltv.cer"); got != "/certs/redbulltv.cer" {
		t.Errorf("CertPath: want /certs/redbulltv.cer, got %q", got)
	}
}
