package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// urednikModela sprema nacrte modela prognoze za stranicu „Slaganje računa”.
// Model se slaže samo na računalu koje izdaje prognozu: nacrt se ondje priprema
// i primjenjuje, a ostalima model stiže razmjenom.
type urednikModela struct {
	pb, arhiva, ocitanja *sql.DB
	mreza                ulogeCvora
	// krug kaže traje li satni krug prognoze; dok traje, model se ne mijenja, da
	// izdanje ne uhvati pola promjene. nil se ne pita.
	krug interface{ UTijeku() bool }
	// izdaj izda prognozu upravo primijenjenim modelom i javi grešku ako letva
	// u njoj nema prognozu. Treba ga primjena na glavni lanac; nil ne izdaje.
	izdaj func(ctx context.Context, letva string) error
}

var errKrugTraje = errors.New("satni krug prognoze upravo traje; pokušaj za minutu")

// smijeMijenjatiModel kaže smije li se model sada mijenjati na ovom računalu.
func (u *urednikModela) smijeMijenjatiModel() error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	if u.krug != nil && u.krug.UTijeku() {
		return errKrugTraje
	}
	return nil
}

// PrimijeniNacrt upiše probani račun iz nacrta kao model letve, s imenom,
// vremenom i razlogom.
func (u *urednikModela) PrimijeniNacrt(letva, tko, razlog string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	promjena := prognoza.PromjenaLetve{Kad: time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"), Tko: tko, Razlog: razlog}
	if prognoza.GlavnaLetva(letva) {
		return u.primijeniGlavni(letva, promjena)
	}
	return prognoza.PrimijeniNacrt(u.pb, letva, promjena, u.okvir())
}

// primijeniGlavniNacrt upisuje račun letve glavnog lanca u model; testovi ga
// zamjenjuju, jer pravi traži probu na arhivi sa satnim nizovima.
var primijeniGlavniNacrt = prognoza.PrimijeniGlavniNacrt

// primijeniGlavni upiše probani račun letve glavnog lanca u model i odmah izda
// prognozu njime. Kad izdavanje ne uspije ili letva u izdanju nema prognozu,
// vrati model kakav je bio; nacrt tada ostaje, s probom.
func (u *urednikModela) primijeniGlavni(letva string, promjena prognoza.PromjenaLetve) error {
	radna, pocisti, err := privremenaBazaPrognoza()
	if err != nil {
		return err
	}
	defer pocisti()
	prije, err := primijeniGlavniNacrt(u.pb, radna, letva, promjena, u.okvir())
	if err != nil {
		return err
	}
	if u.izdaj != nil {
		if err := u.izdaj(context.Background(), letva); err != nil {
			return vratiModel(u.pb, prije, err)
		}
	}
	return prognoza.ObrisiNacrt(u.pb, letva)
}

// vratiModel vraća model kakav je bio prije primjene i složi poruku o tome zašto.
func vratiModel(pb *sql.DB, prije prognoza.PaketModela, razlog error) error {
	if err := prognoza.PrimiModel(pb, prije); err != nil {
		return fmt.Errorf("izdavanje novim modelom nije uspjelo (%v), a prethodni model se nije dao vratiti: %w", razlog, err)
	}
	return fmt.Errorf("model nije primijenjen, vraćen je prethodni: izdavanje novim modelom nije uspjelo (%w)", razlog)
}

// izdajPrimijenjenim je izdavanje prognoze nakon primjene na glavni lanac: račun
// iznova za isti sat, zapis i objava. Letva bez prognoze u tom izdanju je greška.
func izdajPrimijenjenim(osvjezivac *prognoza.Osvjezivac, objaviIzdanje func(context.Context, *prognoza.Ishod)) func(context.Context, string) error {
	return func(ctx context.Context, letva string) error {
		iznova := *osvjezivac
		iznova.Iznova = true
		ishod, err := iznova.Osvjezi(ctx)
		if err != nil {
			return err
		}
		if err := ishod.BezPrognoze[letva]; err != nil {
			return fmt.Errorf("letva s novim računom nema prognozu: %w", err)
		}
		if err := iznova.Zapisi(ishod); err != nil {
			return err
		}
		objaviIzdanje(ctx, ishod)
		return nil
	}
}

// VratiPrethodni vraća prethodni model letve umjesto sadašnjeg, s imenom,
// vremenom i razlogom.
func (u *urednikModela) VratiPrethodni(letva, tko, razlog string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	promjena := prognoza.PromjenaLetve{Kad: time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"), Tko: tko, Razlog: razlog}
	return prognoza.VratiPrethodniZapis(u.pb, letva, promjena, u.okvir())
}

// UkloniModel briše primijenjeni model letve.
func (u *urednikModela) UkloniModel(letva string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	return prognoza.UkloniZapisLetve(u.pb, letva)
}

// ulogeCvora je dio mreže koji kaže što ovo računalo trenutno radi; pita se pri
// svakom pozivu, jer se uloga mijenja dok program radi.
type ulogeCvora interface{ TrenutneUloge() peers.Uloge }

var errNeIzdaje = errors.New("model se slaže na računalu koje izdaje prognozu")

func (u *urednikModela) Izdaje() bool { return u.mreza.TrenutneUloge().Izdaje }

// okvir je ono što provjera nacrta čita iz registra i arhive.
func (u *urednikModela) okvir() prognoza.OkvirModela {
	ctx := context.Background()
	return prognoza.OkvirModela{
		ImaSektor:      func(letva string) bool { return prognoza.LetvaImaSektor(ctx, u.ocitanja, letva) },
		ImaSatniProtok: func(letva string) bool { return prognoza.ImaSatniProtok(ctx, u.arhiva, letva) },
	}
}

func (u *urednikModela) SpremiNacrt(n prognoza.NacrtLetve) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.SpremiNacrt(u.pb, n, u.okvir())
}

func (u *urednikModela) ObrisiNacrt(letva string) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.ObrisiNacrt(u.pb, letva)
}

// pripremiNacrt namješta račun iz nacrta; testovi ga zamjenjuju, jer pravi traži
// arhivu sa satnim nizovima.
var pripremiNacrt = prognoza.PripremiNacrt

// PripremiNacrt namjesti račun iz spremljenog nacrta letve i upiše brojeve u
// nacrt. Model i prognoza se ne mijenjaju.
func (u *urednikModela) PripremiNacrt(letva string, p *poslovi.Posao) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	p.Korak("namještanje računa iz arhive", 0, 0)
	n, err := pripremiNacrt(u.arhiva, u.pb, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"), u.okvir())
	if err != nil {
		return err
	}
	p.Zavrsi(opisPripremeNacrta(n), nil)
	return nil
}

// opisPripremeNacrta je rečenica na kraju posla: koliko razina računa i dokle.
func opisPripremeNacrta(n prognoza.NacrtLetve) string {
	razina := 0
	if len(n.Zapis.Brojevi.Inacice) > 0 {
		razina = len(n.Zapis.Brojevi.Inacice[0])
	}
	return fmt.Sprintf("Račun je pripremljen: razina s računom %d, računa do %d h unaprijed. Prognoza se ne mijenja dok se model ne primijeni.",
		razina, n.Zapis.Racun.Doseg)
}

// probajNacrt mjeri račun iz nacrta unatrag; testovi ga zamjenjuju, jer pravi
// traži arhivu sa satnim nizovima.
var probajNacrt = prognoza.ProbajNacrt

// ProbajNacrt mjeri pripremljeni račun iz nacrta unatrag i upiše ishod u nacrt.
// Proba piše u privremenu bazu koja se poslije briše; model i prognoza se ne
// mijenjaju.
func (u *urednikModela) ProbajNacrt(letva string, p *poslovi.Posao) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	if u.pb != nil && prognoza.MetodaNacrta(u.pb, letva) == prognoza.MetodaDnevni {
		return u.probajDnevni(letva, p)
	}
	if prognoza.GlavnaLetva(letva) {
		return u.probajGlavni(letva, p)
	}
	privremena, pocisti, err := privremenaBazaPrognoza()
	if err != nil {
		return err
	}
	defer pocisti()
	p.Korak("mjerenje računa unatrag", 0, 0)
	n, err := probajNacrt(u.arhiva, u.pb, privremena, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"))
	if err != nil {
		return err
	}
	p.Zavrsi(opisProbeNacrta(n), nil)
	return nil
}

// probajGlavniNacrt mjeri račun letve glavnog lanca; testovi ga zamjenjuju, jer
// pravi traži arhivu sa satnim nizovima.
var probajGlavniNacrt = prognoza.ProbajGlavniNacrt

// probajGlavni proba račun letve glavnog lanca iz nacrta: cijeli lanac mjeri se
// dvaput, sadašnjim modelom i modelom s izmjenom, u dvije privremene baze.
func (u *urednikModela) probajGlavni(letva string, p *poslovi.Posao) error {
	prije, pocistiPrije, err := privremenaBazaPrognoza()
	if err != nil {
		return err
	}
	defer pocistiPrije()
	poslije, pocistiPoslije, err := privremenaBazaPrognoza()
	if err != nil {
		return err
	}
	defer pocistiPoslije()
	n, err := probajGlavniNacrt(u.arhiva, u.pb, prije, poslije, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"),
		func(sto string) { p.Korak(sto, 0, 0) })
	if err != nil {
		return err
	}
	p.Zavrsi(opisProbeGlavnog(n), nil)
	return nil
}

// opisProbeGlavnog je rečenica na kraju probe letve glavnog lanca.
func opisProbeGlavnog(n prognoza.NacrtLetve) string {
	if n.Proba == nil || n.Proba.Lanac == nil {
		return "Proba je gotova."
	}
	letve := map[string]bool{}
	for _, x := range n.Proba.Lanac.Nizvodno {
		letve[x.Letva] = true
	}
	if len(letve) == 0 {
		return "Proba je gotova: ostalim letvama se promašaj nije promijenio. Prognoza se ne mijenja dok se model ne primijeni."
	}
	return fmt.Sprintf("Proba je gotova: promašaj se promijenio na još %d %s. Prognoza se ne mijenja dok se model ne primijeni.",
		len(letve), models.UzBrojHR(len(letve), "letvi", "letve", "letava"))
}

// probajDnevniNacrt proba dnevni model iz nacrta; testovi ga zamjenjuju, jer
// pravi traži arhivu s dnevnim nizovima.
var probajDnevniNacrt = prognoza.ProbajDnevniNacrt

// probajDnevni proba dnevni model iz nacrta letve: uči ga na arhivi bez zadnje
// dvije godine i mjeri na njima. Kišomjeri se čitaju iz registra.
func (u *urednikModela) probajDnevni(letva string, p *poslovi.Posao) error {
	tocke, err := u.tockeRegistra()
	if err != nil {
		return err
	}
	p.Korak("učenje i mjerenje dnevnog modela", 0, 0)
	n, err := probajDnevniNacrt(u.arhiva, u.pb, tocke, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"))
	if err != nil {
		return err
	}
	p.Zavrsi(opisProbeDnevnog(n), nil)
	return nil
}

// tockeRegistra su kišomjeri registra iz kojih dnevni model uči kišu; bez
// registra ih nema.
func (u *urednikModela) tockeRegistra() ([]prognoza.OborinskaTocka, error) {
	if u.ocitanja == nil {
		return nil, nil
	}
	tocke, err := prognoza.TockeSKisomjerima(u.ocitanja)
	if err != nil {
		return nil, fmt.Errorf("kišomjeri iz registra: %w", err)
	}
	return tocke, nil
}

// SpremiDnevni sprema nacrt dnevnog bloka letve dnevnog modela Drave i Dunava.
func (u *urednikModela) SpremiDnevni(letva string, blok prognoza.DnevniLetve, tko string) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.SpremiDnevniNacrt(u.pb, prognoza.NacrtDnevnog{Letva: letva, Blok: blok, Tko: tko,
		Kad: time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04")})
}

// ObrisiDnevniNacrt odbacuje nacrt dnevnog bloka letve.
func (u *urednikModela) ObrisiDnevniNacrt(letva string) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	return prognoza.ObrisiDnevniNacrt(u.pb, letva)
}

// probajDnevniGlavni proba dnevni blok iz nacrta; testovi ga zamjenjuju, jer
// pravi traži arhivu s dnevnim nizovima.
var probajDnevniGlavni = prognoza.ProbajDnevniGlavni

// ProbajDnevniGlavni proba dnevni blok iz nacrta letve: mjeri nove ulaze i
// sadašnje na zadnjim godinama arhive. Model i prognoza se ne mijenjaju.
func (u *urednikModela) ProbajDnevniGlavni(letva string, p *poslovi.Posao) error {
	if !u.Izdaje() {
		return errNeIzdaje
	}
	tocke, err := u.tockeRegistra()
	if err != nil {
		return err
	}
	p.Korak("učenje i mjerenje dnevnog modela", 0, 0)
	n, err := probajDnevniGlavni(u.arhiva, u.pb, tocke, letva, time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"))
	if err != nil {
		return err
	}
	p.Zavrsi(opisProbeBloka(n), nil)
	return nil
}

// opisProbeBloka je rečenica na kraju probe dnevnog bloka.
func opisProbeBloka(n prognoza.NacrtDnevnog) string {
	if n.Proba == nil {
		return "Proba je gotova."
	}
	bolje := 0
	for _, d := range n.Proba.Dani {
		if d.Stanje == prognoza.MjeraBolje {
			bolje++
		}
	}
	return fmt.Sprintf("Proba je gotova: dnevni model je pouzdaniji od zadnjeg mjerenja na %d od %d dana. Prognoza se ne mijenja dok se model ne primijeni.",
		bolje, len(n.Proba.Dani))
}

// PrimijeniDnevni upiše probani dnevni blok u model letve.
func (u *urednikModela) PrimijeniDnevni(letva, tko, razlog string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	return prognoza.PrimijeniDnevniGlavni(u.pb, letva, u.promjena(tko, razlog))
}

// UkloniDnevni miče dnevni blok iz modela letve, pa se njezin dnevni model opet
// uči iz programa.
func (u *urednikModela) UkloniDnevni(letva, tko, razlog string) error {
	if err := u.smijeMijenjatiModel(); err != nil {
		return err
	}
	return prognoza.UkloniDnevniGlavni(u.pb, letva, u.promjena(tko, razlog))
}

// promjena je zapis o tome tko, kad i zašto mijenja model.
func (u *urednikModela) promjena(tko, razlog string) prognoza.PromjenaLetve {
	return prognoza.PromjenaLetve{Kad: time.Now().In(models.Zagreb).Format("2. 1. 2006. 15:04"), Tko: tko, Razlog: razlog}
}

// opisProbeDnevnog je rečenica na kraju probe dnevnog modela.
func opisProbeDnevnog(n prognoza.NacrtLetve) string {
	if n.Proba == nil {
		return "Proba je gotova."
	}
	bolje := 0
	for _, d := range n.Proba.Dani {
		if d.Stanje == prognoza.MjeraBolje {
			bolje++
		}
	}
	return fmt.Sprintf("Proba je gotova: dnevni model je pouzdaniji od zadnjeg mjerenja na %d od %d dana.", bolje, len(n.Proba.Dani))
}

// privremenaBazaPrognoza otvara novu, praznu bazu prognoza u privremenoj mapi i
// vraća funkciju koja je zatvori i obriše.
func privremenaBazaPrognoza() (*sql.DB, func(), error) {
	mapa, err := os.MkdirTemp("", "gocop-proba-")
	if err != nil {
		return nil, nil, fmt.Errorf("privremena baza za probu: %w", err)
	}
	b, err := otvoriPrognoze(filepath.Join(mapa, "proba.db"))
	if err != nil {
		_ = os.RemoveAll(mapa) // mapa je privremena; ostatak počisti sustav
		return nil, nil, fmt.Errorf("privremena baza za probu: %w", err)
	}
	return b, func() {
		_ = b.Close()          // baza se odmah briše
		_ = os.RemoveAll(mapa) // mapa je privremena; ostatak počisti sustav
	}, nil
}

// otvoriPrognoze otvara bazu prognoza; testovi ga zamjenjuju da izazovu grešku.
var otvoriPrognoze = prognoza.Otvori

// opisProbeNacrta je rečenica na kraju probe: na koliko je dosega račun pouzdaniji
// od zadnjeg mjerenja.
func opisProbeNacrta(n prognoza.NacrtLetve) string {
	if n.Proba == nil {
		return "Proba je gotova."
	}
	bolje := 0
	for _, d := range n.Proba.Dosezi {
		if d.Stanje == prognoza.MjeraBolje {
			bolje++
		}
	}
	return fmt.Sprintf("Proba je gotova: račun je pouzdaniji od zadnjeg mjerenja na %d od %d dosega.", bolje, len(n.Proba.Dosezi))
}

// posluziteljModela je dio poslužitelja kojem se uključuje rad s modelom.
type posluziteljModela interface {
	SetPripremaModela(web.PripremaModela)
	SetUrednikModela(web.UrednikModela)
}

// ukljuciModel daje poslužitelju pripremu modela i urednika nacrta.
func ukljuciModel(s posluziteljModela, priprema web.PripremaModela, u *urednikModela) {
	s.SetPripremaModela(priprema)
	s.SetUrednikModela(u)
}
