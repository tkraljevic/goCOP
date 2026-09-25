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
		"HydroBASINS", "Tomislav Kraljević", "Mario Kraljević", "Nenadu Šuvaku",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("odjeljak O programu nema %q", want)
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
		"pregledanom i", "sadrzaj.db", "samo kazalo, pregled ili puni sadržaj",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne prati aktualni tok %q", want)
		}
	}
}
