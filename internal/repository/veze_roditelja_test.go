package repository

import (
	"slices"
	"testing"
)

// Svaki strani ključ između zapisa koji putuju razmjenom mora imati pravilo
// brisanja (vezeNaRoditelje, apply_roditelji.go). Bez njega dijete koje drugi
// čvor upiše obrisanom roditelju opet čeka zauvijek, a čvor koji ga ima ne
// može obrisati roditelja, kao do 13. 10. 2026. Test čita strane ključeve iz
// sheme, pa novi strani ključ bez pravila ili bez obrazložene iznimke ruši
// test, a i zastarjela veza ili iznimka.
func TestStraniKljuceviImajuPraviloBrisanja(t *testing.T) {
	iznimke := map[string]string{
		"duties.assigned_by→users": "korisnik koji je dodijelio dužnost ne da se obrisati ni na izvoru: " +
			"strani ključ je bez kaskade, pa brisanje javi grešku",
		"section_stations.section_code→sections":    "kazalo dionice odlazi s dionicom (ON DELETE CASCADE)",
		"section_structures.section_code→sections":  "kazalo dionice odlazi s dionicom (ON DELETE CASCADE)",
		"section_territories.section_code→sections": "kazalo dionice odlazi s dionicom (ON DELETE CASCADE)",
		"section_stations.station_id→stations": "kazalo dionice odlazi s letvom (ON DELETE CASCADE), a vezu na " +
			"obrisanu letvu obnova kazala preskače (db.zivURegistru)",
		"section_structures.structure_id→structures": "kazalo dionice odlazi s objektom (ON DELETE CASCADE), a vezu " +
			"na obrisani objekt obnova kazala preskače (db.zivURegistru)",
	}
	poznati := PoznatiEntiteti()
	c := noviCvor(t, "ured")
	rows, err := c.db.Query(`SELECT m.name, p."from", p."table", p."to" FROM sqlite_master m
		JOIN pragma_foreign_key_list(m.name) p WHERE m.type = 'table' ORDER BY m.name, p."from"`)
	nuzno(t, err)
	defer rows.Close()
	uShemi := map[string]bool{}
	for rows.Next() {
		var dijete, stupac, roditelj, kljuc string
		nuzno(t, rows.Scan(&dijete, &stupac, &roditelj, &kljuc))
		if !slices.Contains(poznati, dijete) || !slices.Contains(poznati, roditelj) {
			continue
		}
		veza := dijete + "." + stupac + "→" + roditelj
		uShemi[veza] = true
		ima := slices.Contains(vezeNaRoditelje, vezaNaRoditelja{dijete, stupac, roditelj})
		if !ima && iznimke[veza] == "" {
			t.Errorf("strani ključ %s nema pravilo brisanja: dodaj ga u vezeNaRoditelje ili u iznimke s razlogom", veza)
		}
		if ima && kljuc != kljucZapisa(roditelj) {
			t.Errorf("strani ključ %s pokazuje na stupac %s, a primjena roditelja traži po %s", veza, kljuc, kljucZapisa(roditelj))
		}
	}
	nuzno(t, rows.Err())
	for _, r := range vezeNaRoditelje {
		if veza := r.dijete + "." + r.stupac + "→" + r.roditelj; !uShemi[veza] {
			t.Errorf("veza %s iz vezeNaRoditelje nije strani ključ u shemi", veza)
		}
	}
	for veza := range iznimke {
		if !uShemi[veza] {
			t.Errorf("iznimka %s više nije strani ključ u shemi: ukloniti je", veza)
		}
	}
}
