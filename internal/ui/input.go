package ui

import "io"

// termFile mirrors charmbracelet/x/term.File — the exact interface
// bubbletea's initInput probes for (io.ReadWriteCloser + Fd()) to put
// the terminal into raw mode. Failing this probe leaves the tty
// line-buffered and kills interactive input.
type termFile interface {
	io.ReadWriteCloser
	Fd() uintptr
}

// NewKeyNormalizer wraps r so that terminal escape sequences bubbletea
// does not natively decode get rewritten into equivalents it does. As
// of bubbletea v1.3.10 the static keymap has no entries for the SS3
// forms of Home (\x1bOH) and End (\x1bOF) — iTerm2's default profile
// emits exactly these, and unrecognised sequences leak through as
// literal characters. The wrapper translates them into their CSI
// equivalents (\x1b[H, \x1b[F) which bubbletea decodes correctly as
// KeyHome/KeyEnd. SS3 arrows (\x1bO[ABCD]) are left alone because
// bubbletea already recognises them.
//
// When src also satisfies term.File (e.g. it is a *os.File / tty), the
// returned value forwards Write/Close/Fd so bubbletea's raw-mode init
// still works.
func NewKeyNormalizer(src io.Reader) io.Reader {
	base := &keyNormalizer{src: src}
	if tf, ok := src.(termFile); ok {
		return &keyNormalizerFile{keyNormalizer: base, file: tf}
	}
	return base
}

// keyNormalizerFile makes the wrapper satisfy term.File by delegating
// Write/Close/Fd to the wrapped tty. Read still goes through the
// normalizer so escape-sequence translation applies.
type keyNormalizerFile struct {
	*keyNormalizer
	file termFile
}

// Write forwards to the underlying tty.
func (k *keyNormalizerFile) Write(p []byte) (int, error) { return k.file.Write(p) }

// Close forwards to the underlying tty.
func (k *keyNormalizerFile) Close() error { return k.file.Close() }

// Fd returns the underlying tty's file descriptor so bubbletea can
// switch the terminal into raw mode during initInput.
func (k *keyNormalizerFile) Fd() uintptr { return k.file.Fd() }

// keyNormalizer buffers up to two carry-over bytes across Read calls so
// a three-byte SS3 sequence split across reads still translates.
type keyNormalizer struct {
	src   io.Reader
	carry []byte // 0, 1, or 2 bytes held for the next Read
}

func (k *keyNormalizer) Read(p []byte) (int, error) {
	// Read into a scratch buffer, prefixed by any carry-over from the
	// previous call. Translate in place, then decide how many trailing
	// bytes to hold back as a new carry (0, 1, or 2 bytes that could
	// still be the start of an SS3 sequence).
	scratch := make([]byte, len(k.carry)+len(p))
	copy(scratch, k.carry)
	n, err := k.src.Read(scratch[len(k.carry):])
	total := len(k.carry) + n
	if total == 0 {
		return 0, err
	}
	scratch = scratch[:total]
	k.carry = nil

	// Translate: \x1bO followed by 'H' or 'F' → \x1b[<same tail>.
	for i := 0; i+2 < len(scratch); i++ {
		if scratch[i] == 0x1b && scratch[i+1] == 'O' {
			if scratch[i+2] == 'H' || scratch[i+2] == 'F' {
				scratch[i+1] = '['
			}
		}
	}

	// If EOF, we cannot receive more bytes — flush everything and skip
	// the carry-over dance.
	if err == io.EOF {
		w := copy(p, scratch)
		if w < len(scratch) {
			// Extremely unlikely: caller's buffer smaller than our
			// scratch. Hold the rest for next call (but there is no
			// next call after EOF, so this is best-effort).
			k.carry = append([]byte(nil), scratch[w:]...)
			return w, nil
		}
		return w, io.EOF
	}

	// Hold back only a trailing two-byte \x1bO prefix so a split-across-
	// reads SS3 Home/End still translates. Bare trailing \x1b is NOT
	// held: users press Esc to cancel edit mode, and buffering that
	// byte would either delay the cancel indefinitely or fuse ESC with
	// the next keystroke into Alt+X. iTerm2 emits full SS3 sequences
	// atomically in the common case, so losing translation on the rare
	// \x1b + "OH" split is an acceptable trade.
	hold := 0
	if len(scratch) >= 2 && scratch[len(scratch)-2] == 0x1b && scratch[len(scratch)-1] == 'O' {
		hold = 2
	}

	emit := len(scratch) - hold
	if hold > 0 {
		k.carry = append([]byte(nil), scratch[emit:]...)
	}
	w := copy(p, scratch[:emit])
	if w < emit {
		// Caller's buffer smaller than what we want to emit — hold the
		// leftover ahead of the carry so ordering is preserved.
		leftover := append([]byte(nil), scratch[w:emit]...)
		k.carry = append(leftover, k.carry...)
	}
	return w, nil
}
