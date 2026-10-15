package web

import (
	"context"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Upozorenje na nepredane listove (Odluke (11. 10. 2026.)): vodočuvar na
// početnoj stranici i u svom dnevniku vidi dane čije dnevne listove nije
// potpisao i predao, a dan im je prošao, svaki s poveznicom na list
// (service.NepredaniListovi). Predložak je „nepredaniListovi” u base.html.

// najviseNepredanihDana je koliko se dana u upozorenju nabraja; za ostale
// stoji „i još N dana”
const najviseNepredanihDana = 5

// UpozorenjeNepredanih je upozorenje vodočuvaru na dnevne listove koje nije
// potpisao i predao
type UpozorenjeNepredanih struct {
	Ukupno int
	Dani   []DanNepredanog // prvih najviseNepredanihDana, redom dana
	Jos    int             // koliko ih ima iza nabrojanih
}

// DanNepredanog je jedan dan u nabrajanju, s poveznicom na list
type DanNepredanog struct {
	Prije string // ispred datuma: ništa, zarez ili „i”
	ID    string
	Dan   string // „8. 10.”
}

// upozorenjeNepredanih slaže upozorenje iz nepredanih listova, redom dana;
// nil kad ih nema
func upozorenjeNepredanih(listovi []models.VodocuvarskiList) *UpozorenjeNepredanih {
	if len(listovi) == 0 {
		return nil
	}
	nabrojani := listovi[:min(len(listovi), najviseNepredanihDana)]
	u := &UpozorenjeNepredanih{Ukupno: len(listovi), Jos: len(listovi) - len(nabrojani)}
	for i, l := range nabrojani {
		u.Dani = append(u.Dani, DanNepredanog{Prije: razdjelnikDana(i, len(nabrojani), u.Jos), ID: l.ID, Dan: l.Datum.In(models.Zagreb).Format("2. 1.")})
	}
	return u
}

// razdjelnikDana stoji ispred i-tog od n nabrojanih datuma: zarez, a ispred
// zadnjeg „i” kad iza njega ne slijedi „i još …”
func razdjelnikDana(i, n, jos int) string {
	switch {
	case i == 0:
		return ""
	case i == n-1 && jos == 0:
		return " i "
	}
	return ", "
}

// nepredaniZa je upozorenje osobi na njezine nepredane listove; nil kad ih
// nema, kad osoba ne vodi dnevnik ili kad dnevnik nije spreman. Kad se
// knjiga ne može pročitati, upozorenje izostaje, a stranica se prikazuje.
func nepredaniZa(ctx context.Context, s *service.VodocuvarService, u *models.User) *UpozorenjeNepredanih {
	if s == nil {
		return nil
	}
	listovi, _ := s.NepredaniListovi(ctx, u)
	return upozorenjeNepredanih(listovi)
}
