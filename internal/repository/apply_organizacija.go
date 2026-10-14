package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// primjeneOrganizacije su ustroj (sektori, područja i nazivi ustroja),
// izvođači, korisnici s dužnostima i modulima te ključevi za e-potpis osoba
var primjeneOrganizacije = map[string]primjena{
	EntitySectors:               {upis: primijeniSektor, brisanje: obrisi(`DELETE FROM sectors WHERE id = ?`)},
	EntityAreas:                 {upis: primijeniPodrucje, brisanje: obrisiPoBroju(`DELETE FROM areas WHERE id = ?`)},
	EntityOrgTerms:              {upis: primijeniNazive, brisanje: makniNazive},
	EntityContractors:           {upis: primijeniIzvodaca, brisanje: obrisi(`DELETE FROM contractors WHERE id = ?`)},
	EntityContractorAssignments: {upis: primijeniDodjeluIzvodaca, brisanje: obrisi(`DELETE FROM contractor_assignments WHERE id = ?`)},
	EntityUsers:                 {upis: primijeniKorisnika, brisanje: obrisi(`DELETE FROM users WHERE id = ?`)},
	EntityDuties:                {upis: primijeniDuznost, brisanje: obrisi(`DELETE FROM duties WHERE id = ?`)},
	EntityRoleModules:           {upis: primijeniModuleUloge, brisanje: obrisi(`DELETE FROM role_modules WHERE role = ?`)},
	EntityUserModules:           {upis: primijeniModuleKorisnika, brisanje: obrisi(`DELETE FROM user_modules WHERE user_id = ?`)},
	EntityPotpisniKljucevi:      {upis: primijeniPotpisniKljuc, brisanje: obrisi(`DELETE FROM potpisni_kljucevi WHERE user_id = ?`)},
	EntityPotpisniIzdavatelji:   {upis: primijeniIzdavateljaPotpisa, brisanje: obrisi(`DELETE FROM potpisni_izdavatelji WHERE cvor = ?`)},
}

// primijeniSektor stavlja primljeni sektor (ili drugu ustrojstvenu jedinicu)
// na površinu
func primijeniSektor(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var sec models.Sector
	if err := json.Unmarshal(v.Payload, &sec); err != nil {
		return err
	}
	if sec.Level == 0 {
		sec.Level = 2 // verzija otprije razina: jedinica je sektor
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level, vgo_phone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, vgo_name = excluded.vgo_name,
			center_cop = excluded.center_cop, address = excluded.address, phone = excluded.phone,
			email = excluded.email, level = excluded.level, vgo_phone = excluded.vgo_phone
	`, sec.ID, sec.Name, sec.VgoName, sec.CenterCop, sec.Address, sec.Phone, sec.Email, sec.Level, sec.VgoPhone)
	return err
}

// primijeniPodrucje stavlja primljeno branjeno područje na površinu
func primijeniPodrucje(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var a models.Area
	if err := json.Unmarshal(v.Payload, &a); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO areas (id, sector_id, name, vgi_name, subcenter, contractor_name, direct_to_sector, latitude, longitude, vgi_phone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET sector_id = excluded.sector_id, name = excluded.name,
			vgi_name = excluded.vgi_name, subcenter = excluded.subcenter, contractor_name = excluded.contractor_name,
			direct_to_sector = excluded.direct_to_sector, latitude = excluded.latitude, longitude = excluded.longitude,
			vgi_phone = excluded.vgi_phone
	`, a.ID, a.SectorID, a.Name, a.VgiName, a.Subcenter, a.ContractorName, boolToInt(a.DirectToSector), a.Latitude, a.Longitude, a.VgiPhone)
	return err
}

// primijeniNazive stavlja primljene nazive ustroja na površinu i uzima ih u
// program
func primijeniNazive(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var t models.OrgTerms
	if err := json.Unmarshal(v.Payload, &t); err != nil {
		return err
	}
	t = t.Filled()
	if _, err := tx.ExecContext(ctx, termsUpsert, termsArgs(t)...); err != nil {
		return err
	}
	models.SetTerms(t)
	return nil
}

// makniNazive miče nazive ustroja s površine
func makniNazive(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	// bez zapisa vrijede zadani nazivi, kao i kad ih nitko nije upisao
	if _, err := tx.ExecContext(ctx, `DELETE FROM org_terms WHERE id = ?`, v.EntityID); err != nil {
		return err
	}
	models.SetTerms(models.DefaultTerms())
	return nil
}

// primijeniIzvodaca stavlja primljenog izvođača na površinu
func primijeniIzvodaca(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var c models.Contractor
	if err := json.Unmarshal(v.Payload, &c); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, contractorUpsert, c.ID, c.Name, c.ShortName, c.OIB, c.Address, c.Phone, c.Email,
		c.Contact, c.Notes, boolToInt(c.Active), c.UpdatedAt.UTC())
	return err
}

// primijeniDodjeluIzvodaca stavlja primljenu dodjelu izvođača na površinu
func primijeniDodjeluIzvodaca(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var a models.ContractorAssignment
	if err := json.Unmarshal(v.Payload, &a); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, assignmentUpsert, a.ID, a.ContractorID, a.SectorID, a.AreaID, a.Note, a.UpdatedAt.UTC())
	return err
}

// primijeniKorisnika stavlja primljeni korisnički račun na površinu
func primijeniKorisnika(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var uv userVersion
	if err := json.Unmarshal(v.Payload, &uv); err != nil {
		return err
	}
	u := uv.User
	u.PasswordHash = uv.PasswordHash
	// Lozinka: verzije zapisane prije nego što je sažetak počeo putovati
	// nemaju ga, pa se u tom slučaju čuva onaj koji na ovom čvoru već
	// stoji — inače bi sinkronizacija zaključala korisnika.
	// Prijava: uzima se kasnija od dviju, jer je svaki čvor vidio svoje.
	// Potvrda adrese za PIN ide s verzijom kakva jest: verzija bez nje
	// (zapisana prije nego što je postojala ili s uklonjenom potvrdom)
	// ostavlja račun bez potvrde.
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, full_name, title, is_global_admin,
			must_change_password, org_type, org_name, phone, mobile_phone, short_phone, short_mobile, email,
			is_active, last_login_at, created_at, updated_at,
			pin_adresa_potvrdena, pin_adresa_potvrdio, pin_adresa_potvrdena_kad)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = CASE WHEN excluded.password_hash = '' THEN users.password_hash
			                     ELSE excluded.password_hash END,
			full_name = excluded.full_name, title = excluded.title, is_global_admin = excluded.is_global_admin,
			must_change_password = excluded.must_change_password, org_type = excluded.org_type,
			org_name = excluded.org_name, phone = excluded.phone, mobile_phone = excluded.mobile_phone,
			short_phone = excluded.short_phone, short_mobile = excluded.short_mobile, email = excluded.email, is_active = excluded.is_active,
			last_login_at = MAX(COALESCE(excluded.last_login_at, users.last_login_at),
			                    COALESCE(users.last_login_at, excluded.last_login_at)),
			updated_at = excluded.updated_at,
			pin_adresa_potvrdena = excluded.pin_adresa_potvrdena, pin_adresa_potvrdio = excluded.pin_adresa_potvrdio,
			pin_adresa_potvrdena_kad = excluded.pin_adresa_potvrdena_kad
	`, u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Title, boolToInt(u.IsGlobalAdmin),
		boolToInt(u.MustChangePassword), string(u.OrgType), u.OrgName, u.Phone, u.MobilePhone, u.ShortPhone, u.ShortMobile, u.Email,
		boolToInt(u.IsActive), nullTime(u.LastLoginAt), u.CreatedAt.UTC(), u.UpdatedAt.UTC(),
		u.PINAdresaPotvrdena, u.PINAdresuPotvrdio, nullTime(u.PINAdresaPotvrdenaKad))
	return err
}

// primijeniDuznost stavlja primljenu dužnost na površinu
func primijeniDuznost(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var d models.Duty
	if err := json.Unmarshal(v.Payload, &d); err != nil {
		return err
	}
	var assignedBy *string
	if d.AssignedBy != nil {
		s := d.AssignedBy.String()
		assignedBy = &s
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO duties (id, user_id, title, role, scope_type, sector_id, area_id, section_codes,
			is_primary, is_temporary, reason, assigned_by, created_at, expires_at, is_active,
			rok, istece_s_obranom, ovisi_o)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			user_id = excluded.user_id, title = excluded.title, role = excluded.role,
			scope_type = excluded.scope_type, sector_id = excluded.sector_id, area_id = excluded.area_id,
			section_codes = excluded.section_codes, is_primary = excluded.is_primary,
			is_temporary = excluded.is_temporary, reason = excluded.reason, assigned_by = excluded.assigned_by,
			expires_at = excluded.expires_at, is_active = excluded.is_active,
			rok = excluded.rok, istece_s_obranom = excluded.istece_s_obranom, ovisi_o = excluded.ovisi_o
	`, d.ID.String(), d.UserID.String(), d.Title, string(d.Role), string(d.ScopeType), d.SectorID, d.AreaID,
		d.SectionCodes, boolToInt(d.IsPrimary), boolToInt(d.IsTemporary), d.Reason, assignedBy,
		d.CreatedAt.UTC(), nullTime(d.ExpiresAt), boolToInt(d.IsActive),
		nullTime(d.Rok), boolToInt(d.IsticeSObranom), ovisiOZapis(d.OvisiO))
	return err
}

// primijeniModuleUloge stavlja primljene module uloge na površinu
func primijeniModuleUloge(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var rm models.RoleModules
	if err := json.Unmarshal(v.Payload, &rm); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO role_modules (role, modules, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(role) DO UPDATE SET modules = excluded.modules, updated_at = excluded.updated_at`,
		rm.Role, models.JoinModules(rm.Modules), rm.UpdatedAt.UTC())
	return err
}

// primijeniModuleKorisnika stavlja primljene module korisnika na površinu
func primijeniModuleKorisnika(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var um models.UserModules
	if err := json.Unmarshal(v.Payload, &um); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO user_modules (user_id, shown, hidden, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET shown = excluded.shown, hidden = excluded.hidden, updated_at = excluded.updated_at`,
		um.UserID, models.JoinModules(um.Shown), models.JoinModules(um.Hidden), um.UpdatedAt.UTC())
	return err
}

// primijeniPotpisniKljuc stavlja primljeni ključ za e-potpis osobe na
// površinu
func primijeniPotpisniKljuc(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var k models.PotpisniKljuc
	if err := json.Unmarshal(v.Payload, &k); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, potpisniKljucUpsert, k.UserID, k.Ime, k.Cert, k.Kljuc, k.Sol, k.Izdao, k.CreatedAt.UTC(), k.UpdatedAt.UTC())
	return err
}

// primijeniIzdavateljaPotpisa stavlja primljenog izdavatelja potvrda za
// e-potpis na površinu
func primijeniIzdavateljaPotpisa(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var i models.IzdavateljPotpisa
	if err := json.Unmarshal(v.Payload, &i); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, izdavateljUpsert, i.Cvor, i.Cert, i.CreatedAt.UTC())
	return err
}
