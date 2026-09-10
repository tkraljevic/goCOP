package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// ArhivaPogled je pregled povijesti iz arhive za jednu letvu: koje veličine
// postoje, koje godine, vrijednosti odabrane godine i graf.
//
// Stoji na historijatu letve, ne na očitanjima. Očitanja su operativa — ono
// što je danas upisano i po čemu se vodi obrana; arhiva je ono što je
// izmjereno prije nego što je program postojao. Kad je stajala na obje
// stranice, dvije su pokazivale različit „zadnji vodostaj" i nije bilo jasno
// koji vrijedi.
type ArhivaPogled struct {
	ArhVelicine     []string
	ArhVelicina     string
	ArhKorak        string
	ArhGodine       []int
	ArhGodina       int
	ArhNiz          []models.SpojenaVrijednost
	ArhChart        *Chart
	ArhChartUzak    *Chart
	ArhJedinica     string
	ArhSazetak      []models.SazetakVelicine
	ArhPager        Pager
	ArhPromjeneKote []models.PromjenaKote // zabilježena premještanja nule letve
	ArhIspravaka    int
	ArhSada         *models.SpojenaVrijednost // zadnja vrijednost odabrane veličine
	ArhDecimala     int
	KoteZaArhivu    bool   // prikazuje li se uz vodostaj i apsolutna kota vode
	KotaSustav      string // u kojem visinskom sustavu
}

// popuniArhivu puni pregled. Svaka vrijednost nosi izvor i odstupanje, pa se u
// tablici vidi odakle je koji redak.
func popuniArhivu(ctx context.Context, r *http.Request, a *repository.ArhivaRepository,
	isp *repository.IspravakRepository, p *ArhivaPogled, station *models.Station) {
	if a == nil || station == nil || station.Code == "" {
		return
	}
	dosezi, err := a.SpojDosezi(ctx, station.Code)
	if err != nil || len(dosezi) == 0 {
		return
	}
	p.ArhSazetak, _ = a.Sazetak(ctx, station.Code)
	p.ArhPromjeneKote, _ = a.PromjeneKote(ctx, station.Code)

	vidjeno := map[string]bool{}
	for _, d := range dosezi {
		if !vidjeno[d.Velicina] {
			vidjeno[d.Velicina] = true
			p.ArhVelicine = append(p.ArhVelicine, d.Velicina)
		}
	}
	p.ArhVelicina = r.URL.Query().Get("v")
	if !vidjeno[p.ArhVelicina] {
		p.ArhVelicina = p.ArhVelicine[0]
	}
	p.ArhKorak = r.URL.Query().Get("korak")
	if p.ArhKorak != "satni" {
		p.ArhKorak = "dnevni"
	}
	p.ArhJedinica = models.JedinicaVelicine(p.ArhVelicina)
	p.ArhDecimala = decimalaVelicine(p.ArhVelicina)
	p.ArhSada, _ = a.SpojZadnje(ctx, station.Code, p.ArhVelicina, p.ArhKorak)
	if p.ArhVelicina == "vodostaj" && station.ImaKotuNule() {
		if k := station.Kote(0); len(k) > 0 {
			p.KoteZaArhivu, p.KotaSustav = true, k[0].Sustav
		}
	}

	p.ArhGodine, _ = a.SpojGodine(ctx, station.Code, p.ArhVelicina, p.ArhKorak)
	if len(p.ArhGodine) == 0 {
		// tražena gustoća ne postoji za tu veličinu — vrati se na dnevnu
		p.ArhKorak = "dnevni"
		p.ArhGodine, _ = a.SpojGodine(ctx, station.Code, p.ArhVelicina, p.ArhKorak)
	}
	if g, err := strconv.Atoi(r.URL.Query().Get("god")); err == nil {
		p.ArhGodina = g
	}
	imaGodinu := false
	for _, g := range p.ArhGodine {
		if g == p.ArhGodina {
			imaGodinu = true
		}
	}
	if !imaGodinu && len(p.ArhGodine) > 0 {
		p.ArhGodina = p.ArhGodine[0]
	}
	if p.ArhGodina == 0 {
		return
	}
	od := time.Date(p.ArhGodina, 1, 1, 0, 0, 0, 0, time.UTC)
	do := od.AddDate(1, 0, 0).Add(-time.Second)
	ukupno, _ := a.SpojBroj(ctx, station.Code, p.ArhVelicina, p.ArhKorak, od, do)
	p.ArhPager = pagerZa(r, "ap", ukupno, arhivaPoStranici)
	p.ArhNiz, _ = a.SpojRaspon(ctx, station.Code, p.ArhVelicina, p.ArhKorak,
		od, do, p.ArhPager.PerPage, p.ArhPager.Odmak())
	ispravci := ispravciIz(ctx, isp, station.Code, p.ArhVelicina, p.ArhKorak, od, do)
	p.ArhIspravaka = len(ispravci)
	primijeniIspravke(p.ArhNiz, ispravci)

	// Graf crta cijelu godinu, ne samo prikazanu stranicu — inače bi se mijenjao
	// pri svakom listanju i ne bi značio ono što piše.
	cijela, _ := a.SpojRaspon(ctx, station.Code, p.ArhVelicina, p.ArhKorak, od, do, 20000, 0)
	primijeniIspravke(cijela, ispravci)
	krivulje, _ := a.Krivulje(ctx, station.Code)
	p.ArhChart = crtajNiz(prorijediNiz(cijela, 700), p.ArhVelicina, station, krivulje)
	p.ArhChartUzak = crtajNizUzak(prorijediNiz(cijela, 260), p.ArhVelicina, station, krivulje)
}

// ispravciIz čita ispravke arhive; bez pohrane vraća prazno, jer ispravci su
// dodatak arhivi, a ne uvjet da se ona prikaže.
func ispravciIz(ctx context.Context, repo *repository.IspravakRepository,
	letva, velicina, korak string, od, do time.Time) map[int64]models.ArhivaIspravak {
	if repo == nil {
		return nil
	}
	m, err := repo.ZaNiz(ctx, letva, velicina, korak, od, do)
	if err != nil {
		return nil
	}
	return m
}
