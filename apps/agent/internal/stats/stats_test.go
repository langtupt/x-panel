package stats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResponseBytes(t *testing.T) {
	cases := map[string]int64{
		`1.2.3.4 - - [19/Oct/2026:10:00:00 +0700] "GET / HTTP/1.1" 200 5120 "-" "curl/8"` + "\n":              5120,
		`1.2.3.4 - - [19/Oct/2026:10:00:00 +0700] "GET /x\x22y HTTP/1.1" 404 153 "-" "Mozilla"` + "\n":        153,
		`::1 - bob [19/Oct/2026:10:00:00 +0700] "POST /api HTTP/2.0" 301 0 "https://a/" "agent \"x\""` + "\n": 0,
	}
	for line, want := range cases {
		if got, ok := responseBytes(line); !ok || got != want {
			t.Errorf("%q: got %d %v, want %d", line, got, ok, want)
		}
	}
	if _, ok := responseBytes("garbage\n"); ok {
		t.Error("garbage parsed")
	}
}

func TestReadLogIncremental(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.access.log")
	line := `1.1.1.1 - - [x] "GET / HTTP/1.1" 200 100 "-" "-"` + "\n"
	if err := os.WriteFile(path, []byte(line+line), 0o644); err != nil {
		t.Fatal(err)
	}
	off := map[string]logOffset{}
	if b, _ := readLog(path, off); b != 0 {
		t.Fatalf("first sight must start at the end, got %d", b)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(line + line + line + `partial "GET`)
	f.Close()
	if b, n := readLog(path, off); b != 300 || n != 3 {
		t.Fatalf("got %d bytes %d reqs, want 300/3", b, n)
	}
	if b, _ := readLog(path, off); b != 0 {
		t.Fatalf("nothing new, got %d", b)
	}
	// Truncation (rotation by copy) restarts from the beginning.
	os.WriteFile(path, []byte(line), 0o644)
	if b, _ := readLog(path, off); b != 100 {
		t.Fatalf("after truncate got %d, want 100", b)
	}
}
