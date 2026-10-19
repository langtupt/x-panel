package httpapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticHandler serves the Next.js static export (trailingSlash: true), so
// /users resolves to users/index.html. Unknown paths get 404.html.
func staticHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		full := filepath.Join(dir, filepath.FromSlash(p))
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			if _, err := os.Stat(filepath.Join(full, "index.html")); err == nil {
				if !strings.HasSuffix(r.URL.Path, "/") {
					http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
					return
				}
				files.ServeHTTP(w, r)
				return
			}
		} else if err == nil {
			if strings.HasPrefix(p, "/_next/static/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if b, err := os.ReadFile(filepath.Join(dir, "404.html")); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	})
}
