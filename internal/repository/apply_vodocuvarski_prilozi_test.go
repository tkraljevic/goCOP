package repository

import (
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Fotografije uz dnevni list vodočuvara: idu u kanal lista, a čvor koji
// list primi veže ih uz list i traži one koje nema. Osobe su izmišljene:
// vodočuvari Ivo Ivić i Marko Markić.

// dvaSpremista otvara dva spremišta sadržaja, za dva probna čvora.
// Spremište je jedno za program, pa test prije svakog koraka postavi
// spremište čvora koji radi (SetSpremiste); na kraju se vraća prijašnje.
func dvaSpremista(t *testing.T) (*sadrzaj.Spremiste, *sadrzaj.Spremiste) {
	t.Helper()
	prijasnje := Spremiste()
	t.Cleanup(func() { SetSpremiste(prijasnje) })
	return novoSpremiste(t), novoSpremiste(t)
}

func novoSpremiste(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	s, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	t.Cleanup(func() { _ = s.Zatvori() })
	return s
}

// fotografijaNaListu upisuje na čvoru list s fotografijom kako to radi uvoz
// obilazaka: prvo list s opisom fotografije, zatim samu fotografiju
func fotografijaNaListu(t *testing.T, n *cvor, l *models.VodocuvarskiList, prilogID, opis string) []byte {
	t.Helper()
	foto := []byte("\xff\xd8 " + opis)
	l.Prilozi = append(l.Prilozi, models.PrilogLista{ID: prilogID, Naziv: "fotografija 1", Vrsta: "image/jpeg",
		Bajtova: len(foto), Sadrzaj: sadrzaj.Otisak(foto)})
	repo := NewVodocuvarRepository(n.db, n.rec)
	if l.Predan() {
		nuzno(t, repo.Predaj(ctxRaz, l, nil))
	} else {
		nuzno(t, repo.Save(ctxRaz, l))
	}
	nuzno(t, repo.SavePrilog(ctxRaz, l.ID, prilogID, foto))
	return foto
}

// jednaZelja provjerava da je na popisu za dohvat samo zadana fotografija
func jednaZelja(t *testing.T, sp *sadrzaj.Spremiste, otisak, kanal string, bajtova int) {
	t.Helper()
	z, err := sp.Zeljeni(ctxRaz, 10)
	nuzno(t, err)
	if len(z) != 1 || z[0].Otisak != otisak || z[0].Kanal != kanal || z[0].Vrsta != "image/jpeg" || z[0].Bajtova != bajtova {
		t.Errorf("popis za dohvat: %+v; očekivana fotografija %.12s u kanalu %s", z, otisak, kanal)
	}
}

// Fotografija koju uvoz spremi uz list ide u kanal lista; fotografija lista
// kojega nema ostaje bez kanala.
func TestFotografijaListaIdeUKanalLista(t *testing.T) {
	spA, _ := dvaSpremista(t)
	SetSpremiste(spA)
	a := noviCvor(t, "laptop")
	foto := fotografijaNaListu(t, a, listIve("obilazak nasipa"), "foto-1", "nasip kod ustave")
	if _, _, kanal, err := spA.CitajSKanalom(ctxRaz, sadrzaj.Otisak(foto)); err != nil || kanal != "vodocuvarski/16/2026" {
		t.Errorf("fotografija je u kanalu %q (%v), očekivan kanal lista", kanal, err)
	}
	repo := NewVodocuvarRepository(a.db, a.rec)
	if b, err := repo.Prilog(ctxRaz, "foto-1"); err != nil || string(b) != string(foto) {
		t.Errorf("fotografija se ne čita: %q %v", b, err)
	}
	bez := []byte("\xff\xd8 fotografija bez lista")
	nuzno(t, repo.SavePrilog(ctxRaz, "01900000-0000-7000-8000-0000000000ff", "foto-2", bez))
	if _, _, kanal, _ := spA.CitajSKanalom(ctxRaz, sadrzaj.Otisak(bez)); kanal != "" {
		t.Errorf("fotografija bez lista u kanalu %q", kanal)
	}
}

// Ured primi list s laptopa: fotografiju veže uz list i stavi je na popis za
// dohvat, u kanalu lista, bez ijedne verzije u knjizi. Ponovna primjena i
// obnova površine je ne traže dvaput. Kad fotografija stigne i ured je
// poslije otpusti, nova verzija lista je ne traži iznova.
func TestPrimljeniListTraziSvojuFotografiju(t *testing.T) {
	spA, spB := dvaSpremista(t)
	a, b := noviCvor(t, "laptop"), noviCvor(t, "ured")
	SetSpremiste(spA)
	l := listIve("obilazak nasipa")
	foto := fotografijaNaListu(t, a, l, "foto-1", "nasip kod ustave")
	otisak := sadrzaj.Otisak(foto)

	SetSpremiste(spB)
	posalji(t, a, b)
	if got := spB.OtisakPoUlozi(ctxRaz, EntityVodocuvarski, ulogaPriloga("foto-1")); got != otisak {
		t.Errorf("primljeni list ne veže fotografiju: %q", got)
	}
	jednaZelja(t, spB, otisak, "vodocuvarski/16/2026", len(foto))
	var vlastitih int
	nuzno(t, b.db.QueryRow(`SELECT count(*) FROM record_versions WHERE node_id = ?`, b.ime).Scan(&vlastitih))
	if vlastitih != 0 {
		t.Errorf("primjena je upisala %d verzija u knjigu", vlastitih)
	}

	posalji(t, a, b)
	_, err := ReplaySurface(ctxRaz, b.db, b.rec)
	nuzno(t, err)
	jednaZelja(t, spB, otisak, "vodocuvarski/16/2026", len(foto))

	nuzno(t, spB.UpisiProvjereno(ctxRaz, otisak, "image/jpeg", foto, "cvor:laptop"))
	nuzno(t, spB.Otpusti(ctxRaz, otisak))
	SetSpremiste(spA)
	l.Opis = "obilazak nasipa, dopunjeno"
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, l))
	SetSpremiste(spB)
	posalji(t, a, b)
	if z, _ := spB.Zeljeni(ctxRaz, 10); len(z) != 0 {
		t.Errorf("otpuštena fotografija tražena iznova: %+v", z)
	}
}

// Isti list Ive predan na laptopu i na tabletu, svaki s drugom
// fotografijom: ured pokazuje oba lista i veže obje fotografije. Traži samo
// onu s tableta, jer onu s laptopa već ima.
func TestDvostrukiListVezeFotografijeObaPotpisa(t *testing.T) {
	spA, spB := dvaSpremista(t)
	spC := novoSpremiste(t)
	a, c, b := noviCvor(t, "laptop"), noviCvor(t, "tablet"), noviCvor(t, "ured")
	kad := time.Now()
	kasnije := kad.Add(time.Minute)
	SetSpremiste(spA)
	prvi := listIve("predan na laptopu")
	prvi.PredanoAt = &kad
	fotoA := fotografijaNaListu(t, a, prvi, "foto-laptop", "ustava s laptopa")
	SetSpremiste(spC)
	drugi := listIve("predan na tabletu")
	drugi.PredanoAt = &kasnije
	fotoC := fotografijaNaListu(t, c, drugi, "foto-tablet", "ustava s tableta")

	SetSpremiste(spB)
	nuzno(t, spB.UpisiProvjereno(ctxRaz, sadrzaj.Otisak(fotoA), "image/jpeg", fotoA, "cvor:laptop"))
	posalji(t, a, b)
	posalji(t, c, b)
	if n := len(listoviIve(t, b)); n != 2 {
		t.Fatalf("ured ima %d listova Ive, očekivana dva", n)
	}
	for _, p := range []struct{ prilog, otisak string }{
		{"foto-laptop", sadrzaj.Otisak(fotoA)}, {"foto-tablet", sadrzaj.Otisak(fotoC)},
	} {
		if got := spB.OtisakPoUlozi(ctxRaz, EntityVodocuvarski, ulogaPriloga(p.prilog)); got != p.otisak {
			t.Errorf("fotografija %s nije vezana: %q", p.prilog, got)
		}
	}
	jednaZelja(t, spB, sadrzaj.Otisak(fotoC), "vodocuvarski/16/2026", len(fotoC))
}

// Fotografije upisane prije 0.0.38: na laptopu su bez kanala, a ured koji
// je listove primio nema ni vezu ni želju. Korak pri pokretanju laptopu
// upiše kanal svakog lista, a uredu vezu i želju; drugi put ne upisuje
// ništa. Ista fotografija uz dva lista dobiva kanal ranijeg i na popis ide
// jednom. Opis fotografije bez otiska preskače, a opis bez vrste je
// fotografija.
func TestPopravakFotografijaPriPokretanju(t *testing.T) {
	spA, spB := dvaSpremista(t)
	a, b := noviCvor(t, "laptop"), noviCvor(t, "ured")
	SetSpremiste(nil)
	if p, err := PopraviPrilogeListova(ctxRaz, a.db); p != (PopravakPriloga{}) || err != nil {
		t.Errorf("bez spremišta: %+v %v", p, err)
	}

	// kako su upisivali uvoz i 0.0.37: opis na listu, fotografija bez kanala
	repo := NewVodocuvarRepository(a.db, a.rec)
	bez := listIve("list bez fotografija")
	bez.Datum = danIve.AddDate(0, 0, 1)
	nuzno(t, repo.Save(ctxRaz, bez))
	foto16 := []byte("\xff\xd8 nasip kod ustave, 2026.")
	foto25 := []byte("\xff\xd8 crpna stanica, 2025.")
	stari := &models.VodocuvarskiList{UserID: "01900000-0000-7000-8000-0000000017a3", Ime: "Marko Markić", Sektor: "B", AreaID: 16,
		Datum: time.Date(2025, time.May, 20, 0, 0, 0, 0, models.Zagreb), Rekonstrukcija: true, Prilozi: []models.PrilogLista{
			{ID: "foto-2025", Bajtova: len(foto25), Sadrzaj: sadrzaj.Otisak(foto25)},
			{ID: "foto-bez-otiska", Vrsta: "image/jpeg"},
		}}
	nuzno(t, repo.Save(ctxRaz, stari))
	l := listIve("obilazak nasipa")
	l.ID = models.IDListaVodocuvara(ivoIvic, danIve)
	l.Prilozi = []models.PrilogLista{
		{ID: "foto-2026", Vrsta: "image/jpeg", Bajtova: len(foto16), Sadrzaj: sadrzaj.Otisak(foto16)},
		{ID: "foto-2026-ista", Vrsta: "image/jpeg", Bajtova: len(foto25), Sadrzaj: sadrzaj.Otisak(foto25)},
	}
	nuzno(t, repo.Save(ctxRaz, l))
	for _, f := range []struct {
		lista, prilog string
		foto          []byte
	}{{l.ID, "foto-2026", foto16}, {stari.ID, "foto-2025", foto25}} {
		_, err := spA.Upisi(ctxRaz, "image/jpeg", f.foto, "ovdje", sadrzaj.Veza{Entitet: EntityVodocuvarski, EntitetID: f.lista, Uloga: ulogaPriloga(f.prilog)})
		nuzno(t, err)
	}
	posalji(t, a, b)

	SetSpremiste(spA)
	if p, err := PopraviPrilogeListova(ctxRaz, a.db); p != (PopravakPriloga{Kanal: 2, Veza: 1}) || err != nil {
		t.Errorf("laptop: %+v %v", p, err)
	}
	for otisak, kanal := range map[string]string{sadrzaj.Otisak(foto16): "vodocuvarski/16/2026", sadrzaj.Otisak(foto25): "vodocuvarski/16/2025"} {
		if _, _, k, _ := spA.CitajSKanalom(ctxRaz, otisak); k != kanal {
			t.Errorf("fotografija %.12s u kanalu %q, očekivan %s", otisak, k, kanal)
		}
	}
	SetSpremiste(spB)
	if p, err := PopraviPrilogeListova(ctxRaz, b.db); p != (PopravakPriloga{Veza: 3, Zelja: 2}) || err != nil {
		t.Errorf("ured: %+v %v", p, err)
	}
	zelje, err := spB.Zeljeni(ctxRaz, 10)
	nuzno(t, err)
	if len(zelje) != 2 || zelje[0].Vrsta != "image/jpeg" || zelje[1].Vrsta != "image/jpeg" {
		t.Errorf("popis za dohvat: %+v", zelje)
	}

	for _, n := range []struct {
		c  *cvor
		sp *sadrzaj.Spremiste
	}{{a, spA}, {b, spB}} {
		SetSpremiste(n.sp)
		if p, err := PopraviPrilogeListova(ctxRaz, n.c.db); p.String() != "" || err != nil {
			t.Errorf("%s drugi put: %+v %v", n.c.ime, p, err)
		}
	}
	b.db.Close()
	if _, err := PopraviPrilogeListova(ctxRaz, b.db); err == nil {
		t.Error("zatvorena baza bez greške")
	}
}

// Kad spremište ne radi, fotografije lista se preskaču i ništa se ne broji.
func TestFotografijaListaKadSpremisteNeRadi(t *testing.T) {
	sp, _ := dvaSpremista(t)
	SetSpremiste(sp)
	nuzno(t, sp.Zatvori())
	l := listIve("obilazak")
	l.ID = models.IDListaVodocuvara(ivoIvic, danIve)
	l.Prilozi = []models.PrilogLista{{ID: "foto-1", Vrsta: "image/jpeg", Bajtova: 3, Sadrzaj: sadrzaj.Otisak([]byte("abc"))}}
	if p := prilogeUSpremiste(ctxRaz, l); p != (PopravakPriloga{}) {
		t.Errorf("nečitljivo spremište: %+v", p)
	}
}

// Dnevnik kaže što je upisano, s pravim oblikom riječi; bez upisa ništa.
func TestOpisPopravkaPriloga(t *testing.T) {
	for _, p := range []struct {
		p    PopravakPriloga
		opis string
	}{
		{PopravakPriloga{}, ""},
		{PopravakPriloga{Kanal: 259}, "kanal lista upisan za 259 fotografija"},
		{PopravakPriloga{Kanal: 1, Veza: 3, Zelja: 2}, "kanal lista upisan za 1 fotografiju; veza uz list upisana za 3 fotografije; na popisu za dohvat 2"},
		{PopravakPriloga{Veza: 12}, "veza uz list upisana za 12 fotografija"},
	} {
		if got := p.p.String(); got != p.opis {
			t.Errorf("%+v: %q, očekivano %q", p.p, got, p.opis)
		}
	}
}
