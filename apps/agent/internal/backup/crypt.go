package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/scrypt"
)

// Encrypted backup format:
//
//	"XPANELB1" | salt (16) | chunk...
//	chunk = length (4, big endian, of the sealed chunk) | AES-256-GCM(plain ≤ 64 KiB)
//
// The key is scrypt(passphrase, salt). Each chunk's nonce is its index (8
// bytes) and a final flag (1 byte), so chunks cannot be reordered, dropped or
// cut off: the stream must end with the chunk marked final.
const (
	magic     = "XPANELB1"
	saltLen   = 16
	chunkSize = 64 << 10
)

var ErrBadBackup = errors.New("tệp backup hỏng, bị sửa đổi hoặc sai passphrase")

func deriveKey(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(i uint64, final bool) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint64(n, i)
	if final {
		n[8] = 1
	}
	return n
}

type encWriter struct {
	w     io.Writer
	aead  cipher.AEAD
	buf   []byte
	index uint64
	err   error
}

// NewEncrypter returns a writer that encrypts into w; Close writes the final
// chunk and must be called.
func NewEncrypter(w io.Writer, passphrase string) (io.WriteCloser, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(append([]byte(magic), salt...)); err != nil {
		return nil, err
	}
	return &encWriter{w: w, aead: aead, buf: make([]byte, 0, chunkSize)}, nil
}

func (e *encWriter) flush(final bool) error {
	sealed := e.aead.Seal(nil, nonce(e.index, final), e.buf, nil)
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(sealed)))
	if _, err := e.w.Write(l[:]); err != nil {
		return err
	}
	if _, err := e.w.Write(sealed); err != nil {
		return err
	}
	e.index++
	e.buf = e.buf[:0]
	return nil
}

func (e *encWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n := 0
	for len(p) > 0 {
		k := copy(e.buf[len(e.buf):cap(e.buf)], p)
		e.buf = e.buf[:len(e.buf)+k]
		p, n = p[k:], n+k
		// Keep the last full chunk until more data or Close, so that the
		// final chunk is never empty unless the whole stream is.
		if len(e.buf) == cap(e.buf) && len(p) > 0 {
			if e.err = e.flush(false); e.err != nil {
				return n, e.err
			}
		}
	}
	return n, nil
}

func (e *encWriter) Close() error {
	if e.err != nil {
		return e.err
	}
	e.err = errors.New("closed")
	return e.flush(true)
}

type decReader struct {
	r     io.Reader
	aead  cipher.AEAD
	plain []byte
	index uint64
	done  bool
}

// NewDecrypter verifies and decrypts a stream written by NewEncrypter.
func NewDecrypter(r io.Reader, passphrase string) (io.Reader, error) {
	head := make([]byte, len(magic)+saltLen)
	if _, err := io.ReadFull(r, head); err != nil || string(head[:len(magic)]) != magic {
		return nil, ErrBadBackup
	}
	aead, err := deriveKey(passphrase, head[len(magic):])
	if err != nil {
		return nil, err
	}
	return &decReader{r: r, aead: aead}, nil
}

func (d *decReader) next() error {
	var l [4]byte
	if _, err := io.ReadFull(d.r, l[:]); err != nil {
		return ErrBadBackup // ended without the final chunk
	}
	n := binary.BigEndian.Uint32(l[:])
	if n < uint32(d.aead.Overhead()) || n > chunkSize+uint32(d.aead.Overhead()) {
		return ErrBadBackup
	}
	sealed := make([]byte, n)
	if _, err := io.ReadFull(d.r, sealed); err != nil {
		return ErrBadBackup
	}
	if plain, err := d.aead.Open(nil, nonce(d.index, false), sealed, nil); err == nil {
		d.plain, d.index = plain, d.index+1
		return nil
	}
	plain, err := d.aead.Open(nil, nonce(d.index, true), sealed, nil)
	if err != nil {
		return ErrBadBackup
	}
	// Nothing may follow the final chunk.
	if k, _ := d.r.Read(make([]byte, 1)); k != 0 {
		return ErrBadBackup
	}
	d.plain, d.done = plain, true
	return nil
}

func (d *decReader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}
