package lock

import "golang.org/x/sys/unix"

// tiocgsid asks a terminal for its session id. The constant is arch-specific
// on Linux (0x5429 on x86/arm, 0x40047485 elsewhere), so it comes from
// x/sys rather than a literal.
const tiocgsid = unix.TIOCGSID
