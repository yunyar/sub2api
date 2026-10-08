//go:build unit

package ip

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTrustedPublicClientIPUsesVerifiedPeerOrProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"172.21.0.1/32", "8.8.4.4/32"}))
	r.GET("/t", func(c *gin.Context) {
		SetTrustedProxySnapshot(c, []string{"172.21.0.1/32", "8.8.4.4/32"})
		c.String(200, GetTrustedPublicClientIP(c))
	})

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{
			name:       "direct peer wins over spoofed forwarding headers",
			remoteAddr: "8.8.8.8:12345",
			xff:        "1.1.1.1",
			want:       "8.8.8.8",
		},
		{
			name:       "trusted proxy chain selects the nearest untrusted client",
			remoteAddr: "172.21.0.1:12345",
			xff:        "1.1.1.1, 8.8.8.8",
			want:       "8.8.8.8",
		},
		{
			name:       "trusted proxy chain does not fall back to spoofed public prefix for private client",
			remoteAddr: "172.21.0.1:12345",
			xff:        "8.8.8.8, 10.1.2.3",
			want:       "",
		},
		{
			name:       "trusted proxy address is not recorded without client chain",
			remoteAddr: "8.8.4.4:12345",
			want:       "",
		},
		{
			name:       "trusted proxy chain with no forwarded data cannot record private peer",
			remoteAddr: "172.21.0.1:12345",
			xff:        "",
			want:       "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = test.remoteAddr
			req.Header.Set("X-Forwarded-For", test.xff)
			r.ServeHTTP(recorder, req)
			require.Equal(t, test.want, recorder.Body.String())
		})
	}
}

func TestGetTrustedClientIPUsesGinClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))

	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetTrustedClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetTrustedClientIPRejectsSpoofedHeadersAndResolvesTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"172.21.0.1/32"}))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, []string{"X-Client-IP"})
		c.String(200, GetTrustedClientIP(c))
	})

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xrealIP    string
		customIP   string
		want       string
	}{
		{
			name:       "untrusted direct peer cannot spoof forwarding headers",
			remoteAddr: "9.9.9.9:12345",
			xff:        "1.2.3.4",
			xrealIP:    "5.6.7.8",
			customIP:   "8.8.8.8",
			want:       "9.9.9.9",
		},
		{
			name:       "trusted proxy chain ignores client-supplied leftmost value",
			remoteAddr: "172.21.0.1:12345",
			xff:        "198.51.100.99, 203.0.113.42",
			xrealIP:    "192.0.2.55",
			customIP:   "192.0.2.56",
			want:       "203.0.113.42",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = test.remoteAddr
			req.Header.Set("X-Forwarded-For", test.xff)
			req.Header.Set("X-Real-IP", test.xrealIP)
			req.Header.Set("X-Client-IP", test.customIP)
			r.ServeHTTP(recorder, req)

			require.Equal(t, 200, recorder.Code)
			require.Equal(t, test.want, recorder.Body.String())
		})
	}
}

func TestGetClientIPPreservesLegacyDockerForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.2, 203.0.113.42")
	req.Header.Set("X-Real-IP", "192.168.32.1")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "203.0.113.42", w.Body.String())
}

func TestCheckIPRestrictionWithCompiledRules(t *testing.T) {
	whitelist := CompileIPRules([]string{"10.0.0.0/8", "192.168.1.2"})
	blacklist := CompileIPRules([]string{"10.1.1.1"})

	allowed, reason := CheckIPRestrictionWithCompiledRules("10.2.3.4", whitelist, blacklist)
	require.True(t, allowed)
	require.Equal(t, "", reason)

	allowed, reason = CheckIPRestrictionWithCompiledRules("10.1.1.1", whitelist, blacklist)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestCheckIPRestrictionWithCompiledRules_InvalidWhitelistStillDenies(t *testing.T) {
	// 与旧实现保持一致：白名单有配置但全无效时，最终应拒绝访问。
	invalidWhitelist := CompileIPRules([]string{"not-a-valid-pattern"})
	allowed, reason := CheckIPRestrictionWithCompiledRules("8.8.8.8", invalidWhitelist, nil)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestGetSecurityClientIPSwitchEnabledUsesLegacyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPCustomHeaderPrecedenceAndFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		trustForward   bool
		headers        []string
		requestHeaders map[string]string
		want           string
	}{
		{
			name:         "configured order precedes built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":      "198.51.100.10",
				"X-CDN-Second":     "203.0.113.20",
				"CF-Connecting-IP": "8.8.8.8",
			},
			want: "198.51.100.10",
		},
		{
			name:         "comma candidates skip invalid and private values",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":  "not-an-ip, 10.0.0.8",
				"X-CDN-Second": "also-bad, 203.0.113.9",
			},
			want: "203.0.113.9",
		},
		{
			name:         "legacy public header wins over custom private fallback",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "1.2.3.4",
			},
			want: "1.2.3.4",
		},
		{
			name:         "custom private fallback retains configured precedence",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "192.168.1.4",
			},
			want: "10.0.0.8",
		},
		{
			name:         "invalid custom value continues to built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":         "1.2.3.4:443",
				"CF-Connecting-IP": "4.4.4.4",
			},
			want: "4.4.4.4",
		},
		{
			name:         "invalid legacy values continue to a valid forwarded address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid, 203.0.113.50",
			},
			want: "203.0.113.50",
		},
		{
			name:         "all invalid legacy values fall back to the connection address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid",
			},
			want: "9.9.9.9",
		},
		{
			name:         "disabled mode ignores custom and legacy headers",
			trustForward: false,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "1.2.3.4",
				"X-Real-IP": "4.4.4.4",
			},
			want: "9.9.9.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, test.trustForward, test.headers)
				c.String(200, GetSecurityClientIP(c, !test.trustForward))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			for name, value := range test.requestHeaders {
				req.Header.Set(name, value)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

func TestGetSecurityClientIPSwitchDisabledUsesConfiguredTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"9.9.9.9"}))
	r.GET("/t", func(c *gin.Context) { c.String(200, GetSecurityClientIP(c, false)) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetClientIPSwitchDisabledUsesTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotCopiesCustomHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		headers := []string{"X-Original-IP"}
		SetForwardedIPSettings(c, true, headers)
		headers[0] = "X-Mutated-IP"
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Original-IP", "1.2.3.4")
	req.Header.Set("X-Mutated-IP", "4.4.4.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotOverridesLiveFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		requestTrust  bool
		fallbackTrust bool
		want          string
	}{
		{name: "captured secure mode wins", requestTrust: false, fallbackTrust: true, want: "9.9.9.9"},
		{name: "captured compatibility mode wins", requestTrust: true, fallbackTrust: false, want: "1.2.3.4"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetLegacyForwardedIPTrust(c, test.requestTrust)
				c.String(200, GetSecurityClientIP(c, test.fallbackTrust))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			req.Header.Set("X-Real-IP", "1.2.3.4")
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}
