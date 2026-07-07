// Package webui serves the frontend SPA embedded in the binary (ADR-010,
// WU-006). Release builds copy frontend/dist into dist/ before compiling;
// a checkout without that copy still compiles (the embed anchors on
// dist/.gitkeep) and serves a placeholder page instead.
package webui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// placeholder is served when the binary was built without frontend/dist.
const placeholder = `<!doctype html>
<meta charset="utf-8">
<title>DB Portal</title>
<p>DB Portal backend is running, but this binary was built without the
frontend. Build the full artifact with <code>npm run build:release</code>.</p>
`

// Handler serves the embedded SPA. It is mounted as the router's NotFound
// handler, so unmatched /api paths reach it too — those stay plain 404s
// (an API miss must never get an HTML body).
func Handler() http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // unreachable: "dist" is a compile-time embed path
	}
	return handler(dist)
}

func handler(dist fs.FS) http.Handler {
	index, indexErr := fs.ReadFile(dist, "index.html")
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite content-hashes asset filenames, so they cache forever.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}

		// index.html and the SPA fallback for client-side routes: never
		// cached, so a redeploy is picked up on the next page load.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if indexErr != nil {
			_, _ = io.WriteString(w, placeholder)
			return
		}
		_, _ = w.Write(index)
	})
}
