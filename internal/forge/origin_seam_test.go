package forge

import (
	"net/url"
	"testing"
)

// TestSameOriginSeam pins the origin rules a token depends on; each pair is stated, not derived from originOf.
func TestSameOriginSeam(t *testing.T) {
	parse := func(s string) *url.URL {
		t.Helper()
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://h/x", "https://h:443/y", true},
		{"http://h", "http://h:80", true},
		{"https://H", "https://h", true},
		{"https://h", "http://h", false},
		{"https://h", "https://h:8443", false},
		{"https://h", "https://other", false},
		{"https://a.h", "https://h", false},
		{"custom://h", "custom://h:1", false},
		{"/a", "/a", false},
		{"", "", false},
	}
	for _, tt := range tests {
		a, b := parse(tt.a), parse(tt.b)
		if got := SameOrigin(a, b); got != tt.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		if got := SameOrigin(b, a); got != tt.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v (reversed)", tt.b, tt.a, got, tt.want)
		}
	}
	if SameOrigin(nil, parse("https://h")) || SameOrigin(parse("https://h"), nil) {
		t.Error("a nil URL is the same origin as something")
	}
}
