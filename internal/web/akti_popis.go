package web

import (
	"maps"
	"net/url"
	"slices"
	"strings"

	"gocop/internal/models"
)

// CipPopisa je čip iznad popisa: poveznica na isti popis s tim izborom.
type CipPopisa struct {
	Naziv   string
	URL     string
	Odabran bool
}

// zadrziParametre uzima iz adrese samo navedene parametre, bez praznih. Tako
// se u poveznice čipova ne prenose poruke (success, error) ni što drugo.
func zadrziParametre(q url.Values, imena ...string) url.Values {
	out := url.Values{}
	for _, ime := range imena {
		if v := q.Get(ime); v != "" {
			out.Set(ime, v)
		}
	}
	return out
}

func adresaSIzborom(staza string, izbor url.Values) string {
	if len(izbor) == 0 {
		return staza
	}
	return staza + "?" + izbor.Encode()
}

// cipoviSektoraStranice su čipovi sektora iznad popisa: prvi je „Svi”, a
// Direkcija nije među njima. Poveznica čuva pretragu, stupanj, radnju, stanje i
// godinu, a područje briše, jer područje pripada sektoru koji se upravo mijenja.
func cipoviSektoraStranice(staza string, sektori []models.Sector, izabran string, q url.Values) []CipPopisa {
	osnova := zadrziParametre(q, "q", "stupanj", "radnja", "status", "godina")
	return cipoviSektoraPo(staza, "sektor", osnova, sektori, izabran)
}

// cipoviSektoraPo su čipovi sektora koji sektor nose u parametru ime, uz
// stalni dio adrese osnova (dnevnik COP-a sektor bira parametrom centar).
func cipoviSektoraPo(staza, ime string, osnova url.Values, sektori []models.Sector, izabran string) []CipPopisa {
	out := []CipPopisa{{Naziv: "Svi", URL: adresaSIzborom(staza, osnova), Odabran: izabran == ""}}
	for _, s := range sektori {
		if s.ID == "DIREKCIJA" {
			continue
		}
		izbor := maps.Clone(osnova)
		izbor.Set(ime, s.ID)
		naziv := s.ID
		// Sektor bez opisa u nazivu („Sektor B”) ima samo oznaku.
		if ime := imeSektora(s); ime != "" && !strings.HasPrefix(ime, "Sektor "+s.ID) {
			naziv += " · " + ime
		}
		out = append(out, CipPopisa{Naziv: naziv, URL: adresaSIzborom(staza, izbor), Odabran: s.ID == izabran})
	}
	return out
}

// cipoviStanjaAkata mijenjaju samo parametar status, pa ostali izbor ostaje.
func cipoviStanjaAkata(izabrano string, q url.Values) []CipPopisa {
	osnova := zadrziParametre(q, "q", "sektor", "podrucje", "stupanj", "radnja", "godina")
	stanja := []struct{ naziv, status string }{{"Svi", ""}, {"Čeka ovjeru", models.AktNacrt}, {"Ovjereni", models.AktOvjeren}}
	out := make([]CipPopisa, 0, len(stanja))
	for _, s := range stanja {
		izbor := maps.Clone(osnova)
		if s.status != "" {
			izbor.Set("status", s.status)
		}
		out = append(out, CipPopisa{Naziv: s.naziv, URL: adresaSIzborom("/akti", izbor), Odabran: s.status == izabrano})
	}
	return out
}

// izborPopisa upisuje u stranicu popisa čipove sektora i stanja i traku sukoba.
func izborPopisa(data *AktiPageData, perms *models.UserPermissions, q url.Values) {
	data.SukobNaPopisu = recenicaSukoba(data.Akti, data.Sukobi)
	data.CipoviSektora = cipoviSektoraStranice("/akti", data.Sektori, data.Filtar.Sektor, q)
	// Osobu s jednim sektorom prazan izbor vraća na njezin sektor, pa „Svi” ne bi ništa promijenio.
	if jedanSektorRada(perms) {
		data.CipoviSektora = data.CipoviSektora[1:]
	}
	data.CipoviStanja = cipoviStanjaAkata(data.Filtar.Status, q)
}

// jedanSektorRada javlja radi li osoba, koja nije globalni administrator, u
// samo jednom sektoru; popis joj tada uvijek pokazuje taj sektor.
func jedanSektorRada(perms *models.UserPermissions) bool {
	return perms != nil && !perms.IsGlobalAdmin && len(perms.SektoriRada()) == 1
}

// sukobiNaPopisu su nerazriješeni sukobi redaka koji su na popisu: oznake s
// više akata, najveći broj akata iste oznake i ima li drugih ovjera.
type sukobiNaPopisu struct {
	oznake []string
	istih  int
	ovjere bool
}

func skupiSukobe(akti []models.Akt, sukobi map[string]models.SukobOznake) sukobiNaPopisu {
	var out sukobiNaPopisu
	for _, a := range akti {
		s, ok := sukobi[a.ID]
		if !ok || !s.Ima() || s.Rijesen() {
			continue
		}
		out.ovjere = out.ovjere || len(s.Ovjere) > 0
		if len(s.Drugi) == 0 {
			continue
		}
		out.istih = max(out.istih, len(s.Drugi)+1)
		if !slices.Contains(out.oznake, a.Oznaka()) {
			out.oznake = append(out.oznake, a.Oznaka())
		}
	}
	return out
}

// recenicaSukoba kaže koji akti na ovom popisu čekaju odluku o sukobu oznake.
// Sukobi vrijede za cijelu bazu, a traka govori samo o recima koji se vide i
// koji još nisu riješeni. Bez takvih redaka rečenice nema.
func recenicaSukoba(akti []models.Akt, sukobi map[string]models.SukobOznake) string {
	s := skupiSukobe(akti, sukobi)
	var recenice []string
	switch {
	case len(s.oznake) > 1:
		recenice = append(recenice, "Neki akti imaju istu oznaku: "+strings.Join(s.oznake, ", ")+".")
	case len(s.oznake) == 1 && s.istih > 2:
		recenice = append(recenice, "Više akata ima istu oznaku "+s.oznake[0]+".")
	case len(s.oznake) == 1:
		recenice = append(recenice, "Dva akta imaju istu oznaku "+s.oznake[0]+".")
	}
	if s.ovjere {
		recenice = append(recenice, "Isti nacrt ovjeren je na više računala.")
	}
	return strings.Join(recenice, " ")
}
