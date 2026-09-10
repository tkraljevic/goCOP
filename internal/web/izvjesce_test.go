package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/png"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"

	"github.com/google/uuid"
)

func probnoIzvjesce(t *testing.T) IzvjesceLetve {
	t.Helper()
	cm := func(v int) *int { return &v }
	st := batinaSKotama()
	st.ID = uuid.New()
	st.Code = "batina"
	st.Watercourse = "Dunav"
	st.WatercourseSource = models.WatercourseFromOperator
	st.Stationing = "rkm 1.425+000"
	st.ZeroDatumSource = "Geodetski elaborat DHMZ-a"
	st.Extremes = []models.StationExtreme{
		{Kind: models.ExtremeMax, LevelCm: cm(775), OnDate: "2013-06-14",
			Quality: models.QualityMeasured, Source: "DHMZ"},
		{Kind: models.ExtremeMax, LevelCm: cm(795), OnDate: "1965-06-24",
			Quality: models.QualityReconstructed, Source: "postaja Bezdan",
			Method: "preračun iz vodostaja Bezdana", Note: "Potvrda & provjera <iz Mohácsa>"},
	}
	st.ReturnLevels = []models.StationReturnLevel{
		{Years: 25, LevelCm: cm(757), LowCm: cm(739), HighCm: cm(773),
			Method: "POT, generalizirana Pareto", Series: "1902.–2026.",
			Source: "COP Osijek", ComputedOn: "2026-09-08", Note: "Gornji rep nema trend."},
		{Years: 100, LevelCm: cm(792), LowCm: cm(765), HighCm: cm(813),
			Method: "POT, generalizirana Pareto", Series: "1902.–2026."},
	}
	st.Prep = models.Threshold{Cm: cm(300)}
	st.Regular = models.Threshold{Cm: cm(500)}
	st.Emergency = models.Threshold{Cm: cm(650)}
	st.State = models.Threshold{Cm: cm(800)}

	p := probniProfil()
	pragovi := st.PragoviObrane()
	poc := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	var niz []models.HidroTocka
	dodaj := func(sati int, v float64) {
		for i := 0; i < sati; i++ {
			niz = append(niz, models.HidroTocka{Kad: poc.Add(time.Duration(len(niz)) * time.Hour), Vrijednost: v})
		}
	}
	dodaj(1, 100)
	dodaj(48, 400)
	dodaj(96, 700)
	dodaj(1, 100)
	valovi := models.Valovi(niz, pragovi)

	return IzvjesceLetve{
		Station: st, Sastavio: "Ivan Horvat",
		Kad:         time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC),
		PragoviKote: pragoviUKotama(st),
		Profil:      &p,
		Crtez:       crtajKoritoP(p, 300, sirokoKoritoM.uSustavu(st)),
		Valovi:      valovi,
		ValoviZbroj: models.ZbrojValova(valovi, pragovi),
		ValoviNiz:   models.RazdobljeNiza{Od: niz[0].Kad, Do: niz[len(niz)-1].Kad, Zapisa: len(niz)},
	}
}

func dokumentXML(t *testing.T, iz IzvjesceLetve) string {
	t.Helper()
	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		t.Fatalf("sastavljanje: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatalf("paket nije ZIP: %v", err)
	}
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		r, _ := f.Open()
		defer r.Close()
		s, _ := io.ReadAll(r)
		// mora biti ispravan XML, inače Word javlja oštećenu datoteku
		dek := xml.NewDecoder(bytes.NewReader(s))
		for {
			_, err := dek.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("document.xml nije ispravan XML: %v", err)
			}
		}
		return string(s)
	}
	t.Fatal("u paketu nema document.xml")
	return ""
}

// Izvješće preslikava stranicu s koje je sastavljeno i ne nosi ništa tuđe.
// Dokument putuje dalje od programa; onaj tko ga otvori mora znati što je u
// njemu, a ne nagađati je li nešto izostalo ili je toga jednostavno nema.
func TestIzvjesceKarticePreslikavaKarticu(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceKartica
	doc := dokumentXML(t, iz)
	for _, want := range []string{
		"Vodomjerna postaja Batina", "Dunav", "rkm 1.425+000",
		"Osnovni podaci", "Pragovi obrane od poplava", "Pripremno stanje", "Izvanredno stanje",
		"Kota nule vodomjera", "80,450", "80,189", "0,261",
		"Zabilježeni ekstremi", "+775 cm", "+795 cm", "rekonstruirano",
		"Ivan Horvat", "O ovom izvješću",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("u izvješću kartice nema %q", want)
		}
	}
	for _, ne := range []string{
		"Povratni vodostaji", "Valovi obrane u nizu", "Promjene kote nule",
		"Niz mjerenja", "Krivulje protoka",
	} {
		if strings.Contains(doc, ne) {
			t.Errorf("izvješće kartice nosi %q, a to je na historijatu", ne)
		}
	}
}

func TestIzvjesceHistorijataPreslikavaHistorijat(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceHistorijat
	doc := dokumentXML(t, iz)
	for _, want := range []string{
		"Vodomjerna postaja Batina", "historijat",
		"Povratni vodostaji", "+757 cm", "+792 cm", "25 godina", "100 godina",
		"1 % svake godine", "POT, generalizirana Pareto",
		"Valovi obrane u nizu", "Zbroj za cijeli niz", "Redovna obrana",
		"O ovom izvješću",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("u izvješću historijata nema %q", want)
		}
	}
	for _, ne := range []string{
		"Pragovi obrane od poplava", "Kota nule vodomjera", "Zabilježeni ekstremi",
	} {
		if strings.Contains(doc, ne) {
			t.Errorf("izvješće historijata nosi %q, a to je na kartici", ne)
		}
	}
}

// Izvješće je procjena i izračun, ne mjerenje. Ograde koje na stranici stoje
// uz brojke moraju ići i u dokument — dokument putuje dalje od stranice i
// čita ga netko tko izračun nije vidio.
func TestIzvjesceNosiOgradeUzBrojke(t *testing.T) {
	kartica := probnoIzvjesce(t)
	kartica.Dio = izvjesceKartica
	povijest := probnoIzvjesce(t)
	povijest.Dio = izvjesceHistorijat
	doc := dokumentXML(t, kartica) + dokumentXML(t, povijest)
	for _, want := range []string{
		"ne iz proglašenih obrana",
		"Procjena iz niza, ne mjerenje i ne propis",
		"ne ulazi u pragove",
		"stanja se ne preklapaju",
		"Grafovi nisu ugrađeni",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("u izvješću nema ograde %q", want)
		}
	}
}

// Tekst iz baze zna imati znakove koji u XML-u nešto znače. Neizbjegnut
// ampersand razbija datoteku, a korisnik dobije samo „Word ne može otvoriti".
func TestIzvjescePodnosiPosebneZnakove(t *testing.T) {
	doc := dokumentXML(t, probnoIzvjesce(t))
	if !strings.Contains(doc, "Potvrda &amp; provjera &lt;iz Mohácsa&gt;") {
		t.Error("posebni znakovi iz napomene nisu izbjegnuti")
	}
}

// Ime datoteke ide kroz e-poštu i dijeljene mape, gdje dijakritika i razmaci
// znaju doći pokvareni.
func TestImeIzvjescaJeSigurnoZaPrijenos(t *testing.T) {
	kad := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		st   models.Station
		want string
	}{
		{models.Station{Code: "batina"}, "izvjesce-batina-2026-09-09.docx"},
		{models.Station{Name: "Gospić Prag (HEP)"}, "izvjesce-gospic-prag-hep-2026-09-09.docx"},
		{models.Station{Name: "Đakovo Žuta"}, "izvjesce-dakovo-zuta-2026-09-09.docx"},
		{models.Station{}, "izvjesce-postaja-2026-09-09.docx"},
	} {
		if got := imeIzvjesca(s.st, kad, izvjesceKartica); got != s.want {
			t.Errorf("%q → %q, očekivano %q", s.st.Name+s.st.Code, got, s.want)
		}
	}
	// Kartica i historijat daju dva dokumenta; ista imena značila bi da drugo
	// prepiše prvo u mapi preuzimanja.
	kartica := imeIzvjesca(models.Station{Code: "batina"}, kad, izvjesceKartica)
	povijest := imeIzvjesca(models.Station{Code: "batina"}, kad, izvjesceHistorijat)
	if kartica == povijest {
		t.Errorf("oba izvješća zovu se %q", kartica)
	}
	if !strings.Contains(povijest, "historijat") {
		t.Errorf("izvješće historijata se ne raspoznaje po imenu: %q", povijest)
	}

	sigurno := regexp.MustCompile(`^[a-z0-9.-]+$`)
	if !sigurno.MatchString(imeIzvjesca(models.Station{Name: "Čađavica–Sjever"}, kad, izvjesceKartica)) {
		t.Errorf("ime nije sigurno: %q", imeIzvjesca(models.Station{Name: "Čađavica–Sjever"}, kad, izvjesceKartica))
	}
}

// Letva bez ijednog izračuna daje kraće izvješće, ali ne prazne okvire i ne
// izmišljene brojke.
func TestIzvjesceZaPraznuLetvuNeIzmisljaPodatke(t *testing.T) {
	iz := IzvjesceLetve{Station: models.Station{ID: uuid.New(), Name: "Dalj", Code: "dalj"},
		Sastavio: "P", Kad: time.Now()}
	doc := dokumentXML(t, iz)
	for _, ne := range []string{
		"Povratni vodostaji", "Zabilježeni ekstremi", "Valovi obrane",
		"Kota nule vodomjera", "Krivulje protoka", "<w:tbl>",
	} {
		if strings.Contains(doc, ne) {
			t.Errorf("izvješće prazne letve sadrži %q", ne)
		}
	}
	if !strings.Contains(doc, "Vodomjerna postaja Dalj") || !strings.Contains(doc, "O ovom izvješću") {
		t.Error("izvješće mora imati barem naslov i podrijetlo")
	}
}

func paketIzvjesca(t *testing.T, iz IzvjesceLetve) map[string]string {
	t.Helper()
	var b bytes.Buffer
	if err := iz.Sastavi().Zapisi(&b); err != nil {
		t.Fatalf("sastavljanje: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatalf("paket nije ZIP: %v", err)
	}
	dijelovi := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatalf("dio %s: %v", f.Name, err)
		}
		s, _ := io.ReadAll(r)
		r.Close()
		dijelovi[f.Name] = string(s)
	}
	return dijelovi
}

// Karta ide u izvješće kartice, jer i na kartici stoji.
func TestIzvjesceKarticeNosiKartu(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceKartica
	iz.Station.Latitude, iz.Station.Longitude = stupanj(45.845833), stupanj(18.854722)
	png := probnaKartaPNG(t)
	iz.Karta = &KartaSlika{PNG: png, Sirina: sirinaKarte, Visina: visinaKarte,
		Zasluge: "© OpenStreetMap doprinositelji"}

	dijelovi := paketIzvjesca(t, iz)
	if got := dijelovi["word/media/slika1.png"]; got != string(png) {
		t.Errorf("karta nije stigla u paket (%d bajtova)", len(got))
	}
	doc := dijelovi["word/document.xml"]
	if !strings.Contains(doc, `r:embed="rIdSlika1"`) {
		t.Error("dokument ne pokazuje na kartu")
	}
	if !strings.Contains(doc, "Položaj letve") {
		t.Error("poglavlje o položaju nedostaje")
	}
	// zasluge za podlogu su uvjet korištenja, ne ukras
	if !strings.Contains(doc, "OpenStreetMap") {
		t.Error("zasluge za podlogu se ne prenose u dokument")
	}
}

// Bez mreže nema pločica. Izvješće se svejedno mora sastaviti — program je
// zamišljen da radi offline — i reći zašto karte nema, da čitatelj ne misli
// kako letva nije ubilježena.
func TestIzvjesceBezKarteNastajeIObjasnjava(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceKartica
	iz.Station.Latitude, iz.Station.Longitude = stupanj(45.845833), stupanj(18.854722)
	iz.Karta = nil

	dijelovi := paketIzvjesca(t, iz)
	for ime := range dijelovi {
		if strings.HasPrefix(ime, "word/media/") {
			t.Errorf("bez karte paket ipak nosi %s", ime)
		}
	}
	doc := dijelovi["word/document.xml"]
	if !strings.Contains(doc, "Koordinate") {
		t.Error("koordinate moraju ostati i kad karte nema")
	}
	if !strings.Contains(doc, "pločice podloge nisu bile dostupne") {
		t.Error("izostanak karte se ne objašnjava")
	}
}

// Izvješće historijata govori o nizu, ne o mjestu; karta ondje nema što raditi
// ni kad je složena.
func TestIzvjesceHistorijataNemaKartu(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceHistorijat
	iz.Station.Latitude, iz.Station.Longitude = stupanj(45.845833), stupanj(18.854722)
	iz.Karta = &KartaSlika{PNG: probnaKartaPNG(t), Sirina: sirinaKarte, Visina: visinaKarte}

	for ime := range paketIzvjesca(t, iz) {
		if strings.HasPrefix(ime, "word/media/") {
			t.Errorf("izvješće historijata nosi %s", ime)
		}
	}
}

func probnaKartaPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, sirinaKarte, visinaKarte))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("probna karta: %v", err)
	}
	return b.Bytes()
}

func stupanj(v float64) *float64 { return &v }

func probnaOcitanja(kad time.Time) []models.Reading {
	cm := func(v int) *int { return &v }
	return []models.Reading{
		{MeasuredAt: kad, LevelCm: cm(660), Observer: "Ivan Horvat", Origin: "letva",
			Note: "očitano s letve"},
		{MeasuredAt: kad.AddDate(0, 0, -1), LevelCm: cm(505), Observer: "Ivan Horvat", Origin: "letva"},
		{MeasuredAt: kad.AddDate(0, 0, -2), LevelCm: cm(-118), Observer: "Marko Marić", Origin: "letva"},
	}
}

// Treće izvješće: ono što stoji na operativnoj stranici. Kartica govori što
// letva jest, historijat što se dogodilo, očitanja što je upisano.
func TestIzvjesceOcitanjaNosiUpisano(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceOcitanja
	iz.Ocitanja = probnaOcitanja(time.Date(2026, 9, 8, 7, 0, 0, 0, models.Zagreb))
	iz.OcitanjaOpis = "zadnjih 30 dana"

	html := dokumentXML(t, iz)
	for _, want := range []string{
		"Vodomjerna postaja Batina — očitanja",
		"Očitanja — zadnjih 30 dana",
		"660 cm", "505 cm", "-118 cm",
		"Ivan Horvat", "Marko Marić", "očitano s letve",
		"8.9.2026. 07:00",
		"stranica očitanja", // podnožje govori o pravoj stranici
	} {
		if !strings.Contains(html, want) {
			t.Errorf("u izvješću o očitanjima nema %q", want)
		}
	}
	// ne nosi ono što je na drugim stranicama
	for _, ne := range []string{"Valovi obrane", "Povratni vodostaji", "Pragovi obrane"} {
		if strings.Contains(html, ne) {
			t.Errorf("izvješće o očitanjima nosi %q s druge stranice", ne)
		}
	}
}

// Naslov je jedan redak. Prije su to bila dva zasebna naslova, pa se drugi
// lomio od prvoga kao da su dva dokumenta.
func TestNaslovIzvjescaJeJedanRedak(t *testing.T) {
	for dio, want := range map[string]string{
		izvjesceKartica:    "Vodomjerna postaja Batina",
		izvjesceHistorijat: "Vodomjerna postaja Batina — historijat",
		izvjesceOcitanja:   "Vodomjerna postaja Batina — očitanja",
	} {
		iz := probnoIzvjesce(t)
		iz.Dio = dio
		html := dokumentXML(t, iz)
		if !strings.Contains(html, want) {
			t.Errorf("%s: naslov nije %q", dio, want)
		}
		if strings.Count(html, `w:val="Naslov"`) != 1 {
			t.Errorf("%s: naslova ima %d, mora biti jedan", dio, strings.Count(html, `w:val="Naslov"`))
		}
	}
}

// Prazno razdoblje se kaže, a ne prešuti praznom tablicom.
func TestIzvjesceOcitanjaBezUpisaToKaze(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceOcitanja
	iz.OcitanjaOpis = "zadnjih 30 dana"

	html := dokumentXML(t, iz)
	if !strings.Contains(html, "nema upisanih očitanja") {
		t.Error("prazno razdoblje se prešućuje")
	}
}

// Stupci koji nemaju što pokazati ne stoje prazni.
func TestIzvjesceOcitanjaNemaPraznihStupaca(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceOcitanja
	cm := func(v int) *int { return &v }
	iz.Ocitanja = []models.Reading{{MeasuredAt: time.Now(), LevelCm: cm(-118), Origin: "letva"}}

	// "Napomena" je i naziv stila u dokumentu, pa se traži tekst ćelije, ne riječ
	html := dokumentXML(t, iz)
	for _, ne := range []string{"Stupanj obrane", "Napomena"} {
		if strings.Contains(html, ">"+ne+"<") {
			t.Errorf("prazan stupac %q stoji u tablici", ne)
		}
	}
	// a kad ima što pokazati, stupac se pojavi
	iz.Ocitanja[0].Note = "led na letvi"
	if !strings.Contains(dokumentXML(t, iz), ">Napomena<") {
		t.Error("stupac napomene se ne pojavljuje ni kad napomena postoji")
	}
}

// Tri izvješća iste letve moraju imati različita imena — inače se u mapi
// preuzimanja tiho prepisuju.
func TestTriIzvjescaImajuRazlicitaImena(t *testing.T) {
	st := models.Station{Code: "batina", Name: "Batina"}
	kad := time.Date(2026, 9, 10, 12, 0, 0, 0, models.Zagreb)
	vidjeno := map[string]string{}
	for _, dio := range []string{izvjesceKartica, izvjesceHistorijat, izvjesceOcitanja} {
		ime := imeIzvjesca(st, kad, dio)
		if prije, ima := vidjeno[ime]; ima {
			t.Errorf("%s i %s dijele ime %q", prije, dio, ime)
		}
		vidjeno[ime] = dio
	}
}
