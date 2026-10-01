// Package webui serves the compiled web client from inside the binary, so a
// single executable is the whole application. Assets are copied into dist/ by
// `make app` (or scripts/build.ps1); without them the server still runs and
// explains how to build the UI.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Assets is the dist directory, rooted so paths look like "index.html".
func Assets() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return embedded
	}
	return sub
}

// Available reports whether a real UI build was embedded.
func Available() bool {
	_, err := fs.Stat(Assets(), "index.html")
	return err == nil
}

// Handler serves the single-page app: real files when they exist, index.html
// for client-side routes such as /view/today, and a placeholder when the UI was
// never built.
func Handler() http.Handler {
	assets := Assets()
	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Available() {
			writePlaceholder(w)
			return
		}
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" || clean == "." {
			clean = "index.html"
		}

		if _, err := fs.Stat(assets, clean); err != nil {
			// Unknown path: a deep link belongs to the router, a missing asset
			// should stay a 404 rather than silently return HTML.
			if path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, assets)
			return
		}

		if clean == "index.html" {
			serveIndex(w, assets)
			return
		}
		// Vite emits content-hashed asset names, so they can be cached hard.
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		writePlaceholder(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func writePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholder))
}

const placeholder = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>Lumen</title>
<style>
 body{font:14px/1.6 system-ui,sans-serif;max-width:640px;margin:12vh auto;padding:0 24px;
      color:#23241f;background:#fbfbfa}
 code{background:#f2f2ef;padding:2px 6px;border-radius:4px;font-size:13px}
 pre{background:#f2f2ef;padding:12px 14px;border-radius:6px;overflow-x:auto}
 @media (prefers-color-scheme:dark){body{background:#17181a;color:#e4e5e2}
   code,pre{background:#26282c}}
</style></head><body>
<h1>Lumen</h1>
<p>Сервер работает, но веб-интерфейс не вшит в этот бинарник.</p>
<p>Соберите его:</p>
<pre>make app          # или scripts/build.ps1</pre>
<p>Либо запустите клиент отдельно в режиме разработки:</p>
<pre>cd web &amp;&amp; npm run dev</pre>
<p>API доступен по <code>/api/v1</code>, состояние — <code>/healthz</code>.</p>
</body></html>
`
