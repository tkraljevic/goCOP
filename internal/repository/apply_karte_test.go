package repository

import (
	"encoding/json"
	"testing"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Postavke karata primljene razmjenom odmah vrijede u programu, a spomenik
// zapisa vraća zadane postavke (narančasti nasipi, satelit)
func TestPrimljenePostavkeKarteVrijedeOdmah(t *testing.T) {
	prije := models.PostavkeKartePrograma()
	t.Cleanup(func() { models.SetPostavkeKarte(prije) })

	c := noviCvor(t, "b")
	nove := models.PostavkeKarte{Podloge: []models.PodlogaKarte{}}
	b, err := json.Marshal(nove)
	nuzno(t, err)
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntityPostavke, PostavkaKarta, vid(1), Postavka{ID: PostavkaKarta, Vrijednost: string(b)})))
	if got := models.PostavkeKartePrograma(); len(got.Podloge) != 0 {
		t.Fatalf("primljene postavke ne vrijede: %+v", got)
	}
	v := ledger.Version{Entity: EntityPostavke, EntityID: PostavkaKarta, Archived: true, Payload: []byte(`{}`)}
	nuzno(t, primijeniUTx(t, c, v))
	if got := models.PostavkeKartePrograma(); len(got.Podloge) != 1 || got.Podloge[0] != models.SatelitEsri {
		t.Errorf("nakon spomenika: %+v", got)
	}
	if redakaNaPovrsini(t, c, "postavke") != 0 {
		t.Error("spomenik nije maknuo zapis s površine")
	}
}
