package mcpserver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// readBody reads the whole request body before the MCP handler sees it,
// under a read deadline. Doing this in the handler rather than with
// http.Server.ReadTimeout keeps the protection wherever Handler is mounted
// and answers a stalled body with a 408. A fully read body clears the
// deadline so it never bounds the tool call. A rejected body keeps it,
// because net/http still drains the unread body after the handler returns.
func readBody(maxBytes int64, timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		controller := http.NewResponseController(w)
		deadlineSet := controller.SetReadDeadline(time.Now().Add(timeout)) == nil
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				reject(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body exceeds %d bytes", tooLarge.Limit))
			case isTimeout(err):
				reject(w, http.StatusRequestTimeout, "request body not received in time")
			default:
				reject(w, http.StatusBadRequest, "failed to read request body")
			}
			return
		}
		if deadlineSet {
			_ = controller.SetReadDeadline(time.Time{})
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// reject answers with an error and closes the connection after it, so the
// rest of the body is never parsed as another request. Callers keep a read
// deadline on the connection to bound net/http's drain of that body.
func reject(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Connection", "close")
	http.Error(w, message, status)
}

func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// guardHost rejects a request that reached a loopback listener with a Host
// header that is neither loopback nor allowed. That blocks DNS rebinding as
// the SDK's own check does, while letting a reverse proxy on this machine
// forward its public name. A rejected request's unread body is drained
// under bodyTimeout at most.
func guardHost(allowed []string, bodyTimeout time.Duration, next http.Handler) http.Handler {
	allowedHosts := make(map[string]bool, len(allowed))
	for _, host := range allowed {
		allowedHosts[strings.ToLower(hostname(strings.TrimSpace(host)))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		if ok && local != nil && isLoopback(local.String()) &&
			!isLoopback(r.Host) && !allowedHosts[strings.ToLower(hostname(r.Host))] {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyTimeout))
			reject(w, http.StatusForbidden, fmt.Sprintf("Forbidden: invalid Host header %q", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostname strips the port and IPv6 brackets from a host or host:port.
func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

func isLoopback(hostport string) bool {
	host := hostname(hostport)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
