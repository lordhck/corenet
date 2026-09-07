package daemon

import (
	"context"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"corenet/pkg/protocol"
)

// Directory is the part of the registry the daemon needs.
type Directory interface {
	Lookup(name string) (protocol.Service, bool)
	List() []protocol.Service
	ListLocal() []protocol.Service
	Add(svc protocol.Service) (protocol.Service, error)
	Remove(name string) error
	CountRemote(nodeID string) int
}

type targetKey struct{}

// NewProxy returns the CoreNet HTTP entry point. Browsers reach it because
// every .core name resolves here; it routes on the Host header alone.
func NewProxy(dir Directory, logger *log.Logger) http.Handler {
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = r.Context().Value(targetKey{}).(string)
			// r.Host is left alone, so the backend still sees the
			// .core name and can serve virtual hosts.
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Printf("proxy: %s: %v", r.Host, err)
			writeErrorPage(w, http.StatusBadGateway, "Service unavailable",
				fmt.Sprintf("%s is registered on CoreNet but did not answer.", hostName(r.Host)))
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := hostName(r.Host)
		svc, ok := dir.Lookup(name)
		if !ok {
			writeErrorPage(w, http.StatusNotFound, "Unknown service",
				fmt.Sprintf("%s is not registered on this CoreNet node.", name))
			return
		}
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), targetKey{}, svc.Addr())))
	})
}

// hostName reduces a Host header to a CoreNet name.
func hostName(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return protocol.NormalizeName(host)
}

// writeErrorPage answers a browser with plain HTML and CSS. No JavaScript.
func writeErrorPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	page := strings.NewReplacer(
		"{{status}}", fmt.Sprintf("%d", status),
		"{{title}}", html.EscapeString(title),
		"{{message}}", html.EscapeString(message),
	).Replace(errorPage)
	_, _ = w.Write([]byte(page))
}

const errorPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{status}} {{title}} - CoreNet</title>
<style>
body { font-family: sans-serif; margin: 4rem auto; max-width: 36rem; padding: 0 1rem; }
h1 { font-size: 1.25rem; }
p { color: #444; }
footer { margin-top: 2rem; border-top: 1px solid #ddd; padding-top: 0.5rem; color: #777; font-size: 0.85rem; }
</style>
</head>
<body>
<h1>{{status}} {{title}}</h1>
<p>{{message}}</p>
<footer>CoreNet ` + protocol.Version + `</footer>
</body>
</html>
`
