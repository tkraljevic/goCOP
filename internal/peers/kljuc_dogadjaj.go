package peers

// Događaj s kopijom ključa mreže i red slanja obavijesti (G9b).
//
// Ključ mreže se ne da opozvati, pa administratori moraju saznati za svaku
// spremljenu ili učitanu kopiju, i kad računalo na kojem se to dogodilo
// nema račun za slanje PIN-a. Događaj se zato upiše u knjigu
// (dogadjaji_kljuca, bez tablice), a e-poštu pošalje i drugo računalo, kao
// i obavijest o roku.
//
// Da administratori ne dobiju istu poruku od više računala, prvo šalje
// računalo kojem je to posao (za rok računalo s ključem mreže, za kopiju
// ključa računalo na kojem se dogodila), a ostala čekaju po stalnom redu,
// dok zapis o poslanoj obavijesti ne stigne razmjenom (CekanjeObavijesti).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// EntityDogadjajiKljuca je spremljena ili učitana kopija ključa mreže; samo
// u knjizi
const EntityDogadjajiKljuca = "dogadjaji_kljuca"

const (
	// danaDogadjajaKljuca je koliko dana se događaj s ključem još javlja
	danaDogadjajaKljuca = 30
	// cekanjeRoka i cekanjeKljuca su koliko svako drugo računalo po redu
	// čeka prije nego što samo pošalje obavijest; korakKljucaMreze je
	// razmak među računalima s ključem mreže (ima ih više kad je kopija
	// ključa učitana), dulji od razmjene, pa zapis prvoga stigne do drugih
	cekanjeRoka      = 6 * time.Hour
	cekanjeKljuca    = time.Hour
	korakKljucaMreze = 15 * time.Minute
)

// dogadjajKljuca je zapis u knjizi: tko je, gdje, kad i odakle spremio ili
// učitao kopiju ključa mreže
type dogadjajKljuca struct {
	ID       string    `json:"id"`
	Racunalo string    `json:"racunalo"`
	Tko      string    `json:"tko"`
	Odakle   string    `json:"odakle"`
	Sto      string    `json:"sto"`
	Kad      time.Time `json:"kad"`
}

// ZabiljeziDogadjajKljuca upiše u knjigu da je tko s adrese odakle na ovom
// računalu napravio sto (npr. „spremio je kopiju ključa mreže”) i potakne
// slanje obavijesti (KadNovaObavijest)
func (s *Service) ZabiljeziDogadjajKljuca(ctx context.Context, tko, odakle, sto string) error {
	d := dogadjajKljuca{ID: uuid.NewString(), Racunalo: s.node.ID, Tko: tko, Odakle: odakle, Sto: sto, Kad: time.Now().UTC().Truncate(time.Second)}
	if err := s.uTransakciji(ctx, func(tx *sql.Tx) error {
		_, err := s.rec.Record(ctx, tx, EntityDogadjajiKljuca, d.ID, d)
		return err
	}); err != nil {
		return err
	}
	if s.novaObavijest != nil {
		s.novaObavijest()
	}
	return nil
}

// KadNovaObavijest postavlja što se radi kad nastane obavijest koju treba
// poslati odmah (program potakne slanje obavijesti)
func (s *Service) KadNovaObavijest(f func()) { s.novaObavijest = f }

// obavijestKljuca je obavijest o događaju d
func (s *Service) obavijestKljuca(ctx context.Context, d dogadjajKljuca) ObavijestRoka {
	ime := s.imeRacunala(ctx, d.Racunalo)
	return ObavijestRoka{Kljuc: VrstaObavijestiKljuc + "|" + d.ID, Racunalo: ime, Vrsta: VrstaObavijestiKljuc, Izvor: d.Racunalo,
		Tekst: fmt.Sprintf("Na računalu %s %s %s %s, s adrese %s.", ime, d.Tko, d.Sto, d.Kad.Local().Format("02.01.2006. u 15:04"), d.Odakle)}
}

// ObavijestiKljuca su događaji s kopijom ključa mreže iz zadnjih 30 dana,
// iz knjige; zapis koji se ne da pročitati se preskače
func (s *Service) ObavijestiKljuca(ctx context.Context) []ObavijestRoka {
	if s.NetworkInfo() == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM record_versions WHERE entity = ? AND archived = 0 ORDER BY version_id`, EntityDogadjajiKljuca)
	if err != nil {
		return nil
	}
	defer rows.Close()
	od := time.Now().AddDate(0, 0, -danaDogadjajaKljuca)
	var out []ObavijestRoka
	for rows.Next() {
		if d, ok := procitajDogadjaj(rows); ok && d.Kad.After(od) {
			out = append(out, s.obavijestKljuca(ctx, d))
		}
	}
	return out
}

// procitajDogadjaj čita jedan događaj s ključem iz retka knjige
func procitajDogadjaj(rows *sql.Rows) (dogadjajKljuca, bool) {
	var payload string
	var d dogadjajKljuca
	ok := rows.Scan(&payload) == nil && json.Unmarshal([]byte(payload), &d) == nil && d.ID != ""
	return d, ok
}

// tekstObavijestiKljuca je predmet i tekst e-pošte o događajima s ključem
func tekstObavijestiKljuca(obavijesti []ObavijestRoka, racunalo string) (predmet, tekst string) {
	t := "S kopijom ključa mreže u goCOP mreži dogodilo se:\n\n"
	for _, o := range obavijesti {
		t += "- " + o.Tekst + "\n"
	}
	t += "\nAko to niste bili vi ni netko koga znate, odmah javite administratoru: tko ima kopiju ključa mreže i njezinu lozinku, ima ključ mreže, a on se ne da opozvati.\n\n"
	t += "Poruku je poslalo računalo " + racunalo + ".\n"
	return "goCOP: kopija ključa mreže", t
}

// CekanjeObavijesti je koliko ovo računalo čeka od prvog viđenja obavijesti
// prije nego što je samo pošalje. Računalo kojem je to posao šalje prvo: za
// kopiju ključa ono na kojem se dogodila, odmah, a za rok računalo s
// ključem mreže, a kad ih je više, svako za korakKljucaMreze po redu
// oznaka. Ostala čekaju po redu oznaka računala, svako jedan razmak više.
func (s *Service) CekanjeObavijesti(ctx context.Context, o ObavijestRoka) time.Duration {
	red := time.Duration(s.redRacunala(ctx))
	if o.Vrsta == VrstaObavijestiKljuc {
		if o.Izvor == s.node.ID {
			return 0
		}
		return cekanjeKljuca * (1 + red)
	}
	if n := s.NetworkInfo(); n != nil && n.DrziKljuc {
		return korakKljucaMreze * red
	}
	return cekanjeRoka * (1 + red)
}

// redRacunala je mjesto ovog računala među svim poznatim računalima, po
// oznaci; isto na svakom računalu koje zna ista računala
func (s *Service) redRacunala(ctx context.Context) int {
	ids := []string{s.node.ID}
	if list, err := s.ListPeers(ctx); err == nil {
		for _, p := range list {
			ids = append(ids, p.NodeID)
		}
	}
	slices.Sort(ids)
	return slices.Index(slices.Compact(ids), s.node.ID)
}
