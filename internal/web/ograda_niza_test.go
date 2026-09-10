package web

import (
	"strings"
	"testing"

	"gocop/internal/models"

	"github.com/google/uuid"
)

// Što se o nizu zna, a iz brojki se ne vidi — zaleđen mjerač, sumnjive zimske
// vrijednosti — mora se vidjeti uz njega. Prije toga je takvo znanje živjelo
// samo u glavi onoga tko je niz slagao.
func TestOgradaUzNizStojiUzNjega(t *testing.T) {
	nizovi := []models.HidroNiz{
		{ID: 1, Letva: "batina", Izvor: "letva-hv", Velicina: "vodostaj", Vrsta: "satni",
			Od: "2001-03-22", Do: "2023-09-12", Zapisa: 182295,
			Napomena: "Pedesetak dana 2016./2017. mjerač je bio zaleđen."},
		{ID: 2, Letva: "batina", Izvor: "his2000", Velicina: "vodostaj", Vrsta: "srednjak",
			Od: "2001-03-09", Do: "2026-07-31", Zapisa: 9276},
	}
	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     models.Station{ID: uuid.New(), Name: "Batina", Code: "batina"},
		Nizovi:      nizovi, NizID: 1,
	})
	if !strings.Contains(html, "mjerač je bio zaleđen") {
		t.Error("ograda se ne vidi uz niz")
	}
	// niz bez ograde ne dobiva praznu oznaku
	if strings.Count(html, "ograda</span>") != 1 {
		t.Errorf("oznaka ograde stoji %d puta, a ograđen je jedan niz",
			strings.Count(html, "ograda</span>"))
	}
}

// Dokument putuje dalje od stranice i čita ga netko tko niz nije vidio, pa
// ograda vrijedi više ondje nego na zaslonu.
func TestOgradaIdeIUIzvjesce(t *testing.T) {
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceHistorijat
	iz.Nizovi = []models.HidroNiz{
		{Izvor: "letva-hv", Velicina: "vodostaj", Od: "2001-03-22", Do: "2023-09-12",
			Napomena: "Pedesetak dana 2016./2017. mjerač je bio zaleđen."},
		{Izvor: "his2000", Velicina: "protok", Od: "2001-03-09", Do: "2025-12-31"},
	}
	xml := dokumentXML(t, iz)
	if !strings.Contains(xml, "Ograde uz nizove") {
		t.Error("izvješće nema poglavlje s ogradama")
	}
	if !strings.Contains(xml, "mjerač je bio zaleđen") {
		t.Error("ograda nije stigla u izvješće")
	}

	// bez ograda nema ni poglavlja — prazan naslov je gori od nikakvog
	iz.Nizovi[0].Napomena = ""
	if strings.Contains(dokumentXML(t, iz), "Ograde uz nizove") {
		t.Error("poglavlje s ogradama stoji i kad ograda nema")
	}
}
