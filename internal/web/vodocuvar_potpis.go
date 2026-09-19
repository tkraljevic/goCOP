package web

import (
	"context"
	"fmt"
	"net/http"
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

// simulacijaKljuca javlja potpisuje li se simuliranim ključem: gleda se
// tuđim očima i opcija je uključena
func (h *VodocuvarHandler) simulacijaKljuca(r *http.Request) bool {
	viewing, _ := r.Context().Value(contextKeyViewing).(bool)
	if !viewing || h.opcije == nil || h.potpis == nil || h.potpis() == nil {
		return false
	}
	return h.opcije(r.Context()).SimulacijaKljuca
}

// potpisnikZa otključava ključ osobe kad ga ima; bez ključa vraća nil, pa
// se list predaje odnosno ovjerava bez elektroničkog potpisa. Tuđim očima
// uz uključenu simulaciju daje jednokratni simulirani ključ, bez lozinke.
func (h *VodocuvarHandler) potpisnikZa(r *http.Request, u *models.User, lozinka string) (*potpis.Potpisnik, error) {
	if h.potpis == nil {
		return nil, nil
	}
	ctx := r.Context()
	ps := h.potpis()
	if ps == nil {
		return nil, nil
	}
	if h.simulacijaKljuca(r) {
		return ps.Simulirani(ctx, u)
	}
	// Gledanje tuđim očima služi provjeri prikaza i ovlasti, ali ne smije
	// stvoriti zapis da je promatrana osoba nešto potpisala. Njezin pravi ključ
	// ne otključavamo ni kad ga ima: za testno potpisivanje administrator mora
	// izričito uključiti simulaciju, koja potpis i PDF vidljivo označava kao
	// bezvrijedne.
	if viewing, _ := ctx.Value(contextKeyViewing).(bool); viewing {
		return nil, fmt.Errorf("gledanjem tuđim očima nije dopušteno potpisivanje; za test uključite Simulaciju ključa u postavkama")
	}
	if !ps.Ima(ctx, u.ID.String()) {
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
	simulacija := p != nil && p.Simulacija
	if simulacija {
		razlog = "SIMULACIJA potpisa (testiranje, bezvrijedno): " + razlog
	}
	dod := pdfw.Dodatak{
		Stranica: m.stranica, X: x, Y: m.y, W: m.w, H: m.h,
		Crtaj: func(d *pdfw.Doc) {
			crtajPotpisLista(d, 0, 0, m.w, ime, &k, list, kod, l.Cvor, sken, true, simulacija)
		},
		Ime: ime, Razlog: razlog + " " + list, Mjesto: l.Cvor, Kad: kad,
	}
	if simulacija {
		// Cijela stranica je izgled potpisnog polja: tako je veliki pečat dio
		// samog vidljivog PAdES potpisa i nijedan PDF čitač ga ne može sakriti
		// kao običnu bilješku. Ostatak izgleda je proziran.
		dod.X, dod.Y, dod.W, dod.H = 0, 0, pdfw.A4W, pdfw.A4H
		dod.Crtaj = func(d *pdfw.Doc) {
			const naslov = "SIMULACIJA · BEZVRIJEDNO"
			const opis = "potpis simuliranim ključem · samo za testiranje"
			y := d.H/2 - 18
			x1, x2, y1, y2 := 38.0, d.W-38, y-38, y+44
			d.CrtaBoja(x1, y1, x2, y1, 1.2, crvena)
			d.CrtaBoja(x2, y1, x2, y2, 1.2, crvena)
			d.CrtaBoja(x2, y2, x1, y2, 1.2, crvena)
			d.CrtaBoja(x1, y2, x1, y1, 1.2, crvena)
			d.TekstBoja((d.W-pdfw.SirinaTeksta(naslov, 27, true))/2, y, 27, true, naslov, crvena)
			d.TekstBoja((d.W-pdfw.SirinaTeksta(opis, 14, false))/2, y+28, 14, false, opis, crvena)
			crtajPotpisLista(d, x, m.y, m.w, ime, &k, list, kod, l.Cvor, sken, true, true)
		}
	}
	return dod
}

// potpisiIzvornik dodaje vidljivi blok i potpisuje PDF kad postoji ključ
func potpisiIzvornik(pdf []byte, p *potpis.Potpisnik, dod pdfw.Dodatak) ([]byte, error) {
	if p == nil {
		return pdfw.Dodaj(pdf, dod)
	}
	return p.PotpisiPDF(pdf, dod)
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
		pdf, err = potpisiIzvornik(pdf, p, h.dodatakPotpisa(m, m.xVodocuvar, l, l.Ime, *l.PredanoAt, l.KodPredaje(), "Predaja dnevnog lista,", otisci[l.UserID], p))
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
	pdf, err := potpisiIzvornik(pdf, p, dod)
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
