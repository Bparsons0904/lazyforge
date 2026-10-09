package forge_test

import (
	"net/url"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func parseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name string
		a, b *url.URL
		want bool
	}{
		{"default https port is the same origin", parseURL(t, "https://h/x"), parseURL(t, "https://h:443/y"), true},
		{"default http port is the same origin", parseURL(t, "http://h"), parseURL(t, "http://h:80"), true},
		{"hostname case is ignored", parseURL(t, "https://H"), parseURL(t, "https://h"), true},
		{"scheme case is ignored", &url.URL{Scheme: "HTTPS", Host: "h"}, parseURL(t, "https://h"), true},
		{"same explicit port matches", parseURL(t, "https://h:8443/a"), parseURL(t, "https://h:8443/b"), true},
		{"different scheme", parseURL(t, "http://h"), parseURL(t, "https://h"), false},
		{"different port", parseURL(t, "https://h"), parseURL(t, "https://h:8443"), false},
		{"different hostname", parseURL(t, "https://a"), parseURL(t, "https://b"), false},
		{"subdomain is a different origin", parseURL(t, "https://a.h"), parseURL(t, "https://h"), false},
		{"other scheme without a port never matches one with a port", parseURL(t, "ftp://h"), parseURL(t, "ftp://h:21"), false},
		{"nil first argument", nil, parseURL(t, "https://h"), false},
		{"nil second argument", parseURL(t, "https://h"), nil, false},
		{"two empty URLs", &url.URL{}, &url.URL{}, false},
		{"two relative URLs", parseURL(t, "/x"), parseURL(t, "/y"), false},
		{"scheme-relative URLs have no scheme", parseURL(t, "//h/x"), parseURL(t, "//h/y"), false},
		{"empty scheme with a host", &url.URL{Host: "h"}, &url.URL{Host: "h"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := forge.SameOrigin(tt.a, tt.b); got != tt.want {
				t.Errorf("SameOrigin(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
