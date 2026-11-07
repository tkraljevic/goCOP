package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// dodajOdvojenuLetvu upisuje izmišljenu letvu potok-dole u bazu prognoza: račun
// iz belisce i retke samo unaprijed, kao letva koja u satu izdanja nije izmjerena.
func dodajOdvojenuLetvu(t *testing.T, put string, izdano int64) {
	t.Helper()
	baza, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	z := prognoza.ZapisLetve{Oblik: prognoza.OblikModelaLetve, Letva: "potok-dole", Metoda: prognoza.MetodaLanac, Velicina: "vodostaj",
		Racun: prognoza.RacunLetve{Ulazi: []string{"belisce"}, Doseg: 12},
		Brojevi: prognoza.BrojeviLetve{Inacice: [][]prognoza.Pojas{{{Letva: "potok-dole", Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
			Ulazi: []prognoza.Ulaz{{Letva: "belisce", Velicina: "vodostaj", PomakH: 2, Sirina: 1, Nagib: 1}}}}},
			Promasaji: map[int]prognoza.Promasaj{1: {Rasap: 9, Postojanost: 1}}}}
	sadrzaj, err := json.Marshal(z)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO model_letve (letva, sadrzaj) VALUES ('potok-dole', ?)`, string(sadrzaj)); err != nil {
		t.Fatal(err)
	}
	var redci []prognoza.Izdana
	for sat := int64(1); sat <= 12; sat++ {
		redci = append(redci, prognoza.Izdana{Letva: "potok-dole", Velicina: "vodostaj", Izdano: izdano, Ciljni: izdano + sat,
			Vrijednost: 40, Dolje: 37, Gore: 43, Model: "proba"})
	}
	if err := prognoza.SpremiIzdane(baza, redci); err != nil {
		t.Fatal(err)
	}
}

// Letva iz zapisa modela ima svoj račun i promašaje, a kartice glavnih letvi i
// poredak ostaju kakvi jesu. U satu izdanja nema računatog broja.
func TestPregledOdvojeneLetve(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	put := bazaPrognoza(t, izdano)
	citaj := func() map[string]PregledLetve {
		c, err := OtvoriPrognoze(put)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, letve, err := c.Pregled()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]PregledLetve{}
		for i, l := range letve {
			if l.Letva == "potok-dole" && i != len(letve)-1 {
				t.Errorf("nova letva nije na kraju popisa: %d od %d", i, len(letve))
			}
			out[l.Letva] = l
		}
		return out
	}
	prije := citaj()
	dodajOdvojenuLetvu(t, put, izdano)
	poslije := citaj()
	l := poslije["potok-dole"]
	if !l.Odvojena || l.Racuna != "vodostaj" || len(l.Ulazi) != 1 || l.Ulazi[0] != "belisce" {
		t.Errorf("%+v", l)
	}
	if len(l.Sada) != 0 {
		t.Errorf("sada: %v", l.Sada)
	}
	if l.Satno[izdano+1].BoljaOdPostojanosti {
		t.Error("promašaji letve nisu uzeti")
	}
	for k, staro := range prije {
		novo := poslije[k]
		if novo.UlazLanca != staro.UlazLanca || novo.Racuna != staro.Racuna || novo.Odvojena {
			t.Errorf("%s se promijenila: %+v", k, novo)
		}
	}
}

// rukovateljPotoka je stranica s izmišljenom letvom potok-dole u registru; drugi
// povrat upisuje njezino očitanje.
func rukovateljPotoka(t *testing.T, put string, izdano time.Time) (*PrognozeHandler, func(cm int, kad time.Time)) {
	t.Helper()
	h := rukovateljBaze(t, put)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	h.stations = service.NewStationService(stanice, nil, nil)
	h.SetReadings(service.NewReadingService(repository.NewReadingRepository(baza, rec), stanice, nil, nil, nil))
	st := &models.Station{ID: uuid.New(), Code: "potok-dole", Name: "Donji Potok", Watercourse: "Izmišljeni potok"}
	if err := stanice.CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return h, func(cm int, kad time.Time) {
		t.Helper()
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), st.ID.String(), kad, cm, models.ReadingSourceManual, izdano, izdano); err != nil {
			t.Fatal(err)
		}
	}
}

// Kad letva iz zapisa nema mjerenja u satu izdanja, kartica pokazuje zadnje
// mjerenje s vremenom, a glavna letva ne dobiva ništa.
func TestZadnjeMjerenjeOdvojene(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	put := bazaPrognoza(t, izdano.Unix()/3600)
	dodajOdvojenuLetvu(t, put, izdano.Unix()/3600)
	h, upisi := rukovateljPotoka(t, put, izdano)
	upisi(120, izdano.Add(-5*time.Hour))
	r := httptest.NewRequest(http.MethodGet, "/prognoze", nil)
	data := h.podaci(r, models.PrognozaSatni)
	nadjena := false
	for _, l := range data.Letve {
		if l.Kod == "potok-dole" {
			nadjena = true
			if l.Vrh || l.SadaCm != "" || !strings.HasPrefix(l.ZadnjeMjerenje, "Zadnje mjerenje: 120 cm, 23. 9. u ") {
				t.Errorf("%+v", l)
			}
		} else if l.ZadnjeMjerenje != "" {
			t.Errorf("%s: %s", l.Kod, l.ZadnjeMjerenje)
		}
	}
	if !nadjena {
		t.Fatal("nema letve na stranici")
	}
}

// Upozorenje nove vrste piše se opisom izdavača, kao i kod starije inačice.
func TestTekstUpozorenjaOdvojenih(t *testing.T) {
	p := pisacUpozorenja{postaje: izmisljenePostaje()}
	u := prognoza.Upozorenje{Vrsta: prognoza.VrstaOdvojeni, Letva: "gornja", Opis: "bez svježeg ulaza (srednja): drži se zadnje mjerenje",
		Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 3}}}
	if s := p.sto(u); s != "Gornji Brod: bez svježeg ulaza (Srednji Brod): drži se zadnje mjerenje." {
		t.Errorf("%q", s)
	}
	if s := p.naKartici(u, u.Upitno[0]); !strings.Contains(s, "Gornji Brod") {
		t.Errorf("%q", s)
	}
}

// Napomena odvojenog računa stoji na kartici i na grafu, s istim razlogom kao u
// glavnom lancu, a napomena glavnog računa i dalje ne stoji.
func TestOdvojeniNaKartici(t *testing.T) {
	izmisljeniCiljevi(t)
	sluca := []struct{ opis, uzrok string }{
		{"nema mjerenja od 6.11. u 05:00, a prognoza se računa iz uzvodnih letvi", "Gornji Brod bez mjerenja"},
		{"bez svježeg ulaza (srednja): drži se zadnje mjerenje", "Gornji Brod bez svježeg ulaza"},
		{"rezerva: srednja umjesto treca", "Gornji Brod iz rezerve"},
		{"nešto što ova inačica ne zna", "Gornji Brod"},
	}
	for _, s := range sluca {
		letve := letveBezDnevne()
		u := []prognoza.Upozorenje{{Vrsta: prognoza.VrstaOdvojeni, Tezina: prognoza.TezinaNapomena, Letva: "gornja", Opis: s.opis,
			Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 1}}}}
		(pisacUpozorenja{postaje: izmisljenePostaje(), letve: letve}).oznaciLetve(u)
		if want := "Satna upitna najranije od +1 h (" + s.uzrok + ")."; letve[0].Upozorenje != want {
			t.Errorf("%q, želim %q", letve[0].Upozorenje, want)
		}
		if g := upitnoLetve(u, "donja"); g != "Satna upitna najranije od +1 h" {
			t.Errorf("graf: %q", g)
		}
	}
	u := []prognoza.Upozorenje{{Vrsta: prognoza.VrstaUzvodno, Tezina: prognoza.TezinaNapomena, Letva: "gornja",
		Upitno: []prognoza.Upitno{{Letva: "donja", OdSata: 1}}}}
	if g := upitnoLetve(u, "donja"); g != "" {
		t.Errorf("napomena glavnog računa na grafu: %q", g)
	}
}

// Račun je izbacio očitanje kao lažni skok: broj ostaje na kartici, jer je mjeren,
// ali uz njega piše da ga prognoza možda ne uzima.
func TestZadnjeMjerenjeUzIzbacenoOcitanje(t *testing.T) {
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)
	h, upisi := rukovateljPotoka(t, bazaPrognoza(t, izdano.Unix()/3600), izdano)
	upisi(50, izdano.Add(-time.Hour))
	upisi(830, izdano)
	postaje := h.postaje(context.Background())
	if z := h.zadnjeMjerenje(context.Background(), postaje["potok-dole"], izdano.AddDate(0, -2, 0)); z != "" {
		t.Errorf("bez očitanja: %q", z)
	}
	letve := []PregledLetve{{Letva: "potok-dole", Odvojena: true}}
	for _, s := range []struct {
		u    prognoza.Upozorenje
		zeli string
	}{
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaSkok, Letva: "potok-dole"}, "Prognoza ga možda ne uzima"},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaKvarMjerila, Letva: "potok-dole"}, "Prognoza ga možda ne uzima"},
		{prognoza.Upozorenje{Vrsta: prognoza.VrstaSkok, Letva: "druga"}, ""},
	} {
		opisane := []LetvaPrognoze{{Kod: "potok-dole"}}
		h.zadnjaMjerenjaOdvojenih(context.Background(), opisane, letve, postaje, izdano, []prognoza.Upozorenje{s.u})
		z := opisane[0].ZadnjeMjerenje
		if !strings.HasPrefix(z, "Zadnje mjerenje: 830 cm") {
			t.Errorf("%q", z)
		}
		if strings.Contains(z, "Prognoza") != (s.zeli != "") {
			t.Errorf("%+v: %q", s.u, z)
		}
	}
}

// Nova letva koja čita pritoku ne smije pomaknuti nijednu glavnu letvu. Redoslijed
// se uspoređuje kakav jest, bez sortiranja. Vrh novog računa stoji ispred nje.
func TestNovaLetvaNePomiceGlavne(t *testing.T) {
	st := func(kod, rkm string) models.Station { return models.Station{Code: kod, Stationing: rkm} }
	postaje := map[string]models.Station{
		"mursko-sredisce": st("mursko-sredisce", "rkm 67,70"), "gorican": st("gorican", "rkm 35,62"),
		"varazdin": st("varazdin", "rkm 288+185"), "he-dubrava": st("he-dubrava", "rkm 225,05"),
		"botovo": st("botovo", "rkm 226,83"), "novo-virje": st("novo-virje", "rkm 200,60"),
		"potok-drava": st("potok-drava", "rkm 235,90"), "potok-vrh": st("potok-vrh", "rkm 8,00"),
		"potok-dole": st("potok-dole", "rkm 2,00"), "bistrica": st("bistrica", "rkm 3,00"),
		"bistrica-vrh": st("bistrica-vrh", "rkm 9,00"), "bistrica-pregled": st("bistrica-pregled", "rkm 5,00"),
		"bistrica-nova": st("bistrica-nova", "rkm 1,00"),
	}
	glavne := []LetvaPrognoze{
		{Kod: "mursko-sredisce", Voda: "Mura", Racuna: "protok"},
		{Kod: "gorican", Voda: "Mura", Racuna: "protok", Ulazi: []string{"mursko-sredisce"}},
		{Kod: "varazdin", Voda: "Drava", Racuna: "vodostaj"},
		{Kod: "botovo", Voda: "Drava", Racuna: "protok", Ulazi: []string{"he-dubrava", "gorican"}},
		{Kod: "novo-virje", Voda: "Drava", Racuna: "protok", Ulazi: []string{"botovo"}},
		{Kod: "he-dubrava", Voda: "Drava"},
		{Kod: "bistrica", Voda: "Bistrica", Racuna: "vodostaj", Ulazi: []string{"bistrica-vrh"}},
		{Kod: "bistrica-vrh", Voda: "Bistrica"},
		{Kod: "bistrica-pregled", Voda: "Bistrica", Pregledna: true},
		{Kod: "bez-citatelja", Voda: "Bistrica"},
	}
	nove := []LetvaPrognoze{
		{Kod: "potok-drava", Voda: "Drava", Racuna: "protok", Ulazi: []string{"gorican"}, Odvojena: true},
		{Kod: "potok-dole", Voda: "Potok", Racuna: "vodostaj", Ulazi: []string{"potok-vrh", "he-dubrava", "bistrica-vrh"}, Odvojena: true},
		{Kod: "potok-vrh", Voda: "Potok", Odvojena: true},
		{Kod: "bistrica-nova", Voda: "Bistrica", Racuna: "vodostaj", Ulazi: []string{"bistrica"}, Odvojena: true},
	}
	redovi := func(letve []LetvaPrognoze, sNovima bool) string {
		var got []string
		for _, tb := range poVodama(letve, postaje) {
			for _, l := range tb.Letve {
				if sNovima || !l.Odvojena {
					got = append(got, l.Kod)
				}
			}
		}
		return strings.Join(got, " ")
	}
	prije := redovi(slices.Clone(glavne), true)
	// Na početku, na kraju i između: mjesto u popisu ne smije ništa mijenjati.
	for _, letve := range [][]LetvaPrognoze{
		slices.Concat(glavne, nove), slices.Concat(nove, glavne),
		slices.Concat(glavne[:3], nove, glavne[3:]),
	} {
		if poslije := redovi(letve, false); poslije != prije {
			t.Errorf("glavne su se pomaknule\n%s\numjesto\n%s", poslije, prije)
		}
		s := redovi(letve, true)
		if strings.Index(s, "potok-vrh") > strings.Index(s, "potok-dole") {
			t.Errorf("vrh novog računa iza letve koju hrani: %s", s)
		}
	}
}

// Letva iz zapisa ne ide na profil vode koja ima glavne letve, jer bi mu promijenila
// mjerilo i crtu vala. Voda samo s novim letvama crta se normalno.
func TestProfilBezNovihLetviUzGlavne(t *testing.T) {
	kota := 80.0
	st := func(kod, voda string) models.Station {
		return models.Station{Code: kod, Watercourse: voda, ZeroDatum: &kota}
	}
	postaje := map[string]models.Station{"botovo": st("botovo", "Drava"), "potok-drava": st("potok-drava", "Drava"),
		"potok-vrh": st("potok-vrh", "Potok"), "potok-dole": st("potok-dole", "Potok")}
	letve := []PregledLetve{{Letva: "potok-drava", Odvojena: true}, {Letva: "botovo"},
		{Letva: "potok-vrh", Odvojena: true}, {Letva: "potok-dole", Odvojena: true}}
	var got []string
	for _, l := range bezOdvojenihUzGlavne(postaje, letve) {
		got = append(got, l.Letva)
	}
	if s := strings.Join(got, " "); s != "botovo potok-vrh potok-dole" {
		t.Errorf("%s", s)
	}
	if len(samoGlavne(letve)) != 1 {
		t.Errorf("%v", samoGlavne(letve))
	}
	if len(letve) != 4 || letve[0].Letva != "potok-drava" {
		t.Errorf("ulazni popis je promijenjen: %v", letve)
	}
}

// Zapis modela na glavnoj letvi piše se kao napomena, imenom letve.
func TestTekstZapisaNaGlavnojLetvi(t *testing.T) {
	p := pisacUpozorenja{postaje: izmisljenePostaje()}
	u := prognoza.Upozorenje{Vrsta: prognoza.VrstaOdvojeni, Tezina: prognoza.TezinaNapomena,
		Opis: "zapis modela za letvu gornja se ne koristi: letva se računa u glavnom lancu"}
	if s := p.sto(u); s != "Zapis modela za letvu Gornji Brod se ne koristi: letva se računa u glavnom lancu." {
		t.Errorf("%q", s)
	}
	otvorena, napomene := p.slozi([]prognoza.Upozorenje{u})
	if len(otvorena) != 0 || len(napomene) != 1 {
		t.Errorf("%v %v", otvorena, napomene)
	}
}
