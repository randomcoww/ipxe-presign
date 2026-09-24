package config

import (
	"crypto/tls"
	"fmt"
	"net/netip"
	"os"
	"time"

	"github.com/randomcoww/ipxe-presign/internal/tlsutil"
	"gopkg.in/yaml.v3"
)

type YamlConfig struct {
	Listen           string        `yaml:"listen,omitempty"`
	ListenHealthz    string        `yaml:"listenHealthz,omitempty"`
	ServerCert       string        `yaml:"serverCert"`
	ServerKey        string        `yaml:"serverKey"`
	TrustedCAs       []string      `yaml:"trustedCAs,omitempty"`
	AllowedClientCNs []string      `yaml:"allowedClientCNs"`
	S3Endpoint       string        `yaml:"s3Endpoint,omitempty"`
	S3Bucket         string        `yaml:"s3Bucket"`
	S3Region         string        `yaml:"s3Region,omitempty"`
	S3TrustedCAs     []string      `yaml:"s3TrustedCAs,omitempty"`
	PresignTTL       time.Duration `yaml:"presignTTL,omitempty"`
	Profiles         []*Profile    `yaml:"profiles"`
	ServerTLSConfig  *tls.Config
}

// Profile describes how a node boots. Every resource is an object key
// inside the configured S3 bucket and is handed to the node as a
// short-lived pre-signed URL.
type Profile struct {
	Selector   map[string][]string `yaml:"selector,omitempty"`
	KernelURL  string              `yaml:"kernelURL,omitempty"`
	InitrdURLs []string            `yaml:"initrdURLs,omitempty"`
	Kargs      []string            `yaml:"kargs,omitempty"`
}

func LoadConfig(path string) (*YamlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	raw := &YamlConfig{
		Listen:     "0.0.0.0:8443",
		S3Endpoint: "https://s3.amazonaws.com",
		S3Region:   "us-east-1",
		PresignTTL: 60 * time.Second,
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if len(raw.AllowedClientCNs) == 0 {
		return nil, fmt.Errorf("allowedClientCNs must include at least one commonName")
	}

	// --- listen ---

	addrPort, err := netip.ParseAddrPort(raw.Listen)
	if err != nil {
		return nil, fmt.Errorf("parse listen address and port: %w", err)
	}
	raw.Listen = fmt.Sprintf("%s:%d", addrPort.Addr(), addrPort.Port())

	// --- listen healthz ---

	if raw.ListenHealthz != "" {
		addrPort, err = netip.ParseAddrPort(raw.ListenHealthz)
		if err != nil {
			return nil, fmt.Errorf("parse listen healthz address and port: %w", err)
		}
		raw.ListenHealthz = fmt.Sprintf("%s:%d", addrPort.Addr(), addrPort.Port())
	}

	// --- server tls ---

	raw.ServerTLSConfig, err = tlsutil.BuildTLSConfig(raw.ServerCert, raw.ServerKey, raw.TrustedCAs)
	if err != nil {
		return nil, fmt.Errorf("building server TLS config: %w", err)
	}

	return raw, nil
}
