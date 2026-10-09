package forge

import (
	"net/url"
	"strings"
)

// origin is a URL's scheme, hostname and port, lowercased, with the scheme's default port filled in.
type origin struct{ scheme, host, port string }

// defaultPorts lists the schemes whose URLs imply a port; any other scheme with no port implies none.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// SameOrigin reports whether a and b have the same scheme, hostname and port; a missing port means the scheme's default.
// It is false when either URL is nil or lacks a scheme or host, so two relative URLs are never the same origin.
func SameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || a.Scheme == "" || b.Scheme == "" || a.Host == "" || b.Host == "" {
		return false
	}
	return originOf(a) == originOf(b)
}

func originOf(u *url.URL) origin {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		port = defaultPorts[scheme]
	}
	return origin{scheme: scheme, host: strings.ToLower(u.Hostname()), port: port}
}
