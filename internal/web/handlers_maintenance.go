package web

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"gocop/internal/importer/ugovor"
	"gocop/internal/models"
	"gocop/internal/service"
)

// MaintenanceHandler: stranica održavanja po branjenom području — popis
// lokacija izvršenja usluga (što se održava i pod kojom kategorijom) i
// stavke radova koje operateri dopunjuju
type MaintenanceHandler struct {
	maintenance *service.MaintenanceService
	users       *service.UserService
	waters      *service.WatercourseService
	structures  *service.StructureService
	tmpl        *template.Template
}

func NewMaintenanceHandler(m *service.MaintenanceService, users *service.UserService, waters *service.WatercourseService,
	structures *service.StructureService, tmpl *template.Template) *MaintenanceHandler {
	return &MaintenanceHandler{maintenance: m, users: users, waters: waters, structures: structures, tmpl: tmpl}
}

// MaintenanceGroup je skupina popisa: red i skupina vode, pa vrste u njoj
type MaintenanceGroup struct {
	Label string
	Kinds []MaintenanceKindGroup
}

// MaintenanceKindGroup je vrsta lokacije s njezinim redcima
type MaintenanceKindGroup struct {
	Label        string
	PlanPosition string
	Waters       []models.MaintainedWater
}

// MaintenancePageData su podaci stranice
type MaintenancePageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Areas          []models.Area
	Area           *models.Area
	Groups         []MaintenanceGroup
	WaterCount     int
	Unlinked       int
	Items          []models.WorkItem
	InactiveItems  int
	ShowAll        bool
	CanEdit        bool
	Candidates     []models.Watercourse // za vezivanje lokacija: sve vode registra
	Embankments    []models.Structure   // nasipi područja
	Kinds          []string
	ImportReport   *ugovor.Report // izvješće uvoza ugovora, prije upisa
	ImportToken    string         // datoteka koja čeka potvrdu
	ImportFile     string
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *MaintenanceHandler) base(r *http.Request) (*models.User, *models.UserPermissions) {
	u, _ := r.Context().Value(contextKeyUser).(*models.User)
	p, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	return u, p
}

// area čita područje iz upita ili obrasca; kad ga nema, uzima prvo u kojem
// korisnik ima ovlasti, ili prvo uopće
func (h *MaintenanceHandler) area(r *http.Request, perms *models.UserPermissions) (*models.Area, []models.Area) {
	areas, _ := h.users.ListAreas("")
	want, _ := strconv.Atoi(r.FormValue("area"))
	if want == 0 {
		want, _ = strconv.Atoi(r.PathValue("area"))
	}
	if want == 0 && perms != nil {
		for _, a := range areas {
			if perms.AdminAreas[a.ID] || perms.AllowedAreas[a.ID] || perms.AllowedSectors[a.SectorID] || perms.AdminSectors[a.SectorID] {
				want = a.ID
				break
			}
		}
	}
	for i := range areas {
		if areas[i].ID == want {
			return &areas[i], areas
		}
	}
	if len(areas) > 0 && want == 0 {
		return &areas[0], areas
	}
	return nil, areas
}

// ShowMaintenance prikazuje popis lokacija i stavke radova područja
func (h *MaintenanceHandler) ShowMaintenance(w http.ResponseWriter, r *http.Request) {
	data, err := h.pageFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.render(w, data)
}

// pageFor slaže podatke stranice za područje iz zahtjeva
func (h *MaintenanceHandler) pageFor(r *http.Request) (MaintenancePageData, error) {
	ctx := r.Context()
	u, perms := h.base(r)
	area, areas := h.area(r, perms)
	data := MaintenancePageData{
		CurrentUser: u, Permissions: perms, Areas: areas, Area: area, Kinds: models.MaintenanceKinds,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "maintenance", ViewAsBanner: viewBanner(r), ShowAll: r.URL.Query().Get("sve") == "1",
	}
	if area == nil {
		data.ErrorMessage = "Nema branjenog područja s tim brojem."
		return data, nil
	}
	data.CanEdit = h.maintenance.CanEdit(perms, *area)

	waters, err := h.maintenance.ListWaters(ctx, area.ID)
	if err != nil {
		return data, err
	}
	data.WaterCount = len(waters)
	data.Groups = groupWaters(waters)
	for _, mw := range waters {
		if mw.WatercourseCode == "" && mw.StructureID == "" {
			data.Unlinked++
		}
	}

	items, err := h.maintenance.ListItems(ctx, area.ID, true)
	if err != nil {
		return data, err
	}
	for _, it := range items {
		if !it.Active {
			data.InactiveItems++
		}
		if it.Active || data.ShowAll {
			data.Items = append(data.Items, it)
		}
	}

	if data.CanEdit {
		data.Candidates, _ = h.waters.ListWatercourses(ctx, "", "", false)
		data.Embankments, _ = h.structures.List(ctx, "", area.ID, models.StructureKindEmbankment, "")
	}
	return data, nil
}

// HandleAddWater ručno dodaje lokaciju u popis
func (h *MaintenanceHandler) HandleAddWater(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	m := models.MaintainedWater{
		Name: r.FormValue("name"), Seq: strings.TrimSpace(r.FormValue("seq")), Order: r.FormValue("order"),
		Group: r.FormValue("group"), Kind: r.FormValue("kind"), Program: r.FormValue("program"),
	}
	if err := h.maintenance.AddWater(r.Context(), perms, *area, &m, r.FormValue("target")); err != nil {
		redirectWith(w, r, h.back(area)+"#nova-lokacija", "error", err.Error())
		return
	}
	redirectWith(w, r, h.back(area), "success", "Lokacija „"+m.Name+"“ je dodana u popis.")
}

// importDir je mapa u kojoj učitani ugovor čeka potvrdu upisa
func importDir() string {
	d := filepath.Join(os.TempDir(), "gocop-uvoz")
	os.MkdirAll(d, 0o700)
	return d
}

// HandleImportUpload prima ugovor (xlsx) i pokazuje izvješće; ništa ne upisuje
func (h *MaintenanceHandler) HandleImportUpload(w http.ResponseWriter, r *http.Request) {
	u, perms := h.base(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Datoteka nije primljena: "+err.Error())
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Odaberite datoteku ugovora (xlsx).")
		return
	}
	defer f.Close()
	if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".xlsx") {
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Ugovor se uvozi iz radne knjige Excel (.xlsx) koju je napravio dodatak Hrvatskih voda.")
		return
	}
	token := uuid.NewString()
	path := filepath.Join(importDir(), token+".xlsx")
	out, err := os.Create(path)
	if err != nil {
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Datoteka se ne može spremiti: "+err.Error())
		return
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Datoteka se ne može spremiti: "+err.Error())
		return
	}
	out.Close()
	h.showImport(w, r, u, perms, area, token, hdr.Filename, false)
}

// HandleImportWrite upisuje ranije učitani ugovor
func (h *MaintenanceHandler) HandleImportWrite(w http.ResponseWriter, r *http.Request) {
	u, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	token := r.FormValue("token")
	if _, err := uuid.Parse(token); err != nil {
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Učitana datoteka više ne čeka; učitajte je ponovno.")
		return
	}
	h.showImport(w, r, u, perms, area, token, r.FormValue("filename"), true)
}

// showImport pokreće uvoz (izvješće ili upis) i prikazuje stranicu s izvješćem
func (h *MaintenanceHandler) showImport(w http.ResponseWriter, r *http.Request, u *models.User, perms *models.UserPermissions, area *models.Area, token, filename string, write bool) {
	path := filepath.Join(importDir(), token+".xlsx")
	if _, err := os.Stat(path); err != nil {
		redirectWith(w, r, h.back(area)+"#uvoz", "error", "Učitana datoteka više ne čeka; učitajte je ponovno.")
		return
	}
	areas, _ := h.users.ListAreas("")
	aliases := map[string]string{}
	for k, vals := range r.Form {
		if name, ok := strings.CutPrefix(k, "veza:"); ok && len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
			aliases[name] = strings.TrimSpace(vals[0])
		}
	}
	rep, err := h.maintenance.ImportContract(r.Context(), perms, *area, areas, path, aliases, write)
	if err != nil {
		os.Remove(path)
		redirectWith(w, r, h.back(area)+"#uvoz", "error", err.Error())
		return
	}
	if write {
		os.Remove(path)
		redirectWith(w, r, h.back(area), "success",
			"Ugovor je upisan: "+strconv.Itoa(len(rep.Locations))+" lokacija, "+strconv.Itoa(rep.ItemsNew)+" novih stavki radova.")
		return
	}
	// izvješće na istoj stranici, s poljima za ručne veze i gumbom za upis
	r.URL.RawQuery = "area=" + strconv.Itoa(area.ID)
	data, err := h.pageFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.ImportReport, data.ImportToken, data.ImportFile = &rep, token, filename
	h.render(w, data)
}

func (h *MaintenanceHandler) render(w http.ResponseWriter, data MaintenancePageData) {
	if err := h.tmpl.ExecuteTemplate(w, "odrzavanje.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// groupWaters slaže popis kako ga plan navodi: red i skupina, pa vrsta
func groupWaters(waters []models.MaintainedWater) []MaintenanceGroup {
	var groups []MaintenanceGroup
	find := func(label string) *MaintenanceGroup {
		for i := range groups {
			if groups[i].Label == label {
				return &groups[i]
			}
		}
		groups = append(groups, MaintenanceGroup{Label: label})
		return &groups[len(groups)-1]
	}
	for _, mw := range waters {
		label := mw.OrderLabel()
		if label == "" {
			label = "Nerazvrstano"
		}
		label = mw.ProgramOf() + " · " + label
		g := find(label)
		var kg *MaintenanceKindGroup
		for i := range g.Kinds {
			if g.Kinds[i].Label == mw.KindLabel() {
				kg = &g.Kinds[i]
			}
		}
		if kg == nil {
			g.Kinds = append(g.Kinds, MaintenanceKindGroup{Label: mw.KindLabel(), PlanPosition: mw.PlanPosition()})
			kg = &g.Kinds[len(g.Kinds)-1]
		}
		kg.Waters = append(kg.Waters, mw)
	}
	return groups
}

func (h *MaintenanceHandler) back(area *models.Area) string {
	if area == nil {
		return "/odrzavanje"
	}
	return fmt.Sprintf("/odrzavanje?area=%d", area.ID)
}

// HandleSaveItem dodaje ili mijenja stavku radova
func (h *MaintenanceHandler) HandleSaveItem(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	sort, _ := strconv.Atoi(r.FormValue("sort_order"))
	it := models.WorkItem{
		ID: strings.TrimSpace(r.PathValue("id")), Number: r.FormValue("number"), Description: r.FormValue("description"),
		Unit: r.FormValue("unit"), Active: r.FormValue("active") != "0", SortOrder: sort,
	}
	if err := h.maintenance.SaveItem(r.Context(), perms, *area, &it); err != nil {
		redirectWith(w, r, h.back(area), "error", err.Error())
		return
	}
	msg := "Stavka je dodana."
	if r.PathValue("id") != "" {
		msg = "Stavka je spremljena."
	}
	redirectWith(w, r, h.back(area)+"#stavke", "success", msg)
}

// HandleDeleteItem briše stavku (u povijesti ostaje)
func (h *MaintenanceHandler) HandleDeleteItem(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	if err := h.maintenance.DeleteItem(r.Context(), perms, *area, r.PathValue("id")); err != nil {
		redirectWith(w, r, h.back(area), "error", err.Error())
		return
	}
	redirectWith(w, r, h.back(area)+"#stavke", "success", "Stavka je obrisana.")
}

// HandleLinkWater veže lokaciju iz popisa na vodu ili nasip iz registra
func (h *MaintenanceHandler) HandleLinkWater(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	if err := h.maintenance.LinkWater(r.Context(), perms, *area, r.PathValue("id"), r.FormValue("target")); err != nil {
		redirectWith(w, r, h.back(area), "error", err.Error())
		return
	}
	redirectWith(w, r, h.back(area), "success", "Veza je spremljena.")
}

// HandleDeleteWater uklanja lokaciju iz popisa
func (h *MaintenanceHandler) HandleDeleteWater(w http.ResponseWriter, r *http.Request) {
	_, perms := h.base(r)
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/odrzavanje", "error", "Neispravan zahtjev")
		return
	}
	area, _ := h.area(r, perms)
	if area == nil {
		redirectWith(w, r, "/odrzavanje", "error", "Nepoznato područje")
		return
	}
	if err := h.maintenance.DeleteWater(r.Context(), perms, *area, r.PathValue("id")); err != nil {
		redirectWith(w, r, h.back(area), "error", err.Error())
		return
	}
	redirectWith(w, r, h.back(area), "success", "Lokacija je uklonjena iz popisa.")
}
