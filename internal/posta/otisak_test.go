package posta

import "testing"

// Otisak poslužitelja ne ovisi o velikim slovima ni o tome je li EWS upisan
// kao poslužitelj ili puni URL, a mijenja se s adresom, portom, zaštitom
// veze, načinom i domenom. Bez poslužitelja je prazan.
func TestOtisakPosluzitelja(t *testing.T) {
	ews := Postavke{Nacin: NacinEWS, Posluzitelj: "owa.voda.hr", Domena: "VODA"}
	smtp := Postavke{Nacin: NacinSMTP, Posluzitelj: "smtp.voda.hr", Sigurnost: STARTTLS, Domena: "voda"}
	for _, x := range []struct {
		opis string
		a, b Postavke
		isti bool
	}{
		{"puni URL EWS-a", ews, Postavke{Nacin: NacinEWS, Posluzitelj: "https://OWA.voda.hr/EWS/Exchange.asmx", Domena: "voda"}, true},
		{"zadani port SMTP-a", smtp, Postavke{Nacin: NacinSMTP, Posluzitelj: "SMTP.voda.hr", Port: 587, Domena: "voda"}, true},
		{"drugi poslužitelj", ews, Postavke{Nacin: NacinEWS, Posluzitelj: "owa.drugi.test", Domena: "VODA"}, false},
		{"druga domena", ews, Postavke{Nacin: NacinEWS, Posluzitelj: "owa.voda.hr", Domena: "DRUGA"}, false},
		{"drugi port", smtp, Postavke{Nacin: NacinSMTP, Posluzitelj: "smtp.voda.hr", Port: 2525, Domena: "voda"}, false},
		{"druga zaštita veze na istom portu", Postavke{Nacin: NacinSMTP, Posluzitelj: "smtp.voda.hr", Port: 465, Sigurnost: TLS},
			Postavke{Nacin: NacinSMTP, Posluzitelj: "smtp.voda.hr", Port: 465, Sigurnost: STARTTLS}, false},
		{"drugi način", Postavke{Nacin: NacinSMTP, Posluzitelj: "owa.voda.hr", Domena: "VODA"}, ews, false},
	} {
		if got := x.a.Otisak() == x.b.Otisak(); got != x.isti {
			t.Errorf("%s: %q i %q", x.opis, x.a.Otisak(), x.b.Otisak())
		}
	}
	if o := (Postavke{Nacin: NacinEWS}).Otisak(); o != "" {
		t.Errorf("bez poslužitelja: %q", o)
	}
}
