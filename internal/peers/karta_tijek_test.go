package peers

// Tijek razgovora s dijelovima karte, poruku po poruku i za obje uloge, kao
// u razgovor_tijek_test.go. Druga strana (skripta) javlja oznaku
// dijelovi-karte/1, pa želje čvora nose otiske dijelova koji mu nedostaju,
// a sadržaj čvora dijelove koje druga strana traži. Bez oznake (skripta
// 0.0.38) poruke su iste kao prije, i kad čvor kartu treba ili je ima
// (TestTijekBezOznakeNemaDijelovaKarte).

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// granicaSKartom je granica novijeg čvora koji javlja dijelovi-karte/1 i još
// nema ništa
var granicaSKartom = frontierMsg{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.47-alfa",
	Znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaDijeloviKarte, razmjena.OznakaRazmjena}}}

// granica druge strane kako je exchange vrati, pomaknuta do v1
const sKartomDoV1 = `granica {"frontier":{"laptop-baranja|ocitanja/16/2026":"` + tijekV1 + `"},"wants":{"all":true},"program":"0.0.47-alfa","mogucnosti":["dijelovi-karte/1","razmjena/1"]}`

// zeljeDijelovaCvora su želje čvora s otiscima, kako idu žicom
func zeljeDijelovaCvora(otisci []string) string {
	return odCvora(kindZelje, `{"otisci":["`+strings.Join(otisci, `","`)+`"]}`)
}

// dijeloviCvora je sadržaj čvora s dijelovima karte, kako ide žicom
func dijeloviCvora(k models.VektorskaKarta, b []byte, redom ...int) string {
	var stavke []string
	for _, i := range redom {
		d := dio(k, b, i)
		stavke = append(stavke, fmt.Sprintf(`{"otisak":"%s","vrsta":"karta-dio","bajtova":%d,"podaci":"%s"}`, k.Dijelovi[i], len(d), base64.StdEncoding.EncodeToString(d)))
	}
	return odCvora(kindSadrzaj, `{"stavke":[`+strings.Join(stavke, ",")+`]}`)
}

// dijeloviDrugog: druga strana pošalje dijelove karte
func dijeloviDrugog(k models.VektorskaKarta, b []byte, redom ...int) potez {
	var m sadrzajMsg
	for _, i := range redom {
		d := dio(k, b, i)
		m.Stavke = append(m.Stavke, stavkaSadrzaja{Otisak: k.Dijelovi[i], Vrsta: VrstaDijelaKarte, Bajtova: len(d), Podaci: d})
	}
	return salje(kindSadrzaj, m)
}

// tokKarte je pravilan razgovor s drugom stranom s oznakom: čvor (s v1)
// pošalje svoje želje i sadržaj, druga strana d1, svoje želje i sadržaj
func tokKarte(pozivatelj bool, granicaDruge frontierMsg, zeljeC, sadrzajC string, zeljeD, sadrzajD potez) []potez {
	if pozivatelj {
		return []potez{
			cita(granicaV1), salje(kindFrontier, granicaDruge),
			cita(deltaV1), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}),
			cita(zeljeC), zeljeD,
			cita(sadrzajC), sadrzajD,
			cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}), cita(krajVeze),
		}
	}
	return []potez{
		salje(kindFrontier, granicaDruge), cita(granicaV1),
		salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(deltaV1),
		zeljeD, cita(zeljeC),
		sadrzajD, cita(sadrzajC),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)), cita(krajVeze),
	}
}

// kartaCvora je redak zapisa: koliko dijelova karte čvor ima i je li složena
func kartaCvora(s *Service) func() []string {
	return func() []string {
		d := s.karte.Load()
		d.pozadina.Wait()
		st := s.StanjeKarte(ctxRub)
		return []string{fmt.Sprintf("karta %d od %d, složena %v", st.Dijelova, st.Svih, st.Slozena)}
	}
}

// Čvor kojem karta nedostaje traži sve dijelove (stanu u ogradu) i upiše ih,
// a čvor koji je ima daje tražene dijelove; za obje uloge
func TestTijekDijeloviKarte(t *testing.T) {
	k, b, izvor := kartaUDijelovima(t, 1000, 128, 41)
	o := &opisKarte{}
	o.postavi(&k)
	a, s := cistiPar(t)
	ishod := []string{"ishod applied=1 sent=1 err=nil", sKartomDoV1, obicno, "knjiga v1 d1"}
	sviDijelovi := []int{0, 1, 2, 3, 4, 5, 6, 7}
	for _, pozivatelj := range []bool{true, false} {
		trazi := func() { s.PostaviKarte(t.TempDir(), o.citaj, nil) }
		daje := func() { s.PostaviKarte(izvor, o.citaj, nil) }
		provjeriSlucajeve(t, a, s, pozivatelj, []slucaj{
			{ime: "čvor traži", pripremi: trazi, nakon: kartaCvora(s), ishod: append(ishod, "karta 8 od 8, složena true"),
				potezi: tokKarte(pozivatelj, granicaSKartom, zeljeDijelovaCvora(k.Dijelovi), sadrzajPrazan, salje(kindZelje, zeljeMsg{}), dijeloviDrugog(k, b, sviDijelovi...))},
			{ime: "čvor daje", pripremi: daje, nakon: kartaCvora(s), ishod: append(ishod, "karta 8 od 8, složena true"),
				potezi: tokKarte(pozivatelj, granicaSKartom, zeljePrazne, dijeloviCvora(k, b, 0, 7), salje(kindZelje, zeljeMsg{Otisci: []string{k.Dijelovi[0], k.Dijelovi[7]}}), salje(kindSadrzaj, sadrzajMsg{}))},
			{ime: "druga strana šalje netraženo i krivo", pripremi: trazi, nakon: kartaCvora(s), ishod: append(ishod, "karta 1 od 8, složena false"),
				potezi: tokKarte(pozivatelj, granicaSKartom, zeljeDijelovaCvora(k.Dijelovi), sadrzajPrazan, salje(kindZelje, zeljeMsg{}),
					salje(kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
						{Otisak: k.Dijelovi[0], Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 1)},
						{Otisak: strings.Repeat("ab", 32), Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 1)},
						{Otisak: k.Dijelovi[2], Vrsta: VrstaDijelaKarte, Podaci: dio(k, b, 2)},
					}}))},
		})
	}
}

// Bez oznake druge strane poruke su iste kao prije: čvor kojem karta
// nedostaje ne traži dijelove, a čvor koji je ima ne daje ih ni kad ih
// druga strana traži; za obje uloge
func TestTijekBezOznakeNemaDijelovaKarte(t *testing.T) {
	k, _, izvor := kartaUDijelovima(t, 1000, 128, 42)
	o := &opisKarte{}
	o.postavi(&k)
	a, s := cistiPar(t)
	for _, pozivatelj := range []bool{true, false} {
		provjeriSlucajeve(t, a, s, pozivatelj, []slucaj{
			{ime: "čvor kartu treba", pripremi: func() { s.PostaviKarte(t.TempDir(), o.citaj, nil) }, nakon: kartaCvora(s),
				ishod:  append(nakonDelte("nil"), "karta 0 od 8, složena false"),
				potezi: tokKarte(pozivatelj, granicaDrugog, zeljePrazne, sadrzajPrazan, salje(kindZelje, zeljeMsg{}), salje(kindSadrzaj, sadrzajMsg{}))},
			{ime: "čvor kartu ima", pripremi: func() { s.PostaviKarte(izvor, o.citaj, nil) }, nakon: kartaCvora(s),
				ishod:  append(nakonDelte("nil"), "karta 8 od 8, složena true"),
				potezi: tokKarte(pozivatelj, granicaDrugog, zeljePrazne, sadrzajPrazan, salje(kindZelje, zeljeMsg{Otisci: k.Dijelovi[:2]}), salje(kindSadrzaj, sadrzajMsg{}))},
		})
	}
}
