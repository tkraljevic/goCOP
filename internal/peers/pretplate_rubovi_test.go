package peers

// Pretplate na jednom čvoru, bez mreže: opis pravila na ekranu, koji kanal
// pravilo pokriva, koliko dugo sadržaj ostaje, što se ne može upisati i
// brisanje kanala svake vrste s površine i iz knjige.

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
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
	for ime, r := range map[string]Subscription{
		"vrsta":          {Kind: "vodostaji"},
		"razina":         {Razina: "puno"},
		"godine obrnuto": {YearFrom: 2026, YearTo: 2024},
	} {
		if _, err := s.AddSubscription(ctxRub, r); err == nil {
			t.Errorf("%s: pretplata je upisana", ime)
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

// vodocuvarskiList upisuje dnevni list vodočuvara mimo knjige
func vodocuvarskiList(t *testing.T, s *Service, id string, area int, datum string) {
	t.Helper()
	sada := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO vodocuvarski_listovi (id, user_id, area_id, datum, created_at, updated_at) VALUES (?, 'u-1', ?, ?, ?, ?)`,
		id, area, datum, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO vodocuvarski_izvornici (list_id, otisak, updated_at) VALUES (?, ?, ?)`, id, "otisak-"+id, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO vodocuvarski_zadaci (id, user_id, area_id, tekst, zadano_at, list_id, updated_at) VALUES (?, 'u-1', ?, 'obići nasip', ?, ?, ?)`,
		"z-"+id, area, sada, id, sada); err != nil {
		t.Fatal(err)
	}
}

// prijavaSIzvornikom upisuje prijavu s izvornikom mimo knjige
func prijavaSIzvornikom(t *testing.T, s *Service, id string, area int, datum string) {
	t.Helper()
	sada := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO prijave (id, user_id, area_id, datum, created_at, updated_at) VALUES (?, 'u-1', ?, ?, ?, ?)`, id, area, datum, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO prijave_izvornici (prijava_id, otisak, updated_at) VALUES (?, ?, ?)`, id, "otisak-"+id, sada); err != nil {
		t.Fatal(err)
	}
}

func prebroji(t *testing.T, s *Service, upit string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(upit, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Brisanje kanala dnevnika, vodočuvarskih listova i prijava miče s
// površine samo zapise tog područja i godine (s upisima, izvornicima i
// zadacima), a iz knjige verzije kanala. Ostala područja i godine ostaju.
func TestBrisanjeKanalaSvakeVrste(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sada := time.Now().UTC()
	upisi := func(kanal, entitet, id string) {
		t.Helper()
		if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, entitet, id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}

	// dnevnik BP 16 s upisom i listom, i dnevnik BP 3 koji ostaje
	for _, d := range []struct{ id, kanal string }{{"d-16", "dnevnici/16/2026"}, {"d-3", "dnevnici/3/2026"}} {
		if _, err := s.db.Exec(`INSERT INTO journals (id, kind, channel, created_at, updated_at) VALUES (?, 'A.01', ?, ?, ?)`, d.id, d.kanal, sada, sada); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO journal_entries (id, journal_id, date, kind, created_at, updated_at) VALUES (?, ?, '2026-03-02', 'rad', ?, ?)`, "e-"+d.id, d.id, sada, sada); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO journal_sheets (id, journal_id, date, created_at, updated_at) VALUES (?, ?, '2026-03-02', ?, ?)`, "l-"+d.id, d.id, sada, sada); err != nil {
			t.Fatal(err)
		}
		upisi(d.kanal, "journals", d.id)
	}
	n, err := s.PurgeChannel(ctxRub, "dnevnici/16/2026")
	if err != nil || n != 1 {
		t.Fatalf("brisanje dnevnika: %d %v", n, err)
	}
	if prebroji(t, s, `SELECT count(*) FROM journals`) != 1 || prebroji(t, s, `SELECT count(*) FROM journal_entries WHERE journal_id = 'd-16'`) != 0 ||
		prebroji(t, s, `SELECT count(*) FROM journal_sheets WHERE journal_id = 'd-16'`) != 0 || prebroji(t, s, `SELECT count(*) FROM journal_entries`) != 1 {
		t.Error("dnevnik BP 16 nije obrisan cijeli ili je obrisan i BP 3")
	}

	// vodočuvarski listovi: ista godina drugog područja i druga godina ostaju
	vodocuvarskiList(t, s, "v-16", 16, "2026-05-04")
	vodocuvarskiList(t, s, "v-16-lani", 16, "2025-05-04")
	vodocuvarskiList(t, s, "v-3", 3, "2026-05-04")
	upisi("vodocuvarski/16/2026", "vodocuvarski_listovi", "v-16")
	if n, err := s.PurgeChannel(ctxRub, "vodocuvarski/16/2026"); err != nil || n != 1 {
		t.Fatalf("brisanje vodočuvarskih listova: %d %v", n, err)
	}
	if prebroji(t, s, `SELECT count(*) FROM vodocuvarski_listovi WHERE id = 'v-16'`) != 0 ||
		prebroji(t, s, `SELECT count(*) FROM vodocuvarski_izvornici WHERE list_id = 'v-16'`) != 0 ||
		prebroji(t, s, `SELECT count(*) FROM vodocuvarski_zadaci WHERE list_id = 'v-16'`) != 0 {
		t.Error("list BP 16 iz 2026. je ostao")
	}
	if prebroji(t, s, `SELECT count(*) FROM vodocuvarski_listovi`) != 2 || prebroji(t, s, `SELECT count(*) FROM vodocuvarski_zadaci`) != 2 {
		t.Error("obrisani su i listovi drugog područja ili godine")
	}

	// prijave
	prijavaSIzvornikom(t, s, "p-16", 16, "2026-07-01")
	prijavaSIzvornikom(t, s, "p-3", 3, "2026-07-01")
	upisi("prijave/16/2026", "prijave", "p-16")
	if n, err := s.PurgeChannel(ctxRub, "prijave/16/2026"); err != nil || n != 1 {
		t.Fatalf("brisanje prijava: %d %v", n, err)
	}
	if prebroji(t, s, `SELECT count(*) FROM prijave`) != 1 || prebroji(t, s, `SELECT count(*) FROM prijave_izvornici`) != 1 ||
		prebroji(t, s, `SELECT count(*) FROM prijave WHERE id = 'p-3'`) != 1 {
		t.Error("prijave: obrisano krivo područje ili ništa")
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["dnevnici/16/2026"]+c["vodocuvarski/16/2026"]+c["prijave/16/2026"] != 0 || c["dnevnici/3/2026"] != 1 {
		t.Errorf("knjiga nakon brisanja: %v", c)
	}

	// kanal nepoznate vrste se ne briše
	upisi("vodostaji/16/2026", "probni", "x")
	if _, err := s.PurgeChannel(ctxRub, "vodostaji/16/2026"); err == nil || !strings.Contains(err.Error(), "nepoznata vrsta kanala") {
		t.Errorf("nepoznata vrsta: %v", err)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["vodostaji/16/2026"] != 1 {
		t.Error("knjiga kanala nepoznate vrste je obrisana")
	}
}

// Kad brisanje s površine ili iz knjige padne, ništa se ne briše: ni
// površina ni knjiga kanala. Svaki kvar pogađa samo svoj kanal.
func TestBrisanjeKanalaKadUpisPadne(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sada := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO journals (id, kind, channel, created_at, updated_at) VALUES ('d-16', 'A.01', 'dnevnici/16/2026', ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO journal_entries (id, journal_id, date, kind, created_at, updated_at) VALUES ('e-1', 'd-16', '2026-03-02', 'rad', ?, ?)`, sada, sada); err != nil {
		t.Fatal(err)
	}
	vodocuvarskiList(t, s, "v-17", 17, "2026-05-04")
	prijavaSIzvornikom(t, s, "p-18", 18, "2026-07-01")
	prijavaSIzvornikom(t, s, "p-19", 19, "2026-07-01")
	for _, p := range []struct {
		kanal, povrsina string
		kvar            [3]string
	}{
		{"dnevnici/16/2026", `SELECT count(*) FROM journals WHERE id = 'd-16'`, [3]string{"DELETE", "journal_entries", "OLD.journal_id = 'd-16'"}},
		{"vodocuvarski/17/2026", `SELECT count(*) FROM vodocuvarski_listovi WHERE id = 'v-17'`, [3]string{"DELETE", "vodocuvarski_izvornici", "OLD.list_id = 'v-17'"}},
		{"prijave/18/2026", `SELECT count(*) FROM prijave WHERE id = 'p-18'`, [3]string{"DELETE", "prijave_izvornici", "OLD.prijava_id = 'p-18'"}},
		{"prijave/19/2026", `SELECT count(*) FROM prijave WHERE id = 'p-19'`, [3]string{"DELETE", "record_versions", "OLD.channel = 'prijave/19/2026'"}},
	} {
		if _, err := s.rec.RecordIn(ctxRub, s.db, p.kanal, "probni", "r-"+p.kanal, map[string]int{"cm": 1}); err != nil {
			t.Fatal(err)
		}
		kvarUpisa(t, s, p.kvar[0], p.kvar[1], p.kvar[2])
		if _, err := s.PurgeChannel(ctxRub, p.kanal); err == nil {
			t.Errorf("%s: brisanje je prošlo iako upis u %s nije", p.kanal, p.kvar[1])
		}
		if prebroji(t, s, p.povrsina) != 1 {
			t.Errorf("%s: površina je obrisana", p.kanal)
		}
		if c, _ := s.rec.CountByChannel(ctxRub); c[p.kanal] != 1 {
			t.Errorf("%s: knjiga kanala %v", p.kanal, c)
		}
	}
}

// Sadržaj kanala briše se sa spremišta nakon knjige; kad spremište ne
// radi, brisanje javi grešku i broj obrisanih verzija (knjiga je već
// obrisana).
func TestBrisanjeKanalaISadrzaja(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpremiste(sp)
	kanal := "prijave/16/2026"
	otisak, err := sp.Upisi(ctxRub, "application/pdf", []byte("%PDF-1.4 prijava Pere Perića"), "cvor:ured", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-16", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	drugi, _ := sp.Upisi(ctxRub, "application/pdf", []byte("%PDF-1.4 prijava BP 3"), "cvor:ured", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-3", Uloga: "izvornik", Kanal: "prijave/3/2026"})
	for _, k := range []string{kanal, "prijave/17/2026"} {
		if _, err := s.rec.RecordIn(ctxRub, s.db, k, "probni", "r-"+k, map[string]int{"cm": 1}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.PurgeChannel(ctxRub, kanal); err != nil || n != 1 {
		t.Fatalf("brisanje: %d %v", n, err)
	}
	if sp.Ima(ctxRub, otisak) || !sp.Ima(ctxRub, drugi) {
		t.Errorf("sadržaj nakon brisanja kanala: BP 16 %v, BP 3 %v", sp.Ima(ctxRub, otisak), sp.Ima(ctxRub, drugi))
	}

	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	n, err := s.PurgeChannel(ctxRub, "prijave/17/2026")
	if err == nil || n != 1 {
		t.Errorf("zatvoreno spremište: %d %v", n, err)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["prijave/17/2026"] != 0 {
		t.Errorf("knjiga kanala nije obrisana: %v", c)
	}
}

// Popis kanala i brisanje neželjenih: kanal pod pretplatom ostaje, ostali
// se brišu; kad se knjiga ili pretplate ne mogu pročitati, ne briše se ništa.
func TestBrisanjeNezeljenihKanala(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	for _, k := range []string{"ocitanja/16/2026", "ocitanja/3/2026", "ocitanja/16/2025"} {
		if _, err := s.rec.RecordIn(ctxRub, s.db, k, "probni", "r-"+k, map[string]int{"cm": 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddSubscription(ctxRub, Subscription{Kind: "ocitanja", AreaID: 16, YearFrom: 2026}); err != nil {
		t.Fatal(err)
	}
	za, err := s.ChannelsFor(ctxRub, "ocitanja", 16, 0)
	if err != nil || strings.Join(za, ",") != "ocitanja/16/2026,ocitanja/16/2025" {
		t.Errorf("kanali BP 16: %v %v", za, err)
	}
	if za, _ := s.ChannelsFor(ctxRub, "", 0, 2025); strings.Join(za, ",") != "ocitanja/16/2025" {
		t.Errorf("kanali 2025.: %v", za)
	}
	if za, _ := s.ChannelsFor(ctxRub, "dnevnici", 0, 0); len(za) != 0 {
		t.Errorf("kanali dnevnika: %v", za)
	}

	obrisano, err := s.PurgeUnwanted(ctxRub)
	if err != nil || len(obrisano) != 2 || obrisano["ocitanja/3/2026"] != 1 || obrisano["ocitanja/16/2025"] != 1 {
		t.Fatalf("obrisano %v %v", obrisano, err)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["ocitanja/16/2026"] != 1 || len(c) != 1 {
		t.Errorf("ostalo u knjizi: %v", c)
	}

	// brisanje koje padne na pola vraća što je dotad obrisano i grešku
	for _, k := range []string{"ocitanja/5/2026", "prijave/5/2026"} {
		if _, err := s.rec.RecordIn(ctxRub, s.db, k, "probni", "r-"+k, map[string]int{"cm": 1}); err != nil {
			t.Fatal(err)
		}
	}
	prijavaSIzvornikom(t, s, "p-5", 5, "2026-07-01")
	kvarUpisa(t, s, "DELETE", "prijave_izvornici", "")
	obrisano, err = s.PurgeUnwanted(ctxRub)
	if err == nil || obrisano["ocitanja/5/2026"] != 1 || len(obrisano) != 1 {
		t.Errorf("brisanje koje padne: %v %v", obrisano, err)
	}

	pokvariTablicu(t, s, "subscriptions")
	if _, err := s.Channels(ctxRub); err == nil {
		t.Error("popis kanala bez čitljivih pretplata")
	}
	if _, err := s.ChannelsFor(ctxRub, "", 0, 0); err == nil {
		t.Error("kanali za izvoz bez čitljivih pretplata")
	}
	if _, err := s.PurgeChannel(ctxRub, "prijave/5/2026"); err == nil {
		t.Error("brisanje kanala bez čitljivih pretplata")
	}
	pokvariTablicu(t, s, "record_versions")
	if _, err := s.PurgeUnwanted(ctxRub); err == nil {
		t.Error("brisanje bez čitljive knjige")
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
