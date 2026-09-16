package proxy

import (
	"net/http"
	"strings"
)

// copyRequestHeaders forwards client headers except credentials, hop-by-hop
// headers, and proxy or Cloudflare metadata.
func copyRequestHeaders(destination, source http.Header) {
	for key, values := range source {
		if isProxyInternalHeader(key) {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func isProxyInternalHeader(key string) bool {
	canonical := http.CanonicalHeaderKey(key)
	return isHopByHopHeader(canonical) || canonical == "Authorization" || canonical == "X-Api-Key" ||
		canonical == "Host" || canonical == "Content-Length" || canonical == "Accept-Encoding" ||
		canonical == "Forwarded" || canonical == "Cdn-Loop" || canonical == "X-Real-Ip" ||
		strings.HasPrefix(canonical, "Cf-") || strings.HasPrefix(canonical, "X-Forwarded-")
}

func copyResponseHeaders(destination, source http.Header) {
	for key, values := range source {
		if isHopByHopHeader(key) || http.CanonicalHeaderKey(key) == "Content-Length" {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}
