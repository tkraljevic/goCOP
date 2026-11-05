package web

import (
	"math"
	"slices"
	"strings"

	"gocop/internal/models"
)

// Skupine pregleda prognoza: sektor, pa vodotoci sektora, pa letve
// (Odluke (5. 11. 2026.)). Letva ide u sektor koji je prati — preko dionica,
// upisanog sektora ili područja. Tuđa letva (mađarska, slovenska, srpska)
// ide uz sektor kojem je ulaz ili koji je iz nje računa, a kad ni to ne
// postoji, uz najbližu letvu na istoj vodi. Dunav se dijeli kod Batine i
// Bezdana. Letva bez sektora ide u „Ostale letve”, da ne nestane.

// SektorPrognoze je sektor s vodotocima na kojima ima letve izdanja.
type SektorPrognoze struct {
	ID       string // A … F; „ostale” za letve bez sektora
	Ime      string // ime iz registra, bez „Sektor A —”
	Vodotoci []VodotokPrognoze
}

// VodotokPrognoze je vodotok sektora s letvama, redom pregleda (od uzvodne
// prema nizvodnoj).
type VodotokPrognoze struct {
	Oznaka string // u adresi: dunav-uzvodno, drava
	Ime    string // na čipu: Dunav uzvodno, Drava
	Voda   string // vodotok iz registra (obje polovice Dunava su Dunav), za „Cijeli vodotok”
	Letve  []string
}

const (
	// OstaleLetve je skupina letvi bez sektora ili bez vodotoka.
	OstaleLetve   = "Ostale letve"
	oznakaOstalih = "ostale"
	DunavUzvodno  = "Dunav uzvodno"
	DunavNizvodno = "Dunav nizvodno"
)

// granicaDunava je mjesto na kojem se Dunav dijeli: najuzvodnija hrvatska i
// najuzvodnija srpska letva na Dunavu (Batina i Bezdan). Sve od njih naniže
// je „Dunav nizvodno”.
type granicaDunava struct {
	km    float64
	ima   bool
	imena []string // hrvatska pa srpska letva na granici
}

// tudaLetva javlja je li letva u drugoj državi: ime joj nosi državu u
// zagradi, „Mohács (Mađarska)”.
func tudaLetva(st models.Station) bool {
	_, drzava := imeIDrzava(st.Name)
	return drzava != ""
}

// granicaIzRegistra traži najuzvodniju hrvatsku i srpsku letvu na Dunavu.
func granicaIzRegistra(postaje map[string]models.Station) granicaDunava {
	var g granicaDunava
	naj, najKm := najuzvodnijeNaDunavu(postaje)
	for _, drzava := range []string{"", "RS"} {
		st, ima := naj[drzava]
		if !ima {
			continue
		}
		ime, _ := imeIDrzava(st.Name)
		g.imena = append(g.imena, ime)
		if !g.ima || najKm[drzava] > g.km {
			g.km, g.ima = najKm[drzava], true
		}
	}
	return g
}

// najuzvodnijeNaDunavu je po državi (naša bez kratice i srpska)
// najuzvodnija letva na Dunavu s riječnim kilometrom.
func najuzvodnijeNaDunavu(postaje map[string]models.Station) (map[string]models.Station, map[string]float64) {
	naj := map[string]models.Station{}
	najKm := map[string]float64{}
	for _, st := range postaje {
		_, drzava := imeIDrzava(st.Name)
		km, ok := rijecniKm(st.Stationing)
		if st.Watercourse != "Dunav" || !ok || (drzava != "" && drzava != "RS") {
			continue
		}
		if k, bilo := najKm[drzava]; !bilo || km > k {
			naj[drzava], najKm[drzava] = st, km
		}
	}
	return naj, najKm
}

// dioDunava kaže je li letva na Dunavu uzvodno ili nizvodno od granice: po
// riječnom kilometru, a bez njega po državi (naša i srpska nizvodno).
func dioDunava(st models.Station, g granicaDunava) string {
	if km, ok := rijecniKm(st.Stationing); ok && g.ima {
		if km <= g.km+0.001 {
			return DunavNizvodno
		}
		return DunavUzvodno
	}
	if _, drzava := imeIDrzava(st.Name); drzava == "" || drzava == "RS" {
		return DunavNizvodno
	}
	return DunavUzvodno
}

// vodotokLetve je vodotok pod kojim letva stoji na pregledu.
func vodotokLetve(kod string, postaje map[string]models.Station, g granicaDunava) VodotokPrognoze {
	st := postaje[kod]
	voda := skupinaPrikaza(kod, strings.TrimSpace(st.Watercourse))
	ime := voda
	switch voda {
	case "":
		return VodotokPrognoze{Oznaka: oznakaOstalih, Ime: OstaleLetve}
	case "Dunav":
		ime = dioDunava(st, g)
	}
	return VodotokPrognoze{Oznaka: oznakaImena(ime), Ime: ime, Voda: voda}
}

// imeSektora je ime sektora bez početka „Sektor B —”: „Dunav i donja Drava”.
func imeSektora(s models.Sector) string {
	ime := s.Name
	if strings.HasPrefix(ime, "Sektor "+s.ID) {
		ime = strings.TrimLeft(strings.TrimPrefix(ime, "Sektor "+s.ID), " —–-:")
	}
	if ime == "" {
		return s.Name
	}
	return ime
}

// sektoriSPodrucjima su sektori koji imaju branjena područja (A … F), bez
// direkcije.
func sektoriSPodrucjima(sektori []models.Sector, podrucja []models.Area) []models.Sector {
	ima := map[string]bool{}
	for _, a := range podrucja {
		ima[a.SectorID] = true
	}
	var out []models.Sector
	for _, s := range sektori {
		if ima[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// uredeni: bez praznih i dvojnika, poredani.
func uredeni(s []string) []string {
	s = slices.DeleteFunc(slices.Clone(s), func(x string) bool { return x == "" })
	slices.Sort(s)
	return slices.Compact(s)
}

// vlastitiSektori su sektori koji letvu prate po registru: preko dionica
// (dionice: ID postaje → sektori), upisani sektori i sektori upisanih
// područja.
func vlastitiSektori(postaje map[string]models.Station, dionice map[string][]string, podrucja []models.Area) map[string][]string {
	sektorPodrucja := map[int]string{}
	for _, a := range podrucja {
		sektorPodrucja[a.ID] = a.SectorID
	}
	out := map[string][]string{}
	for kod, st := range postaje {
		if s := uredeni(append(st.SektoriPracenja(sektorPodrucja), dionice[st.ID.String()]...)); len(s) > 0 {
			out[kod] = s
		}
	}
	return out
}

// izvedeniSektori daje sektor i letvama koje ga u registru nemaju, preko
// lanca: letva računata iz letve sektora i tuđa letva koja je ulaz letve
// sektora idu u taj sektor. Sektor dalje uz lanac prenosi samo letva koja
// ga ima po registru ili je tuđa — tako mađarski Dunav od Budimpešte do
// Mohácsa ide uz sektor B, a Botovo, ulaz Novog Virja, ne vuče za sobom Muru
// i gornju Dravu. Tuđa letva bez ikakve veze ide uz najbližu letvu na istoj
// vodi.
func izvedeniSektori(letve []LetvaPrognoze, postaje map[string]models.Station, vlastiti map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, l := range letve {
		if s := vlastiti[l.Kod]; len(s) > 0 {
			out[l.Kod] = s
		}
	}
	prenosi := func(kod string) []string {
		if len(vlastiti[kod]) > 0 || tudaLetva(postaje[kod]) {
			return out[kod]
		}
		return nil
	}
	for promjena := true; promjena; {
		promjena = false
		for _, l := range letve {
			if len(out[l.Kod]) > 0 {
				continue
			}
			if s := sektoriVeza(l, letve, prenosi); len(s) > 0 {
				out[l.Kod], promjena = s, true
			}
		}
	}
	uzNajblizu(letve, postaje, out)
	return out
}

// sektoriVeza su sektori letvi iz kojih se letva računa i, za tuđu letvu,
// letvi kojima je ulaz.
func sektoriVeza(l LetvaPrognoze, letve []LetvaPrognoze, prenosi func(string) []string) []string {
	var s []string
	for _, u := range l.Ulazi {
		s = append(s, prenosi(u)...)
	}
	for _, m := range letve {
		if slices.Contains(m.Ulazi, l.Kod) {
			s = append(s, prenosi(m.Kod)...)
		}
	}
	return uredeni(s)
}

// mjestoNaVodi je letva sa sektorom i riječnim kilometrom.
type mjestoNaVodi struct {
	voda    string
	km      float64
	sektori []string
}

// uzNajblizu daje tuđoj letvi bez sektora sektor najbliže letve na istoj
// vodi (Bezdan uz Batinu, Bačka Palanka uz Ilok).
func uzNajblizu(letve []LetvaPrognoze, postaje map[string]models.Station, out map[string][]string) {
	var poznata []mjestoNaVodi
	for _, l := range letve {
		st := postaje[l.Kod]
		if km, ok := rijecniKm(st.Stationing); ok && len(out[l.Kod]) > 0 {
			poznata = append(poznata, mjestoNaVodi{st.Watercourse, km, out[l.Kod]})
		}
	}
	for _, l := range letve {
		st := postaje[l.Kod]
		km, ok := rijecniKm(st.Stationing)
		if len(out[l.Kod]) == 0 && ok && tudaLetva(st) {
			out[l.Kod] = najblizaNaVodi(poznata, st.Watercourse, km)
		}
	}
}

// najblizaNaVodi su sektori najbliže poznate letve na istoj vodi; nil kad
// je na toj vodi nema.
func najblizaNaVodi(poznata []mjestoNaVodi, voda string, km float64) []string {
	var sektori []string
	naj := -1.0
	for _, p := range poznata {
		if d := math.Abs(p.km - km); p.voda == voda && (naj < 0 || d < naj) {
			naj, sektori = d, p.sektori
		}
	}
	return sektori
}

// skupinePrognoza slaže letve izdanja (redom pregleda) u sektore i
// vodotoke. Svaki sektor iz popisa dobije svoju skupinu, i kad nema letvi
// (čip „nema prognoze”); letve bez sektora idu u skupinu „Ostale letve” na
// kraju. Vodotok kroz više sektora stoji u svakom.
func skupinePrognoza(letve []LetvaPrognoze, postaje map[string]models.Station, sektori []models.Sector,
	vlastiti map[string][]string) []SektorPrognoze {
	sektorLetve := izvedeniSektori(letve, postaje, vlastiti)
	g := granicaIzRegistra(postaje)
	out := make([]SektorPrognoze, 0, len(sektori)+1)
	mjesto := map[string]int{}
	for _, s := range sektori {
		mjesto[s.ID] = len(out)
		out = append(out, SektorPrognoze{ID: s.ID, Ime: imeSektora(s)})
	}
	ostale := SektorPrognoze{ID: oznakaOstalih, Ime: OstaleLetve}
	for _, l := range letve {
		v := vodotokLetve(l.Kod, postaje, g)
		smjestena := false
		for _, id := range sektorLetve[l.Kod] {
			if i, ima := mjesto[id]; ima {
				out[i].dodaj(v, l.Kod)
				smjestena = true
			}
		}
		if !smjestena {
			ostale.dodaj(v, l.Kod)
		}
	}
	if len(ostale.Vodotoci) > 0 {
		out = append(out, ostale)
	}
	for i := range out {
		// letve bez vodotoka na kraj sektora
		slices.SortStableFunc(out[i].Vodotoci, func(a, b VodotokPrognoze) int {
			return boolInt(a.Oznaka == oznakaOstalih) - boolInt(b.Oznaka == oznakaOstalih)
		})
	}
	return out
}

// dodaj stavlja letvu u vodotok sektora; vodotok se otvara kad se prvi put
// pojavi, pa vodotoci idu redom pregleda.
func (s *SektorPrognoze) dodaj(v VodotokPrognoze, kod string) {
	for i := range s.Vodotoci {
		if s.Vodotoci[i].Oznaka == v.Oznaka {
			s.Vodotoci[i].Letve = append(s.Vodotoci[i].Letve, kod)
			return
		}
	}
	v.Letve = []string{kod}
	s.Vodotoci = append(s.Vodotoci, v)
}

// cijeliVodotok su letve vode kroz sve sektore i ostale letve, redom
// pregleda, bez dvojnika.
func cijeliVodotok(skupine []SektorPrognoze, voda string, redom []string) []string {
	if voda == "" {
		return nil
	}
	ima := map[string]bool{}
	for _, s := range skupine {
		for _, v := range s.Vodotoci {
			if v.Voda != voda {
				continue
			}
			for _, k := range v.Letve {
				ima[k] = true
			}
		}
	}
	var out []string
	for _, k := range redom {
		if ima[k] {
			out = append(out, k)
		}
	}
	return out
}

// lokativ je ime vode u lokativu, za „Letve na Dravi”: na -a u -i, na
// suglasnik dodaje -u; ostalo (više riječi, -o, -e) se ne mijenja i javlja
// ok=false.
func lokativ(ime string) (string, bool) {
	if ime == "" || strings.Contains(ime, " ") {
		return ime, false
	}
	switch zadnje := ime[len(ime)-1]; {
	case zadnje == 'a':
		return ime[:len(ime)-1] + "i", true
	case strings.ContainsRune("eiou", rune(zadnje)):
		return ime, false
	}
	return ime + "u", true
}

// naslovLetvi je naslov iznad kartica letvi: „Letve na Dravi, od uzvodne
// prema nizvodnoj”.
func naslovLetvi(v VodotokPrognoze, cijeli bool) string {
	const red = ", od uzvodne prema nizvodnoj"
	ime := v.Ime
	if cijeli && v.Voda != "" {
		ime = v.Voda
	}
	switch {
	case v.Oznaka == oznakaOstalih:
		return OstaleLetve
	case ime == SkupinaInundacije:
		return "Letve u inundaciji Dunava, redom kako voda teče"
	}
	// Lokativ samo za ime vode od jedne riječi; dio Dunava zadrži „uzvodno”.
	if l, ok := lokativ(v.Voda); ok && strings.HasPrefix(ime, v.Voda) {
		return "Letve na " + l + strings.TrimPrefix(ime, v.Voda) + red
	}
	return "Letve: " + ime + red
}

// opisDijela je natpis desno u glavi profila: koji dio vodotoka sektor
// pokazuje. Polovice Dunava se opisuju granicom („uzvodno od Batine i
// Bezdana”); vodotok koji teče i kroz druge sektore „dio u sektoru B”, uz
// „do ušća u Dunav” kad dio seže do zadnje letve vode. Vodotok sav u
// sektoru opisa nema.
func opisDijela(sektor SektorPrognoze, v VodotokPrognoze, cijeli []string, g granicaDunava, usce string) string {
	switch v.Ime {
	case DunavUzvodno:
		return "uzvodno od " + imenaGranice(g)
	case DunavNizvodno:
		return "od " + imenaGranice(g) + " nizvodno"
	}
	if sektor.ID == oznakaOstalih || len(cijeli) <= len(v.Letve) || len(v.Letve) == 0 {
		return ""
	}
	opis := "dio u sektoru " + sektor.ID
	if usce != "" && v.Letve[len(v.Letve)-1] == cijeli[len(cijeli)-1] {
		opis += ", do ušća u " + akuzativ(usce)
	}
	return opis
}

// imenaGranice su letve na granici Dunava u genitivu: „Batine i Bezdana”.
func imenaGranice(g granicaDunava) string {
	var imena []string
	for _, ime := range g.imena {
		if gen, ok := genitivVodotoka(ime); ok {
			ime = gen
		}
		imena = append(imena, ime)
	}
	if len(imena) == 0 {
		return "granice"
	}
	return strings.Join(imena, " i ")
}
