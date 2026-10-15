package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Dnevni list za dan koji još nije počeo (Odluke (8. 10. 2026.), pitanja 5
// i 6): smije postojati samo kao nacrt, najviše 30 dana unaprijed (kao
// zadaci); potpisuje se i predaje tek od 00:00 svog dana, po hrvatskom
// vremenu, a ovjerava tek predan i kad mu je dan počeo. Nacrt za budući dan
// nema redni broj: broj dobiva prvim spremanjem ili predajom kad njegov dan
// počne, a najkasnije kad broj dobije list nekog kasnijeg dana, i to prije
// njega, pa brojevi idu redom dana (dodijeliBroj). Brojevi koje listovi već
// imaju ne mijenjaju se.
//
// List za dan koji je prošao (pitanje 6) više se ne otvara: tko list nije
// otvorio na svoj dan, taj dan nije radio. Ni vodočuvar ni rukovoditeljev
// „Upiši” ne otvaraju list za prošli dan; list koji za taj dan već postoji
// i dalje se dopunjuje, predaje, parafira i ovjerava. Ni program ga ne
// otvara: akt o obrani i objava prijave s terena za prošli dan bez lista
// upisuju se na današnji list, uz napomenu o danu (listZaUpisPrograma), kao
// i za dan čiji je list prenesen iz ranije evidencije (BP16): takav list
// ostaje kakav jest i nikad ne dobiva redni broj.
// Pravilo provodi čvor ovog programa; list za prošli dan koji stigne
// razmjenom (npr. s čvora 0.0.34) prima se kao i dosad.

// sada je trenutak po satu servisa; testovi ga postavljaju
func (s *VodocuvarService) sada() time.Time {
	if s.sad != nil {
		return s.sad()
	}
	return time.Now()
}

// danPoceo javlja je li dan lista počeo: od 00:00 tog dana, po hrvatskom
// vremenu (pocetakDana)
func danPoceo(dan, sad time.Time) bool {
	return !pocetakDana(dan).After(sad)
}

// danProsao javlja je li dan lista prošao: od 00:00 idućeg dana, po
// hrvatskom vremenu
func danProsao(dan, sad time.Time) bool {
	return !pocetakDana(dan).AddDate(0, 0, 1).After(sad)
}

// istiDan javlja jesu li dva trenutka istog dana po hrvatskom vremenu
func istiDan(a, b time.Time) bool {
	return pocetakDana(a).Equal(pocetakDana(b))
}

// kljucDana je dan trenutka po hrvatskom vremenu, kao GGGG-MM-DD
func kljucDana(t time.Time) string {
	return t.In(models.Zagreb).Format("2006-01-02")
}

// ErrProsliDan: za dan koji je prošao nema dnevnog lista, a takav list se
// više ne otvara (Odluke (8. 10. 2026.), pitanje 6)
var ErrProsliDan = errors.New("list za dan koji je prošao više se ne otvara")

// provjeriNoviList odbija nov list za dan koji je prošao; poruka je za
// vodočuvara koji list otvara. Kao ni pomoć, ne upućuje ga da list priprema
// unaprijed.
func provjeriNoviList(dan, sad time.Time) error {
	if !danProsao(dan, sad) {
		return nil
	}
	return fmt.Errorf("za %s nema dnevnog lista, a %w: dnevni list otvara se na svoj dan", pocetakDana(dan).Format("02.01.2006."), ErrProsliDan)
}

// postojeciZaDan vraća list vodočuvara za dan kad postoji. Kad ga nema,
// vraća nil, a za dan koji je prošao grešku (provjeriNoviList), jer se nov
// list za takav dan više ne otvara.
func (s *VodocuvarService) postojeciZaDan(ctx context.Context, userID string, dan time.Time) (*models.VodocuvarskiList, error) {
	l, err := s.repo.ZaDan(ctx, userID, dan)
	if err != nil || l != nil {
		return l, err
	}
	return nil, provjeriNoviList(dan, s.sada())
}

// bezListaZaUpis je greška rukovoditeljeva upisa na dan za koji vodočuvar
// nema lista, a dan je prošao; ostale greške ostaju kakve jesu
func bezListaZaUpis(v *models.User, dan time.Time, err error) error {
	if !errors.Is(err, ErrProsliDan) {
		return err
	}
	return fmt.Errorf("%s nema dnevni list za %s, a %w, pa se ni bilješka za taj dan ne upisuje", v.FullName, pocetakDana(dan).Format("02.01.2006."), ErrProsliDan)
}

// listZaUpisPrograma je list na koji program sam upisuje (akt o obrani,
// objava prijave s terena): list tog dana, a današnji list vodočuvara kad je
// dan prošao i za njega nema lista, ili kad je list tog dana prenesen iz
// ranije evidencije (BP16), koji ostaje kakav jest. Drugi rezultat javlja da
// je list tog dana prenesen.
func (s *VodocuvarService) listZaUpisPrograma(ctx context.Context, v *models.User, dan time.Time) (*models.VodocuvarskiList, bool, error) {
	l, err := s.pripremiZa(ctx, v, dan)
	prenesen := err == nil && l.Rekonstrukcija
	if prenesen || errors.Is(err, ErrProsliDan) {
		l, err = s.pripremiZa(ctx, v, pocetakDana(s.sada()))
	}
	return l, prenesen, err
}

// najdaljiDanLista je zadnji dan za koji se list smije pripremiti unaprijed
func najdaljiDanLista(sad time.Time) time.Time {
	return pocetakDana(sad).AddDate(0, 0, models.NajdaljePlaniranje)
}

// provjeriDanLista odbija list za dan dalje od 30 dana unaprijed
func provjeriDanLista(dan, sad time.Time) error {
	najdalji := najdaljiDanLista(sad)
	if pocetakDana(dan).After(najdalji) {
		return fmt.Errorf("dnevni list za budući dan priprema se najviše %d dana unaprijed, do %s", models.NajdaljePlaniranje, najdalji.Format("02.01.2006."))
	}
	return nil
}

// DanListaPoceo javlja je li dan lista počeo, po satu servisa: list za dan
// koji još nije počeo ne ovjerava se („Ne može se ovjeriti i zaključiti
// nešto što još nije počelo.”)
func (s *VodocuvarService) DanListaPoceo(l *models.VodocuvarskiList) bool {
	return danPoceo(l.Datum, s.sada())
}

// provjeriDanOvjere odbija ovjeru lista čiji dan još nije počeo. Novi
// program takav list ne predaje, ali čvor 0.0.34 ga do izdanja 0.0.35
// predaje, pa stiže predan.
func provjeriDanOvjere(dan, sad time.Time) error {
	if !danPoceo(dan, sad) {
		return fmt.Errorf("list za %s ovjerava se tek kad taj dan počne, od 00:00", pocetakDana(dan).Format("02.01.2006."))
	}
	return nil
}

// provjeriPocetakDana odbija potpis i predaju lista čiji dan još nije počeo
func provjeriPocetakDana(dan, sad time.Time) error {
	if !danPoceo(dan, sad) {
		return fmt.Errorf("list za %s potpisuje se i predaje tek kad taj dan počne, od 00:00", pocetakDana(dan).Format("02.01.2006."))
	}
	return nil
}

// dodijeliBroj daje listu bez broja sljedeći redni broj u knjizi, ali tek
// kad je njegov dan počeo; nacrt za budući dan ostaje bez broja, a list
// prenesen iz ranije evidencije (BP16) bez broja uvijek, i kad mu
// rukovoditelj upiše bilješku, jer ne ulazi u numeraciju knjige. Nacrti
// ranijih dana iste knjige koji još nemaju broj dobivaju ga prije njega,
// redom dana (brojNacrtimaRanijihDana), pa brojevi idu redom dana i kad
// nacrt pripremljen unaprijed nitko nije spremio na njegov dan (Odluke
// (8. 10. 2026.), pitanje 6).
func (s *VodocuvarService) dodijeliBroj(ctx context.Context, l *models.VodocuvarskiList) error {
	if l.Broj != 0 || l.Rekonstrukcija || !danPoceo(l.Datum, s.sada()) {
		return nil
	}
	if err := s.brojNacrtimaRanijihDana(ctx, l); err != nil {
		return err
	}
	return s.sljedeciBroj(ctx, l)
}

// sljedeciBroj daje listu sljedeći redni broj u knjizi njegove godine
func (s *VodocuvarService) sljedeciBroj(ctx context.Context, l *models.VodocuvarskiList) error {
	broj, err := s.repo.SljedeciBroj(ctx, l.UserID, l.Datum.In(models.Zagreb).Year())
	if err != nil {
		return err
	}
	l.Broj = broj
	return nil
}

// brojNacrtimaRanijihDana daje redni broj, redom dana, nacrtima iste knjige
// (osoba i godina) bez broja čiji je dan prije dana lista l
// (nacrtiRanijihDana), i sprema ih. Brojevi koje listovi već imaju ne
// mijenjaju se.
func (s *VodocuvarService) brojNacrtimaRanijihDana(ctx context.Context, l *models.VodocuvarskiList) error {
	knjiga, err := s.repo.List(ctx, repository.FiltarListova{UserID: l.UserID, Godina: l.Datum.In(models.Zagreb).Year()})
	if err != nil {
		return err
	}
	for _, n := range nacrtiRanijihDana(knjiga, l) {
		if err := s.sljedeciBroj(ctx, &n); err != nil {
			return err
		}
		if err := s.repo.Save(ctx, &n); err != nil {
			return fmt.Errorf("redni broj nacrta od %s: %w", pocetakDana(n.Datum).Format("02.01.2006."), err)
		}
	}
	return nil
}

// nacrtiRanijihDana su, redom dana, nacrti bez broja iz knjige čiji je dan
// prije dana lista l: po jedan za dan (od dva stariji), i samo za dan na
// kojem nijedan list nema broj, nije predan ni prenesen iz ranije
// evidencije (preneseni listovi ostaju bez broja)
func nacrtiRanijihDana(knjiga []models.VodocuvarskiList, l *models.VodocuvarskiList) []models.VodocuvarskiList {
	sloziRedomDana(knjiga)
	zauzet := map[string]bool{}
	for _, x := range knjiga {
		if x.Broj != 0 || x.Predan() || x.Rekonstrukcija {
			zauzet[kljucDana(x.Datum)] = true
		}
	}
	var out []models.VodocuvarskiList
	for _, x := range knjiga {
		if zauzet[kljucDana(x.Datum)] || !pocetakDana(x.Datum).Before(pocetakDana(l.Datum)) {
			continue
		}
		zauzet[kljucDana(x.Datum)] = true
		out = append(out, x)
	}
	return out
}

// sloziRedomDana slaže listove redom dana, a listove istog dana od starijeg
func sloziRedomDana(listovi []models.VodocuvarskiList) {
	sort.SliceStable(listovi, func(i, j int) bool {
		a, b := listovi[i], listovi[j]
		if !istiDan(a.Datum, b.Datum) {
			return a.Datum.Before(b.Datum)
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}
