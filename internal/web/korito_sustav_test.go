package web

import (
	"math"
	"strings"
	"testing"

	"gocop/internal/models"
)

func probniProfil() models.ProfilKorita {
	return models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.450,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 86.45},   // lijeva obala, +600 cm
			{Stacionaza: 100, Visina: 78.45}, // dno, -200 cm
			{Stacionaza: 200, Visina: 85.45}, // desna obala, +500 cm
		}}
}

func batinaSKotama() models.Station {
	stara, nova := 80.450, 80.189
	return models.Station{Name: "Batina", ZeroDatum: &stara, ZeroDatumSystem: "TRST",
		ZeroDatumNew: &nova, ZeroDatumNewSystem: "HVRS71"}
}

// Profil je snimljen u starom sustavu, a kote se prikazuju u novom. Razlika se
// računa iz kota nule same letve — nigdje ne stoji upisana kao konstanta, pa
// ne može zastarjeti kad se kota ispravi.
func TestPresjekPrikazujeKoteUNovomSustavu(t *testing.T) {
	p := probniProfil()
	st := batinaSKotama()
	c := crtajKoritoP(p, 300, sirokoKoritoM.uSustavu(st))
	if c == nil {
		t.Fatal("presjek se nije nacrtao")
	}
	if c.Sustav != "HVRS71" {
		t.Errorf("sustav prikaza: %q", c.Sustav)
	}
	if math.Abs(c.PomakSustava-(-0.261)) > 1e-9 {
		t.Errorf("pomak %.4f, očekivan -0,261", c.PomakSustava)
	}
	// vodna ploha pri +300 cm: 80,450 + 3,00 = 83,450 TRST → 83,189 HVRS71
	if math.Abs(c.KotaVode-83.189) > 1e-9 {
		t.Errorf("kota vode %.4f, očekivana 83,189", c.KotaVode)
	}
	// dno 78,450 TRST → 78,189 HVRS71
	if math.Abs(c.Dno-78.189) > 1e-9 {
		t.Errorf("dno %.4f, očekivano 78,189", c.Dno)
	}
	// dubina je razlika i pomak je ne smije dirati
	if math.Abs(c.DubinaM-5.0) > 1e-9 {
		t.Errorf("dubina %.4f, očekivana 5,000 — pomak sustava je ne mijenja", c.DubinaM)
	}
	// centimetri na letvi su mjera od nule i ostaju isti u oba sustava
	if c.DnoCm != -200 || c.LijevaCm != 600 || c.DesnaCm != 500 {
		t.Errorf("centimetri na letvi su se pomaknuli: dno %d, l. obala %d, d. obala %d",
			c.DnoCm, c.LijevaCm, c.DesnaCm)
	}
	if c.NazivOsi() != "m n.m. HVRS71" {
		t.Errorf("naziv osi: %q", c.NazivOsi())
	}
}

// Podjele moraju biti okrugle u sustavu u kojem se ispisuju. Bez toga bi crta
// s natpisom „80" stajala na 80,26 stvarne visine.
func TestPodjelePresjekaSuOkrugleUPrikazanomSustavu(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	if len(c.KoteY) < 3 {
		t.Fatalf("premalo podjela: %d", len(c.KoteY))
	}
	for _, k := range c.KoteY {
		if math.Abs(k.Kota-math.Round(k.Kota)) > 1e-6 {
			t.Errorf("podjela %.4f nije okrugla u prikazanom sustavu", k.Kota)
		}
		// uz svaku kotu stoji i vodostaj koji joj odgovara; provjera je da se
		// iz njega vrati ista visina
		natrag := 80.450 + float64(k.Cm)/100 - 0.261
		if math.Abs(natrag-k.Kota) > 0.006 {
			t.Errorf("kota %.3f i vodostaj %d cm ne govore o istoj visini (%.3f)", k.Kota, k.Cm, natrag)
		}
	}
}

// Letva bez novog sustava nema iz čega izračunati razliku, pa se kote
// prikazuju kako su snimljene. Nagađanje bi pomaknulo cijeli presjek.
func TestBezNovogSustavaPresjekOstajeKakoJeSnimljen(t *testing.T) {
	stara := 80.450
	st := models.Station{ZeroDatum: &stara, ZeroDatumSystem: "TRST"}
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(st))
	if c.PomakSustava != 0 || c.Sustav != "" {
		t.Errorf("pomak %.4f, sustav %q — bez novog sustava se ne smije pomicati", c.PomakSustava, c.Sustav)
	}
	if math.Abs(c.KotaVode-83.450) > 1e-9 {
		t.Errorf("kota vode %.4f, očekivana 83,450", c.KotaVode)
	}
	if c.NazivOsi() != "m n.m." {
		t.Errorf("naziv osi: %q", c.NazivOsi())
	}
}

// Ista se visina čita s dvije strane: lijevo kota, desno vodostaj koji bi joj
// na letvi odgovarao. Bez druge osi se s presjeka ne da očitati koji vodostaj
// koju visinu korita doseže.
func TestPresjekImaObjeOsi(t *testing.T) {
	c := crtajKoritoP(probniProfil(), 300, sirokoKoritoM.uSustavu(batinaSKotama()))
	if !c.DvijeOsi {
		t.Fatal("presjek na kartici mora imati obje osi")
	}
	if c.NazivDesneOsi() != "cm na letvi" {
		t.Errorf("desna os: %q", c.NazivDesneOsi())
	}
	if c.DesnaOsCrtaX() >= float64(c.Sirina) {
		t.Error("vodoravne crte moraju stati prije brojki desne osi")
	}
	if c.DesnaOsX() <= c.DesnaOsCrtaX() {
		t.Error("brojke desne osi stoje desno od kraja crta")
	}

	// uz graf očitanja je obrnuto: lijevo centimetri, desno kote
	u := crtajKoritoP(probniProfil(), 300, sirokoKorito.uSustavu(batinaSKotama()))
	if !u.OsUCm || u.NazivOsi() != "cm na letvi" || u.NazivDesneOsi() != "m n.m. HVRS71" {
		t.Errorf("uz graf: lijevo %q, desno %q", u.NazivOsi(), u.NazivDesneOsi())
	}
	if len(u.KoteY) == 0 {
		t.Fatal("nema podjela")
	}
	if !strings.Contains(u.KoteY[0].DesniIspis(), ",") {
		t.Errorf("desni ispis uz graf mora biti kota u metrima: %q", u.KoteY[0].DesniIspis())
	}
}

// Presjek na očitanjima mora biti isti kao na kartici letve: spojen iz svih
// snimaka. Prije se ondje birala jedna, pa je korito ispadalo niže nego što
// jest — Batinina snimka iz 2020. lijevu obalu presijeca na +166 cm, a
// spojena seže do +966. Vraćanje na jednu snimku ne bi ništa srušilo, samo
// bi tiho odrezalo pola korita.
func TestOcitanjaCrtajuSpojeniPresjek(t *testing.T) {
	stara := models.ProfilKorita{Datum: "2010-03-22", KotaNule: 80.45,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 90.11}, // lijeva obala do vrha, samo u staroj
			{Stacionaza: 105, Visina: 82.11},
			{Stacionaza: 200, Visina: 73.20},
			{Stacionaza: 500, Visina: 89.25},
		}}
	nova := models.ProfilKorita{Datum: "2020-08-18", KotaNule: 80.45, PomakM: 104.5,
		Tocke: []models.TockaProfila{
			{Stacionaza: 0, Visina: 82.11}, // presječena na +166 cm
			{Stacionaza: 95, Visina: 72.81},
			{Stacionaza: 294, Visina: 89.24},
		}}
	spoj := models.SpojiProfile([]models.ProfilKorita{stara, nova})
	if !spoj.Spojen() {
		t.Fatal("priprema: profil nije spojen")
	}
	st := batinaSKotama()

	spojeni := crtajKoritoP(spoj, 300, sirokoKorito.uSustavu(st))
	sama := crtajKoritoP(nova, 300, sirokoKorito.uSustavu(st))
	if spojeni == nil || sama == nil {
		t.Fatal("presjek se nije nacrtao")
	}
	if spojeni.LijevaCm <= sama.LijevaCm {
		t.Errorf("spojeni presjek mora sezati više uz lijevu obalu: %d naspram %d",
			spojeni.LijevaCm, sama.LijevaCm)
	}
	// dno ostaje iz novije snimke — novija ima prednost gdje seže
	if spojeni.DnoCm > sama.DnoCm {
		t.Errorf("dno spojenog (%d cm) nije iz novije snimke (%d cm)", spojeni.DnoCm, sama.DnoCm)
	}
}
