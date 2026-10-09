package web

import (
	"testing"
	"time"

	"gocop/internal/prognoza"
)

// Stranica čita dnevnu prognozu istog izdanja kao satnu: izdanje u kojem
// dnevni model nije računao nema dnevne, a starija se ne podmeće.
func TestDnevnoZaIstoIzdanje(t *testing.T) {
	izdano := int64(497635)
	put := bazaPrognoza(t, izdano)
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := prognoza.SpremiDnevne(db, []prognoza.DnevnaIzdana{
		{Letva: "belisce", Izdano: izdano - 1, Dan: 1, Ciljni: izdano + 23, Vrijednost: 80, Model: prognoza.ModelDnevni}}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if d, err := c.DnevnoZa(time.Unix(izdano*3600, 0)); err != nil || len(d) != 0 {
		t.Errorf("izdanje bez dnevne: %v, %v", d, err)
	}
	if d, err := c.DnevnoZa(time.Unix((izdano-1)*3600, 0)); err != nil || len(d["belisce"]) != 1 {
		t.Errorf("izdanje s dnevnom: %v, %v", d, err)
	}
	var nema *CitacPrognoza
	if d, err := nema.DnevnoZa(time.Now()); d != nil || err != nil {
		t.Errorf("bez baze: %v, %v", d, err)
	}
}
