package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
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

// Izvješće mora nositi sve što je o letvi izračunato. Kad se doda nova
// analiza a izvješće se zaboravi, dokument tiho ostane nepotpun — a nitko to
// ne vidi jer se datoteka i dalje otvara.
func TestIzvjesceNosiSveAnalize(t *testing.T) {
	doc := dokumentXML(t, probnoIzvjesce(t))
	for _, want := range []string{
		"Vodomjerna postaja Batina", "Dunav", "rkm 1.425+000",
		"Osnovni podaci", "Kota nule vodomjera", "80,450", "80,189", "0,261",
		"Pragovi obrane od poplava", "Pripremno stanje", "Izvanredno stanje",
		"Zabilježeni ekstremi", "+775 cm", "+795 cm", "rekonstruirano",
		"Povratni vodostaji", "+757 cm", "+792 cm", "25 godina", "100 godina",
		"1 % svake godine", "POT, generalizirana Pareto",
		"Valovi obrane u nizu", "Zbroj za cijeli niz", "Redovna obrana",
		"Korito", "Ivan Horvat", "O ovom izvješću",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("u izvješću nema %q", want)
		}
	}
}

// Izvješće je procjena i izračun, ne mjerenje. Ograde koje na stranici stoje
// uz brojke moraju ići i u dokument — dokument putuje dalje od stranice i
// čita ga netko tko izračun nije vidio.
func TestIzvjesceNosiOgradeUzBrojke(t *testing.T) {
	doc := dokumentXML(t, probnoIzvjesce(t))
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
		if got := imeIzvjesca(s.st, kad); got != s.want {
			t.Errorf("%q → %q, očekivano %q", s.st.Name+s.st.Code, got, s.want)
		}
	}
	sigurno := regexp.MustCompile(`^[a-z0-9.-]+$`)
	if !sigurno.MatchString(imeIzvjesca(models.Station{Name: "Čađavica–Sjever"}, kad)) {
		t.Errorf("ime nije sigurno: %q", imeIzvjesca(models.Station{Name: "Čađavica–Sjever"}, kad))
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
