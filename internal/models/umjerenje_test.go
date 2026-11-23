package models

import "testing"

// Umjerenje vrijedi samo s oba broja i razumnim nagibom.
func TestImaUmjerenje(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, s := range []struct {
		ods, nag *float64
		ima      bool
		uredjaj  int
		letva    int
	}{
		{nil, nil, false, 600, 600},
		{f(300), nil, false, 600, 600},
		{f(300), f(0.1), false, 600, 600},
		{f(300), f(0.6), true, 600, 500},
		{f(-20), f(1.5), true, 580, 400},
	} {
		st := Station{UmjerenjeOdsjecak: s.ods, UmjerenjeNagib: s.nag}
		if st.ImaUmjerenje() != s.ima || st.Umjeri(s.uredjaj) != s.letva {
			t.Errorf("%v %v: ima %v, %d → %d; želim %v, %d", s.ods, s.nag, st.ImaUmjerenje(), s.uredjaj, st.Umjeri(s.uredjaj), s.ima, s.letva)
		}
	}
}
