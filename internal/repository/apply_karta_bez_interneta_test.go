package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// postavkeKarte0045 je zamrznut oblik postavki karata iz v0.0.45-alfa
// (models.PostavkeKarte sa StilObjekta, PodlogaKarte i VlastitaIkona), bez
// karte bez interneta. Ne mijenja se s programom. 0.0.45 zapis „karta”
// pročita u taj oblik, izmijeni i spremi cijeli pri svakom spremanju
// izgleda, podloga, ikona ili „zadano”, pa iz njega ispadne sve što ne zna.
type postavkeKarte0045 struct {
	Objekti       map[string]stilObjekta0045 `json:"objekti,omitempty"`
	Podloge       []podlogaKarte0045         `json:"podloge"`
	Ikone         []vlastitaIkona0045        `json:"ikone,omitempty"`
	SKrugom       bool                       `json:"s_krugom,omitempty"`
	VelicinaTocke int                        `json:"velicina_tocke,omitempty"`
}

type stilObjekta0045 struct {
	Boja     string `json:"boja,omitempty"`
	Debljina int    `json:"debljina,omitempty"`
	Ikona    string `json:"ikona,omitempty"`
}

type podlogaKarte0045 struct {
	Naziv    string `json:"naziv"`
	Vrsta    string `json:"vrsta"`
	URL      string `json:"url"`
	Slojevi  string `json:"slojevi,omitempty"`
	Zasluge  string `json:"zasluge,omitempty"`
	Kljuc    string `json:"kljuc,omitempty"`
	NajviseZ int    `json:"najvise_z,omitempty"`
}

type vlastitaIkona0045 struct {
	Ime     string `json:"ime"`
	Naziv   string `json:"naziv"`
	ViewBox string `json:"viewbox"`
	Sadrzaj string `json:"sadrzaj"`
}

// opisKarteZaRazmjenu je ispravan opis karte s dva dijela
func opisKarteZaRazmjenu() models.VektorskaKarta {
	return models.VektorskaKarta{Naziv: "Baranja", Izvor: "OpenStreetMap (proba)", Otisak: strings.Repeat("ab", 32), Bajtova: 150,
		Obuhvat: [4]float64{18.3, 45.5, 18.95, 45.95}, NajviseZ: 15, Dio: 100, Dijelovi: []string{strings.Repeat("cd", 32), strings.Repeat("ef", 32)}}
}

func verzijaOpisaKarte(t *testing.T, versionID string, k *models.VektorskaKarta) ledger.Version {
	t.Helper()
	vrijednost := ""
	if k != nil {
		b, err := json.Marshal(k)
		nuzno(t, err)
		vrijednost = string(b)
	}
	return verzijaZa(t, EntityPostavke, PostavkaKartaBezInterneta, versionID, Postavka{ID: PostavkaKartaBezInterneta, Vrijednost: vrijednost})
}

// cvorStarogPrograma je čvor O koji radi kao 0.0.45: postavke karata koje
// je napisao novi program (s vlastitom ikonom i stilom, a karta bez
// interneta ide zasebno) primi razmjenom, zatim ih pri spremanju izgleda
// pročita iz baze u oblik iz 0.0.45, izmijeni i spremi. Primjena na O
// mijenja postavke programa, pa se poslije vraćaju na zadano.
func cvorStarogPrograma(t *testing.T, opis models.VektorskaKarta) *cvor {
	t.Helper()
	o := noviCvor(t, "o")
	ik, err := models.ProciscenaIkona("Crpka", []byte(`<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="5"/></svg>`))
	nuzno(t, err)
	nove, err := json.Marshal(models.PostavkeKarte{
		Objekti: map[string]models.StilObjekta{models.StructureKindEmbankment: {Debljina: 4, Ikona: ik.Ime}},
		Podloge: []models.PodlogaKarte{}, Ikone: []models.VlastitaIkona{ik}, Vektorska: &opis,
	})
	nuzno(t, err)
	nuzno(t, primiPaket(t, o, verzijaZa(t, EntityPostavke, PostavkaKarta, vid(1), Postavka{ID: PostavkaKarta, Vrijednost: string(nove)})))

	repo := NewAktiRepository(o.db, o.rec)
	v, err := repo.GetPostavka(ctxRaz, PostavkaKarta)
	nuzno(t, err)
	var p postavkeKarte0045
	nuzno(t, json.Unmarshal([]byte(v), &p))
	p.SKrugom, p.VelicinaTocke = true, 40
	b, err := json.Marshal(p)
	nuzno(t, err)
	nuzno(t, repo.SavePostavka(ctxRaz, PostavkaKarta, string(b)))

	models.SetPostavkeKarte(models.ZadanePostavkeKarte())
	models.SetVektorskaKarta(nil)
	return o
}

// zapisKarte0045 je zapis „karta” koji je čvor O spremio
func zapisKarte0045(t *testing.T, o *cvor) string {
	t.Helper()
	v, err := NewAktiRepository(o.db, o.rec).GetPostavka(ctxRaz, PostavkaKarta)
	nuzno(t, err)
	if strings.Contains(v, "vektorska") || !strings.Contains(v, `"ikone"`) || !strings.Contains(v, `"s_krugom":true`) {
		t.Fatalf("zapis postavki karata s čvora 0.0.45: %s", v)
	}
	return v
}

// verzijaPostavkiKarte0045 je zapis s čvora O kao verzija s poretkom versionID
func verzijaPostavkiKarte0045(t *testing.T, o *cvor, versionID string) ledger.Version {
	t.Helper()
	return verzijaZa(t, EntityPostavke, PostavkaKarta, versionID, Postavka{ID: PostavkaKarta, Vrijednost: zapisKarte0045(t, o)})
}

// sKartomPrograma vraća postavke i kartu programa nakon testa, a za test ih
// postavlja na zadano
func sKartomPrograma(t *testing.T) {
	t.Helper()
	prijeP, prijeK := models.PostavkeKartePrograma(), models.VektorskaKartaPrograma()
	t.Cleanup(func() { models.SetPostavkeKarte(prijeP); models.SetVektorskaKarta(prijeK) })
	models.SetPostavkeKarte(models.ZadanePostavkeKarte())
	models.SetVektorskaKarta(nil)
}

// Čvor 0.0.45 spremi postavke karata (zapis „karta” bez karte bez
// interneta). Opis karte je zaseban zapis, pa ostaje, stigla ta verzija
// prije ili poslije opisa, u istom paketu ili u dva, ili cijela knjiga
// čvora 0.0.45 koji je opis i proslijedio; izgled iz 0.0.45 se primijeni
func TestOpisKartePrezivljavaStariProgram(t *testing.T) {
	opis := opisKarteZaRazmjenu()
	for ime, paketi := range map[string]func(t *testing.T, o *cvor) [][]ledger.Version{
		"0.0.45 poslije opisa": func(t *testing.T, o *cvor) [][]ledger.Version {
			return [][]ledger.Version{{verzijaOpisaKarte(t, vid(1), &opis)}, {verzijaPostavkiKarte0045(t, o, vid(2))}}
		},
		"0.0.45 prije opisa": func(t *testing.T, o *cvor) [][]ledger.Version {
			return [][]ledger.Version{{verzijaPostavkiKarte0045(t, o, vid(1))}, {verzijaOpisaKarte(t, vid(2), &opis)}}
		},
		"zajedno, 0.0.45 novija": func(t *testing.T, o *cvor) [][]ledger.Version {
			return [][]ledger.Version{{verzijaPostavkiKarte0045(t, o, vid(2)), verzijaOpisaKarte(t, vid(1), &opis)}}
		},
		"zajedno, opis noviji": func(t *testing.T, o *cvor) [][]ledger.Version {
			return [][]ledger.Version{{verzijaOpisaKarte(t, vid(2), &opis), verzijaPostavkiKarte0045(t, o, vid(1))}}
		},
		"knjiga čvora 0.0.45": func(t *testing.T, o *cvor) [][]ledger.Version {
			nuzno(t, primiPaket(t, o, verzijaOpisaKarte(t, vid(2), &opis))) // O ga ne tumači, ali ga čuva i prosljeđuje
			models.SetVektorskaKarta(nil)
			sve, err := o.rec.Since(ctxRaz, "", 0)
			nuzno(t, err)
			return [][]ledger.Version{sve}
		},
	} {
		t.Run(ime, func(t *testing.T) {
			sKartomPrograma(t)
			o := cvorStarogPrograma(t, opis)
			c := noviCvor(t, "b")
			for _, p := range paketi(t, o) {
				nuzno(t, primiPaket(t, c, p...))
			}
			k := models.VektorskaKartaPrograma()
			if k == nil || k.Otisak != opis.Otisak || k.Dio != opis.Dio || len(k.Dijelovi) != 2 {
				t.Fatalf("karta programa: %+v", k)
			}
			p := models.PostavkeKartePrograma()
			if p.Vektorska == nil || p.Vektorska.Otisak != opis.Otisak {
				t.Errorf("postavke karata bez karte: %+v", p.Vektorska)
			}
			if !p.SKrugom || p.VelicinaTocke != 40 || len(p.Ikone) != 1 || p.Objekti[models.StructureKindEmbankment].Debljina != 4 {
				t.Errorf("izgled iz 0.0.45 nije primijenjen: %+v", p)
			}
			if redakaNaPovrsini(t, c, "postavke") != 2 {
				t.Error("na površini nisu oba zapisa")
			}
		})
	}
}

// Micanje karte piše prazan opis (ne spomenik) i miče kartu programa;
// starija verzija opisa koja stigne kasnije je ne vraća. Spomenik opisa
// miče i zapis s površine, a spomenik postavki karata ne dira kartu.
func TestMakniKartuZasebnaPostavka(t *testing.T) {
	sKartomPrograma(t)
	opis := opisKarteZaRazmjenu()
	o := cvorStarogPrograma(t, opis)
	c := noviCvor(t, "b")

	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(1), &opis)))
	if models.VektorskaKartaPrograma() == nil {
		t.Fatal("opis karte nije primijenjen")
	}
	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(3), nil)))
	if k := models.VektorskaKartaPrograma(); k != nil {
		t.Fatalf("prazan opis nije maknuo kartu: %+v", k)
	}
	if redakaNaPovrsini(t, c, "postavke") != 1 {
		t.Error("prazan opis nije na površini")
	}
	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(2), &opis)))
	if k := models.VektorskaKartaPrograma(); k != nil {
		t.Errorf("starija verzija vratila je kartu: %+v", k)
	}

	// opis iz razmjene s popisom dijelova koji ovaj program ne prihvaća daje
	// kartu bez dijelova, a opis koji ne valja ni bez njih ne daje kartu
	los := opis
	los.Dijelovi = los.Dijelovi[:1]
	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(4), &los)))
	if k := models.VektorskaKartaPrograma(); k == nil || k.Otisak != opis.Otisak || k.Dio != 0 || k.Dijelovi != nil {
		t.Errorf("opis s krivim popisom dijelova: %+v", k)
	}
	los.NajviseZ = 0
	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(5), &los)))
	if k := models.VektorskaKartaPrograma(); k != nil {
		t.Errorf("neispravan opis dao je kartu: %+v", k)
	}

	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(6), &opis)))
	spomenik := func(id, versionID string) ledger.Version {
		return ledger.Version{VersionID: versionID, Entity: EntityPostavke, EntityID: id, NodeID: "ured", Archived: true,
			Payload: []byte(`{"id":"` + id + `"}`), SchemaVersion: ledger.SchemaVersion}
	}
	nuzno(t, primiPaket(t, c, verzijaPostavkiKarte0045(t, o, vid(7)), spomenik(PostavkaKarta, vid(8))))
	if models.VektorskaKartaPrograma() == nil || models.PostavkeKartePrograma().Vektorska == nil {
		t.Fatal("spomenik postavki karata maknuo je kartu")
	}
	nuzno(t, primiPaket(t, c, spomenik(PostavkaKartaBezInterneta, vid(9))))
	if k := models.VektorskaKartaPrograma(); k != nil {
		t.Errorf("spomenik opisa nije maknuo kartu: %+v", k)
	}
	if redakaNaPovrsini(t, c, "postavke") != 0 {
		t.Error("spomenici nisu maknuli zapise s površine")
	}

	// spomenik koji ne prođe na površini ne miče kartu programa
	nuzno(t, primiPaket(t, c, verzijaOpisaKarte(t, vid(10), &opis)))
	zabraniUpis(t, c, "DELETE", "postavke")
	if err := primijeniUTx(t, c, spomenik(PostavkaKartaBezInterneta, vid(11))); err == nil {
		t.Fatal("spomenik bez brisanja s površine javio je uspjeh")
	}
	if models.VektorskaKartaPrograma() == nil {
		t.Error("neuspjeli spomenik maknuo je kartu programa")
	}
}
