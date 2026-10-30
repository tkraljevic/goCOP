package repository

// Polja prava računa i dužnosti kao vrijednosti (Odluke (30. 10. 2026.),
// dopuna 16). Prava zapisa zapisuju se kao JSON stalnog oblika: tako ih
// potpisuje računalo s ključem mreže (razmjena.PotvrdaPrava.Prava), tako se
// uspoređuju (ista prava imaju isti zapis) i iz njih se slaže verzija za
// površinu s pravima koja vrijede (sPravima). Vremena dužnosti zapisuju se
// u sekundama, pa ni zona ni djelići sekunde u kojima ih je računalo
// zapisalo ne mijenjaju prava.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// pravaObrisanog su prava zapisa čija je verzija spomenik
const pravaObrisanog = `{"obrisan":true}`

// uJSON je zapis obične strukture; Marshal nad njom ne pada
func uJSON(v any) string {
	b, _ := json.Marshal(v) // samo strukture bez kanala i funkcija
	return string(b)
}

// otisakPrava je otisak zapisa prava, za tablicu prava_na_potvrdi; prazno
// kad prava nema
func otisakPrava(prava string) string {
	if prava == "" {
		return ""
	}
	h := sha256.Sum256([]byte(prava))
	return hex.EncodeToString(h[:])
}

// zivaPrava: zapis s tim pravima postoji
func zivaPrava(prava string) bool { return prava != "" && prava != pravaObrisanog }

// ---- račun ----

// pravaRacuna su polja prava računa
type pravaRacuna struct {
	Zastavica        bool   `json:"zastavica"`
	Aktivan          bool   `json:"aktivan"`
	Lozinka          string `json:"lozinka"`
	ObaveznaPromjena bool   `json:"obaveznaPromjena"`
}

func pravaKorisnika(u models.User, lozinka string) string {
	return uJSON(pravaRacuna{Zastavica: u.IsGlobalAdmin, Aktivan: u.IsActive, Lozinka: lozinka, ObaveznaPromjena: u.MustChangePassword})
}

// pravaVerzijeRacuna su prava u verziji računa; false kad se sadržaj ne da
// pročitati
func pravaVerzijeRacuna(v ledger.Version) (string, bool) {
	if v.Archived {
		return pravaObrisanog, true
	}
	var uv userVersion
	if json.Unmarshal(v.Payload, &uv) != nil {
		return "", false
	}
	return pravaKorisnika(uv.User, uv.PasswordHash), true
}

// pravaRacunaSPovrsine su prava računa na površini; false kad ga nema
func pravaRacunaSPovrsine(ctx context.Context, tx *sql.Tx, id string) (string, bool, error) {
	u, err := getUserTx(ctx, tx, id)
	if err != nil {
		return "", false, bezRetka(err)
	}
	return pravaKorisnika(u, u.PasswordHash), true, nil
}

// racunSPravima je verzija računa s poljima prava iz zapisa prava
func racunSPravima(v ledger.Version, prava string) (ledger.Version, error) {
	var uv userVersion
	var p pravaRacuna
	err := json.Unmarshal(v.Payload, &uv)
	if err == nil {
		err = json.Unmarshal([]byte(prava), &p)
	}
	if err != nil {
		return v, err
	}
	uv.IsGlobalAdmin, uv.IsActive, uv.PasswordHash, uv.MustChangePassword = p.Zastavica, p.Aktivan, p.Lozinka, p.ObaveznaPromjena
	v, _, err = sDrugimSadrzajem(v, uv)
	return v, err
}

// identitetRacuna je oznaka računa pod kojom bi primjena upisala verziju:
// spomenik briše po zapisu, a upis po identitetu iz sadržaja
func identitetRacuna(v ledger.Version) string {
	var uv userVersion
	if v.Archived || json.Unmarshal(v.Payload, &uv) != nil {
		return v.EntityID
	}
	return uv.ID.String()
}

// promjenaRacunaOd je promjena prava u verziji računa prema pravima koja
// vrijede (prazno: računa nema); ime i uprava organizacije s površine
func promjenaRacunaOd(ctx context.Context, tx *sql.Tx, id, prava string, v ledger.Version) (promjenaPrava, error) {
	c, err := citajRacun(ctx, tx, id)
	if err != nil {
		return promjenaPrava{}, err
	}
	var p pravaRacuna
	c.ima = zivaPrava(prava) && json.Unmarshal([]byte(prava), &p) == nil
	c.zastavica, c.aktivan, c.lozinka, c.obaveznaPromjena = p.Zastavica, p.Aktivan, p.Lozinka, p.ObaveznaPromjena
	return promjenaRacunaProtiv(c, v), nil
}

// promjenaRacunaProtiv je promjena prava u verziji računa prema računu c
func promjenaRacunaProtiv(c racunNaPovrsini, v ledger.Version) promjenaPrava {
	prava, _ := pravaVerzijeRacuna(v) // nečitljiv sadržaj nema prava
	if v.Archived {
		if !c.globalni() {
			return promjenaPrava{}
		}
		return promjenaPrava{racun: c.ime, promjene: []string{"briše račun globalnog administratora"}, otisak: otisakPrava(prava)}
	}
	var uv userVersion
	if json.Unmarshal(v.Payload, &uv) != nil {
		return promjenaPrava{} // primjena javi grešku sadržaja
	}
	p := promjenaPrava{racun: imeZaPotvrdu(c, uv.User), otisak: otisakPrava(prava)}
	for _, pr := range pravilaRacuna {
		if pr.vrijedi(c, uv) {
			p.promjene = append(p.promjene, pr.opis)
		}
	}
	return p
}

// ---- dužnost ----

// pravaDuznostiZapis su polja prava dužnosti; vremena u sekundama
type pravaDuznostiZapis struct {
	Racun      string  `json:"racun"`
	Uloga      string  `json:"uloga"`
	Aktivna    bool    `json:"aktivna"`
	Istek      *int64  `json:"istek"`
	Privremena bool    `json:"privremena"`
	Rok        *int64  `json:"rok"`
	SObranom   bool    `json:"sObranom"`
	OvisiO     *string `json:"ovisiO"`
}

func pravaDuznosti(d models.Duty) string {
	p := pravaDuznostiZapis{Racun: d.UserID.String(), Uloga: string(d.Role), Aktivna: d.IsActive, Istek: sekunde(d.ExpiresAt),
		Privremena: d.IsTemporary, Rok: sekunde(d.Rok), SObranom: d.IsticeSObranom}
	if d.OvisiO != nil {
		o := d.OvisiO.String()
		p.OvisiO = &o
	}
	return uJSON(p)
}

// sekunde je trenutak u sekundama; nil kad ga nema
func sekunde(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	s := t.Unix()
	return &s
}

// pravaVerzijeDuznosti su prava u verziji dužnosti; false kad se sadržaj ne
// da pročitati
func pravaVerzijeDuznosti(v ledger.Version) (string, bool) {
	if v.Archived {
		return pravaObrisanog, true
	}
	var d models.Duty
	if json.Unmarshal(v.Payload, &d) != nil {
		return "", false
	}
	return pravaDuznosti(d), true
}

// pravaDuznostiSPovrsine su prava dužnosti na površini; false kad je nema
func pravaDuznostiSPovrsine(ctx context.Context, tx *sql.Tx, id string) (string, bool, error) {
	d, err := getDutyTx(ctx, tx, id)
	if err != nil {
		return "", false, bezRetka(err)
	}
	return pravaDuznosti(d), true, nil
}

// duznostIzPrava je dužnost s poljima prava iz zapisa prava (ostala polja
// prazna); nil kad dužnosti nema
func duznostIzPrava(prava string) *models.Duty {
	var p pravaDuznostiZapis
	if !zivaPrava(prava) || json.Unmarshal([]byte(prava), &p) != nil {
		return nil
	}
	d := &models.Duty{Role: models.Role(p.Uloga), IsActive: p.Aktivna, IsTemporary: p.Privremena, IsticeSObranom: p.SObranom}
	d.UserID, _ = uuid.Parse(p.Racun) // račun koji nije oznaka ostaje prazan
	d.ExpiresAt, d.Rok = trenutak(p.Istek, nil), trenutak(p.Rok, nil)
	if p.OvisiO != nil {
		if o, err := uuid.Parse(*p.OvisiO); err == nil {
			d.OvisiO = &o
		}
	}
	return d
}

// trenutak je trenutak iz sekundi; isti trenutak iz verzije (bio) ostaje
// kakav jest, s djelićima sekunde
func trenutak(s *int64, bio *time.Time) *time.Time {
	switch {
	case s == nil:
		return nil
	case bio != nil && bio.Unix() == *s:
		return bio
	}
	t := time.Unix(*s, 0).UTC()
	return &t
}

// duznostSPravima je verzija dužnosti s poljima prava iz zapisa prava
func duznostSPravima(v ledger.Version, prava string) (ledger.Version, error) {
	var d models.Duty
	if err := json.Unmarshal(v.Payload, &d); err != nil {
		return v, err
	}
	p := duznostIzPrava(prava)
	if p == nil {
		return v, errNemaPrava
	}
	d.UserID, d.Role, d.IsActive, d.IsTemporary, d.IsticeSObranom, d.OvisiO = p.UserID, p.Role, p.IsActive, p.IsTemporary, p.IsticeSObranom, p.OvisiO
	d.ExpiresAt, d.Rok = trenutak(sekunde(p.ExpiresAt), d.ExpiresAt), trenutak(sekunde(p.Rok), d.Rok)
	v, _, err := sDrugimSadrzajem(v, d)
	return v, err
}

// identitetDuznosti je oznaka dužnosti pod kojom bi primjena upisala verziju
func identitetDuznosti(v ledger.Version) string {
	var d models.Duty
	if v.Archived || json.Unmarshal(v.Payload, &d) != nil {
		return v.EntityID
	}
	return d.ID.String()
}

// promjenaDuznostiOd je promjena prava u verziji dužnosti prema pravima koja
// vrijede (prazno: dužnosti nema): nova uprava organizacije, prenesena na
// drugi račun, s drukčijim trajanjem, ili oduzeta (ugašena, obrisana,
// istekla). Uspoređuju se polja prava, pa djelići sekunde ne broje.
func promjenaDuznostiOd(ctx context.Context, tx *sql.Tx, _ string, prava string, v ledger.Version) (promjenaPrava, error) {
	nova, citljivo := pravaVerzijeDuznosti(v)
	if !citljivo {
		return promjenaPrava{}, nil // primjena javi grešku sadržaja
	}
	d := duznostIzPrava(nova)
	if d == nil {
		d = &models.Duty{}
	}
	opis, vlasnik := opisPromjeneDuznosti(duznostIzPrava(prava), *d, v.Archived, time.Now())
	if opis == "" {
		return promjenaPrava{}, nil
	}
	racun, err := imeVlasnika(ctx, tx, vlasnik.String())
	return promjenaPrava{racun: racun, promjene: []string{opis}, otisak: otisakPrava(nova)}, err
}
