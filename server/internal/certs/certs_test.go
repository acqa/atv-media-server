package certs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsure_GeneratesAllArtefacts(t *testing.T) {
	dir := t.TempDir()
	gen, err := EnsureCertificate(dir, "appletv.redbull.tv")
	if err != nil {
		t.Fatalf("EnsureCertificate: %v", err)
	}
	if !gen {
		t.Errorf("first run should report generated=true")
	}
	for _, name := range []string{"redbulltv.pem", "redbulltv.key", "redbulltv.cer"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestEnsure_IdempotentWhenAllPresent(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureCertificate(dir, "h"); err != nil {
		t.Fatal(err)
	}
	pemPath := filepath.Join(dir, "redbulltv.pem")
	before, _ := os.Stat(pemPath)

	gen, err := EnsureCertificate(dir, "h")
	if err != nil {
		t.Fatal(err)
	}
	if gen {
		t.Errorf("second run should report generated=false")
	}
	after, _ := os.Stat(pemPath)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf(".pem was rewritten unnecessarily")
	}
}

func TestEnsure_RegeneratesIfAnyArtefactMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureCertificate(dir, "h"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "redbulltv.cer")); err != nil {
		t.Fatal(err)
	}
	gen, err := EnsureCertificate(dir, "h")
	if err != nil {
		t.Fatal(err)
	}
	if !gen {
		t.Errorf("should regenerate when .cer is missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "redbulltv.cer")); err != nil {
		t.Errorf(".cer not recreated: %v", err)
	}
}

func TestGeneratedCertificate_HasExpectedCN_DNSNames_Validity(t *testing.T) {
	dir := t.TempDir()
	const host = "appletv.redbull.tv"
	if _, err := EnsureCertificate(dir, host); err != nil {
		t.Fatal(err)
	}
	cerBytes, err := os.ReadFile(filepath.Join(dir, "redbulltv.cer"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(cerBytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if cert.Subject.CommonName != host {
		t.Errorf("CN: want %q, got %q", host, cert.Subject.CommonName)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != host {
		t.Errorf("DNS names: %v", cert.DNSNames)
	}
	want := time.Now().AddDate(20, 0, 0)
	if cert.NotAfter.Before(want.AddDate(0, 0, -1)) || cert.NotAfter.After(want.AddDate(0, 0, 1)) {
		t.Errorf("NotAfter not ~20y: %v", cert.NotAfter)
	}
}

func TestGeneratedKeyPair_LoadableByTLS(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureCertificate(dir, "h"); err != nil {
		t.Fatal(err)
	}
	_, err := tls.LoadX509KeyPair(filepath.Join(dir, "redbulltv.pem"), filepath.Join(dir, "redbulltv.key"))
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
}

func TestGeneratedPEM_HasCertificateAndKeyBlocks(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureCertificate(dir, "h"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "redbulltv.pem"))
	rest := data
	var sawCert, sawKey bool
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch block.Type {
		case "CERTIFICATE":
			sawCert = true
		case "RSA PRIVATE KEY", "PRIVATE KEY":
			sawKey = true
		}
	}
	if !sawCert || !sawKey {
		t.Errorf(".pem missing block(s): cert=%v key=%v", sawCert, sawKey)
	}
}
