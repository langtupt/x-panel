package backup

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func roundTrip(t *testing.T, data []byte, pass string) ([]byte, []byte) {
	var enc bytes.Buffer
	w, err := NewEncrypter(&enc, pass)
	if err != nil {
		t.Fatal(err)
	}
	// Odd write sizes exercise chunk boundaries.
	for p := data; len(p) > 0; {
		n := min(len(p), 7777)
		if _, err := w.Write(p[:n]); err != nil {
			t.Fatal(err)
		}
		p = p[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return enc.Bytes(), data
}

func decrypt(b []byte, pass string) ([]byte, error) {
	r, err := NewDecrypter(bytes.NewReader(b), pass)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestCryptRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 123} {
		data := make([]byte, size)
		rand.Read(data)
		enc, _ := roundTrip(t, data, "correct horse")
		got, err := decrypt(enc, "correct horse")
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
}

func TestCryptRejectsTampering(t *testing.T) {
	data := make([]byte, 3*chunkSize)
	rand.Read(data)
	enc, _ := roundTrip(t, data, "pw-123456789")
	if _, err := decrypt(enc, "wrong-pass"); err == nil {
		t.Error("wrong passphrase accepted")
	}
	flipped := bytes.Clone(enc)
	flipped[len(flipped)/2] ^= 1
	if _, err := decrypt(flipped, "pw-123456789"); err == nil {
		t.Error("modified data accepted")
	}
	// Cut after the first chunk: must not look like a complete backup.
	cut := enc[:len(magic)+saltLen+4+chunkSize+16]
	if _, err := decrypt(cut, "pw-123456789"); err == nil {
		t.Error("truncated backup accepted")
	}
	if _, err := decrypt(append(bytes.Clone(enc), 0), "pw-123456789"); err == nil {
		t.Error("trailing garbage accepted")
	}
}
