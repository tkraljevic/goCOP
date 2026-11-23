package service

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
	"gocop/internal/repository"
)

// ZamijeniSken stavlja novi sken ovjerenog akta umjesto dosadašnjeg, kad je
// sken bio nečitak, falila mu je stranica ili je snimljen krivi papir.
// Sadržaj akta, ovjera i potpisnik ostaju; zamjena se bilježi uz razlog i
// potpisuje ključem čvora, a stari sken ostaje u povijesti akta. Sken
// zamjenjuje onaj tko ga smije i učitati: tko akt smije ovjeriti, svojim
// očima.
func (s *AktService) ZamijeniSken(ctx context.Context, perms *models.UserPermissions, u *models.User, id string, datoteka []byte, razlog string) (*models.Akt, error) {
	a, err := s.repo.GetAkt(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.provjeriZamjenuSkena(ctx, perms, u, a, razlog); err != nil {
		return nil, err
	}
	pdf, err := pdfSkena(datoteka, a.Naslov())
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(pdf)
	novi := hex.EncodeToString(h[:])
	stari := a.SazetakSkena()
	if novi == stari {
		return nil, fmt.Errorf("to je isti sken koji je već uz akt; odaberite novi sken")
	}
	z := models.ZamjenaSkena{StariSazetak: stari, NoviSazetak: novi, ZamijenioID: u.ID.String(), Zamijenio: u.FullName,
		ZamijenjenoAt: time.Now().UTC().Truncate(time.Second), Razlog: strings.TrimSpace(razlog), Cvor: s.cvor}
	if iz, err := s.repo.GetIzvornik(ctx, a.ID); err == nil && iz != nil {
		z.StariBajtova = iz.Bajtova
	}
	if len(s.kljuc) == ed25519.PrivateKeySize {
		z.KljucCvora = razmjena.PublicKeyString(s.kljuc.Public().(ed25519.PublicKey))
		z.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.kljuc, a.PorukaZamjene(z)))
	}
	a.ZamjeneSkena = append(a.ZamjeneSkena, z)
	if err := s.repo.ZamijeniSken(ctx, a, &repository.Izvornik{AktID: a.ID, PDF: pdf, Sazetak: novi}); err != nil {
		return nil, err
	}
	return a, nil
}

// provjeriZamjenuSkena: akt postoji, ovjeren je skenom i nije poništen,
// osoba smije učitati sken i razlog je upisan
func (s *AktService) provjeriZamjenuSkena(ctx context.Context, perms *models.UserPermissions, u *models.User, a *models.Akt, razlog string) error {
	if err := zamjenjivSken(a); err != nil {
		return err
	}
	if err := s.smijeUcitatiSken(ctx, perms, u, a); err != nil {
		return err
	}
	if strings.TrimSpace(razlog) == "" {
		return fmt.Errorf("upišite zašto se sken zamjenjuje")
	}
	return nil
}

// zamjenjivSken javlja zašto se sken akta ne može zamijeniti; nil kad može
func zamjenjivSken(a *models.Akt) error {
	switch {
	case a == nil:
		return fmt.Errorf("akt ne postoji")
	case !a.Ovjeren():
		return fmt.Errorf("sken se zamjenjuje samo na ovjerenom aktu; nacrt se ovjerava učitavanjem skena")
	case a.Rucno == nil:
		return fmt.Errorf("akt %s je ovjeren u goCOP-u i nema sken koji bi se zamijenio", a.Oznaka())
	case a.Storniran():
		return fmt.Errorf("akt %s je poništen, pa se njegov sken više ne mijenja", a.Oznaka())
	}
	return nil
}

// SmijeZamijenitiSken javlja nudi li stranica akta zamjenu skena ovoj osobi
func (s *AktService) SmijeZamijenitiSken(ctx context.Context, perms *models.UserPermissions, u *models.User, a *models.Akt) bool {
	return zamjenjivSken(a) == nil && s.smijeUcitatiSken(ctx, perms, u, a) == nil
}

// ZamijenjeniSken vraća sken koji je na aktu bio zamijenjen; nil kad ga
// akt nikad nije imao kao zamijenjeni ili ga ovaj čvor još nema
func (s *AktService) ZamijenjeniSken(ctx context.Context, a *models.Akt, sazetak string) []byte {
	if a == nil {
		return nil
	}
	for _, z := range a.ZamjeneSkena {
		if z.StariSazetak == sazetak {
			return s.repo.ZamijenjeniSken(ctx, sazetak)
		}
	}
	return nil
}
