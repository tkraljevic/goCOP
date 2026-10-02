package peers

import (
	"context"
	"errors"
)

// Uloge su poslovi koje jedno računalo radi za cijelu mrežu. Vodostaje s
// vodostaji.voda.hr, mađarskih, austrijskih i ostalih izvora dovoljno je da
// preuzima jedan čvor: očitanja putuju razmjenom kao i sva druga. Prognozu
// izdaje jedan čvor, a izdanje stiže ostalima razmjenom; inače dva računala
// izdaju dvije malo različite prognoze za isti sat i stranice izvora dobivaju
// isti upit od svakog čvora. Uloge su lokalne: ne putuju razmjenom.
type Uloge struct {
	Preuzima bool // preuzima vodostaje s izvora
	Izdaje   bool // izdaje prognozu: tuđe prognoze, kiša, izračun
}

const (
	ulogaPreuzima = "preuzima"
	ulogaIzdaje   = "izdaje"
)

// UcitajUloge čita uloge iz baze u memoriju; postavljeno je false dok ih
// nitko nije zapisao (novi čvor ili baza od prije uloga).
func (s *Service) UcitajUloge(ctx context.Context) (Uloge, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kljuc, vrijednost FROM uloge_cvora`)
	if err != nil {
		return Uloge{}, false, err
	}
	defer rows.Close()
	var u Uloge
	postavljeno := false
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Uloge{}, false, err
		}
		postavljeno = true
		switch k {
		case ulogaPreuzima:
			u.Preuzima = v == "1"
		case ulogaIzdaje:
			u.Izdaje = v == "1"
		}
	}
	if err := rows.Err(); err != nil {
		return Uloge{}, false, err
	}
	s.uloge.Store(&u)
	return u, postavljeno, nil
}

// PostaviUloge zapisuje uloge i odmah ih primjenjuje: sljedeći satni krug
// već radi po njima.
func (s *Service) PostaviUloge(ctx context.Context, u Uloge) error {
	if s.db == nil {
		return errors.New("baza nije otvorena")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range map[string]bool{ulogaPreuzima: u.Preuzima, ulogaIzdaje: u.Izdaje} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO uloge_cvora (kljuc, vrijednost) VALUES (?, ?)
			ON CONFLICT(kljuc) DO UPDATE SET vrijednost = excluded.vrijednost`, k, map[bool]string{true: "1", false: "0"}[v]); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.uloge.Store(&u)
	if f := s.naPromjenuUloga; f != nil {
		go f(u)
	}
	return nil
}

// NaPromjenuUloga postavlja što se radi kad se uloge promijene (npr. upis
// primljenih izdanja kad čvor prestane izdavati prognozu).
func (s *Service) NaPromjenuUloga(f func(Uloge)) { s.naPromjenuUloga = f }

// TrenutneUloge vraća uloge iz memorije; prije učitavanja ništa.
func (s *Service) TrenutneUloge() Uloge {
	if u := s.uloge.Load(); u != nil {
		return *u
	}
	return Uloge{}
}

// OsvjeziSebe objavljuje zapis ovog čvora kad mu se naziv promijenio (npr.
// upisan je u gocop.toml nakon uparivanja). Drugi čvorovi naziv inače znaju
// samo iz uparivanja, pa bi zauvijek pamtili ime računala.
func (s *Service) OsvjeziSebe(ctx context.Context) error {
	if s.node.Name == "" {
		return nil
	}
	stored, err := s.GetPeer(ctx, s.node.ID)
	if err != nil {
		return err
	}
	// Bez zatečenog zapisa nema što ispraviti (zapis nastaje pri uparivanju),
	// a novi bi s praznim adresama pregazio javne adrese koje su drugi upisali.
	if stored == nil || stored.Name == s.node.Name {
		return nil
	}
	stored.Name = s.node.Name
	return s.SavePeer(ctx, *stored)
}
