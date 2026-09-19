package web

import (
	"context"
	"fmt"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
	"gocop/internal/potpis"
	"gocop/internal/service"
)

// potpisiIzvornika su potpisi u izvorniku lista, za prikaz na stranici
type potpisiIzvornika struct {
	Ima     bool // izvornik postoji
	Potpisi []potpis.Potpis
}

// potpisnikZa otključava ključ osobe kad ga ima; bez ključa vraća nil, pa
// se list predaje odnosno ovjerava bez elektroničkog potpisa
func (h *VodocuvarHandler) potpisnikZa(ctx context.Context, u *models.User, lozinka string) (*potpis.Potpisnik, error) {
	if h.potpis == nil {
		return nil, nil
	}
	ps := h.potpis()
	if ps == nil || !ps.Ima(ctx, u.ID.String()) {
		return nil, nil
	}
	if lozinka == "" {
		return nil, fmt.Errorf("upišite lozinku: njome otključavate svoj potpisni ključ")
	}
	return ps.Potpisnik(ctx, u, lozinka)
}

// dodatakPotpisa slaže polje potpisa s izgledom bloka na mjestu koje list
// ostavlja; kad potpisnika nema, dodatak je pečat s istim izgledom
func (h *VodocuvarHandler) dodatakPotpisa(m mjestaPotpisa, x float64, l *models.VodocuvarskiList, ime string, kad time.Time, kod, razlog string, sken *models.PotpisSlika, p *potpis.Potpisnik) pdfw.Dodatak {
	list := fmt.Sprintf("list %03d/%d", l.Broj, l.Datum.In(models.Zagreb).Year())
	k := kad
	return pdfw.Dodatak{
		Stranica: m.stranica, X: x, Y: m.y, W: m.w, H: m.h,
		Crtaj: func(d *pdfw.Doc) {
			crtajPotpisLista(d, 0, 0, m.w, ime, &k, list, kod, l.Cvor, sken, true)
		},
		Ime: ime, Razlog: razlog + " " + list, Mjesto: l.Cvor, Kad: kad,
	}
}

// izvornikPredaje sastavlja PDF predanog lista: vodočuvar ga potpiše svojim
// ključem ako ga ima, inače blok stoji nacrtan; mjesto rukovoditelja ostaje
// prazno za ovjeru
func (h *VodocuvarHandler) izvornikPredaje(ctx context.Context, s *service.VodocuvarService, l *models.VodocuvarskiList, p *potpis.Potpisnik) error {
	if l.PredanoAt == nil {
		return nil
	}
	area := h.podrucje(ctx, l)
	otisci := h.otisci(ctx, l)
	pdf, m := pdfLista(l, models.Terms(), area, otisci, crtanjeBlokova{vodocuvar: p == nil, rukovoditelj: true})
	if p != nil {
		var err error
		pdf, err = p.PotpisiPDF(pdf, h.dodatakPotpisa(m, m.xVodocuvar, l, l.Ime, *l.PredanoAt, l.KodPredaje(), "Predaja dnevnog lista,", otisci[l.UserID], p))
		if err != nil {
			return err
		}
	}
	return s.SpremiIzvornik(ctx, l.ID, pdf)
}

// izvornikOvjere dodaje ovjeru rukovoditelja na izvornik predaje, kao
// dodatak na iste bajtove pa potpis vodočuvara vrijedi i dalje; bez
// izvornika predaje list se iscrta iznova
func (h *VodocuvarHandler) izvornikOvjere(ctx context.Context, s *service.VodocuvarService, perms *models.UserPermissions, l *models.VodocuvarskiList, p *potpis.Potpisnik) error {
	if l.PotvrdenoAt == nil {
		return nil
	}
	area := h.podrucje(ctx, l)
	otisci := h.otisci(ctx, l)
	var pdf []byte
	var m mjestaPotpisa
	if iz, _ := s.Izvornik(ctx, perms, l.ID); iz != nil && len(iz.PDF) > 0 {
		pdf = iz.PDF
		_, m = pdfLista(l, models.Terms(), area, otisci, crtanjeBlokova{vodocuvar: true, rukovoditelj: false})
	} else {
		pdf, m = pdfLista(l, models.Terms(), area, otisci, crtanjeBlokova{vodocuvar: true, rukovoditelj: false})
	}
	dod := h.dodatakPotpisa(m, m.xRuk, l, l.Potvrdio, *l.PotvrdenoAt, l.KodOvjere(), "Ovjera dnevnog lista,", otisci[l.PotvrdioID], p)
	var err error
	if p != nil {
		pdf, err = p.PotpisiPDF(pdf, dod)
	} else {
		pdf, err = pdfw.Dodaj(pdf, dod)
	}
	if err != nil {
		return err
	}
	return s.SpremiIzvornik(ctx, l.ID, pdf)
}

func (h *VodocuvarHandler) podrucje(ctx context.Context, l *models.VodocuvarskiList) *models.Area {
	if org := h.org(); org != nil && l.AreaID > 0 {
		a, _ := org.GetArea(ctx, l.AreaID)
		return a
	}
	return nil
}

// potpisiIzvornika provjerava potpise u izvorniku lista za prikaz
func (h *VodocuvarHandler) provjeriIzvornik(ctx context.Context, s *service.VodocuvarService, perms *models.UserPermissions, id string) potpisiIzvornika {
	iz, _ := s.Izvornik(ctx, perms, id)
	if iz == nil {
		return potpisiIzvornika{}
	}
	out := potpisiIzvornika{Ima: true}
	if h.potpis != nil {
		if ps := h.potpis(); ps != nil {
			out.Potpisi = ps.Provjeri(ctx, iz.PDF)
		}
	}
	return out
}
