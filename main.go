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
  "regexp"
  "syscall"
  "time"

  c "github.com/randomcoww/ipxe-presign/pkg/config"
  h "github.com/randomcoww/ipxe-presign/pkg/handler"
  ps "github.com/randomcoww/ipxe-presign/pkg/presigner"
  "github.com/randomcoww/ipxe-presign/pkg/tlsutil"
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
  reList := regexp.MustCompile(`\s*,\s*`)

  var (
    listenAddr  = flag.String("listen-address", "0.0.0.0:8443", "listen address (TLS always enabled)")
    advertise   = flag.String("advertise-url", "", "public base URL iPXE uses to reach this server, e.g. https://ipxe.internal:8443 (required; used to build the chain URL in the entry script)")
    clientCNs   = flag.String("client-cns", "", "comma-separated list of allowed client certificate commonNames, e.g. ipxe-node-1,ipxe-node-2 (required)")
    s3Endpoint  = flag.String("s3-endpoint", "", "S3/MinIO endpoint, e.g. minio.internal:9000 (may be prefixed with http:// or https://)")
    s3Region    = flag.String("s3-region", "us-east-1", "S3 region used to sign pre-signed URLs (MinIO defaults to us-east-1; set it so presigning never queries the endpoint)")
    s3Bucket    = flag.String("s3-bucket", "", "bucket containing all boot resources")
    s3TrustedCA = flag.String("s3-trusted-ca", "", "path to the CA certificate (PEM) used to verify self hosted S3")
    presignTTL  = flag.Duration("presign-duration", 60*time.Second, "validity of pre-signed S3 URLs")
    configPath  = flag.String("config", "", "path to the YAML boot profile config file")
    serverCert  = flag.String("server-cert", "", "path to the server TLS certificate (PEM)")
    serverKey   = flag.String("server-key", "", "path to the server TLS private key (PEM)")
    trustedCA   = flag.String("trusted-ca", "", "path to the CA certificate (PEM) used to verify client certificates")
  )
  flag.Parse()

  if missing := missingFlags([]string{*s3Endpoint, *s3Bucket, *configPath, *serverCert, *serverKey, *trustedCA, *advertise, *clientCNs}); len(missing) > 0 {
    log.Fatalf("missing required flags: %v", missing)
  }
  if *presignTTL <= 0 {
    return fmt.Errorf("-presign-duration must be greater than 0")
  }
  cns := reList.Split(*clientCNs, -1)
  if len(cns) == 0 {
    return fmt.Errorf("-client-cns must name at least one commonName")
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

  s3TLSConfig, err := tlsutil.BuildTLSCAConfig(*s3TrustedCA)
  if err != nil {
    return fmt.Errorf("building S3 TLS config: %v", err)
  }

  presigner, err := ps.NewPresigner(*s3Endpoint, *s3Region, *s3Bucket, s3TLSConfig, *presignTTL)
  if err != nil {
    return fmt.Errorf("initializing S3 presigner: %v", err)
  }

  serverTLSConfig, err := tlsutil.BuildTLSConfig(*serverCert, *serverKey, *trustedCA)
  if err != nil {
    return fmt.Errorf("building TLS config: %v", err)
  }

  handler, err := h.NewHandler(*advertise, cns, cfg, presigner)
  if err != nil {
    return fmt.Errorf("new HTTP handler: %v", err)
  }

  srv := &http.Server{
    Addr:         *listenAddr,
    Handler:      handler,
    TLSConfig:    serverTLSConfig,
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 10 * time.Second,
  }

  ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
  defer stop()

  ln, err := net.Listen("tcp", *listenAddr)
  if err != nil {
    return fmt.Errorf("listening on %s: %v", *listenAddr, err)
  }

  errCh := make(chan error, 1)
  go func() {
    log.Printf("listening on %s", *listenAddr)
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

var flagNames = []string{"-s3-endpoint", "-s3-bucket", "-config", "-server-cert", "-server-key", "-trusted-ca", "-advertise-url", "-client-cns"}

func missingFlags(values []string) []string {
  var missing []string
  for i, v := range values {
    if v == "" {
      missing = append(missing, flagNames[i])
    }
  }
  return missing
}
