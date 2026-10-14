package peers_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Brisanje kanala zapisa koji je nastao na laptopu, a ured ga je poslije
// mijenjao ili obrisao. Tuđa izmjena odlazi s laptopa, a zapis ostaje
// onakav kakav je nastao ovdje, odmah i nakon ponovnog pokretanja
// programa. Osobe su izmišljene: Ivo Ivić je vodočuvar BP 16.

// laptopIUred diže ured, koji prati sve, i laptop koji prati vodočuvarski
// dnevnik BP 16, svakog sa svojim spremištem sadržaja
func laptopIUred(t *testing.T, ctx context.Context) (ured, laptop *node, spU, spL *sadrzaj.Spremiste, pravilo peers.Subscription) {
	t.Helper()
	ured = startNodeBezRegistra(t, ctx, "cop-osijek")
	laptop = startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)
	prijasnje := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prijasnje) })
	spU, spL = otvoriSpremiste(t), otvoriSpremiste(t)
	ured.svc.SetSpremiste(spU)
	laptop.svc.SetSpremiste(spL)
	ured.svc.SetWantsAll(true)
	pravilo, err := laptop.svc.AddSubscription(ctx, peers.Subscription{Kind: ledger.ChannelVodocuvar, AreaID: 16, Razina: peers.RazinaSve})
	if err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(spL)
	return ured, laptop, spU, spL, pravilo
}

// listIve je Ivin dnevni list za dan obilaska
func listIve(opis string) *models.VodocuvarskiList {
	return &models.VodocuvarskiList{UserID: vodocuvarBP16, Ime: "Ivo Ivić", Sektor: "B", AreaID: 16, Datum: danObilaska, Opis: opis}
}

// obrisiKanalNaLaptopu makne pravilo i obriše kanal, a pregled prije
// brisanja mora reći isto što brisanje učini
func obrisiKanalNaLaptopu(t *testing.T, ctx context.Context, laptop *node, pravilo peers.Subscription, kanal string) peers.Brisanje {
	t.Helper()
	if err := laptop.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	pregled, err := laptop.svc.PregledBrisanja(ctx, kanal)
	if err != nil {
		t.Fatal(err)
	}
	obrisano, err := laptop.svc.PurgeChannel(ctx, kanal)
	if err != nil || obrisano != pregled {
		t.Fatalf("obrisano %+v, pregled %+v, %v", obrisano, pregled, err)
	}
	return obrisano
}

// ponovnoPokretanje obnovi površinu iz knjige kao program pri pokretanju
func ponovnoPokretanje(t *testing.T, ctx context.Context, n *node) {
	t.Helper()
	if _, err := repository.ReplaySurface(ctx, n.db, n.rec); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ObnoviPovrsinu(ctx, n.db, n.rec); err != nil {
		t.Fatal(err)
	}
}

// Ivin nacrt napisan na laptopu ured obriše, a laptop primi brisanje. Kad
// laptop zatim obriše kanal, nacrt se ne smije vratiti ni nakon ponovnog
// pokretanja, ni u ured kad ga Ivo na laptopu ponovno otvori. Do ispravka
// brisanje je odnijelo spomenik ureda, pa je nacrt u knjizi opet bio živ.
func TestBrisanjeKanalaNeVracaZapisKojiJeUredObrisao(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, _, _, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)

	l := listIve("nacrt s laptopa")
	if err := repository.NewVodocuvarRepository(laptop.db, laptop.rec).Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if err := repository.NewVodocuvarRepository(ured.db, ured.rec).Delete(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if imaListVodocuvara(t, ctx, laptop, vodocuvarBP16) {
		t.Fatal("brisanje iz ureda nije stiglo na laptop")
	}
	vlastitih := verzijaAutora(t, laptop, kanal, laptop.id)

	b := obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal)
	if b.Zapisa != 0 || b.Verzija == 0 || b.OstajeVerzija != vlastitih+1 {
		t.Errorf("brisanje %+v, vlastitih verzija prije %d", b, vlastitih)
	}
	ponovnoPokretanje(t, ctx, laptop)
	if imaListVodocuvara(t, ctx, laptop, vodocuvarBP16) {
		t.Fatal("nakon ponovnog pokretanja na laptopu se vratio nacrt koji je ured obrisao")
	}
	zadnja, err := laptop.rec.Latest(ctx, repository.EntityVodocuvarski, l.ID)
	if err != nil || !zadnja.Archived || zadnja.NodeID != laptop.id || zadnja.Channel != kanal {
		t.Fatalf("zadnja verzija nacrta na laptopu: %+v %v", zadnja, err)
	}

	razmijeni(t, ctx, laptop, ured)
	if imaListVodocuvara(t, ctx, ured, vodocuvarBP16) {
		t.Error("nacrt koji je ured obrisao vratio se u ured")
	}
	if verzijaAutora(t, laptop, kanal, ured.id) != 0 {
		t.Error("razmjena je vratila verzije ureda na laptop koji kanal ne prati")
	}
	if primljeno, poslano, err := laptop.svc.SyncWith(ctx, ured.id); err != nil || primljeno != 0 || poslano != 0 {
		t.Errorf("ponovljena razmjena: primljeno %d, poslano %d, %v", primljeno, poslano, err)
	}
}

// Isti Ivin list predan je u uredu i, kasnije, bez veze na laptopu. Na
// laptopu tada stoje dva lista: uredov pod oznakom lista, a laptopov kao
// dvojnik. Brisanje kanala odnese uredov list, a laptopov ostaje, odmah i
// nakon ponovnog pokretanja. Do ispravka brisanje je odnijelo laptopov
// list, a uredov ostavilo kao nastao ovdje.
func TestBrisanjeKanalaOstavljaDvojnikNastaoNaLaptopu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, _, _, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)

	u := listIve("predan u uredu")
	kad := time.Now().UTC()
	u.PredanoAt = &kad
	if err := repository.NewVodocuvarRepository(ured.db, ured.rec).Predaj(ctx, u, nil); err != nil {
		t.Fatal(err)
	}
	l := listIve("predan na laptopu")
	kasnije := kad.Add(time.Minute)
	l.PredanoAt = &kasnije
	if err := repository.NewVodocuvarRepository(laptop.db, laptop.rec).Predaj(ctx, l, nil); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if opisi := opisiListovaIve(t, ctx, laptop); len(opisi) != 2 {
		t.Fatalf("laptop ima listove %v, očekivana dva", opisi)
	}

	b := obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal)
	if b.Zapisa != 1 || b.OstajeZapisa != 1 {
		t.Errorf("brisanje %+v: očekivan jedan obrisan i jedan preostali list", b)
	}
	if opisi := opisiListovaIve(t, ctx, laptop); len(opisi) != 1 || opisi[0] != "predan na laptopu" {
		t.Fatalf("nakon brisanja na laptopu su listovi %v, očekivan samo predan na laptopu", opisi)
	}
	ponovnoPokretanje(t, ctx, laptop)
	if opisi := opisiListovaIve(t, ctx, laptop); len(opisi) != 1 || opisi[0] != "predan na laptopu" {
		t.Fatalf("nakon ponovnog pokretanja na laptopu su listovi %v", opisi)
	}
	razmijeni(t, ctx, laptop, ured)
	if opisi := opisiListovaIve(t, ctx, ured); len(opisi) != 2 {
		t.Errorf("u uredu su nakon brisanja na laptopu listovi %v, očekivana oba", opisi)
	}
}

// opisiListovaIve su opisi svih Ivinih listova za dan obilaska na čvoru
func opisiListovaIve(t *testing.T, ctx context.Context, n *node) []string {
	t.Helper()
	listovi, err := repository.NewVodocuvarRepository(n.db, n.rec).ListoviDana(ctx, vodocuvarBP16, danObilaska)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range listovi {
		out = append(out, x.Opis)
	}
	return out
}

// Ivo preda list na laptopu s PDF-om, a u uredu se izvornik zamijeni PDF-om
// ovjere. Brisanje kanala na laptopu odnese ovjeru iz ureda, a list s
// laptopa ostaje sa svojim PDF-om. Do ispravka je opis izvornika ostao i
// pokazivao na PDF ureda, koji je brisanje obrisalo.
func TestBrisanjeKanalaVracaIzvornikNastaoNaLaptopu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, spU, spL, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)

	l := listIve("predan na laptopu")
	kad := time.Now().UTC()
	l.PredanoAt = &kad
	rl := repository.NewVodocuvarRepository(laptop.db, laptop.rec)
	if err := rl.Predaj(ctx, l, nil); err != nil {
		t.Fatal(err)
	}
	pdfLaptopa, pdfUreda := []byte("%PDF list s laptopa"), []byte("%PDF ovjeren u uredu")
	if err := rl.SaveIzvornik(ctx, &models.IzvornikLista{ListID: l.ID, PDF: pdfLaptopa, Sazetak: "s"}); err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(spU)
	razmijeni(t, ctx, laptop, ured)
	if err := repository.NewVodocuvarRepository(ured.db, ured.rec).SaveIzvornik(ctx, &models.IzvornikLista{ListID: l.ID, PDF: pdfUreda, Sazetak: "s"}); err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(spL)
	razmijeni(t, ctx, laptop, ured)
	if iz, err := rl.GetIzvornik(ctx, l.ID); err != nil || iz == nil || string(iz.PDF) != string(pdfUreda) {
		t.Fatalf("ovjera iz ureda nije stigla na laptop: %+v %v", iz, err)
	}

	b := obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal)
	if b.Dokumenata != 1 || b.OstajeDokumenata != 1 {
		t.Errorf("brisanje %+v: očekivan jedan obrisan i jedan preostali dokument", b)
	}
	iz, err := rl.GetIzvornik(ctx, l.ID)
	if err != nil || iz == nil || string(iz.PDF) != string(pdfLaptopa) {
		t.Fatalf("nakon brisanja izvornik lista s laptopa je %+v, %v", iz, err)
	}
	if spL.Ima(ctx, sadrzaj.Otisak(pdfUreda)) {
		t.Error("PDF ovjere iz ureda ostao je na laptopu")
	}
	ponovnoPokretanje(t, ctx, laptop)
	if iz, err := rl.GetIzvornik(ctx, l.ID); err != nil || iz == nil || string(iz.PDF) != string(pdfLaptopa) {
		t.Fatalf("nakon ponovnog pokretanja izvornik je %+v, %v", iz, err)
	}
}

// Ivin nacrt s laptopa ured obriše, laptop makne praćenje, a ured zatim
// napiše Ivin list za isti dan, pod istom oznakom. Brisanje kanala na
// laptopu ponovi brisanje ureda vlastitim spomenikom, ali taj spomenik ne
// smije obrisati novi list u uredu, ni nakon razmjene. Kad laptop opet
// prati područje, dobije novi list. Do ispravka je spomenik nosio vrijeme
// brisanja, pa je bio noviji od lista ureda i obrisao ga.
func TestBrisanjeKanalaNeBriseNoviListUreda(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, _, _, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)
	ru := repository.NewVodocuvarRepository(ured.db, ured.rec)

	l := listIve("nacrt s laptopa")
	if err := repository.NewVodocuvarRepository(laptop.db, laptop.rec).Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if err := ru.Delete(ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if err := laptop.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	nov := listIve("novi list u uredu")
	if err := ru.Save(ctx, nov); err != nil || nov.ID != l.ID {
		t.Fatalf("novi list u uredu: %s (nacrt %s), %v", nov.ID, l.ID, err)
	}
	razmijeni(t, ctx, laptop, ured)

	if _, err := laptop.svc.PurgeChannel(ctx, kanal); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if opisi := opisiListovaIve(t, ctx, ured); len(opisi) != 1 || opisi[0] != "novi list u uredu" {
		t.Fatalf("u uredu su nakon brisanja na laptopu listovi %v", opisi)
	}
	if imaListVodocuvara(t, ctx, laptop, vodocuvarBP16) {
		t.Error("na laptopu je ostao list koji ne prati")
	}

	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{Kind: ledger.ChannelVodocuvar, AreaID: 16, Razina: peers.RazinaSve}); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if opisi := opisiListovaIve(t, ctx, laptop); len(opisi) != 1 || opisi[0] != "novi list u uredu" {
		t.Errorf("laptop koji opet prati područje ima listove %v", opisi)
	}
}

// Zadatak koji je ured zadao Ivi, a Ivo ga još nije zatvorio, nema list.
// Brisanje kanala ga odnese s laptopa, pregled ga broji, a ponovno
// pokretanje ga ne vraća. Do ispravka je ostajao na laptopu bez ijedne
// verzije u knjizi.
func TestBrisanjeKanalaOdnosiOtvoreniZadatak(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, _, _, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)
	z := &models.Zadatak{UserID: vodocuvarBP16, Sektor: "B", AreaID: 16, Tekst: "obići nasip Draž", ZadanoAt: danObilaska, Za: danObilaska, Status: models.ZadatakOtvoren}
	if err := repository.NewVodocuvarRepository(ured.db, ured.rec).SaveZadatak(ctx, z); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	zadataka := func() int {
		var n int
		if err := laptop.db.QueryRow(`SELECT count(*) FROM vodocuvarski_zadaci WHERE id = ?`, z.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if zadataka() != 1 {
		t.Fatal("zadatak iz ureda nije stigao na laptop")
	}

	if b := obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal); b.Zapisa != 1 {
		t.Errorf("brisanje %+v: očekivan jedan obrisan zadatak", b)
	}
	if zadataka() != 0 {
		t.Fatal("otvoreni zadatak ureda ostao je na laptopu")
	}
	ponovnoPokretanje(t, ctx, laptop)
	if zadataka() != 0 {
		t.Error("ponovno pokretanje vratilo je zadatak")
	}
}

// Ivin list predan je u uredu bez PDF-a, a kasnije na laptopu s PDF-om, pa
// je list s laptopa dvojnik. Nakon brisanja kanala list s laptopa odmah ima
// svoj PDF, a ne tek nakon ponovnog pokretanja.
func TestBrisanjeKanalaVracaPDFDvojnikaNastalogNaLaptopu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, _, _, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)

	u := listIve("predan u uredu")
	kad := time.Now().UTC()
	u.PredanoAt = &kad
	if err := repository.NewVodocuvarRepository(ured.db, ured.rec).Predaj(ctx, u, nil); err != nil {
		t.Fatal(err)
	}
	l := listIve("predan na laptopu")
	kasnije := kad.Add(time.Minute)
	l.PredanoAt = &kasnije
	rl := repository.NewVodocuvarRepository(laptop.db, laptop.rec)
	if err := rl.Predaj(ctx, l, nil); err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF list s laptopa")
	if err := rl.SaveIzvornik(ctx, &models.IzvornikLista{ListID: l.ID, PDF: pdf, Sazetak: "s"}); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, laptop, ured)
	if opisi := opisiListovaIve(t, ctx, laptop); len(opisi) != 2 {
		t.Fatalf("laptop ima listove %v, očekivana dva", opisi)
	}

	obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal)
	listovi, err := rl.ListoviDana(ctx, vodocuvarBP16, danObilaska)
	if err != nil || len(listovi) != 1 || listovi[0].Opis != "predan na laptopu" {
		t.Fatalf("nakon brisanja listovi %+v, %v", listovi, err)
	}
	if iz, err := rl.GetIzvornik(ctx, listovi[0].ID); err != nil || iz == nil || string(iz.PDF) != string(pdf) {
		t.Errorf("nakon brisanja list s laptopa ima izvornik %+v, %v", iz, err)
	}
}

// Perin list s fotografijom stigao je iz ureda, a rukovoditelj je na
// laptopu upisao zapažanje. List ostaje jer je mijenjan ovdje, a s njim i
// fotografija. Do ispravka je fotografija obrisana, a list je na nju i
// dalje pokazivao.
func TestBrisanjeKanalaOstavljaFotografijuListaMijenjanogNaLaptopu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured, laptop, spU, spL, pravilo := laptopIUred(t, ctx)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)
	foto := listSFotografijomZa(t, ctx, ured, spU, vodocuvarPero, "Pero Perić", "foto-pero")
	repository.SetSpremiste(spL)
	razmijeni(t, ctx, laptop, ured)
	if fotografijaNaCvoru(t, ctx, laptop, spL, "foto-pero") != string(foto) {
		t.Fatal("fotografija nije stigla na laptop")
	}
	rl := repository.NewVodocuvarRepository(laptop.db, laptop.rec)
	l, err := rl.ZaDan(ctx, vodocuvarPero, danObilaska)
	if err != nil || l == nil {
		t.Fatalf("Perin list na laptopu: %v", err)
	}
	l.Zapazanja = "rukovoditelj: nasip pregledan"
	if err := rl.Save(ctx, l); err != nil {
		t.Fatal(err)
	}

	if b := obrisiKanalNaLaptopu(t, ctx, laptop, pravilo, kanal); b.Dokumenata != 0 || b.OstajeDokumenata != 1 {
		t.Errorf("brisanje %+v: fotografija lista koji ostaje mora ostati", b)
	}
	if fotografijaNaCvoru(t, ctx, laptop, spL, "foto-pero") != string(foto) {
		t.Error("fotografija lista koji je ostao na laptopu obrisana je")
	}
}
