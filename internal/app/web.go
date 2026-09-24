// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/iencodev/live-subtitles/internal/api/handlers"
)

const placeholder = `<!doctype html><meta charset="utf-8"><title>Live Subtitles</title>
<p>The web app isn't built yet. Run <code>task dev</code> for development, or <code>task build</code>.</p>
`

// spa serves the built web app. Unknown paths get index.html so client-side
// routes work on reload; unknown /api/ and /ws/ paths get a JSON 404.
func spa(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") {
			handlers.WriteError(w, http.StatusNotFound, "route.not_found", "no such endpoint")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			handlers.WriteError(w, http.StatusMethodNotAllowed, "method.not_allowed", "method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean(p), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(dist, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") { // Vite output, content-hashed
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if errors.Is(err, fs.ErrNotExist) {
			index = []byte(placeholder)
		} else if err != nil {
			http.Error(w, "read index.html", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
