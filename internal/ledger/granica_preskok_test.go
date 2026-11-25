package ledger

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"
)

// granicaGrupiranjem je granica onako kako se slagala prije preskakanja kroz
// indekse: jedan GROUP BY nad cijelom knjigom.
func granicaGrupiranjem(t *testing.T, r *Recorder) map[string]string {
	t.Helper()
	uvjet, args := uvjetGranice(Sada())
	rows, err := r.db.Query(`SELECT node_id, channel, MAX(version_id) FROM record_versions WHERE `+uvjet+` GROUP BY node_id, channel`, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, c, m string
		if err := rows.Scan(&n, &c, &m); err != nil {
			t.Fatal(err)
		}
		out[FrontierKey(n, c)] = m
	}
	return out
}

// Granica preskakanjem kroz indekse daje isto što i GROUP BY: više autora i
// kanala, zajednički kanal, i verzije koje granicu ne pomiču (krivi oblik,
// budućnost, | u autoru ili kanalu, autor kojem nijedna verzija ne vrijedi).
func TestGranicaPreskakanjemIstaKaoGrupiranjem(t *testing.T) {
	ctx := context.Background()
	sada := time.Now()
	db := openTestDB(t)
	p := New(db, "laptop-baranja")
	broj := 0
	ver := func(kad time.Time, autor, kanal string) Version {
		broj++
		id := oznakaU(kad, fmt.Sprintf("%012x", broj))
		return Version{VersionID: id, Entity: "readings", EntityID: id, NodeID: autor, Channel: kanal, CreatedAt: sada}
	}
	var ispravne []Version
	for i, autor := range []string{"cop-osijek", "laptop-baranja", "ured", "zz-zadnji"} {
		for j, kanal := range []string{"", "ocitanja/16/2026", "ocitanja/17/2026", "dnevnici/16/2026"} {
			if (i+j)%3 == 2 {
				continue // ne pišu svi autori u sve kanale
			}
			for k := range 5 {
				ispravne = append(ispravne, ver(sada.Add(-time.Duration(i*100+j*10+k)*time.Minute), autor, kanal))
			}
		}
	}
	if _, err := p.Apply(ctx, ispravne); err != nil {
		t.Fatal(err)
	}
	for _, v := range []Version{
		ver(sada.Add(-time.Hour), "cop-osijek|ocitanja/16/2026", ""),
		ver(sada.Add(-time.Hour), "ured", "ocitanja|16"),
		ver(sada.Add(NajviseUnaprijed+time.Minute), "cop-osijek", "ocitanja/16/2026"),
		ver(sada.Add(NajviseUnaprijed+time.Hour), "samo-buducnost", ""),
		{VersionID: "~", Entity: "readings", EntityID: "~", NodeID: "ured", Channel: "", CreatedAt: sada},
		{VersionID: "ffffffff-ffff-7fff-bfff-ffffffffffff", Entity: "readings", EntityID: "f", NodeID: "a-krivi", Channel: "x", CreatedAt: sada},
	} {
		ubaciMimoProvjere(t, db, v)
	}
	granica, err := p.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ocekivano := granicaGrupiranjem(t, p)
	if len(ocekivano) < 10 || !maps.Equal(granica, ocekivano) {
		t.Errorf("granica preskakanjem\n%v\nGROUP BY\n%v", granica, ocekivano)
	}

	prazna, err := New(openTestDB(t), "novi").Frontier(ctx)
	if err != nil || len(prazna) != 0 {
		t.Errorf("prazna knjiga: %v %v", prazna, err)
	}
}
