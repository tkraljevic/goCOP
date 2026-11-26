package web

import (
	"context"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Svaka stranica propisa iscrtava se i prazna i puna, s pravim pomoćnicima
// predložaka. Predložak koji nije upisan u NewServer ili kojem nedostaje
// zajednički dio (propisi_dijelovi.html) ovdje pada (Odluke (25. 11. 2026.)).
func TestStranicePropisaSeIscrtavaju(t *testing.T) {
	o := novaOkolinaPropisa(t)
	o.napuni()
	for _, ime := range []string{"propisi.html", "propis.html", "propis_izmjena.html"} {
		if o.server.templates[ime] == nil {
			t.Errorf("%s nije upisan u NewServer", ime)
		}
		html := iscrtaj(t, ime, PropisiPageData{})
		if strings.Contains(html, "template:") {
			t.Errorf("%s prazan: %s", ime, html)
		}
	}

	ctx := context.Background()
	admin := &models.UserPermissions{IsGlobalAdmin: true}
	skupine, err := o.svc.Popis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	popis := iscrtaj(t, "propisi.html", PropisiPageData{Skupine: skupine, Permissions: admin})
	if !strings.Contains(popis, ">Vodočuvari</h3>") || !strings.Contains(popis, "Propisi o vodočuvarskoj službi.") {
		t.Errorf("popis: %s", popis)
	}
	if prazno := iscrtaj(t, "propisi.html", PropisiPageData{}); !strings.Contains(prazno, "Još nema učitanih propisa.") {
		t.Error("prazan popis nema rečenicu")
	}

	st, err := o.svc.Propis(ctx, "plan-obrane-primjerova")
	if err != nil {
		t.Fatal(err)
	}
	plan := iscrtaj(t, "propis.html", PropisiPageData{Stranica: st, Permissions: admin})
	for _, ima := range []string{"Propis u programu nema izmjena.", `href="/propisi/plan-obrane-primjerova/dio-1"`, "točka XXIII.",
		"propis-neprepoznato", "Nije prepoznato", `data-naziv-stranice>Plan obrane Primjerova</h1>`} {
		if !strings.Contains(plan, ima) {
			t.Errorf("naslovnica plana nema %q", ima)
		}
	}

	d, err := o.svc.Dio(ctx, "zakon-o-cuvanju-nasipa", "clanak-1")
	if err != nil {
		t.Fatal(err)
	}
	clanak := iscrtaj(t, "propis.html", PropisiPageData{Stranica: &d.StranicaPropisa, Dio: d})
	for _, ima := range []string{"<thead><tr><th>Stupanj</th><th colspan=\"2\">Vodostaj</th></tr></thead>", `<td rowspan="2">pripremno</td>`,
		"<strong>300 cm</strong>", "– danju", `<span class="propis-oznaka-tocke">a)</span>`, "ako nije drukčije određeno.",
		`<p class="propis-naslov-iznad">Predmet</p>`, `<span class="propis-oznaka-stavka">(1)</span>`, "OPĆE ODREDBE</a> ›"} {
		if !strings.Contains(clanak, ima) {
			t.Errorf("članak 1. nema %q", ima)
		}
	}

	// akt bez cjelina: naslovnica pokazuje cijeli tekst, a sadržaj vodi na sidra
	sviClanci := &service.StranicaPropisa{Propis: &models.Propis{Adresa: "pravilnik", Naziv: "Pravilnik"}, SviClanci: true,
		Dijelovi: []service.DioNaStranici{{DioPropisa: models.DioPropisa{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1."}, Poveznica: "/propisi/pravilnik/clanak-1"}},
		Sadrzaj:  []service.DioNaStranici{{DioPropisa: models.DioPropisa{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1."}}}}
	if html := iscrtaj(t, "propis.html", PropisiPageData{Stranica: sviClanci}); !strings.Contains(html, `href="#clanak-1"`) || !strings.Contains(html, `id="clanak-1"`) {
		t.Errorf("akt bez cjelina: %s", html)
	}

	iz, err := o.svc.Izmjena(ctx, "zakon-o-cuvanju-nasipa", "nn-2026-47-808")
	if err != nil {
		t.Fatal(err)
	}
	izmjena := iscrtaj(t, "propis_izmjena.html", PropisiPageData{Izmjena: iz})
	for _, ima := range []string{"<thead><tr><th>Odredba izmjene</th>", `data-stupac="Stanje"`, "propis-navod", "Članak 1."} {
		if !strings.Contains(izmjena, ima) {
			t.Errorf("izmjena nema %q", ima)
		}
	}
	bezUputa := *iz
	bezUputa.Upute = nil
	if html := iscrtaj(t, "propis_izmjena.html", PropisiPageData{Izmjena: &bezUputa}); !strings.Contains(html, "nije našao nijednu odredbu") {
		t.Error("izmjena bez odredbi nema rečenicu")
	}

	rez, err := o.svc.Trazi(ctx, "nasip", "")
	if err != nil {
		t.Fatal(err)
	}
	if html := iscrtaj(t, "propisi.html", PropisiPageData{Pretraga: rez, Upit: "nasip"}); !strings.Contains(html, "<mark>") || !strings.Contains(html, "Svi propisi (") {
		t.Errorf("pretraga: %s", html)
	}
}
