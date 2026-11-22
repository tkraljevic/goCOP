package prognoza

// Primjena računa letve glavnog lanca. Novi model slaže se u privremenoj bazi:
// sadašnji model, u njemu račun letve iz nacrta, promašaji letvi kojima ih je
// proba promijenila i redak letve. Tek gotov prelazi u bazu prognoza, odjednom.
// Model kakav je bio vraća se pozivatelju, da ga može vratiti ako izdavanje
// novim modelom ne uspije.

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
)

// PrimijeniGlavniNacrt upiše probani račun letve glavnog lanca u model. radna
// je nova, prazna baza prognoza u kojoj se novi model slaže. Razlog je
// obavezan i ostaje uz model. Nacrt ostaje; briše ga pozivatelj kad izdavanje
// novim modelom uspije. Vraća model kakav je bio prije primjene.
func PrimijeniGlavniNacrt(baza, radna *sql.DB, letva string, promjena PromjenaLetve, o OkvirModela) (PaketModela, error) {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return PaketModela{}, errors.New("upiši razlog; ostaje uz model")
	}
	n, err := probaniGlavniNacrt(baza, letva)
	if err != nil {
		return PaketModela{}, err
	}
	prije, zapisi, err := modelZaPrimjenu(baza, n, o)
	if err != nil {
		return PaketModela{}, err
	}
	novi, err := sloziNoviModel(radna, prije, n, zapisZaGlavnu(n.Zapis, zapisi, promjena))
	if err != nil {
		return PaketModela{}, err
	}
	return prije, PrimiModel(baza, novi)
}

// modelZaPrimjenu čita model na koji se nacrt primjenjuje i njegove retke. Odbija
// kad to više nije model na kojem je proba mjerila ili kad nacrt više ne prolazi
// provjeru.
func modelZaPrimjenu(baza *sql.DB, n NacrtLetve, o OkvirModela) (PaketModela, map[string]ZapisLetve, error) {
	prije, otisak, err := SastaviModel(baza)
	if err != nil {
		return PaketModela{}, nil, err
	}
	if otisak != n.Proba.Lanac.Model {
		return PaketModela{}, nil, errors.New("model se promijenio nakon probe; probaj ponovno, pa primijeni")
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return PaketModela{}, nil, err
	}
	return prije, zapisi, n.Zapis.ProvjeriGlavni(o, zapisi)
}

// probaniGlavniNacrt je nacrt letve glavnog lanca koji je pripremljen i proban
// probom cijelog lanca.
func probaniGlavniNacrt(baza *sql.DB, letva string) (NacrtLetve, error) {
	n, err := pripremljeniGlavniNacrt(baza, letva)
	if err != nil {
		return NacrtLetve{}, err
	}
	if n.Proba == nil || n.Proba.Lanac == nil {
		return NacrtLetve{}, errors.New("račun nije proban; probaj ga, pa primijeni")
	}
	return n, nil
}

// zapisZaGlavnu je redak letve glavnog lanca za upis: definicija iz nacrta bez
// brojeva, s poviješću. Prije prvog retka prethodni je račun iz programa.
func zapisZaGlavnu(z ZapisLetve, zapisi map[string]ZapisLetve, promjena PromjenaLetve) ZapisLetve {
	z.Brojevi, z.Racun.Doseg = BrojeviLetve{}, najveciDosegZapisa
	stari := maps.Clone(zapisi)
	if _, ima := stari[z.Letva]; !ima {
		stari[z.Letva], _ = ZapisIzLanca(z.Letva)
	}
	return zapisZaPrimjenu(z, stari, promjena)
}

// sloziNoviModel slaže u radnoj bazi model s izmjenom i vraća ga: model kakav
// je sada, pa račun letve iz nacrta, promašaji koje je proba promijenila i
// redak letve.
func sloziNoviModel(radna *sql.DB, prije PaketModela, n NacrtLetve, redak ZapisLetve) (PaketModela, error) {
	for _, korak := range []func() error{
		func() error { return novaSModelom(radna, prije) },
		func() error { _, err := zamijeniRacun(radna, n.Zapis); return err },
		func() error { return zamijeniPromasaje(radna, n.Proba.Lanac.Promasaji, n.Proba.Kad) },
		func() error { return upisiZapisLetve(radna, redak, otisakProbe(n.Zapis)) },
	} {
		if err := korak(); err != nil {
			return PaketModela{}, err
		}
	}
	novi, _, err := SastaviModel(radna)
	return novi, err
}

// zamijeniPromasaje upisuje u bazu promašaje letvi iz probe umjesto zatečenih za
// iste letve; ostalim letvama promašaji ostaju. Ključ je „letva|veličina”.
func zamijeniPromasaje(baza *sql.DB, novi map[string]map[int]Promasaj, kad string) error {
	svi, err := Promasaji(baza)
	if err != nil {
		return err
	}
	var redom []Promasaj
	for iz, p := range svi {
		if _, zamijenjen := novi[kljucPromasaja(iz)]; !zamijenjen {
			redom = append(redom, slices.Collect(maps.Values(p))...)
		}
	}
	for _, p := range novi {
		redom = append(redom, slices.Collect(maps.Values(p))...)
	}
	slices.SortFunc(redom, func(a, b Promasaj) int {
		return cmp.Or(strings.Compare(a.Letva, b.Letva), strings.Compare(a.Velicina, b.Velicina), cmp.Compare(a.DosegH, b.DosegH))
	})
	return SpremiPromasaje(baza, redom, kad)
}

// PrethodniGlavni je račun koji je letva glavnog lanca imala prije sadašnjeg
// retka: prethodni redak, a prije prvog retka račun iz programa. Bez retka
// nema se na što vratiti.
func PrethodniGlavni(baza *sql.DB, letva string) (ZapisLetve, error) {
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return ZapisLetve{}, err
	}
	sadasnji, ima := zapisi[letva]
	var prethodni ZapisLetve
	if !ima || !GlavnaLetva(letva) || !ImaPrethodniZapis(sadasnji) || json.Unmarshal(sadasnji.Prethodni, &prethodni) != nil || prethodni.Letva != letva {
		return ZapisLetve{}, errors.New("model nema prethodnu inačicu na koju bi se vratio")
	}
	prethodni.Promjene, prethodni.Prethodni, prethodni.Brojevi = nil, nil, BrojeviLetve{}
	return prethodni, nil
}
