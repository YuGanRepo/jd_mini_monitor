package cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/syscmd"
)

const (
	rootCertFile = "root-ca.pem"
	rootKeyFile  = "root-ca-key.pem"
)

type Manager struct {
	dir      string
	certPath string
	keyPath  string
	rootCert *x509.Certificate
	rootKey  *rsa.PrivateKey
	cache    map[string]tls.Certificate
	mu       sync.Mutex
}

func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	manager := &Manager{
		dir:      dir,
		certPath: filepath.Join(dir, rootCertFile),
		keyPath:  filepath.Join(dir, rootKeyFile),
		cache:    make(map[string]tls.Certificate),
	}
	if err := manager.loadOrCreateRoot(); err != nil {
		return nil, err
	}

	return manager, nil
}

func (manager *Manager) RootCertPath() string {
	return manager.certPath
}

func (manager *Manager) Thumbprint() string {
	hash := sha1.Sum(manager.rootCert.Raw)
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func (manager *Manager) CertificateFor(host string) (tls.Certificate, error) {
	host = normalizeHost(host)
	if host == "" {
		return tls.Certificate{}, fmt.Errorf("host is required")
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	if certificate, ok := manager.cache[host]; ok {
		return certificate, nil
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}

	leaf := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: host,
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		leaf.IPAddresses = []net.IP{ip}
	} else {
		leaf.DNSNames = []string{host}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, leaf, manager.rootCert, &leafKey.PublicKey, manager.rootKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	certificate := tls.Certificate{
		Certificate: [][]byte{certDER, manager.rootCert.Raw},
		PrivateKey:  leafKey,
		Leaf:        leaf,
	}
	manager.cache[host] = certificate
	return certificate, nil
}

func (manager *Manager) InstallTrustedRoot() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("certificate install is only supported on Windows")
	}
	command := syscmd.Command("certutil", "-user", "-addstore", "Root", manager.certPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil addstore failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (manager *Manager) UninstallTrustedRoot() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("certificate uninstall is only supported on Windows")
	}
	command := syscmd.Command("certutil", "-user", "-delstore", "Root", manager.Thumbprint())
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil delstore failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (manager *Manager) IsTrustedRootInstalled() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	command := syscmd.Command("certutil", "-user", "-store", "Root", manager.Thumbprint())
	output, err := command.CombinedOutput()
	return err == nil && strings.Contains(strings.ToUpper(string(output)), manager.Thumbprint())
}

func (manager *Manager) loadOrCreateRoot() error {
	certPEM, certErr := os.ReadFile(manager.certPath)
	keyPEM, keyErr := os.ReadFile(manager.keyPath)
	if certErr == nil && keyErr == nil {
		return manager.loadRoot(certPEM, keyPEM)
	}
	return manager.createRoot()
}

func (manager *Manager) loadRoot(certPEM, keyPEM []byte) error {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid root certificate file")
	}
	rootCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return err
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "RSA PRIVATE KEY" {
		return fmt.Errorf("invalid root key file")
	}
	rootKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return err
	}

	manager.rootCert = rootCert
	manager.rootKey = rootKey
	return nil
}

func (manager *Manager) createRoot() error {
	rootKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}

	rootCert := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Mini Proxy Local Root CA",
			Organization: []string{"Mini Proxy"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, rootCert, rootCert, &rootKey.PublicKey, rootKey)
	if err != nil {
		return err
	}
	parsedRoot, err := x509.ParseCertificate(certDER)
	if err != nil {
		return err
	}

	certFile, err := os.OpenFile(manager.certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		_ = certFile.Close()
		return err
	}
	if err := certFile.Close(); err != nil {
		return err
	}

	keyFile, err := os.OpenFile(manager.keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rootKey)}); err != nil {
		_ = keyFile.Close()
		return err
	}
	if err := keyFile.Close(); err != nil {
		return err
	}

	manager.rootCert = parsedRoot
	manager.rootKey = rootKey
	return nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil && parsedHost != "" {
		return strings.Trim(parsedHost, "[]")
	}
	lastColon := strings.LastIndex(host, ":")
	if lastColon > -1 && !strings.Contains(host[:lastColon], ":") {
		return strings.Trim(host[:lastColon], "[]")
	}
	return strings.Trim(host, "[]")
}
