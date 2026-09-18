package pdfpotpis

import (
	"testing"
)

// Potpis dodan na kraj PDF-a čita se i provjerava; izmjena sadržaja nakon
// potpisa ga ruši; certifikat bez izjave o kvalifikaciji se prepozna
func TestPotpisUPDF(t *testing.T) {
	izvorni := []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")
	c, k, err := ProbniCertifikat("Mile Kunac", "12345678903", true)
	if err != nil {
		t.Fatal(err)
	}
	potpisan, err := ProbnoPotpisi(izvorni, c, k)
	if err != nil {
		t.Fatal(err)
	}
	ps := Pronadji(potpisan)
	if len(ps) != 1 {
		t.Fatalf("potpisa %d", len(ps))
	}
	p := Zadnji(ps)
	if p == nil || !p.Ispravan || !p.CijeliPDF || !p.Kvalificiran || p.Ime != "Mile Kunac" || p.OIB != "12345678903" || p.Vrijeme.IsZero() {
		t.Fatalf("potpis: %+v", ps[0])
	}

	// izmjena jednog bajta u potpisanom dijelu
	krivo := append([]byte{}, potpisan...)
	krivo[20] = 'X'
	if q := Zadnji(Pronadji(krivo)); q == nil || q.Ispravan {
		t.Errorf("izmijenjen sadržaj ne smije proći: %+v", q)
	}

	// nekvalificiran certifikat
	c2, k2, _ := ProbniCertifikat("Netko Drugi", "", false)
	p2, _ := ProbnoPotpisi(izvorni, c2, k2)
	if q := Zadnji(Pronadji(p2)); q == nil || !q.Ispravan || q.Kvalificiran {
		t.Errorf("nekvalificiran certifikat: %+v", q)
	}

	// PDF bez potpisa
	if len(Pronadji(izvorni)) != 0 {
		t.Error("nepotpisan PDF nema potpisa")
	}
}
