package service

// Privremeno imenovanje (privremena ispomoć): kad za obranu od poplava
// nedostaje ljudi, rukovoditelj sektora privremeno imenuje rukovoditelja ili
// zamjenika dionice, odnosno branjenog područja. Rješenje „prestaje važiti
// prestankom mjera izvanredne i redovne obrane na dionici”, a može imati i
// zadan datum; što privremena uprava dodijeli na razini uprave ističe zajedno
// s njezinom dužnošću. Sve ostale dužnosti su stalne, dok ih uprava ne
// izmijeni ili opozove.
//
// Stvarni istek ostaje u Duty.ExpiresAt, koji provjerava svaki dio programa i
// čvor starije inačice; ovdje se računa iz zadanog datuma (Rok), kraja obrane
// (IsticeSObranom) i dužnosti iz koje je dodijeljena (OvisiO).

import (
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// razlogPrivremeneUprave je razlog dužnosti koju dodijeli privremena uprava
const razlogPrivremeneUprave = "do isteka privremene uprave koja ju je dodijelila"

// trazenaPrivremenost je privremenost kako je tražena u obrascu
func trazenaPrivremenost(req AddDutyRequest) privremenost {
	return privremenost{privremena: req.IsTemporary, rok: req.ExpiresAt, sObranom: req.IsticeSObranom}
}

// upisi upisuje dopuštenu privremenost u dužnost; dužnost koju je dodijelila
// privremena uprava bez upisanog razloga dobiva razlog
func (p privremenost) upisi(d *models.Duty) {
	d.IsTemporary, d.Rok, d.IsticeSObranom, d.OvisiO = p.privremena, p.rok, p.sObranom, p.ovisiO
	if p.ovisiO != nil && strings.TrimSpace(d.Reason) == "" {
		d.Reason = razlogPrivremeneUprave
	}
}

// istekDuznosti postavlja stvarni istek dužnosti. Stalna ga nema (i gubi zadani
// datum, istek s obranom i ovisnost, koji vrijede samo za privremenu);
// privremenoj je najraniji od zadanog datuma, kraja obrane i isteka dužnosti iz
// koje je dodijeljena. Bez ičega od toga privremena vrijedi do opoziva.
func (s *UserService) istekDuznosti(d *models.Duty, sad time.Time) {
	if !d.IsTemporary {
		d.Rok, d.ExpiresAt, d.IsticeSObranom, d.OvisiO, d.Reason = nil, nil, false, nil, ""
		return
	}
	istek := d.Rok
	if p := s.prestanakObraneZa(*d); p != nil {
		istek = raniji(istek, &p.Kad)
	}
	if d.OvisiO != nil {
		istek = raniji(istek, s.istekIzvora(*d.OvisiO, d.ExpiresAt, sad))
	}
	d.ExpiresAt = istek
}

// prestanakObraneZa je prestanak obrane za dužnost koja ističe s obranom
func (s *UserService) prestanakObraneZa(d models.Duty) *models.PrestanakObrane {
	if !d.IsticeSObranom || s.prestanakObrane == nil {
		return nil
	}
	return s.prestanakObrane(d)
}

// IzvoriIsteka kaže, za privremene dužnosti kojima istek nije zadani dan,
// odakle im je istek: prestanak obrane prema ovjerenom aktu (s njegovom
// oznakom) ili istek privremene uprave iz koje su dodijeljene. Za profil.
func (s *UserService) IzvoriIsteka(duznosti []models.Duty) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, d := range duznosti {
		if izvor := s.izvorIsteka(d); izvor != "" {
			out[d.ID] = izvor
		}
	}
	return out
}

func (s *UserService) izvorIsteka(d models.Duty) string {
	if !d.IsTemporary || d.ExpiresAt == nil || istiKraj(d.ExpiresAt, d.Rok) {
		return ""
	}
	if p := s.prestanakObraneZa(d); p != nil && p.Kad.Equal(*d.ExpiresAt) {
		return "prestanak obrane, akt " + p.Akt.Oznaka()
	}
	if d.OvisiO != nil {
		return "istek privremene uprave koja ju je dodijelila"
	}
	return ""
}

// istekIzvora je istek dužnosti iz koje je privremena dodijeljena. Opozvana
// ili obrisana (s računom) prekida je odmah, a istek koji je već prošao tada
// ostaje: trenutak opoziva nije zapisan, pa bi svaki čvor upisao svoj „sad”
// i prepisivali bi ga jedan drugome. Kad se izvor ne da pročitati, ostaje
// dosadašnji istek.
func (s *UserService) istekIzvora(id uuid.UUID, dosad *time.Time, sad time.Time) *time.Time {
	izvor, err := s.userRepo.GetDuty(id)
	switch {
	case err != nil:
		return dosad
	case izvor != nil && izvor.IsActive:
		return izvor.ExpiresAt
	case dosad != nil && !dosad.After(sad):
		return dosad
	}
	return &sad
}

// UskladiPrivremene preračunava stvarni istek privremenih dužnosti kojima on
// ovisi o obrani ili o drugoj dužnosti: nakon ovjere i poništenja akta, nakon
// opoziva dužnosti i u redovnom krugu (akti stižu i razmjenom). Promjena se
// bilježi u knjigu, pa je dobivaju i drugi čvorovi. Ovisnost može ići preko
// više dužnosti (privremena uprava sektora dodijeli upravu područja, a ona
// dalje), pa se ponavlja dok se ništa ne mijenja, najviše tri puta.
func (s *UserService) UskladiPrivremene() error {
	promjena := true
	for i := 0; i < 3 && promjena; i++ {
		var err error
		if promjena, err = s.uskladiJednom(time.Now()); err != nil {
			return err
		}
	}
	return nil
}

// uskladiPoOpozivu: dužnosti koje je privremena uprava dodijelila ističu s
// njezinom; opoziv je već spremljen, pa se greška samo bilježi (krug je
// ponavlja)
func (s *UserService) uskladiPoOpozivu(dutyID uuid.UUID) {
	if err := s.UskladiPrivremene(); err != nil {
		log.Printf("privremene dužnosti nakon opoziva %s: %v", dutyID, err)
	}
}

// uskladiJednom jednom prolazi privremene dužnosti i upisuje promijenjen istek
func (s *UserService) uskladiJednom(sad time.Time) (bool, error) {
	duznosti, err := s.userRepo.PrivremeneSIstekom()
	if err != nil {
		return false, err
	}
	promjena := false
	for i := range duznosti {
		d := &duznosti[i]
		staro := d.ExpiresAt
		s.istekDuznosti(d, sad)
		if istiKraj(staro, d.ExpiresAt) {
			continue
		}
		if err := s.userRepo.PostaviIstek(d.ID, d.ExpiresAt); err != nil {
			return promjena, err
		}
		promjena = true
	}
	return promjena, nil
}

// istiKraj: oba bez kraja ili isti trenutak
func istiKraj(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
