package ui_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/someson/azform/internal/ui"
)

// readAll reads the wrapper to EOF and returns the accumulated bytes.
func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var out bytes.Buffer
	buf := make([]byte, 8)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err == io.EOF {
			return out.Bytes()
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
}

func TestKeyNormalizerTranslatesSS3Home(t *testing.T) {
	src := bytes.NewReader([]byte{0x1b, 'O', 'H'})
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b, '[', 'H'}
	if !bytes.Equal(got, want) {
		t.Errorf("SS3 Home translation: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerTranslatesSS3End(t *testing.T) {
	src := bytes.NewReader([]byte{0x1b, 'O', 'F'})
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b, '[', 'F'}
	if !bytes.Equal(got, want) {
		t.Errorf("SS3 End translation: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerLeavesArrowsAlone(t *testing.T) {
	// \x1bOA/B/C/D are the SS3 arrow sequences bubbletea already handles.
	// Translating them would break navigation.
	for _, tail := range []byte{'A', 'B', 'C', 'D'} {
		src := bytes.NewReader([]byte{0x1b, 'O', tail})
		got := readAll(t, ui.NewKeyNormalizer(src))
		want := []byte{0x1b, 'O', tail}
		if !bytes.Equal(got, want) {
			t.Errorf("SS3 %c should pass through: got %q, want %q", tail, got, want)
		}
	}
}

func TestKeyNormalizerBareEscapePassesThroughImmediately(t *testing.T) {
	// A lone ESC (user pressed Esc to cancel edit mode) must NOT be
	// buffered waiting to see if an SS3 sequence follows. Buffering it
	// would either delay the Esc indefinitely or fuse it with the next
	// keystroke into Alt+X. The normalizer accepts that a split-read
	// SS3 Home/End (\x1b in one read, OH in the next) won't translate.
	src := &chunkedReader{chunks: [][]byte{{0x1b}, {'a'}}}
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b, 'a'}
	if !bytes.Equal(got, want) {
		t.Errorf("bare ESC + 'a' across reads: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerHandlesSplitReadsMidSequence(t *testing.T) {
	// 0x1b + 'O' in first chunk, 'F' in second.
	src := &chunkedReader{chunks: [][]byte{{0x1b, 'O'}, {'F'}}}
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b, '[', 'F'}
	if !bytes.Equal(got, want) {
		t.Errorf("split-read SS3 End: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerLoneEscape(t *testing.T) {
	// A trailing bare ESC (user pressed Esc) must be emitted immediately,
	// not buffered — Esc is a single-byte keypress the app needs to see.
	src := bytes.NewReader([]byte{0x1b})
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b}
	if !bytes.Equal(got, want) {
		t.Errorf("lone ESC: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerLoneEscapeO(t *testing.T) {
	// ESC + 'O' with nothing after (user pressed Alt+O) must survive as
	// two bytes, not get eaten waiting for a third.
	src := bytes.NewReader([]byte{0x1b, 'O'})
	got := readAll(t, ui.NewKeyNormalizer(src))
	want := []byte{0x1b, 'O'}
	if !bytes.Equal(got, want) {
		t.Errorf("lone ESC+O: got %q, want %q", got, want)
	}
}

func TestKeyNormalizerLargePassThrough(t *testing.T) {
	// Plain ASCII payload passes through untouched.
	payload := strings.Repeat("hello world ", 20)
	src := bytes.NewReader([]byte(payload))
	got := readAll(t, ui.NewKeyNormalizer(src))
	if string(got) != payload {
		t.Errorf("large pass-through mismatch:\n got: %q\nwant: %q", got, payload)
	}
}

func TestKeyNormalizerMixedStream(t *testing.T) {
	// Plain runes, then SS3 Home, then plain runes, then SS3 End.
	in := append([]byte("abc"), 0x1b, 'O', 'H')
	in = append(in, []byte("xy")...)
	in = append(in, 0x1b, 'O', 'F')
	want := append([]byte("abc"), 0x1b, '[', 'H')
	want = append(want, []byte("xy")...)
	want = append(want, 0x1b, '[', 'F')
	src := bytes.NewReader(in)
	got := readAll(t, ui.NewKeyNormalizer(src))
	if !bytes.Equal(got, want) {
		t.Errorf("mixed stream:\n got: %q\nwant: %q", got, want)
	}
}

// TestKeyNormalizerSatisfiesTermFile verifies that when the source is
// a *os.File-like term.File (ReadWriteCloser + Fd), the wrapper
// satisfies the same interface. Bubbletea's initInput uses this
// interface check to put the terminal in raw mode — failing it kills
// interactive editing.
func TestKeyNormalizerSatisfiesTermFile(t *testing.T) {
	src := &fakeTermFile{Reader: bytes.NewReader(nil), fd: 42}
	w := ui.NewKeyNormalizer(src)
	tf, ok := w.(interface {
		io.ReadWriteCloser
		Fd() uintptr
	})
	if !ok {
		t.Fatal("wrapped reader must satisfy term.File when source does")
	}
	if got := tf.Fd(); got != 42 {
		t.Errorf("Fd forwarding: got %d, want 42", got)
	}
	if _, err := tf.Write([]byte("x")); err != nil {
		t.Errorf("Write forwarding: %v", err)
	}
	if !src.wroteX {
		t.Error("Write should have reached the wrapped source")
	}
	if err := tf.Close(); err != nil {
		t.Errorf("Close forwarding: %v", err)
	}
	if !src.closed {
		t.Error("Close should have reached the wrapped source")
	}
}

// TestKeyNormalizerPlainReaderStaysPlain verifies plain io.Reader
// sources (like bytes.Reader) don't get promoted to term.File.
func TestKeyNormalizerPlainReaderStaysPlain(t *testing.T) {
	src := bytes.NewReader(nil)
	w := ui.NewKeyNormalizer(src)
	if _, ok := w.(interface{ Fd() uintptr }); ok {
		t.Error("wrapped reader must NOT expose Fd() when source doesn't")
	}
}

// fakeTermFile is a test double satisfying term.File (ReadWriteCloser + Fd).
type fakeTermFile struct {
	io.Reader
	fd     uintptr
	wroteX bool
	closed bool
}

func (f *fakeTermFile) Write(p []byte) (int, error) {
	if len(p) > 0 && p[0] == 'x' {
		f.wroteX = true
	}
	return len(p), nil
}
func (f *fakeTermFile) Close() error { f.closed = true; return nil }
func (f *fakeTermFile) Fd() uintptr  { return f.fd }

// chunkedReader delivers bytes in pre-baked chunks, one per Read call.
type chunkedReader struct {
	chunks [][]byte
	idx    int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.idx >= len(c.chunks) {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[c.idx])
	c.idx++
	return n, nil
}
