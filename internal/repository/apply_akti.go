package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/sadrzaj"
)

// primjeneAkata su akti s izvornicima i slanjima, špance, oznake rješenja,
// primatelji, potpisi e-pošte, žigovi sektora i opće postavke
var primjeneAkata = map[string]primjena{
	EntityAkti:           {upis: primijeniAkt, brisanje: obrisi(`DELETE FROM akti WHERE id = ?`)},
	EntityIzvornici:      {upis: primijeniIzvornikAkta, brisanje: obrisi(`DELETE FROM akti_izvornici WHERE akt_id = ?`)},
	EntitySlanja:         {upis: primijeniSlanje, brisanje: obrisi(`DELETE FROM akti_slanja WHERE id = ?`)},
	EntitySprance:        {upis: primijeniSprancu, brisanje: obrisi(`DELETE FROM akti_sprance WHERE sektor = ?`)},
	EntityRjesenjaOznaka: {upis: primijeniRjesenjeOznake, brisanje: obrisi(`DELETE FROM akti_rjesenja_oznaka WHERE id = ?`)},
	EntityPrimatelji:     {upis: primijeniPrimatelja, brisanje: obrisi(`DELETE FROM primatelji WHERE id = ?`)},
	EntityPotpisi:        {upis: primijeniPotpisPoste, brisanje: obrisi(`DELETE FROM posta_potpisi WHERE user_id = ?`)},
	EntityZigovi:         {upis: primijeniZig, brisanje: obrisi(`DELETE FROM zigovi WHERE sektor = ?`)},
	EntityPostavke:       {upis: primijeniPostavku, brisanje: makniPostavku},
}

// verzijaZaPovrsinu je verzija zapisa koja ide na površinu kad razmjena
// donese v, i javlja treba li je primijeniti. Za većinu zapisa to je
// zadnja verzija u knjizi, i to samo kad je v ta zadnja: starija verzija
// koja stigne zaobilazno ne vraća površinu unatrag. Iznimke su akt
// (aktNaPovrsini), dnevnik (dnevnikNaPovrsini), upis, list i dežurstvo
// ovjerenog dnevnika (dijeteDnevnikaZaPovrsinu) i opoziv
// (opozivZaPovrsinu). Verzija koja u sadržaju nosi drugu oznaku zapisa ne
// broji se (istaOznaka).
func verzijaZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	switch v.Entity {
	case EntityAkti:
		return izPovijesti(ctx, rec, v, aktNaPovrsini)
	case EntityJournals:
		return izPovijesti(ctx, rec, v, dnevnikNaPovrsini)
	case EntityJournalEntries, EntityJournalSheets, EntityDezurstva:
		return dijeteDnevnikaZaPovrsinu(ctx, rec, v)
	case entitetOpozivi:
		return opozivZaPovrsinu(ctx, rec, v)
	}
	return zadnjaZaPovrsinu(ctx, rec, v)
}

// zadnjaZaPovrsinu je pravilo zadnje verzije: na površinu ide v kad je
// zadnja u knjizi, a kad zadnja nosi drugu oznaku, zadnja koja nosi pravu
func zadnjaZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	top, err := rec.Latest(ctx, v.Entity, v.EntityID)
	if err != nil {
		return v, false, err
	}
	if !istaOznaka(*top) {
		return zadnjaSIstomOznakom(ctx, rec, v)
	}
	// Dnevni list vodočuvara i njegov izvornik slažu površinu iz svih verzija
	// (predan list ima prednost, dva potpisa ostaju dva lista), pa ide svaka
	// primljena verzija (primjenjujeSe; Odluke (8. 10. 2026.), pitanje 7).
	return v, primjenjujeSe(top, v), nil
}

// izPovijesti je verzija akta ili dnevnika koja ide na površinu kad
// razmjena donese bilo koju njegovu verziju: izbor je bira iz povijesti
// zapisa, od najnovije prema najstarijoj. Primjena je ista kad je površina
// već njezina, pa se primjenjuje uvijek: tako i starija verzija, ovjera s
// drugog čvora, dođe na površinu kad je u knjizi poslije nje neovjerena.
// Bira se samo među verzijama koje u sadržaju nose oznaku zapisa.
func izPovijesti(ctx context.Context, rec *ledger.Recorder, v ledger.Version, izbor func([]ledger.Version) ledger.Version) (ledger.Version, bool, error) {
	povijest, err := rec.History(ctx, v.Entity, v.EntityID)
	if err == nil && len(povijest) == 0 {
		err = ledger.ErrNoVersion
	}
	if err != nil {
		return v, false, err
	}
	// verzija s drugom oznakom u sadržaju ne broji se ni kao ovjera
	if povijest = sIstomOznakom(povijest); len(povijest) == 0 {
		return v, false, nil
	}
	return izbor(povijest), true, nil
}

// aktNaPovrsini je verzija akta koja stoji na površini, iz povijesti od
// najnovije prema najstarijoj: najnovija ovjerena, a dok akt nije ovjeren
// nijednom verzijom, najnovija. Nacrt (i brisanje nacrta) s čvora koji
// ovjeru još nije primio tako ne vraća ovjeren akt u nacrt bez broja i ne
// briše ga: objavljena oznaka ne nestaje i čvor njegov broj ne daje ponovno
// (Odluke (8. 10. 2026.), pitanje 4). Ovjeren akt u nacrt ne vraća nijedna
// radnja programa, pa se tako ne gubi ništa što je nastalo poslije ovjere.
// Pravilo drže čvorovi od 0.0.35; čvor na 0.0.34 i dalje pokazuje
// najnoviju verziju, dok se ne nadogradi (površina se tada obnovi iz knjige).
func aktNaPovrsini(povijest []ledger.Version) ledger.Version {
	return najnovijaOvjerena(povijest, verzijaOvjerenogAkta)
}

// najnovijaOvjerena je iz povijesti od najnovije prema najstarijoj prva
// verzija koja nosi ovjeru, i brisanje kad ga nosi; bez takve, najnovija
func najnovijaOvjerena(povijest []ledger.Version, ovjerena func(ledger.Version) bool) ledger.Version {
	for _, x := range povijest {
		if ovjerena(x) {
			return x
		}
	}
	return povijest[0]
}

// verzijaOvjerenogAkta javlja nosi li verzija ovjeren akt; brisanje nosi
// akt kakav je bio kad je obrisan
func verzijaOvjerenogAkta(v ledger.Version) bool {
	var a struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(v.Payload, &a) == nil && a.Status == models.AktOvjeren
}

// primijeniAkt stavlja primljeni akt na površinu
func primijeniAkt(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var a models.Akt
	if err := json.Unmarshal(v.Payload, &a); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, aktUpsert, aktArgs(&a)...)
	return err
}

// primijeniIzvornikAkta stavlja primljeni izvornik akta na površinu, a PDF u
// spremište
func primijeniIzvornikAkta(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var iz Izvornik
	if err := json.Unmarshal(v.Payload, &iz); err != nil {
		return err
	}
	popuniOpis(&iz.Otisak, &iz.Bajtova, &iz.Vrsta, iz.PDF)
	if err := primiSadrzajZapisa(ctx, iz.Otisak, iz.Vrsta, iz.Bajtova, iz.PDF,
		sadrzaj.Veza{Entitet: EntityAkti, EntitetID: iz.AktID, Uloga: "izvornik", Kanal: v.Channel}); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, izvornikUpsert, iz.AktID, iz.Otisak, iz.Bajtova, iz.Vrsta, iz.Sazetak, iz.CreatedAt.UTC())
	return err
}

// primijeniSlanje stavlja primljeno slanje akta na površinu
func primijeniSlanje(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.SlanjeAkta
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, slanjeUpsert, slanjeArgs(&x)...)
	return err
}

// primijeniSprancu stavlja primljenu špancu sektora na površinu
func primijeniSprancu(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var sp models.Spranca
	if err := json.Unmarshal(v.Payload, &sp); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, sprancaUpsert, sprancaArgs(&sp)...)
	return err
}

// primijeniRjesenjeOznake stavlja primljenu oznaku rješenja na površinu
func primijeniRjesenjeOznake(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var x models.RjesenjeOznake
	if err := json.Unmarshal(v.Payload, &x); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, rjesenjeOznakeUpsert, rjesenjeOznakeArgs(&x)...)
	return err
}

// primijeniPrimatelja stavlja primljenog primatelja akata na površinu
func primijeniPrimatelja(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p models.Primatelj
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, primateljUpsert, primateljArgs(&p)...)
	return err
}

// primijeniPotpisPoste stavlja primljeni potpis e-pošte na površinu,
// očišćen istim čistačem kao potpis spremljen na ovom čvoru: drugi čvor
// (i stariji program) potpis možda nije očistio jednako
func primijeniPotpisPoste(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p PotpisPoste
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, potpisUpsert, p.UserID, posta.OcistiHTML(p.HTML), v.CreatedAt.UTC())
	return err
}

// primijeniZig stavlja primljeni žig sektora na površinu
func primijeniZig(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var z models.Zig
	if err := json.Unmarshal(v.Payload, &z); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, zigUpsert, z.Sektor, z.Mime, z.Slika, z.Uredio, v.CreatedAt.UTC())
	return err
}

// primijeniPostavku stavlja primljenu opću postavku na površinu; tema,
// postavke karata i karta bez interneta odmah se uzimaju u program
func primijeniPostavku(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	var p Postavka
	if err := json.Unmarshal(v.Payload, &p); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, opcaPostavkaUpsert, p.ID, p.Vrijednost, v.CreatedAt.UTC()); err != nil {
		return err
	}
	switch p.ID {
	case PostavkaTema:
		models.SetTema(models.CitajTemu(p.Vrijednost))
	case PostavkaKarta:
		models.SetPostavkeKarte(models.CitajPostavkeKarte(p.Vrijednost))
	case PostavkaKartaBezInterneta:
		models.SetVektorskaKarta(models.CitajVektorskuKartu(p.Vrijednost))
	}
	return nil
}

// makniPostavku miče opću postavku s površine
func makniPostavku(ctx context.Context, tx *sql.Tx, v ledger.Version) error {
	if v.EntityID == PostavkaTema {
		// bez zapisa vrijede zadane boje iz style.css
		if _, err := tx.ExecContext(ctx, `DELETE FROM postavke WHERE id = ?`, v.EntityID); err != nil {
			return err
		}
		models.SetTema(models.Tema{})
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM postavke WHERE id = ?`, v.EntityID)
	if err != nil {
		return err
	}
	switch v.EntityID {
	case PostavkaKarta:
		models.SetPostavkeKarte(models.ZadanePostavkeKarte())
	case PostavkaKartaBezInterneta:
		models.SetVektorskaKarta(nil)
	}
	return nil
}
