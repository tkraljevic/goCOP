package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Plan dežurstava i obračun sati su jedna evidencija: razmak od–do jedne
// osobe s opisom rada. Prije obrane je plan, poslije nje ulaz za obrazac.
// Plan slaže uprava centra (voditelj ili zamjenik COP-a); dežurni ga vidi.

// UpravaCentra: plan slaže voditelj ili zamjenik centra — uprava sektora,
// isto pravo koje dnevnik COP-a i otvara. Pravo pisanja (CanWrite) ovdje
// nije dovoljno: dežurni piše u dnevnik, ali plan ne slaže sam sebi.
func (s *JournalService) UpravaCentra(perms *models.UserPermissions, j *models.Journal) bool {
	return perms != nil && j != nil && j.CentarSektor != "" && perms.CanAdminister(j.CentarSektor, 0)
}

// Dezurstva vraća dežurstva dnevnika redom početka
func (s *JournalService) Dezurstva(ctx context.Context, journalID string) ([]models.Dezurstvo, error) {
	return s.repo.ListDezurstva(ctx, journalID)
}

// SpremiDezurstvo upisuje ili mijenja dežurstvo. Razmak mora biti unutar
// trajanja dnevnika: dežurstvo prije početka obrane ili poslije zaključenja
// nije dežurstvo u ovoj obrani.
func (s *JournalService) SpremiDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, d *models.Dezurstvo) error {
	if u == nil {
		return errors.New("upis zahtijeva prijavu")
	}
	if j == nil || j.CentarSektor == "" {
		return errors.New("plan dežurstava vodi se uz dnevnik COP-a")
	}
	if !s.UpravaCentra(perms, j) {
		return errors.New("plan dežurstava slaže voditelj ili zamjenik centra")
	}
	if d.UserID == "" || strings.TrimSpace(d.UserName) == "" {
		return errors.New("odaberite osobu")
	}
	if !d.Do.After(d.Od) {
		return errors.New("kraj dežurstva mora biti poslije početka")
	}
	if d.Do.Sub(d.Od) > 36*time.Hour {
		return errors.New("dežurstvo dulje od 36 sati upišite kao više razmaka")
	}
	if j.StartedAt != nil && d.Od.Before(*j.StartedAt) {
		return fmt.Errorf("dnevnik počinje %s: dežurstvo prije toga u njega ne ide", j.StartedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	if j.EndedAt != nil && d.Do.After(j.EndedAt.AddDate(0, 0, 1)) {
		return fmt.Errorf("dnevnik je zaključen %s: dežurstvo poslije toga ide u novi dnevnik", j.EndedAt.In(models.Zagreb).Format("2.1.2006."))
	}
	d.Mjesto = models.MjestoZaOpis(d.Opis)
	if d.Mjesto == "" {
		return errors.New("odaberite opis rada s popisa")
	}
	d.JournalID = j.ID
	d.UserName, d.Napomena = strings.TrimSpace(d.UserName), strings.TrimSpace(d.Napomena)
	if d.ID != "" {
		cur, err := s.repo.GetDezurstvo(ctx, d.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.JournalID != j.ID {
			return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
		}
		d.CreatedBy, d.CreatedAt = cur.CreatedBy, cur.CreatedAt
	} else {
		d.CreatedBy = u.ID.String()
	}
	return s.repo.SaveDezurstvo(ctx, d)
}

// MakniDezurstvo miče dežurstvo iz plana; u knjizi verzija ostaje trag
func (s *JournalService) MakniDezurstvo(ctx context.Context, u *models.User, perms *models.UserPermissions, o models.Opseg, j *models.Journal, id string) error {
	if u == nil || !s.UpravaCentra(perms, j) {
		return errors.New("plan dežurstava slaže voditelj ili zamjenik centra")
	}
	d, err := s.repo.GetDezurstvo(ctx, id)
	if err != nil {
		return err
	}
	if d == nil || d.JournalID != j.ID {
		return errors.New("dežurstvo nije pronađeno u ovom dnevniku")
	}
	return s.repo.ArhivirajDezurstvo(ctx, d)
}

// ObracunOsobe su sati jedne osobe u razdoblju, po mjestu rada i razredu,
// i ono što iz toga slijedi po koeficijentima
type ObracunOsobe struct {
	UserID, UserName string
	Ured, Teren      obracun.Sati
	Stvarni          time.Duration
	Obracunski       float64
}

// Sati vraća stvarne sate razreda za mjesto, za ispis u tablici
func (o ObracunOsobe) Sati(mjesto string, r obracun.Razred) time.Duration {
	if mjesto == models.MjestoTeren {
		return o.Teren[r]
	}
	return o.Ured[r]
}

// Obracun zbraja dežurstva dnevnika u razdoblju [od, do) po osobi. Razmak
// koji viri iz razdoblja uzima se samo onim dijelom koji je unutra, pa se
// obračun za mjesec ne mijenja time što smjena prelazi u sljedeći.
func (s *JournalService) Obracun(ctx context.Context, j *models.Journal, od, do time.Time, k obracun.Koeficijenti) ([]ObracunOsobe, error) {
	dez, err := s.repo.ListDezurstva(ctx, j.ID)
	if err != nil {
		return nil, err
	}
	poOsobi := map[string]*ObracunOsobe{}
	for _, d := range dez {
		a, b := d.Od, d.Do
		if a.Before(od) {
			a = od
		}
		if b.After(do) {
			b = do
		}
		if !b.After(a) {
			continue
		}
		o := poOsobi[d.UserID]
		if o == nil {
			o = &ObracunOsobe{UserID: d.UserID, UserName: d.UserName, Ured: obracun.Sati{}, Teren: obracun.Sati{}}
			poOsobi[d.UserID] = o
		}
		sati := obracun.Razvrstaj(a, b, obracun.Hrvatski{})
		if d.Mjesto == models.MjestoTeren {
			o.Teren.Dodaj(sati)
		} else {
			o.Ured.Dodaj(sati)
		}
	}
	var out []ObracunOsobe
	for _, o := range poOsobi {
		o.Stvarni = o.Ured.Ukupno() + o.Teren.Ukupno()
		o.Obracunski = k.Obracunski(o.Ured, obracun.Ured) + k.Obracunski(o.Teren, obracun.Teren)
		out = append(out, *o)
	}
	sort.Slice(out, func(i, l int) bool { return out[i].UserName < out[l].UserName })
	return out, nil
}
