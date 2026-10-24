//go:build !windows

package razmjena

import "syscall"

// EHOSTDOWN javlja macOS za računalo u istoj podmreži koje se ne javlja
var (
	greskeOdbijeno = []error{syscall.ECONNREFUSED}
	greskeBezRute  = []error{syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EHOSTDOWN}
)
