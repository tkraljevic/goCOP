package web

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Naslovna dežurnog u COP-u (Odluke (31. 10. 2026.)): Moje dežurstvo, Za
// riješiti, Stanje obrane sektora po branjenim područjima, Dnevnik COP-a,
// Vodostaji, Vrijeme i Jače oborine.

// kStanjeSektora je kartica stanja obrane jednog sektora po područjima
const kStanjeSektora = "stanje-sektora"

// fazaPodrucja je najviši stupanj obrane na dionicama područja, s
// trenutkom od kada traje
func fazaPodrucja(area int, stanja map[string]models.StanjeObrane, dionice []models.Section) models.AktivniStadij {
	najvisi := models.AktivniStadij{}
	for _, d := range dionice {
		if v := stanja[d.Code].Vrh(); d.AreaID == area && v.Stupanj.Severity() > najvisi.Stupanj.Severity() {
			najvisi = v
		}
	}
	return najvisi
}

// karticaStanjaSektora: svako branjeno područje sektora u jednom retku,
// područja u obrani prva i s plohom faze, ostala u preventivnoj obrani; uz
// područje rukovoditelj i ljudi na terenu (ljudi po području)
func karticaStanjaSektora(sektor string, podrucja []models.Area, stanja map[string]models.StanjeObrane, dionice []models.Section, ljudi map[int]string) KarticaNaslovne {
	k := KarticaNaslovne{ID: kStanjeSektora, Naslov: "Stanje obrane · Sektor " + sektor, Poveznica: "Akti", URL: "/akti", HitnoNaziv: "u obrani"}
	type redPodrucja struct {
		a models.Area
		f models.AktivniStadij
	}
	var redovi []redPodrucja
	for _, a := range podrucja {
		redovi = append(redovi, redPodrucja{a, fazaPodrucja(a.ID, stanja, dionice)})
	}
	slices.SortStableFunc(redovi, func(x, y redPodrucja) int {
		return cmp.Or(cmp.Compare(y.f.Stupanj.Severity(), x.f.Stupanj.Severity()), cmp.Compare(x.a.ID, y.a.ID))
	})
	for _, r := range redovi {
		red := RedakKartice{Glavno: podrucjeNaslovne(r.a), Sporedno: ljudi[r.a.ID], Vrijednost: "preventivna obrana", Tiho: true}
		if r.f.Stupanj.InForce() {
			red.Istaknuto, red.Vrijednost, red.Hitno = r.f.Stupanj.PillClass(), r.f.Stupanj.Label(), true
			red.Sporedno = strings.TrimPrefix(red.Sporedno+" · od "+odKadFaze(r.f.Od), " · ")
		}
		k.Redovi = append(k.Redovi, red)
	}
	if len(k.Redovi) == 0 {
		k.Prazno = &PraznaKartica{Vrsta: PraznoNedostupno, Tekst: "Područja sektora trenutačno nisu dostupna."}
	}
	return k
}

// podrucjeNaslovne: „BP 34 · Međudržavne rijeke Drava i Dunav”
func podrucjeNaslovne(a models.Area) string {
	if ime := strings.TrimSpace(a.Name); ime != "" {
		return fmt.Sprintf("BP %d · %s", a.ID, ime)
	}
	return fmt.Sprintf("BP %d", a.ID)
}

// Kartice dežurnog
const (
	kMojeDezurstvo = "moje-dezurstvo"
	kDnevnikCOP    = "dnevnik-cop"
)

// dezurnaDuznost je valjana dužnost dežurnog operatera osobe, sa sektorom;
// nil kad je nema
func dezurnaDuznost(u *models.User, sad time.Time) *models.Duty {
	for _, d := range models.VazecaZaduzenja(u.Duties, sad) {
		if d.Role == models.RoleOperator && d.SectorID != nil && *d.SectorID != "" {
			return &d
		}
	}
	return nil
}

// stanjeSektora je stanje obrane sektora iz trake stanja; nil kad ga nema
func stanjeSektora(stanja []service.StanjeObrane, sektor string) *service.StanjeObrane {
	for i := range stanja {
		if stanja[i].Sektor == sektor {
			return &stanja[i]
		}
	}
	return nil
}

// opisDezurstva je opis dežurstva u planu (OpisiRada[0])
var opisDezurstva = models.OpisiRada[0].Opis

// karticaMojegDezurstva: do kada osoba dežura i tko preuzima smjenu, po
// planu dežurstava; dok nitko ne dežura ili dežura netko drugi, to piše
func karticaMojegDezurstva(sektor string, st *service.StanjeObrane, plan []models.Dezurstvo, u *models.User, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kMojeDezurstvo, Naslov: "Moje dežurstvo"}
	if st == nil || st.Dnevnik == nil {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Sektor " + sektor + " je u preventivnoj obrani, pa dežurstva nema."}
		return k
	}
	k.Poveznica, k.URL = "Plan", "/dnevnici/"+st.Dnevnik.ID+"/dezurstva"
	moje, iduce := smjeneDezurstva(plan, u.ID.String(), sad)
	switch {
	case moje != nil:
		r := RedakKartice{Ikona: "clock", Glavno: "Dežurstvo do " + kadDo(moje.Do, sad), Sporedno: st.Centar + " · smjenu nakon toga plan nema"}
		if iduce != nil {
			r.Sporedno = st.Centar + " · smjenu preuzima " + iduce.UserName
		}
		k.Redovi = append(k.Redovi, r)
	case st.Dnevnik.NetkoDezura():
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "user", Glavno: "Dežura " + st.Dnevnik.DezurniIme, Sporedno: "od " + kadNaslovne(*st.Dnevnik.DezurniOd, sad)})
	default:
		k.Redovi = append(k.Redovi, RedakKartice{Ikona: "triangle-alert", Glavno: "Nitko ne dežura", Sporedno: "dežurstvo se preuzima u dnevniku COP-a", URL: "/dnevnici/" + st.Dnevnik.ID, Hitno: true})
	}
	return k
}

// smjeneDezurstva su smjena osobe koja sada traje i prva smjena drugoga
// koja počinje kad njezina završi
func smjeneDezurstva(plan []models.Dezurstvo, userID string, sad time.Time) (moje, iduce *models.Dezurstvo) {
	if moje = mojaSmjena(plan, userID, sad); moje == nil {
		return nil, nil
	}
	return moje, iducaSmjena(plan, userID, moje.Do)
}

// mojaSmjena je dežurstvo osobe u planu koje sada traje
func mojaSmjena(plan []models.Dezurstvo, userID string, sad time.Time) *models.Dezurstvo {
	for i := range plan {
		if p := &plan[i]; p.Opis == opisDezurstva && p.UserID == userID && !sad.Before(p.Od) && sad.Before(p.Do) {
			return p
		}
	}
	return nil
}

// iducaSmjena je najranije dežurstvo drugoga koje počinje oko kraja smjene
// (najviše sat ranije, jer se smjene preklapaju zbog primopredaje)
func iducaSmjena(plan []models.Dezurstvo, userID string, kraj time.Time) *models.Dezurstvo {
	var iduce *models.Dezurstvo
	for i := range plan {
		p := &plan[i]
		if p.Opis == opisDezurstva && p.UserID != userID && !p.Od.Before(kraj.Add(-time.Hour)) && (iduce == nil || p.Od.Before(iduce.Od)) {
			iduce = p
		}
	}
	return iduce
}

// kadDo: „19:00” za danas, inače „3. 11. 07:00”
func kadDo(t, sad time.Time) string {
	return strings.TrimPrefix(kadNaslovne(t, sad), "danas ")
}

// NajviseUpisaDnevnika je koliko zadnjih upisa dnevnika COP-a kartica
// pokazuje (Odluke (31. 10. 2026.))
const NajviseUpisaDnevnika = 3

// karticaDnevnikaCOP: zadnja tri upisa otvorenog dnevnika COP-a, najnoviji
// prvi, i gumb „Upiši u dnevnik”
func karticaDnevnikaCOP(st *service.StanjeObrane, upisi []models.JournalEntry, sad time.Time) KarticaNaslovne {
	k := KarticaNaslovne{ID: kDnevnikCOP, Naslov: "Dnevnik COP-a"}
	if st == nil || st.Dnevnik == nil {
		k.Prazno = &PraznaKartica{Vrsta: PraznoCeka, Tekst: "Dnevnik COP-a nije otvoren: sektor je u preventivnoj obrani."}
		return k
	}
	k.Poveznica, k.URL = "Sve", "/dnevnici/"+st.Dnevnik.ID
	k.Gumbi = []GumbNaslovne{{Tekst: "Upiši u dnevnik", URL: "/dnevnici/" + st.Dnevnik.ID + "#novi-zapis", Glavni: true}}
	for i := len(upisi) - 1; i >= 0 && len(k.Redovi) < NajviseUpisaDnevnika; i-- {
		e := upisi[i]
		if e.Voided {
			continue
		}
		kad := e.CreatedAt
		if e.HappenedAt != nil {
			kad = *e.HappenedAt
		}
		k.Redovi = append(k.Redovi, RedakKartice{Glavno: skrati(e.Text, 140), Sporedno: strings.TrimSuffix(kadNaslovne(kad, sad)+" · "+e.UserName, " · ")})
	}
	if len(k.Redovi) == 0 {
		k.Tekst = "U dnevnik danas još nema upisa."
	}
	return k
}

// karticaDezurnog slaže kartice dežurnog za njegov sektor
func (h *DashboardHandler) karticaDezurnog(r *http.Request, perms *models.UserPermissions, data *DashboardViewData, id string) KarticaNaslovne {
	sad := time.Now()
	sektor := sektorOsobe(perms, sad)
	if sektor == "" {
		return nedostupna(id, "Dužnost u sektoru je u međuvremenu istekla.")
	}
	st := stanjeSektora(data.Stanja, sektor)
	switch id {
	case kMojeDezurstvo:
		var plan []models.Dezurstvo
		if st != nil && st.Dnevnik != nil && h.izvori.Dezurstva != nil {
			plan, _ = h.izvori.Dezurstva(r.Context(), st.Dnevnik.ID) // bez plana kartica kaže samo tko dežura
		}
		return karticaMojegDezurstva(sektor, st, plan, &perms.User, sad)
	case kDnevnikCOP:
		var upisi []models.JournalEntry
		if st != nil && st.Dnevnik != nil && h.izvori.Zapisi != nil {
			upisi, _ = h.izvori.Zapisi(r.Context(), st.Dnevnik.ID) // bez upisa kartica ima samo gumb
		}
		return karticaDnevnikaCOP(st, upisi, sad)
	}
	return h.stanjeSektoraKartica(r, perms, sektor, sad)
}

// stanjeSektoraKartica: stanje obrane sektora po branjenim područjima
func (h *DashboardHandler) stanjeSektoraKartica(r *http.Request, perms *models.UserPermissions, sektor string, sad time.Time) KarticaNaslovne {
	if h.izvori.StanjaSektora == nil || h.izvori.Podrucja == nil {
		return nedostupna(kStanjeSektora, "Stanje sektora trenutačno nije dostupno.")
	}
	stanja, dionice, err := h.izvori.StanjaSektora(r.Context(), sektor)
	podrucja, err2 := h.izvori.Podrucja(sektor)
	if err != nil || err2 != nil {
		k := nedostupna(kStanjeSektora, "Stanje sektora trenutačno nije dostupno.")
		k.Naslov = "Stanje obrane · Sektor " + sektor
		return k
	}
	return karticaStanjaSektora(sektor, podrucja, stanja, dionice, h.ljudiPodrucja(r.Context(), perms, sektor, sad))
}
