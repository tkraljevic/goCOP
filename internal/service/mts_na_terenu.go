package service

import (
	"context"
	"time"

	"gocop/internal/models"
)

// dodajPodrijetlo upisuje stanjima na terenu skladište koje je zadnje izdalo
// tu vrstu na to mjesto i prvi dan dolaska toga oblika (crtež 19a, 6 · Na
// terenu). Vrsta bez izdavanja, npr. vreće ispražnjene na terenu, uzima
// skladište po vrsti, bez obzira na oblik.
func (s *MtsService) dodajPodrijetlo(ctx context.Context, stanja []models.Stanje, journalID, sektor string) error {
	dolasci, err := s.repo.DolasciNaTeren(ctx, journalID, sektor)
	if err != nil {
		return err
	}
	od := map[string]string{}
	izdalo := map[string]string{}
	for _, d := range dolasci {
		vrsta := d.Mjesto.Kljuc() + "|" + d.VrstaID
		if _, ima := od[vrsta+"|"+d.Oblik]; !ima {
			od[vrsta+"|"+d.Oblik] = d.Datum
		}
		if d.SkladisteID != "" {
			izdalo[vrsta] = d.SkladisteID
		}
	}
	nazivi := map[string]string{"": ""}
	for i := range stanja {
		st := &stanja[i]
		vrsta := st.Teren().Kljuc() + "|" + st.VrstaID
		st.Od = danIz(od[vrsta+"|"+st.Oblik])
		st.IzdaloID = izdalo[vrsta]
		st.IzdaloNaziv = s.nazivSkladista(ctx, nazivi, st.IzdaloID)
	}
	return nil
}

// nazivSkladista je naziv skladišta, pročitan jednom po pozivu stranice;
// skladište koje se ne da pročitati ostaje bez naziva, a poveznica radi
func (s *MtsService) nazivSkladista(ctx context.Context, nazivi map[string]string, id string) string {
	if n, ima := nazivi[id]; ima {
		return n
	}
	if sk, err := s.repo.GetSkladiste(ctx, id); err == nil && sk != nil {
		nazivi[id] = sk.Naziv
	} else {
		nazivi[id] = ""
	}
	return nazivi[id]
}

// danIz čita dan „2006-01-02” kao početak dana po zagrebačkom vremenu;
// nečitljiv je nulto vrijeme
func danIz(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, models.Zagreb)
	if err != nil {
		return time.Time{}
	}
	return t
}
