//go:build embed || unit

package web

import (
	"net/http"
	"strings"
)

// landingHTMLFile is the optional standalone landing page bundled in the
// frontend dist (frontend/public/landing.html). When present it replaces the
// SPA home view for "/" and "/home"; every other route keeps serving the SPA.
const landingHTMLFile = "landing.html"

// isLandingRequest reports whether a request should receive the landing page.
func isLandingRequest(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	switch strings.TrimSpace(path) {
	case "/", "/home", "/home/":
		return true
	default:
		return false
	}
}
