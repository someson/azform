package lock

// tiocgsid asks a terminal for its session id. golang.org/x/sys/unix does
// not export TIOCGSID for darwin, so the value is spelled out: it is
// _IOR('t', 99, int), identical on amd64 and arm64 because int is 4 bytes
// on both.
const tiocgsid = 0x40047463
