package mail

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useSenders(t *testing.T, content string) {
	t.Helper()
	sendersFile = filepath.Join(t.TempDir(), "senders")
	if err := os.WriteFile(sendersFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFrom(t *testing.T) {
	useSenders(t, "a@x.com a@x.com\nb@x.com b@x.com\nsales@x.com a@x.com,b@x.com\n")
	for _, c := range []struct {
		login string
		from  []string
		ok    bool
	}{
		{"a@x.com", []string{"a@x.com"}, true},
		{"a@x.com", []string{`"Anh A" <A@X.com>`}, true},
		{"a@x.com", []string{"Sales <sales@x.com>"}, true},
		{"a@x.com", nil, true}, // cleanup adds From from the checked envelope
		{"a@x.com", []string{"b@x.com"}, false},
		{"a@x.com", []string{"=?utf-8?q?S=E1=BA=BFp?= <b@x.com>"}, false},
		{"a@x.com", []string{"a@x.com, b@x.com"}, false},
		{"a@x.com", []string{"a@x.com", "b@x.com"}, false}, // two From headers
		{"a@x.com", []string{"someone@gmail.com"}, false},
		{"a@x.com", []string{"=?x-unknown?q?abc?= <b@x.com>"}, false},
		{"a@x.com", []string{"garbage"}, false},
	} {
		if got := checkFrom(c.login, c.from) == ""; got != c.ok {
			t.Errorf("checkFrom(%q, %q) allowed=%v, want %v", c.login, c.from, got, c.ok)
		}
	}
}

func packet(cmd byte, data ...string) []byte {
	body := []byte(strings.Join(data, ""))
	b := make([]byte, 5, 5+len(body))
	binary.BigEndian.PutUint32(b, uint32(len(body)+1))
	b[4] = cmd
	return append(b, body...)
}

func replies(t *testing.T, out []byte) []string {
	t.Helper()
	var got []string
	r := bytes.NewReader(out)
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return got
		}
		p := make([]byte, binary.BigEndian.Uint32(hdr[:]))
		if _, err := io.ReadFull(r, p); err != nil {
			t.Fatal(err)
		}
		got = append(got, string(p))
	}
}

// One SMTP session as Postfix speaks it: negotiation, MAIL macros, headers,
// end of message; two messages to check the state is reset.
func TestMilterSession(t *testing.T) {
	useSenders(t, "a@x.com a@x.com\nb@x.com b@x.com\n")
	neg := make([]byte, 12)
	binary.BigEndian.PutUint32(neg[0:], 6)
	binary.BigEndian.PutUint32(neg[4:], 0x1ff)
	binary.BigEndian.PutUint32(neg[8:], 0x1fffff)
	var in bytes.Buffer
	in.Write(packet('O', string(neg)))
	for _, from := range []string{"b@x.com", "a@x.com"} {
		in.Write(packet('D', "M", "{auth_authen}\x00a@x.com\x00{mail_addr}\x00a@x.com\x00"))
		in.Write(packet('M', "<a@x.com>\x00"))
		in.Write(packet('L', "Subject\x00hi\x00"))
		in.Write(packet('L', "From\x00"+from+"\x00"))
		in.Write(packet('N'))
		in.Write(packet('E'))
	}
	in.Write(packet('Q'))
	var out bytes.Buffer
	if err := serveMilter(&in, &out); err != nil {
		t.Fatal(err)
	}
	got := replies(t, out.Bytes())
	if len(got) != 1+2*5 || got[0][0] != 'O' {
		t.Fatalf("replies: %q", got)
	}
	if flags := binary.BigEndian.Uint32([]byte(got[0][9:13])); flags != smfipSkip {
		t.Errorf("protocol flags %#x", flags)
	}
	if first := got[5]; first[0] != 'y' || !strings.Contains(first, "550 5.7.1") || !strings.Contains(first, "b@x.com") {
		t.Errorf("spoofed From not rejected: %q", first)
	}
	if second := got[10]; second != "a" {
		t.Errorf("own From not accepted: %q", second)
	}
}

// smtpd parses per-milter settings itself and dies on "name = value"
// (postconf/postfix check accept it): every submission then fails.
func TestSubmissionMiltersSyntax(t *testing.T) {
	inner := submissionMilters[strings.Index(submissionMilters, "{")+1 : strings.Index(submissionMilters, "}")]
	for _, part := range strings.Split(inner, ",")[1:] {
		if strings.Contains(part, " =") || strings.Contains(part, "= ") {
			t.Errorf("milter attribute %q has spaces around '='", strings.TrimSpace(part))
		}
	}
}
