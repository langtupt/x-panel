package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "dữ liệu gửi lên không hợp lệ")
		return false
	}
	return true
}

// clientIP trusts X-Real-IP only when the request comes from the local nginx.
func clientIP(r *http.Request) string {
	// Only nginx sets X-Real-IP: in production it is the only client of the
	// API's unix socket (empty or "@" remote address).
	if r.RemoteAddr == "" || r.RemoteAddr == "@" {
		if real := r.Header.Get("X-Real-IP"); net.ParseIP(real) != nil {
			return real
		}
		return "unix"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if real := r.Header.Get("X-Real-IP"); net.ParseIP(real) != nil {
			return real
		}
	}
	return host
}
