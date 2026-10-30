package peers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
)

// losaVerzija i dobraVerzija nose oznaku ispravnog oblika koja završava
// kratkom oznakom id (kratkaOznaka je vraća)
func losaVerzija(cvor, id string) ledger.Version {
	return ledger.Version{VersionID: oznakaKratko(id), NodeID: cvor, Channel: "ocitanja/16/2026", Entity: "probni", EntityID: "r-" + id, Payload: json.RawMessage("nije json")}
}

func dobraVerzija(cvor, id string) ledger.Version {
	return ledger.Version{VersionID: oznakaKratko(id), NodeID: cvor, Channel: "ocitanja/16/2026", Entity: "probni", EntityID: "r-" + id, Payload: json.RawMessage(`{"cm":120}`)}
}

// oznakaKratko je oznaka ispravnog oblika koja završava s id (do 12 znamenki)
func oznakaKratko(id string) string {
	return "0190a000-0000-7000-8000-" + strings.Repeat("0", 12-len(id)) + id
}

// kratkaOznaka je kratka oznaka iz oznakaKratko, bez vodećih nula
func kratkaOznaka(oznaka string) string {
	return strings.TrimLeft(strings.TrimPrefix(oznaka, "0190a000-0000-7000-8000-"), "0")
}

// Ključ kojemu je delta puna samo neispravnih verzija čita se dalje, do
// prve ispravne ili do kraja ključa; ključ koji nije pun ili u delti ima
// ispravnu verziju ne čita se. Dodano ide na kraj delte, redom nastanka.
func TestPrekoNeispravnihCitaZapeleKljuceve(t *testing.T) {
	delta := []ledger.Version{
		losaVerzija("a", "01"), losaVerzija("a", "02"), // pun, zapeo; iza još jedna puna neispravnih
		losaVerzija("b", "01"), losaVerzija("b", "02"), // pun, zapeo; iza kraj ključa
		losaVerzija("c", "01"),                          // nije pun
		losaVerzija("d", "01"), dobraVerzija("d", "02"), // pun, ali ima ispravnu
	}
	iza := map[string][]ledger.Version{
		"a|02": {losaVerzija("a", "03"), losaVerzija("a", "04")},
		"a|04": {losaVerzija("a", "05"), dobraVerzija("a", "06")},
		"b|02": {losaVerzija("b", "03")},
	}
	var pitano []string
	citaj := func(_ context.Context, cvor, kanal, iza1 string, n int) ([]ledger.Version, error) {
		k := cvor + "|0" + kratkaOznaka(iza1)
		pitano = append(pitano, k)
		if kanal != "ocitanja/16/2026" || n != 2 {
			t.Errorf("čita %s s ogradom %d", kanal, n)
		}
		return iza[k], nil
	}
	out, err := prekoNeispravnih(ctxRub, delta, 2, citaj)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pitano, ","); got != "a|02,a|04,b|02" {
		t.Errorf("pročitano iza: %s", got)
	}
	var ids []string
	for _, v := range out[len(delta):] {
		ids = append(ids, v.NodeID+"0"+kratkaOznaka(v.VersionID))
	}
	if got := strings.Join(ids, ","); got != "a03,a04,a05,a06,b03" || len(out) != len(delta)+5 {
		t.Errorf("dodano %s (ukupno %d)", got, len(out))
	}

	// bez zapelih ključeva delta ostaje ista i ništa se ne čita
	pitano = nil
	if out, err := prekoNeispravnih(ctxRub, delta[4:], 2, citaj); err != nil || len(out) != 3 || len(pitano) != 0 {
		t.Errorf("bez zapelih: %d verzija, pitano %v, %v", len(out), pitano, err)
	}
}

// Čitanje preko neispravnih ograđeno je brojem krugova, a greška čitanja
// prekida razgovor kao i greška delte.
func TestPrekoNeispravnihOgradaIGreska(t *testing.T) {
	delta := []ledger.Version{losaVerzija("a", "0000")}
	krugova := 0
	uvijekLose := func(_ context.Context, cvor, _, _ string, _ int) ([]ledger.Version, error) {
		krugova++
		return []ledger.Version{losaVerzija(cvor, fmt.Sprintf("%04d", krugova))}, nil
	}
	out, err := prekoNeispravnih(ctxRub, delta, 1, uvijekLose)
	if err != nil || krugova != najviseKrugovaPrekoNeispravnih || len(out) != 1+najviseKrugovaPrekoNeispravnih {
		t.Errorf("krugova %d, verzija %d, %v", krugova, len(out), err)
	}

	kvar := errors.New("knjiga se ne da pročitati")
	pukne := func(context.Context, string, string, string, int) ([]ledger.Version, error) { return nil, kvar }
	if out, err := prekoNeispravnih(ctxRub, delta, 1, pukne); !errors.Is(err, kvar) || out != nil {
		t.Errorf("greška čitanja: %d verzija, %v", len(out), err)
	}
}

// Kad iza granice primatelja u jednom ključu stoji punih 5000 neispravnih
// verzija, razmjena čita dalje i pošalje ispravnu iza njih, pa se granica
// primatelja pomakne. Bez toga je delta tog ključa bila samo od neispravnih,
// ništa se nije slalo i ključ je zapeo zauvijek. Isto vrijedi za verziju
// bez identifikatora zapisa (ispravan JSON): primatelj je od G3 preskoči bez
// pomaka granice, pa bi je pošiljatelj slao svakom razmjenom, a krug
// razmjena (syncPeers) išao bi do 200 razgovora zaredom s istih 5000.
func TestNeispravneVerzijeNeZaustavljajuKljuc(t *testing.T) {
	for _, p := range []struct{ ime, zapis, payload, dnevnik string }{
		{"nije JSON", "r-%d", "nije json", "u knjizi nije ispravan JSON"},
		{"bez identifikatora zapisa", "", `{"cm":120}`, "u knjizi nema oznaku, entitet ili identifikator zapisa"},
	} {
		t.Run(p.ime, func(t *testing.T) {
			neispravneNeZaustavljajuKljuc(t, p.zapis, p.payload, p.dnevnik)
		})
	}
}

// neispravneNeZaustavljajuKljuc: pošiljatelj ima u jednom ključu punu deltu
// neispravnih verzija (zapis je printf uzorak identifikatora zapisa) i iza
// njih jednu ispravnu; druga strana ima sve osim tog ključa
func neispravneNeZaustavljajuKljuc(t *testing.T, zapis, payload, uDnevniku string) {
	a, b := izdavacIPrimatelj(t)
	dnevnik := brojiDnevnik(t, uDnevniku)
	const kanal = "ocitanja/16/2026"
	if _, err := b.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
		SELECT printf('0190a000-0000-7000-8000-%012d', i), 'probni', ifnull(printf(?, i), ''), ?, '', 0, ?, ?, 1, ? FROM n`,
		NajviseVerzijaPoRazmjeni, zapis, b.node.ID, payload, time.Now().UTC(), kanal); err != nil {
		t.Fatal(err)
	}
	ispravna := fmt.Sprintf("0190a000-0000-7000-8000-%012d", NajviseVerzijaPoRazmjeni+1)
	upisiRedakKnjige(t, b, ispravna, kanal, `{"cm":120}`)
	kljuc := ledger.FrontierKey(b.node.ID, kanal)
	mine, err := b.rec.Frontier(ctxRub)
	if err != nil {
		t.Fatal(err)
	}
	granica := map[string]string{}
	for k := range mine {
		if k != kljuc {
			granica[k] = "~" // druga strana ima sve osim tog ključa
		}
	}

	var stiglo deltaMsg
	o := nazoviSkriptu(t, b, a, false, primiDeltu(granica, &stiglo))
	if o.err != nil {
		t.Fatalf("razmjena: %v", o.err)
	}
	if got := idoviVerzija(stiglo.Versions); got != ispravna || o.sent != 1 {
		t.Errorf("poslano %d: %.80s", o.sent, got)
	}
	if o.theirs.Frontier[kljuc] != ispravna {
		t.Errorf("zapamćena granica %q, očekivano %q", o.theirs.Frontier[kljuc], ispravna)
	}
	if n, _ := dnevnik.broj(); n != NajviseVerzijaPoRazmjeni {
		t.Errorf("u dnevniku %d redaka, očekivano %d", n, NajviseVerzijaPoRazmjeni)
	}
}
