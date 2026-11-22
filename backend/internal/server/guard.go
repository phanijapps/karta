package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// guardMutations hardens the unauthenticated loopback daemon against
// cross-origin and DNS-rebound abuse. The MCP surface makes this change's
// stakes concrete: list/detail GETs carry authored env values and the
// probe endpoint spawns configured commands, so EVERY request (reads
// included) must present a trusted Host — the configured loopback server
// address or a loopback/localhost literal. A rebound hostname
// (attacker.example resolving to 127.0.0.1) fails this check and never
// reaches a handler. Rules:
//
//   - Host must be a loopback IP, `localhost`, or absent (HTTP/1.0-style
//     clients, tests).
//   - Mutating methods (POST/PATCH/DELETE) additionally require a JSON
//     content type (kills the form-post vector; DELETE carries no body)
//     and, when the browser supplies an Origin, that origin must be
//     loopback or the request's own host.
//
// exposedBind reports an operator-selected non-loopback bind: Host-header
// enforcement is skipped (the operator owns that trust decision) and the
// Origin rule compares hosts port-insensitively so a LAN frontend on a
// different port passes.
var exposedBind bool

func guardMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exposedBind && !trustedHost(r.Host) {
			writeError(w, http.StatusForbidden, "host_forbidden", "requests must target the daemon's loopback address")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// The archive-import preview is the ONE multipart exemption:
			// trusted-Host (above) plus Origin enforcement replaces the
			// JSON-type cross-site barrier on this route only.
			multipart := r.URL.Path == "/api/skills/import/preview" &&
				strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
			if r.Method != http.MethodDelete && !multipart {
				ct := strings.ToLower(r.Header.Get("Content-Type"))
				if !strings.HasPrefix(ct, "application/json") {
					writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "mutations require a JSON content type")
					return
				}
			}
			if origin := r.Header.Get("Origin"); origin != "" && !originAllowedFor(origin, r.Host) {
				writeError(w, http.StatusForbidden, "origin_forbidden", "cross-origin mutations are not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// trustedHost reports whether the request's Host is a loopback address
// or localhost (port optional). Absent Host (HTTP/1.0-style) passes.
func trustedHost(hostPort string) bool {
	if hostPort == "" {
		return true
	}
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// originAllowed accepts loopback origins and same-host origins (the dev
// proxy forwards same-origin requests; Origin host equals the request
// Host there). Port-insensitive comparison lets a LAN frontend on a
// different port reach an exposed daemon.
func originAllowed(origin, requestHost string) bool {
	host := originHost(origin)
	if host == "" {
		return false
	}
	if sameHostNoPort(host, requestHost) {
		return true
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func originHost(origin string) string {
	if i := strings.Index(origin, "://"); i >= 0 {
		return origin[i+3:]
	}
	return ""
}

var _ = fmt.Sprintf

// originAllowedFor is the entry the guard uses: identical to
// originAllowed (hosts compared port-insensitively).
func originAllowedFor(origin, requestHost string) bool {
	return originAllowed(origin, requestHost)
}

// sameHostNoPort compares two host[:port] strings ignoring ports.
func sameHostNoPort(a, b string) bool {
	strip := func(h string) string {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return h
	}
	return strip(a) == strip(b)
}

// SetExposedBind marks the daemon as bound to a non-loopback address
// (operator choice from config); the guard adapts as documented above.
func SetExposedBind(exposed bool) { exposedBind = exposed }
