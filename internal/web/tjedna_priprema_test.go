package web

import (
	gohtml "html"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Promašaji u ovim provjerama su izmišljeni.

func terminSubote(d int) time.Time { return time.Date(2026, 11, d, 2, 0, 0, 0, models.Zagreb) }

// Blok „Tjedna priprema” po crtežima: primijenjena, nije primijenjena,
// preskočena, tri puta zaredom i lanac koji slabi.
func TestPrikazTjedne(t *testing.T) {
	sljedeca := terminSubote(29)
	nazivi := map[string]string{"botovo": "Botovo", "terezino-polje": "Terezino Polje"}
	lanacOd := time.Date(2026, 11, 15, 2, 0, 0, 0, models.Zagreb)
	nije := prognoza.TjednaPriprema{Kad: terminSubote(22), Ishod: prognoza.TjednaNijePrimijenjena, Prije: 12.4, Poslije: 12.9, LanacOd: lanacOd,
		Losije: []prognoza.LosijaLetva{{Letva: "botovo", Velicina: "vodostaj", Razlika: 3}, {Letva: "terezino-polje", Velicina: "vodostaj", Razlika: 2}}}
	for _, s := range []struct {
		ime      string
		pripreme []prognoza.TjednaPriprema
		zelim    []string
	}{
		{"primijenjena", []prognoza.TjednaPriprema{{Kad: terminSubote(22), Ishod: prognoza.TjednaPrimijenjena, Prije: 12.4, Poslije: 11.9}},
			[]string{"Zadnja: subota 22. 11. 2026. u 2:00 · primijenjena", "promašaj 1.–4. dan: 12,4 → 11,9 cm", `rac-cip dobro">bolje`,
				"Sljedeća: subota 29. 11. 2026. u 2:00"}},
		{"nije primijenjena", []prognoza.TjednaPriprema{nije},
			[]string{`rac-cip pozor">nije primijenjena`, "nije primijenjena: novi lanac bio je lošiji (12,4 → 12,9 cm), ostaje lanac od 15. 11.",
				"lošije: Botovo +3 cm, Terezino Polje +2 cm"}},
		{"preskočena", []prognoza.TjednaPriprema{{Kad: terminSubote(22), Ishod: prognoza.TjednaPreskocena, Razlog: "traje obrana od poplava (B-3/2026)"}},
			[]string{`rac-cip">preskočena`, "Preskočena: traje obrana od poplava (B-3/2026)"}},
		{"zaredom", []prognoza.TjednaPriprema{nije, nije, nije},
			[]string{"3 puta zaredom", "Tjedna priprema 3 puta zaredom nije prošla probu. Provjerite arhivu"}},
		{"slabi", []prognoza.TjednaPriprema{{Kad: terminSubote(22), Ishod: prognoza.TjednaPrimijenjena, Prije: 14.6, Poslije: 14.6, Slabi: true, Osnovica: 11.9}},
			[]string{"potreban pregled", "primijenjena, ali bez pomaka (14,6 → 14,6 cm)",
				"Prognoza je lošija nego kad je lanac primijenjen (promašaj 1.–4. dan 11,9 → 14,6 cm), a novi lanac ne pomaže."}},
	} {
		p := PrikazTjedne(StanjeTjedne{Pripreme: s.pripreme, Sljedeca: sljedeca, Izdaje: true, Nazivi: nazivi})
		html := gohtml.UnescapeString(iscrtaj(t, "prognoze_postavke.html", PrognozePageData{MozePripremiti: true, Tjedna: &p}))
		for _, z := range s.zelim {
			if !strings.Contains(html, z) {
				t.Errorf("%s: nema %q", s.ime, z)
			}
		}
		if !strings.Contains(html, "Pripremi model</button>") {
			t.Errorf("%s: nema gumba za ručnu pripremu", s.ime)
		}
	}
	p := PrikazTjedne(StanjeTjedne{})
	if html := iscrtaj(t, "prognoze_postavke.html", PrognozePageData{MozePripremiti: true, Tjedna: &p}); !strings.Contains(html, "Ovo računalo ne izdaje prognozu") {
		t.Error("računalo koje ne izdaje")
	}
}

// Upozorenje tjedne pripreme je redak u Za riješiti globalnog administratora.
func TestZaRijesitiModelPrognoze(t *testing.T) {
	k := karticaZaRijesiti(stavkeZaRijesiti{Model: prognoza.UpozorenjeZaredom}, time.Now())
	if k.Broj != 1 || k.Redovi[0].Glavno != "Model prognoze: tjedna priprema 3 puta zaredom nije prošla probu" ||
		k.Redovi[0].URL != "/prognoze/postavke#priprema" || k.Redovi[0].Sporedno != "Postavke prognoze" {
		t.Errorf("redak %+v", k.Redovi)
	}
}

// Neuspjela priprema, protok na popisu lošijih i letva bez naziva; blok i
// redak Za riješiti dolaze samo kad program daje stanje.
func TestTjednaPripremaRubovi(t *testing.T) {
	p := PrikazTjedne(StanjeTjedne{Izdaje: true, Sljedeca: terminSubote(29), Pripreme: []prognoza.TjednaPriprema{
		{Kad: terminSubote(22), Ishod: prognoza.TjednaGreska, Razlog: "arhiva je zauzeta"}}})
	if p.Zadnja != "Zadnja: subota 22. 11. 2026. u 2:00 · nije uspjela: arhiva je zauzeta" || p.Oznaka != "nije uspjela" {
		t.Errorf("neuspjela: %+v", p)
	}
	if p := PrikazTjedne(StanjeTjedne{Izdaje: true, Sljedeca: terminSubote(29)}); p.Zadnja != "Zadnja: još nije bilo" {
		t.Errorf("bez priprema: %+v", p)
	}
	if l := losijeLetve([]prognoza.LosijaLetva{{Letva: "he-dubrava", Velicina: "protok", Razlika: 12}}, nil); l != "lošije: he-dubrava +12 m³/s" {
		t.Errorf("protok: %q", l)
	}
	if p := PrikazTjedne(StanjeTjedne{Izdaje: true, Pripreme: []prognoza.TjednaPriprema{
		{Kad: terminSubote(22), Ishod: prognoza.TjednaNijePrimijenjena, Prije: 1, Poslije: 2}}}); strings.Contains(p.Zadnja, "ostaje lanac") {
		t.Errorf("bez dana lanca: %q", p.Zadnja)
	}

	s := &Server{}
	if s.stanjeTjedne() != nil || tjedniPrikaz(true, s.stanjeTjedne) != nil || upozorenjeModela(nil) != "" || upozorenjeModela(s.stanjeTjedne) != "" {
		t.Error("bez programa")
	}
	s.SetTjednaPriprema(func() StanjeTjedne {
		return StanjeTjedne{Izdaje: true, Pripreme: []prognoza.TjednaPriprema{{Ishod: prognoza.TjednaPrimijenjena, Slabi: true}}}
	})
	if tjedniPrikaz(false, s.stanjeTjedne) != nil || tjedniPrikaz(true, nil) != nil {
		t.Error("blok bez prava ili bez stanja")
	}
	if b := tjedniPrikaz(true, s.stanjeTjedne); b == nil || b.Oznaka != "potreban pregled" {
		t.Errorf("blok %+v", b)
	}
	if u := upozorenjeModela(s.stanjeTjedne); u != prognoza.UpozorenjeSlabi || len(redakModela("")) != 0 {
		t.Errorf("upozorenje %q", u)
	}
}
