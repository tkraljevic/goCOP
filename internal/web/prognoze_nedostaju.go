package web

import (
	"cmp"
	"context"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Traka na vrhu stranice Prognoze: kojih podataka na ovom računalu nema i
// napomena administratora. Popis svako računalo računa samo iz svojih očitanja.

const (
	// NedostajeNakon je koliko staro zadnje preuzeto mjerenje letve smije biti prije
	// nego što uđe na popis. Javni izvori redovito kasne sat-dva i znaju
	// preskočiti koji sat, pa bi manji prag stalno javljao lažnu uzbunu.
	NedostajeNakon = 6 * time.Hour
	// NedostajeUnatrag je koliko se daleko traži zadnje mjerenje. Letva koja
	// toliko dugo nema mjerenja te veličine više se ne navodi, bez obzira na
	// razlog: nema objavljen protok, ovo računalo je ne prati ili je izvor stao
	// prije više od toga. Zato to piše i u pomoći.
	NedostajeUnatrag = 14 * 24 * time.Hour
	// PamtiNedostaje je koliko dugo stranica pamti izračunati popis, da se
	// ne čita iz očitanja pri svakom otvaranju.
	PamtiNedostaje = 5 * time.Minute
	// nedostajeOdmah je koliko letvi jedne veličine traka pokazuje odmah;
	// ostale su pod „i još N”.
	nedostajeOdmah = 8
)

// RedakNedostaju je jedna veličina (vodostaj ili protok) u traci.
type RedakNedostaju struct {
	Velicina string // „Vodostaj” ili „Protok”
	Tekst    string // letve koje se vide odmah
	Jos      int    // koliko je ostalih letvi
	Ostatak  string // ostale letve, sklopljene
}

// zapamcenoNedostaju je zadnji izračunati popis i kada je složen.
type zapamcenoNedostaju struct {
	mu    sync.Mutex
	kad   time.Time
	retci []RedakNedostaju
}

// letvaBezPodataka je letva na popisu i zadnje mjerenje veličine.
type letvaBezPodataka struct {
	naziv  string
	zadnje time.Time
}

// kasni javlja je li zadnje mjerenje preko praga. Izvor koji se javlja rjeđe od
// praga (npr. dvaput na dan) nije u kvaru samo zato što se još nije javio, pa
// se njemu čeka dvostruki njegov uobičajeni razmak, ali najviše pola razdoblja
// koje se gleda, da letva koja stane uđe na popis prije nego što ispadne iz njega.
func kasni(m repository.MjerenjaVelicine, sad time.Time) bool {
	if m.Broj == 0 || m.Zadnje.IsZero() {
		return false
	}
	prag := NedostajeNakon
	if m.Broj > 1 {
		prag = max(prag, min(2*m.Zadnje.Sub(m.Prvo)/time.Duration(m.Broj-1), NedostajeUnatrag/2))
	}
	return sad.Sub(m.Zadnje) > prag
}

// tekstLetvi piše letve po satu zadnjeg mjerenja, npr. „Botovo, Ludbreg (od
// 6. 11. u 14 h); Krkanec (od 6. 11. u 9 h)”. Očekuje letve složene po vremenu.
// Zagrada se ne prelama, da sat ne ostane u novom retku bez dana.
func tekstLetvi(letve []letvaBezPodataka) string {
	var skupine []string
	var nazivi []string
	for i, l := range letve {
		nazivi = append(nazivi, l.naziv)
		sat := satUpozorenja(l.zadnje)
		if i == len(letve)-1 || satUpozorenja(letve[i+1].zadnje) != sat {
			skupine = append(skupine, strings.Join(nazivi, ", ")+" "+strings.ReplaceAll("(od "+sat+")", " ", "\u00a0"))
			nazivi = nil
		}
	}
	return strings.Join(skupine, "; ")
}

// redakVelicine slaže redak jedne veličine; nil kad nijedna letva ne kasni.
// Najsvježiji prekidi idu prvi: stari su već poznati.
func redakVelicine(naziv string, letve []repository.MjerenjaLetve, velicina func(repository.MjerenjaLetve) repository.MjerenjaVelicine, sad time.Time) *RedakNedostaju {
	var bez []letvaBezPodataka
	for _, l := range letve {
		if m := velicina(l); kasni(m, sad) {
			ime, _ := imeIDrzava(l.Naziv)
			bez = append(bez, letvaBezPodataka{naziv: ime, zadnje: m.Zadnje})
		}
	}
	if len(bez) == 0 {
		return nil
	}
	slices.SortStableFunc(bez, func(a, b letvaBezPodataka) int {
		return cmp.Compare(b.zadnje.Unix()/3600, a.zadnje.Unix()/3600)
	})
	odmah := min(len(bez), nedostajeOdmah)
	return &RedakNedostaju{Velicina: naziv, Tekst: tekstLetvi(bez[:odmah]), Jos: len(bez) - odmah, Ostatak: tekstLetvi(bez[odmah:])}
}

// retciNedostaju su retci trake za zadana mjerenja: vodostaj pa protok.
func retciNedostaju(letve []repository.MjerenjaLetve, sad time.Time) []RedakNedostaju {
	var out []RedakNedostaju
	if r := redakVelicine("Vodostaj", letve, func(l repository.MjerenjaLetve) repository.MjerenjaVelicine { return l.Vodostaj }, sad); r != nil {
		out = append(out, *r)
	}
	if r := redakVelicine("Protok", letve, func(l repository.MjerenjaLetve) repository.MjerenjaVelicine { return l.Protok }, sad); r != nil {
		out = append(out, *r)
	}
	return out
}

// nedostajuPodaci je popis za traku, zapamćen PamtiNedostaje. Kad se očitanja
// ne daju pročitati, popisa nema, a stranica radi i bez njega.
func (h *PrognozeHandler) nedostajuPodaci(ctx context.Context) []RedakNedostaju {
	if h.readings == nil {
		return nil
	}
	p := &h.nedostaju
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.kad.IsZero() && time.Since(p.kad) < PamtiNedostaje {
		return p.retci
	}
	sad := time.Now()
	letve, err := h.readings.MjerenjaPreuzimanih(ctx, sad.Add(-NedostajeUnatrag), sad)
	if err != nil {
		log.Printf("Prognoze: popis podataka koji ne stižu nije izračunat: %v", err)
		return nil
	}
	p.retci, p.kad = retciNedostaju(letve, sad), sad
	return p.retci
}

// trakaPodataka puni popis i napomenu za pregled; izvoz ih nema.
func (h *PrognozeHandler) trakaPodataka(ctx context.Context, data *PrognozePageData) {
	data.Nedostaju = h.nedostajuPodaci(ctx)
	if h.napomena != nil {
		data.NapomenaPodataka = h.napomena(ctx)
	}
}

// SetNapomena daje čitanje i spremanje napomene o podacima.
func (h *PrognozeHandler) SetNapomena(citaj func(ctx context.Context) string,
	spremi func(ctx context.Context, p *models.UserPermissions, tekst string) error) {
	h.napomena, h.spremiNapomenu = citaj, spremi
}

// SpremiNapomenu sprema ili briše napomenu o podacima (Postavke prognoze).
func (h *PrognozeHandler) SpremiNapomenu(w http.ResponseWriter, r *http.Request) {
	const natrag = "/prognoze/postavke#napomena"
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if h.spremiNapomenu == nil || perms == nil || !perms.IsGlobalAdmin {
		redirectWith(w, r, natrag, "error", "Napomenu upisuje globalni administrator.")
		return
	}
	tekst := r.FormValue("napomena")
	if r.FormValue("obrisi") != "" {
		tekst = ""
	}
	if err := h.spremiNapomenu(r.Context(), perms, tekst); err != nil {
		redirectWith(w, r, natrag, "error", err.Error())
		return
	}
	poruka := "Napomena je spremljena i vidi se na stranici Prognoze."
	if strings.TrimSpace(tekst) == "" {
		poruka = "Napomena je obrisana."
	}
	redirectWith(w, r, natrag, "success", poruka)
}
