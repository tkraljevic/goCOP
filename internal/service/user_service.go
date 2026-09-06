package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"

	"github.com/google/uuid"
)

var (
	ErrUnauthorized    = errors.New("nemate ovlasti za ovu radnju")
	ErrUsernameExists  = errors.New("korisničko ime već postoji")
	ErrUserNotFound    = errors.New("korisnik nije pronađen")
	ErrInvalidUserData = errors.New("neispravni podaci korisnika")
)

type UserService struct {
	userRepo *repository.UserRepository
	auth     *AuthService
	sse      *SSEBroker
}

func NewUserService(uRepo *repository.UserRepository, auth *AuthService, sse *SSEBroker) *UserService {
	return &UserService{
		userRepo: uRepo,
		auth:     auth,
		sse:      sse,
	}
}

type CreateUserRequest struct {
	Username      string
	Password      string
	FullName      string
	Title         string
	IsGlobalAdmin bool
	OrgType       models.OrgType
	OrgName       string
	Phone         string
	MobilePhone   string
	ShortPhone    string
	ShortMobile   string
	Email         string
	// Inicijalna funkcija / zaduženje
	DutyTitle    string
	Role         models.Role
	ScopeType    models.ScopeType
	SectorID     *string
	AreaID       *int
	SectionCodes string
}

// areaSectors vraća pretragu sektora po području iz registra
func (s *UserService) areaSectors() areaSector {
	m := map[int]string{}
	if areas, err := s.userRepo.ListAreas(""); err == nil {
		for _, a := range areas {
			m[a.ID] = a.SectorID
		}
	}
	return func(id int) string { return m[id] }
}

// CreateUser stvara novog korisnika i dodjeljuje mu početnu funkciju
func (s *UserService) CreateUser(actor *models.UserPermissions, req CreateUserRequest) (*models.User, error) {
	if actorRank(actor) == 0 {
		return nil, ErrUnauthorized
	}
	if req.IsGlobalAdmin && !actor.IsGlobalAdmin {
		return nil, ErrUnauthorized
	}
	sectors := s.areaSectors()
	if req.Role != "" {
		scope, sectorID, areaID, err := normalizeScope(req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors)
		if err != nil {
			return nil, err
		}
		req.ScopeType, req.SectorID, req.AreaID = scope, sectorID, areaID
		if err := mayAssign(actor, req.Role, req.SectorID, req.AreaID, sectors); err != nil {
			return nil, err
		}
	} else if !actor.IsGlobalAdmin {
		return nil, fmt.Errorf("%w: račun bez dužnosti otvara samo uprava organizacije", ErrUnauthorized)
	}

	req.Username = strings.TrimSpace(req.Username)
	req.FullName = strings.TrimSpace(req.FullName)
	if req.Username == "" || req.FullName == "" || req.Password == "" {
		return nil, fmt.Errorf("%w: korisničko ime, ime i lozinka su obavezni", ErrInvalidUserData)
	}

	existing, err := s.userRepo.GetUserByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrUsernameExists
	}

	pwHash, err := s.auth.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	user := &models.User{
		ID:            userID,
		Username:      req.Username,
		PasswordHash:  pwHash,
		FullName:      req.FullName,
		Title:         req.Title,
		IsGlobalAdmin: req.IsGlobalAdmin,
		OrgType:       req.OrgType,
		OrgName:       req.OrgName,
		Phone:         req.Phone,
		MobilePhone:   req.MobilePhone,
		ShortPhone:    req.ShortPhone,
		ShortMobile:   req.ShortMobile,
		Email:         req.Email,
		IsActive:      true,
		// Lozinku je odabrao administrator i zna je: osoba je pri prvoj
		// prijavi mora zamijeniti svojom, kao i nakon poništavanja
		MustChangePassword: true,
	}

	var initialDuty *models.Duty
	if req.Role != "" {
		dutyTitle := req.DutyTitle
		if dutyTitle == "" {
			dutyTitle = req.Role.Label()
		}
		dutyID, _ := uuid.NewV7()
		initialDuty = &models.Duty{
			ID:           dutyID,
			UserID:       userID,
			Title:        dutyTitle,
			Role:         req.Role,
			ScopeType:    req.ScopeType,
			SectorID:     req.SectorID,
			AreaID:       req.AreaID,
			SectionCodes: req.SectionCodes,
			IsPrimary:    true,
			IsTemporary:  false,
			IsActive:     true,
		}
	}

	if err := s.userRepo.CreateUser(user, initialDuty); err != nil {
		return nil, err
	}

	s.sse.Broadcast("users_updated", fmt.Sprintf("Kreiran novi djelatnik: %s", user.FullName), user)
	return user, nil
}

type UpdateUserRequest struct {
	ID            uuid.UUID
	Username      string
	Password      string
	FullName      string
	Title         string
	IsGlobalAdmin bool
	OrgType       models.OrgType
	OrgName       string
	Phone         string
	MobilePhone   string
	ShortPhone    string
	ShortMobile   string
	Email         string
	IsActive      bool
}

// UpdateUser ažurira matične podatke korisnika
func (s *UserService) UpdateUser(actor *models.UserPermissions, req UpdateUserRequest) (*models.User, error) {
	target, err := s.userRepo.GetUserByID(req.ID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrUserNotFound
	}

	if !actor.IsGlobalAdmin {
		// Tko nije uprava organizacije, uređuje svoj profil ili račune u
		// svom dosegu čije su sve dužnosti na njegovoj razini ili niže
		if actor.User.ID != target.ID {
			if err := mayManage(actor, target, s.areaSectors()); err != nil {
				return nil, err
			}
			req.IsGlobalAdmin = target.IsGlobalAdmin
		} else {
			// Korisnik uređuje SAM SVOJ profil (može mijenjati ime, titulu, telefone, email, lozinku)
			req.IsGlobalAdmin = target.IsGlobalAdmin
			req.IsActive = target.IsActive
			req.Username = target.Username // korisnik ne može mijenjati svoje korisničko ime
			if req.OrgType == "" {
				req.OrgType = target.OrgType
			}
			if req.OrgName == "" {
				req.OrgName = target.OrgName
			}
		}
	}

	target.Username = strings.TrimSpace(req.Username)
	target.FullName = strings.TrimSpace(req.FullName)
	target.Title = req.Title
	target.IsGlobalAdmin = req.IsGlobalAdmin
	target.OrgType = req.OrgType
	target.OrgName = req.OrgName
	target.Phone = req.Phone
	target.MobilePhone = req.MobilePhone
	target.ShortPhone = req.ShortPhone
	target.ShortMobile = req.ShortMobile
	target.Email = req.Email
	target.IsActive = req.IsActive

	if req.Password != "" {
		pwHash, err := s.auth.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		target.PasswordHash = pwHash
	}

	if err := s.userRepo.UpdateUser(target); err != nil {
		return nil, err
	}

	s.sse.Broadcast("users_updated", fmt.Sprintf("Ažuriran djelatnik: %s", target.FullName), target)
	return target, nil
}

type AddDutyRequest struct {
	UserID       uuid.UUID
	Title        string
	Role         models.Role
	ScopeType    models.ScopeType
	SectorID     *string
	AreaID       *int
	SectionCodes string // npr. "A.19.1, A.19.2, A.19.3"
	IsPrimary    bool
	IsTemporary  bool
	Reason       string
	ExpiresAt    *time.Time
}

// AddDuty dodjeljuje korisniku dodatnu funkciju, dionice ili privremenu ispomoć
func (s *UserService) AddDuty(actor *models.UserPermissions, req AddDutyRequest) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	sectors := s.areaSectors()
	scope, sectorID, areaID, err := normalizeScope(req.Role, req.SectorID, req.AreaID, req.SectionCodes, sectors)
	if err != nil {
		return err
	}
	req.ScopeType, req.SectorID, req.AreaID = scope, sectorID, areaID
	if err := mayAssign(actor, req.Role, req.SectorID, req.AreaID, sectors); err != nil {
		return err
	}

	if strings.TrimSpace(req.Title) == "" {
		req.Title = req.Role.Label()
	}

	dutyID, err := uuid.NewV7()
	if err != nil {
		return err
	}

	actorID := actor.User.ID
	duty := &models.Duty{
		ID:           dutyID,
		UserID:       req.UserID,
		Title:        req.Title,
		Role:         req.Role,
		ScopeType:    req.ScopeType,
		SectorID:     req.SectorID,
		AreaID:       req.AreaID,
		SectionCodes: req.SectionCodes,
		IsPrimary:    req.IsPrimary,
		IsTemporary:  req.IsTemporary,
		Reason:       req.Reason,
		AssignedBy:   &actorID,
		ExpiresAt:    req.ExpiresAt,
		IsActive:     true,
	}

	if err := s.userRepo.AddDuty(duty); err != nil {
		return err
	}

	s.sse.Broadcast("duty_added", fmt.Sprintf("Dodijeljena nova funkcija/ispomoć: %s", req.Title), duty)
	return nil
}

// RevokeDuty opoziva funkciju ili privremenu ispomoć; smije tko bi je smio i dati
func (s *UserService) RevokeDuty(actor *models.UserPermissions, dutyID uuid.UUID) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}
	duty, err := s.userRepo.GetDuty(dutyID)
	if err != nil {
		return err
	}
	if duty == nil {
		return fmt.Errorf("zaduženje nije pronađeno")
	}
	if !actor.IsGlobalAdmin && duty.UserID == actor.User.ID {
		return fmt.Errorf("%w: vlastitu dužnost opoziva nadređena razina", ErrUnauthorized)
	}
	if err := mayAssign(actor, duty.Role, duty.SectorID, duty.AreaID, s.areaSectors()); err != nil {
		return err
	}

	if err := s.userRepo.RevokeDuty(dutyID); err != nil {
		return err
	}

	s.sse.Broadcast("duty_revoked", "Opozvana funkcija / ovlast", dutyID.String())
	return nil
}

// DeleteUser briše račun koji se nitko nikad nije prijavio, sa svim
// dužnostima. Račun koji je radio ne briše se, jer bi očitanja i upisi ostali
// bez imena: njemu se isključi prijava.
func (s *UserService) DeleteUser(actor *models.UserPermissions, targetID uuid.UUID) error {
	if actorRank(actor) == 0 {
		return ErrUnauthorized
	}

	target, err := s.userRepo.GetUserByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrUserNotFound
	}

	// Korisnik ne može obrisati samoga sebe
	if actor.User.ID == target.ID {
		return fmt.Errorf("ne možete obrisati vlastiti korisnički profil")
	}

	if target.LastLoginAt != nil {
		return fmt.Errorf("račun koji se prijavljivao ne briše se: isključite mu prijavu, da upisi zadrže ime")
	}
	if !actor.IsGlobalAdmin {
		if err := mayManage(actor, target, s.areaSectors()); err != nil {
			return err
		}
	}

	if err := s.userRepo.DeleteUser(targetID); err != nil {
		return err
	}

	s.sse.Broadcast("user_deleted", fmt.Sprintf("Obrisan profil djelatnika: %s", target.FullName), targetID.String())
	return nil
}

func (s *UserService) ListUsers(sectorID string, areaID int, role, search, status string) ([]models.User, error) {
	return s.userRepo.ListUsers(sectorID, areaID, role, search, status)
}

func (s *UserService) GetUserByID(id uuid.UUID) (*models.User, error) {
	return s.userRepo.GetUserByID(id)
}

// GlobalAdminContact je kontakt glavnog administratora za stranicu prijave
func (s *UserService) GlobalAdminContact() (name, phone, email string, ok bool) {
	return s.userRepo.GlobalAdminContact()
}

func (s *UserService) ListSectors() ([]models.Sector, error) {
	return s.userRepo.ListSectors()
}

func (s *UserService) ListAreas(sectorID string) ([]models.Area, error) {
	return s.userRepo.ListAreas(sectorID)
}

func (s *UserService) GetDashboardStats() (repository.DashboardStats, error) {
	return s.userRepo.GetDashboardStats()
}
