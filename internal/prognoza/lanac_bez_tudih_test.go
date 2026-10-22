package prognoza

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Lanac ne čita nijednu tuđu prognozu (Odluke (22. 10. 2026.)): ni kad je
// svježa i stoji u bazi, vrh lanca ne slijedi nju, a letva sa svojim računom
// se računa. Sve vrijednosti su izmišljene.

// probniLanac slaže lanac zadanih pojasa s izmišljenim očitanjima letvi do
// zadnji (ocitanjaLetvi: u zadnjem satu 100 cm).
func probniLanac(t *testing.T, zadnji time.Time, letve []string, pojasi []Pojas) *Osvjezivac {
	t.Helper()
	probne := make([]probnaLetva, 0, len(letve))
	for _, l := range letve {
		probne = append(probne, probnaLetva{sifra: l})
	}
	ocitanja := ocitanjaLetvi(t, zadnji, probne...)
	baza := otvoriProbnu(t, "p.db")
	if err := Spremi(baza, pojasi, "proba"); err != nil {
		t.Fatal(err)
	}
	return &Osvjezivac{Baza: baza, Ocitanja: ocitanja, Najdalje: 12, Model: ModelLanac, Iznova: true}
}

// karika je pojas letve koja se računa iz jednog ulaza, s kašnjenjem pomak i
// nagibom 1.
func karika(letva, ulaz string, pomak int) Pojas {
	return Pojas{Letva: letva, Velicina: "vodostaj", Od: -1000, Do: 1000, Rasap: 3,
		Ulazi: []Ulaz{{Letva: ulaz, Velicina: "vodostaj", PomakH: pomak, Sirina: 1, Nagib: 1}}}
}

// upisiTudu upisuje tuđu prognozu letve izdanu u satu izdano, za sate od
// izdano+prvi do izdano+zadnji: od vrijednosti v0 raste korak po satu.
func upisiTudu(t *testing.T, baza *sql.DB, izvor, letva string, izdano, prvi, zadnji int64, v0, korak float64) {
	t.Helper()
	for h := prvi; h <= zadnji; h++ {
		if _, err := baza.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost) VALUES (?,?,?,?,?)`,
			izvor, letva, izdano, izdano+h, v0+korak*float64(h-prvi)); err != nil {
			t.Fatal(err)
		}
	}
}

// Wildungsmauer, vrh Dunava, ne slijedi austrijsku prognozu: ni kad je
// svježa stoji u bazi, Nagybajcs se računa iz zadnjeg mjerenja vrha. Ni kad je
// nema, uz izdanje nema upozorenja da vrhu fali budućnost.
func TestWildungsmauerNeSlijediAustrijskuPrognozu(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	o := probniLanac(t, zadnji, []string{"wildungsmauer", "nagybajcs"}, []Pojas{karika("nagybajcs", "wildungsmauer", 3)})
	// austrijska za Wildungsmauer izdana u zadnjem satu: raste 10 cm na sat
	upisiTudu(t, o.Baza, PodrijetloNOEL, "wildungsmauer", zadnji.Unix()/3600, 0, 12, 100, 10)
	bezAustrijske := func(ime string, ishod *Ishod) {
		t.Helper()
		if iz, ima := ishod.Izbor["vrh:wildungsmauer"]; ima {
			t.Errorf("%s: Wildungsmauer je dobio budućnost: %+v", ime, iz)
		}
		if _, kraj := izdanihLetve(ishod, "nagybajcs"); kraj == nil || kraj.Vrijednost != 100 {
			t.Errorf("%s: Nagybajcs za 12 h mora stajati na zadnjem mjerenju vrha (100): %+v", ime, kraj)
		}
		for _, u := range ishod.Upozorenja {
			if u.Letva == "wildungsmauer" || strings.Contains(u.Opis, PodrijetloNOEL) {
				t.Errorf("%s: upozorenje o austrijskoj prognozi: %+v", ime, u)
			}
		}
	}

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bezAustrijske("austrijska u bazi", ishod)

	if _, err := o.Baza.Exec(`DELETE FROM tude`); err != nil {
		t.Fatal(err)
	}
	if ishod, err = o.Osvjezi(context.Background()); err != nil {
		t.Fatal(err)
	}
	bezAustrijske("austrijske nema", ishod)
}

// Komárom računa naš lanac iz Nagybajcsa i Wildungsmauera i kad u bazi stoji
// svježa mađarska prognoza Komároma koja pokriva sat izdanja i naglo raste:
// lanac je ne čita, pa ni Esztergom niže ne raste. Mađarska ostaje zapisana,
// za usporedbu.
func TestKomaromRacunaNasLanacIUzMadjarsku(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	o := probniLanac(t, zadnji, []string{"wildungsmauer", "nagybajcs", "komarom", "esztergom"}, []Pojas{
		karika("nagybajcs", "wildungsmauer", 3), karika("komarom", "nagybajcs", 2), karika("esztergom", "komarom", 2)})
	// izdana dva sata prije sata izdanja, od tada raste 10 cm na sat
	upisiTudu(t, o.Baza, Podrijetlo, "komarom", sada-2, 0, 74, 100, 10)

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ishod.Sada != sada {
		t.Fatalf("sat izdanja %d, želim %d", ishod.Sada, sada)
	}
	if iz, ima := ishod.Izbor["vrh:komarom"]; ima {
		t.Errorf("Komárom je vrh lanca: %+v", iz)
	}
	for _, letva := range []string{"komarom", "esztergom"} {
		if n, kraj := izdanihLetve(ishod, letva); n < 12 || kraj == nil || kraj.Vrijednost != 100 {
			t.Errorf("%s za 12 h mora ići za zadnjim mjerenjem Wildungsmauera (100), ne za mađarskom: izdanih %d, %+v",
				letva, n, kraj)
		}
	}
	n, ok, err := TudaPrognoza(o.Baza, Podrijetlo, "komarom", sada-48, sada)
	if v, _ := n.U(sada + 12); err != nil || !ok || v != 240 {
		t.Errorf("mađarska prognoza Komároma mora ostati zapisana: ok %v, %v, za 12 h %g", ok, err, v)
	}
}

// Letenye, vrh lanca Mure, budućnost dobiva samo iz našeg dnevnog modela.
// Kad je on ne da (ovdje nema arhive, pa ni dnevnog modela), Letenye drži
// zadnje mjerenje i uz izdanje stoji upozorenje, iako u bazi stoji svježa
// mađarska prognoza Letenyea.
func TestLetenyeBezDnevnogDrziZadnjeMjerenje(t *testing.T) {
	postavi(t, &PoluvijekIspravka, 0)
	zadnji := time.Now().UTC().Truncate(time.Hour)
	sada := zadnji.Unix() / 3600
	o := probniLanac(t, zadnji, []string{"letenye", "kotoriba"}, []Pojas{karika("kotoriba", "letenye", 3)})
	upisiTudu(t, o.Baza, Podrijetlo, "letenye", sada-2, 0, 74, 100, 10)

	ishod, err := o.Osvjezi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if iz, ima := ishod.Izbor["vrh:letenye"]; ima || len(ishod.NasiVrhovi) != 0 {
		t.Errorf("Letenye je dobio budućnost: %+v, naši vrhovi %v", iz, ishod.NasiVrhovi)
	}
	if _, kraj := izdanihLetve(ishod, "kotoriba"); kraj == nil || kraj.Vrijednost != 100 {
		t.Errorf("Kotoriba za 12 h mora stajati na zadnjem mjerenju Letenyea (100): %+v", kraj)
	}
	want := Upozorenje{Vrsta: VrstaVrh, Tezina: TezinaUpozorenje, Letva: "letenye",
		Opis: "nema budućnosti vrha (dnevni model); drži se zadnje mjerenje"}
	nadjeno := false
	for _, u := range ishod.Upozorenja {
		if u.Vrsta == want.Vrsta && u.Letva == want.Letva {
			nadjeno = u.Tezina == want.Tezina && u.Opis == want.Opis
		}
	}
	if !nadjeno {
		t.Errorf("uz izdanje nema upozorenja %+v: %+v", want, ishod.Upozorenja)
	}
}
