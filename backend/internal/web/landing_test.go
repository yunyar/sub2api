//go:build unit

package web

import (
	"net/http"
	"testing"
)

func TestIsLandingRequest(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/", true},
		{http.MethodHead, "/", true},
		{http.MethodGet, "/home", true},
		{http.MethodGet, "/home/", true},
		{http.MethodPost, "/", false},
		{http.MethodGet, "/login", false},
		{http.MethodGet, "/dashboard", false},
		{http.MethodGet, "/homepage", false},
		{http.MethodGet, "/home/extra", false},
		{http.MethodGet, "/landing.html", false},
		{http.MethodGet, "/api/v1/settings/public", false},
	}
	for _, tc := range cases {
		if got := isLandingRequest(tc.method, tc.path); got != tc.want {
			t.Errorf("isLandingRequest(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
