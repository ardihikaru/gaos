package main

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Templates and static assets are EMBEDDED, not copied into the image.
//
// pustaka's Dockerfile does the opposite -- it copies docs/ in -- and that is
// right for pustaka, because docs are user content, authored and validated at
// runtime. Templates and stylesheets here are program assets: they change only
// when the binary changes. Embedding makes the distroless image a single file
// and removes a COPY that could silently drift from the code that reads it.
//
//go:embed templates static
var assets embed.FS

// Renderer parses the template set. In --prod it parses once; in dev it
// reparses from disk per render, which is the same dev/prod split pustaka draws
// with its rebuild timer (main.go:455) and means a template edit needs no
// restart.
type Renderer struct {
	dev  bool
	root string // directory to read from in dev mode

	mu sync.RWMutex
	t  *template.Template
}

func NewRenderer(dev bool, root string) (*Renderer, error) {
	r := &Renderer{dev: dev, root: root}
	t, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.t = t
	return r, nil
}

func (r *Renderer) parse() (*template.Template, error) {
	var src fs.FS = assets
	pattern := "templates/*.html"
	if r.dev {
		if _, err := os.Stat(r.root + "/templates"); err == nil {
			src = os.DirFS(r.root)
		}
	}
	return template.New("").Funcs(funcs).ParseFS(src, pattern)
}

func (r *Renderer) tmpl() (*template.Template, error) {
	if !r.dev {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.t, nil
	}
	t, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.t = t
	r.mu.Unlock()
	return t, nil
}

// Render writes one named template. It buffers first: a template that fails
// halfway through has already written a 200 and half a page, and the browser
// will render that half quite happily. Buffering turns a template bug into a
// 500 you can see in a log instead of a layout you cannot explain.
func (r *Renderer) Render(w http.ResponseWriter, status int, name string, data any) {
	t, err := r.tmpl()
	if err != nil {
		log.Printf("template parse: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("template %q: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// RenderWithChrome writes a fragment followed by the out-of-band chrome: the
// stat strip, the bell, the timeline counter, any new event pills and the
// toast. HTMX applies hx-swap-oob elements wherever they appear in the
// response, so appending them to every mutation is how five widgets stay
// current without five extra round trips.
func (r *Renderer) RenderWithChrome(w http.ResponseWriter, name string, v *View) {
	t, err := r.tmpl()
	if err != nil {
		log.Printf("template parse: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, v); err != nil {
		log.Printf("template %q: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(&buf, "oob-chrome", v); err != nil {
		log.Printf("template oob-chrome: %v", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

var funcs = template.FuncMap{
	"money":     func(v float64) string { return fmt.Sprintf("$%.2f", v) },
	"pct":       func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
	"seq":       RenderSequence,
	"roleShort": roleShort,
	"lines":     func(v []string) string { return strings.Join(v, "\n") },
	"join":      func(sep string, v []string) string { return strings.Join(v, sep) },
	"add":       func(a, b int) int { return a + b },
	"pluralS":   pluralS,
	"num":       thousands,
	"clip":      clip,
	"confPct":   func(v float64) int { return int(v*100 + 0.5) },
	// timingURL path-escapes both ids, as the answer sheet's URL does (D-081).
	"timingURL": timingURL,
	// since is a unix time as RFC 3339 UTC, for a <time datetime> live.js ticks.
	"since": func(sec int64) string { return time.Unix(sec, 0).UTC().Format(time.RFC3339) },
	// styleWidth exists so a template can size a bar without the caller having
	// to build a template.CSS. It is the only func that produces CSS, and it
	// takes a number, so there is nothing to inject.
	"styleWidth": func(v float64) template.CSS { return pctStyle("width", v) },
}

// thousands writes 1284 as "1,284". The chip's count is the only caller; a
// count that can reach four digits should not be read digit by digit.
func thousands(n int) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ---------- mount-point survival ----------

// sitePrefix reads the mount point Traefik's stripPrefix announces.
//
// Lifted from pustaka/main.go:836 including its validation, and for the same
// reason: this value ends up in browser-visible output (<base href>), and an
// unproxied client can set the header to anything at all. VALIDATE, NEVER
// TRUST. The worst a forged value can then do is produce a same-site path.
//
// If the routing ever stops setting this header the dashboard's assets resolve
// against whatever owns the site root -- the landing page's 404 -- and the
// failure is silent, browser-side, and invisible to a status-code check. That
// is documented at length in deploy/traefik/dynamic.yml; it is not hypothetical.
func sitePrefix(r *http.Request) string {
	p := r.Header.Get("X-Forwarded-Prefix")
	if p == "" || len(p) > 128 {
		return ""
	}
	if p[0] != '/' || strings.HasPrefix(p, "//") {
		return ""
	}
	if strings.ContainsAny(p, "\\ \t\r\n\"'<>") || strings.Contains(p, "://") {
		return ""
	}
	for _, c := range p {
		if c < 0x20 || c == 0x7f {
			return ""
		}
	}
	return strings.TrimRight(p, "/")
}

// ---------- gzip ----------

// gzipMiddleware, lifted from pustaka/main.go:405 unchanged. The 940-line
// stylesheet on its own justifies it; the HTML fragments HTMX refetches on
// every tick justify it twice.
//
// The extension switch covers "" deliberately -- that is what "/" and every
// "/ui/*" fragment path look like, i.e. everything this service actually
// serves.
type gzWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w gzWriter) Write(b []byte) (int, error) { return w.gz.Write(b) }
func (w gzWriter) WriteHeader(code int) {
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(code)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The live stream is never compressed. gzWriter is not an http.Flusher,
		// so wrapping it made handleStream answer 501 to every browser (they all
		// send Accept-Encoding: gzip); and a compressed stream would sit in the
		// gzip buffer instead of reaching the page. Found in M15.
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Header.Get("Range") != "" ||
			r.URL.Path == "/events/stream" || strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			next.ServeHTTP(w, r)
			return
		}
		switch strings.ToLower(path.Ext(r.URL.Path)) {
		case "", ".html", ".css", ".js", ".json", ".svg", ".txt":
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			next.ServeHTTP(gzWriter{w, gz}, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// jsonStatus writes v with an explicit status. Denials must use this: writing
// the body first commits a 200, and /healthz callers only look at the code.
func jsonStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonOut(w http.ResponseWriter, v any) { jsonStatus(w, http.StatusOK, v) }
