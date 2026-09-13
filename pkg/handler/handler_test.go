package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/randomcoww/ipxe-presign/pkg/config"
	"github.com/randomcoww/ipxe-presign/pkg/render"
	"github.com/randomcoww/ipxe-presign/pkg/tlsutil"
	"github.com/stretchr/testify/assert"
)

// fakePresigner returns fixed URLs instead of contacting S3.
type fakePresigner struct {
	fail   error
	called []string
}

func (f *fakePresigner) URL(ctx context.Context, object string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.called = append(f.called, object)
	return "https://minio.internal:9000/presigned/" + object, nil
}

func testConfig() *config.Config {
	return &config.Config{
		Profiles: map[string]*config.Profile{
			"aa-bb-cc-dd-ee-01": {
				KernelS3Resource:   "fcos/vmlinuz",
				InitrdS3Resources:  []string{"fcos/initramfs.img"},
				IgnitionS3Resource: "ignition/worker.ign",
				RootfsS3Resource:   "fcos/worker-rootfs.img",
				Kargs:              []string{"console=tty0", "ignition.firstboot"},
				Selector:           nil,
			},
		},
	}
}

func TestClientCommonName(t *testing.T) {
	tlsCNRequest := newTLSRequest(t, "ipxe-node-1", "/")
	assert.Equal(t, "ipxe-node-1", clientCommonName(tlsCNRequest))

	noTLSRequest, _ := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	assert.Equal(t, "", clientCommonName(noTLSRequest))
}

func TestEntryAuthorizedAndRenders(t *testing.T) {
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/boot.ipxe")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `#!ipxe
chain https://ipxe.internal:8443/ipxe?mac=${mac:hexhyp}&uuid=${uuid}
`, rec.Body.String())
}

func TestBootMatchedMAC(t *testing.T) {
	presigner := &fakePresigner{}
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), presigner)
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/ipxe?mac=aa-bb-cc-dd-ee-01&uuid=deadbeef")

	assert.Equal(t, http.StatusOK, rec.Code)
	expectedPresigned := []string{
		"fcos/vmlinuz",
		"fcos/initramfs.img",
		"ignition/worker.ign",
		"fcos/worker-rootfs.img",
	}
	assert.Equal(t, expectedPresigned, presigner.called)
	assert.Equal(t, `#!ipxe
kernel https://minio.internal:9000/presigned/fcos/vmlinuz console=tty0 ignition.firstboot ignition.config.url=https://minio.internal:9000/presigned/ignition/worker.ign coreos.live.rootfs_url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img
initrd https://minio.internal:9000/presigned/fcos/initramfs.img
boot
`, rec.Body.String())
}

func TestBootUnknownMACExits(t *testing.T) {
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	for _, target := range []string{"/ipxe?mac=11-22-33-44-55-66", "/ipxe?mac=garbage", "/ipxe"} {
		rec := invokeHandler(t, h, "ipxe-node-1", target)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, render.ExitScript, rec.Body.String())
	}
}

func TestRejectedCN(t *testing.T) {
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	for _, target := range []string{"/boot.ipxe", "/ipxe?mac=aa-bb-cc-dd-ee-01"} {
		rec := invokeHandler(t, h, "not-allowed", target)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	}
}

func TestBootPresignFailure(t *testing.T) {
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{fail: context.DeadlineExceeded})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := invokeHandler(t, h, "ipxe-node-1", "/ipxe?mac=aa-bb-cc-dd-ee-01")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHealthz(t *testing.T) {
	h, err := NewHandler("https://ipxe.internal:8443", []string{"ipxe-node-1"}, testConfig(), &fakePresigner{})
	if err != nil {
		t.Fatalf("Create handler: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBuildTLSConfigRequiresClientCert(t *testing.T) {
	dir := t.TempDir()
	if err := writeTestCA(t, dir); err != nil {
		t.Fatal(err)
	}
	tlsConfig, err := tlsutil.BuildTLSConfig(dir+"/server.crt", dir+"/server.key", dir+"/ca.pem")
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
	assert.NotNil(t, tlsConfig.ClientCAs)
}

// --- helpers ---

func writeTestCA(t *testing.T, dir string) error {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ipxe-presign-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serverTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ipxe-presign.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"ipxe-presign.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTmpl, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return err
	}

	files := map[string][]byte{
		"ca.pem":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		"server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		"server.key": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER}),
	}
	for name, data := range files {
		if err := os.WriteFile(dir+"/"+name, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// newTLSRequest builds an http.Request with a fake verified peer
// certificate carrying the given CN, as http.Request.TLS would hold.
func newTLSRequest(t *testing.T, cn string, target string) *http.Request {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://localhost"+target, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	return req
}

// invokeHandler runs the handler directly with a request whose r.TLS
// carries a fake verified peer certificate — how the handler sees a
// completed mTLS handshake without a live TLS server.
func invokeHandler(t *testing.T, h http.Handler, cn, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newTLSRequest(t, cn, target))
	return rec
}
