package web

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
)

func nizPrognoze(izdano time.Time, sati int) *PrognozaNiza {
	p := &PrognozaNiza{Izdano: izdano, Udio: 68}
	for i := 0; i <= sati; i++ {
		p.Tocke = append(p.Tocke, TockaPrognoze{
			Kad:        izdano.Add(time.Duration(i) * time.Hour),
			Vrijednost: 300 + float64(i)*2,
			Raspon:     float64(i) * 1.5, // raspon raste s dosegom
		})
	}
	return p
}

func nizOcitanja(do_ time.Time, sati int) []models.SpojenaVrijednost {
	var out []models.SpojenaVrijednost
	for i := sati; i >= 0; i-- {
		out = append(out, models.SpojenaVrijednost{
			Kad: do_.Add(-time.Duration(i) * time.Hour), Vrijednost: 300 - float64(i),
		})
	}
	return out
}

// Vremenska os mora obuhvatiti i ono što tek dolazi. Bez toga bi crta prognoze
// izlazila iz slike, a graf bi tvrdio da je razdoblje kraće nego što jest.
func TestGrafRastegneOsDoKrajaPrognoze(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	c := crtajNizSPrognozom(nizOcitanja(izdano, 48), "vodostaj", nil, nil, nizPrognoze(izdano, 72))
	if c == nil {
		t.Fatal("nema grafa")
	}
	if !c.ImaPrognozu {
		t.Fatal("graf ne zna za prognozu")
	}
	kraj := izdano.Add(72 * time.Hour)
	if !c.To.Equal(kraj) {
		t.Errorf("os završava %s, a prognoza ide do %s", c.To, kraj)
	}
	// Sjena mora početi na satu izdavanja i ići do desnog ruba plohe.
	if c.PrognozaSir <= 0 {
		t.Errorf("sjena prognoziranog razdoblja široka %g", c.PrognozaSir)
	}
	if c.PrognozaOd <= c.Lijevo || c.PrognozaOd >= c.DesnoX() {
		t.Errorf("sjena počinje na %g, izvan plohe %g–%g", c.PrognozaOd, c.Lijevo, c.DesnoX())
	}
}

// Pojas mora obuhvatiti crtu prognoze: ondje gdje je raspon veći, pojas je
// širi. Ako ih okrenemo, ploha se izvrne i prikaže besmislicu.
func TestPojasObuhvacaCrtuPrognoze(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	c := crtajNizSPrognozom(nizOcitanja(izdano, 24), "vodostaj", nil, nil, nizPrognoze(izdano, 24))
	if c == nil || !c.ImaPrognozu {
		t.Fatal("nema prognoze na grafu")
	}
	// Prva točka pojasa ima raspon nula, pa gornji i donji rub padaju na crtu.
	if !strings.HasPrefix(c.PrognozaPojas, "M") || !strings.HasSuffix(c.PrognozaPojas, " Z") {
		t.Errorf("pojas nije zatvorena ploha: %.40s…", c.PrognozaPojas)
	}
	if !strings.HasPrefix(c.PrognozaPut, "M") {
		t.Errorf("crta prognoze ne počinje potezom M: %.40s…", c.PrognozaPut)
	}
	// Pojas nosi obje strane, pa ima dvostruko više poteza od crte.
	if a, b := strings.Count(c.PrognozaPojas, "L"), strings.Count(c.PrognozaPut, "L"); a < 2*b {
		t.Errorf("pojas ima %d poteza, crta %d — donji rub nedostaje", a, b)
	}
}

// Uz graf mora pisati kad je prognoza izdana i koliki dio promašaja u raspon
// doista stane. Raspon bez te brojke obećava točnost koju nitko nije izmjerio.
func TestUzGrafPiseKadJeIzdanaIKolikoDrzi(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	c := crtajNizSPrognozom(nizOcitanja(izdano, 24), "vodostaj", nil, nil, nizPrognoze(izdano, 24))
	for _, want := range []string{"23.9.2026.", "68 %"} {
		if !strings.Contains(c.PrognozaNatpis, want) {
			t.Errorf("natpis %q nema %q", c.PrognozaNatpis, want)
		}
	}
}

// Graf bez prognoze mora ostati kakav je bio: baza prognoza je zasebna
// datoteka i program mora raditi i kad je nema.
func TestGrafBezPrognozeOstajeIsti(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	ocitanja := nizOcitanja(izdano, 24)
	sPraznom := crtajNizSPrognozom(ocitanja, "vodostaj", nil, nil, nil)
	bez := crtajNiz(ocitanja, "vodostaj", nil, nil)
	if sPraznom.ImaPrognozu || sPraznom.Path != bez.Path || !sPraznom.To.Equal(bez.To) {
		t.Error("prazna prognoza mijenja graf")
	}
}

// Prognoza mora doći i na stranicu, ne samo u crtež.
func TestStranicaOcitanjaPokazujePrognozu(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	st := models.Station{ID: uuid.New(), Name: "Belišće", Code: "belisce"}
	c := crtajNizSPrognozom(nizOcitanja(izdano, 48), "vodostaj", nil, nil, nizPrognoze(izdano, 48))
	html := iscrtaj(t, "reading_history.html", ReadingHistoryData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     &st, GaugeName: "Belišće", LetvaStranica: "ocitanja",
		Chart: c,
	})
	for _, want := range []string{"prognoza-crta", "prognoza-pojas", "prognoza-polje",
		"s prognozom", "68 %"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica očitanja nema %q", want)
		}
	}
}
