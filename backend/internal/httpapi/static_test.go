package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticCachingAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	for path, content := range map[string]string{
		"index.html": "<html>Vocabulary Live</html>", "assets/app-12345678.js": "export {}", "theme.js": "// theme", ".env": "PRIVATE",
	} {
		file := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{config: Config{StaticDir: dir}}
	for _, tc := range []struct {
		path   string
		status int
		cache  string
	}{
		{"/", 200, "no-cache"}, {"/instructor", 200, "no-cache"}, {"/theme.js", 200, "no-cache"},
		{"/assets/app-12345678.js", 200, "public, max-age=31536000, immutable"},
		{"/assets/missing-12345678.js", 404, "no-cache"}, {"/fonts/missing.woff2", 404, "no-cache"},
		{"/unknown", 404, "no-cache"}, {"/.env", 404, "no-cache"}, {"/assets/", 404, "no-cache"},
		{"/assets/../.env", 404, "no-cache"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			s.static(response, httptest.NewRequest("GET", tc.path, nil))
			if response.Code != tc.status || response.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("status %d, cache %q", response.Code, response.Header().Get("Cache-Control"))
			}
			if strings.Contains(response.Body.String(), "PRIVATE") || (tc.status == 404 && strings.Contains(response.Body.String(), "<html>")) {
				t.Fatal("private file or SPA returned for a missing asset")
			}
		})
	}
	r := httptest.NewRequest("GET", "/assets/app-12345678.js", nil)
	r.Header.Set("If-Modified-Since", "Wed, 01 Jan 2098 00:00:00 GMT")
	response := httptest.NewRecorder()
	s.static(response, r)
	if response.Code != 304 || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("conditional asset caching failed")
	}
}
