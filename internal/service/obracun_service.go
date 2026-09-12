package service

import (
	"context"
	"errors"
	"strings"

	"gocop/internal/obracun"
	"gocop/internal/repository"
)

// ObracunService daje obračunu ono što organizacija sama određuje — blagdane
// i koeficijente — i pušta administratora da to uređuje. Bez baze (u testu)
// vrijedi ono što program nosi u sebi.
type ObracunService struct {
	repo *repository.ObracunRepository
}

func NewObracunService(repo *repository.ObracunRepository) *ObracunService {
	return &ObracunService{repo: repo}
}

// Kalendar vraća blagdane iz baze; bez baze hrvatski zakon
func (s *ObracunService) Kalendar(ctx context.Context) obracun.Kalendar {
	if s == nil || s.repo == nil {
		return obracun.Hrvatski{}
	}
	ps, err := s.repo.Blagdani(ctx)
	if err != nil || len(ps) == 0 {
		return obracun.Hrvatski{}
	}
	return ps
}

// Koeficijenti vraća množitelje iz baze; bez baze one iz obrasca IORS 2026
func (s *ObracunService) Koeficijenti(ctx context.Context) obracun.Koeficijenti {
	if s == nil || s.repo == nil {
		return obracun.IORS2026
	}
	k, err := s.repo.Koeficijenti(ctx)
	if err != nil {
		return obracun.IORS2026
	}
	return k
}

// Blagdani vraća pravila za uređivanje
func (s *ObracunService) Blagdani(ctx context.Context) (obracun.Pravila, error) {
	return s.repo.Blagdani(ctx)
}

// SpremiBlagdan provjerava pravilo i upisuje ga. Oznaka se izvodi iz naziva
// kad je nova, da se isti blagdan s dva čvora ne upiše dvaput pod dva ključa.
func (s *ObracunService) SpremiBlagdan(ctx context.Context, p obracun.Pravilo) error {
	p.Naziv = strings.TrimSpace(p.Naziv)
	if p.Naziv == "" {
		return errors.New("blagdan mora imati naziv")
	}
	switch p.Vrsta {
	case obracun.Stalni:
		if p.Mjesec < 1 || p.Mjesec > 12 || p.Dan < 1 || p.Dan > 31 {
			return errors.New("stalni blagdan traži mjesec i dan")
		}
		p.Pomak, p.Datum = 0, ""
	case obracun.PoUskrsu:
		p.Mjesec, p.Dan, p.Datum = 0, 0, ""
	case obracun.Jednokratni:
		if len(p.Datum) != 10 {
			return errors.New("jednokratni neradni dan traži datum")
		}
		p.Mjesec, p.Dan, p.Pomak = 0, 0, 0
	default:
		return errors.New("nepoznata vrsta blagdana")
	}
	if p.DoGodine != 0 && p.OdGodine != 0 && p.DoGodine < p.OdGodine {
		return errors.New("godina do ne može biti prije godine od")
	}
	if p.ID == "" {
		p.ID = oznakaIzNaziva(p.Naziv)
		if p.Vrsta == obracun.Jednokratni {
			p.ID += "-" + p.Datum
		}
	}
	return s.repo.SaveBlagdan(ctx, p)
}

// MakniBlagdan miče pravilo. Ukinut blagdan bolje je zatvoriti godinom do,
// da prošli obračuni ostanu točni; micanje je za krivo upisano.
func (s *ObracunService) MakniBlagdan(ctx context.Context, id string) error {
	return s.repo.MakniBlagdan(ctx, id)
}

// SpremiKoeficijente upisuje sve množitelje odjednom, kako ih obrazac nosi
func (s *ObracunService) SpremiKoeficijente(ctx context.Context, k obracun.Koeficijenti) error {
	for mjesto, po := range k {
		for razred, v := range po {
			if v < 0 || v > 10 {
				return errors.New("koeficijent mora biti između 0 i 10")
			}
			if err := s.repo.SaveKoeficijent(ctx, repository.Koeficijent{ID: repository.KoeficijentID(mjesto, razred), Mjesto: string(mjesto), Razred: string(razred), K: v}); err != nil {
				return err
			}
		}
	}
	return nil
}

// oznakaIzNaziva: "Dan državnosti" → "dan-drzavnosti"
func oznakaIzNaziva(naziv string) string {
	zamjene := strings.NewReplacer("č", "c", "ć", "c", "š", "s", "đ", "d", "ž", "z", "Č", "c", "Ć", "c", "Š", "s", "Đ", "d", "Ž", "z")
	var b strings.Builder
	prosli := '-'
	for _, r := range strings.ToLower(zamjene.Replace(naziv)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prosli = r
		default:
			if prosli != '-' {
				b.WriteRune('-')
				prosli = '-'
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// OznakaIzNaziva daje oznaku bez dijakritike i razmaka, za ključeve i imena
// datoteka: "COP Osijek" → "cop-osijek"
func OznakaIzNaziva(naziv string) string { return oznakaIzNaziva(naziv) }
