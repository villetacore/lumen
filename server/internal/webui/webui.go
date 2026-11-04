// Package webui serves the compiled web client from inside the binary, so a
// single executable is the whole application. Assets are copied into dist/ by
// `make app` (or scripts/build.ps1); without them the server still runs and
// explains how to build the UI.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// placeholder is the param the static export is prebuilt with (see the
// generateStaticParams of web/src/app). A real address such as /tracker/WEB/board
// is served from tracker/_/board.html, and the browser reads the real params from
// the URL.
const placeholder = "_"

// catchAll lists routes whose last param spans the rest of the path.
var catchAll = []string{"note"}

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

// Handler serves the embedded web client, or a placeholder page when the UI was
// never built.
func Handler() http.Handler {
	if !Available() {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writePlaceholderPage(w) })
	}
	return handlerFor(Assets())
}

func handlerFor(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		name, ok := resolve(assets, clean, r.URL.Query().Has("_rsc"))
		if !ok {
			// A missing asset stays a bare 404; an unknown address gets the page.
			if path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			serveFile(w, r, assets, "404.html", http.StatusNotFound)
			return
		}
		serveFile(w, r, assets, name, http.StatusOK)
	})
}

// resolve finds the embedded file for a request path. Client-side navigation asks
// for a page's data next to its address (flagged by _rsc): /calendar.txt or
// /calendar/__next._tree.txt.
func resolve(assets fs.FS, clean string, data bool) (string, bool) {
	if clean == "" || clean == "." {
		clean = "index"
	}
	if isFile(assets, clean) {
		return clean, true
	}
	if data {
		dir, file := path.Split(clean)
		dir = strings.TrimSuffix(dir, "/")
		if strings.HasPrefix(file, "__next") {
			if dir == "" {
				return clean, isFile(assets, clean)
			}
			if prebuilt, ok := prebuiltRoute(assets, dir); ok {
				name := path.Join(prebuilt, file)
				return name, isFile(assets, name)
			}
			return "", false
		}
		if route, isData := strings.CutSuffix(clean, ".txt"); isData {
			if prebuilt, ok := prebuiltRoute(assets, route); ok {
				return prebuilt + ".txt", true
			}
		}
		return "", false
	}
	if prebuilt, ok := prebuiltRoute(assets, clean); ok {
		return prebuilt + ".html", true
	}
	return "", false
}

// prebuiltRoute maps a page address to the name it was exported under, with the
// placeholder in place of params. It reports false when no such page exists.
func prebuiltRoute(assets fs.FS, route string) (string, bool) {
	if isFile(assets, route+".html") {
		return route, true
	}
	segments := strings.Split(route, "/")
	for _, root := range catchAll {
		if segments[0] == root && len(segments) > 1 {
			prebuilt := root + "/" + placeholder
			return prebuilt, isFile(assets, prebuilt+".html")
		}
	}
	// Static segments win over params, so walk the tree one level at a time.
	prebuilt := ""
	for i, segment := range segments {
		exact := path.Join(prebuilt, segment)
		param := path.Join(prebuilt, placeholder)
		switch {
		case i == len(segments)-1 && isFile(assets, exact+".html"):
			prebuilt = exact
		case i == len(segments)-1:
			prebuilt = param
		case isDir(assets, exact):
			prebuilt = exact
		case isDir(assets, param):
			prebuilt = param
		default:
			return "", false
		}
	}
	return prebuilt, isFile(assets, prebuilt+".html")
}

func isFile(assets fs.FS, name string) bool {
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

func isDir(assets fs.FS, name string) bool {
	info, err := fs.Stat(assets, name)
	return err == nil && info.IsDir()
}

func serveFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name string, status int) {
	data, err := fs.ReadFile(assets, name)
	if err != nil {
		writePlaceholderPage(w)
		return
	}
	// Next emits content-hashed asset names, so they can be cached hard. Pages and
	// their data must follow a new build at once.
	switch {
	case strings.HasPrefix(name, "_next/static/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".txt"):
		w.Header().Set("Cache-Control", "no-store")
	}
	if status != http.StatusOK {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(data)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func writePlaceholderPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholderPage))
}

const placeholderPage = `<!doctype html>
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
