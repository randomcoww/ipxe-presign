package main

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
	"strings"
	"testing"
	"time"
)

// fakePresigner returns fixed URLs instead of contacting S3.
type fakePresigner struct {
	fail  error
	urled bool
}

func (f *fakePresigner) URL(ctx context.Context, object string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.urled = true
	return "https://minio.internal:9000/presigned/" + object, nil
}

func testPresigner() *fakePresigner { return &fakePresigner{} }

func testProfiles() map[string]Profile {
	return map[string]Profile{
		"worker-profile": {
			KernelURL:          "https://minio.internal:9000/boot-assets/fcos/vmlinuz",
			InitrdURLs:         []string{"https://minio.internal:9000/boot-assets/fcos/initramfs.img"},
			IgnitionS3Resource: "ignition/worker.ign",
			RootfsS3Resource:   "fcos/worker-rootfs.img",
			Kargs: []string{
				"console=tty0",
				"ignition.firstboot",
			},
		},
	}
}

// newTLSRequest builds an http.Request with a fake verified peer
// certificate carrying the given CN, as http.Request.TLS would hold.
func newTLSRequest(t *testing.T, cn string, path string) *http.Request {
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

	req, err := http.NewRequest(http.MethodGet, "https://localhost"+path, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
	}
	return req
}

func TestClientCommonName(t *testing.T) {
	if cn := clientCommonName(newTLSRequest(t, "worker-profile", "/")); cn != "worker-profile" {
		t.Fatalf("clientCommonName = %q, want %q", cn, "worker-profile")
	}
	noTLS, _ := http.NewRequest(http.MethodGet, "https://localhost/", nil)
	if cn := clientCommonName(noTLS); cn != "" {
		t.Fatalf("clientCommonName without TLS = %q, want empty", cn)
	}
}

func TestRenderIPXE(t *testing.T) {
	script, err := RenderIPXE(ipxeScript{
		KernelURL:   "https://minio.internal:9000/boot-assets/fcos/vmlinuz",
		InitrdURLs:  []string{"https://minio.internal:9000/boot-assets/fcos/initramfs.img"},
		Kargs:       []string{"console=tty0", "ignition.firstboot"},
		IgnitionURL: "https://minio.internal:9000/presigned/ignition/worker.ign?X-Amz-Signature=abc",
		RootfsURL:   "https://minio.internal:9000/presigned/fcos/worker-rootfs.img?X-Amz-Signature=def",
	})
	if err != nil {
		t.Fatalf("RenderIPXE: %v", err)
	}

	for _, want := range []string{
		"#!ipxe",
		"kernel https://minio.internal:9000/boot-assets/fcos/vmlinuz console=tty0 ignition.firstboot ignition.url=",
		"initrd https://minio.internal:9000/boot-assets/fcos/initramfs.img",
		"\nboot\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("rendered script missing %q\n---\n%s", want, script)
		}
	}
}

// invokeHandler runs the handler directly with a request whose r.TLS
// carries a fake verified peer certificate. This is how the handler
// sees a completed mTLS handshake without a live TLS server.
func invokeHandler(t *testing.T, h http.Handler, cn string) *httptest.ResponseRecorder {
	t.Helper()
	req := newTLSRequest(t, cn, "/")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerMatchedProfile(t *testing.T) {
	presigner := &fakePresigner{}
	h := NewHandler(testProfiles(), presigner)

	rec := invokeHandler(t, h, "worker-profile")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !presigner.urled {
		t.Fatal("expected presigner to be called for matched profile")
	}
	for _, want := range []string{
		"#!ipxe",
		"ignition.url=https://minio.internal:9000/presigned/ignition/worker.ign",
		"rootfs.url=https://minio.internal:9000/presigned/fcos/worker-rootfs.img",
		"boot",
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body missing %q\n---\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerUnknownProfileExits(t *testing.T) {
	presigner := &fakePresigner{}
	h := NewHandler(testProfiles(), presigner)

	rec := invokeHandler(t, h, "rogue-node")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != exitScript {
		t.Fatalf("body = %q, want exit script\n---\n%s", rec.Body.String(), exitScript)
	}
	if presigner.urled {
		t.Fatal("presigner must not be called for unmatched CN")
	}
}

func TestHandlerPresignFailure(t *testing.T) {
	presigner := &fakePresigner{fail: context.DeadlineExceeded}
	h := NewHandler(testProfiles(), presigner)

	rec := invokeHandler(t, h, "worker-profile")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandlerHealthz(t *testing.T) {
	srv := httptest.NewServer(NewHandler(testProfiles(), testPresigner()))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestLoadConfig(t *testing.T) {
	path := t.TempDir() + "/profiles.yaml"

	valid := `profiles:
  worker-profile:
    kernel_url: "https://minio.internal:9000/boot-assets/fcos/vmlinuz"
    initrd_urls:
      - "https://minio.internal:9000/boot-assets/fcos/initramfs.img"
    ignition_s3_resource: "ignition/worker.ign"
    rootfs_s3_resource: "fcos/worker-rootfs.img"
    kargs:
      - "console=tty0"
`
	// A profile with an empty kernel_url must reject the whole file.
	if err := os.WriteFile(path, []byte(strings.Replace(valid, "console=tty0", "console=tty0\n  bad-profile:\n    kernel_url: \"\"\n", 1)), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for profile with empty kernel_url")
	}

	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	profiles, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p, ok := profiles["worker-profile"]
	if !ok {
		t.Fatal("worker-profile missing from loaded config")
	}
	if p.KernelURL == "" || len(p.InitrdURLs) != 1 || p.IgnitionS3Resource == "" || p.RootfsS3Resource == "" || len(p.Kargs) != 1 {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestProfileValidate(t *testing.T) {
	if err := (Profile{}).Validate(); err == nil {
		t.Fatal("expected error for empty profile")
	}
	good := Profile{
		KernelURL:          "https://k",
		InitrdURLs:         []string{"https://i"},
		IgnitionS3Resource: "a.b",
		RootfsS3Resource:   "c.d",
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good profile rejected: %v", err)
	}
}

func TestBuildTLSConfigRequiresClientCert(t *testing.T) {
	dir := t.TempDir()
	if err := writeTestCA(t, dir); err != nil {
		t.Fatal(err)
	}

	cfg, err := buildTLSConfig(dir+"/server.crt", dir+"/server.key", dir+"/ca.pem")
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil || len(cfg.ClientCAs.Subjects()) != 1 {
		t.Fatal("ClientCAs pool is empty or has wrong count")
	}
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
		"ca.pem":     pemEncode("CERTIFICATE", caDER),
		"server.crt": pemEncode("CERTIFICATE", serverDER),
		"server.key": pemEncode("EC PRIVATE KEY", serverKeyDER),
	}
	for name, data := range files {
		if err := os.WriteFile(dir+"/"+name, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func pemEncode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}
