package javnivodostaji

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Ponovni pokušaj ide samo kad je upit istekao, ne kad je izvor odbio.
func TestIsteklo(t *testing.T) {
	for _, s := range []struct {
		err  error
		zeli bool
	}{
		{fmt.Errorf(`Get "https://vizugy.hu/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), true},
		{fmt.Errorf("čitanje: %w", context.DeadlineExceeded), true},
		{errors.New("HTTP 403"), false},
		{errors.New("tablica je prazna"), false},
	} {
		if g := isteklo(s.err); g != s.zeli {
			t.Errorf("isteklo(%v) = %v, želi %v", s.err, g, s.zeli)
		}
	}
}
