package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pomoć mora reći tko zadužuje račun bez dužnosti, da se piše po dosegu
// dužnosti (i u dnevnike svog područja), da lozinku
// e-pošte nakon promjene ili poništenja lozinke treba upisati ponovno i kad
// se briše, da se zauzeta adresa ne upisuje, da se ključ uklanja uz lozinku
// i da certifikat nosi korisničko ime, bez zagrada iz punog imena.
func TestPomocOpisujeOvlastiPoDosegu(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"Račun bez aktivne dužnosti", "zadužuje samo globalni administrator", "kao ispomoć, pod nazivom uloge",
		"Pravo pisanja ide po dosegu dužnosti", "ne daje pisanje po sektoru",
		"prestaje vrijediti i briše se", "pri prvoj sljedećoj uporabi ili pokretanju čvora", "brišu na ovom čvoru i spremljenu",
		// „Akt vodomjera piše tko piše na bilo kojoj njegovoj dionici” više
		// ne vrijedi: nacrt akta sastavlja samo Centar obrane od poplava
		// (Odluke (8. 10. 2026.), pitanje 18; TestDosegDuznostiNeDajeAkt)
		"u dnevnicima svog područja i COP-a", "upisuje samo tko",
		"Terenska uloga traži dionice ili", "pri izmjeni naziv i", "zagrade iz punog imena se",
		"Adresu e-pošte koju već ima drugi aktivni račun program ne upisuje nikome", "ni preimenovanjem",
		"uklanjanju ključa (tuđim očima ni jedno ni drugo)", "„Ime Prezime (korisnicko)”",
		"pri prvoj prijavi mora zamijeniti svojom",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje pravilo %q", want)
		}
	}
}

// Pomoć mora reći tko što smije u knjizi vodočuvara i s prijavama s terena
// (Odluke (8. 10. 2026.), pitanje 1), i kad je prijava vezana uz dionicu
// rukovoditelja dionice.
func TestPomocOpisujeKnjiguVodocuvara(t *testing.T) {
	h := strings.Join(strings.Fields(pomocHTML(t)), " ")
	for _, want := range []string{
		"Dežurni operater upisuje bilješku, ali ne parafira i ne zadaje zadatke",
		"Rukovoditelj dionice kojemu na dužnosti nije upisana nijedna dionica vodi sve dionice svog područja",
		"rukovoditelj dionice samo prijavu vezanu uz svoju dionicu: onu u kojoj je navedena njegova dionica ili objekt koji je u registru na njegovoj dionici",
		"onu u kojoj nije navedena ni dionica ni objekt s dionicom, kad mu je vezan i vodočuvarov list za taj dan",
		"i onu vodočuvara kojemu je na dan prijave njegova dionica bila upisana na zaduženju, osim kad prijava navodi drugu dionicu s tog zaduženja",
		"koji je na dan lista čuvao njegovu dionicu ili cijelo područje (i kad je to zaduženje poslije isteklo, opozvano ili izmijenjeno; list s dana prije zaduženja nije vezan)",
		"Dnevni list i njegov potpisani PDF nose oznaku, vrstu i naslov svake prijave objavljene za taj dan",
		"tko čita list vidi ih, ali samu prijavu otvara samo tko je smije čitati",
		"Voditelj usluga / poslovođa izvođača vidi samo dnevnike svoje firme na branjenom području na kojem ima dužnost",
		"i one u kojima je već pisao (otvorio dnevnik ili list, ili upisao zapis)",
		"i mora biti isti: dio naziva nije ista firma",
		"Dnevnike drugih firmi ne vidi, ni na svom ni na drugim područjima, a ni dnevnik bez izvođača na naslovnici u kojem nije pisao",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje pravilo %q", want)
		}
	}
}

// Kartica Vodočuvarskog dnevnika pod Dokumentacijom ne smije reći da
// parafiraju svi ostali rukovoditelji: rukovoditelj dionice parafira samo
// list koji se tiče njegove dionice, a parafira i uprava (Odluke (8. 10.
// 2026.), pitanje 1).
func TestKarticaKnjigeVodocuvaraKazeTkoParafira(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "dnevnici_izbor.html"))
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Join(strings.Fields(string(b)), " ")
	if strings.Contains(h, "ostali rukovoditelji i ovlaštenici parafiraju") {
		t.Error("kartica kaže da parafiraju svi ostali rukovoditelji")
	}
	want := "parafiraju ovlaštenici za praćenje ugovora i uprava, a rukovoditelj dionice list koji se tiče njegove dionice"
	if !strings.Contains(h, want) {
		t.Errorf("kartica ne kaže %q", want)
	}
}

// Doseg dužnosti ne daje pravo na akt o stupnju obrane: nacrt sastavlja samo
// Centar obrane od poplava sektora (voditelj, zamjenik, dežurni operater), pa
// ni Pomoć (Korisnici) ni upute za instalaciju, koje se čitaju prije
// odobrenja, ne smiju reći da vodočuvar ili rukovoditelj dionice piše akte
// svojih dionica, ni da akt vodomjera piše tko piše na njegovoj dionici
// (Odluke (8. 10. 2026.), pitanje 18).
func TestDosegDuznostiNeDajeAkt(t *testing.T) {
	inst, err := os.ReadFile(filepath.Join("..", "..", "docs", "INSTALACIJA.md"))
	if err != nil {
		t.Fatal(err)
	}
	for ime, tekst := range map[string]string{"pomoć": pomocHTML(t), "INSTALACIJA.md": string(inst)} {
		h := strings.Join(strings.Fields(tekst), " ")
		for _, ne := range []string{
			"njihovim objektima i aktima", "objekte, akte ni dnevnike",
			"Akt vodomjera piše tko piše",
		} {
			if strings.Contains(h, ne) {
				t.Errorf("%s i dalje kaže %q", ime, ne)
			}
		}
		if !strings.Contains(h, "Doseg dužnosti ne daje pravo na akt o stupnju obrane: nacrt rješenja ili obavijesti sastavlja samo Centar obrane od poplava sektora") {
			t.Errorf("%s ne kaže da doseg dužnosti ne daje pravo na akt", ime)
		}
	}
}
