//go:build soak

package apitest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"peekaping/internal/probeproto"
)

// heapAfterGC is the live heap in bytes after a collection
func heapAfterGC() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func mib(bytes uint64) string { return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20)) }

// batchTimes notes, for each batch of results a probe posted, how many it held and how long the API
// took to answer: the answer comes after the last of them is stored
type batchTimes struct {
	mu    sync.Mutex
	sizes []int
	took  []time.Duration
}

func (b *batchTimes) add(size int, took time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sizes, b.took = append(b.sizes, size), append(b.took, took)
}

// summary is what a run stored: how many batches and results, and how fast
type summary struct {
	batches, results       int
	medianRate, lowRate    float64 // results per second of a batch's request
	median, p95, max, busy time.Duration
	meanSize               float64
}

func (b *batchTimes) summarize() (summary, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var s summary
	rates := make([]float64, 0, len(b.sizes))
	latencies := make([]time.Duration, 0, len(b.sizes))
	for i, size := range b.sizes {
		s.results += size
		s.busy += b.took[i]
		latencies = append(latencies, b.took[i])
		if b.took[i] > 0 {
			rates = append(rates, float64(size)/b.took[i].Seconds())
		}
	}
	if len(rates) == 0 {
		return s, false
	}
	slices.Sort(rates)
	slices.Sort(latencies)
	s.batches = len(b.sizes)
	s.meanSize = float64(s.results) / float64(s.batches)
	s.medianRate, s.lowRate = rates[len(rates)/2], rates[len(rates)/20]
	s.median, s.p95, s.max = latencies[len(latencies)/2], latencies[len(latencies)*95/100], latencies[len(latencies)-1]
	return s, true
}

// report logs the batches: how many, and the rate they were stored at, as results per second of
// the time the request took
func (b *batchTimes) report(t *testing.T) {
	t.Helper()
	s, ok := b.summarize()
	if !ok {
		t.Log("no batch was posted")
		return
	}
	t.Logf("%d batches of %.1f results on average (%d results); a batch is stored at %.0f results/s median, %.0f at the 5th percentile; "+
		"a request takes %s median, %s at the 95th percentile, %s at most; the requests kept the API busy %s in all",
		s.batches, s.meanSize, s.results, s.medianRate, s.lowRate,
		s.median.Round(100*time.Microsecond), s.p95.Round(100*time.Microsecond), s.max.Round(100*time.Microsecond), s.busy.Round(time.Millisecond))
}

// timedTransport notes how long the API takes to answer the posts of results
type timedTransport struct {
	inner   http.RoundTripper
	batches *batchTimes
}

func (rt timedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.Path != "/probe/v1/results" {
		return rt.inner.RoundTrip(req)
	}
	started := time.Now()
	resp, err := rt.inner.RoundTrip(req)
	took := time.Since(started)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		return resp, nil
	}
	reader := io.Reader(bytes.NewReader(body))
	if resp.Header.Get("Content-Encoding") == "gzip" {
		if unzipped, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
			reader = unzipped
		}
	}
	var outcomes probeproto.ResultsResponse
	if json.NewDecoder(reader).Decode(&outcomes) != nil {
		return resp, nil
	}
	accepted := 0
	for _, outcome := range outcomes.Results {
		if outcome.Accepted {
			accepted++
		}
	}
	if accepted > 0 {
		rt.batches.add(accepted, took)
	}
	return resp, nil
}
