package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomcoww/ipxe-presign/pkg/tlstest"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	tlsDir := t.TempDir()
	if err := tlstest.WriteTestCA(t, tlsDir); err != nil {
		t.Fatal(err)
	}

	rawYaml := fmt.Sprintf(`
serverCert: "%s"
serverKey: "%s"
trustedCAs:
- "%s"
allowedClientCNs:
- "ipxe-node-1"
s3Endpoint: "https://minio.local:9000"
s3Bucket: "boot"
s3TrustedCAs:
- "%s"
`, tlsDir+"/server.crt", tlsDir+"/server.key", tlsDir+"/ca.pem", tlsDir+"/ca.pem")

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(rawYaml), 0644)
	if err != nil {
		t.Fatalf("Create test config: %v", err)
	}

	_, err = LoadConfig(filepath.Join(dir, "config.yaml"))
	assert.NoError(t, err)
}
