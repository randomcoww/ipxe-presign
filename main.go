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
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	c "github.com/randomcoww/ipxe-presign/config"
	h "github.com/randomcoww/ipxe-presign/pkg/handler"
	p "github.com/randomcoww/ipxe-presign/pkg/presigner"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	log.SetFlags(log.LstdFlags)

	var (
		configPath = flag.String("config", "", "path to the YAML boot profile config file")
	)
	flag.Parse()

	if missing := missingFlags([]string{*configPath}); len(missing) > 0 {
		log.Fatalf("missing required flags: %v", missing)
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables must be set")
	}

	cfg, err := c.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("loading config: %v", err)
	}
	presigner, err := p.NewPresignerFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("new presigner: %v", err)
	}
	handler, err := h.NewHandler(cfg.AllowedClientCNs, c.NewProfileConfig(cfg), presigner)
	if err != nil {
		return fmt.Errorf("new HTTP handler: %v", err)
	}

	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      handler,
		TLSConfig:    cfg.ServerTLSConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %v", cfg.Listen, err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.Listen)
		errCh <- srv.ServeTLS(ln, "", "")
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %v", err)
		}
	}

	return nil
}

var flagNames = []string{"-config"}

func missingFlags(values []string) []string {
	var missing []string
	for i, v := range values {
		if v == "" {
			missing = append(missing, flagNames[i])
		}
	}
	return missing
}
