package pop3server

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func FuzzDotStuffWriterChunks(f *testing.F) {
	for _, body := range []string{"", ".\r\n..\nend\r", "a\r.b\r\n", "a\n.b\r\nc", strings.Repeat("x", 4097) + "\r\n.dot\r\n"} {
		f.Add([]byte(body), uint8(1))
		f.Add([]byte(body), uint8(127))
	}
	f.Fuzz(func(t *testing.T, body []byte, chunk uint8) {
		if len(body) > 1<<20 {
			t.Skip()
		}
		var out bytes.Buffer
		writer := newDotStuffWriter(&out)
		size := int(chunk) + 1
		for offset := 0; offset < len(body); offset += size {
			part := body[offset:min(offset+size, len(body))]
			n, err := writer.Write(part)
			if n != len(part) || err != nil {
				t.Fatalf("write=%d,%v", n, err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		normalized := crlfNormalizeRef(string(body))
		want := strings.ReplaceAll("\n"+normalized, "\n.", "\n..")[1:]
		if len(body) > 0 && body[len(body)-1] != '\n' {
			if body[len(body)-1] == '\r' {
				want += "\n"
			} else {
				want += "\r\n"
			}
		}
		want += ".\r\n"
		if out.String() != want {
			t.Fatalf("body=%q chunk=%d got=%q want=%q", body, size, out.String(), want)
		}
	})
}

type shortDotWriter struct{}

func (shortDotWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }
func TestDotStuffWriterShortWrite(t *testing.T) {
	for _, tc := range []struct {
		body string
		n    int
	}{{"abcdef", 3}, {".abc", 0}, {"\nabc", 0}} {
		writer := newDotStuffWriter(shortDotWriter{})
		n, err := writer.Write([]byte(tc.body))
		if n != tc.n || !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("%q: %d,%v", tc.body, n, err)
		}
	}
}

type dotBenchmarkReader struct {
	block     []byte
	offset    int
	remaining int64
}

func (r *dotBenchmarkReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n := 0
	for n < len(p) {
		k := copy(p[n:], r.block[r.offset:])
		n += k
		r.offset = (r.offset + k) % len(r.block)
	}
	r.remaining -= int64(n)
	return n, nil
}

func BenchmarkDotStuffWriter(b *testing.B) {
	block := []byte(strings.Repeat(strings.Repeat("x", 76)+"\r\n", 13443))
	buffer := make([]byte, 128<<10)
	b.SetBytes(2 << 30)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink := bufio.NewWriter(io.Discard)
		writer := newDotStuffWriter(sink)
		n, err := io.CopyBuffer(writer, &dotBenchmarkReader{block: block, remaining: 2 << 30}, buffer)
		if err != nil || n != 2<<30 {
			b.Fatalf("write=%d,%v", n, err)
		}
		if err = writer.Close(); err != nil {
			b.Fatal(err)
		}
		if err = sink.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}
