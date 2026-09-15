package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/randomcoww/ipxe-presign/pkg/profile"
	"github.com/randomcoww/ipxe-presign/pkg/render"
)

// urlPresigner issues short-lived URLs for S3 objects. *Presigner
// satisfies it; tests use a fake.
type URLPresigner interface {
	URL(ctx context.Context, object string) (string, error)
}

// Handler serves the two-stage iPXE flow:
//
//	GET /boot.ipxe first-stage entry script (static template, chains to /ipxe)
//	GET /ipxe      second-stage boot script, selected by the ?mac= query
//	GET /healthz
//
// Every request must present a client certificate signed by the
// configured CA (enforced at the TLS layer) whose commonName is in the
// allowlist (enforced here). Profile selection is by MAC address only.
type Handler struct {
	Render           *render.Config
	Profiles         *profile.Config
	Presigner        URLPresigner
	allowedClientCNs map[string]struct{}
}

// NewHandler builds a Handler. advertiseURL is the public base URL
// (scheme + host[:port]) that iPXE uses to reach this server.
func NewHandler(r *render.Config, allowedClientCNs []string, p *profile.Config, pr URLPresigner) (*Handler, error) {
	h := &Handler{
		Render:           r,
		Profiles:         p,
		Presigner:        pr,
		allowedClientCNs: make(map[string]struct{}, len(allowedClientCNs)),
	}
	for _, cn := range allowedClientCNs {
		h.allowedClientCNs[cn] = struct{}{}
	}
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	case "/boot.ipxe":
		h.serveEntry(w, r)
	case "/ipxe":
		h.serveBoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveEntry renders the first-stage script. It carries no node
// identity, so only the CN allowlist applies.
func (h *Handler) serveEntry(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, h.Render.ChainIPXEScript)
}

// serveBoot selects the profile by MAC address and renders the
// second-stage script with presigned resource URLs.
func (h *Handler) serveBoot(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	mac := strings.ToLower(r.URL.Query().Get("mac"))
	profile, ok := h.Profiles.Profiles[mac]
	if !ok {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, h.Render.ExitIPXEScript)
		return
	}

	ipxe := &render.BootIPXE{
		Kargs: profile.Kargs,
	}
	if err := h.appendPresignedURLs(r.Context(), profile, ipxe); err != nil {
		log.Printf("presigning resources for profile: %v", err)
		http.Error(w, "failed to generate resource URLs", http.StatusInternalServerError)
		return
	}

	script, err := h.Render.RenderBootIPXE(ipxe)
	if err != nil {
		log.Printf("rendering boot script for profile: %v", err)
		http.Error(w, "failed to render boot script", http.StatusInternalServerError)
		return
	}

	log.Printf("served boot script for profile (MAC %s)", mac)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
}

func (h *Handler) appendPresignedURLs(ctx context.Context, p *profile.Profile, ipxe *render.BootIPXE) error {
	var err error
	ipxe.KernelURL, err = h.Presigner.URL(ctx, p.Kernel)
	if err != nil {
		return err
	}
	for _, res := range p.Initrds {
		u, err := h.Presigner.URL(ctx, res)
		if err != nil {
			return err
		}
		ipxe.InitrdURLs = append(ipxe.InitrdURLs, u)
	}
	ipxe.IgnitionURL, err = h.Presigner.URL(ctx, p.Ignition)
	if err != nil {
		return err
	}
	ipxe.RootfsURL, err = h.Presigner.URL(ctx, p.Rootfs)
	if err != nil {
		return err
	}
	return nil
}

// authorize enforces that the verified client certificate's commonName
// is on the allowlist. It writes the error response and returns false
// if the CN is unknown (including the absence of any peer certificate).
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	cn := clientCommonName(r)
	if _, ok := h.allowedClientCNs[cn]; !ok {
		log.Printf("rejected client with CN %q: not an allowed iPXE client", cn)
		http.Error(w, "unauthorized client", http.StatusForbidden)
		return false
	}
	return true
}

// clientCommonName extracts the commonName from the verified client
// certificate. It is empty when no peer certificate is present, which
// never happens under RequireAndVerifyClientCert (the handshake fails
// first) but keeps the function safe for tests.
func clientCommonName(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
}
