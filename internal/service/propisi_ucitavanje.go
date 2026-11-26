package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"gocop/internal/models"
	"gocop/internal/propisi"
	"gocop/internal/repository"
)

// Učitavanje propisa (Odluke (25. 11. 2026.)): globalni administrator upiše
// broj Narodnih novina i akta, zalijepi poveznicu ili tekst, pogleda podjelu
// i tek onda spremi. Sve upisne metode traže globalnog administratora i u
// servisu, ne samo na ruti.

// GreskaPropisa je rečenica za administratora kad učitavanje, spremanje ili
// uklanjanje nije uspjelo. Web je ispisuje kako jest; uzrok (greška
// dohvata) ide u dnevnik.
type GreskaPropisa string

func (g GreskaPropisa) Error() string { return string(g) }

// Rečenice kad učitavanje ili spremanje ne uspije; ništa nije spremljeno
const (
	ErrBezRazloga          GreskaPropisa = "Upišite razlog uklanjanja. Ništa nije uklonjeno."
	porukaKriviBroj        GreskaPropisa = "Broj NN upišite kao 84/2010, a broj akta kao 2389. Možete zalijepiti i poveznicu s narodne-novine.nn.hr."
	porukaNijeNN           GreskaPropisa = "Program dohvaća akte samo s narodne-novine.nn.hr. Zalijepite poveznicu s te stranice ili upišite broj NN i broj akta."
	porukaRazliciti        GreskaPropisa = "Broj NN i poveznica ne govore o istom aktu. Ostavite jedno od dvoje."
	porukaNemaTijela       GreskaPropisa = "Na stranici nema teksta akta. Provjerite brojeve ili zalijepite tekst."
	porukaRok              GreskaPropisa = "Narodne novine nisu odgovorile u 30 sekundi. Pokušajte ponovno ili zalijepite tekst."
	porukaNemaVeze         GreskaPropisa = "Ovo računalo ne može doći do Narodnih novina. Učitajte propis na računalu s internetom; ostala ga dobiju razmjenom."
	porukaPresretanje      GreskaPropisa = "Mreža ovog računala presreće vezu s Narodnim novinama, pa joj program ne vjeruje. Učitajte propis na drugoj mreži ili zalijepite tekst."
	porukaPrevelikaStr     GreskaPropisa = "Stranica akta veća je od 8 MB, pa je program nije pročitao. Zalijepite tekst akta."
	porukaBezNaziva        GreskaPropisa = "Upišite naziv propisa, pa podjela nije napravljena."
	porukaBezTeksta        GreskaPropisa = "Zalijepite tekst akta ili izaberite datoteku .txt."
	porukaKrivaAdresa      GreskaPropisa = "Stalna adresa smije imati samo mala slova bez kvačica, brojeve i crtice. Propis nije spremljen."
	porukaBezClanka        GreskaPropisa = "Program u tekstu nije našao nijedan članak ni točku. Propis nije spremljen. Provjerite je li to stranica akta."
	porukaPrevelikPropis   GreskaPropisa = "Propis je prevelik za razmjenu (više od 8 MB), pa nije spremljen."
	porukaCiljNijeUPropisu GreskaPropisa = "Izmjena nije spremljena: odabrani članak nije u propisu. Odaberite članak s popisa."
)

// najduljaSkupina je najviše znakova u nazivu skupine
const najduljaSkupina = 60

// UlazPropisa je ono što je administrator upisao u obrazac „Učitaj iz
// Narodnih novina” ili „Dodaj izmjenu”. IzTeksta je sporedni put: tekst je
// zalijepljen ili učitan iz datoteke .txt, pa se Narodne novine ne pitaju.
type UlazPropisa struct {
	BrojNN, BrojAkta, Poveznica     string
	Tekst, Naziv, Donositelj, Datum string
	Skupina                         string
	IzTeksta                        bool
}

// PregledPropisa je pročitan i podijeljen propis koji čeka potvrdu
type PregledPropisa struct {
	Propis    models.Propis
	Podjela   propisi.Podjela
	Opseg     propisi.Opseg
	Raspon    string         // „I.–XLV.” ili „1.–254.”
	Postoji   *models.Propis // isti akt je već učitan
	Znakova   int
	Neucitane []models.IzdanjeNN
}

// OpisTeksta je duljina teksta za karticu „Akt je dohvaćen”: „oko 44.000
// znakova”
func (p *PregledPropisa) OpisTeksta() string {
	n := p.Znakova
	if n >= 1000 {
		n = (n + 500) / 1000 * 1000
	}
	return "oko " + tisuceHR(n) + " " + models.UzBrojHR(n, "znak", "znaka", "znakova")
}

// tisuceHR piše broj s točkom iza tisuća: 44000 → „44.000”
func tisuceHR(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

// Objava je datum objave: „7. 7. 2010.”
func (p *PregledPropisa) Objava() string { return danHR(p.Propis.DatumObjave) }

// Stupa je dan stupanja na snagu: „7. 7. 2010.”
func (p *PregledPropisa) Stupa() string { return danHR(p.Propis.StupaNaSnagu) }

// Tekst su dijelovi propisa za prikaz na provjeri podjele
func (p *PregledPropisa) Tekst() []DioNaStranici { return naStranici(p.Propis.Dijelovi) }

// naStranici su dijelovi za ispis, bez oznaka izmjena i stalnih adresa
func naStranici(d []models.DioPropisa) []DioNaStranici {
	out := make([]DioNaStranici, 0, len(d))
	for _, dio := range d {
		out = append(out, DioNaStranici{DioPropisa: dio})
	}
	return out
}

// NaziviSkupina su stalne skupine propisa, redom kao na popisu
func NaziviSkupina() []string {
	out := make([]string, 0, len(stalneSkupine))
	for _, sk := range stalneSkupine {
		out = append(out, sk.Naziv)
	}
	return out
}

// Neprepoznatih je broj odlomaka koje čitač nije prepoznao
func (p *PregledPropisa) Neprepoznatih() int {
	n := 0
	for _, u := range p.Podjela.Upozorenja {
		if u.Vrsta == propisi.UpozorenjeNeprepoznato {
			n++
		}
	}
	return n
}

// Procitaj dohvaća akt iz Narodnih novina ili čita zalijepljen tekst, dijeli
// ga na dijelove i slaže propis za provjeru. Ništa se ne sprema.
func (s *PropisiService) Procitaj(ctx context.Context, perms *models.UserPermissions, u UlazPropisa) (*PregledPropisa, error) {
	if err := requireGlobalAdmin(perms, "učitavanje propisa"); err != nil {
		return nil, err
	}
	nn, pr, err := s.procitajAkt(ctx, u)
	if err != nil {
		return nil, err
	}
	podjela := propisi.Podijeli(pr.Odlomci)
	p := models.Propis{ID: nn.ID(), NN: nn, Naziv: pr.Naziv, Vrsta: pr.Vrsta, Donositelj: pr.Donositelj, ELI: pr.ELI,
		DatumObjave: pr.DatumObjave, Veze: pr.Veze, Izvor: izvorUlaza(u), Citac: propisi.InacicaCitaca,
		Dijelovi: podjela.Dijelovi, Skupina: skupinaIliOstali(u.Skupina)}
	p.StupaNaSnagu, _ = stupanje(podjela.Dijelovi, p.DatumObjave)
	pregled := &PregledPropisa{Podjela: podjela, Opseg: propisi.OpsegIz(p.Dijelovi), Raspon: rasponBrojeva(p.Dijelovi), Znakova: znakovaUDijelovima(p.Dijelovi)}
	if pregled.Postoji, err = s.repo.Get(ctx, p.ID); err != nil {
		return nil, err
	}
	var izmjene []models.IzmjenaPropisa
	if pregled.Postoji != nil {
		p.Adresa, p.CreatedAt = pregled.Postoji.Adresa, pregled.Postoji.CreatedAt
		if izmjene, err = s.repo.Izmjene(ctx, p.ID); err != nil {
			return nil, err
		}
	} else if p.Adresa, err = s.slobodnaAdresa(ctx, p); err != nil {
		return nil, err
	}
	pregled.Neucitane = neucitaneIzmjene(&p, izmjene)
	pregled.Propis = p
	return pregled, nil
}

// izvorUlaza: zalijepljen tekst ili stranica iz Narodnih novina
func izvorUlaza(u UlazPropisa) string {
	if u.IzTeksta {
		return models.IzvorPropisaTekst
	}
	return models.IzvorPropisaNN
}

// skupinaIliOstali je upisana skupina, najviše 60 znakova; bez nje Ostali
func skupinaIliOstali(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return skupinaOstali
	}
	if utf8.RuneCountInString(s) > najduljaSkupina {
		s = string([]rune(s)[:najduljaSkupina])
	}
	return s
}

// stupanje je dan stupanja na snagu („2010-07-07”) i izraz iz odredbe
func stupanje(d []models.DioPropisa, datumObjave string) (dan, izraz string) {
	objava, _ := time.Parse("2006-01-02", datumObjave) // bez datuma objave ostaje samo stalni datum
	t, izraz, ok := propisi.StupaNaSnagu(d, objava)
	if !ok {
		return "", izraz
	}
	return t.Format("2006-01-02"), izraz
}

// procitajAkt rastavi brojeve i poveznicu, pa dohvati stranicu akta ili
// pročita zalijepljen tekst
func (s *PropisiService) procitajAkt(ctx context.Context, u UlazPropisa) (models.IzdanjeNN, *propisi.Procitano, error) {
	nn, err := propisi.RastaviUlaz(u.BrojNN, u.BrojAkta, u.Poveznica)
	if err != nil {
		return nn, nil, porukaUlaza(err)
	}
	if u.IzTeksta {
		pr, err := procitajZalijepljeno(u)
		return nn, pr, err
	}
	pr, err := s.dohvatiAkt(ctx, nn)
	return nn, pr, err
}

// porukaUlaza je rečenica za krivo upisan broj ili poveznicu
func porukaUlaza(err error) error {
	switch {
	case errors.Is(err, propisi.ErrNijeNN):
		return porukaNijeNN
	case errors.Is(err, propisi.ErrRazliciti):
		return porukaRazliciti
	}
	return porukaKriviBroj
}

// procitajZalijepljeno čita zalijepljen tekst; naziv upisuje administrator
func procitajZalijepljeno(u UlazPropisa) (*propisi.Procitano, error) {
	if strings.TrimSpace(u.Tekst) == "" {
		return nil, porukaBezTeksta
	}
	if strings.TrimSpace(u.Naziv) == "" {
		return nil, porukaBezNaziva
	}
	pr := propisi.ProcitajTekst(u.Tekst)
	pr.Naziv, pr.Donositelj = strings.TrimSpace(u.Naziv), strings.TrimSpace(u.Donositelj)
	if _, err := time.Parse("2006-01-02", u.Datum); err == nil {
		pr.DatumObjave = u.Datum
	}
	return pr, nil
}

// dohvatiAkt dohvaća stranicu akta iz Narodnih novina i čita je
func (s *PropisiService) dohvatiAkt(ctx context.Context, nn models.IzdanjeNN) (*propisi.Procitano, error) {
	if s.dohvat == nil {
		return nil, porukaNemaVeze
	}
	b, err := s.dohvat.Akt(ctx, nn)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", porukaDohvata(err, nn), err)
	}
	pr, err := propisi.ProcitajHTML(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", porukaNemaTijela, err)
	}
	return pr, nil
}

// porukaDohvata je rečenica za grešku dohvata iz Narodnih novina
func porukaDohvata(err error, nn models.IzdanjeNN) GreskaPropisa {
	var mreza net.Error
	switch {
	case errors.Is(err, propisi.ErrNemaAkta):
		return GreskaPropisa(fmt.Sprintf("Narodne novine nemaju akt %d u %s. Provjerite brojeve ili zalijepite poveznicu.", nn.Akt, nn.Puno()))
	case errors.Is(err, propisi.ErrNemaTijelaAkta), errors.Is(err, propisi.ErrNijeHTML):
		return porukaNemaTijela
	case errors.Is(err, propisi.ErrPrevelikaStranica):
		return porukaPrevelikaStr
	case propisi.JePresretanje(err):
		return porukaPresretanje
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &mreza) && mreza.Timeout():
		return porukaRok
	}
	return porukaNemaVeze
}

// slobodnaAdresa je stalna adresa iz naziva; kad je zauzeta, dobije godinu
// izdanja („zakon-o-vodama-2019”)
func (s *PropisiService) slobodnaAdresa(ctx context.Context, p models.Propis) (string, error) {
	adresa := propisi.AdresaIzNaziva(p.Naziv)
	if adresa == "" {
		return p.ID, nil
	}
	zauzeta, err := s.adresaZauzeta(ctx, adresa, p.ID)
	if err != nil || !zauzeta {
		return adresa, err
	}
	return fmt.Sprintf("%s-%d", adresa, p.NN.Godina), nil
}

// adresaZauzeta: adresu koristi program ili drugi propis
func (s *PropisiService) adresaZauzeta(ctx context.Context, adresa, id string) (bool, error) {
	if adresa == propisi.RijecUcitavanja {
		return true, nil
	}
	drugi, err := s.repo.PoAdresi(ctx, adresa)
	return drugi != nil && drugi.ID != id, err
}

// rasponBrojeva su prvi i zadnji članak ili točka: „I.–XLV.”
func rasponBrojeva(d []models.DioPropisa) string {
	var prvi, zadnji string
	for _, dio := range d {
		if (dio.Vrsta == models.DioClanak || dio.Vrsta == models.DioTocka) && dio.Broj != "" {
			if prvi == "" {
				prvi = dio.Broj
			}
			zadnji = dio.Broj
		}
	}
	if prvi == zadnji {
		return prvi
	}
	return prvi + "–" + zadnji
}

// znakovaUDijelovima je duljina teksta svih dijelova, u znakovima
func znakovaUDijelovima(d []models.DioPropisa) int {
	n := 0
	for _, dio := range d {
		n += utf8.RuneCountInString(propisi.TekstDijela(dio))
	}
	return n
}

// OdabirPropisa je ono što administrator upiše na provjeri podjele
type OdabirPropisa struct {
	Adresa, Skupina string
}

// Spremi sprema propis iz pregleda. Novi propis dobije upisanu stalnu
// adresu; isti akt, učitan ponovno, zadrži staru adresu i izmjene.
func (s *PropisiService) Spremi(ctx context.Context, perms *models.UserPermissions, tko *models.User, p *PregledPropisa, o OdabirPropisa) (*models.Propis, error) {
	if err := requireGlobalAdmin(perms, "spremanje propisa"); err != nil {
		return nil, err
	}
	propis := p.Propis
	propis.Dijelovi = append([]models.DioPropisa(nil), p.Propis.Dijelovi...)
	if !imaClanak(propis.Dijelovi) {
		return nil, porukaBezClanka
	}
	propis.Skupina = skupinaIliOstali(o.Skupina)
	postoji, err := s.repo.Get(ctx, propis.ID)
	if err != nil {
		return nil, err
	}
	if postoji != nil {
		propis.Adresa, propis.CreatedAt = postoji.Adresa, postoji.CreatedAt
	} else if propis.Adresa, err = s.novaAdresa(ctx, o.Adresa, propis); err != nil {
		return nil, err
	}
	propis.Ucitao, propis.UcitaoID = tko.FullName, tko.ID.String()
	if err := s.repo.Spremi(ctx, &propis); err != nil {
		return nil, porukaSpremanja(err)
	}
	return &propis, nil
}

// imaClanak: propis ima barem jedan članak, točku ili prilog
func imaClanak(d []models.DioPropisa) bool {
	for _, dio := range d {
		if dio.Vrsta == models.DioClanak || dio.Vrsta == models.DioTocka || dio.Vrsta == models.DioPrilog {
			return true
		}
	}
	return false
}

// novaAdresa provjerava upisanu stalnu adresu novog propisa; prazna znači
// adresu iz pregleda
func (s *PropisiService) novaAdresa(ctx context.Context, upisano string, p models.Propis) (string, error) {
	adresa := strings.TrimSpace(upisano)
	if adresa == "" {
		adresa = p.Adresa
	}
	if err := propisi.ProvjeriAdresu(adresa); errors.Is(err, propisi.ErrZauzetaAdresa) {
		return "", GreskaPropisa("Adresu „" + adresa + "” koristi program. Upišite drugu.")
	} else if err != nil {
		return "", porukaKrivaAdresa
	}
	drugi, err := s.repo.PoAdresi(ctx, adresa)
	if err != nil {
		return "", err
	}
	if drugi != nil && drugi.ID != p.ID {
		return "", GreskaPropisa("Adresu „" + adresa + "” već ima propis „" + drugi.Naziv + "”. Upišite drugu.")
	}
	return adresa, nil
}

// porukaSpremanja: prevelik propis dobije rečenicu, ostalo ostaje kvar
func porukaSpremanja(err error) error {
	if errors.Is(err, repository.ErrPrevelikPropis) {
		return porukaPrevelikPropis
	}
	return err
}

// IzborClanka je članak ili točka propisa u izboru „Članak propisa”
type IzborClanka struct {
	Oznaka, Naziv string
}

// PregledIzmjene je pročitana izmjena s prepoznatim odredbama koja čeka
// potvrdu
type PregledIzmjene struct {
	Izmjena    models.IzmjenaPropisa
	Propis     *models.Propis
	Podjela    propisi.Podjela
	Upozorenja []propisi.Upozorenje
	Postoji    *models.IzmjenaPropisa // ista izmjena je već dodana
	NeVezeSe   bool                   // izmjena ne spominje propis, a ni NN je ne vežu uz njega
	Retci      []RedakUpute
	Clanci     []IzborClanka
	Stupa      string // „4. 5. 2023.”
	Odredbi    int    // odredbe koje bi trebale mijenjati propis
	Prepoznato int    // od njih povezane s člankom ili u cijelom tekstu
	Nepovezano int    // od njih nisu povezane s člankom
}

// Objava je datum objave izmjene: „3. 5. 2023.”
func (p *PregledIzmjene) Objava() string { return danHR(p.Izmjena.DatumObjave) }

// Tekst su dijelovi izmjene za prikaz
func (p *PregledIzmjene) Tekst() []DioNaStranici { return naStranici(p.Izmjena.Dijelovi) }

// ProcitajIzmjenu dohvaća ili čita izmjenu propisa, prepoznaje koje članke
// mijenja i veže ih uz članke propisa. Ništa se ne sprema.
func (s *PropisiService) ProcitajIzmjenu(ctx context.Context, perms *models.UserPermissions, adresa string, u UlazPropisa) (*PregledIzmjene, error) {
	if err := requireGlobalAdmin(perms, "dodavanje izmjene propisa"); err != nil {
		return nil, err
	}
	osnova, err := s.osnova(ctx, adresa)
	if err != nil {
		return nil, err
	}
	nn, pr, err := s.procitajAkt(ctx, u)
	if err != nil {
		return nil, err
	}
	podjela := propisi.Podijeli(pr.Odlomci)
	upute, upozorenja := propisi.PoveziSOsnovom(propisi.PrepoznajUpute(podjela.Dijelovi), osnova)
	iz := models.IzmjenaPropisa{ID: idIzmjene(osnova, nn), PropisID: osnova.ID, Naziv: pr.Naziv, Donositelj: pr.Donositelj,
		NN: nn, ELI: pr.ELI, DatumObjave: pr.DatumObjave, Izvor: izvorUlaza(u), Citac: propisi.InacicaCitaca,
		Dijelovi: podjela.Dijelovi, Upute: upute, Veze: pr.Veze}
	iz.StupaNaSnagu, iz.StupanjeIzraz = stupanje(podjela.Dijelovi, iz.DatumObjave)
	p := &PregledIzmjene{Izmjena: iz, Propis: osnova, Podjela: podjela, Upozorenja: append(podjela.Upozorenja, upozorenja...),
		NeVezeSe: !propisi.VezeSe(pr, podjela.Dijelovi, osnova), Clanci: izborClanaka(osnova)}
	if p.Postoji, err = s.repo.Izmjena(ctx, iz.ID); err != nil {
		return nil, err
	}
	p.osvjezi()
	return p, nil
}

// osnova je propis po stalnoj adresi; ErrNemaPropisa kad ga nema
func (s *PropisiService) osnova(ctx context.Context, adresa string) (*models.Propis, error) {
	p, err := s.repo.PoAdresi(ctx, adresa)
	if err == nil && p == nil {
		err = ErrNemaPropisa
	}
	return p, err
}

func idIzmjene(p *models.Propis, nn models.IzdanjeNN) string {
	return p.ID + "-izmjena-" + nn.ID()
}

// osvjezi slaže tablicu odredbi i zbroj povezanih
func (p *PregledIzmjene) osvjezi() {
	p.Retci = retciUputa(p.Propis, &p.Izmjena)
	p.Stupa = danHR(p.Izmjena.StupaNaSnagu)
	p.Odredbi, p.Nepovezano = 0, 0
	for _, u := range p.Izmjena.Upute {
		if u.Vrsta == models.UputaVlastita || u.Vrsta == models.UputaStupanje {
			continue
		}
		p.Odredbi++
		if u.Vrsta == models.UputaNeprepoznata && !u.Rucno && len(u.Cilj.Clanci) == 0 {
			p.Nepovezano++
		}
	}
	p.Prepoznato = p.Odredbi - p.Nepovezano
}

// izborClanaka su članci i točke propisa za ručno povezivanje
func izborClanaka(p *models.Propis) []IzborClanka {
	var out []IzborClanka
	for _, d := range p.Dijelovi {
		if d.Vrsta == models.DioClanak || d.Vrsta == models.DioTocka {
			out = append(out, IzborClanka{Oznaka: d.Oznaka, Naziv: d.Naziv()})
		}
	}
	return out
}

// NeMijenjaClanke je izbor „Ne mijenja članke” u stupcu „Članak propisa”
const NeMijenjaClanke = "-"

// SpremiIzmjenu sprema izmjenu iz pregleda. Ciljevi su ručni izbori po
// rednom broju odredbe: prazno ostavlja prepoznato, „-” kaže da odredba ne
// mijenja članke, a oznaka mora biti članak ili točka propisa.
func (s *PropisiService) SpremiIzmjenu(ctx context.Context, perms *models.UserPermissions, tko *models.User, p *PregledIzmjene, ciljevi map[int]string) (*models.IzmjenaPropisa, error) {
	if err := requireGlobalAdmin(perms, "spremanje izmjene propisa"); err != nil {
		return nil, err
	}
	osnova, err := s.repo.Get(ctx, p.Propis.ID)
	if err == nil && osnova == nil {
		err = ErrNemaPropisa
	}
	if err != nil {
		return nil, err
	}
	iz := p.Izmjena
	if iz.Upute, err = rucniCiljevi(iz.Upute, ciljevi, osnova); err != nil {
		return nil, err
	}
	postoji, err := s.repo.Izmjena(ctx, iz.ID)
	if err != nil {
		return nil, err
	}
	if postoji != nil {
		iz.CreatedAt = postoji.CreatedAt
	}
	iz.Ucitao, iz.UcitaoID = tko.FullName, tko.ID.String()
	if err := s.repo.SpremiIzmjenu(ctx, &iz); err != nil {
		return nil, porukaSpremanja(err)
	}
	return &iz, nil
}

// rucniCiljevi primjenjuje izbore administratora na kopiju uputa
func rucniCiljevi(upute []models.UputaIzmjene, ciljevi map[int]string, osnova *models.Propis) ([]models.UputaIzmjene, error) {
	postoji := map[string]bool{}
	for _, d := range osnova.Dijelovi {
		postoji[d.Oznaka] = true
	}
	out := make([]models.UputaIzmjene, len(upute))
	copy(out, upute)
	for i := range out {
		izbor := strings.TrimSpace(ciljevi[out[i].Redni])
		switch {
		case izbor == "":
			continue
		case izbor == NeMijenjaClanke:
			out[i].Cilj.Clanci = nil
		case !postoji[izbor]:
			return nil, porukaCiljNijeUPropisu
		default:
			out[i].Cilj.Clanci = []string{izbor}
		}
		out[i].Rucno = true
	}
	return out, nil
}

// Ukloni miče propis i njegove izmjene sa svih računala; u povijesti zapisa
// ostaje s razlogom
func (s *PropisiService) Ukloni(ctx context.Context, perms *models.UserPermissions, tko *models.User, adresa, razlog string) (*models.Propis, error) {
	u, err := s.uklanjanje(perms, tko, razlog)
	if err != nil {
		return nil, err
	}
	p, err := s.osnova(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return p, s.repo.Ukloni(ctx, p.ID, u)
}

// UkloniIzmjenu miče izmjenu propisa i njezine oznake sa svih računala
func (s *PropisiService) UkloniIzmjenu(ctx context.Context, perms *models.UserPermissions, tko *models.User, adresa, nnID, razlog string) (*models.IzmjenaPropisa, error) {
	u, err := s.uklanjanje(perms, tko, razlog)
	if err != nil {
		return nil, err
	}
	p, err := s.osnova(ctx, adresa)
	if err != nil {
		return nil, err
	}
	iz, err := s.repo.Izmjena(ctx, p.ID+"-izmjena-"+nnID)
	if err == nil && iz == nil {
		err = ErrNemaIzmjene
	}
	if err != nil {
		return nil, err
	}
	return iz, s.repo.UkloniIzmjenu(ctx, iz.ID, u)
}

// uklanjanje provjerava pravo i razlog i slaže zapis o uklanjanju
func (s *PropisiService) uklanjanje(perms *models.UserPermissions, tko *models.User, razlog string) (models.Uklanjanje, error) {
	if err := requireGlobalAdmin(perms, "uklanjanje propisa"); err != nil {
		return models.Uklanjanje{}, err
	}
	razlog = strings.TrimSpace(razlog)
	if razlog == "" {
		return models.Uklanjanje{}, ErrBezRazloga
	}
	return models.Uklanjanje{Razlog: razlog, Tko: tko.FullName, Kad: s.sada().UTC()}, nil
}
