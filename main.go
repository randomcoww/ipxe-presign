// ipxe-presign is an HTTPS iPXE boot script server.
//
// It presents an iPXE script to PXE-booting nodes over mTLS. The client
// certificate's commonName selects a boot profile; the rendered script
// contains kernel/initrd URLs from the profile plus short-lived
// pre-signed S3 (MinIO) URLs for the ignition and rootfs objects, so
// nodes never need long-term credentials to fetch sensitive resources.
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
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags)

	var (
		listenAddr = flag.String("listen-address", "0.0.0.0:8443", "listen address (TLS always enabled)")
		s3Endpoint = flag.String("s3-endpoint", "", "S3/MinIO endpoint, e.g. minio.internal:9000 (may be prefixed with http:// or https://)")
		s3Region   = flag.String("s3-region", "us-east-1", "S3 region used to sign pre-signed URLs (MinIO defaults to us-east-1; set it so presigning never queries the endpoint)")
		s3Bucket   = flag.String("s3-bucket", "", "bucket containing the ignition and rootfs resources")
		presignTTL = flag.Duration("presign-duration", 60*time.Second, "validity of pre-signed S3 URLs")
		configPath = flag.String("config", "", "path to the YAML boot profile config")
		tlsCert    = flag.String("tls-cert", "", "path to the server TLS certificate (PEM)")
		tlsKey     = flag.String("tls-key", "", "path to the server TLS private key (PEM)")
		tlsCA      = flag.String("tls-ca", "", "path to the CA certificate (PEM) used to verify client certificates")
	)
	flag.Parse()

	if missing := missingFlags([]string{*s3Endpoint, *s3Bucket, *configPath, *tlsCert, *tlsKey, *tlsCA}); len(missing) > 0 {
		log.Fatalf("missing required flags: %v", missing)
	}
	if *presignTTL <= 0 {
		log.Fatal("-presign-duration must be greater than 0")
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		log.Fatal("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables must be set")
	}

	profiles, err := LoadConfig(*configPath)
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
		Handler:      NewHandler(profiles, presigner),
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
		log.Printf("listening on %s (mTLS, %d profiles)", *listenAddr, len(profiles))
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

// buildTLSConfig assembles a TLS configuration that requires and verifies
// client certificates signed by the given CA.
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

func missingFlags(values []string) []string {
	var missing []string
	names := []string{"-s3-endpoint", "-s3-bucket", "-config", "-tls-cert", "-tls-key", "-tls-ca"}
	for i, v := range values {
		if v == "" {
			missing = append(missing, names[i])
		}
	}
	return missing
}
