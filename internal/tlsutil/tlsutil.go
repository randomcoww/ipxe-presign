package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// buildTLSConfig assembles a TLS configuration that requires and
// verifies client certificates signed by the given CA.
func BuildTLSConfig(certPath, keyPath string, caPaths []string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading server key pair: %w", err)
	}
	pool, err := newCertPool(caPaths)
	if err != nil {
		return nil, fmt.Errorf("no valid certificates found in %v", caPaths)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func BuildTLSCAConfig(caPaths []string) (*tls.Config, error) {
	pool, err := newCertPool(caPaths)
	if err != nil {
		return nil, fmt.Errorf("no valid certificates found in %v", caPaths)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
	}, nil
}

func newCertPool(caPaths []string) (*x509.CertPool, error) {
	certPool, err := x509.SystemCertPool()
	if err != nil {
		certPool = x509.NewCertPool()
	}
	for _, caPath := range caPaths {
		pemByte, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}

		for {
			var block *pem.Block
			block, pemByte = pem.Decode(pemByte)
			if block == nil {
				break
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			certPool.AddCert(cert)
		}
	}

	return certPool, nil
}
