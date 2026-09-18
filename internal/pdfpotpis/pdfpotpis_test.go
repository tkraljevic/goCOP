package pdfpotpis

import (
	"fmt"
	"testing"
	"time"
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

// Kako izgleda PDF iz SIGNATOR-a: parafa se bilježi kvalificiranim pečatom
// HRVATSKE VODE s imenom parafista u razlogu, zatim potpis rukovoditelja, a
// iza svakog potpisa dopišu se podaci za dugoročnu provjeru (DSS).
func TestPotpisIzSignatora(t *testing.T) {
	pdf := []byte("%PDF-1.6\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n%%EOF\n")
	pc, pk, err := ProbniPecat("HRVATSKE VODE", "28921383001")
	if err != nil {
		t.Fatal(err)
	}
	razlog := "<FEFF"
	for _, r := range "Mario Spajić 12.02.2026. 07:03 - " {
		razlog += fmt.Sprintf("%04X", r)
	}
	razlog += ">"
	pdf, err = ProbnoPotpisiS(pdf, pc, pk, "/Reason "+razlog+" /M (D:20260212070319Z)")
	if err != nil {
		t.Fatal(err)
	}
	dss := "\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm 3 0 R /DSS 4 0 R >>\nendobj\n3 0 obj\n<< /Fields [] /SigFlags 3 /DR << /Font << /Helv 5 0 R >> >> >>\nendobj\n4 0 obj\n<< /Certs [6 0 R] /OCSPs [7 0 R] >>\nendobj\n6 0 obj\n<< /Length 4 >>\nstream\n\x00/Annots\x01\nendstream\nendobj\n%%EOF\n"
	pdf = append(pdf, dss...)
	oc, ok, _ := ProbniCertifikat("ŽELJKO KOVAČEVIĆ", "07283093909", true)
	pdf, err = ProbnoPotpisiS(pdf, oc, ok, "/M (D:20260212143407Z)")
	if err != nil {
		t.Fatal(err)
	}
	pdf = append(pdf, dss...)

	ps := Pronadji(pdf)
	if len(ps) != 2 {
		t.Fatalf("potpisa: %d", len(ps))
	}
	if !ps[0].Pecat || ps[0].Razlog != "Mario Spajić 12.02.2026. 07:03" || ps[0].CijeliPDF {
		t.Errorf("pečat: %+v", ps[0])
	}
	o := Osobni(ps)
	if o == nil || o.Ime != "ŽELJKO KOVAČEVIĆ" || o.Pecat || !o.QSCD || !o.Ispravan || !o.CijeliPDF || !o.DodanLTV {
		t.Fatalf("osobni potpis: %+v", o)
	}
	// PAdES potpis ne nosi vrijeme u CMS-u; tada vrijedi /M iz rječnika
	if v := vrijemeIzRjecnika(rjecnikPotpisa(pdf, []byte("/M (D:20260212143407Z) /ByteRange"))); !v.Equal(time.Date(2026, 2, 12, 14, 34, 7, 0, time.UTC)) {
		t.Errorf("vrijeme iz /M: %v", v)
	}
	if Zadnji(ps) == nil {
		t.Error("dodani podaci za provjeru ne smiju poništiti potpis")
	}

	// dopisana stranica iza potpisa nije podatak za provjeru
	izmijenjen := append(append([]byte{}, pdf...), "\n8 0 obj\n<< /Type /Page /Parent 2 0 R /Contents 9 0 R >>\nendobj\n1 0 obj\n<< /DSS 4 0 R >>\nendobj\n%%EOF\n"...)
	if Zadnji(Pronadji(izmijenjen)) != nil {
		t.Error("izmjena stranica iza potpisa mora poništiti pokrivenost")
	}
}
