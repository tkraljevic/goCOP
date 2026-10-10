package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sektori i branjena područja koji letvu prate (Odluke (8. 10. 2026.),
// pitanja 8 i 9) stoje u stupcima prate_sektori i prate_podrucja kao popis
// odvojen zarezom. U knjizi verzija putuju kao polja letve prate_sektori i
// prate_podrucja: čvor 0.0.34 ih ne poznaje, pa ih čuva i prenosi, a pri
// svojoj izmjeni letve knjiga ih prepiše iz prethodne verzije.

// sektoriUTekst slaže šifre sektora za stupac
func sektoriUTekst(sektori []string) string {
	return strings.Join(sektori, ",")
}

// sektoriIzTeksta čita šifre sektora iz stupca; prazno je nil
func sektoriIzTeksta(tekst string) []string {
	var out []string
	for _, s := range strings.Split(tekst, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// podrucjaUTekst slaže brojeve područja za stupac
func podrucjaUTekst(podrucja []int) string {
	dijelovi := make([]string, len(podrucja))
	for i, a := range podrucja {
		dijelovi[i] = strconv.Itoa(a)
	}
	return strings.Join(dijelovi, ",")
}

// podrucjaIzTeksta čita brojeve područja iz stupca; što nije broj veći od
// nule preskače se, a prazno je nil
func podrucjaIzTeksta(tekst string) []int {
	var out []int
	for _, s := range strings.Split(tekst, ",") {
		if a, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && a > 0 {
			out = append(out, a)
		}
	}
	return out
}

// NepoznatoPracenje vraća prvi od zadanih sektora i područja kojeg nema u
// registru, opisan za poruku („sektor X”, „branjeno područje 99”); prazno
// kad su svi u registru
func (r *StationRepository) NepoznatoPracenje(ctx context.Context, sektori []string, podrucja []int) (string, error) {
	for _, s := range sektori {
		ima, err := r.uRegistru(ctx, `SELECT 1 FROM sectors WHERE id = ?`, s)
		if err != nil || !ima {
			return "sektor " + s, err
		}
	}
	for _, a := range podrucja {
		ima, err := r.uRegistru(ctx, `SELECT 1 FROM areas WHERE id = ?`, a)
		if err != nil || !ima {
			return fmt.Sprintf("branjeno područje %d", a), err
		}
	}
	return "", nil
}

// uRegistru javlja vraća li upit ijedan redak
func (r *StationRepository) uRegistru(ctx context.Context, upit string, arg any) (bool, error) {
	var x int
	err := r.db.QueryRowContext(ctx, upit, arg).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("greška pri provjeri registra: %w", err)
	}
	return true, nil
}
