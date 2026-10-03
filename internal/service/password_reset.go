package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Poništavanje lozinke: čovjek koji je zaboravio lozinku zove administratora,
// a on mu daje privremenu lozinku. Program je sam smišlja, jednom je pokaže
// administratoru i pamti samo njezin sažetak. Račun se tada zaključava na
// promjenu lozinke, pa privremena vrijedi za jednu prijavu, a otvorene sesije
// te osobe se gase — tko god ih je držao, više ne ulazi bez nove lozinke.

// tempWords su riječi od kojih se slaže privremena lozinka. Kratke su i
// jednoznačne, jer se čitaju preko telefona: bez č, ć, đ, š, ž i bez slova
// koja se miješaju u izgovoru.
var tempWords = []string{
	"bagat", "bara", "brana", "brod", "buk", "cesta", "dolina", "drava",
	"dunav", "greda", "jarak", "jezero", "kanal", "kesten", "kisa", "korito",
	"kupa", "lipa", "livada", "luka", "mjera", "most", "mura", "nasip",
	"obala", "oluja", "otok", "plima", "polje", "potok", "prag", "presjek",
	"rijeka", "sava", "sifon", "slap", "splav", "struja", "sunce", "brijeg",
	"topola", "ustava", "val", "vjetar", "voda", "vrba", "zdenac", "zora",
}

// randomInt vraća broj iz [0, n) iz izvora slučajnosti prikladnog za lozinke
func randomInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, fmt.Errorf("greška pri stvaranju privremene lozinke: %w", err)
	}
	return int(v.Int64()), nil
}

// GenerateTempPassword slaže privremenu lozinku oblika "nasip-vrba-most-472":
// tri riječi i tri znamenke, dovoljno da se pročita preko telefona bez
// slovkanja, a prekratko traje da bi je itko pogađao.
func GenerateTempPassword() (string, error) {
	parts := make([]string, 0, 4)
	for i := 0; i < 3; i++ {
		idx, err := randomInt(len(tempWords))
		if err != nil {
			return "", err
		}
		parts = append(parts, tempWords[idx])
	}
	num, err := randomInt(1000)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%03d", strings.Join(parts, "-"), num), nil
}

// canManageTarget javlja smije li actor vidjeti stanje tuđeg računa:
// globalni administrator svakoga, administrator sektora ili područja one
// koji u njegovom sektoru ili području imaju zaduženje. Za radnje koje daju
// pristup računu (poništenje lozinke, privremeni kod) vrijedi strože
// smijePonistiti.
func canManageTarget(actor *models.UserPermissions, target *models.User) bool {
	if actor == nil || target == nil {
		return false
	}
	if actor.IsGlobalAdmin {
		return true
	}
	if target.IsGlobalAdmin {
		return false // globalnog administratora poništava samo drugi globalni
	}
	for _, d := range target.Duties {
		if d.SectorID != nil && actor.AdminSectors[*d.SectorID] {
			return true
		}
		if d.AreaID != nil && actor.AdminAreas[*d.AreaID] {
			return true
		}
	}
	return false
}

// smijePonistiti javlja smije li actor dati tuđem računu privremenu
// lozinku ili kod za prijavu izvana: samo onaj tko ga smije i uređivati
// (mayManage: sve dužnosti osobe u njegovu dosegu i na njegovoj razini ili
// niže). Jedna dužnost u dosegu nije dovoljna, inače bi uprava nižeg dosega
// preuzela račun nadređenoga koji ima ispomoć u njezinu području, a s
// njim, jer poništenje uklanja potpisni ključ, i potpis njegovim imenom.
func smijePonistiti(actor *models.UserPermissions, target *models.User, sektori areaSector) error {
	if actor == nil || target == nil {
		return ErrUnauthorized
	}
	if err := mayManage(actor, target, sektori); err != nil {
		return fmt.Errorf("%w; lozinku i kod daje onaj tko smije uređivati cijeli račun (viša razina ili globalni administrator)", err)
	}
	// Ljude iste razine uprava uređuje, ali im ne poništava lozinku: s
	// lozinkom bi preuzela i potpis (poništenje uklanja potpisni ključ)
	if rank := actorRank(actor); rank > 1 {
		for _, d := range target.Duties {
			if d.Role.RazinaZaUpravu() <= rank {
				return fmt.Errorf("%w: lozinku i kod osobi s dužnošću na vašoj razini uprave ili višoj („%s”) daje viša razina ili globalni administrator", ErrUnauthorized, d.Role.Label())
			}
		}
	}
	return nil
}

// ResetPassword daje osobi novu privremenu lozinku i vraća je administratoru
// da je pročita naglas. Vraćena lozinka nigdje se ne zapisuje.
func (s *UserService) ResetPassword(actor *models.UserPermissions, targetID uuid.UUID) (*models.User, string, error) {
	target, err := s.userRepo.GetUserByID(targetID)
	if err != nil {
		return nil, "", err
	}
	if target == nil {
		return nil, "", ErrUserNotFound
	}
	if actor != nil && actor.User.ID == target.ID {
		return nil, "", fmt.Errorf("vlastitu lozinku mijenjate na svom profilu, ne poništavanjem")
	}
	if err := smijePonistiti(actor, target, s.areaSectors()); err != nil {
		return nil, "", err
	}

	temp, err := GenerateTempPassword()
	if err != nil {
		return nil, "", err
	}
	hash, err := s.auth.HashPassword(temp)
	if err != nil {
		return nil, "", err
	}
	if err := s.userRepo.ResetPassword(target.ID, hash); err != nil {
		return nil, "", err
	}
	// Otvorene sesije te osobe više ne vrijede: lozinka je poznata drugome
	if err := s.auth.EndAllSessions(target.ID); err != nil {
		return nil, "", err
	}
	// ...kao ni zapamćena računala, prijave na čekanju i privremeni kodovi
	if err := s.auth.opozoviPrijave(target.ID); err != nil {
		return nil, "", err
	}
	// Potpisni ključ zaključan je starom lozinkom, koju osoba više nema
	if err := s.ukloniPotpisniKljuc(target.ID); err != nil {
		return nil, "", fmt.Errorf("lozinka je poništena, ali osobni potpisni ključ nije uklonjen pa osoba lozinku ne bi mogla promijeniti; poništite je ponovno: %w", err)
	}
	// ...a lozinka sandučića e-pošte otvarala bi poštu osobe onome tko zna privremenu
	s.zaboraviSanducic(target)

	s.sse.Broadcast("users_updated", fmt.Sprintf("Poništena lozinka: %s", target.FullName), target.ID.String())
	return target, temp, nil
}
