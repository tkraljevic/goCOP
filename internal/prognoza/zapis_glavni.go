package prognoza

// Zapis letve glavnog lanca. Letva koju računa glavni lanac (Tokovi) smije imati
// redak u model_letve. On ne nosi brojeve, nego kaže iz čega se njezin račun
// namješta: ulaze, rezerve, razine i upisana kašnjenja. Brojevi ostaju u
// tablicama glavnog modela, a Pripremi model takvu letvu namješta iz retka
// umjesto iz programa. Letva bez retka radi kao i dosad.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// sifraLetve je šifra iz zapisa u programu, koji smije nositi i veličinu
// („letva:protok”).
func sifraLetve(zapis string) string {
	sifra, _, _ := strings.Cut(zapis, ":")
	return strings.TrimSpace(sifra)
}

// sifreLetvi su šifre iz popisa zapisa u programu.
func sifreLetvi(zapisi []string) []string {
	out := make([]string, 0, len(zapisi))
	for _, z := range zapisi {
		out = append(out, sifraLetve(z))
	}
	return out
}

// racuniLanca su računi glavnog lanca iz programa, po šifri letve.
func racuniLanca() map[string]Racun {
	out := map[string]Racun{}
	for _, t := range Tokovi {
		for _, r := range t.Racuni {
			out[sifraLetve(r.Letva)] = r
		}
	}
	return out
}

// GlavnaLetva javlja računa li letvu glavni lanac.
func GlavnaLetva(letva string) bool {
	_, ima := racuniLanca()[letva]
	return ima
}

// velicinaULancu je veličina u kojoj glavni lanac vodi letvu; prazno kad letva
// nije u lancu ili joj se veličina ne zna.
func velicinaULancu(letva string) string {
	r, ima := racuniLanca()[letva]
	if !ima {
		return ""
	}
	_, velicina, err := RastaviLetvu(r.Letva)
	if err != nil {
		return ""
	}
	return velicina
}

// ZapisIzLanca je račun letve glavnog lanca kako stoji u programu, u obliku
// zapisa: polazište za izmjenu na stranici.
func ZapisIzLanca(letva string) (ZapisLetve, bool) {
	r, ima := racuniLanca()[letva]
	if !ima {
		return ZapisLetve{}, false
	}
	rezerve := make([][]string, 0, len(Rezerve[letva]))
	for _, rez := range Rezerve[letva] {
		rezerve = append(rezerve, sifreLetvi(rez))
	}
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: letva, Metoda: MetodaLanac, Velicina: velicinaULancu(letva),
		Racun: RacunLetve{Ulazi: sifreLetvi(r.Ulazi), Rezerve: rezerve, Doseg: najveciDosegZapisa}}, true
}

// bezGlavnih miče iz redaka letve glavnog lanca: njih ne računa odvojeni račun.
func bezGlavnih[T any](redci map[string]T) {
	maps.DeleteFunc(redci, func(letva string, _ T) bool { return GlavnaLetva(letva) })
}

// GlavniZapisi su valjani redci letvi glavnog lanca. Redak koji ne prolazi
// provjeru vraća se među odbijene, s razlogom; njegova letva radi iz programa.
func GlavniZapisi(baza *sql.DB, o OkvirModela) (map[string]ZapisLetve, map[string]error, error) {
	svi, pokvareni, err := ZapisiLetvi(baza)
	if err != nil {
		return nil, nil, err
	}
	glavni, odbijeni := map[string]ZapisLetve{}, map[string]error{}
	for letva, razlog := range pokvareni {
		if GlavnaLetva(letva) {
			odbijeni[letva] = razlog
		}
	}
	for letva, z := range svi {
		if !GlavnaLetva(letva) {
			continue
		}
		if err := z.ProvjeriGlavni(o, svi); err != nil {
			odbijeni[letva] = err
			continue
		}
		glavni[letva] = z
	}
	return glavni, odbijeni, nil
}

// ProvjeriGlavni odbija zapis letve glavnog lanca koji se ne smije namjestiti.
// zapisi su svi redci modela; po njima se zna koje letve imaju odvojen račun.
func (z ZapisLetve) ProvjeriGlavni(o OkvirModela, zapisi map[string]ZapisLetve) error {
	z.Racun.Doseg, z.Brojevi = najveciDosegZapisa, BrojeviLetve{}
	for _, korak := range []func() error{
		z.provjeriOblik, z.provjeriRacun, z.Racun.provjeriPonavljanje, z.provjeriIskljuceneIRezerve,
		z.provjeriOkvirLanca,
		func() error { return z.provjeriUlazeLanca(zapisi) },
		func() error { return z.provjeriRegistar(o) },
	} {
		if err := korak(); err != nil {
			return err
		}
	}
	return nil
}

// provjeriOkvirLanca gleda ono što se letvi glavnog lanca ovdje ne mijenja.
func (z ZapisLetve) provjeriOkvirLanca() error {
	switch {
	case !GlavnaLetva(z.Letva):
		return errors.New("letva nije u glavnom lancu")
	case z.Velicina != velicinaULancu(z.Letva):
		return fmt.Errorf("letva se u glavnom lancu vodi kao %s; to se ovdje ne mijenja", velicinaULancu(z.Letva))
	case z.Racun.Rucno.Nagib != nil:
		return errors.New("letva glavnog lanca namješta se iz arhive; nagib se ne upisuje")
	}
	return nil
}

// provjeriUlazeLanca odbija ulaz koji glavni lanac ne smije čitati: samu letvu,
// letvu s odvojenim računom i ulaz koji bi zatvorio krug kroz lanac.
func (z ZapisLetve) provjeriUlazeLanca(zapisi map[string]ZapisLetve) error {
	odvojene := map[string]bool{}
	for _, r := range OdvojeniRacuni {
		odvojene[sifraLetve(r.Letva)] = true
	}
	for letva := range zapisi {
		odvojene[letva] = !GlavnaLetva(letva)
	}
	for _, u := range z.letveUlaza() {
		switch {
		case u == z.Letva:
			return errors.New("letva ne može biti sama sebi ulaz")
		case odvojene[u]:
			return fmt.Errorf("letva %s ima odvojen račun, a glavni lanac takvu ne smije čitati", u)
		}
	}
	if slices.Contains(kruzniRacuni(lanacSaZapisom(zapisi, z)), z.Letva) {
		return errKrug
	}
	return nil
}

// lanacSaZapisom je glavni lanac kao skup zapisa: računi iz programa, preko njih
// redci letvi lanca, a preko svega zadani zapis.
func lanacSaZapisom(zapisi map[string]ZapisLetve, z ZapisLetve) map[string]ZapisLetve {
	lanac := map[string]ZapisLetve{}
	for letva := range racuniLanca() {
		lanac[letva], _ = ZapisIzLanca(letva)
		if redak, ima := zapisi[letva]; ima {
			lanac[letva] = ZapisLetve{Letva: letva, Racun: redak.Racun}
		}
	}
	lanac[z.Letva] = ZapisLetve{Letva: z.Letva, Racun: z.Racun}
	return lanac
}

// namjestiGlavnu namješta račun letve glavnog lanca iz zapisa: ulazi, rezerve,
// razine i upisana kašnjenja su iz zapisa, a granice sati kao i ostatku lanca.
// Poziva se dok se drži namjestanjeTraje.
func namjestiGlavnu(arhiva *sql.DB, z ZapisLetve, glavne map[string][][]Pojas) (PripremaLetve, error) {
	if RijetkeLetve[z.Letva] {
		defer postaviGraniceSati(100, 20)()
	}
	okvir := OkvirModela{Glavne: glavne,
		ImaSatniProtok: func(letva string) bool { return ImaSatniProtok(context.Background(), arhiva, letva) }}
	s := &pripremaRacuna{arhiva: arhiva, z: z, glavni: true, okvir: okvir}
	inacice, ucenje, cvorovi, napomene, err := s.namjestiInacice()
	if err != nil {
		return PripremaLetve{}, err
	}
	z.Brojevi, z.Racun.Doseg = BrojeviLetve{Inacice: inacice}, najveciDosegZapisa
	return PripremaLetve{Zapis: z, Ucenje: ucenje, Cvorovi: cvorovi, Napomene: napomene}, nil
}

// namjestiIzZapisa vraća pojase svih inačica računa iz zapisa i napomene.
func namjestiIzZapisa(arhiva *sql.DB, z ZapisLetve, glavne map[string][][]Pojas) ([]Pojas, []string, error) {
	p, err := namjestiGlavnu(arhiva, z, glavne)
	return slices.Concat(p.Zapis.Brojevi.Inacice...), p.Napomene, err
}

// namjestiRacunLanca namješta jedan račun lanca: iz retka letve kad ga ima,
// inače iz programa. Redak koji se ne da namjestiti samo se napiše, a račun ide
// iz programa, da letva zbog retka ne ostane bez računa; greška čitanja arhive
// prekida.
func namjestiRacunLanca(arhiva *sql.DB, r Racun, zapisi map[string]ZapisLetve, glavne map[string][][]Pojas, w io.Writer) ([]Pojas, error) {
	z, ima := zapisi[sifraLetve(r.Letva)]
	if !ima {
		return namjestiRacun(arhiva, r, w)
	}
	zaglavlje := fmt.Sprintf("%-56s", z.Letva+" ← "+strings.Join(z.Racun.Ulazi, " + ")+" (iz zapisa)")
	pojasi, napomene, err := namjestiIzZapisa(arhiva, z, glavne)
	if errors.Is(err, ErrArhiva) {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(zaglavlje), err)
	}
	if err != nil {
		fmt.Fprintf(w, "%s %v; račun ide iz programa\n", zaglavlje, err)
		return namjestiRacun(arhiva, r, w)
	}
	fmt.Fprintf(w, "%s %s\n", zaglavlje, z.Velicina)
	for _, n := range napomene {
		fmt.Fprintf(w, "   %s\n", n)
	}
	ispisiPojase(w, pojasi, z.Letva)
	return pojasi, nil
}

// zapisiZaNamjestanje čita retke letvi glavnog lanca prije namještanja i napiše
// koje redak mijenja, a koji je redak odbijen pa letva ide iz programa.
func zapisiZaNamjestanje(baza *sql.DB, w io.Writer) (map[string]ZapisLetve, map[string][][]Pojas, error) {
	glavne, err := SveInacice(baza)
	if err != nil {
		return nil, nil, err
	}
	zapisi, odbijeni, err := GlavniZapisi(baza, OkvirModela{Glavne: glavne})
	if err != nil {
		return nil, nil, err
	}
	for _, letva := range slices.Sorted(maps.Keys(odbijeni)) {
		fmt.Fprintf(w, "zapis letve %s se ne koristi (%v); račun ide iz programa\n", letva, odbijeni[letva])
	}
	return zapisi, glavne, nil
}
