package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Fotografije uz dnevni list (prilozi) putuju kao i ostali sadržaj: list
// nosi otisak svake fotografije, a bajtovi su u spremištu sadržaja, u kanalu
// lista (SavePrilog). Čvor koji list primi veže fotografiju uz list i, kad je
// veza nova, a fotografije ovdje nema, stavlja je na popis za dohvat, kao
// fotografije prijava (primiSlikePrijave). Koje će fotografije zaista
// dohvatiti, odlučuje pretplata pri razmjeni: razina sadržaja i područje
// kanala lista. Fotografiju za koju je čvor već znao ne traži iznova: možda
// ju je otpustio jer joj je prošao rok iz pretplate, pa bi je razmjena inače
// vraćala u krug. Primjena ne piše ništa u knjigu.

// PopravakPriloga kaže što je za fotografije dnevnih listova upisano u
// spremište sadržaja
type PopravakPriloga struct {
	Kanal int // fotografije koje su dobile kanal lista
	Veza  int // fotografije koje su dobile vezu uz list
	Zelja int // od njih, fotografije koje ovaj čvor nema, pa ih traži
}

func (p *PopravakPriloga) dodaj(q PopravakPriloga) {
	p.Kanal += q.Kanal
	p.Veza += q.Veza
	p.Zelja += q.Zelja
}

// String je opis za dnevnik; prazan kad ništa nije upisano
func (p PopravakPriloga) String() string {
	var dijelovi []string
	if p.Kanal > 0 {
		dijelovi = append(dijelovi, "kanal lista upisan za "+brojFotografija(p.Kanal))
	}
	if p.Veza > 0 {
		dijelovi = append(dijelovi, "veza uz list upisana za "+brojFotografija(p.Veza))
	}
	if p.Zelja > 0 {
		dijelovi = append(dijelovi, fmt.Sprintf("na popisu za dohvat %d", p.Zelja))
	}
	return strings.Join(dijelovi, "; ")
}

func brojFotografija(n int) string {
	return fmt.Sprintf("%d %s", n, models.UzBrojHR(n, "fotografiju", "fotografije", "fotografija"))
}

// PopraviPrilogeListova pri pokretanju dovršava fotografije listova koji su
// na ovom čvoru: sadržaj fotografije bez kanala dobiva kanal lista, a
// fotografija lista primljenog prije 0.0.38, kad primjena nije bilježila
// priloge, dobiva vezu uz list i, ako je ovdje nema, ide na popis za dohvat.
// Listovi idu po danu, pa ista fotografija uz više listova dobiva kanal
// najranijeg. Što je već upisano, ne dira i ne broji, pa nakon prvog
// pokretanja ne upisuje ništa.
func PopraviPrilogeListova(ctx context.Context, baza *sql.DB) (PopravakPriloga, error) {
	var p PopravakPriloga
	if spremiste == nil {
		return p, nil
	}
	listovi, err := (&VodocuvarRepository{db: baza}).upitListova(ctx, `SELECT `+vodocuvarskiColumns+` FROM vodocuvarski_listovi
		WHERE prilozi NOT IN ('', '[]', 'null') ORDER BY datum, id`)
	for i := range listovi {
		p.dodaj(prilogeUSpremiste(ctx, &listovi[i]))
	}
	return p, err
}

// primiPrilogeSlozenih bilježi fotografije listova koje primjena upravo
// stavlja na površinu: lista pod oznakom zapisa i listova kasnijih potpisa
func primiPrilogeSlozenih(ctx context.Context, s slozeniListovi) {
	if s.glavni != nil {
		prilogeUSpremiste(ctx, s.glavni)
	}
	for i := range s.dvojnici {
		prilogeUSpremiste(ctx, &s.dvojnici[i])
	}
}

// prilogeUSpremiste veže fotografije lista uz njihov sadržaj, u kanalu lista
func prilogeUSpremiste(ctx context.Context, l *models.VodocuvarskiList) PopravakPriloga {
	var p PopravakPriloga
	if spremiste == nil {
		return p
	}
	kanal := listChannel(l)
	for _, pr := range l.Prilozi {
		p.dodaj(prilogUSpremiste(ctx, l.ID, kanal, pr))
	}
	return p
}

// prilogUSpremiste daje sadržaju fotografije kanal lista, ako ga nema, i
// veže fotografiju uz list. Kad je veza nova, a fotografije ovdje nema,
// fotografija ide na popis za dohvat; želja se upisuje prije veze, pa prekid
// između njih ne ostavlja vezu bez želje. Opis fotografije otprije spremišta
// nema otiska, pa se fotografija ne da dohvatiti.
func prilogUSpremiste(ctx context.Context, listID, kanal string, pr models.PrilogLista) PopravakPriloga {
	var p PopravakPriloga
	if pr.Sadrzaj == "" {
		return p
	}
	if upisan, _ := spremiste.PostaviKanal(ctx, pr.Sadrzaj, kanal); upisan {
		p.Kanal = 1
	}
	veza := sadrzaj.Veza{Entitet: EntityVodocuvarski, EntitetID: listID, Uloga: ulogaPriloga(pr.ID), Kanal: kanal}
	if spremiste.ImaVezu(ctx, pr.Sadrzaj, veza) {
		return p
	}
	p.Zelja = zeliPrilog(ctx, pr, kanal)
	if spremiste.Vezi(ctx, pr.Sadrzaj, veza) == nil {
		p.Veza = 1
	}
	return p
}

// zeliPrilog stavlja fotografiju koje ovdje nema na popis za dohvat; vraća 1
// kad ju je stavio. Ista fotografija uz drugi list već je na popisu i ne
// broji se dvaput.
func zeliPrilog(ctx context.Context, pr models.PrilogLista, kanal string) int {
	if spremiste.Ima(ctx, pr.Sadrzaj) || spremiste.Zeljen(ctx, pr.Sadrzaj) {
		return 0
	}
	if spremiste.Zeli(ctx, pr.Sadrzaj, vrstaPriloga(pr), pr.Bajtova, "pretplata", kanal) != nil {
		return 0
	}
	return 1
}

// vrstaPriloga je vrsta sadržaja priloga; opis bez vrste je fotografija
func vrstaPriloga(pr models.PrilogLista) string {
	if pr.Vrsta == "" {
		return "image/jpeg"
	}
	return pr.Vrsta
}
