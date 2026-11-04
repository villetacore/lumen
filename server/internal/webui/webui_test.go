package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// export has the shape `next build` produces for a static export.
var export = fstest.MapFS{
	"index.html":                 {Data: []byte("root")},
	"index.txt":                  {Data: []byte("root data")},
	"__next._tree.txt":           {Data: []byte("root tree")},
	"404.html":                   {Data: []byte("not found page")},
	"calendar.html":              {Data: []byte("calendar")},
	"calendar.txt":               {Data: []byte("calendar data")},
	"calendar/__next._tree.txt":  {Data: []byte("calendar tree")},
	"tracker.html":               {Data: []byte("tracker")},
	"tracker/_.html":             {Data: []byte("issues")},
	"tracker/_.txt":              {Data: []byte("issues data")},
	"tracker/_/board.html":       {Data: []byte("board")},
	"tracker/_/board.txt":        {Data: []byte("board data")},
	"tracker/_/board/__next.txt": {Data: []byte("board tree")},
	"task/_.html":                {Data: []byte("task")},
	"note/_.html":                {Data: []byte("note")},
	"note/_.txt":                 {Data: []byte("note data")},
	"note/_/__next.note.txt":     {Data: []byte("note tree")},
	"_next/static/chunks/a.js":   {Data: []byte("js")},
	"fonts/Manrope.ttf":          {Data: []byte("font")},
}

func TestHandlerResolvesAddresses(t *testing.T) {
	tests := []struct {
		url    string
		status int
		body   string
	}{
		{"/", 200, "root"},
		{"/calendar", 200, "calendar"},
		{"/tracker", 200, "tracker"},
		{"/tracker/WEB", 200, "issues"},
		{"/tracker/WEB/board", 200, "board"},
		{"/tracker/board", 200, "issues"},
		{"/task/a.b.c", 200, "task"},
		{"/note/x.md", 200, "note"},
		{"/note/20-projects/%D0%B0%20%D0%B1.md", 200, "note"},
		{"/note/data.txt", 200, "note"},
		// Data for client-side navigation.
		{"/index.txt?_rsc=1", 200, "root data"},
		{"/__next._tree.txt?_rsc=1", 200, "root tree"},
		{"/calendar.txt?_rsc=1", 200, "calendar data"},
		{"/calendar/__next._tree.txt?_rsc=1", 200, "calendar tree"},
		{"/tracker/WEB.txt?_rsc=1", 200, "issues data"},
		{"/tracker/WEB/board.txt?_rsc=1", 200, "board data"},
		{"/tracker/WEB/board/__next.txt?_rsc=1", 200, "board tree"},
		{"/note/a/b.md.txt?_rsc=1", 200, "note data"},
		{"/note/a/b.md/__next.note.txt?_rsc=1", 200, "note tree"},
		// Files.
		{"/_next/static/chunks/a.js", 200, "js"},
		{"/fonts/Manrope.ttf", 200, "font"},
		// Unknown addresses.
		{"/nope", 404, "not found page"},
		{"/tracker/WEB/nope", 404, "not found page"},
		{"/calendar/deep", 404, "not found page"},
		{"/note", 404, "not found page"},
		{"/missing.js", 404, "404 page not found\n"},
		{"/_next/static/missing.js", 404, "404 page not found\n"},
		{"/nope.txt?_rsc=1", 404, "404 page not found\n"},
		{"/../../etc/passwd", 404, "not found page"},
	}
	handler := handlerFor(export)
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))
			if rec.Code != tt.status || rec.Body.String() != tt.body {
				t.Errorf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}
}

func TestHandlerCaching(t *testing.T) {
	tests := []struct{ url, want string }{
		{"/_next/static/chunks/a.js", "public, max-age=31536000, immutable"},
		{"/tracker/WEB/board", "no-store"},
		{"/tracker/WEB/board.txt?_rsc=1", "no-store"},
		{"/nope", "no-store"},
	}
	handler := handlerFor(export)
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.url, nil))
		if got := rec.Header().Get("Cache-Control"); got != tt.want {
			t.Errorf("%s: Cache-Control %q, want %q", tt.url, got, tt.want)
		}
	}
}
