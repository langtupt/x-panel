package gdrive

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDrive implements the token endpoint and a resumable upload session.
type fakeDrive struct {
	mu       sync.Mutex
	got      bytes.Buffer
	ranges   []string
	fail5xx  int // fail the next N chunk PUTs with 503
	finished bool
}

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/token":
		fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
	case r.Method == http.MethodPost && r.URL.Path == "/upload":
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Location", "http://"+r.Host+"/session")
	case r.Method == http.MethodPut && r.URL.Path == "/session":
		if f.fail5xx > 0 {
			f.fail5xx--
			w.WriteHeader(503)
			return
		}
		rng := r.Header.Get("Content-Range")
		f.ranges = append(f.ranges, rng)
		b, _ := io.ReadAll(r.Body)
		f.got.Write(b)
		if strings.HasSuffix(rng, "/*") {
			w.WriteHeader(308)
			return
		}
		f.finished = true
		fmt.Fprintf(w, `{"id":"file1","name":"x","size":"%d"}`, f.got.Len())
	default:
		w.WriteHeader(404)
	}
}

func withFake(t *testing.T, f *fakeDrive) {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	oldTok, oldUp, oldChunk := tokenURL, uploadURL, ChunkSize
	tokenURL, uploadURL, ChunkSize = srv.URL+"/token", srv.URL+"/upload", 256<<10
	t.Cleanup(func() { tokenURL, uploadURL, ChunkSize = oldTok, oldUp, oldChunk })
}

func TestUploadChunks(t *testing.T) {
	for _, size := range []int{0, 1000, 256 << 10, 600 << 10} {
		f := &fakeDrive{fail5xx: 1}
		withFake(t, f)
		data := bytes.Repeat([]byte("x"), size)
		file, err := New(Creds{}).Upload(context.Background(), "x", "folder", bytes.NewReader(data), nil)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !f.finished || f.got.Len() != size || file.SizeBytes() != int64(size) {
			t.Fatalf("size %d: uploaded %d, ranges %v", size, f.got.Len(), f.ranges)
		}
		if last := f.ranges[len(f.ranges)-1]; !strings.HasSuffix(last, fmt.Sprintf("/%d", size)) {
			t.Fatalf("size %d: final range %q does not state the total", size, last)
		}
	}
}
