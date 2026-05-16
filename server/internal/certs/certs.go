// Package certs generates the self-signed certificate ATV3 expects for the
// hijacked hostname (default appletv.redbull.tv). On first run, the package
// creates redbulltv.pem (cert+key), redbulltv.key (key only) and
// redbulltv.cer (DER cert for the ATV3 profile). Subsequent runs see the
// files and skip generation.
package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// EnsureCertificate creates the three artefacts under dir if any is missing.
// Returns nil and a "generated" flag indicating whether files were written
// (caller logs accordingly). The cert is valid for 20 years.
func EnsureCertificate(dir, host string) (generated bool, err error) {
	pemPath := filepath.Join(dir, "redbulltv.pem")
	keyPath := filepath.Join(dir, "redbulltv.key")
	cerPath := filepath.Join(dir, "redbulltv.cer")
	if exists(pemPath) && exists(keyPath) && exists(cerPath) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return false, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return false, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
			Country:    []string{"US"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{host},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return false, err
	}

	// .cer is DER directly — that's what the ATV3 profile installer expects.
	if err := os.WriteFile(cerPath, der, 0o644); err != nil {
		return false, err
	}

	// .key is the private key in PEM form.
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return false, err
	}

	// .pem is the cert + private key concatenated, for Go's tls.LoadX509KeyPair.
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemBytes := append(certPEM, keyPEM...)
	if err := os.WriteFile(pemPath, pemBytes, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
