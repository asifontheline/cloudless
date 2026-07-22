package gateway

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipWriter wraps a ResponseWriter, transparently gzip-encoding the body
// once the caller's own WriteHeader/Write reveals it's actually compressible
// content. Underlying bytes are already-encrypted or already-compressed data
// (vault reads, model blobs, sealed backup archives) which gzip cannot
// shrink and must never be routed through this — see the mux wiring for
// which endpoints opt in.
type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// Flush lets streaming JSON writers (there are none today, but this keeps
// the wrapper safe if one is added) push partial output through gzip.
func (g *gzipWriter) Flush() {
	g.gz.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withGzip compresses the response body when the client advertises gzip
// support (O7). Only wrap handlers whose response is text/JSON — never
// binary blobs, streaming inference, or the sealed backup archive, none of
// which shrink under gzip and some of which need real-time flushing this
// wrapper does not preserve byte-for-byte.
func withGzip(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length") // length changes; let it be chunked
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next(&gzipWriter{ResponseWriter: w, gz: gz}, r)
	}
}
