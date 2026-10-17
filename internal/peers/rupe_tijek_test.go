package peers

// Tijek razgovora s rupama u nizu (G4b u docs/plan-web-sync.md §3), poruku
// po poruku i za obje uloge, kao u granica_odbijenih_tijek_test.go. Druga
// strana (skripta) ovdje javlja i oznaku rupe/1, pa iza granice odbijenih
// obje strane pošalju poruku rupa, redom uloge, i prazne. Bez oznake
// (skripte u razgovor_tijek_test.go i granica_odbijenih_tijek_test.go)
// poruke rupa nema, a delta ne nosi Rupe ni Popunu.

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// kljucTreceg je ključ trećeg autora, ureda; čvor u njemu ima w2, a
// traži w1
const kljucTreceg = "windows-ured|ocitanja/16/2026"

var (
	treciW1 = stalnaVerzija("0190a000-0000-7000-8000-000000000601", "windows-ured", "ocitanja/16/2026", "r-21", `{"cm":140}`)
	treciW2 = stalnaVerzija("0190a000-0000-7000-8000-000000000602", "windows-ured", "ocitanja/16/2026", "r-22", `{"cm":141}`)
)

func init() {
	imenaVerzija[treciW1.VersionID], imenaVerzija[treciW2.VersionID] = "w1", "w2"
}

// granicaSRupama je granica novijeg čvora koji javlja i rupe/1 i ima
// zadane verzije (ključ → najveća)
func granicaSRupama(frontier map[string]string) frontierMsg {
	if frontier == nil {
		frontier = map[string]string{}
	}
	return frontierMsg{Frontier: frontier, Wants: &Wants{All: true}, Program: "0.0.39-alfa",
		Znanje: razmjena.Znanje{Mogucnosti: []string{razmjena.OznakaGranicaOdbijenih, razmjena.OznakaRazmjena, razmjena.OznakaRupe}}}
}

// sRupama je granica druge strane kako je exchange vrati
func sRupama(frontier string) string {
	return `granica {"frontier":{` + frontier + `},"wants":{"all":true},"program":"0.0.39-alfa","mogucnosti":["granica-odbijenih/1","razmjena/1","rupe/1"]}`
}

// sRupamaDo je granica druge strane pomaknuta do zadane verzije čvora
func sRupamaDo(v string) string { return sRupama(`"` + kljucCvora + `":"` + v + `"`) }

const sRupamaNepomaknuta = `granica {"frontier":{},"wants":{"all":true},"program":"0.0.39-alfa","mogucnosti":["granica-odbijenih/1","razmjena/1","rupe/1"]}`

// trazimCvora je poruka rupa koju čvor pošalje
func trazimCvora(trazim string) string { return odCvora(kindRupe, `{"trazim":`+trazim+`}`) }

// trazimDrugog: druga strana javi što traži
func trazimDrugog(trazim map[string]raspon) potez {
	if trazim == nil {
		trazim = map[string]raspon{}
	}
	return salje(kindRupe, rupeMsg{Trazim: trazim})
}

// tokSRupamaPozivatelja je pravilan razgovor kad čvor (s v1) zove drugu
// stranu s oznakama: nitko ništa ne traži, a druga strana šalje d1
func tokSRupamaPozivatelja() []potez {
	return []potez{
		cita(granicaV1), salje(kindFrontier, granicaSRupama(nil)),
		cita(odbijeneCvora(`{}`)), odbijeneDrugog(nil),
		cita(trazimCvora(`{}`)), trazimDrugog(nil),
		cita(deltaV1), salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}),
		cita(zeljePrazne), salje(kindZelje, zeljeMsg{}),
		cita(sadrzajPrazan), salje(kindSadrzaj, sadrzajMsg{}),
		cita(krajCvora(1)), salje(kindDone, doneMsg{Applied: 1}),
	}
}

// tokSRupamaPozvanog je isti razgovor kad druga strana zove čvor
func tokSRupamaPozvanog() []potez {
	return []potez{
		salje(kindFrontier, granicaSRupama(nil)), cita(granicaV1),
		odbijeneDrugog(nil), cita(odbijeneCvora(`{}`)),
		trazimDrugog(nil), cita(trazimCvora(`{}`)),
		salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}}), cita(deltaV1),
		salje(kindZelje, zeljeMsg{}), cita(zeljePrazne),
		salje(kindSadrzaj, sadrzajMsg{}), cita(sadrzajPrazan),
		salje(kindDone, doneMsg{Applied: 1}), cita(krajCvora(1)),
	}
}

// indeksiRupa su indeksi poteza: granica druge strane, poruka rupa čvora,
// poruka rupa druge strane, delta čvora i delta druge strane
func indeksiRupa(pozivatelj bool) (tok []potez, granica, mojeRupe, njihoveRupe, mojaDelta, njihovaDelta int) {
	if pozivatelj {
		return tokSRupamaPozivatelja(), 1, 4, 5, 6, 7
	}
	return tokSRupamaPozvanog(), 0, 5, 4, 7, 6
}

// zapisRupa je redak zapisa: rupe čvora, poredane po ključu
func zapisRupa(b *Service) func() []string {
	return func() []string {
		rupe, err := citajRupe(ctxRub, b.db)
		if err != nil {
			return []string{"rupe se ne čitaju: " + err.Error()}
		}
		var retci []string
		for _, k := range slices.Sorted(maps.Keys(rupe)) {
			retci = append(retci, fmt.Sprintf("%s odbijeno=(%s,%s] trazi=(%s,%s]", k, rupe[k].odbijeno.Od, rupe[k].odbijeno.Do, rupe[k].trazi.Od, rupe[k].trazi.Do))
		}
		return []string{"rupe " + strings.Join(retci, "; ")}
	}
}

// bezRupa obriše zapis rupa čvora
func bezRupa(t *testing.T, b *Service) func() {
	return func() {
		if _, err := b.db.Exec(`DELETE FROM rupe_knjige`); err != nil {
			t.Fatal(err)
		}
	}
}

// Pravilan razgovor s drugom stranom koja javlja rupe/1: poruka rupa ide
// iza granice odbijenih, prazna kad nitko ništa ne traži, a ostatak
// razgovora ide kao inače. Za obje uloge.
func TestTijekRupaPrazna(t *testing.T) {
	a, b := cistiPar(t)
	ishod := []string{"ishod applied=1 sent=1 err=nil", sRupamaDo(tijekV1), obicno, "knjiga v1 d1", "rupe "}
	for _, pozivatelj := range []bool{true, false} {
		tok, _, _, _, _, _ := indeksiRupa(pozivatelj)
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{{ime: "prazna", pripremi: bezRupa(t, b), nakon: zapisRupa(b), ishod: ishod, potezi: append(tok, cita(krajVeze))}})
	}
}

// Čvor traži dio ključa trećeg autora (w1, a ima w2) i dobije ga popunom:
// w1 stigne s Popunom od početka tražene rupe, pa se trazi isprazni. Bez
// Popune, s Popunom za ključ koji nije tražen, koja ne počinje gdje rupa ili
// ide iza nje, i uz Rupe druge strane trazi ostaje. Za obje uloge.
func TestTijekRupaCvorTrazi(t *testing.T) {
	a, b := cistiPar(t)
	trazeno := raspon{Do: treciW1.VersionID}
	trazim := `{"` + kljucTreceg + `":{"od":"","do":"` + treciW1.VersionID + `"}}`
	pripremi := func() {
		bezRupa(t, b)()
		upisiStalno(t, b, treciW2)
		upisiRupuTrazi(t, b, kljucTreceg, trazeno)
	}
	saW1 := func(rupe, popuna map[string]raspon) potez {
		return salje(kindDelta, deltaMsg{Versions: []ledger.Version{treciW1}, Rupe: rupe, Popuna: popuna})
	}
	pokriveno := map[string]raspon{kljucTreceg: trazeno}
	ostaje := "rupe " + kljucTreceg + " odbijeno=(,] trazi=(," + treciW1.VersionID + "]"
	granica := sRupama(`"` + kljucCvora + `":"` + tijekV1 + `","` + kljucTreceg + `":"` + treciW2.VersionID + `"`)
	granicaCvora := cita(odCvora(kindFrontier, `{"frontier":{"`+kljucCvora+`":"`+tijekV1+`","`+kljucTreceg+`":"`+treciW2.VersionID+`"},"wants":{"all":true},`+programCvora+`,"mogucnosti":`+naseOznake+`}`))
	for _, pozivatelj := range []bool{true, false} {
		tok, g, moje, _, _, njihova := indeksiRupa(pozivatelj)
		tok = zamijeni(zamijeni(tok, moje, cita(trazimCvora(trazim))), g, salje(kindFrontier, granicaSRupama(map[string]string{kljucTreceg: treciW2.VersionID})))
		tok = zamijeni(tok, 1-g, granicaCvora)
		sl := func(ime string, d potez, rupe string) slucaj {
			return slucaj{ime: ime, pripremi: pripremi, nakon: zapisRupa(b), potezi: append(zamijeni(tok, njihova, d), cita(krajVeze)),
				ishod: []string{"ishod applied=1 sent=1 err=nil", granica, obicno, "knjiga v1 w1 w2", rupe}}
		}
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			sl("popunjeno", saW1(nil, pokriveno), "rupe "),
			sl("bez popune", saW1(nil, nil), ostaje),
			sl("popuna netraženog", saW1(nil, map[string]raspon{"drugi|ocitanja/16/2026": trazeno}), ostaje),
			sl("popuna od drugog početka", saW1(nil, map[string]raspon{kljucTreceg: {Od: "0", Do: treciW1.VersionID}}), ostaje),
			sl("popuna iza rupe", saW1(nil, map[string]raspon{kljucTreceg: {Do: treciW2.VersionID}}), ostaje),
			sl("popunjeno uz rupu", saW1(pokriveno, pokriveno), ostaje),
			sl("popunjeno uz neispravnu rupu", saW1(map[string]raspon{kljucTreceg: {Od: "1", Do: "1"}}, pokriveno), ostaje),
		})
	}
}

// Druga strana s rupama: neispravna rupa ne jamči ni za što primljeno, pa
// čvor koji nije ništa tražio počne tražiti; rupa ključa koji nije u delti
// se zanemari. Za obje uloge.
func TestNeispravnaRupaIPopunaDruge(t *testing.T) {
	a, b := cistiPar(t)
	saD1 := func(rupe map[string]raspon) potez {
		return salje(kindDelta, deltaMsg{Versions: []ledger.Version{dolaznaD1}, Rupe: rupe})
	}
	trazi := "rupe " + kljucDrugog + " odbijeno=(,] trazi=(," + tijekD1 + "]"
	predugo := strings.Repeat("0", najduljaOznaka+1)
	for _, pozivatelj := range []bool{true, false} {
		tok, _, _, _, _, njihova := indeksiRupa(pozivatelj)
		sl := func(ime string, d potez, rupe string) slucaj {
			return slucaj{ime: ime, pripremi: bezRupa(t, b), nakon: zapisRupa(b), potezi: append(zamijeni(tok, njihova, d), cita(krajVeze)),
				ishod: []string{"ishod applied=1 sent=1 err=nil", sRupamaDo(tijekV1), obicno, "knjiga v1 d1", rupe}}
		}
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			sl("rupa", saD1(map[string]raspon{kljucDrugog: {Do: tijekD1}}), trazi),
			sl("rupa iza poslanoga", saD1(map[string]raspon{kljucDrugog: {Do: beskraj}}), trazi),
			sl("prazna rupa", saD1(map[string]raspon{kljucDrugog: {Od: tijekD1, Do: tijekD1}}), trazi),
			sl("preduga oznaka", saD1(map[string]raspon{kljucDrugog: {Od: predugo, Do: predugo + "1"}}), trazi),
			sl("ključ izvan delte", saD1(map[string]raspon{"drugi|ocitanja/16/2026": {Do: tijekD1}}), "rupe "),
		})
	}
}

// Druga strana traži dio ključa čvora: čvor ga pošalje od početka rupe, na
// kraju delte, s Popunom; ono što ne prati, gdje ne jamči i ono čega nema
// ne šalje. Za obje uloge.
func TestTijekRupaDrugaTrazi(t *testing.T) {
	a, b := cistiPar(t)
	saV2 := func() { upisiStalno(t, b, vlastitaV2) }
	tuzi := func(ime string, trazim map[string]raspon, granica frontierMsg, delta string, pozivatelj bool) slucaj {
		tok, g, _, njihove, mojaDelta, _ := indeksiRupa(pozivatelj)
		tok = zamijeni(zamijeni(zamijeni(tok, g, salje(kindFrontier, granica)), njihove, trazimDrugog(trazim)), mojaDelta, cita(delta))
		tok = zamijeni(tok, 1-g, cita(granicaV2))
		return slucaj{ime: ime, pripremi: saV2, potezi: append(tok, cita(krajVeze)),
			ishod: []string{"ishod applied=1 sent=" + fmt.Sprint(strings.Count(delta, `"version_id"`)) + " err=nil", sRupamaDo(tijekV2), obicno, "knjiga v1 v2 d1"}}
	}
	do := func(v string) frontierMsg { return granicaSRupama(map[string]string{kljucCvora: v}) }
	cijela := `{"versions":[` + jsonV1 + `,` + jsonV2 + `],"popuna":{"` + kljucCvora + `":{"od":"","do":"` + tijekV2 + `"}}}`
	doV1 := `{"versions":[` + jsonV1 + `],"popuna":{"` + kljucCvora + `":{"od":"","do":"` + tijekV1 + `"}}}`
	// između v1 i v2 nema ničega
	izmedju := tijekV1 + "5"
	prazna := `{"versions":null,"popuna":{"` + kljucCvora + `":{"od":"` + tijekV1 + `","do":"` + izmedju + `"}}}`
	bezIcega := `{"versions":null}`
	for _, pozivatelj := range []bool{true, false} {
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			tuzi("cijela", map[string]raspon{kljucCvora: {Do: tijekV2}}, do(tijekV2), odCvora(kindDelta, cijela), pozivatelj),
			tuzi("do kraja rupe", map[string]raspon{kljucCvora: {Do: tijekV1}}, do(tijekV2), odCvora(kindDelta, doV1), pozivatelj),
			// druga strana ima samo v1: ono iznad ide redovno, a popuna samo do v1
			tuzi("do tuđe granice", map[string]raspon{kljucCvora: {Do: tijekV2}}, do(tijekV1), odCvora(kindDelta, `{"versions":[`+jsonV2+`,`+jsonV1+`],"popuna":{"`+kljucCvora+`":{"od":"","do":"`+tijekV1+`"}}}`), pozivatelj),
			// ničega u rupi: popuna bez verzija pokrije cijelu rupu
			tuzi("ničega u rupi", map[string]raspon{kljucCvora: {Od: tijekV1, Do: izmedju}}, do(tijekV2), odCvora(kindDelta, prazna), pozivatelj),
			tuzi("ključ koji čvor nema", map[string]raspon{"drugi|ocitanja/16/2026": {Do: tijekV2}}, do(tijekV2), odCvora(kindDelta, bezIcega), pozivatelj),
			tuzi("rupa iznad tuđe granice", map[string]raspon{kljucCvora: {Od: tijekV2, Do: tijekVIza}}, do(tijekV2), odCvora(kindDelta, bezIcega), pozivatelj),
		})
	}
}

// Na koraku rupa: veza nestane ili padne, stigne druga vrsta poruke ili
// odbijenica. Razgovor stane greškom kao na drugim koracima. Pokvarena
// poruka rupa, previše ključeva i predug ključ nisu greška: zanemare se,
// pa čvor ne popunjava ništa. Za obje uloge.
func TestTijekRupaRubovi(t *testing.T) {
	a, b := cistiPar(t)
	stao := func(greska string) []string {
		return []string{"ishod applied=0 sent=0 err=" + greska, sRupamaNepomaknuta, obicno, "knjiga v1"}
	}
	cijeli := []string{"ishod applied=1 sent=1 err=nil", sRupamaDo(tijekV1), obicno, "knjiga v1 d1"}
	druga := salje(kindDelta, deltaMsg{})
	drugaGreska := `očekivana poruka "rupe", stigla "delta"`
	odbijenica := odbijaKodom(razmjena.KodZauzet, razmjena.RazlogZauzet)
	zauzet := "odbijeno 0x0102: čvor je zauzet, pokušajte kasnije"
	pokvarena := saljeSirovo("rupe pokvarene", razmjena.Envelope{Kind: kindRupe, DeviceID: "cop-osijek", Payload: json.RawMessage(`{"trazim":[1]}`)})
	previse := map[string]raspon{}
	for i := range najviseTrazenih + 1 {
		previse[fmt.Sprintf("cvor-%d|ocitanja/16/2026", i)] = raspon{Do: tijekV1}
	}
	predug := map[string]raspon{strings.Repeat("k", najduljiKljuc+1): {Do: tijekV1}}
	for _, pozivatelj := range []bool{true, false} {
		tok, _, moje, njihove, _, _ := indeksiRupa(pozivatelj)
		provjeriSlucajeve(t, a, b, pozivatelj, []slucaj{
			{ime: "nestane prije rupa", potezi: do(tok, njihove), ishod: stao("kraj veze")},
			{ime: "druga vrsta", potezi: do(tok, njihove, druga, cita(krajVeze)), ishod: stao(drugaGreska)},
			{ime: "odbijenica", potezi: do(tok, njihove, odbijenica, cita(krajVeze)), ishod: stao(zauzet)},
			{ime: "pokvarena", potezi: append(zamijeni(tok, njihove, pokvarena), cita(krajVeze)), ishod: cijeli},
			{ime: "previše ključeva", potezi: append(zamijeni(tok, njihove, trazimDrugog(previse)), cita(krajVeze)), ishod: cijeli},
			{ime: "predug ključ", potezi: append(zamijeni(tok, njihove, trazimDrugog(predug)), cita(krajVeze)), ishod: cijeli},
			// sljedeće pisanje čvora je njegova poruka rupa
			{ime: "pisanje rupa", potezi: do(tok, moje-1, kvar(), tok[moje-1], cita(krajVeze)), ishod: stao("veza pala")},
		})
	}
}
