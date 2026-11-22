package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Letva glavnog lanca ima obrazac: veličina i način računa joj se ne biraju,
// rezerva smije imati dva ulaza, a ručnog nagiba i primjene nema.
func TestStranicaLetveGlavnogLanca(t *testing.T) {
	h, _ := rukovateljRacuna(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	html := stranicaRacuna(t, h, "?sektor=B&vodotok="+oznakaImena("Brodska rijeka")+"&letva=donji-miholjac", admin)
	redom(t, html, "<span>Donji Brod</span>", "protok · ulaz: ", `<span class="rac-cip">Glavni lanac</span>`,
		`<h2 id="rac-obrazac-naslov">Donji Brod</h2>`, `name="metoda" value="lanac"`,
		"Letva je u glavnom lancu Drave i Dunava i vodi se u protoku", `name="rezerva"`, `name="rezerva_b"`,
		"Rezerva 4", `value="spremi" class="btn btn-primary">Spremi nacrt`, "proba mjeri cijeli lanac dvaput", "do nekoliko minuta")
	for _, nema := range []string{`name="velicina"`, "Letva s malo arhive", `value="primijeni"`, `value="dnevni"`, porukaULancu} {
		if strings.Contains(html, nema) {
			t.Errorf("letva glavnog lanca ne smije imati %q", nema)
		}
	}
}

// Svaki račun glavnog lanca stane u obrazac: najviše dva ulaza i četiri rezerve
// od po najviše dva ulaza. Inače bi spremanje nacrta tiho skratilo račun.
func TestSvakiRacunLancaStaneUObrazac(t *testing.T) {
	for _, tok := range prognoza.Tokovi {
		for _, r := range tok.Racuni {
			z, ima := prognoza.ZapisIzLanca(r.Letva)
			if !ima {
				t.Fatalf("%s nije u lancu", r.Letva)
			}
			o := racuniModela{}.obrazac(r.Letva, r.Letva)
			if o.Zakljucano != "" || !o.Glavna || o.Velicina != z.Velicina || len(z.Racun.Ulazi) > len(o.Ulazi) || len(z.Racun.Rezerve) > len(o.Rezerve) {
				t.Errorf("%s ne stane u obrazac: %+v", r.Letva, z.Racun)
				continue
			}
			f := url.Values{"razine_nacin": {razineZadane}, "ulaz": o.Ulazi}
			for _, rez := range o.Rezerve {
				f.Add("rezerva", rez.Prvi)
				f.Add("rezerva_b", rez.Drugi)
			}
			natrag, err := zapisIzObrasca(f, r.Letva, z)
			if err != nil || natrag.Velicina != z.Velicina || !reflect.DeepEqual(natrag.Racun.Ulazi, z.Racun.Ulazi) ||
				len(natrag.Racun.Rezerve) != len(z.Racun.Rezerve) || (len(z.Racun.Rezerve) > 0 && !reflect.DeepEqual(natrag.Racun.Rezerve, z.Racun.Rezerve)) {
				t.Errorf("%s: iz obrasca %+v, u lancu %+v, %v", r.Letva, natrag.Racun, z.Racun, err)
			}
		}
	}
}

// Iz obrasca letve glavnog lanca veličina ostaje ona iz lanca, dnevni model se
// ne prima, a rezerva se slaže iz oba svoja polja.
func TestZapisGlavneLetveIzObrasca(t *testing.T) {
	stari, _ := prognoza.ZapisIzLanca("donji-miholjac")
	f := url.Values{"metoda": {"dnevni"}, "velicina": {"vodostaj"}, "ulaz": {"vrbovka", ""}, "razine_nacin": {razineZadane},
		"rezerva": {"moslavina", "", "belisce", ""}, "rezerva_b": {"", "", " osijek ", "barcs"}}
	z, err := zapisIzObrasca(f, "donji-miholjac", stari)
	if err != nil || z.Metoda != prognoza.MetodaLanac || z.Velicina != "protok" || !reflect.DeepEqual(z.Racun.Ulazi, []string{"vrbovka"}) ||
		!reflect.DeepEqual(z.Racun.Rezerve, [][]string{{"moslavina"}, {"belisce", "osijek"}, {"barcs"}}) {
		t.Errorf("zapis letve glavnog lanca: %+v, %v", z, err)
	}
	rm := racuniModela{zapisi: map[string]prognoza.ZapisLetve{"donji-miholjac": z}}
	if o := rm.obrazac("donji-miholjac", "Donji Brod"); !o.Glavna || o.Stanje != oznakaModel ||
		o.Rezerve[1] != (RezervaObrasca{Prvi: "belisce", Drugi: "osijek"}) || len(o.Rezerve) != najviseRezerviLanca {
		t.Errorf("obrazac iz zapisa: %+v", o)
	}
	if opis := (racuniModela{}).opis("donji-miholjac", nil); !strings.HasPrefix(opis, "protok · ulaz: ") {
		t.Errorf("opis letve glavnog lanca: %q", opis)
	}
	if r := rezervaUObrascu(nil); r != (RezervaObrasca{}) {
		t.Errorf("prazna rezerva: %+v", r)
	}
}

// Proba letve glavnog lanca pokazuje uz račun s izmjenom i račun sadašnjim
// modelom, a ispod letve nizvodno kojima se promašaj promijenio.
func TestProbaLancaNaStranici(t *testing.T) {
	proba := &prognoza.ProbaNacrta{Kad: "22. 11. 2026. 14:00", Razdoblje: []string{"2023-01-01", "2025-12-31"},
		Dosezi: []prognoza.MjeraDosega{{Doseg: 24, Promasaj: 5.2, BezModela: 9, Slucaja: 2100, Stanje: prognoza.MjeraBolje},
			{Doseg: 96, Promasaj: 20, BezModela: 21, Slucaja: 2000, Stanje: prognoza.MjeraIsto}},
		Lanac: &prognoza.ProbaLanca{Prije: []prognoza.MjeraDosega{{Doseg: 24, Promasaj: 6.1, Slucaja: 2100, Stanje: prognoza.MjeraBolje},
			{Doseg: 96, Slucaja: 3, Stanje: prognoza.MjeraNijeProvjereno}},
			Nizvodno: []prognoza.PromjenaNizvodno{{Letva: "donji-miholjac", Velicina: "vodostaj", Doseg: 24, Prije: 12.3, Poslije: 14},
				{Letva: "nepoznata", Velicina: "protok", Doseg: 48, Prije: 30, Poslije: 28.5}}}}
	p := probaRacuna(proba, "vodostaj")
	if !p.Lanac || p.Dosezi[0].Sada != "6,1" || p.Dosezi[1].Sada != "—" || len(p.Nizvodno) != 2 ||
		!p.Nizvodno[0].Losije || p.Nizvodno[1].Losije || p.Nizvodno[1].Jedinica != "m³/s" {
		t.Fatalf("proba lanca: %+v", p)
	}
	var nista *ProbaRacuna
	nista.imenuj(nil)
	p.imenuj(map[string]models.Station{"donji-miholjac": {Name: "Donji Brod"}})
	if p.Nizvodno[0].Ime != "Donji Brod" || p.Nizvodno[1].Ime != "nepoznata" {
		t.Errorf("imena nizvodno: %+v", p.Nizvodno)
	}
	h, _ := rukovateljRacuna(t)
	r := zahtjevOsobe(http.MethodGet, "/prognoze/model/racun", "", &models.UserPermissions{IsGlobalAdmin: true})
	obrazac := &ObrazacRacuna{Kod: "donji-miholjac", Ime: "Donji Brod", Glavna: true, Velicina: "vodostaj", Ulazi: []string{"a", ""},
		Rezerve: make([]RezervaObrasca, najviseRezerviLanca), NacinRazina: razineZadane, Kad: "13:00", Pripremljen: "13:01", Proba: p}
	data := RacunPrognozePageData{PrognozePageData: h.zaglavlje(r), Sektor: SektorPrognoze{ID: "B"}, Vodotok: VodotokPrognoze{Ime: "Brodska rijeka"},
		SmijeUrediti: true, Obrazac: obrazac}
	w := httptest.NewRecorder()
	if err := h.racunTmpl.ExecuteTemplate(w, "prognoze_racun.html", data); err != nil {
		t.Fatal(err)
	}
	redom(t, w.Body.String(), `role="columnheader">S izmjenom (cm)</span>`, `role="columnheader">Sada (cm)</span>`, "+24 h", "5,2", "6,1",
		"Letve nizvodno", "Donji Brod · +24 h", "12,3 → 14,0 cm (lošije)", "nepoznata · +48 h", "30,0 → 28,5 m³/s", "Probaj ponovno")
	p.Nizvodno = nil
	w = httptest.NewRecorder()
	if err := h.racunTmpl.ExecuteTemplate(w, "prognoze_racun.html", data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "Nijednoj drugoj letvi promašaj se nije promijenio.") {
		t.Error("proba bez promjena nizvodno mora to reći")
	}
}

// citacSRetkom je čitač baze prognoza u kojoj letva glavnog lanca ima redak s
// prethodnim računom.
func citacSRetkom(t *testing.T) *CitacPrognoza {
	t.Helper()
	put := filepath.Join(t.TempDir(), "prognoze.db")
	pb, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pb.Exec(`INSERT INTO model_letve (letva, proba, sadrzaj) VALUES ('donji-miholjac', '', '{"oblik":1,"letva":"donji-miholjac",
		"metoda":"lanac","velicina":"protok","racun":{"ulazi":["vrbovka"],"doseg":96},
		"promjene":[{"kad":"22. 11. 2026. 15:00","tko":"Iva Proba","razlog":"proba"}],
		"prethodni":{"oblik":1,"letva":"donji-miholjac","metoda":"lanac","velicina":"protok","racun":{"ulazi":["moslavina"],"doseg":96}}}')`); err != nil {
		t.Fatal(err)
	}
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Primjena na glavni lanac kaže da je prognoza odmah izdana, a prethodni račun
// se ne vraća izravno nego učita u nacrt, pa ide kroz pripremu i probu.
func TestPrimjenaIPovratakGlavneLetveSaStranice(t *testing.T) {
	h, u := rukovateljRacuna(t)
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	polja := func(radnja string) url.Values {
		return url.Values{"sektor": {"B"}, "vodotok": {oznakaImena("Brodska rijeka")}, "letva": {"donji-miholjac"}, "radnja": {radnja},
			"razlog": {"bliže izmjerenom"}}
	}
	if lok := posaljiRacun(h, polja("primijeni"), admin, false); !strings.Contains(lok, "primijenjen i prognoza je izdana novim modelom") ||
		len(u.primijenjeni) != 1 || !strings.HasPrefix(u.primijenjeni[0], "donji-miholjac|") {
		t.Errorf("primjena: %q, %v", lok, u.primijenjeni)
	}
	if lok := posaljiRacun(h, polja("vrati-nacrt"), admin, false); !strings.Contains(lok, "Model nema prethodnu inačicu") || len(u.spremljeni) != 0 {
		t.Errorf("povratak bez retka: %q", lok)
	}
	c := citacSRetkom(t)
	h.citac = func() *CitacPrognoza { return c }
	if lok := posaljiRacun(h, polja("vrati-nacrt"), admin, false); !strings.Contains(lok, "učitan je u nacrt") || len(u.spremljeni) != 1 ||
		u.spremljeni[0].Zapis.Racun.Ulazi[0] != "moslavina" || u.spremljeni[0].Tko == "" || len(u.spremljeni[0].Zapis.Promjene) != 0 {
		t.Errorf("povratak u nacrt: %q, %+v", lok, u.spremljeni)
	}
	html := stranicaRacuna(t, h, "?sektor=B&vodotok="+oznakaImena("Brodska rijeka")+"&letva=donji-miholjac", admin)
	redom(t, html, `<span class="rac-cip dobro">Model</span>`, "Primijenjeni model", "22. 11. 2026. 15:00 · Iva Proba",
		`value="vrati-nacrt" class="btn prog-obrub">Učitaj prethodni račun u nacrt`, "Model se mijenja istim putem")
	for _, nema := range []string{`value="ukloni"`, `value="vrati"`} {
		if strings.Contains(html, nema) {
			t.Errorf("letva glavnog lanca ne smije imati %q", nema)
		}
	}
	h.citac = nil
	if err := h.prethodniUNacrt("donji-miholjac", "Iva Proba"); err == nil {
		t.Error("bez baze prognoza nema prethodnog računa")
	}
	if _, err := (*CitacPrognoza)(nil).PrethodniGlavni("donji-miholjac"); err == nil {
		t.Error("čitač bez baze mora javiti grešku")
	}
}

// Uz probanu letvu glavnog lanca stranica nudi primjenu, kaže da se prognoza
// izdaje odmah i upozorava kad je nekoj letvi nizvodno gore.
func TestPrimjenaGlavneLetveNaStranici(t *testing.T) {
	proba := probaRacuna(&prognoza.ProbaNacrta{Dosezi: []prognoza.MjeraDosega{{Doseg: 24, Promasaj: 5, BezModela: 9, Slucaja: 2000, Stanje: prognoza.MjeraBolje}},
		Lanac: &prognoza.ProbaLanca{Nizvodno: []prognoza.PromjenaNizvodno{{Letva: "belisce", Velicina: "protok", Doseg: 24, Prije: 10, Poslije: 12},
			{Letva: "belisce", Velicina: "protok", Doseg: 48, Prije: 15, Poslije: 18}, {Letva: "osijek", Velicina: "vodostaj", Doseg: 24, Prije: 9, Poslije: 8}}}}, "protok")
	if proba.LosijihNizvodno != 1 {
		t.Fatalf("letvi kojima je gore: %d", proba.LosijihNizvodno)
	}
	h, _ := rukovateljRacuna(t)
	r := zahtjevOsobe(http.MethodGet, "/prognoze/model/racun", "", &models.UserPermissions{IsGlobalAdmin: true})
	data := RacunPrognozePageData{PrognozePageData: h.zaglavlje(r), Sektor: SektorPrognoze{ID: "B"}, Vodotok: VodotokPrognoze{Ime: "Brodska rijeka"},
		SmijeUrediti: true, Obrazac: &ObrazacRacuna{Kod: "donji-miholjac", Ime: "Donji Brod", Glavna: true, Velicina: "protok", Ulazi: []string{"a", ""},
			Rezerve: make([]RezervaObrasca, najviseRezerviLanca), NacinRazina: razineZadane, Kad: "13:00", Pripremljen: "13:01", Proba: proba}}
	w := httptest.NewRecorder()
	if err := h.racunTmpl.ExecuteTemplate(w, "prognoze_racun.html", data); err != nil {
		t.Fatal(err)
	}
	redom(t, w.Body.String(), "Primijeni", "Na 1 letvi nizvodno promašaj je s ovom izmjenom veći nego sada", `name="razlog"`,
		`value="primijeni" class="btn btn-primary">Primijeni model`, "prognoza se odmah izdaje iznova, za sve letve")
}
