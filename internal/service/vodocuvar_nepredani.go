package service

import (
	"context"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Nepredani listovi (Odluke (11. 10. 2026.)): „ako vodočuvar nije potpisao
// neki list i predao ga dobije upozorenje”. Upozorenje dobiva sam
// vodočuvar, za svoje listove, na početnoj stranici i u svom dnevniku. List
// ulazi kad mu je dan prošao (od 00:00 idućeg dana, po hrvatskom vremenu), a
// još nije predan; to vrijedi i za nacrt otvoren unaprijed kad mu dan prođe.
// Ne ulaze nacrti za danas i za buduće dane, predani listovi ni listovi
// preneseni iz ranije evidencije (BP16), a ni knjiga arhivirana istekom
// godine: u nju se više ne upisuje, pa se ni list iz nje ne može predati.
// Ne ulazi ni list koji je otvorio samo program (akt, upis rukovoditelja),
// a vodočuvar na njemu nije ništa upisao (vodocuvarPisao).

// SetSat daje servisu sat po kojem se zna je li dan lista počeo ili prošao;
// nil je time.Now. Služi testovima drugih paketa.
func (s *VodocuvarService) SetSat(f func() time.Time) { s.sad = f }

// NepredaniListovi vraća, redom dana, listove osobe koje nije potpisala i
// predala, a dan im je prošao (nepredaniListovi), iz knjige tekuće godine
// po satu servisa. Tko ne vodi dnevnik nema ni lista koji bi mogao predati.
func (s *VodocuvarService) NepredaniListovi(ctx context.Context, u *models.User) ([]models.VodocuvarskiList, error) {
	if !VodiDnevnik(u) {
		return nil, nil
	}
	sad := s.sada()
	knjiga, err := s.repo.List(ctx, repository.FiltarListova{UserID: u.ID.String(), Godina: sad.In(models.Zagreb).Year()})
	if err != nil {
		return nil, err
	}
	return nepredaniListovi(knjiga, sad), nil
}

// nepredaniListovi su, redom dana, nacrti iz knjige čiji je dan prošao: po
// jedan za dan, onaj koji uzimaju i „Današnji list” i spremanje (od dva
// stariji), i samo za dan na kojem nijedan list nije predan ni prenesen iz
// ranije evidencije
func nepredaniListovi(knjiga []models.VodocuvarskiList, sad time.Time) []models.VodocuvarskiList {
	sloziRedomDana(knjiga)
	zakljucen := map[string]bool{}
	for _, x := range knjiga {
		if x.Zakljucen() {
			zakljucen[kljucDana(x.Datum)] = true
		}
	}
	var out []models.VodocuvarskiList
	for _, x := range knjiga {
		if zakljucen[kljucDana(x.Datum)] || !danProsao(x.Datum, sad) || !vodocuvarPisao(x) {
			continue
		}
		zakljucen[kljucDana(x.Datum)] = true
		out = append(out, x)
	}
	return out
}

// vodocuvarPisao javlja je li vodočuvar na listu nešto upisao sam: spremio
// svoj obrazac (NapisanoAt), upisao prilike, naredbe, opis ili zapažanja
// (listovi spremljeni prije 0.0.35 nemaju NapisanoAt) ili na njemu ima
// svoju objavljenu prijavu. List koji je otvorio samo program — upis
// ovjerenog akta ili „Upiši” rukovoditelja — ne upozorava: vodočuvar koji
// taj dan nije radio list nije ni otvorio (Odluke (11. 10. 2026.)). Radno
// vrijeme ne govori ništa, jer ga program upiše u svaki novi list.
func vodocuvarPisao(l models.VodocuvarskiList) bool {
	return l.NapisanoAt != nil || l.Prilike != "" || l.Naredbe != "" || l.Opis != "" ||
		l.Zapazanja != "" || len(l.Prijave) > 0
}

// NepredaniUSektoru vraća nepredane listove svih vodočuvara sektora koje
// osoba smije čitati, po istom pravilu kao NepredaniListovi za jednu osobu
func (s *VodocuvarService) NepredaniUSektoru(ctx context.Context, perms *models.UserPermissions, sektor string) ([]models.VodocuvarskiList, error) {
	sad := s.sada()
	svi, err := s.Tudji(ctx, perms, repository.FiltarListova{Sektor: sektor, Godina: sad.In(models.Zagreb).Year()})
	if err != nil {
		return nil, err
	}
	poOsobi := map[string][]models.VodocuvarskiList{}
	for _, l := range svi {
		poOsobi[l.UserID] = append(poOsobi[l.UserID], l)
	}
	var out []models.VodocuvarskiList
	for _, knjiga := range poOsobi {
		out = append(out, nepredaniListovi(knjiga, sad)...)
	}
	return out, nil
}

// DanasnjiList je današnji list osobe kako je spremljen, bez stvaranja
// novog lista i bez dohvata vremena; nil kad ga danas još nema ili osoba ne
// vodi dnevnik
func (s *VodocuvarService) DanasnjiList(ctx context.Context, u *models.User) (*models.VodocuvarskiList, error) {
	if !VodiDnevnik(u) {
		return nil, nil
	}
	sad := s.sada().In(models.Zagreb)
	return s.repo.ZaDan(ctx, u.ID.String(), time.Date(sad.Year(), sad.Month(), sad.Day(), 0, 0, 0, 0, models.Zagreb))
}
