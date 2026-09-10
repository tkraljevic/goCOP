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

// Ograda ima dva podrijetla: izdavačeva stiže s paketom i stoji uz sam niz u
// arhivi; naša je upisana ovdje i putuje knjigom verzija. Arhiva se otvara
// samo za čitanje i pregrađuje pri obnovi, pa naša ondje ne bi preživjela.
func TestObjeOgradeStojeUzIstiNiz(t *testing.T) {
	niz := models.HidroNiz{ID: 1, Letva: "batina", Izvor: "letva-hv", Velicina: "vodostaj",
		Vrsta: "satni", Od: "2001-03-22", Do: "2023-09-12", Zapisa: 182295,
		Napomena: "radar OTT RLS od 16.6.2017."}
	st := models.Station{ID: uuid.New(), Name: "Batina", Code: "batina",
		OgradeNiza: []models.OgradaNiza{
			{Izvor: "letva-hv", Velicina: "vodostaj", Od: "2016-12-01", Do: "2017-02-15",
				Tekst: "mjerač zaleđen, vrijednosti nepouzdane"},
			{Izvor: "his2000", Tekst: "ne odnosi se na ovaj niz"},
		}}

	html := iscrtaj(t, "station_history.html", StationPageData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Station:     st, Nizovi: []models.HidroNiz{niz}, NizID: 1,
	})
	if !strings.Contains(html, "mjerač zaleđen") {
		t.Error("vlastita ograda se ne vidi uz niz")
	}
	if !strings.Contains(html, "radar OTT RLS") {
		t.Error("izdavačeva ograda se izgubila")
	}
	if !strings.Contains(html, "iz izdanja") {
		t.Error("ne piše koja je ograda izdavačeva")
	}
	if strings.Contains(html, "ne odnosi se na ovaj niz") {
		t.Error("ograda za drugi izvor stoji uz krivi niz")
	}

	// obje idu i u izvješće
	iz := probnoIzvjesce(t)
	iz.Dio = izvjesceHistorijat
	iz.Station.OgradeNiza = st.OgradeNiza
	iz.Nizovi = []models.HidroNiz{niz}
	xml := dokumentXML(t, iz)
	for _, want := range []string{"mjerač zaleđen", "radar OTT RLS", "1.12.2016. – 15.2.2017."} {
		if !strings.Contains(xml, want) {
			t.Errorf("izvješće nema %q", want)
		}
	}
}
