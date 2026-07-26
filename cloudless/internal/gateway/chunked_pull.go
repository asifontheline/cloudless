package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// Chunked parallel transfers (O4): pulling a large artifact from one peer
// serially wastes every other peer that also holds it. When more than one
// peer has the artifact and it's big enough to be worth splitting, fetch
// disjoint byte ranges from all of them concurrently and reassemble —
// http.ServeFile (relay's /blob handler) already answers Range requests,
// so no server-side change was needed to make this possible.

const (
	chunkMinSplitSize = 4 << 20 // below this, a single GET beats the overhead of splitting
	chunkSize         = 4 << 20
	chunkMaxWorkers   = 6
)

// chunkSource is one peer known to hold the artifact.
type chunkSource struct {
	Base string // peer's blob-serving base URL, e.g. https://host:port
	Name string
}

// fetchChunked pulls an artifact into a temp file, splitting it across
// sources when there's more than one and it's large enough to be worth it.
// The caller is responsible for verifying the result (Store.Add re-hashes
// on add) and removing the temp file — a single lying or corrupt peer for
// one chunk fails the whole pull rather than silently poisoning the store.
func fetchChunked(ctx context.Context, client *http.Client, sources []chunkSource, size int64) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("no source peers")
	}
	if len(sources) == 1 || size <= chunkMinSplitSize {
		return fetchWhole(ctx, client, sources[0])
	}

	tmp, err := os.CreateTemp("", "chunked-pull-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if err := tmp.Truncate(size); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}

	type chunkRange struct{ start, end int64 }
	var chunks []chunkRange
	for start := int64(0); start < size; start += chunkSize {
		end := start + chunkSize - 1
		if end >= size {
			end = size - 1
		}
		chunks = append(chunks, chunkRange{start, end})
	}

	sem := make(chan struct{}, chunkMaxWorkers)
	var wg sync.WaitGroup
	errCh := make(chan error, len(chunks))
	for i, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c chunkRange) {
			defer wg.Done()
			defer func() { <-sem }()
			src := sources[i%len(sources)] // spread chunks round-robin across peers
			if err := fetchRange(ctx, client, src, c.start, c.end, tmp); err != nil {
				errCh <- fmt.Errorf("chunk %d-%d from %s: %w", c.start, c.end, src.Base, err)
			}
		}(i, c)
	}
	wg.Wait()
	close(errCh)
	tmp.Close()
	if err, ok := <-errCh; ok {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func fetchRange(ctx context.Context, client *http.Client, src chunkSource, start, end int64, out *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.Base+"/blob?name="+src.Name, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, end-start+1))
	if err != nil {
		return err
	}
	_, err = out.WriteAt(buf, start)
	return err
}

func fetchWhole(ctx context.Context, client *http.Client, src chunkSource) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.Base+"/blob?name="+src.Name, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp("", "chunked-pull-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}
