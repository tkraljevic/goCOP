package web

import (
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Upis iz akta nosi vrijeme akta kako je pročitano iz baze, u UTC-u. Na
// stranici i u obrascu ispravka mora stajati naše vrijeme: akt od 6 h
// ne smije pisati 05:00.
func TestVrijemeUpisaIzAktaPoNasem(t *testing.T) {
	dan := time.Date(2026, 11, 23, 0, 0, 0, 0, models.Zagreb)
	kad := time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)
	j := models.Journal{ID: "dn-7", Kind: models.JournalKindDefense, CentarSektor: "B", Title: "Dnevnik COP-a, 2026."}
	data := JournalPageData{CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		Journal: &j, KarticaCOP: "zapisi", Today: "2026-11-23",
		Dani: []DanZapisa{{Dan: dan, Zapisi: []models.JournalEntry{{ID: "z1", Number: 1, Kind: models.EntryKindNotice,
			HappenedAt: &kad, ReportedBy: "goCOP, akt B-2/2026", Text: "Obavijest o pripremnom stanju", UserName: "Iva Ivić"}}}}}
	html := iscrtaj(t, "dnevnik_cop.html", data)
	if !strings.Contains(html, `<div class="zapis-kad mono">06:00</div>`) {
		t.Error("vrijeme upisa nije po našem vremenu")
	}
	if strings.Contains(html, "05:00") {
		t.Error("stranica još pokazuje vrijeme u UTC-u")
	}
}
