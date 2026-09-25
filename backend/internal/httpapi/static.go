package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var fingerprintedAsset = regexp.MustCompile(`^/assets/[A-Za-z0-9_-]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9.]+$`)

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	path := r.URL.Path
	if path == "/" || path == "/instructor" {
		http.ServeFile(w, r, filepath.Join(s.config.StaticDir, "index.html"))
		return
	}
	if strings.Contains(path, "\\") || strings.Contains(path, "\x00") {
		http.NotFound(w, r)
		return
	}
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ".") {
			http.NotFound(w, r)
			return
		}
	}
	file := filepath.Join(s.config.StaticDir, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if fingerprintedAsset.MatchString(path) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, file)
}
