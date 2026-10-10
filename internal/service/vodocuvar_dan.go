package service

import (
	"context"
	"fmt"
	"time"

	"gocop/internal/models"
)

// Dnevni list za dan koji još nije počeo (Odluke (8. 10. 2026.), pitanja 5
// i 6): smije postojati samo kao nacrt, najviše 30 dana unaprijed (kao
// zadaci); potpisuje se i predaje tek od 00:00 svog dana, po hrvatskom
// vremenu, a ovjerava tek predan i kad mu je dan počeo. Nacrt za budući dan
// nema redni broj: broj dobiva prvim spremanjem ili predajom kad njegov dan
// počne. Za tekući i prošli dan broj se dodjeljuje kao i dosad (pitanje 6
// o redu brojeva je otvoreno), pa brojevi u knjizi ne idu nužno redom dana.

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
// kad je njegov dan počeo; nacrt za budući dan ostaje bez broja
func (s *VodocuvarService) dodijeliBroj(ctx context.Context, l *models.VodocuvarskiList) error {
	if l.Broj != 0 || !danPoceo(l.Datum, s.sada()) {
		return nil
	}
	broj, err := s.repo.SljedeciBroj(ctx, l.UserID, l.Datum.In(models.Zagreb).Year())
	if err != nil {
		return err
	}
	l.Broj = broj
	return nil
}
