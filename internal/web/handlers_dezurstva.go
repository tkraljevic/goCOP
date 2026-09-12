package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Plan dežurstava stoji uz dnevnik COP-a: uprava centra ga slaže, dežurni ga
// vidi, a poslije obrane isti zapisi daju obračun sati po osobi.

// HandleSaveDezurstvo upisuje ili mijenja dežurstvo iz obrasca na dnevniku
func (h *JournalsHandler) HandleSaveDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "#dezurstva"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	d := models.Dezurstvo{ID: f("id"), UserID: f("user_id"), Opis: f("opis"), Napomena: r.FormValue("napomena")}
	// Ime se uzima iz imenika u trenutku upisa i ostaje uz zapis
	if id, err := uuid.Parse(d.UserID); err == nil {
		if osoba, _ := h.users.GetUserByID(id); osoba != nil {
			d.UserName = osoba.FullName
		}
	}
	var err error
	if d.Od, err = time.ParseInLocation("2006-01-02 15:04", f("od_date")+" "+f("od_time"), models.Zagreb); err != nil {
		redirectWith(w, r, back, "error", "Upišite dan i sat početka")
		return
	}
	doDan := f("do_date")
	if doDan == "" {
		doDan = f("od_date")
	}
	if d.Do, err = time.ParseInLocation("2006-01-02 15:04", doDan+" "+f("do_time"), models.Zagreb); err != nil {
		redirectWith(w, r, back, "error", "Upišite sat kraja")
		return
	}
	if err := h.journals.SpremiDezurstvo(r.Context(), u, perms, h.opseg(j, area), j, &d); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Dežurstvo je upisano u plan.")
}

// HandleMakniDezurstvo miče dežurstvo iz plana
func (h *JournalsHandler) HandleMakniDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "#dezurstva"
	if err := h.journals.MakniDezurstvo(r.Context(), u, perms, h.opseg(j, area), j, r.PathValue("dez")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Dežurstvo je maknuto iz plana.")
}

// ShowObracun prikazuje sate po osobi za razdoblje; zadano je cijelo
// trajanje dnevnika, a razdoblje se sužava upitom od/do (dan uključivo)
func (h *JournalsHandler) ShowObracun(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	if !data.CanWrite {
		http.Error(w, "Obračun sati vidi tko piše u dnevnik", http.StatusForbidden)
		return
	}
	od := time.Date(1900, 1, 1, 0, 0, 0, 0, models.Zagreb)
	if j.StartedAt != nil {
		od = j.StartedAt.In(models.Zagreb)
	}
	do := time.Now().In(models.Zagreb).AddDate(0, 0, 1)
	if j.EndedAt != nil {
		do = j.EndedAt.In(models.Zagreb).AddDate(0, 0, 1)
	}
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("od"), models.Zagreb); err == nil {
		od = t
	}
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("do"), models.Zagreb); err == nil {
		do = t.AddDate(0, 0, 1)
	}
	data.From, data.To = od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02")
	data.Razredi = obracun.Razredi
	var err error
	if data.Obracun, err = h.journals.Obracun(r.Context(), j, od, do, obracun.IORS2026); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, h.tmplObracun, "dnevnik_obracun.html", data)
}
