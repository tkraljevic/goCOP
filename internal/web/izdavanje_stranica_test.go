package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/models"
	"gocop/internal/ulaganje"
)

func vrataZaTest() UvozPageData {
	return UvozPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		PodaciDir:   "vodostaji",
	}
}

// Dok posao traje, stranica mora nositi njegov broj — bez toga preglednik ne
// zna koga pitati i traka stoji prazna.
func TestVrataCrtajuTrakuDokPosaoTraje(t *testing.T) {
	d := vrataZaTest()
	d.PosaoID, d.PosaoNaziv = "abc-1", "Izgradnja letve vukovar"
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{`data-posao="abc-1"`, "posao-traka-crta", "posao-dnevnik",
		"Izgradnja letve vukovar"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	// Dok posao traje ne nudi se novo izdavanje: arhiva se upravo mijenja.
	if strings.Contains(html, "/administracija/izdavanje") {
		t.Error("gumb za izdavanje se nudi dok gradnja još traje")
	}
}

// Prijedlog izdanja mora reći i staro i novo izdanje i otisak — inače čovjek
// ne vidi zašto bi broj uopće skočio.
func TestPrijedlogIzdanjaPokazujeSkokIOtisak(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.IzdanjeLetva = "vukovar"
	d.Izdanja = &arhiva.IzvjestajIzdanja{
		Probno: true, Mapa: "pakete", Promijenjenih: 1,
		Redci: []arhiva.RedIzdanja{{Letva: "vukovar", Prije: 2, Izdanje: 3, Novo: true,
			Otisak: "b2ed34cc", Zapisa: 808495, Od: "1900-01-01", Do: "2026-09-11"}},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"v2 → v3", "b2ed34cc", "808.495", "Izdaj vukovar",
		`action="/administracija/izdavanje"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Kad se ništa nije promijenilo, gumb za izdavanje te letve se ne nudi: izdanje
// bez promjene sadržaja je isti paket pod novim brojem.
func TestBezPromjeneNemaGumbaZaIzdavanje(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.IzdanjeLetva = "vukovar"
	d.Izdanja = &arhiva.IzvjestajIzdanja{
		Probno: true, Mapa: "pakete", Istih: 1,
		Redci: []arhiva.RedIzdanja{{Letva: "vukovar", Prije: 2, Izdanje: 2, Otisak: "a7036846"}},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	if !strings.Contains(html, "v2 nepromijenjeno") {
		t.Error("stranica ne kaže da je nepromijenjeno")
	}
	if strings.Contains(html, `action="/administracija/izdavanje"`) {
		t.Error("nudi se izdavanje iako se ništa nije promijenilo")
	}
}

// Čvor bez mape za izdavanje ne smije nuditi izdavanje; on arhivu prima, ne daje.
func TestCvorKojiNeIzdajeNemaOdjeljak(t *testing.T) {
	d := vrataZaTest()
	html := iscrtaj(t, "uvoz_niza.html", d)
	if strings.Contains(html, "Izdanja arhive") {
		t.Error("čvor koji ne izdaje ipak nudi izdavanje")
	}
}

// Bez prijedloga se pokazuje što katalog već ima, da se vidi stanje prije klika.
func TestKatalogSeVidiIBezProvjere(t *testing.T) {
	d := vrataZaTest()
	d.IzdavanjeRadi, d.PaketiDir = true, "pakete"
	d.Katalog = arhiva.Katalog{Inacica: 2, Izdao: "cop-osijek-node",
		Nastalo: time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC),
		Paketi:  []arhiva.UPaketu{{Letva: "vukovar", Izdanje: 2}, {Letva: "batina", Izdanje: 2}}}
	d.KatalogNastalo = "11.09.2026. 11:30"
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Izdanja arhive", "cop-osijek-node", "11.09.2026. 11:30", "pakete"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Datoteka koja pokriva uže razdoblje od zatečenog niza pri zamjeni odnese
// ostatak, jer upis briše stariju datoteku istog niza. Na Dalju je to bilo
// 36.477 satnih vrijednosti iz 1986.–2004. Stranica to mora reći prije klika.
func TestUzaDatotekaUpozoravaStoBiOdnijela(t *testing.T) {
	d := vrataZaTest()
	d.Ime = "satni.csv"
	d.Pogodak = &Pogodak{Razdjelnik: ";", Zaglavlje: []string{"stupac 1", "stupac 2"},
		Uzorak: [][]string{{"1. 1.2005 0", "291"}}}
	d.Prijedlg = UvozNiza{Letva: "dalj", Sliv: "dunav", Izvor: "his2000",
		Velicina: "vodostaj", Vrsta: "satni", Zona: "Europe/Zagreb", Nacin: "dopuni"}
	d.Redaka, d.Od, d.Do = 189146, "01.01.2005.", "31.07.2026."
	d.Zatecen = &ZatecenNiz{Redaka: 211776, Od: "31.12.1985.", Do: "31.12.2024.",
		IzvanNov: 36477, OdIzvan: "31.12.1985.", DoIzvan: "31.12.2004."}

	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Niz već postoji", "211.776", "36.477",
		"31.12.1985. – 31.12.2004.", `value="dopuni"`, `value="zamijeni"`} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	// Dopuna mora biti unaprijed odabrana: ona ne može ništa odnijeti.
	i := strings.Index(html, `value="dopuni"`)
	j := strings.Index(html[i:], ">")
	if !strings.Contains(html[i:i+j], "checked") {
		t.Error("dopuna nije unaprijed odabrana")
	}
}

// Niz kojeg u stablu još nema ne treba izbor: nema se što izgubiti.
func TestNoviNizNemaIzboraNacina(t *testing.T) {
	d := vrataZaTest()
	d.Ime = "satni.csv"
	d.Pogodak = &Pogodak{Razdjelnik: ";", Zaglavlje: []string{"a", "b"}}
	d.Prijedlg = UvozNiza{Letva: "nova", Sliv: "dunav", Izvor: "his2000",
		Velicina: "vodostaj", Vrsta: "satni"}
	html := iscrtaj(t, "uvoz_niza.html", d)
	if strings.Contains(html, "Niz već postoji") {
		t.Error("novi niz nudi izbor dopune i zamjene")
	}
}

// zateceno mora prebrojati baš one vrijednosti koje nova datoteka ne pokriva,
// jer se po tom broju odlučuje hoće li se zamijeniti ili dopuniti.
func TestZatecenoBrojiSamoOnoStoNovaDatotekaNema(t *testing.T) {
	koren := t.TempDir()
	u := UvozNiza{Sliv: "dunav", Letva: "dalj", Izvor: "his2000",
		Velicina: "vodostaj", Vrsta: "satni"}

	// U stablu: 1.1.2004. i 1.1.2005., po jedan sat.
	staro := []arhiva.Redak{
		{Vrijeme: time.Date(2004, 1, 1, 0, 0, 0, 0, models.Zagreb), Vrijednost: 100},
		{Vrijeme: time.Date(2005, 1, 1, 0, 0, 0, 0, models.Zagreb), Vrijednost: 200},
	}
	if _, err := arhiva.Upisi(koren, u.Sliv, u.Letva, u.Izvor, u.Velicina, u.Vrsta, staro); err != nil {
		t.Fatal(err)
	}

	// Nova datoteka počinje tek 2005. — 2004. bi zamjena odnijela.
	novi := []arhiva.Redak{
		{Vrijeme: time.Date(2005, 1, 1, 0, 0, 0, 0, models.Zagreb), Vrijednost: 201},
		{Vrijeme: time.Date(2006, 1, 1, 0, 0, 0, 0, models.Zagreb), Vrijednost: 300},
	}
	z := zateceno(koren, u, novi)
	if z == nil {
		t.Fatal("zatečeni niz nije pronađen")
	}
	if z.Redaka != 2 {
		t.Errorf("zatečeno %d vrijednosti", z.Redaka)
	}
	if z.IzvanNov != 1 {
		t.Errorf("izvan nove datoteke %d, a mora biti 1", z.IzvanNov)
	}
	if z.OdIzvan != "01.01.2004." || z.DoIzvan != "01.01.2004." {
		t.Errorf("razdoblje koje bi se izgubilo: %s – %s", z.OdIzvan, z.DoIzvan)
	}

	// Datoteka koja pokriva sve zatečeno ne prijeti ničim.
	sve := append([]arhiva.Redak{staro[0]}, novi...)
	if z2 := zateceno(koren, u, sve); z2 == nil || z2.IzvanNov != 0 {
		t.Errorf("datoteka koja sve pokriva javlja gubitak: %+v", z2)
	}

	// Niz kojeg u stablu nema uopće nema što javiti.
	if z3 := zateceno(koren, UvozNiza{Sliv: "dunav", Letva: "nova", Izvor: "his2000",
		Velicina: "vodostaj", Vrsta: "satni"}, novi); z3 != nil {
		t.Errorf("nepostojeći niz javio %+v", z3)
	}
}

// Druga vrata: očitanja iz programa. Pregled mora razdvojiti dojavu od onoga
// što je čovjek očitao na letvi — pri maloj vodi je ručno očitanje jedina
// neovisna provjera onoga što telemetrija javlja.
func TestUlaganjeRazdvajaDojavuOdRucnog(t *testing.T) {
	d := vrataZaTest()
	d.UlaganjeRadi = true
	d.UlLetva, d.UlOd, d.UlDo = "batina", "2026-09-01", "2026-09-11"
	d.Ulaganje = &ulaganje.Pregled{
		Postaja: models.Station{Name: "Batina", Code: "batina"},
		Ukupno:  87, Mjereno: 78, Rucno: 9, Sumnjivo: 2, BezVrijednosti: 1, SBiljeskom: 5,
		Izvor: "cop", IzvorRucnog: "cop-rucno", Vrsta: "satni", VrstaRucnog: "satni",
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Očitanja iz programa", "Batina", "cop-rucno",
		"Dojavljeno", "Ručno s letve", "Sumnjivo", "S bilješkom"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	// Sumnjivo se ne ulaže i to mora pisati, inače bi u arhivi izgledalo kao
	// mjerenje.
	if !strings.Contains(html, "ne ulaže se") {
		t.Error("ne piše da se sumnjivo ne ulaže")
	}
}

// Pospremanje briše, pa mora tražiti potvrdu i stajati odvojeno od ulaganja.
func TestPospremanjeTraziPotvrdu(t *testing.T) {
	d := vrataZaTest()
	d.UlaganjeRadi = true
	d.Pospremivo = []ulaganje.ZaPospremanje{
		{StationID: "abc", Letva: "vukovar", Naziv: "Vukovar", Broj: 128, Oznake: "2026-09-10"},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Uloženo, čeka pospremanje", "vukovar", "128",
		"onsubmit=\"return confirm(", "/administracija/ulaganje/pospremi"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Čvor koji ne može ulagati ne smije nuditi ta vrata.
func TestCvorBezStablaNemaUlaganja(t *testing.T) {
	d := vrataZaTest()
	html := iscrtaj(t, "uvoz_niza.html", d)
	if strings.Contains(html, "Očitanja iz programa") {
		t.Error("čvor koji ne ulaže ipak nudi ulaganje")
	}
}

// Niz koji je ostao u arhivi bez datoteke mora se vidjeti, s brojem vrijednosti
// koje su ušle u spojeni niz — po tome se vidi šteti li ili samo leži.
func TestSirotanSeVidiSaSvojimUcinkom(t *testing.T) {
	d := vrataZaTest()
	d.Sirotani = []arhiva.Sirotan{
		{Letva: "batina", Izvor: "cop-rucno", Velicina: "vodostaj", Vrsta: "jutarnji",
			Zapisa: 1, USpoju: 11},
	}
	html := iscrtaj(t, "uvoz_niza.html", d)
	for _, want := range []string{"Nizovi bez datoteke u stablu", "cop-rucno", "jutarnji",
		"u spojenom nizu", "/administracija/uvoz-niza/makni-niz", "return confirm("} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
}

// Dionice se unose u nizu, pa obrazac nudi upis i odmah sljedeću, a šifru
// predlaže unaprijed. Bez toga je svaka od 63 dionice sektora B dva klika i
// jedan izbor područja dalje nego što treba.
func TestObrazacDioniceNudiSljedecu(t *testing.T) {
	d := SectionPageData{
		CurrentUser:     &models.User{FullName: "P"},
		Permissions:     &models.UserPermissions{IsGlobalAdmin: true},
		Section:         models.Section{AreaID: 34, SectorID: "B", Parts: []models.SectionPart{{Seq: 1}}},
		PredlozenaSifra: "B.34.3",
	}
	html := iscrtaj(t, "section_form.html", d)
	for _, want := range []string{`name="dalje"`, "Upiši i sljedeću", `value="B.34.3"`,
		"prvi slobodan broj"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}

	// Pri uređivanju postojeće dionice nema smisla nuditi sljedeću.
	d.IsEdit = true
	d.Section.Code = "B.34.2"
	html = iscrtaj(t, "section_form.html", d)
	if strings.Contains(html, `name="dalje"`) {
		t.Error("uređivanje nudi „i sljedeću“")
	}
}

// Dnevnici se ne miješaju: dežurni zapisnik COP-a i dnevnik usluge održavanja
// nemaju isti sadržaj ni istog voditelja. Razdjelnica ih dijeli karticama, kao
// na Administraciji.
func TestRazdjelnicaDnevnikaDijeliTriVrste(t *testing.T) {
	html := iscrtaj(t, "dnevnici_izbor.html", JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		BrojCOP:     13, BrojA02: 2,
	})
	for _, want := range []string{
		"Dnevnici COP-a", "Dnevnici usluga A.02", "Dnevnici usluga A.03",
		"/dnevnici/popis?vrsta=OBRANA", "/dnevnici/popis?vrsta=ODRZAVANJE_A02", "/dnevnici/popis?vrsta=ODRZAVANJE_A03",
		"operateri", ">13<",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("razdjelnica nema %q", want)
		}
	}
	// Svaka kartica ima ikonu, kao i na Administraciji.
	if n := strings.Count(html, "dash-card-icon-box"); n != 3 {
		t.Errorf("ikona na %d kartica, a ima ih tri", n)
	}
}

// Popis zna koju vrstu pokazuje: naslov, povratak i gumb za novi dnevnik
// moraju je nositi, inače se s razdjelnice padne natrag u pomiješan popis.
// Dnevnik COP-a se otvara svojim obrascem, ne naslovnicom usluge — i samo
// onome tko vodi neki centar.
func TestPopisDnevnikaNosiSvojuVrstu(t *testing.T) {
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Vrsta:       "OBRANA",
		Area:        &models.Area{ID: 34, Name: "Međudržavne rijeke"},
		CanManage:   true,
	}
	html := iscrtaj(t, "dnevnici.html", d)
	for _, want := range []string{"Obrana od poplava", `href="/dnevnici"`, "operateri"} {
		if !strings.Contains(html, want) {
			t.Errorf("popis nema %q", want)
		}
	}
	for _, nesmije := range []string{"kind=OBRANA", "novi-cop"} {
		if strings.Contains(html, nesmije) {
			t.Errorf("popis bez centra za otvaranje nudi %q", nesmije)
		}
	}
	d.CentriZaOtvaranje = []models.Centar{{Sektor: "B", Naziv: "COP Osijek"}}
	d.Centar = "B"
	html = iscrtaj(t, "dnevnici.html", d)
	if !strings.Contains(html, `href="/dnevnici/novi-cop?centar=B"`) {
		t.Error("voditelj centra nema gumb za novi dnevnik COP-a")
	}
}

// Obrazac dnevnika COP-a: centar i početak, bez izvođača i nadzora. Pri
// izmjeni se centar ne mijenja — dnevnik je njegov.
func TestObrazacDnevnikaCOPa(t *testing.T) {
	pocetak := time.Date(2026, 9, 11, 0, 0, 0, 0, models.Zagreb)
	d := JournalPageData{
		CurrentUser:       &models.User{FullName: "P"},
		Permissions:       &models.UserPermissions{IsGlobalAdmin: true},
		CentriZaOtvaranje: []models.Centar{{Sektor: "B", Naziv: "COP Osijek"}, {Sektor: "D", Naziv: "COP Slavonski Brod"}},
		Journal:           &models.Journal{Kind: models.JournalKindDefense, CentarSektor: "B", Year: 2026, StartedAt: &pocetak},
	}
	html := iscrtaj(t, "dnevnik_cop_form.html", d)
	for _, want := range []string{`action="/dnevnici/novi-cop"`, `<option value="B" selected>COP Osijek`, `value="2026-09-11"`, "Otvori dnevnik"} {
		if !strings.Contains(html, want) {
			t.Errorf("obrazac nema %q", want)
		}
	}
	for _, nesmije := range []string{"Izvođač", "Nadzor", "contractor"} {
		if strings.Contains(html, nesmije) {
			t.Errorf("obrazac dnevnika COP-a nudi %q", nesmije)
		}
	}

	d.IsEdit = true
	d.Journal.ID, d.Journal.CentarNaziv = "dn-1", "COP Osijek"
	html = iscrtaj(t, "dnevnik_cop_form.html", d)
	if !strings.Contains(html, `action="/dnevnici/dn-1"`) || !strings.Contains(html, "Spremi zaglavlje") {
		t.Error("izmjena ne ide na dnevnik")
	}
	if strings.Contains(html, `name="centar"`) {
		t.Error("izmjena nudi promjenu centra")
	}
}

// Dnevnik COP-a nema listova ni naloga: dežurstvo teče danima, a zapis nosi
// vrijeme, onoga tko je javio i tekst.
func TestDnevnikCOPPokazujeZapisePoDanima(t *testing.T) {
	kad := time.Date(2009, 6, 29, 7, 15, 0, 0, models.Zagreb)
	dan := time.Date(2009, 6, 29, 0, 0, 0, 0, models.Zagreb)
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal: &models.Journal{ID: "dn-1", Kind: models.JournalKindDefense,
			CentarSektor: "B", Title: "Dnevnik COP-a, lipanj 2009.", Year: 2009,
			Reconstruction: true, Notes: "Prijepis iz digitaliziranog uveza."},
		Dani: []DanZapisa{{Dan: dan, Zapisi: []models.JournalEntry{
			{Kind: models.EntryKindDuty, Text: "Dežurstvo 07:00 – 15:00",
				HappenedAt: &dan, ReportedBy: "Ana Anić"},
			{Kind: models.EntryKindNote, Text: "vodostaj Batina u 07:00 +551",
				HappenedAt: &kad, ReportedBy: "Marko Marić"},
		}}},
	}
	html := iscrtaj(t, "dnevnik_cop.html", d)
	for _, want := range []string{
		"Ponedjeljak 29.6.2009.", "07:15", "Marko Marić", "vodostaj Batina",
		"Ana Anić", "zapis-smjena", "Prijepis iz uveza",
		"ispraviti na mjestu",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica nema %q", want)
		}
	}
	// Građevinskog ovdje nema: ni listova, ni izvođača, ni naloga.
	for _, nesmije := range []string{"Novi list", "Izvođač", "Otvoreni nalozi"} {
		if strings.Contains(html, nesmije) {
			t.Errorf("zapisnik dežurstva nudi %q", nesmije)
		}
	}
}

// Živi dnevnik ne nudi ispravak na mjestu: ondje je zapis sam dokument.
func TestZiviDnevnikNeNudiPrepravak(t *testing.T) {
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal: &models.Journal{ID: "dn-2", Kind: models.JournalKindDefense,
			CentarSektor: "B", Title: "Dnevnik COP-a"},
	}
	html := iscrtaj(t, "dnevnik_cop.html", d)
	if strings.Contains(html, "ispraviti na mjestu") {
		t.Error("živi dnevnik nudi prepravak zapisa")
	}
	if !strings.Contains(html, "još nema zapisa") {
		t.Error("prazan dnevnik to ne kaže")
	}
}

// Dnevnici COP-a ne idu preko branjenog područja: vezani su na centar i
// područje im je prazno, pa ih popis po području nikad nije našao — stranica
// je izgledala prazno iako je u bazi trinaest dnevnika.
func TestPopisCOPNeTraziPodrucje(t *testing.T) {
	d := JournalPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Vrsta:       models.JournalKindDefense,
		Areas:       []models.Area{{ID: 15}, {ID: 34}},
		Journals: []models.Journal{{
			ID: "dn-1", Kind: models.JournalKindDefense, CentarSektor: "B",
			Title: "Dnevnik COP-a, lipanj 2009.", Year: 2009,
			Reconstruction: true, SheetCount: 3, LastSheetOn: "2009-06-30",
		}},
	}
	d.Centri = []models.Centar{{Sektor: "B", Naziv: "COP Osijek", Dnevnika: 13}}
	d.Journals[0].CentarNaziv = "COP Osijek"

	html := iscrtaj(t, "dnevnici.html", d)
	for _, want := range []string{"Dnevnik COP-a, lipanj 2009.", "dana dežurstva", "prijepis iz uveza",
		"COP Osijek", `id="centar"`} {
		if !strings.Contains(html, want) {
			t.Errorf("popis nema %q", want)
		}
	}
	// Bira se po centru, ne po području: dnevnik COP-a nije ničijeg područja.
	if strings.Contains(html, `id="area"`) {
		t.Error("popis dnevnika COP-a nudi birač područja")
	}
	// Centar se zove svojim imenom; "sektor B" traži da netko zna koji je to.
	if strings.Contains(html, "sektor B") {
		t.Error("centar se predstavlja oznakom sektora umjesto imenom")
	}
	// Ni pojam lista, jer dežurstvo teče danima.
	if strings.Contains(html, "listova") {
		t.Error("dnevnik COP-a broji listove")
	}
}
