package web

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Izvoz i ispravak operativnih očitanja preko CSV-a.
//
// Razlika prema arhivi je bitna. Arhiva se ne dira: ispravak stoji uz nju i
// vidi se kao ispravak. Očitanja su živ zapis ovog programa, pa se ispravkom
// stvarno mijenjaju — kroz istu knjigu verzija kao izmjena jednog po jednog,
// tako da se poslije zna tko je i kad što promijenio.
//
// Zato ovdje ključ nije vrijeme nego identifikator očitanja: vrijeme je jedna
// od stvari koje se ispravljaju (netko upiše 13 umjesto 07 sati), pa po njemu
// redak ne bi bio prepoznatljiv.

// stupciOcitanja je zaglavlje izvezene datoteke. Prva tri stupca ne diraj —
// oni kažu koji je redak koji; ostali su ono što se ispravlja.
var stupciOcitanja = []string{
	"id", "vrijeme", "vodostaj_cm", "nizvodni_cm", "ocitao", "napomena", "obrisi", "izvor", "upisao",
}

// letvaIzPutanje čita letvu iz putanje, postaju ili objekt, kako je već
// stranica povijesti pozvana.
func (h *ReadingsHandler) letvaIzPutanje(r *http.Request) (*models.Station, *models.Structure) {
	if strings.HasPrefix(r.URL.Path, "/readings/structure/") {
		return h.gauge(r, "", r.PathValue("id"))
	}
	return h.gauge(r, r.PathValue("id"), "")
}

// vezaLetve je putanja natrag na stranicu te letve.
func vezaLetve(station *models.Station, structure *models.Structure) string {
	if structure != nil {
		return "/readings/structure/" + structure.ID.String()
	}
	if station != nil {
		return "/readings/station/" + station.ID.String()
	}
	return "/readings"
}

// HandleOcitanjaIzvoz šalje očitanja letve kao CSV, za pregled i ispravak.
func (h *ReadingsHandler) HandleOcitanjaIzvoz(w http.ResponseWriter, r *http.Request) {
	station, structure := h.letvaIzPutanje(r)
	if station == nil && structure == nil {
		http.NotFound(w, r)
		return
	}
	f := repository.ReadingFilter{}
	sifra := "letva"
	if structure != nil {
		f.StructureID = structure.ID.String()
		sifra = structure.Code
	} else {
		f.StationID = station.ID.String()
		sifra = station.Code
	}
	sve, err := h.readingService.List(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Izvozi se ono razdoblje koje je na zaslonu odabrano, da datoteka odgovara
	// onome što se gledalo. Bez odabira ide sve.
	sve = uRazdoblju(sve, r.URL.Query().Get("pogled"))
	// od najstarijeg prema novijem: tako se čita i tako se ispravlja
	for i, j := 0, len(sve)-1; i < j; i, j = i+1, j-1 {
		sve[i], sve[j] = sve[j], sve[i]
	}

	if sifra == "" {
		sifra = "letva"
	}
	ime := fmt.Sprintf("%s_ocitanja_%s.csv", sifra, time.Now().In(models.Zagreb).Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+ime+`"`)
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM, da Excel prepozna hrvatska slova

	cw := csv.NewWriter(w)
	cw.Comma = ';'
	defer cw.Flush()
	_ = cw.Write(stupciOcitanja)
	for _, rd := range sve {
		_ = cw.Write([]string{
			rd.ID.String(),
			rd.LocalTime().Format("2006-01-02 15:04"),
			cijeliBroj(rd.LevelCm),
			cijeliBroj(rd.Level2Cm),
			rd.Observer,
			rd.Note,
			"", // ovdje se upisuje "da" za brisanje
			rd.SourceLabel(),
			rd.UserName,
		})
	}
}

// uRazdoblju sužava popis na isti pogled koji stranica pokazuje.
func uRazdoblju(sve []models.Reading, pogled string) []models.Reading {
	if pogled == "" || pogled == "sve" {
		return sve
	}
	n, err := strconv.Atoi(pogled)
	if err != nil {
		return sve
	}
	out := sve[:0:0]
	if n > 1900 { // godina
		for _, rd := range sve {
			if rd.LocalTime().Year() == n {
				out = append(out, rd)
			}
		}
		return out
	}
	if n <= 0 || n > 3650 {
		return sve
	}
	granica := time.Now().AddDate(0, 0, -n)
	for _, rd := range sve {
		if rd.MeasuredAt.After(granica) {
			out = append(out, rd)
		}
	}
	return out
}

func cijeliBroj(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// RedakOcitanja je jedan redak vraćene datoteke: što je bilo, što bi postalo.
type RedakOcitanja struct {
	Redak   int
	ID      uuid.UUID
	Staro   models.Reading
	Novo    models.Reading
	Brisati bool
	Izmjene []Izmjena
	Greska  string
}

// Izmjena je jedno promijenjeno polje, da se u pregledu vidi točno što se mijenja.
type Izmjena struct {
	Polje, Staro, Novo string
}

func (r RedakOcitanja) Mijenja() bool { return r.Greska == "" && (r.Brisati || len(r.Izmjene) > 0) }

// citajOcitanja čita vraćenu datoteku i uspoređuje je s onim što je u bazi.
// Ništa se ne upisuje: vraća se popis onoga što bi se promijenilo. Excel zna
// sam prepraviti datum ili odsjeći vodeću nulu, pa je taj pogled jedina obrana
// od tihog prepisivanja.
func citajOcitanja(sadrzaj []byte, postojeca map[uuid.UUID]models.Reading) ([]RedakOcitanja, error) {
	cr := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(sadrzaj), "\ufeff")))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	redci, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("datoteka nije čitljiva: %w", err)
	}
	if len(redci) < 2 {
		return nil, fmt.Errorf("datoteka nema nijedan redak s podacima")
	}
	glava := redci[0]
	stupac := func(naziv string) int {
		for i, g := range glava {
			if strings.EqualFold(strings.TrimSpace(g), naziv) {
				return i
			}
		}
		return -1
	}
	iID, iVrijeme := stupac("id"), stupac("vrijeme")
	iVodostaj, iNizvodni := stupac("vodostaj_cm"), stupac("nizvodni_cm")
	iOcitao, iNapomena, iObrisi := stupac("ocitao"), stupac("napomena"), stupac("obrisi")
	if iID < 0 || iVrijeme < 0 || iVodostaj < 0 {
		return nil, fmt.Errorf("datoteci nedostaje stupac „id“, „vrijeme“ ili „vodostaj_cm“ — je li izvezena odavde?")
	}
	polje := func(r []string, i int) string {
		if i < 0 || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}

	var out []RedakOcitanja
	vidjeno := map[uuid.UUID]bool{}
	for i, r := range redci[1:] {
		sirovID := polje(r, iID)
		if sirovID == "" {
			continue // prazan redak na kraju datoteke
		}
		red := RedakOcitanja{Redak: i + 2}
		id, err := uuid.Parse(sirovID)
		if err != nil {
			red.Greska = "prvi stupac nije identifikator očitanja: " + sirovID
			out = append(out, red)
			continue
		}
		red.ID = id
		staro, ima := postojeca[id]
		if !ima {
			red.Greska = "tog očitanja nema na ovoj letvi — je li datoteka s druge?"
			out = append(out, red)
			continue
		}
		if vidjeno[id] {
			red.Greska = "isto očitanje dolazi dvaput u datoteci"
			out = append(out, red)
			continue
		}
		vidjeno[id] = true
		red.Staro = staro

		if da(polje(r, iObrisi)) {
			red.Brisati = true
			out = append(out, red)
			continue
		}

		novo := staro
		kad, err := vrijemeOcitanja(polje(r, iVrijeme))
		if err != nil {
			red.Greska = err.Error()
			out = append(out, red)
			continue
		}
		novo.MeasuredAt = kad
		if novo.MeasuredAt.After(time.Now().Add(2 * time.Minute)) {
			red.Greska = "vrijeme je u budućnosti: " + polje(r, iVrijeme)
			out = append(out, red)
			continue
		}
		if v, err := neobavezanCm(polje(r, iVodostaj)); err != nil {
			red.Greska = "vodostaj: " + err.Error()
			out = append(out, red)
			continue
		} else {
			novo.LevelCm = v
		}
		if v, err := neobavezanCm(polje(r, iNizvodni)); err != nil {
			red.Greska = "nizvodni vodostaj: " + err.Error()
			out = append(out, red)
			continue
		} else {
			novo.Level2Cm = v
		}
		if iOcitao >= 0 {
			novo.Observer = polje(r, iOcitao)
		}
		if iNapomena >= 0 {
			novo.Note = polje(r, iNapomena)
		}
		if novo.LevelCm == nil && novo.Level2Cm == nil {
			red.Greska = "očitanje bez ijednog vodostaja — za brisanje upiši „da“ u stupac obrisi"
			out = append(out, red)
			continue
		}
		red.Novo = novo
		red.Izmjene = razlike(staro, novo)
		out = append(out, red)
	}
	return out, nil
}

// razlike su polja koja se stvarno mijenjaju. Redak koji ništa ne mijenja ne
// treba ni pokazivati ni upisivati — inače bi svako vraćanje datoteke izgledalo
// kao izmjena svih očitanja.
func razlike(staro, novo models.Reading) []Izmjena {
	var out []Izmjena
	if !staro.MeasuredAt.Equal(novo.MeasuredAt) {
		out = append(out, Izmjena{"vrijeme",
			staro.LocalTime().Format("2.1.2006. 15:04"), novo.LocalTime().Format("2.1.2006. 15:04")})
	}
	if !istiCm(staro.LevelCm, novo.LevelCm) {
		out = append(out, Izmjena{"vodostaj", cmIliCrta(staro.LevelCm), cmIliCrta(novo.LevelCm)})
	}
	if !istiCm(staro.Level2Cm, novo.Level2Cm) {
		out = append(out, Izmjena{"nizvodni", cmIliCrta(staro.Level2Cm), cmIliCrta(novo.Level2Cm)})
	}
	if staro.Observer != novo.Observer {
		out = append(out, Izmjena{"očitao", staro.Observer, novo.Observer})
	}
	if staro.Note != novo.Note {
		out = append(out, Izmjena{"napomena", staro.Note, novo.Note})
	}
	return out
}

func istiCm(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cmIliCrta(v *int) string {
	if v == nil {
		return "—"
	}
	return strconv.Itoa(*v) + " cm"
}

// da prepoznaje potvrdu onako kako je ljudi pišu u tablici.
func da(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "da", "d", "x", "1", "true", "obriši", "obrisi":
		return true
	}
	return false
}

// vrijemeOcitanja prima oblik koji izvoz piše, ali i one koje Excel od njega
// napravi kad stupac proglasi datumom.
func vrijemeOcitanja(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("vrijeme je prazno")
	}
	for _, oblik := range []string{
		"2006-01-02 15:04", "2006-01-02 15:04:05", "2006-01-02T15:04",
		"2.1.2006. 15:04", "2.1.2006 15:04", "02.01.2006 15:04", "02.01.2006. 15:04",
		"1.2.2006 15:04", "2006-01-02",
	} {
		if t, err := time.ParseInLocation(oblik, s, models.Zagreb); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("vrijeme nije čitljivo: %s", s)
}

// neobavezanCm čita vodostaj koji smije i izostati.
func neobavezanCm(s string) (*int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimSuffix(s, "cm")
	// Excel zna cijeli broj prikazati kao 123,00
	if zarez := strings.IndexAny(s, ",."); zarez >= 0 {
		rep := strings.TrimRight(s[zarez+1:], "0")
		if rep != "" {
			return nil, fmt.Errorf("vodostaj se vodi u punim centimetrima: %s", s)
		}
		s = s[:zarez]
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("nije cijeli broj: %s", s)
	}
	return &v, nil
}

// PregledOcitanja je ono što se pokaže prije nego što se išta upiše.
type PregledOcitanja struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions
	Station     *models.Station
	Structure   *models.Structure
	GaugeName   string
	BackURL     string
	Redci       []RedakOcitanja
	Izmjena     int
	Brisanja    int
	Greske      int
	Netaknuto   int
	Datoteka    string

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// HandleOcitanjaUvoz prima vraćenu datoteku i pokazuje što bi se promijenilo.
func (h *ReadingsHandler) HandleOcitanjaUvoz(w http.ResponseWriter, r *http.Request) {
	station, structure := h.letvaIzPutanje(r)
	if station == nil && structure == nil {
		http.NotFound(w, r)
		return
	}
	back := vezaLetve(station, structure)
	u, perms := h.base(r)
	if !h.smijeMijenjati(perms, station, structure) {
		redirectWith(w, r, back, "error", "Nemate pravo mijenjati očitanja ove letve")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		redirectWith(w, r, back, "error", "Datoteka nije primljena: "+err.Error())
		return
	}
	f, zaglavlje, err := r.FormFile("datoteka")
	if err != nil {
		redirectWith(w, r, back, "error", "Odaberi datoteku")
		return
	}
	defer f.Close()
	sadrzaj, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	postojeca, err := h.ocitanjaLetve(r, station, structure)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redci, err := citajOcitanja(sadrzaj, postojeca)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	ime, _ := gaugeNames(station, structure)
	data := PregledOcitanja{
		CurrentUser: u, Permissions: perms, Station: station, Structure: structure,
		GaugeName: ime, BackURL: back, Redci: redci, Datoteka: zaglavlje.Filename,
		ActiveNav: "readings", ViewAsBanner: viewBanner(r),
	}
	for _, x := range redci {
		switch {
		case x.Greska != "":
			data.Greske++
		case x.Brisati:
			data.Brisanja++
		case len(x.Izmjene) > 0:
			data.Izmjena++
		default:
			data.Netaknuto++
		}
	}
	if err := h.tmplOcitanjaCSV.ExecuteTemplate(w, "ocitanja_ispravci.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleOcitanjaPotvrda upisuje ispravke koje je čovjek vidio i potvrdio.
// Datoteka se ne čita ponovno: upisuje se samo ono što je bilo na zaslonu, i
// to redak po redak kroz servis, pa svaka izmjena prolazi iste provjere i
// istu knjigu verzija kao izmjena jednog očitanja.
func (h *ReadingsHandler) HandleOcitanjaPotvrda(w http.ResponseWriter, r *http.Request) {
	station, structure := h.letvaIzPutanje(r)
	if station == nil && structure == nil {
		http.NotFound(w, r)
		return
	}
	back := vezaLetve(station, structure)
	_, perms := h.base(r)
	if !h.smijeMijenjati(perms, station, structure) {
		redirectWith(w, r, back, "error", "Nemate pravo mijenjati očitanja ove letve")
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	postojeca, err := h.ocitanjaLetve(r, station, structure)
	if err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}

	var promijenjeno, obrisano int
	var greske []string
	for i, sirovID := range r.Form["id"] {
		id, err := uuid.Parse(sirovID)
		if err != nil {
			continue
		}
		staro, ima := postojeca[id]
		if !ima {
			greske = append(greske, "očitanje više ne postoji")
			continue
		}
		if da(nth(r.Form["obrisi"], i)) {
			if _, err := h.readingService.Delete(r.Context(), perms, id); err != nil {
				greske = append(greske, err.Error())
				continue
			}
			obrisano++
			continue
		}
		novo := staro
		kad, err := time.Parse(time.RFC3339, nth(r.Form["vrijeme"], i))
		if err != nil {
			greske = append(greske, "vrijeme nije čitljivo")
			continue
		}
		novo.MeasuredAt = kad.UTC()
		novo.LevelCm = cmIzObrasca(nth(r.Form["vodostaj"], i))
		novo.Level2Cm = cmIzObrasca(nth(r.Form["nizvodni"], i))
		novo.Observer = strings.TrimSpace(nth(r.Form["ocitao"], i))
		novo.Note = strings.TrimSpace(nth(r.Form["napomena"], i))
		if len(razlike(staro, novo)) == 0 {
			continue
		}
		if err := h.readingService.Update(r.Context(), perms, &novo); err != nil {
			greske = append(greske, err.Error())
			continue
		}
		promijenjeno++
	}

	if promijenjeno == 0 && obrisano == 0 {
		poruka := "Nijedan redak nije promijenjen"
		if len(greske) > 0 {
			poruka += ": " + greske[0]
		}
		redirectWith(w, r, back, "error", poruka)
		return
	}
	poruka := fmt.Sprintf("Ispravljeno %s", ocitanja(promijenjeno))
	if obrisano > 0 {
		poruka += fmt.Sprintf(", obrisano %s", ocitanja(obrisano))
	}
	poruka += ". Svaka izmjena stoji u knjizi verzija s vašim imenom."
	if len(greske) > 0 {
		poruka += fmt.Sprintf(" %d redaka nije prošlo: %s", len(greske), greske[0])
	}
	redirectWith(w, r, back, "success", poruka)
}

// smijeMijenjati je isto pravilo po kojem se očitanje smije i upisati.
func (h *ReadingsHandler) smijeMijenjati(perms *models.UserPermissions,
	station *models.Station, structure *models.Structure) bool {
	if structure != nil {
		return h.readingService.CanRecordStructure(perms, structure)
	}
	return h.readingService.CanRecordStation(perms, station)
}

// ocitanjaLetve vraća sva očitanja letve po identifikatoru.
func (h *ReadingsHandler) ocitanjaLetve(r *http.Request,
	station *models.Station, structure *models.Structure) (map[uuid.UUID]models.Reading, error) {
	f := repository.ReadingFilter{}
	if structure != nil {
		f.StructureID = structure.ID.String()
	} else {
		f.StationID = station.ID.String()
	}
	sve, err := h.readingService.List(r.Context(), f)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]models.Reading, len(sve))
	for _, rd := range sve {
		out[rd.ID] = rd
	}
	return out, nil
}

func cmIzObrasca(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &v
}

func ocitanja(n int) string {
	switch {
	case n == 1:
		return "1 očitanje"
	case n < 5:
		return fmt.Sprintf("%d očitanja", n)
	}
	return fmt.Sprintf("%d očitanja", n)
}
