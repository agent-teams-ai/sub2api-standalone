//go:build linux

package gatewaybootstrap

import (
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/gatewaytransport"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type privateHTTPPhase struct{ handler http.Handler }

// The same bounded listener starts with read-only discovery. It switches once
// enrollment succeeds; readiness is emitted only after the full handler exists.
// No partially enrolled execution transport is ever exposed through this type.
type privateStartupHandler struct {
	phase atomic.Pointer[privateHTTPPhase]
}

func (h *privateStartupHandler) install(next http.Handler) {
	h.phase.Store(&privateHTTPPhase{handler: next})
}

func (h *privateStartupHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	phase := h.phase.Load()
	if phase == nil {
		http.Error(w, "private native bootstrap pending", http.StatusServiceUnavailable)
		return
	}
	phase.handler.ServeHTTP(w, r)
}

func privateCatalogOnly(v1 service.GatewayAccountCapabilities, v2 service.GatewayAccountCapabilitiesV2, auth func(*http.Request) (gatewaytransport.Peer, error)) (http.Handler, error) {
	legacy, e1 := json.Marshal(v1)
	current, e2 := json.Marshal(v2)
	if auth == nil || e1 != nil || e2 != nil || len(legacy) > 2<<20 || len(current) > 2<<20 {
		return nil, ErrDenied
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := auth(r)
		if err != nil || peer.Role != "management" {
			http.Error(w, "private native bootstrap denied", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" ||
			r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Header.Get("Content-Encoding") != "" {
			http.Error(w, "private native bootstrap pending", http.StatusServiceUnavailable)
			return
		}
		var body []byte
		switch r.URL.Path {
		case "/private/native/v1/account-capabilities":
			body = legacy
		case "/private/native/v2/account-capabilities":
			body = current
		default:
			http.Error(w, "private native bootstrap pending", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}), nil
}
