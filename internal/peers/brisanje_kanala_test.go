package peers

// Brisanje kanala na jednom čvoru, bez mreže: što odlazi s površine, iz
// knjige i iz spremišta, što ostaje jer je nastalo ovdje, i da pregled
// prije brisanja kaže isto što brisanje učini. Zapisi koji su „stigli s
// ureda” upisani su drugim autorom u istu bazu, kao da ih je razmjena
// donijela.

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// tudjaVerzija upisuje verziju zapisa kakvu je ovaj čvor primio s ureda
func tudjaVerzija(t *testing.T, s *Service, kanal, entitet, id string) {
	t.Helper()
	if _, err := ledger.New(s.db, "cop-osijek-ured").RecordIn(ctxRub, s.db, kanal, entitet, id, map[string]string{"id": id}); err != nil {
		t.Fatal(err)
	}
}

// vlastitaVerzija upisuje verziju zapisa koju je napisao ovaj čvor
func vlastitaVerzija(t *testing.T, s *Service, kanal, entitet, id string) {
	t.Helper()
	if _, err := s.rec.RecordIn(ctxRub, s.db, kanal, entitet, id, map[string]string{"id": id}); err != nil {
		t.Fatal(err)
	}
}

// vodocuvarskiList upisuje dnevni list vodočuvara mimo knjige, s izvornikom
// i zadatkom
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

// dnevnikSListomIUpisom upisuje dnevnik kanala s jednim listom i upisom
func dnevnikSListomIUpisom(t *testing.T, s *Service, id, kanal string) {
	t.Helper()
	sada := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO journals (id, kind, channel, created_at, updated_at) VALUES (?, 'A.01', ?, ?, ?)`, id, kanal, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO journal_entries (id, journal_id, date, kind, created_at, updated_at) VALUES (?, ?, '2026-03-02', 'rad', ?, ?)`, "e-"+id, id, sada, sada); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO journal_sheets (id, journal_id, date, created_at, updated_at) VALUES (?, ?, '2026-03-02', ?, ?)`, "l-"+id, id, sada, sada); err != nil {
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

// obrisiKanal briše kanal i provjerava da je pregled prije brisanja rekao
// isto
func obrisiKanal(t *testing.T, s *Service, kanal string) Brisanje {
	t.Helper()
	pregled, err := s.PregledBrisanja(ctxRub, kanal)
	if err != nil {
		t.Fatalf("pregled %s: %v", kanal, err)
	}
	b, err := s.PurgeChannel(ctxRub, kanal)
	if err != nil {
		t.Fatalf("brisanje %s: %v", kanal, err)
	}
	if b != pregled {
		t.Errorf("%s: pregled %+v, obrisano %+v", kanal, pregled, b)
	}
	return b
}

// Brisanje kanala dnevnika, vodočuvarskih listova i prijava koji su stigli
// s ureda miče s površine samo zapise tog područja i godine (s upisima,
// izvornicima i zadacima), a iz knjige verzije kanala. Ostala područja i
// godine ostaju.
func TestBrisanjeKanalaSvakeVrste(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")

	// dnevnik BP 16 s upisom i listom, i dnevnik BP 3 koji ostaje
	for _, d := range []struct{ id, kanal string }{{"d-16", "dnevnici/16/2026"}, {"d-3", "dnevnici/3/2026"}} {
		dnevnikSListomIUpisom(t, s, d.id, d.kanal)
		tudjaVerzija(t, s, d.kanal, "journals", d.id)
	}
	if b := obrisiKanal(t, s, "dnevnici/16/2026"); b != (Brisanje{Kanal: "dnevnici/16/2026", Zapisa: 3, Verzija: 1}) {
		t.Errorf("brisanje dnevnika: %+v", b)
	}
	if prebroji(t, s, `SELECT count(*) FROM journals`) != 1 || prebroji(t, s, `SELECT count(*) FROM journal_entries WHERE journal_id = 'd-16'`) != 0 ||
		prebroji(t, s, `SELECT count(*) FROM journal_sheets WHERE journal_id = 'd-16'`) != 0 || prebroji(t, s, `SELECT count(*) FROM journal_entries`) != 1 {
		t.Error("dnevnik BP 16 nije obrisan cijeli ili je obrisan i BP 3")
	}

	// vodočuvarski listovi: ista godina drugog područja i druga godina ostaju
	vodocuvarskiList(t, s, "v-16", 16, "2026-05-04")
	vodocuvarskiList(t, s, "v-16-lani", 16, "2025-05-04")
	vodocuvarskiList(t, s, "v-3", 3, "2026-05-04")
	tudjaVerzija(t, s, "vodocuvarski/16/2026", "vodocuvarski_listovi", "v-16")
	tudjaVerzija(t, s, "vodocuvarski/16/2026", "vodocuvarski_zadaci", "z-v-16")
	tudjaVerzija(t, s, "vodocuvarski/16/2025", "vodocuvarski_zadaci", "z-v-16-lani")
	if b := obrisiKanal(t, s, "vodocuvarski/16/2026"); b.Zapisa != 3 || b.Verzija != 2 || b.OstajeZapisa != 0 {
		t.Errorf("brisanje vodočuvarskih listova: %+v", b)
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
	tudjaVerzija(t, s, "prijave/16/2026", "prijave", "p-16")
	if b := obrisiKanal(t, s, "prijave/16/2026"); b.Zapisa != 2 || b.Verzija != 1 {
		t.Errorf("brisanje prijava: %+v", b)
	}
	if prebroji(t, s, `SELECT count(*) FROM prijave`) != 1 || prebroji(t, s, `SELECT count(*) FROM prijave_izvornici`) != 1 ||
		prebroji(t, s, `SELECT count(*) FROM prijave WHERE id = 'p-3'`) != 1 {
		t.Error("prijave: obrisano krivo područje ili ništa")
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["dnevnici/16/2026"]+c["vodocuvarski/16/2026"]+c["prijave/16/2026"] != 0 || c["dnevnici/3/2026"] != 1 {
		t.Errorf("knjiga nakon brisanja: %v", c)
	}

	// kanal nepoznate vrste se ne briše
	tudjaVerzija(t, s, "vodostaji/16/2026", "probni", "x")
	if _, err := s.PurgeChannel(ctxRub, "vodostaji/16/2026"); err == nil || !strings.Contains(err.Error(), "nepoznata vrsta kanala") {
		t.Errorf("nepoznata vrsta: %v", err)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["vodostaji/16/2026"] != 1 {
		t.Error("knjiga kanala nepoznate vrste je obrisana")
	}
}

// Odluke (13. 10. 2026.): brisanje kanala ne dira ono što je nastalo na
// ovom računalu. U svakoj vrsti jedan zapis je napisan ovdje, a drugi je
// stigao s ureda: prvi ostaje s površine i iz knjige, drugi odlazi.
// Dnevnik s ureda u koji je ovdje upisan rad ostaje, jer bez njega upis
// nema gdje stajati; njegov list s ureda odlazi.
func TestBrisanjeKanalaOstavljaNastaloOvdje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	oc := "ocitanja/16/2026"
	if _, err := s.db.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, created_at, updated_at, channel)
		VALUES ('r-nase', 'st-1', '2026-03-01 07:00:00', 210, '2026-03-01', '2026-03-01', ?), ('r-ured', 'st-1', '2026-03-02 07:00:00', 215, '2026-03-02', '2026-03-02', ?)`, oc, oc); err != nil {
		t.Fatal(err)
	}
	vlastitaVerzija(t, s, oc, "readings", "r-nase")
	tudjaVerzija(t, s, oc, "readings", "r-nase") // ured je naše očitanje ispravio
	tudjaVerzija(t, s, oc, "readings", "r-ured")
	if b := obrisiKanal(t, s, oc); b != (Brisanje{Kanal: oc, Zapisa: 1, Verzija: 2, OstajeZapisa: 1, OstajeVerzija: 1}) {
		t.Errorf("očitanja: %+v", b)
	}
	if prebroji(t, s, `SELECT count(*) FROM readings WHERE id = 'r-nase'`) != 1 || prebroji(t, s, `SELECT count(*) FROM readings WHERE id = 'r-ured'`) != 0 {
		t.Error("očitanja: obrisano krivo")
	}

	vk := "vodocuvarski/16/2026"
	vodocuvarskiList(t, s, "v-ivo", 16, "2026-05-04")
	vodocuvarskiList(t, s, "v-pero", 16, "2026-05-04")
	vlastitaVerzija(t, s, vk, "vodocuvarski_listovi", "v-ivo")
	vlastitaVerzija(t, s, vk, "vodocuvarski_zadaci", "z-v-ivo")
	tudjaVerzija(t, s, vk, "vodocuvarski_izvornici", "v-ivo") // ured je ovjerio Ivin list
	tudjaVerzija(t, s, vk, "vodocuvarski_listovi", "v-pero")
	tudjaVerzija(t, s, vk, "vodocuvarski_zadaci", "z-v-pero")
	if b := obrisiKanal(t, s, vk); b != (Brisanje{Kanal: vk, Zapisa: 4, Verzija: 3, OstajeZapisa: 2, OstajeVerzija: 2}) {
		t.Errorf("vodočuvarski dnevnik: %+v", b)
	}
	if prebroji(t, s, `SELECT count(*) FROM vodocuvarski_listovi WHERE id = 'v-ivo'`) != 1 || prebroji(t, s, `SELECT count(*) FROM vodocuvarski_zadaci WHERE id = 'z-v-ivo'`) != 1 ||
		prebroji(t, s, `SELECT count(*) FROM vodocuvarski_listovi WHERE id = 'v-pero'`) != 0 || prebroji(t, s, `SELECT count(*) FROM vodocuvarski_izvornici`) != 0 {
		t.Error("vodočuvarski dnevnik: obrisano krivo")
	}

	pk := "prijave/16/2026"
	prijavaSIzvornikom(t, s, "p-nasa", 16, "2026-07-01")
	prijavaSIzvornikom(t, s, "p-ured", 16, "2026-07-02")
	vlastitaVerzija(t, s, pk, "prijave", "p-nasa")
	vlastitaVerzija(t, s, pk, "prijave_izvornici", "p-nasa")
	tudjaVerzija(t, s, pk, "prijave", "p-ured")
	if b := obrisiKanal(t, s, pk); b != (Brisanje{Kanal: pk, Zapisa: 2, Verzija: 1, OstajeZapisa: 2, OstajeVerzija: 2}) {
		t.Errorf("prijave: %+v", b)
	}

	dk := "dnevnici/16/2026"
	dnevnikSListomIUpisom(t, s, "d-ured", dk)
	tudjaVerzija(t, s, dk, "journals", "d-ured")
	tudjaVerzija(t, s, dk, "journal_sheets", "l-d-ured")
	vlastitaVerzija(t, s, dk, "journal_entries", "e-d-ured")
	if b := obrisiKanal(t, s, dk); b != (Brisanje{Kanal: dk, Zapisa: 1, Verzija: 2, OstajeZapisa: 2, OstajeVerzija: 1}) {
		t.Errorf("dnevnici: %+v", b)
	}
	if prebroji(t, s, `SELECT count(*) FROM journals`) != 1 || prebroji(t, s, `SELECT count(*) FROM journal_entries`) != 1 || prebroji(t, s, `SELECT count(*) FROM journal_sheets`) != 0 {
		t.Error("dnevnici: obrisano krivo")
	}

	// drugo brisanje istog kanala nema više što obrisati
	if b := obrisiKanal(t, s, pk); b != (Brisanje{Kanal: pk, OstajeZapisa: 2, OstajeVerzija: 2}) {
		t.Errorf("drugo brisanje: %+v", b)
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
		tudjaVerzija(t, s, p.kanal, "probni", "r-"+p.kanal)
		kvarUpisa(t, s, p.kvar[0], p.kvar[1], p.kvar[2])
		if b, err := s.PurgeChannel(ctxRub, p.kanal); err == nil || b != (Brisanje{Kanal: p.kanal}) {
			t.Errorf("%s: brisanje je prošlo iako upis u %s nije: %+v", p.kanal, p.kvar[1], b)
		}
		if prebroji(t, s, p.povrsina) != 1 {
			t.Errorf("%s: površina je obrisana", p.kanal)
		}
		if c, _ := s.rec.CountByChannel(ctxRub); c[p.kanal] != 1 {
			t.Errorf("%s: knjiga kanala %v", p.kanal, c)
		}
	}
}

// Dokumenti kanala brišu se sa spremišta nakon knjige: odlaze oni s drugih
// čvorova, a ostaju oni nastali ovdje. Kad spremište ne radi, brisanje javi
// grešku i što je iz baze već obrisano.
func TestBrisanjeKanalaISadrzaja(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpremiste(sp)
	kanal := "prijave/16/2026"
	tudji := []byte("%PDF-1.4 prijava Pere Perića")
	otisak, err := sp.Upisi(ctxRub, "application/pdf", tudji, "cvor:ured", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-16", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	nas, err := sp.Upisi(ctxRub, "image/jpeg", []byte("fotografija Ive Ivića"), "", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-ivo", Uloga: "slika:1", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	drugi, _ := sp.Upisi(ctxRub, "application/pdf", []byte("%PDF-1.4 prijava BP 3"), "cvor:ured", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-3", Uloga: "izvornik", Kanal: "prijave/3/2026"})
	for _, k := range []string{kanal, "prijave/17/2026"} {
		tudjaVerzija(t, s, k, "probni", "r-"+k)
	}
	want := Brisanje{Kanal: kanal, Verzija: 1, Dokumenata: 1, Bajtova: int64(len(tudji)), OstajeDokumenata: 1}
	if b := obrisiKanal(t, s, kanal); b != want {
		t.Fatalf("brisanje: %+v", b)
	}
	if sp.Ima(ctxRub, otisak) || !sp.Ima(ctxRub, nas) || !sp.Ima(ctxRub, drugi) {
		t.Errorf("sadržaj nakon brisanja kanala: s ureda %v, naš %v, BP 3 %v", sp.Ima(ctxRub, otisak), sp.Ima(ctxRub, nas), sp.Ima(ctxRub, drugi))
	}

	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PregledBrisanja(ctxRub, "prijave/17/2026"); err == nil {
		t.Error("pregled uz zatvoreno spremište")
	}
	b, err := s.PurgeChannel(ctxRub, "prijave/17/2026")
	if err == nil || b.Verzija != 1 || !strings.HasPrefix(err.Error(), "Obrisani su zapisi koji su stigli s drugih računala, ali njihovi dokumenti nisu.") {
		t.Errorf("zatvoreno spremište: %+v %v", b, err)
	}
	if _, err := s.Channels(ctxRub); err == nil {
		t.Error("popis kanala uz zatvoreno spremište")
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["prijave/17/2026"] != 0 {
		t.Errorf("knjiga kanala nije obrisana: %v", c)
	}
}

// Popis kanala kaže koje pretplata pokriva. Kanal pod pretplatom ne briše
// se, ostali da. Kad se pretplate ili knjiga ne mogu pročitati, ne briše se
// ništa.
func TestBrisanjeNezeljenihKanala(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	for _, k := range []string{"ocitanja/16/2026", "ocitanja/3/2026", "ocitanja/16/2025"} {
		tudjaVerzija(t, s, k, "probni", "r-"+k)
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

	popis, err := s.Channels(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	obrisano := map[string]int{}
	for _, c := range popis {
		if c.Wanted {
			if _, err := s.PurgeChannel(ctxRub, c.Channel); err == nil || !strings.HasPrefix(err.Error(), "Ništa nije obrisano") {
				t.Errorf("kanal pod pretplatom %s: %v", c.Channel, err)
			}
			continue
		}
		obrisano[c.Channel] = obrisiKanal(t, s, c.Channel).Verzija
	}
	if len(obrisano) != 2 || obrisano["ocitanja/3/2026"] != 1 || obrisano["ocitanja/16/2025"] != 1 {
		t.Errorf("obrisano %v", obrisano)
	}
	if c, _ := s.rec.CountByChannel(ctxRub); c["ocitanja/16/2026"] != 1 || len(c) != 1 {
		t.Errorf("ostalo u knjizi: %v", c)
	}

	bezKnjige := rubniCvor(t, "laptop-drugi")
	pokvariTablicu(t, bezKnjige, "record_versions")
	if _, err := bezKnjige.PregledBrisanja(ctxRub, "prijave/5/2026"); err == nil {
		t.Error("pregled brisanja bez čitljive knjige")
	}

	pokvariTablicu(t, s, "subscriptions")
	if _, err := s.Channels(ctxRub); err == nil {
		t.Error("popis kanala bez čitljivih pretplata")
	}
	if _, err := s.ChannelsFor(ctxRub, "", 0, 0); err == nil {
		t.Error("kanali za izvoz bez čitljivih pretplata")
	}
	if _, err := s.PurgeChannel(ctxRub, "ocitanja/3/2026"); err == nil {
		t.Error("brisanje kanala bez čitljivih pretplata")
	}
}

// Kanal u kojem su ostali samo dokumenti s drugih računala, bez ijedne
// verzije u knjizi, stoji u tablici „Što računalo drži”, pa se brisanje
// dokumenata može ponoviti. Nakon brisanja ga više nema.
func TestKanalSamoSDokumentimaSeDaObrisati(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sp.Zatvori() })
	s.SetSpremiste(sp)
	kanal := "prijave/16/2026"
	tudji := []byte("%PDF-1.4 prijava Pere Perića")
	otisak, err := sp.Upisi(ctxRub, "application/pdf", tudji, "cvor:ured", sadrzaj.Veza{Entitet: "prijave", EntitetID: "p-16", Uloga: "izvornik", Kanal: kanal})
	if err != nil {
		t.Fatal(err)
	}
	tudjaVerzija(t, s, "prijave/3/2026", "prijave", "p-3")

	popis, err := s.Channels(ctxRub)
	if err != nil || len(popis) != 2 || popis[0].Channel != "prijave/3/2026" || popis[1].Channel != kanal || popis[1].Versions != 0 || popis[1].Wanted {
		t.Fatalf("kanali %+v, %v", popis, err)
	}
	if b := obrisiKanal(t, s, kanal); b != (Brisanje{Kanal: kanal, Dokumenata: 1, Bajtova: int64(len(tudji))}) {
		t.Errorf("brisanje: %+v", b)
	}
	if sp.Ima(ctxRub, otisak) {
		t.Error("dokument je ostao")
	}
	if popis, err := s.Channels(ctxRub); err != nil || len(popis) != 1 {
		t.Errorf("kanali nakon brisanja %+v, %v", popis, err)
	}
}

// Dokument treba zapisu koji ostaje kad ga spominje vlastita verzija, a
// zapis nakon brisanja ostaje: zadnja vlastita verzija je živa, a zadnja
// verzija nije tuđe brisanje.
func TestDokumentTrebaZapisuKojiOstaje(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	kanal := "prijave/16/2026"
	ured := ledger.New(s.db, "cop-osijek-ured")
	upis := func(rec *ledger.Recorder, id string, obrisano bool, otisak string) {
		t.Helper()
		zapis := map[string]string{"id": id, "otisak": otisak}
		var err error
		if obrisano {
			_, err = rec.ArchiveIn(ctxRub, s.db, kanal, "prijave", id, zapis)
		} else {
			_, err = rec.RecordIn(ctxRub, s.db, kanal, "prijave", id, zapis)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	upis(s.rec, "p-ziva", false, "fotografija-1")
	upis(s.rec, "p-ured-obrisao", false, "fotografija-1")
	upis(ured, "p-ured-obrisao", true, "fotografija-1")
	upis(s.rec, "p-obrisana-ovdje", false, "fotografija-1")
	upis(s.rec, "p-obrisana-ovdje", true, "")
	upis(ured, "p-ureda", false, "fotografija-1")
	upis(s.rec, "p-ured-ispravio", false, "fotografija-1")
	upis(ured, "p-ured-ispravio", false, "fotografija-2")
	for _, c := range []struct {
		id, otisak string
		treba      bool
	}{
		{"p-ziva", "fotografija-1", true},
		{"p-ziva", "fotografija-2", false},
		{"p-ured-obrisao", "fotografija-1", false},
		{"p-obrisana-ovdje", "fotografija-1", false},
		{"p-ureda", "fotografija-1", false},
		{"p-ured-ispravio", "fotografija-1", true},
		{"p-ured-ispravio", "fotografija-2", false},
		{"p-nema", "fotografija-1", false},
	} {
		if treba, err := s.trebaZapisuKojiOstaje(ctxRub, c.otisak, "prijave", c.id); err != nil || treba != c.treba {
			t.Errorf("%s, %s: treba %v, %v", c.id, c.otisak, treba, err)
		}
	}
	pokvariTablicu(t, s, "record_versions")
	if _, err := s.trebaZapisuKojiOstaje(ctxRub, "fotografija-1", "prijave", "p-ziva"); err == nil {
		t.Error("provjera bez čitljive knjige")
	}
}
