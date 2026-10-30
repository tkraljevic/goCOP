package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func pomocHTML(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "pomoc.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Znak „?" u zaglavlju vodi na /pomoc#<modul>, gdje je modul vrijednost
// ActiveNav te stranice. Nestane li sidro, poveznica ne puca nego tiho
// otvori vrh pomoći — korisnik traži svoj odjeljak i ne nalazi ga, a nijedan
// drugi test to ne primijeti.
func TestSvakiModulImaSvojOdjeljakUPomoci(t *testing.T) {
	h := pomocHTML(t)
	sidra := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		sidra[m[1]] = true
	}
	// vrijednosti ActiveNav koje stranice postavljaju
	for _, modul := range []string{
		"pocetak", "dashboard", "teren", "sections", "stations", "readings", "arhiva",
		"prognoze", "journals", "users", "registers", "slivovi", "organizacija", "territories", "structures",
		"watercourses", "firme", "maintenance", "admin", "moduli", "settings", "sync",
		"sredstva", "posta", "profile", "pojmovi", "o-programu", "pomoc",
	} {
		if !sidra[modul] {
			t.Errorf("pomoć nema odjeljak #%s — znak „?“ s te stranice vodi na vrh", modul)
		}
	}
}

// Završna stranica pomoći ujedno je čitljiva obavijest o programu. Licenca
// koda ne smije se pomiješati s licencama ovisnosti i vanjskih podataka.
func TestPomocImaOProgramuLicenceIZahvale(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		`id="o-programu"`, "Što je goCOP", "EUPL‑1.2", "LICENSE_hr.txt",
		"KaTeX", "Leaflet", "Lucide", "goldmark", "modernc.org/sqlite",
		"OpenStreetMap", "Open‑Meteo", "CC BY‑SA 4.0", "Zahvale",
		"HydroBASINS", "Tomislav Kraljević", "Mario Kraljević", "Nenad Šuvak",
		"Dél-dunántúli Vízügyi Igazgatóság", "Republički hidrometeorološki zavod Srbije",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("odjeljak O programu nema %q", want)
		}
	}
}

func TestPomocOpisujeStalniCvorITunelskuRazmjenu(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"Stalni čvor i terenska računala", "HTTPS/WebSocket tunel",
		"Razmjena s čvorovima", "javlja se sam", "30 sekundi",
		"zadnjih 48 sati", "broj zapisa", "docs/INSTALACIJA.md",
		"sigurnosno učvršćivanje nije", "<code>x/net</code>",
		"izdanje programa", "stariji program još ne razumije", "Računalo s izdanjem 0.0.38 ili starijim taj broj ne javlja",
		"od danas, piše i kad ga je javilo", "promijenio ga je noviji, vidi se odmah, ali se ne", "Od godine",
		"malo mjesta: manje od 10 GB ili", "manje od 10 % diska", "Ako ne znate kako, javite administratoru.",
		"stiže i e-poštom, jednom na dan", "spremile lozinku sandučića",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje %q", want)
		}
	}
}

func TestPomocOpisujeSigurnost025(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"tuđih stranica (CSRF)", "sigurni sesijski",
		"prijavljuje se ni iz ureda",
		"i potvrđuje globalni administrator", "Svjež čvor bez računa",
		"ostale prijave na istom čvoru", "može tražiti PIN",
		"Potpisane ovlasti izdavatelja", "ostaju razvojni zadaci",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje sigurnosno pravilo %q", want)
		}
	}
}

// Djelatnik izvan službene domene dobiva PIN samo na adresu koju je
// potvrdio globalni administrator; pomoć mora reći tko potvrđuje i kada
// potvrda prestaje, inače se ljudi pitaju zašto PIN ne stiže.
func TestPomocOpisujePotvrduAdreseZaPIN(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		`id="users-potvrda-adrese"`, `href="#users-potvrda-adrese"`, "Adresa je provjerena",
		"Vlastitu adresu nitko ne", "potvrdu time briše", "PIN ne dobiva ni kad je",
		"Ni tuđim očima potvrda se ne daje",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje potvrdu adrese: %q", want)
		}
	}
}

func TestPomocObjasnjavaKontroleKarte(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"puni zaslon", "vaš položaj", "Kotačić miša", "dva prsta", "četiri strelice", "ne sprema se", "Moj položaj", "HTTPS veze",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne objašnjava kontrolu karte %q", want)
		}
	}
}

// Sidro upisano dvaput vodi na prvo pojavljivanje, pa poveznica završi na
// krivom mjestu bez ijedne naznake.
func TestSidraUPomociSuJedinstvena(t *testing.T) {
	h := pomocHTML(t)
	broj := map[string]int{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		broj[m[1]]++
	}
	var dupli []string
	for id, n := range broj {
		if n > 1 {
			dupli = append(dupli, id)
		}
	}
	sort.Strings(dupli)
	if len(dupli) > 0 {
		t.Errorf("udvostručena sidra u pomoći: %v", dupli)
	}
}

// Kazalo vodi na odjeljke; poveznica bez cilja je mrtva i ne javlja se.
func TestKazaloPomociVodiNaPostojecaSidra(t *testing.T) {
	h := pomocHTML(t)
	sidra := map[string]bool{}
	for _, m := range regexp.MustCompile(`id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		sidra[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`href="#([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		if !sidra[m[1]] {
			t.Errorf("kazalo pomoći vodi na #%s, a tog odjeljka nema", m[1])
		}
	}
	// kazalo mora pokrivati sve odjeljke prve razine
	for _, m := range regexp.MustCompile(`<section class="detail-section" id="([a-z0-9-]+)"`).FindAllStringSubmatch(h, -1) {
		if !strings.Contains(h, `href="#`+m[1]+`"`) {
			t.Errorf("odjeljak #%s nije u kazalu", m[1])
		}
	}
}

// Postupci koje korisnik izvodi rukom moraju biti opisani, jer ih nema u
// sučelju: arhiva se gradi izvan programa, a očitanja se sele alatom.
func TestPomocOpisujePostupkeSArhivom(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"arhiva-vodostaja", "selidba-arhive", "vodostaji/",
		"ne briše dok arhiva ne dokaže da ga pokriva",
		"arhiva ostaje netaknuta",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje %q", want)
		}
	}
}

// Datoteka krivulja koja se ne da pročitati zaustavi gradnju. Pomoć mora reći
// koja je to datoteka i da je najčešći uzrok zarez umjesto točke-zareza, jer
// čovjek inače ne zna što u datoteci traži.
func TestPomocOpisujeDatotekuKrivulja(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, want := range []string{
		"Prvi redak datoteke mora biti zaglavlje s imenima stupaca",
		"<code>vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena</code>",
		"stupac od_cm ili do_cm nema cijelog broja, u nekom od stupaca p1 do p4 piše nešto što nije broj",
		"Najčešće je to zarez upisan umjesto točke-zareza.",
		"Prazan redak gradnja preskače", "datoteka sa samim zaglavljem znači da letva krivulja nema",
		"Prazno polje u stupcu p1 do p4 gradnja čita kao nulu.",
		"Uvoz izvoza HIS-2000 prazan redak isto preskoči, a datoteku kojoj ne valja zaglavlje ili neki redak ne spaja s izvozom",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć o datoteci krivulja ne kaže %q", want)
		}
	}
}

// Dnevnici su prerasli jedan građevinski obrazac: pomoć mora objasniti cijeli
// put od zapisnika centra preko plana do obračuna, jer se pogrešan unos sati
// inače otkrije tek u računovodstvu.
func TestPomocOpisujeDnevnikCOPDezurstvaIObracun(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		`id="dnevnik-cop"`, `id="dnevnik-dezurstva"`, `id="dnevnik-obracun"`,
		"tko je javio", "čeka potvrdu uprave centra", "Nedjelja se obračunava kao",
		"Moj profil", `BP_&lt;broj&gt;`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje %q", want)
		}
	}
}

// Duga pomoć bez tražilice opet postaje priručnik koji se čita od početka.
// Česti zadaci moraju voditi na postojeća sidra, a skripta imati polazište u
// predlošku umjesto da se tiho ne pokrene.
func TestPomocImaPretraguICesteZadatke(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		`id="pomoc-pretraga"`, `id="pomoc-pretraga-stanje"`,
		"Upisati vodostaj", "Voditi dnevnik COP-a", "Upisati dežurstvo",
		"Izvesti obračun", "Uvesti hidrološki niz", "Povezati računalo",
		`class="pomoc-na-vrh"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć nema tražilicu ili česti zadatak %q", want)
		}
	}
}

// Na kraju dugog poglavlja korisnik ne smije ostati u slijepoj ulici. Svako
// glavno poglavlje vodi na susjedne teme i natrag na kazalo.
func TestSvakoPoglavljePomociImaNavigaciju(t *testing.T) {
	h := pomocHTML(t)
	poglavlja := strings.Count(h, `<section class="detail-section" id="`)
	navigacije := strings.Count(h, `{{template "pomocPoglavljeNav"`)
	if navigacije != poglavlja {
		t.Errorf("%d poglavlja, a %d završnih navigacija", poglavlja, navigacije)
	}
	for _, want := range []string{`id="pomoc-kazalo"`, `href="#pomoc-kazalo"`, "← {{$prethodni}}", "{{$sljedeci}} →"} {
		if !strings.Contains(h, want) {
			t.Errorf("navigacija poglavlja nema %q", want)
		}
	}
}

// Pomoć je korisnički priručnik za cijeli proizvodni tok, dok README ostaje
// sažet vodič administratoru. Nestane li jedan od novih tokova iz pomoći,
// korisniku ostane funkcija bez objašnjenja ovlasti, zaključavanja i izvornika.
func TestPomocPratiOperativneTokove(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		`id="tok-obrane"`, "preventivnoj obrani", "aktivna obrana",
		`id="izvjesca"`, `id="dogadjanja"`, `id="vodocuvar"`,
		`id="prijave"`, "PDF izvornik s ugrađenim", `id="akti"`,
		`id="sredstva"`, `id="posta"`, `id="potpisi"`,
		"Simulirani potpis", `id="admin-test"`, "Repozitorij službenih zapisa",
		"pregledanom i", "sadrzaj.db", "samo kazalo, samo pregledi (slike,",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne prati aktualni tok %q", want)
		}
	}
}

// Pomoć o dnevnom listu za budući dan (Odluke (8. 10. 2026.), pitanja 5 i
// 6) ne obećava više od programa: redni broj nacrt dobiva prvim spremanjem
// ili predajom kad njegov dan počne. Pomoć kaže i kako takav nacrt nastaje,
// jer vodočuvar budući dan ne otvara sam. Do provedbe pitanja 6 pomoć nije
// smjela reći da brojevi idu redom dana; sad to program jamči za nove
// listove (nacrt ranijeg dana dobiva broj prije lista kasnijeg dana), a
// postojeći brojevi se ne mijenjaju, pa pomoć to i kaže.
func TestPomocOBuducemListuNeObecavaViseOdPrograma(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, ne := range []string{"vodočuvar u njega može upisati plan"} {
		if strings.Contains(h, ne) {
			t.Errorf("pomoć obećava %q", ne)
		}
	}
	for _, da := range []string{"rukovoditeljev upis („Upiši”)", "prvim spremanjem ili predajom kad njegov dan počne",
		"pa brojevi novih listova idu redom dana", "Brojevi koje listovi već imaju ne mijenjaju se"} {
		if !strings.Contains(h, da) {
			t.Errorf("pomoć ne kaže %q", da)
		}
	}
}

// Pomoć o listu prenesenom iz ranije evidencije i danu koji je prošao
// (Odluke (8. 10. 2026.), pitanje 6): prenesen list nikad ne dobiva redni
// broj, a akt i prijava za njegov dan idu na današnji list, kao za dan bez
// lista; rukovoditeljeva bilješka za taj dan ide na prenesen list. Poruka
// vodočuvaru koji otvori prošli dan navodi se kako je program javlja i ne
// upućuje ga da list priprema unaprijed.
func TestPomocOPrenesenomListuIProslomDanu(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	if strings.Contains(h, "ili unaprijed kao nacrt") {
		t.Error("pomoć upućuje vodočuvara da list priprema unaprijed")
	}
	for _, da := range []string{
		"„za 09.10.2026. nema dnevnog lista, a list za dan koji je prošao više se ne otvara: dnevni list otvara se na svoj dan”",
		"nema lista ili mu je list tog dana prenesen iz ranije evidencije, na današnji list",
		"Listovi preneseni iz ranije evidencije ostaju kakvi jesu i nikad ne dobivaju redni broj",
		"rukovoditeljeva bilješka za taj dan upisuje se na prenesen list",
		"lista nema ili je list tog dana prenesen iz ranije evidencije, upisuje se na današnji list",
	} {
		if !strings.Contains(h, da) {
			t.Errorf("pomoć ne kaže %q", da)
		}
	}
}

// Pomoć kaže da list prenesen iz ranije evidencije pri razmjeni vrijedi kao
// predan: nacrt s drugog čvora ga ne zamjenjuje, a preneseni obilasci
// ostaju (Odluke (8. 10. 2026.), pitanje 7).
func TestPomocOPrenesenomListuSDrugogCvora(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	if !strings.Contains(h, "list prenesen iz ranije evidencije: nacrt s drugog čvora ga ne zamjenjuje") {
		t.Error("pomoć ne kaže da nacrt s drugog čvora ne zamjenjuje prenesen list")
	}
}

// Nacrt akta sastavlja i ispravlja samo Centar obrane od poplava, ali to
// provjerava tek računalo s novim izdanjem. Pomoć mora reći da računalo sa
// starijom verzijom (0.0.34) i dalje pušta nacrt i izmjenu izvan centra, da
// oni stižu razmjenom i zamjenjuju neovjereni nacrt, i da uz nacrt ne piše
// tko ga je mijenjao (Odluke (8. 10. 2026.), pitanje 18).
func TestPomocONacrtuAktaSaStarijegCvora(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, da := range []string{
		"Računalo sa starijom verzijom (0.0.34) i dalje pušta da nacrt sastavi ili mijenja i netko izvan centra",
		"stiže razmjenom i zamjenjuje neovjereni nacrt, a tko ga je mijenjao, uz nacrt ne piše",
	} {
		if !strings.Contains(h, da) {
			t.Errorf("pomoć ne kaže %q", da)
		}
	}
}

// Odluke (8. 10. 2026.), pitanje 10: mjesto rada program izvodi samo iz
// opisa s popisa. Pomoć ne smije reći da ga izvodi iz opisa kojega na popisu
// nema — upravo tada se mjesto ne zna i dežurstvo je za provjeru.
func TestPomocMjestoRadaIzvodiSamoIzOpisaSPopisa(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"Mjesto rada slijedi iz opisa s popisa",
		"Kad opisa nema na popisu",
		"<strong>„mjesto nepoznato — za provjeru”</strong>",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje mjesto rada iz opisa: nema %q", want)
		}
	}
	if strings.Contains(h, "s opisom kojega nema na popisu — program mjesto najprije izvede iz opisa") {
		t.Error("pomoć kaže da program mjesto izvodi i iz opisa kojega nema na popisu")
	}
}

// Odluke (8. 10. 2026.), pitanje 10: bilješku o ispravku opisa dežurstva
// upisuje samo program; pomoć to mora reći, jer se rukom upisana odbija.
func TestPomocBiljeskuOIspravkuOpisaUpisujeProgram(t *testing.T) {
	if !strings.Contains(pomocHTML(t), "Tu bilješku u napomenu upisuje samo program") {
		t.Error("pomoć ne kaže da bilješku o ispravku opisa upisuje samo program")
	}
}

// Odluke (8. 10. 2026.), pitanje 12: predano izvješće sektora spremljeno bez
// izmjene teksta i odabira ostaje predano s tablicama kakve su predane, i
// kad je zapis koji je ušao u nj storniran; pomoć mora reći kako se
// preuzimaju nove brojke.
func TestPomocPredanoSektorskoINoveBrojke(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{"zapis koji je ušao u izvješće storniran", "Nove brojke preuzima izmjena izvješća sektora ili spremanje"} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje %q", want)
		}
	}
}
