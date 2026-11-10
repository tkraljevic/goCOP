package web

import (
	"testing"
)

// Razdoblje i dan na obračunu i IORS-u pišu se kao na crtežu: godina jednom
// kad je ista, inače uz oba dana; nepročitan dan ostaje kako je upisan.
func TestRasponDanaIDanKratko(t *testing.T) {
	for _, s := range []struct{ od, do, isto string }{
		{"2026-10-12", "2026-10-26", "12. 10. – 26. 10. 2026."},
		{"2025-12-28", "2026-01-03", "28. 12. 2025. – 3. 1. 2026."},
		{"krivo", "2026-01-03", "krivo – 2026-01-03"},
	} {
		if r := rasponDana(s.od, s.do); r != s.isto {
			t.Errorf("rasponDana(%q, %q) = %q, a mora %q", s.od, s.do, r, s.isto)
		}
	}
}
