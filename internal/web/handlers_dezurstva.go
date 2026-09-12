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
	// Uprava bira bilo koga iz sektora, ali program najprije ponudi nju
	// samu — najčešće upisuje sebe; ostali upisuju samo sebe.
	if data.UpravaCentra {
		data.Osobe, _ = h.users.ListUsers(j.CentarSektor, 0, "", "", "")
		if data.CurrentUser != nil {
			svoj := false
			for _, o := range data.Osobe {
				if o.ID == data.CurrentUser.ID {
					svoj = true
				}
			}
			if !svoj {
				data.Osobe = append([]models.User{*data.CurrentUser}, data.Osobe...)
			}
		}
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
	od, do := h.razdobljeObracuna(r, j)
	data.From, data.To = od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02")
	var err error
	postavke := h.postavkeObracuna()
	nazivi := map[int]string{}
	if podrucja, err := h.users.ListAreas(j.CentarSektor); err == nil {
		for _, a := range podrucja {
			nazivi[a.ID] = a.Name
		}
	}
	data.RadnoVrijeme = postavke.RadnoVrijeme(r.Context())
	if data.Obracun, err = h.journals.Obracun(r.Context(), j, od, do, postavke.Kalendar(r.Context()), postavke.Koeficijenti(r.Context()), nazivi); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, h.tmplObracun, "dnevnik_obracun.html", data)
}

// razdobljeObracuna: zadano od početka dnevnika do kraja plana; ?od i ?do
// (dan uključivo) ga sužavaju
func (h *JournalsHandler) razdobljeObracuna(r *http.Request, j *models.Journal) (od, do time.Time) {
	od = time.Date(1900, 1, 1, 0, 0, 0, 0, models.Zagreb)
	if j.StartedAt != nil {
		od = j.StartedAt.In(models.Zagreb)
	}
	do = time.Now().In(models.Zagreb).AddDate(0, 0, 1)
	if dez, err := h.journals.Dezurstva(r.Context(), j.ID); err == nil {
		for _, d := range dez {
			if kraj := d.Do.In(models.Zagreb).AddDate(0, 0, 1); kraj.After(do) {
				do = time.Date(kraj.Year(), kraj.Month(), kraj.Day(), 0, 0, 0, 0, models.Zagreb)
			}
		}
	}
	if j.EndedAt != nil {
		do = j.EndedAt.In(models.Zagreb).AddDate(0, 0, 1)
	}
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("od"), models.Zagreb); err == nil {
		od = t
	}
	if t, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("do"), models.Zagreb); err == nil {
		do = t.AddDate(0, 0, 1)
	}
	return od, do
}

// ShowIORS prikazuje izvješće o radnim satima jedne osobe — ono što obrazac
// IORS traži po djelatniku. Vidi ga tko vidi obračun, i osoba sama.
func (h *JournalsHandler) ShowIORS(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	data := h.pageData(r)
	data.Journal, data.Area = j, area
	h.fillRights(&data)
	userID := r.PathValue("user")
	if !data.CanWrite && (data.CurrentUser == nil || data.CurrentUser.ID.String() != userID) {
		http.Error(w, "Izvješće o satima vidi tko piše u dnevnik, i osoba sama", http.StatusForbidden)
		return
	}
	od, do := h.razdobljeObracuna(r, j)
	data.From, data.To = od.Format("2006-01-02"), do.AddDate(0, 0, -1).Format("2006-01-02")
	postavke := h.postavkeObracuna()
	nazivi := map[int]string{}
	if podrucja, err := h.users.ListAreas(j.CentarSektor); err == nil {
		for _, a := range podrucja {
			nazivi[a.ID] = a.Name
		}
	}
	var err error
	data.RadnoVrijeme = postavke.RadnoVrijeme(r.Context())
	if data.IORS, err = h.journals.ObracunOsobe(r.Context(), j, userID, od, do, postavke.Kalendar(r.Context()), postavke.Koeficijenti(r.Context()), nazivi); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if data.IORS.UserName == "" {
		if id, err := uuid.Parse(userID); err == nil {
			if osoba, _ := h.users.GetUserByID(id); osoba != nil {
				data.IORS.UserName = osoba.FullName
			}
		}
	}
	data.Razredi = obracun.Razredi
	h.render(w, h.tmplIORS, "dnevnik_iors.html", data)
}

// HandlePreuzmiDezurstvo: operater dolaskom preuzima dežurstvo
func (h *JournalsHandler) HandlePreuzmiDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, area, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	if err := h.journals.PreuzmiDezurstvo(r.Context(), u, perms, h.opseg(j, area), j); err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/dnevnici/"+j.ID+"#kraj", "success", "Dežurstvo je preuzeto.")
}

// HandlePredajDezurstvo: operater odlaskom predaje dežurstvo
func (h *JournalsHandler) HandlePredajDezurstvo(w http.ResponseWriter, r *http.Request) {
	j, _, ok := h.loadJournal(w, r)
	if !ok {
		return
	}
	u, perms := h.base(r)
	if err := h.journals.PredajDezurstvo(r.Context(), u, perms, j); err != nil {
		redirectWith(w, r, "/dnevnici/"+j.ID, "error", err.Error())
		return
	}
	redirectWith(w, r, "/dnevnici/"+j.ID+"#kraj", "success", "Dežurstvo je predano; razmak je u planu dežurstava.")
}
