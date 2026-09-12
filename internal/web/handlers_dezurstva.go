package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/obracun"
)

// Plan dežurstava stoji uz dnevnik COP-a: uprava centra ga slaže, dežurni ga
// vidi, a poslije obrane isti zapisi daju obračun sati po osobi.

// ShowDezurstva prikazuje plan dežurstava dnevnika COP-a: popis, obrazac za
// upravu centra, i put do obračuna
func (h *JournalsHandler) ShowDezurstva(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	if j.CentarSektor == "" {
		http.Error(w, "plan dežurstava vodi se uz dnevnik COP-a", http.StatusNotFound)
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	data.Dezurstva, _ = h.journals.Dezurstva(r.Context(), j.ID)
	data.OpisiRada = models.OpisiRada
	data.Areas, _ = h.users.ListAreas(j.CentarSektor)
	data.UpravaCentra = h.journals.UpravaCentra(data.Permissions, j)
	data.MozeSebe = h.journals.MozeSebeUPlan(data.Permissions, h.opseg(j, area), j)
	// Uprava bira bilo koga iz sektora; ostali upisuju samo sebe.
	if data.UpravaCentra {
		data.Osobe, _ = h.users.ListUsers(j.CentarSektor, 0, "", "", "")
	} else if data.MozeSebe && data.CurrentUser != nil {
		data.Osobe = []models.User{*data.CurrentUser}
	}
	h.render(w, h.tmplDezurstva, "dnevnik_dezurstva.html", data)
}

// HandleSaveDezurstvo upisuje ili mijenja dežurstvo iz obrasca na planu
func (h *JournalsHandler) HandleSaveDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "/dezurstva"
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, back, "error", "Neispravan zahtjev")
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	d := models.Dezurstvo{ID: f("id"), UserID: f("user_id"), Opis: f("opis"), Napomena: r.FormValue("napomena")}
	// Za koga: broj područja, ili prazno za cijeli sektor
	if n, err := strconv.Atoi(f("podrucje")); err == nil && n > 0 {
		d.Podrucje = &n
	}
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
	j, _, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "/dezurstva"
	if err := h.journals.MakniDezurstvo(r.Context(), u, perms, j, r.PathValue("dez")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Dežurstvo je maknuto iz plana.")
}

// HandlePotvrdiDezurstvo: uprava centra provjerila je upis i potvrđuje ga
func (h *JournalsHandler) HandlePotvrdiDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, _, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	back := "/dnevnici/" + j.ID + "/dezurstva"
	if err := h.journals.PotvrdiDezurstvo(r.Context(), u, perms, j, r.PathValue("dez")); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, back, "success", "Dežurstvo je potvrđeno.")
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
	postavke := h.postavkeObracuna()
	nazivi := map[int]string{}
	if podrucja, err := h.users.ListAreas(j.CentarSektor); err == nil {
		for _, a := range podrucja {
			nazivi[a.ID] = a.Name
		}
	}
	if data.Obracun, data.CekaPotvrdu, err = h.journals.Obracun(r.Context(), j, od, do, postavke.Kalendar(r.Context()), postavke.Koeficijenti(r.Context()), nazivi); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, h.tmplObracun, "dnevnik_obracun.html", data)
}
