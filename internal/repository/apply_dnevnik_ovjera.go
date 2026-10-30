package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Ovjeren dnevnik COP-a ostaje ovjeren i kad razmjena donese izmjenu ili
// brisanje s računala koje ovjeru još nije primilo (Odluke (30. 10. 2026.)).
// Na površini stoji najnovija ovjerena verzija zaglavlja (dnevnikNaPovrsini),
// a upis, list i dežurstvo ovjerenog dnevnika ne odlaze s brisanjem koje
// ovjeru ne poznaje (zivoDijeteOvjerenog). Novija verzija ostaje u knjizi,
// primjena ništa ne odbija i ne stvara novu verziju. Pravilo drže čvorovi od
// 0.0.47; stariji i dalje pokazuju najnoviju verziju, a nakon nadogradnje
// površinu ispravi obnova pri pokretanju.

// dnevnikNaPovrsini je verzija dnevnika koja stoji na površini, istim
// pravilom kao akt (aktNaPovrsini): iz povijesti od najnovije prema
// najstarijoj najnovija ovjerena, a dok dnevnik nije ovjeren nijednom
// verzijom, najnovija. Zaglavlje i brisanje neovjerenog dnevnika s čvora koji
// ovjeru još nije primio tako ne skidaju ovjeru. Ovjeren dnevnik program ne
// briše (ObrisiDnevnik); brisanje koje nosi ovjeren dnevnik vrijedi.
func dnevnikNaPovrsini(povijest []ledger.Version) ledger.Version {
	return najnovijaOvjerena(povijest, verzijaOvjerenogDnevnika)
}

// verzijaOvjerenogDnevnika javlja nosi li verzija ovjeren dnevnik
func verzijaOvjerenogDnevnika(v ledger.Version) bool {
	_, ovjeren := vrijemeOvjere(v)
	return ovjeren
}

// vrijemeOvjere je trenutak ovjere koji nosi verzija dnevnika; false kad je
// verzija ne nosi
func vrijemeOvjere(v ledger.Version) (time.Time, bool) {
	var j struct {
		ZakljucenoAt *time.Time `json:"zakljuceno_at"`
	}
	if json.Unmarshal(v.Payload, &j) != nil || j.ZakljucenoAt == nil {
		return time.Time{}, false
	}
	return *j.ZakljucenoAt, true
}

// dijeteDnevnikaZaPovrsinu je verzija upisa, lista ili dežurstva koja ide na
// površinu: po pravilu zadnje verzije (zadnjaZaPovrsinu), osim kad je zadnja
// verzija brisanje koje ne pogađa ovjeren dnevnik. Tada ostaje zadnja živa
// verzija (zivoDijeteOvjerenog).
func dijeteDnevnikaZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	dijete, err := rec.History(ctx, v.Entity, v.EntityID)
	var dnevnik []ledger.Version
	if dijete = sIstomOznakom(dijete); err == nil && len(dijete) > 0 && dijete[0].Archived {
		dnevnik, err = rec.History(ctx, EntityJournals, dnevnikDjeteta(dijete[0]))
	}
	if err != nil {
		return v, false, err
	}
	if ziva, ostaje := zivoDijeteOvjerenog(dijete, sIstomOznakom(dnevnik)); ostaje {
		return ziva, true, nil
	}
	return zadnjaZaPovrsinu(ctx, rec, v)
}

// dnevnikDjeteta je oznaka dnevnika iz sadržaja upisa, lista ili dežurstva
func dnevnikDjeteta(v ledger.Version) string {
	var d struct {
		JournalID string `json:"journal_id"`
	}
	_ = json.Unmarshal(v.Payload, &d) // nečitljiv sadržaj nema dnevnika
	return d.JournalID
}

// zivoDijeteOvjerenog je zadnja živa verzija djeteta dnevnika kojem je
// zadnja verzija brisanje (dijete, od najnovije), kad je dnevnik (njegova
// povijest, od najnovije) na površini ovjeren, a brisanje dolazi s čvora koji
// ovjeru nije imao (brisanjeBezOvjere). Inače false: brisanje vrijedi.
func zivoDijeteOvjerenog(dijete, dnevnik []ledger.Version) (ledger.Version, bool) {
	if len(dijete) == 0 || !dijete[0].Archived || len(dnevnik) == 0 {
		return ledger.Version{}, false
	}
	z := dnevnikNaPovrsini(dnevnik)
	ovjera, ovjeren := vrijemeOvjere(z)
	if z.Archived || !ovjeren || !brisanjeBezOvjere(dijete, ovjera, dnevnik) {
		return ledger.Version{}, false
	}
	return zadnjaZiva(dijete[1:])
}

// zadnjaZiva je prva verzija iz povijesti koja nije brisanje
func zadnjaZiva(povijest []ledger.Version) (ledger.Version, bool) {
	for _, x := range povijest {
		if !x.Archived {
			return x, true
		}
	}
	return ledger.Version{}, false
}

// brisanjeBezOvjere javlja je li brisanje djeteta (dijete[0]) došlo s čvora
// koji ovjeru dnevnika nije imao. Upis i list ovjerenog dnevnika program
// briše samo s cijelim dnevnikom, pa to vrijedi za svako njihovo brisanje.
// Dežurstvo iz zaključanog plana miče uprava centra uz razlog koji ostaje u
// napomeni (MakniDezurstvo, Odluke (30. 10. 2026.), 5): takvo brisanje
// vrijedi, kao i micanje prije ovjere. Ne vrijedi brisanje bez novog razloga
// nastalo poslije ovjere ni brisanje uz brisanje cijelog dnevnika.
func brisanjeBezOvjere(dijete []ledger.Version, ovjera time.Time, dnevnik []ledger.Version) bool {
	b := dijete[0]
	if b.Entity != EntityDezurstva {
		return true
	}
	if dodanRazlogIspravka(dijete) {
		return false
	}
	kad, _ := ledger.VrijemeOznake(b.VersionID) // knjiga prima samo ispravne oznake
	return kad.After(ovjera) || sBrisanjemDnevnika(b, dnevnik)
}

// dodanRazlogIspravka javlja nosi li brisanje dežurstva razlog ispravka
// nakon ovjere koji prethodna živa verzija nema
func dodanRazlogIspravka(dijete []ledger.Version) bool {
	prije, _ := zadnjaZiva(dijete[1:])
	return brojRazloga(dijete[0]) > brojRazloga(prije)
}

// brojRazloga broji razloge ispravka nakon ovjere u napomeni dežurstva
func brojRazloga(v ledger.Version) int {
	var d struct {
		Napomena string `json:"napomena"`
	}
	_ = json.Unmarshal(v.Payload, &d) // nečitljiv ili prazan sadržaj nema razloga
	return len(models.TragoviIspravkaNakonOvjere(d.Napomena))
}

// sBrisanjemDnevnika javlja je li brisanje djeteta dio brisanja cijelog
// dnevnika: ArhivirajDnevnik u istoj transakciji briše djecu pa dnevnik, pa
// brisanje dnevnika s istog čvora stoji najviše minutu iza
func sBrisanjemDnevnika(b ledger.Version, dnevnik []ledger.Version) bool {
	kad, _ := ledger.VrijemeOznake(b.VersionID) // knjiga prima samo ispravne oznake
	for _, x := range dnevnik {
		t, _ := ledger.VrijemeOznake(x.VersionID)
		if x.Archived && x.NodeID == b.NodeID && !t.Before(kad) && t.Sub(kad) <= time.Minute {
			return true
		}
	}
	return false
}

// vratiDjecuOvjerenog vraća na površinu upise, listove i dežurstva dnevnika
// kojima je zadnja verzija brisanje koje ovjeru ne pogađa
// (zivoDijeteOvjerenog). Ide kad se dnevnik vraća na površinu (vratiDjecu):
// ovjera koja stigne poslije takvog brisanja tako vrati i ono što je
// brisanje odnijelo, pa isti skup verzija daje istu površinu kojim god redom
// stigao.
func vratiDjecuOvjerenog(ctx context.Context, tx *sql.Tx, id string) error {
	dnevnik, err := povijestTx(ctx, tx, EntityJournals, id)
	if err != nil || len(dnevnik) == 0 || !verzijaOvjerenogDnevnika(dnevnikNaPovrsini(dnevnik)) {
		return err
	}
	for _, entitet := range djecaDnevnika {
		if err == nil {
			err = vratiObrisanuDjecu(ctx, tx, entitet, id, dnevnik)
		}
	}
	return err
}

// vratiObrisanuDjecu vraća djecu entiteta za vratiDjecuOvjerenog: zapise s
// brisanjem u knjizi koje nosi dnevnik id, a na površini ih nema
func vratiObrisanuDjecu(ctx context.Context, tx *sql.Tx, entitet, id string, dnevnik []ledger.Version) error {
	ids, err := idsOf(ctx, tx, `SELECT DISTINCT w.entity_id FROM record_versions w
		WHERE w.entity = ? AND w.archived = 1
			AND CASE WHEN json_valid(w.payload) THEN CAST(json_extract(w.payload, '$.journal_id') AS TEXT) = ? END
			AND NOT EXISTS (SELECT 1 FROM `+entitet+` p WHERE p.id = w.entity_id)`, entitet, id)
	for _, d := range ids {
		if err == nil {
			err = vratiObrisanoDijete(ctx, tx, d, entitet, dnevnik)
		}
	}
	return err
}

// vratiObrisanoDijete stavlja na površinu zadnju živu verziju djeteta kad je
// njegova zadnja verzija brisanje koje ovjeru ne pogađa
func vratiObrisanoDijete(ctx context.Context, tx *sql.Tx, id, entitet string, dnevnik []ledger.Version) error {
	dijete, err := povijestTx(ctx, tx, entitet, id)
	ziva, ostaje := zivoDijeteOvjerenog(dijete, dnevnik)
	if err != nil || !ostaje {
		return err
	}
	return vratiDijete(ctx, tx, ziva)
}

// povijestTx su verzije zapisa iz knjige koje po oznaci idu na površinu, od
// najnovije, čitane u transakciji primjene
func povijestTx(ctx context.Context, tx *sql.Tx, entitet, id string) ([]ledger.Version, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version_id, node_id, archived, payload, channel, schema_version FROM record_versions
		WHERE entity = ? AND entity_id = ? ORDER BY version_id DESC`, entitet, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Version
	for err == nil && rows.Next() {
		v := ledger.Version{Entity: entitet, EntityID: id}
		var sadrzaj string
		err = rows.Scan(&v.VersionID, &v.NodeID, &v.Archived, &sadrzaj, &v.Channel, &v.SchemaVersion)
		v.Payload = json.RawMessage(sadrzaj)
		out = append(out, v)
	}
	if err == nil {
		err = rows.Err()
	}
	return sIstomOznakom(out), err
}
