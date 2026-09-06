package web

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Registar organizacije: sektori i branjena područja. Program ih ne zna sam,
// pa je ovo prvo što globalni administrator upisuje na novom čvoru; sve
// ostalo (ovlasti, dionice, zaduženja) veže se na njih.

type OrgHandler struct {
	org           *service.OrgService
	tmplList      *template.Template
	tmplSector    *template.Template
	tmplArea      *template.Template
	tmplContr     *template.Template
	tmplContrList *template.Template
}

func NewOrgHandler(org *service.OrgService, list, sector, area, contractor, contractors *template.Template) *OrgHandler {
	return &OrgHandler{org: org, tmplList: list, tmplSector: sector, tmplArea: area,
		tmplContr: contractor, tmplContrList: contractors}
}

// SectorView je sektor sa svojim branjenim područjima i firmama cijelog sektora
type SectorView struct {
	models.Sector
	Areas       []AreaView
	Contractors []models.Contractor
}

// AreaView je područje sa svojim licenciranim firmama
type AreaView struct {
	models.Area
	Contractors []models.Contractor
}

type OrgPageData struct {
	CurrentUser    *models.User
	Permissions    *models.UserPermissions
	Level1         []SectorView // krovne jedinice (razina 1)
	Sectors        []SectorView
	Orphans        []models.Area // područja čiji sektor ne postoji
	TotalAreas     int
	Sector         models.Sector
	Area           models.Area
	SectorOptions  []models.Sector
	Terms          models.OrgTerms
	Contractors    []models.Contractor
	Contractor     models.Contractor
	SectorChecked  map[string]bool // obrazac firme: gdje radi
	AreaChecked    map[int]bool
	RoleRows       []roleRow   // sudionici obrane s nazivima organizacije
	RoleGroups     []roleGroup // isti sudionici po skupinama, za kartice
	IsEdit         bool
	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *OrgHandler) pageData(r *http.Request) OrgPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return OrgPageData{
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "organizacija", ViewAsBanner: viewBanner(r), Terms: models.Terms(), RoleRows: roleRows(), RoleGroups: roleGroups(),
	}
}

// ShowOrganization prikazuje sektore s branjenim područjima
func (h *OrgHandler) ShowOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	sectors, err := h.org.ListSectors(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	areas, _ := h.org.ListAreas(ctx, "")
	data.TotalAreas = len(areas)
	idx, _ := h.org.ContractorIndex(ctx)
	bySector := map[string][]AreaView{}
	known := map[string]bool{}
	for _, s := range sectors {
		known[s.ID] = true
	}
	for _, a := range areas {
		if known[a.SectorID] {
			bySector[a.SectorID] = append(bySector[a.SectorID], AreaView{Area: a, Contractors: append(append([]models.Contractor{}, idx.BySector[a.SectorID]...), idx.ByArea[a.ID]...)})
		} else {
			data.Orphans = append(data.Orphans, a)
		}
	}
	for _, s := range sectors {
		v := SectorView{Sector: s, Areas: bySector[s.ID], Contractors: idx.BySector[s.ID]}
		if s.IsLevel1() {
			data.Level1 = append(data.Level1, v)
		} else {
			data.Sectors = append(data.Sectors, v)
		}
	}
	if err := h.tmplList.ExecuteTemplate(w, "organizacija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *OrgHandler) requireAdmin(w http.ResponseWriter, data OrgPageData) bool {
	if data.Permissions == nil || !data.Permissions.IsGlobalAdmin {
		http.Error(w, "Organizaciju uređuje globalni administrator", http.StatusForbidden)
		return false
	}
	return true
}

// ShowSectorForm prikazuje obrazac za novi sektor ili izmjenu postojećeg
func (h *OrgHandler) ShowSectorForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.requireAdmin(w, data) {
		return
	}
	if r.URL.Query().Get("razina") == "1" {
		data.Sector.Level = 1
	}
	if id := r.PathValue("id"); id != "" {
		s, err := h.org.GetSector(r.Context(), id)
		if err != nil || s == nil {
			http.NotFound(w, r)
			return
		}
		data.Sector, data.IsEdit = *s, true
	}
	if err := h.tmplSector.ExecuteTemplate(w, "sector_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ShowAreaForm prikazuje obrazac za novo branjeno područje ili izmjenu postojećeg
func (h *OrgHandler) ShowAreaForm(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	if !h.requireAdmin(w, data) {
		return
	}
	data.SectorOptions, _ = h.org.ListSectors(r.Context())
	if raw := r.PathValue("id"); raw != "" {
		id, _ := strconv.Atoi(raw)
		a, err := h.org.GetArea(r.Context(), id)
		if err != nil || a == nil {
			http.NotFound(w, r)
			return
		}
		data.Area, data.IsEdit = *a, true
	} else {
		data.Area.SectorID = strings.ToUpper(r.URL.Query().Get("sector"))
	}
	if err := h.tmplArea.ExecuteTemplate(w, "area_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeCSV šalje tablicu kao CSV za Excel: UTF-8 s oznakom BOM i točka-zarez,
// kako ga hrvatski Excel otvara izravno u stupce
func writeCSV(w http.ResponseWriter, name string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.UseCRLF = true
	cw.WriteAll(rows)
}

// ExportSectorsCSV daje tablicu sektora kao CSV
func (h *OrgHandler) ExportSectorsCSV(w http.ResponseWriter, r *http.Request) {
	sectors, err := h.org.ListSectors(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t := models.Terms()
	rows := [][]string{{"Oznaka", "Naziv", t.SectorOffice, t.Center, "Adresa", "Telefon", "E-pošta", "Razina"}}
	for _, s := range sectors {
		rows = append(rows, []string{s.ID, s.Name, s.VgoName, s.CenterCop, s.Address, s.Phone, s.Email, strconv.Itoa(s.Level)})
	}
	writeCSV(w, "sektori.csv", rows)
}

// ExportAreasCSV daje tablicu branjenih područja kao CSV
func (h *OrgHandler) ExportAreasCSV(w http.ResponseWriter, r *http.Request) {
	areas, err := h.org.ListAreas(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t := models.Terms()
	idx, _ := h.org.ContractorIndex(r.Context())
	rows := [][]string{{t.Sector, t.AreaShort, "Naziv", t.AreaOffice, t.Subcenter, "Licencirane firme", "Izravno pod razinom 2"}}
	for _, a := range areas {
		direct := ""
		if a.DirectToSector {
			direct = "da"
		}
		var names []string
		for _, c := range append(idx.BySector[a.SectorID], idx.ByArea[a.ID]...) {
			names = append(names, c.Name)
		}
		rows = append(rows, []string{a.SectorID, strconv.Itoa(a.ID), a.Name, a.VgiName, a.Subcenter, strings.Join(names, ", "), direct})
	}
	writeCSV(w, "branjena-podrucja.csv", rows)
}

// readCSV čita CSV kakav daje izvoz ili Excel: s oznakom BOM ili bez nje,
// s točka-zarezom ili zarezom. Prvi redak je zaglavlje kad ne izgleda kao podatak.
func readCSV(file io.Reader) ([][]string, error) {
	data, err := io.ReadAll(io.LimitReader(file, 4<<20))
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	first, _, _ := strings.Cut(string(data), "\n")
	cr := csv.NewReader(bytes.NewReader(data))
	cr.Comma = ','
	if strings.Count(first, ";") >= strings.Count(first, ",") {
		cr.Comma = ';'
	}
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV se ne može pročitati: %w", err)
	}
	var out [][]string
	for _, r := range rows {
		empty := true
		for i := range r {
			r[i] = strings.TrimSpace(r[i])
			if r[i] != "" {
				empty = false
			}
		}
		if !empty {
			out = append(out, r)
		}
	}
	return out, nil
}

// isHeader prepoznaje redak zaglavlja: prvi stupac je naziv, ne oznaka
func isHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	c := strings.ToLower(row[0])
	t := models.Terms()
	return c == "oznaka" || c == strings.ToLower(t.Sector) || c == "sektor" || c == "sector"
}

func cell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

// HandleImportCSV uvozi sektore ili područja iz CSV-a istog oblika kao izvoz.
// Postojeći se osvježe, novi se dodaju; redci s greškom se preskaču i navedu.
func (h *OrgHandler) HandleImportCSV(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev ili prevelika datoteka")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	file, _, err := r.FormFile("csv")
	if err != nil {
		redirectWith(w, r, "/organizacija", "error", "Odaberite CSV datoteku")
		return
	}
	defer file.Close()
	rows, err := readCSV(file)
	if err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	if len(rows) > 0 && isHeader(rows[0]) {
		rows = rows[1:]
	}
	kind := r.FormValue("kind")
	ctx := r.Context()
	var added, updated int
	var errs []string
	for i, row := range rows {
		var err error
		switch kind {
		case "podrucja":
			id, _ := strconv.Atoi(cell(row, 1))
			direct := strings.ToLower(cell(row, 6))
			a := &models.Area{SectorID: cell(row, 0), ID: id, Name: cell(row, 2), VgiName: cell(row, 3),
				Subcenter: cell(row, 4), DirectToSector: direct == "da" || direct == "1" || direct == "x"}
			existing, _ := h.org.GetArea(ctx, id)
			if err = h.org.SaveArea(ctx, perms, a, existing == nil); err == nil {
				if existing == nil {
					added++
				} else {
					updated++
				}
			}
		default:
			level, _ := strconv.Atoi(cell(row, 7))
			s := &models.Sector{ID: cell(row, 0), Name: cell(row, 1), VgoName: cell(row, 2), CenterCop: cell(row, 3),
				Address: cell(row, 4), Phone: cell(row, 5), Email: cell(row, 6), Level: level}
			existing, _ := h.org.GetSector(ctx, strings.ToUpper(s.ID))
			if err = h.org.SaveSector(ctx, perms, s, existing == nil); err == nil {
				if existing == nil {
					added++
				} else {
					updated++
				}
			}
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("redak %d: %s", i+1, err.Error()))
		}
	}
	msg := fmt.Sprintf("Uvoz: %d novih, %d osvježenih.", added, updated)
	if len(errs) > 0 {
		redirectWith(w, r, "/organizacija", "error", msg+" Preskočeno: "+strings.Join(errs, "; "))
		return
	}
	redirectWith(w, r, "/organizacija", "success", msg)
}

// ShowContractors je registar licenciranih firmi kao vlastita stranica pod Registrima
func (h *OrgHandler) ShowContractors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	data.ActiveNav = "firme"
	data.Contractors, _ = h.org.ListContractors(ctx)
	sectors, _ := h.org.ListSectors(ctx)
	for _, s := range sectors {
		data.Sectors = append(data.Sectors, SectorView{Sector: s})
	}
	if err := h.tmplContrList.ExecuteTemplate(w, "firme.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ExportContractorsCSV daje registar licenciranih firmi kao CSV
func (h *OrgHandler) ExportContractorsCSV(w http.ResponseWriter, r *http.Request) {
	list, err := h.org.ListContractors(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := [][]string{{"Naziv", "Kratki naziv", "OIB", "Adresa", "Telefon", "E-pošta", "Osoba za obranu", "Gdje radi", "Aktivan", "Napomena"}}
	for _, c := range list {
		var where []string
		for _, a := range c.Assignments {
			where = append(where, a.Where())
		}
		active := "da"
		if !c.Active {
			active = "ne"
		}
		rows = append(rows, []string{c.Name, c.ShortName, c.OIB, c.Address, c.Phone, c.Email, c.Contact, strings.Join(where, ", "), active, c.Notes})
	}
	writeCSV(w, "licencirane-firme.csv", rows)
}

// ShowContractorForm prikazuje obrazac za novu ili postojeću firmu
func (h *OrgHandler) ShowContractorForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	data.ActiveNav = "firme"
	if !h.requireAdmin(w, data) {
		return
	}
	data.SectorChecked, data.AreaChecked = map[string]bool{}, map[int]bool{}
	if id := r.PathValue("id"); id != "" {
		c, err := h.org.GetContractor(ctx, id)
		if err != nil || c == nil {
			http.NotFound(w, r)
			return
		}
		data.Contractor, data.IsEdit = *c, true
		for _, a := range c.Assignments {
			if a.AreaID > 0 {
				data.AreaChecked[a.AreaID] = true
			} else {
				data.SectorChecked[a.SectorID] = true
			}
		}
	}
	sectors, _ := h.org.ListSectors(ctx)
	areas, _ := h.org.ListAreas(ctx, "")
	for _, s := range sectors {
		v := SectorView{Sector: s}
		for _, a := range areas {
			if a.SectorID == s.ID {
				v.Areas = append(v.Areas, AreaView{Area: a})
			}
		}
		data.Sectors = append(data.Sectors, v)
	}
	if err := h.tmplContr.ExecuteTemplate(w, "contractor_form.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleSaveContractor upisuje firmu i gdje radi
func (h *OrgHandler) HandleSaveContractor(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	c := &models.Contractor{ID: f("id"), Name: f("name"), ShortName: f("short_name"), OIB: f("oib"), Address: f("address"),
		Phone: f("phone"), Email: f("email"), Contact: f("contact"), Notes: f("notes"), Active: r.FormValue("active") == "1"}
	var where []models.ContractorAssignment
	for _, s := range r.Form["sector"] {
		where = append(where, models.ContractorAssignment{SectorID: s})
	}
	for _, a := range r.Form["area"] {
		if id, err := strconv.Atoi(a); err == nil && id > 0 {
			where = append(where, models.ContractorAssignment{AreaID: id})
		}
	}
	back := "/firme/new"
	if c.ID != "" {
		back = "/firme/" + c.ID + "/edit"
	}
	if err := h.org.SaveContractor(r.Context(), perms, c, where); err != nil {
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/firme", "success", "Firma "+c.Name+" je spremljena.")
}

// HandleDeleteContractor briše firmu
func (h *OrgHandler) HandleDeleteContractor(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := h.org.DeleteContractor(r.Context(), perms, r.FormValue("id")); err != nil {
		redirectWith(w, r, "/firme", "error", err.Error())
		return
	}
	redirectWith(w, r, "/firme", "success", "Firma je obrisana.")
}

// roleRow je jedna uloga na stranici organizacije: zadani naziv, naziv
// organizacije i što uloga smije
type roleRow struct {
	Role, Group, Name, Custom, Label, Desc string
}

func roleRows() []roleRow {
	t := models.Terms()
	out := make([]roleRow, 0, len(models.RoleCatalog))
	for _, d := range models.RoleCatalog {
		out = append(out, roleRow{Role: string(d.Role), Group: d.Group, Name: d.Name,
			Custom: t.RoleLabels[string(d.Role)], Label: d.Role.Label(), Desc: d.Desc})
	}
	return out
}

// roleGroup su uloge jedne skupine (razine)
type roleGroup struct {
	Group string
	Rows  []roleRow
}

func roleGroups() []roleGroup {
	var out []roleGroup
	for _, r := range roleRows() {
		if len(out) == 0 || out[len(out)-1].Group != r.Group {
			out = append(out, roleGroup{Group: r.Group})
		}
		out[len(out)-1].Rows = append(out[len(out)-1].Rows, r)
	}
	return out
}

// readLogo čita učitani znak i prepoznaje mu vrstu; prima SVG, PNG, JPEG i WebP
// do models.LogoMaxBytes
func readLogo(file io.Reader, name string) (mime string, data []byte, err error) {
	data, err = io.ReadAll(io.LimitReader(file, models.LogoMaxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("čitanje znaka: %w", err)
	}
	if len(data) > models.LogoMaxBytes {
		return "", nil, fmt.Errorf("znak smije imati najviše %d KB", models.LogoMaxBytes/1024)
	}
	if len(data) == 0 {
		return "", nil, errors.New("datoteka znaka je prazna")
	}
	head := strings.ToLower(string(data[:min(len(data), 512)]))
	switch {
	case strings.Contains(head, "<svg") || (strings.HasSuffix(strings.ToLower(name), ".svg") && strings.Contains(head, "<?xml")):
		if strings.Contains(strings.ToLower(string(data)), "<script") {
			return "", nil, errors.New("SVG znak ne smije sadržavati skripte")
		}
		return "image/svg+xml", data, nil
	}
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return ct, data, nil
	}
	return "", nil, errors.New("znak mora biti slika: SVG, PNG, JPEG ili WebP")
}

// ServeLogo daje znak organizacije; bez vlastitog znaka vraća ugrađeni. Ne
// traži prijavu, jer ga pokazuje i stranica prijave.
func ServeLogo(w http.ResponseWriter, r *http.Request) {
	t := models.Terms()
	if !t.HasLogo() {
		http.Redirect(w, r, "/static/img/hv-mark.svg", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", t.LogoMime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "logo", t.UpdatedAt, bytes.NewReader(t.Logo))
}

// LogoURL je adresa znaka za predloške; mijenja se s verzijom, da preglednik
// ne drži stari znak u međuspremniku
func LogoURL() string {
	t := models.Terms()
	if !t.HasLogo() {
		return "/static/img/hv-mark.svg"
	}
	return fmt.Sprintf("/logo?v=%d", t.UpdatedAt.Unix())
}

// HandleSaveTerms mijenja nazive razina ustroja (za organizacije koje ih zovu drukčije)
func (h *OrgHandler) HandleSaveTerms(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev ili prevelika datoteka")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	current, err := h.org.Terms(r.Context())
	if err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	t := models.OrgTerms{
		LogoMime: current.LogoMime, Logo: current.Logo, LoginInfo: r.FormValue("login_info"),
		OrgName: f("org_name"), OrgLegalForm: f("org_legal_form"), OrgRegistryNo: f("org_registry_no"), OrgTaxID: f("org_tax_id"),
		Level1Unit: f("level1_unit"), Level1Center: f("level1_center"), Level1CenterShort: f("level1_center_short"),
		Sector: f("sector"), Sectors: f("sectors"), SectorOffice: f("sector_office"), SectorOfficeShort: f("sector_office_short"),
		Center: f("center"), CenterShort: f("center_short"),
		Area: f("area"), Areas: f("areas"), AreaShort: f("area_short"), AreaOffice: f("area_office"), AreaOfficeShort: f("area_office_short"),
		Subcenter: f("subcenter"),
	}
	t.RoleLabels = map[string]string{}
	for _, d := range models.RoleCatalog {
		t.RoleLabels[string(d.Role)] = f("role_" + string(d.Role))
	}
	if r.FormValue("logo_remove") == "1" {
		t.LogoMime, t.Logo = "", nil
	}
	if file, hdr, err := r.FormFile("logo"); err == nil {
		defer file.Close()
		mime, data, err := readLogo(file, hdr.Filename)
		if err != nil {
			redirectWith(w, r, "/organizacija", "error", err.Error())
			return
		}
		t.LogoMime, t.Logo = mime, data
	}
	if err := h.org.SaveTerms(r.Context(), perms, t); err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Nazivi razina su spremljeni i vrijede na svim čvorovima mreže.")
}

func sectorFromForm(r *http.Request) *models.Sector {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	level, _ := strconv.Atoi(f("level"))
	return &models.Sector{ID: f("id"), Name: f("name"), VgoName: f("vgo_name"), CenterCop: f("center_cop"),
		Address: f("address"), Phone: f("phone"), Email: f("email"), Level: level}
}

func areaFromForm(r *http.Request) *models.Area {
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	id, _ := strconv.Atoi(f("id"))
	return &models.Area{ID: id, SectorID: f("sector_id"), Name: f("name"), VgiName: f("vgi_name"),
		Subcenter: f("subcenter"), DirectToSector: r.FormValue("direct_to_sector") == "1"}
}

// HandleSaveSector upisuje sektor iz obrasca (nov ili izmijenjen)
func (h *OrgHandler) HandleSaveSector(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	sec := sectorFromForm(r)
	isNew := r.FormValue("is_new") == "1"
	if err := h.org.SaveSector(r.Context(), perms, sec, isNew); err != nil {
		back := "/organizacija/sektori/new"
		if !isNew {
			back = "/organizacija/sektori/" + sec.ID + "/edit"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Sektor "+sec.ID+" je upisan.")
}

// HandleDeleteSector briše sektor na koji se ništa ne veže
func (h *OrgHandler) HandleDeleteSector(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	if err := h.org.DeleteSector(r.Context(), perms, r.FormValue("id")); err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Sektor je obrisan.")
}

// HandleSaveArea upisuje branjeno područje iz obrasca (novo ili izmijenjeno)
func (h *OrgHandler) HandleSaveArea(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	a := areaFromForm(r)
	isNew := r.FormValue("is_new") == "1"
	if err := h.org.SaveArea(r.Context(), perms, a, isNew); err != nil {
		back := "/organizacija/podrucja/new?sector=" + a.SectorID
		if !isNew {
			back = "/organizacija/podrucja/" + strconv.Itoa(a.ID) + "/edit"
		}
		redirectWith(w, r, back, "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Branjeno područje "+strconv.Itoa(a.ID)+" je upisano.")
}

// HandleDeleteArea briše branjeno područje na koje se ništa ne veže
func (h *OrgHandler) HandleDeleteArea(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/organizacija", "error", "Neispravan zahtjev")
		return
	}
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id, _ := strconv.Atoi(r.FormValue("id"))
	if err := h.org.DeleteArea(r.Context(), perms, id); err != nil {
		redirectWith(w, r, "/organizacija", "error", err.Error())
		return
	}
	redirectWith(w, r, "/organizacija", "success", "Branjeno područje je obrisano.")
}
