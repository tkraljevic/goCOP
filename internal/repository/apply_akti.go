package repository

import (
	"context"
	"encoding/json"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// verzijaZaPovrsinu je verzija zapisa koja ide na površinu kad razmjena
// donese v, i javlja treba li je primijeniti. Za većinu zapisa to je
// zadnja verzija u knjizi, i to samo kad je v ta zadnja: starija verzija
// koja stigne zaobilazno ne vraća površinu unatrag. Iznimke su akt
// (aktNaPovrsini) i opoziv (opozivZaPovrsinu).
func verzijaZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	switch v.Entity {
	case EntityAkti:
		return aktZaPovrsinu(ctx, rec, v)
	case entitetOpozivi:
		return opozivZaPovrsinu(ctx, rec, v)
	}
	top, err := rec.Latest(ctx, v.Entity, v.EntityID)
	if err != nil {
		return v, false, err
	}
	// Dnevni list vodočuvara i njegov izvornik slažu površinu iz svih verzija
	// (predan list ima prednost, dva potpisa ostaju dva lista), pa ide svaka
	// primljena verzija (primjenjujeSe; Odluke (8. 10. 2026.), pitanje 7).
	return v, primjenjujeSe(top, v), nil
}

// aktZaPovrsinu je verzija akta koja ide na površinu kad razmjena donese
// bilo koju njegovu verziju. Primjena je ista kad je površina već njezina,
// pa se primjenjuje uvijek: tako i starija verzija, ovjera s drugog čvora,
// dođe na površinu kad je u knjizi poslije nje nacrt.
func aktZaPovrsinu(ctx context.Context, rec *ledger.Recorder, v ledger.Version) (ledger.Version, bool, error) {
	povijest, err := rec.History(ctx, v.Entity, v.EntityID)
	if err == nil && len(povijest) == 0 {
		err = ledger.ErrNoVersion
	}
	if err != nil {
		return v, false, err
	}
	return aktNaPovrsini(povijest), true, nil
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
	for _, x := range povijest {
		if verzijaOvjerenogAkta(x) {
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
