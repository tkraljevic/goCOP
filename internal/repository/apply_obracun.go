package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/obracun"
)

// primjeneObracuna su pravila blagdana, koeficijenti i postavke obračuna te
// materijalno-tehnička sredstva: vrste, skladišta, promet, popisi i potrebe
var primjeneObracuna = map[string]primjena{
	EntityBlagdani:        {upis: primijeniBlagdan, brisanje: obrisi(`DELETE FROM blagdani WHERE id = ?`)},
	EntityKoeficijenti:    {upis: primijeniKoeficijent, brisanje: obrisi(`DELETE FROM koeficijenti WHERE id = ?`)},
	EntityObracunPostavke: {upis: primijeniPostavkuObracuna, brisanje: obrisi(`DELETE FROM obracun_postavke WHERE id = ?`)},
	EntityMtsVrste:        {upis: upisPoArgumentima(vrstaUpsert, vrstaArgs), brisanje: obrisi(`DELETE FROM mts_vrste WHERE id = ?`)},
	EntityMtsSkladista:    {upis: primijeniSkladiste, brisanje: obrisi(`DELETE FROM mts_skladista WHERE id = ?`)},
	EntityMtsPromet:       {upis: primijeniPromet, brisanje: obrisi(`DELETE FROM mts_promet WHERE id = ?`)},
	EntityMtsPopisi:       {upis: upisPoArgumentima(popisUpsert, popisArgs), brisanje: obrisi(`DELETE FROM mts_popisi WHERE id = ?`)},
	EntityMtsPotrebe:      {upis: primijeniPotrebu, brisanje: obrisi(`DELETE FROM mts_potrebe WHERE id = ?`)},
}

// primijeniBlagdan stavlja primljeno pravilo blagdana na površinu
func primijeniBlagdan(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p obracun.Pravilo
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, blagdanUpsert, blagdanArgs(p, v.CreatedAt)...)
	return err
}

// primijeniKoeficijent stavlja primljeni koeficijent obračuna na površinu
func primijeniKoeficijent(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var k Koeficijent
	if err := json.Unmarshal(v.Payload, &k); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, koeficijentUpsert, k.ID, k.Mjesto, k.Razred, k.K, v.CreatedAt.UTC())
	return err
}

// primijeniPostavkuObracuna stavlja primljenu postavku obračuna na površinu
func primijeniPostavkuObracuna(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p Postavka
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, postavkaUpsert, p.ID, p.Vrijednost, v.CreatedAt.UTC())
	return err
}

// primijeniSkladiste stavlja primljeno skladište sredstava na površinu
func primijeniSkladiste(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.Skladiste
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, skladisteUpsert, skladisteArgs(&x)...)
	return err
}

// primijeniPromet stavlja primljeni promet sredstava na površinu
func primijeniPromet(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.Promet
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, prometUpsert, prometArgs(&x)...)
	return err
}

// primijeniPotrebu stavlja primljenu potrebu za sredstvima na površinu
func primijeniPotrebu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.Potreba
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, potrebaUpsert, potrebaArgs(&x)...)
	return err
}
