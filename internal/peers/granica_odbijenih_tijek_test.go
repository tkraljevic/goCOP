package peers

// Tijek razgovora s granicom odbijenih (G4 u docs/plan-web-sync.md §3),
// poruku po poruku i za obje uloge, kao u razgovor_tijek_test.go. Druga
// strana (skripta) ovdje javlja oznaku granica-odbijenih/1, pa odmah iza
// granica obje strane pošalju granicu odbijenih, redom uloge. Bez oznake
// (skripta u razgovor_tijek_test.go glumi čvor 0.0.38) te poruke nema, i
// kad čvor ima što javiti (TestTijekBezOznakeNemaGraniceOdbijenih).

import (
	"encoding/json"
	"fmt"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

const (
	jsonV2      = `{"version_id":"0190a000-0000-7000-8000-000000000002","entity":"probni","entity_id":"r-2","node_id":"laptop-baranja","supersedes":"","archived":false,"payload":{"cm":121},"created_at":"2026-10-15T08:00:00Z","schema_version":1,"channel":"ocitanja/16/2026"}`
	kljucCvora  = "laptop-baranja|ocitanja/16/2026"
	kljucDrugog = "cop-osijek|ocitanja/16/2026"
	// tijekVIza je verzija čvora koju on nema, iza svih njegovih
	tijekVIza = "0190a000-0000-7000-8000-0000000000ff"
)

// granicaSOznakom je granica novijeg čvora koji javlja granica-odbijenih/1 i
// još nema ništa
var granicaSOznakom = frontierMsg{Frontier: map[string]string{}, Wants: &Wants{All: true}, Program: "0.0.39-alfa",
	Znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaGranicaOdbijenih, razmjena.OznakaRazmjena}}}

// granica druge strane kako je exchange vrati, prije pomaka
const sOznakomNepomaknuta = `granica {"frontier":{},"wants":{"all":true},"program":"0.0.39-alfa","mogucnosti":["granica-odbijenih/1","razmjena/1"]}`

// sOznakomDo je granica druge strane kako je exchange vrati, pomaknuta do
// zadane verzije čvora
func sOznakomDo(v string) string {
	return `granica {"frontier":{"` + kljucCvora + `":"` + v + `"},"wants":{"all":true},"program":"0.0.39-alfa","mogucnosti":["granica-odbijenih/1","razmjena/1"]}`
}

// odbijeneCvora je granica odbijenih koju čvor pošalje
func odbijeneCvora(granica string) string {
	return odCvora(kindGranicaOdbijenih, `{"granica":`+granica+`}`)
}

// odbijeneDrugog: druga strana javi što je od čvora odbila
func odbijeneDrugog(granica map[string]string) potez {
	return salje(kindGranicaOdbijenih, odbijeneMsg{Granica: granica})
}

// tokSOznakomPozivatelja je pravilan razgovor kad čvor (s v1) zove drugu
// stranu s oznakom: nijedna nije ništa odbila, a druga strana šalje d1
func tokSOznakomPozivatelja() []potez {
	return []potez{
		cita(granicaV1), salje(kindFrontier, granicaSOznakom),
		cita(odbijeneCvora(`{}`)), odbijeneDrugog(nil),
		cita(deltaV1), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}),
		cita(zeljePrazne), salje(kindZelje, zeljeMsg{}),
		cita(sadrzajPrazan), salje(kindSadrzaj, sadrzajMsg{}),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
	}
}

// tokSOznakomPozvanog je isti razgovor kad druga strana zove čvor
func tokSOznakomPozvanog() []potez {
	return []potez{
		salje(kindFrontier, granicaSOznakom), cita(granicaV1),
		odbijeneDrugog(nil), cita(odbijeneCvora(`{}`)),
		salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(deltaV1),
		salje(kindZelje, zeljeMsg{}), cita(zeljePrazne),
		salje(kindSadrzaj, sadrzajMsg{}), cita(sadrzajPrazan),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
	}
}

// odbijenoOdDrugog je redak zapisa: što čvor b pamti da je odbio od druge
// strane a
func odbijenoOdDrugog(a, b *Service) func() []string {
	return func() []string {
		_, z := b.odbijeneOd(a.node.PublicKey(), b.ogradaSada(), b.sadOdbijenih())
		zapis, _ := json.Marshal(z.granica)
		return []string{"odbijeno od drugog " + string(zapis)}
	}
}

// Pravilan razgovor s drugom stranom koja javlja oznaku: granica odbijenih
// ide odmah iza granica, prazna kad nijedna strana nije ništa odbila, a
// ostatak razgovora ide kao inače. Za obje uloge.
func TestTijekGranicaOdbijenihPrazna(t *testing.T) {
	a, b := cistiPar(t)
	ishod := []string{"ishod applied=1 sent=1 err=nil", sOznakomDo(tijekV1), obicno, "knjiga v1 d1"}
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "prazna", ishod: ishod, potezi: append(tokSOznakomPozivatelja(), cita(krajVeze))}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "prazna", ishod: ishod, potezi: append(tokSOznakomPozvanog(), cita(krajVeze))}})
}

// Druga strana javi da je od čvora odbila do v1: čvor šalje tek iza toga
// (v2). Kad javi i više nego što čvor ima, i ključ koji čvor nema, čvor ne
// šalje ništa, a za ploču granicu druge strane pomakne samo do svoje
// najveće verzije. Za obje uloge.
func TestTijekGranicaOdbijenihDrugeStrane(t *testing.T) {
	a, b := cistiPar(t)
	saV2 := func() { upisiStalno(t, b, vlastitaV2) }
	doV1 := odbijeneDrugog(map[string]string{kljucCvora: tijekV1})
	prekoSvega := odbijeneDrugog(map[string]string{kljucCvora: tijekVIza, "tudji-cvor|ocitanja/16/2026": tijekVIza})
	deltaV2 := odCvora(kindDelta, `{"versions":[`+jsonV2+`]}`)
	for _, pozivatelj := range []bool{true, false} {
		// indeksi poteza: granica čvora, granica odbijenih druge strane i delta čvora
		tok, f, g, d := tokSOznakomPozvanog(), 1, 2, 5
		if pozivatelj {
			tok, f, g, d = tokSOznakomPozivatelja(), 0, 3, 4
		}
		tok = zamijeni(tok, f, cita(granicaV2))
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			{ime: "do v1", pripremi: saV2, potezi: append(zamijeni(zamijeni(tok, g, doV1), d, cita(deltaV2)), cita(krajVeze)),
				ishod: []string{"ishod applied=1 sent=1 err=nil", sOznakomDo(tijekV2), obicno, "knjiga v1 v2 d1"}},
			{ime: "preko svega", pripremi: saV2, potezi: append(zamijeni(zamijeni(tok, g, prekoSvega), d, cita(deltaPrazna)), cita(krajVeze)),
				ishod: []string{"ishod applied=1 sent=0 err=nil", sOznakomDo(tijekV2), obicno, "knjiga v1 v2 d1"}},
		})
	}
}

// Čvor odbije d2 (Accept) i to javi u sljedećem razgovoru, samo ono iznad
// svoje granice knjige. Pamti po drugoj strani, tek kad je upis prošao.
// Za obje uloge.
func TestTijekGranicaOdbijenihCvora(t *testing.T) {
	a, b := cistiPar(t)
	b.Accept(func(v ledger.Version) bool { return v.EntityID != dolaznaD2.EntityID })
	zaboravi := func() { b.zaboraviOdbijene() }
	sD2 := salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1, dolaznaD2}})
	odbijenoD2 := fmt.Sprintf(`{"%s":"%s"}`, kljucDrugog, tijekD2)
	ishod := []string{"ishod applied=1 sent=1 err=nil", sOznakomDo(tijekV1), obicno, "knjiga v1 d1", "odbijeno od drugog " + odbijenoD2}
	for _, pozivatelj := range []bool{true, false} {
		// indeksi poteza: granica odbijenih čvora i delta druge strane
		tok, g, d := tokSOznakomPozvanog(), 3, 4
		if pozivatelj {
			tok, g, d = tokSOznakomPozivatelja(), 2, 5
		}
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			{ime: "odbije d2", pripremi: zaboravi, nakon: odbijenoOdDrugog(a, b), potezi: append(zamijeni(tok, d, sD2), cita(krajVeze)), ishod: ishod},
			// knjiga je opet samo v1 (provjeriSlucajeve), pa je d2 iznad nje;
			// d1 stigne opet i zapamćeno ostaje
			{ime: "javi d2", nakon: odbijenoOdDrugog(a, b), potezi: append(zamijeni(tok, g, cita(odbijeneCvora(odbijenoD2))), cita(krajVeze)), ishod: ishod},
		})
	}
}

// Razgovor s drugom stranom bez oznake (čvor 0.0.38) ide bajt po bajt kao
// prije G4 i kad čvor od nje pamti odbijeno: granica odbijenih ne ode, a
// zapamćeno se zaboravi. Za obje uloge.
func TestTijekBezOznakeNemaGraniceOdbijenih(t *testing.T) {
	a, b := cistiPar(t)
	zapamti := func() {
		b.zapamtiOdbijene(b.pocetakOdbijenih(a.node.PublicKey(), nil), map[string]string{kljucDrugog: tijekD2})
	}
	ishod := append(nakonDelte("nil"), "odbijeno od drugog null")
	provjeriSlucajeve(t, a, b, true, []slucaj{{ime: "bez oznake", pripremi: zapamti, nakon: odbijenoOdDrugog(a, b), ishod: ishod,
		potezi: append(tokPozivatelja(), cita(krajVeze))}})
	provjeriSlucajeve(t, a, b, false, []slucaj{{ime: "bez oznake", pripremi: zapamti, nakon: odbijenoOdDrugog(a, b), ishod: ishod,
		potezi: append(tokPozvanog(), cita(krajVeze))}})
}

// Na koraku granice odbijenih: veza nestane ili padne, stigne druga vrsta
// poruke ili odbijenica. Razgovor stane greškom kao na drugim koracima, s
// granicom druge strane koja je stigla. Pokvarena granica odbijenih nije
// greška: zanemari se, pa čvor šalje sve. Za obje uloge.
func TestTijekGranicaOdbijenihRubovi(t *testing.T) {
	a, b := cistiPar(t)
	stao := func(greska string) []string {
		return []string{"ishod applied=0 sent=0 err=" + greska, sOznakomNepomaknuta, obicno, "knjiga v1"}
	}
	druga := salje(kindDelta, deltaMsg{})
	drugaGreska := `očekivana poruka "granica-odbijenih", stigla "delta"`
	odbijenica := odbijaKodom(razmjena.KodZauzet, razmjena.RazlogZauzet)
	zauzet := "odbijeno 0x0102: čvor je zauzet, pokušajte kasnije"
	pokvarena := saljeSirovo("granica-odbijenih pokvarena", razmjena.Envelope{Kind: kindGranicaOdbijenih, DeviceID: "cop-osijek", Payload: json.RawMessage(`{"granica":[1]}`)})
	bezPayloada := saljeSirovo("granica-odbijenih bez payloada", razmjena.Envelope{Kind: kindGranicaOdbijenih, DeviceID: "cop-osijek"})
	cijeli := []string{"ishod applied=1 sent=1 err=nil", sOznakomDo(tijekV1), obicno, "knjiga v1 d1"}
	tok := tokSOznakomPozivatelja()
	provjeriSlucajeve(t, a, b, true, []slucaj{
		{ime: "nestane nakon granice odbijenih", potezi: do(tok, 3), ishod: stao("kraj veze")},
		{ime: "pisanje granice odbijenih", potezi: do(tok, 1, kvar(), tok[1], cita(krajVeze)), ishod: stao("veza pala")},
		{ime: "druga vrsta", potezi: do(tok, 3, druga, cita(krajVeze)), ishod: stao(drugaGreska)},
		{ime: "odbijenica", potezi: do(tok, 3, odbijenica, cita(krajVeze)), ishod: stao(zauzet)},
		{ime: "pokvarena", potezi: append(zamijeni(tok, 3, pokvarena), cita(krajVeze)), ishod: cijeli},
		{ime: "bez payloada", potezi: append(zamijeni(tok, 3, bezPayloada), cita(krajVeze)), ishod: cijeli},
	})
	tok = tokSOznakomPozvanog()
	provjeriSlucajeve(t, a, b, false, []slucaj{
		{ime: "nestane prije granice odbijenih", potezi: do(tok, 2), ishod: stao("kraj veze")},
		{ime: "pisanje granice odbijenih", potezi: do(tok, 2, kvar(), tok[2], cita(krajVeze)), ishod: stao("veza pala")},
		{ime: "druga vrsta", potezi: do(tok, 2, druga, cita(krajVeze)), ishod: stao(drugaGreska)},
		{ime: "odbijenica", potezi: do(tok, 2, odbijenica, cita(krajVeze)), ishod: stao(zauzet)},
		{ime: "pokvarena", potezi: append(zamijeni(tok, 2, pokvarena), cita(krajVeze)), ishod: cijeli},
		{ime: "bez payloada", potezi: append(zamijeni(tok, 2, bezPayloada), cita(krajVeze)), ishod: cijeli},
	})
}
