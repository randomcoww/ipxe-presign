package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/randomcoww/ipxe-presign/config"
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
	advertiseURL     string
	Profiles         *config.Profiles
	Presigner        URLPresigner
	allowedClientCNs map[string]struct{}
}

func NewHandler(allowedClientCNs []string, p *config.Profiles, pr URLPresigner) (*Handler, error) {
	h := &Handler{
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
	case "/boot.ipxe":
		h.serveChain(w, r)
	case "/ipxe":
		h.serveBoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveEntry renders the first-stage script. It carries no node
// identity, so only the CN allowlist applies.
func (h *Handler) serveChain(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ipxeChain)
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

	selector := make(map[string]string)
	for k, v := range r.URL.Query() {
		selector[k] = v[len(v)-1]
	}
	profile, ok := h.Profiles.GetMerged(selector)
	if !ok {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, ipxeExit)
		return
	}
	script, err := h.renderIPXETemplate(r.Context(), profile, selector)
	if err != nil {
		log.Printf("presigning resources for profile: %v", err)
		http.Error(w, "failed to generate resource URLs", http.StatusInternalServerError)
		return
	}
	log.Printf("served boot script for profile (Selector %v)", selector)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, script)
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
