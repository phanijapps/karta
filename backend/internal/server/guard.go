package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// guardMutations hardens the unauthenticated loopback daemon against
// cross-origin abuse: browsers can fire "simple" cross-site requests
// (form posts with text/plain) at localhost even though the daemon binds
// loopback only. A hostile page could otherwise create a provider whose
// credential reference points at a real secret and whose base URL points
// at the attacker — exfiltrating the credential on the next discovery.
//
// Rules: mutating methods (POST/PATCH/DELETE) require a JSON content type
// (kills the form-post vector; DELETE carries no body and is exempt) and,
// when the browser supplies an Origin, that origin must be loopback or
// the request's own host. Absent Origin (curl, CLI clients) is allowed.
func guardMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Method != http.MethodDelete {
				ct := strings.ToLower(r.Header.Get("Content-Type"))
				if !strings.HasPrefix(ct, "application/json") {
					writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "mutations require a JSON content type")
					return
				}
			}
			if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host) {
				writeError(w, http.StatusForbidden, "origin_forbidden", "cross-origin mutations are not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed accepts loopback origins and same-host origins (the dev
// proxy forwards same-origin requests; Origin host equals the request
// Host there).
func originAllowed(origin, requestHost string) bool {
	host := originHost(origin)
	if host == "" {
		return false
	}
	if host == requestHost {
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
