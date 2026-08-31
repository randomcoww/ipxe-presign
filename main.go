// ipxe-presign is an mTLS iPXE boot script server.
//
// Nodes PXE-boot into iPXE, which fetches a first-stage entry script
// from this server over mTLS. The entry script chains back to the
// server's /pxe endpoint with the node's MAC address; the MAC selects
// the boot profile. The rendered script carries short-lived pre-signed
// S3 (MinIO) URLs for kernel, initrd, ignition and rootfs, so nodes
// never need long-term credentials to fetch boot-critical resources.
//
// The client certificate is used purely as an authentication gate: it
// must be signed by the configured CA (enforced at the TLS layer) and
// its commonName must be in the -client-cns allowlist.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags)

	var (
		listenAddr = flag.String("listen-address", "0.0.0.0:8443", "listen address (TLS always enabled)")
		advertise  = flag.String("advertise-url", "", "public base URL iPXE uses to reach this server, e.g. https://ipxe.internal:8443 (required; used to build the chain URL in the entry script)")
		clientCNs  = flag.String("client-cns", "", "comma-separated list of allowed client certificate commonNames, e.g. ipxe-node-1,ipxe-node-2 (required)")
		s3Endpoint = flag.String("s3-endpoint", "", "S3/MinIO endpoint, e.g. minio.internal:9000 (may be prefixed with http:// or https://)")
		s3Region   = flag.String("s3-region", "us-east-1", "S3 region used to sign pre-signed URLs (MinIO defaults to us-east-1; set it so presigning never queries the endpoint)")
		s3Bucket   = flag.String("s3-bucket", "", "bucket containing all boot resources")
		presignTTL = flag.Duration("presign-duration", 60*time.Second, "validity of pre-signed S3 URLs")
		configPath = flag.String("config", "", "path to the YAML boot profile config file")
		tlsCert    = flag.String("tls-cert", "", "path to the server TLS certificate (PEM)")
		tlsKey     = flag.String("tls-key", "", "path to the server TLS private key (PEM)")
		tlsCA      = flag.String("tls-ca", "", "path to the CA certificate (PEM) used to verify client certificates")
	)
	flag.Parse()

	if missing := missingFlags([]string{*s3Endpoint, *s3Bucket, *configPath, *tlsCert, *tlsKey, *tlsCA, *advertise, *clientCNs}); len(missing) > 0 {
		log.Fatalf("missing required flags: %v", missing)
	}
	if *presignTTL <= 0 {
		log.Fatal("-presign-duration must be greater than 0")
	}
	cns := splitAndTrim(*clientCNs)
	if len(cns) == 0 {
		log.Fatal("-client-cns must name at least one commonName")
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables must be set")
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	presigner, err := NewPresigner(*s3Endpoint, *s3Region, accessKey, secretKey, *s3Bucket, *presignTTL)
	if err != nil {
		log.Fatalf("initializing S3 presigner: %v", err)
	}

	tlsConfig, err := buildTLSConfig(*tlsCert, *tlsKey, *tlsCA)
	if err != nil {
		log.Fatalf("building TLS config: %v", err)
	}

	srv := &http.Server{
		Addr:         *listenAddr,
		Handler:      NewHandler(*advertise, cns, cfg, presigner),
		TLSConfig:    tlsConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listening on %s: %v", *listenAddr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s (mTLS, %d profiles, %d MAC assignments, %d allowed CNs)",
			*listenAddr, len(cfg.Profiles), len(cfg.MACToProfile), len(cns))
		errCh <- srv.ServeTLS(ln, "", "")
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}
}

// buildTLSConfig assembles a TLS configuration that requires and
// verifies client certificates signed by the given CA.
func buildTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading server key pair: %w", err)
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no valid certificates found in %s", caPath)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

var flagNames = []string{"-s3-endpoint", "-s3-bucket", "-config", "-tls-cert", "-tls-key", "-tls-ca", "-advertise-url", "-client-cns"}

func missingFlags(values []string) []string {
	var missing []string
	for i, v := range values {
		if v == "" {
			missing = append(missing, flagNames[i])
		}
	}
	return missing
}
