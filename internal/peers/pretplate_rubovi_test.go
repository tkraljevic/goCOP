package peers

// Pretplate na jednom čvoru, bez mreže: opis pravila na ekranu, koji kanal
// pravilo pokriva, koliko dugo sadržaj ostaje i što se ne može upisati.
// Brisanje kanala je u brisanje_kanala_test.go.

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/ledger"
)

// Pravilo se na ekranu čita kao rečenica: vrsta, gdje, kada, razina i rok
// sadržaja. Područje ima prednost pred sektorom, a arhiva nema godina.
func TestOpisPretplate(t *testing.T) {
	for _, p := range []struct {
		s    Subscription
		opis string
	}{
		{Subscription{}, "sve vrste, sva područja, sve godine"},
		{Subscription{Kind: ledger.ChannelReadings, AreaID: 16, SectorID: "P", YearFrom: 2024, YearTo: 2026},
			"očitanja, BP 16, 2024.–2026."},
		{Subscription{Kind: ledger.ChannelJournals, SectorID: "P", YearFrom: 2026, YearTo: 2026},
			"dnevnici, sektor P, 2026."},
		{Subscription{Kind: ledger.ChannelPrijave, YearFrom: 2025}, "prijave s terena, sva područja, od 2025."},
		{Subscription{Kind: ledger.ChannelVodocuvar, YearTo: 2025, Razina: RazinaKazalo},
			"vodočuvarski dnevnik, sva područja, do 2025., samo kazalo"},
		{Subscription{Kind: ledger.ChannelArhiva, AreaID: 3, YearFrom: 2020, Razina: RazinaPregled, DrziDana: 1},
			"arhiva vodostaja, BP 3, cijela povijest, samo pregledi, sadržaj 1 dan"},
		{Subscription{Razina: RazinaSve, DrziDana: 3}, "sve vrste, sva područja, sve godine, sadržaj 3 dana"},
		{Subscription{DrziDana: 30}, "sve vrste, sva područja, sve godine, sadržaj 30 dana"},
		{Subscription{Kind: "nepoznata"}, "sve vrste, sva područja, sve godine"},
	} {
		if got := p.s.Label(); got != p.opis {
			t.Errorf("%+v: %q, očekivano %q", p.s, got, p.opis)
		}
	}
}

// Pravilo pokriva kanal kad se slažu vrsta, područje (ili sektor područja)
// i godine. Paket arhive nosi cijelu povijest, pa za njega godine ne vrijede
// ni uz jedno pravilo koje ga pokriva: „arhiva”, „sve vrste” za područje ili
// sektor i „sve vrste” za sva područja (Odluke (13. 10. 2026.)).
func TestPraviloPokrivaKanal(t *testing.T) {
	sektor := func(area int) string {
		if area == 16 {
			return "P"
		}
		return "D"
	}
	kanal := "ocitanja/16/2026"
	for _, p := range []struct {
		ime     string
		pravilo Subscription
		kanal   string
		pokriva bool
	}{
		{"druga vrsta", Subscription{Kind: ledger.ChannelJournals}, kanal, false},
		{"ista vrsta", Subscription{Kind: ledger.ChannelReadings}, kanal, true},
		{"drugo područje", Subscription{AreaID: 3}, kanal, false},
		{"drugi sektor", Subscription{SectorID: "D"}, kanal, false},
		{"sektor područja", Subscription{SectorID: "P"}, kanal, true},
		{"područje ispred sektora", Subscription{AreaID: 16, SectorID: "D"}, kanal, true},
		{"prije godine od", Subscription{YearFrom: 2027}, kanal, false},
		{"poslije godine do", Subscription{YearTo: 2025}, kanal, false},
		{"unutar godina", Subscription{YearFrom: 2026, YearTo: 2026}, kanal, true},
		{"arhiva bez godina", Subscription{Kind: ledger.ChannelArhiva, YearFrom: 2030, YearTo: 2031}, "arhiva/16/0", true},
		{"arhiva ne pokriva očitanja", Subscription{Kind: ledger.ChannelArhiva, AreaID: 16}, kanal, false},
		// sve vrste za područje: arhiva tog područja bez obzira na godine
		{"sve vrste s godinama, arhiva područja", Subscription{AreaID: 16, YearFrom: 2025, YearTo: 2026}, "arhiva/16/0", true},
		{"sve vrste od godine, arhiva sektora", Subscription{SectorID: "P", YearFrom: 2025}, "arhiva/16/0", true},
		{"sve vrste do godine, arhiva", Subscription{YearTo: 2020}, "arhiva/16/0", true},
		{"sve vrste, arhiva drugog područja", Subscription{AreaID: 3, YearFrom: 2025}, "arhiva/16/0", false},
		{"sve vrste, arhiva drugog sektora", Subscription{SectorID: "D", YearFrom: 2025}, "arhiva/16/0", false},
		{"druga vrsta, arhiva", Subscription{Kind: ledger.ChannelReadings, AreaID: 16}, "arhiva/16/0", false},
		{"sve vrste s godinama i dalje ograđuju očitanja", Subscription{AreaID: 16, YearFrom: 2025, YearTo: 2025}, kanal, false},
		// sve vrste za sva područja (zadani obrazac, s godinom početka):
		// cijela arhiva, i strane i meteorološke letve (područje 0)
		{"sve vrste za sva područja od godine, arhiva", Subscription{YearFrom: 2026}, "arhiva/16/0", true},
		{"sve vrste za sva područja od godine, strana letva", Subscription{YearFrom: 2026}, "arhiva/0/0", true},
		{"sve vrste za sva područja s godinama, arhiva", Subscription{YearFrom: 2025, YearTo: 2026}, "arhiva/3/0", true},
		{"sve vrste za sva područja bez godina, strana letva", Subscription{}, "arhiva/0/0", true},
		{"sve vrste za sva područja od godine i dalje ograđuju očitanja", Subscription{YearFrom: 2027}, kanal, false},
		{"arhiva za sva područja s godinama", Subscription{Kind: ledger.ChannelArhiva, YearFrom: 2026}, "arhiva/0/0", true},
		{"sve vrste za područje, strana letva", Subscription{AreaID: 16, YearFrom: 2026}, "arhiva/0/0", false},
	} {
		w := Wants{Rules: []Subscription{p.pravilo}}
		if got := w.Match(p.kanal, sektor); got != p.pokriva {
			t.Errorf("%s: pokriva %v, očekivano %v", p.ime, got, p.pokriva)
		}
	}
}

// Paket arhive dohvaća se uz pravilo „sve vrste” za područje kao i uz
// pravilo samo za arhivu, a godine pravila ne ograđuju ga. Razina sadržaja
// za paket ne vrijedi: paket su podaci vodostaja, a ne PDF ili slika uz
// zapis, pa stiže i uz „Samo pregledi” i „Samo kazalo” (Odluke
// (13. 10. 2026.)). Rok držanja vrijedi kao za ostalo. Pravilo drugog
// područja paket ne donosi, a PDF uz zapise i dalje prati razinu.
func TestSveVrstePokrivajuPaketArhive(t *testing.T) {
	sektor := func(int) string { return "P" }
	const paket = "application/vnd.gocop.arhiva"
	w := Wants{Rules: []Subscription{
		{AreaID: 16, YearFrom: 2025, YearTo: 2026, Razina: RazinaSve, DrziDana: 30},
		{AreaID: 3, YearFrom: 2025, Razina: RazinaKazalo},
		{AreaID: 4, YearFrom: 2025, Razina: RazinaPregled},
		{Kind: ledger.ChannelArhiva, AreaID: 7, Razina: RazinaKazalo},
	}}
	if got := w.RazinaZa("arhiva/16/0", sektor); got != RazinaSve {
		t.Errorf("razina arhive BP 16: %q", got)
	}
	if got := w.DrziDanaZa("arhiva/16/0", sektor); got != 30 {
		t.Errorf("rok sadržaja arhive BP 16: %d", got)
	}
	for _, p := range []struct {
		kanal, vrsta string
		zeli         bool
	}{
		{"arhiva/16/0", paket, true},
		{"arhiva/3/0", paket, true}, // samo kazalo
		{"arhiva/4/0", paket, true}, // samo pregledi
		{"arhiva/7/0", paket, true}, // pravilo samo za arhivu, samo kazalo
		{"arhiva/5/0", paket, false},
		{"prijave/3/2026", "application/pdf", false},
		{"prijave/4/2026", "application/pdf", false},
		{"prijave/4/2026", "image/jpeg", true},
	} {
		if got := w.zeliSadrzaj(p.kanal, p.vrsta, sektor); got != p.zeli {
			t.Errorf("%s, %s: dohvaća se %v, očekivano %v", p.kanal, p.vrsta, got, p.zeli)
		}
	}
}

// Stranica pretplata nudi arhivu vodostaja kao vrstu: pravilo se upisuje i
// čita kao „arhiva vodostaja … cijela povijest”. Razina sadržaja za paket ne
// vrijedi, pa se pravilo samo za arhivu pamti sa „Sve” i opis ne kaže
// „samo kazalo”; rok držanja ostaje.
func TestPretplataNaArhivuSeUpisuje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	r, err := s.AddSubscription(ctxRub, Subscription{Kind: ledger.ChannelArhiva, AreaID: 16, YearFrom: 2026, Razina: RazinaKazalo, DrziDana: 30})
	if err != nil {
		t.Fatalf("pretplata na arhivu: %v", err)
	}
	if got := r.Label(); got != "arhiva vodostaja, BP 16, cijela povijest, sadržaj 30 dana" {
		t.Errorf("opis pravila: %q", got)
	}
	if l, err := s.ListSubscriptions(ctxRub); err != nil || len(l) != 1 || l[0].Razina != RazinaSve || l[0].DrziDana != 30 {
		t.Errorf("upisano pravilo: %+v %v", l, err)
	}
	if _, err := s.AddSubscription(ctxRub, Subscription{Kind: ledger.ChannelArhiva, Razina: "puno"}); err == nil {
		t.Error("pravilo samo za arhivu s nepoznatom razinom je upisano")
	}
	w, err := s.CurrentWants(ctxRub)
	if err != nil || !w.Match("arhiva/16/0", s.sectorLookup(ctxRub)) || w.Match("ocitanja/16/2026", s.sectorLookup(ctxRub)) {
		t.Errorf("pretplata na arhivu pokriva krivo: %+v %v", w, err)
	}
}

// Sadržaj kanala ostaje onoliko dana koliko kaže najdulje pravilo koje ga
// pokriva; pravilo bez roka (0) znači trajno i ima prednost. Zajednički
// kanal, uredski čvor i kanal bez pravila nemaju roka.
func TestRokDrzanjaSadrzaja(t *testing.T) {
	bezSektora := func(int) string { return "" }
	kanal := "prijave/16/2026"
	if got := (Wants{All: true, Rules: []Subscription{{DrziDana: 5}}}).DrziDanaZa(kanal, bezSektora); got != 0 {
		t.Errorf("uredski čvor: %d", got)
	}
	if got := (Wants{Rules: []Subscription{{DrziDana: 5}}}).DrziDanaZa("", bezSektora); got != 0 {
		t.Errorf("zajednički kanal: %d", got)
	}
	for _, p := range []struct {
		ime   string
		rules []Subscription
		dana  int
	}{
		{"bez pravila", nil, 0},
		{"pravilo drugog područja", []Subscription{{AreaID: 3, DrziDana: 365}}, 0},
		{"najdulje od dva", []Subscription{{AreaID: 16, DrziDana: 30}, {Kind: "prijave", DrziDana: 90}, {AreaID: 3, DrziDana: 365}}, 90},
		{"kraći poslije duljeg", []Subscription{{DrziDana: 90}, {AreaID: 16, DrziDana: 30}}, 90},
		{"trajno ima prednost", []Subscription{{AreaID: 16, DrziDana: 30}, {Kind: "prijave"}}, 0},
	} {
		if got := (Wants{Rules: p.rules}).DrziDanaZa(kanal, bezSektora); got != p.dana {
			t.Errorf("%s: %d dana, očekivano %d", p.ime, got, p.dana)
		}
	}
}

// Pretplata s nepoznatom vrstom ili razinom, ili s godinama obrnutim redom,
// ne upisuje se; ostalo se svede na zapisani oblik (razina "sve", sektor
// velikim slovima, negativan rok je trajno).
func TestPretplataSeUpisujeSvedena(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	// poruka ide korisniku na stranici pretplata, pa je cijela rečenica
	for ime, p := range map[string]struct {
		r      Subscription
		poruka string
	}{
		"vrsta":          {Subscription{Kind: "vodostaji"}, "Pretplata nije dodana: vrsta mora biti"},
		"razina":         {Subscription{Razina: "puno"}, "Pretplata nije dodana: sadržaj uz zapise"},
		"godine obrnuto": {Subscription{YearFrom: 2026, YearTo: 2024, DrziDana: -5}, "Pretplata nije dodana: godina u polju „Do godine” je prije godine u polju „Od godine”."},
	} {
		if _, err := s.AddSubscription(ctxRub, p.r); err == nil || !strings.HasPrefix(err.Error(), p.poruka) {
			t.Errorf("%s: pretplata je upisana ili poruka nije rečenica: %v", ime, err)
		}
	}
	if l, _ := s.ListSubscriptions(ctxRub); len(l) != 0 {
		t.Fatalf("neispravne pretplate su u bazi: %+v", l)
	}

	r, err := s.AddSubscription(ctxRub, Subscription{Kind: " ocitanja ", SectorID: " p ", DrziDana: -5})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddSubscription(ctxRub, Subscription{Kind: ledger.ChannelVodocuvar, AreaID: 16, YearFrom: 2026, YearTo: 2026, Razina: RazinaKazalo, DrziDana: 14})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 || k.ID == r.ID {
		t.Errorf("brojevi pravila: %d, %d", r.ID, k.ID)
	}
	l, err := s.ListSubscriptions(ctxRub)
	if err != nil || len(l) != 2 {
		t.Fatalf("pretplate: %+v %v", l, err)
	}
	if g := l[0]; g.Kind != "ocitanja" || g.SectorID != "P" || g.Razina != RazinaSve || g.DrziDana != 0 {
		t.Errorf("svedeno pravilo: %+v", g)
	}
	if g := l[1]; g.Kind != ledger.ChannelVodocuvar || g.AreaID != 16 || g.YearFrom != 2026 || g.YearTo != 2026 || g.Razina != RazinaKazalo || g.DrziDana != 14 {
		t.Errorf("pravilo s razinom kazala: %+v", g)
	}

	// pravilo koje se ne može pročitati (stupac bez vrijednosti): popis javlja grešku
	pokvariTablicu(t, s, "subscriptions")
	if _, err := s.AddSubscription(ctxRub, Subscription{Kind: "prijave"}); err == nil || !strings.Contains(err.Error(), "upis pretplate") {
		t.Errorf("upis bez tablice: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE subscriptions (id INTEGER PRIMARY KEY, kind TEXT, sector_id TEXT, area_id INTEGER,
		year_from INTEGER, year_to INTEGER, razina TEXT, drzi_dana INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO subscriptions (kind, sector_id, area_id, year_from, year_to, razina, drzi_dana)
		VALUES ('ocitanja', NULL, 16, 0, 0, 'sve', 0)`); err != nil {
		t.Fatal(err)
	}
	if l, err := s.ListSubscriptions(ctxRub); err == nil {
		t.Errorf("nečitljivo pravilo je pročitano: %+v", l)
	}
}

// Kad se područja ne mogu pročitati, sektor nije poznat ni za jedno
// područje: pravilo po sektoru tada ne pokriva ništa.
func TestSektorBezUstroja(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if _, err := s.db.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Primjer', 'VGO Primjer', 'COP Primjer')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (16, 'P', 'Primjerovo', 'VGI Primjer')`); err != nil {
		t.Fatal(err)
	}
	if got := s.sectorLookup(ctxRub)(16); got != "P" {
		t.Fatalf("sektor područja 16: %q", got)
	}
	ctx, otkazi := context.WithCancel(ctxRub)
	otkazi()
	if got := s.sectorLookup(ctx)(16); got != "" {
		t.Errorf("sektor bez čitljivog ustroja: %q", got)
	}
}
