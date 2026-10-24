package razmjena

import "syscall"

// Na Windowsima connectex i slanje vraćaju brojeve WSAE*, a
// syscall.ECONNREFUSED i ostali ovdje su izmišljene vrijednosti koje se s
// njima ne podudaraju: WSAECONNREFUSED 10061, WSAENETUNREACH 10051,
// WSAEHOSTDOWN 10064, WSAEHOSTUNREACH 10065.
var (
	greskeOdbijeno = []error{syscall.ECONNREFUSED, syscall.Errno(10061)}
	greskeBezRute  = []error{syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.Errno(10051), syscall.Errno(10064), syscall.Errno(10065)}
)
