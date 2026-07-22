package relay

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipWriter and withGzip mirror the gateway package's O7 compression
// wrapper. Duplicated rather than shared: relay and gateway are separate
// packages with no existing http-utils dependency between them, and the
// wrapper is a dozen lines — not worth a new shared package for.
type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// withGzip compresses JSON listing responses when the peer advertises gzip
// support. Never wrap blobOf (raw model/vault bytes — already dense,
// wouldn't shrink) or the inference proxy (streaming).
func withGzip(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next(&gzipWriter{ResponseWriter: w, gz: gz}, r)
	}
}
