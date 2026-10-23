package peers

// Obavijest e-poštom o roku članstva i ovlasti (G9b; Odluke (18. 10. 2026.)
// i (23. 10. 2026.)): računalo koje prima javi globalnim administratorima
// članstvo ili ovlast kojoj ostaje manje od 30 dana, i članstvo koje je
// isteklo i u pričuvi je, ili ovlast koja je istekla. Šalje i računalo
// kojem je vlastita ovlast istekla, jer tada je obavijest najpotrebnija.
//
// Svaka obavijest ide svakom administratoru jednom po računalu, vrsti,
// stanju i roku; obnovljeno članstvo ima novi rok, pa i novu obavijest kad
// joj dođe vrijeme. Zapis o poslanoj obavijesti ide u knjigu
// (obavijesti_roka, bez tablice), pa je drugo računalo koje prima ne šalje
// ponovno, a ni računalo koje tek počne primati. Šalje je program
// (cmd/gocop), računom za slanje PIN-a, ako je podešen.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"gocop/internal/models"
)

// EntityObavijestiRoka je zapis da je obavijest o roku poslana jednom
// administratoru; samo u knjizi
const EntityObavijestiRoka = "obavijesti_roka"

// Vrste obavijesti za e-poštu administratorima
const (
	VrstaObavijestiClanstvo = "clanstvo" // rok članstva
	VrstaObavijestiOvlast   = "ovlast"   // rok ovlasti za primanje
	VrstaObavijestiKljuc    = "kljuc"    // spremljena ili učitana kopija ključa mreže
)

// ObavijestRoka je jedna obavijest za e-poštu administratorima: rok
// članstva ili ovlasti, ili događaj s kopijom ključa mreže
type ObavijestRoka struct {
	// Kljuc je računalo, vrsta, stanje i rok (za ključ: oznaka događaja):
	// obavijest ide svakom administratoru jednom
	Kljuc    string
	Racunalo string // ime računala
	Vrsta    string // VrstaObavijesti*
	Rok      Rok    // za članstvo i ovlast
	// Izvor je oznaka računala na kojem se dogodilo (za ključ), a Tekst
	// redak obavijesti o događaju
	Izvor, Tekst string
}

// poslanaObavijest je zapis u knjizi da je obavijest kljuc poslana
// administratoru primatelj
type poslanaObavijest struct {
	Kljuc     string    `json:"kljuc"`
	Primatelj string    `json:"primatelj"`
	Poslao    string    `json:"poslao"`
	Kad       time.Time `json:"kad"`
}

// ObavijestiRoka su rokovi koji traže pažnju, na računalu koje šalje
// obavijesti: drži ključ mreže ili ima ovlast za primanje, i kad je istekla
func (s *Service) ObavijestiRoka(ctx context.Context) []ObavijestRoka {
	n := s.NetworkInfo()
	if n == nil || (!n.DrziKljuc && s.ovlastZaObnovu(ctx, s.node.ID, s.node.PublicKey()) == nil) {
		return nil
	}
	return append(s.rokoviClanstava(ctx), s.rokoviOvlasti(ctx, n.DrziKljuc)...)
}

// NoveObavijestiRoka su od obavijesti one koje administrator primatelj
// (oznaka korisnika) još nije dobio, ni s drugog računala
func (s *Service) NoveObavijestiRoka(ctx context.Context, primatelj string, obavijesti []ObavijestRoka) []ObavijestRoka {
	var out []ObavijestRoka
	for _, o := range obavijesti {
		if !s.poslana(ctx, o.Kljuc, primatelj) {
			out = append(out, o)
		}
	}
	return out
}

// obavijest je obavijest vrste vrsta o roku r računala id
func (s *Service) obavijest(ctx context.Context, id, vrsta string, r Rok) ObavijestRoka {
	return ObavijestRoka{Kljuc: fmt.Sprintf("%s|%s|%s|%d", id, vrsta, r.Stanje, r.Do.Unix()), Racunalo: s.imeRacunala(ctx, id), Vrsta: vrsta, Rok: r}
}

// rokoviClanstava: valjana članstva (i vlastito) koja ističu ili su u pričuvi
func (s *Service) rokoviClanstava(ctx context.Context) []ObavijestRoka {
	clanovi, err := s.ListMembers(ctx)
	if err != nil {
		return nil
	}
	var out []ObavijestRoka
	for _, m := range clanovi {
		if m.Valid && m.Rok.TraziPaznju() {
			out = append(out, s.obavijest(ctx, m.DeviceID, VrstaObavijestiClanstvo, m.Rok))
		}
	}
	return out
}

// rokoviOvlasti: neopozvane ovlasti s potpisom ključa mreže koje ističu ili
// su istekle. Vlastita ovlast ne ide kad ovo računalo drži ključ mreže: ono
// prima i bez nje.
func (s *Service) rokoviOvlasti(ctx context.Context, kljucMreze bool) []ObavijestRoka {
	ovlasti, err := s.sveOvlasti(ctx)
	if err != nil {
		return nil
	}
	sada := time.Now()
	var out []ObavijestRoka
	for _, o := range ovlasti {
		r := rokZa(o.ExpiresAt, sada, false)
		switch {
		case !r.TraziPaznju() || (kljucMreze && o.DeviceID == s.node.ID):
		case s.ovlastZaObnovu(ctx, o.DeviceID, o.DeviceKey) != nil:
			out = append(out, s.obavijest(ctx, o.DeviceID, VrstaObavijestiOvlast, r))
		}
	}
	return out
}

// oznakaPoslane je oznaka zapisa poslane obavijesti u knjizi
func oznakaPoslane(kljuc, primatelj string) string { return kljuc + "|" + primatelj }

// poslana javlja je li obavijest kljuc već poslana administratoru
// primatelj; kad se knjiga ne da pročitati, kao da jest, da e-pošta ne ide
// svaki sat
func (s *Service) poslana(ctx context.Context, kljuc, primatelj string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions WHERE entity = ? AND entity_id = ?`, EntityObavijestiRoka, oznakaPoslane(kljuc, primatelj)).Scan(&n)
	return err != nil || n > 0
}

// ZabiljeziObavijestiRoka upiše u knjigu da je administrator primatelj
// dobio obavijesti
func (s *Service) ZabiljeziObavijestiRoka(ctx context.Context, primatelj string, obavijesti []ObavijestRoka) {
	if err := s.uTransakciji(ctx, func(tx *sql.Tx) error {
		for _, o := range obavijesti {
			z := poslanaObavijest{Kljuc: o.Kljuc, Primatelj: primatelj, Poslao: s.node.ID, Kad: time.Now().UTC().Truncate(time.Second)}
			if _, err := s.rec.Record(ctx, tx, EntityObavijestiRoka, oznakaPoslane(o.Kljuc, primatelj), z); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Printf("razmjena: poslana obavijest o roku nije zapamćena: %v", err)
	}
}

// TekstObavijesti je predmet i tekst e-pošte o obavijestima iste skupine
// (Skupine); racunalo je ime računala koje šalje
func TekstObavijesti(obavijesti []ObavijestRoka, racunalo string) (predmet, tekst string) {
	if len(obavijesti) > 0 && obavijesti[0].Vrsta == VrstaObavijestiKljuc {
		return tekstObavijestiKljuca(obavijesti, racunalo)
	}
	var b strings.Builder
	b.WriteString("Članstvo ili ovlast nekih računala u goCOP mreži traži pažnju:\n\n")
	for _, o := range obavijesti {
		b.WriteString("- " + redakObavijesti(o) + "\n")
	}
	b.WriteString("\nObnova se traži sama kad računalo razmjenjuje podatke. Računalo koje prima u mrežu može članstvo obnoviti odmah: Postavke, pod Mreža, gumb Obnovi uz računalo. Ovlast za primanje obnavlja računalo s ključem mreže.\n\n")
	b.WriteString("Poruku je poslalo računalo " + racunalo + ".\n")
	return "goCOP: rok članstva u mreži", b.String()
}

// Skupine dijele obavijesti na rokove i događaje s ključem mreže, svaku
// skupinu za svoju poruku; prazne skupine nema
func Skupine(obavijesti []ObavijestRoka) [][]ObavijestRoka {
	var rokovi, kljuc []ObavijestRoka
	for _, o := range obavijesti {
		if o.Vrsta == VrstaObavijestiKljuc {
			kljuc = append(kljuc, o)
		} else {
			rokovi = append(rokovi, o)
		}
	}
	var out [][]ObavijestRoka
	for _, sk := range [][]ObavijestRoka{rokovi, kljuc} {
		if len(sk) > 0 {
			out = append(out, sk)
		}
	}
	return out
}

// redakObavijesti je jedan rok riječima
func redakObavijesti(o ObavijestRoka) string {
	d := danaDo(o.Rok.Do, time.Now())
	za := fmt.Sprintf("(za %d %s)", d, models.UzBrojHR(d, "dan", "dana", "dana"))
	ovlast := o.Vrsta == VrstaObavijestiOvlast
	switch {
	case ovlast && o.Rok.Stanje == RokIstjece:
		return fmt.Sprintf("%s: ovlast za primanje ističe %s %s.", o.Racunalo, datumHR(o.Rok.Do), za)
	case ovlast:
		return fmt.Sprintf("%s: ovlast za primanje istekla je %s; ako to računalo nema ključ mreže, više ne prima i ne obnavlja druga računala.", o.Racunalo, datumHR(o.Rok.Do))
	case o.Rok.Stanje == RokIstjece:
		return fmt.Sprintf("%s: članstvo u mreži ističe %s %s.", o.Racunalo, datumHR(o.Rok.Do), za)
	}
	return fmt.Sprintf("%s: članstvo u mreži isteklo je %s Razmjena radi još do %s, a onda računalo treba ponovno primiti u mrežu.", o.Racunalo, datumHR(o.Rok.Do), datumHR(o.Rok.Do.Add(PricuvniRok)))
}
