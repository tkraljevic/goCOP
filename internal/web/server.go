package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gocop/internal/hydro"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
)

type contextKey string

const (
	contextKeyUser    contextKey = "current_user"
	contextKeyPerms   contextKey = "current_perms"
	contextKeyRealUsr contextKey = "real_user"  // prijavljeni administrator
	contextKeyViewing contextKey = "viewing_as" // gleda li se tuđim očima
	contextKeySession contextKey = "session_id"
	contextKeyModules contextKey = "modules"
)

type Server struct {
	addr               string
	authService        *service.AuthService
	userService        *service.UserService
	sectionService     *service.SectionService
	territoryService   *service.TerritoryService
	stationService     *service.StationService
	watercourseService *service.WatercourseService
	structureService   *service.StructureService
	readingService     *service.ReadingService
	moduleService      *service.ModuleService
	maintenanceService *service.MaintenanceService
	journalService     *service.JournalService
	orgService         *service.OrgService
	support            SupportContact
	followRepo         *repository.FollowRepository
	onFollowChange     func()
	peersService       *peers.Service
	db                 *sql.DB
	dbPath             string
	recorder           *ledger.Recorder
	sseBroker          *service.SSEBroker
	templates          map[string]*template.Template
	mux                *http.ServeMux
}

func NewServer(
	addr string,
	authService *service.AuthService,
	userService *service.UserService,
	sectionService *service.SectionService,
	territoryService *service.TerritoryService,
	stationService *service.StationService,
	watercourseService *service.WatercourseService,
	structureService *service.StructureService,
	readingService *service.ReadingService,
	moduleService *service.ModuleService,
	maintenanceService *service.MaintenanceService,
	journalService *service.JournalService,
	orgService *service.OrgService,
	support SupportContact,
	followRepo *repository.FollowRepository,
	onFollowChange func(),
	peersService *peers.Service,
	recorder *ledger.Recorder,
	sseBroker *service.SSEBroker,
) (*Server, error) {
	// Parsiranje HTML predložaka iz ugrađenog embed.FS
	tmplFuncs := template.FuncMap{
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("02.01.2006 15:04")
		},
		"formatDateShort": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "Trajno"
			}
			return t.Format("02.01.2006")
		},
		"roleLabel": func(r any) string {
			switch v := r.(type) {
			case models.Role:
				return v.Label()
			case string:
				return models.Role(v).Label()
			}
			return ""
		},
		"orgLabel": func(o models.OrgType) string {
			return o.Label()
		},
		"derefString": func(s *string) string {
			if s == nil {
				return "-"
			}
			return *s
		},
		// json ugrađuje podatke u onclick atribut; predložak ih mora dobiti kao
		// ispravan JS literal, a ne kao escapirani tekst
		"json": func(v any) template.JS {
			data, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(data)
		},
		"bankLabel":   hydro.BankLabel,
		"muniType":    models.MunicipalityTypeLabel,
		"term":        func(key string) string { return models.Terms().Get(key) },
		"terml":       func(key string) string { return models.Terms().Lower(key) },
		"logo":        LogoURL,
		"roleOptions": roleOptions,
		"humanBytes":  humanBytes,
		"km":          models.FormatKm,
		"derefTime": func(t *time.Time) time.Time {
			if t == nil {
				return time.Time{}
			}
			return *t
		},
		"derefFloat": func(f *float64, decimals int) string {
			if f == nil {
				return "-"
			}
			return strconv.FormatFloat(*f, 'f', decimals, 64)
		},
		// dionice sklanja broj dionica po hrvatskoj gramatici (1 dionica,
		// 2-4 dionice, 5+ dionica), uz iznimku za brojeve 11-14
		"postaje": func(n int) string {
			last2 := n % 100
			last := n % 10
			switch {
			case last2 >= 11 && last2 <= 14:
				return fmt.Sprintf("%d postaja", n)
			case last == 1:
				return fmt.Sprintf("%d postaja", n)
			case last >= 2 && last <= 4:
				return fmt.Sprintf("%d postaje", n)
			default:
				return fmt.Sprintf("%d postaja", n)
			}
		},
		"dionice": func(n int) string {
			last2 := n % 100
			last := n % 10
			switch {
			case last2 >= 11 && last2 <= 14:
				return fmt.Sprintf("%d dionica", n)
			case last == 1:
				return fmt.Sprintf("%d dionica", n)
			case last >= 2 && last <= 4:
				return fmt.Sprintf("%d dionice", n)
			default:
				return fmt.Sprintf("%d dionica", n)
			}
		},
		"add":            func(a, b int) int { return a + b },
		"structureKind":  models.StructureKindLabel,
		"readingSource":  models.ReadingSourceLabel,
		"structureState": models.StructureStateLabel,
		"gateLabel":      models.GateLabel,
		"localTime":      func(t time.Time) time.Time { return t.In(models.Zagreb) },
		"intOf": func(i *int) int {
			if i == nil {
				return 0
			}
			return *i
		},
		"ago":             humanAgo,
		"journalKind":     models.JournalKindLabel,
		"maintenanceKind": models.MaintenanceKindLabel,
		"entryKind":       models.EntryKindLabel,
		"ratingLabel":     models.RatingLabel,
		"countsText":      models.CountsText,
		"now":             func() time.Time { return time.Now().In(models.Zagreb) },
		// icon crta ikonu iz ugrađenog SVG sprajta (Lucide, ISC). Ikone su
		// dio binarne datoteke, pa izgledaju isto na svakom uređaju i rade
		// bez mreže — emoji su se crtali drukčije na svakom sustavu.
		"icon": func(name string) template.HTML {
			return template.HTML(`<svg class="icon" aria-hidden="true"><use href="/static/img/icons.svg#` +
				template.HTMLEscapeString(name) + `"/></svg>`)
		},
		"thresholdInput": func(t models.Threshold) string {
			if t.Cm != nil {
				return fmt.Sprintf("%+d", *t.Cm)
			}
			return t.Raw
		},
		"derefInt": func(i *int) string {
			if i == nil {
				return "-"
			}
			return fmt.Sprintf("%d", *i)
		},
		"renderMarkdown": func(input string) template.HTML {
			if strings.TrimSpace(input) == "" {
				return ""
			}
			var buf bytes.Buffer
			if err := goldmark.Convert([]byte(input), &buf); err != nil {
				return template.HTML(html.EscapeString(input))
			}
			return template.HTML(buf.String())
		},
	}

	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		return nil, fmt.Errorf("greška pri pristupu templates mapi: %w", err)
	}

	templates := make(map[string]*template.Template)

	// Predlošci koji proširuju base.html
	for _, page := range []string{"dashboard.html", "registri.html", "users.html", "user_detail.html", "user_form.html", "duty_form.html", "profile.html", "sections.html", "section_detail.html", "section_form.html", "territories.html", "county_form.html", "municipality_form.html", "municipality_detail.html", "stations.html", "station_detail.html", "station_form.html", "watercourses.html", "watercourse_detail.html", "watercourse_form.html", "structures.html", "structure_detail.html", "structure_form.html", "readings.html", "reading_history.html", "reading_form.html", "teren.html", "moduli.html", "settings.html", "odrzavanje.html", "organizacija.html", "sector_form.html", "area_form.html",
		"administracija.html", "uvozi.html", "sinkronizacija.html", "pretplate.html", "baza.html",
		"dnevnici.html", "dnevnik_form.html", "dnevnik.html", "dnevnik_list.html"} {
		t, err := template.New("base.html").Funcs(tmplFuncs).ParseFS(templatesFS, "base.html", page)
		if err != nil {
			return nil, fmt.Errorf("greška pri parsiranju predloška %s: %w", page, err)
		}
		templates[page] = t
	}

	// Samostalne stranice: prijava i ispis dnevnika
	for _, page := range []string{"login.html", "dnevnik_ispis.html", "uparivanje.html"} {
		t, err := template.New(page).Funcs(tmplFuncs).ParseFS(templatesFS, page)
		if err != nil {
			return nil, fmt.Errorf("greška pri parsiranju predloška %s: %w", page, err)
		}
		templates[page] = t
	}

	s := &Server{
		addr:               addr,
		authService:        authService,
		userService:        userService,
		sectionService:     sectionService,
		territoryService:   territoryService,
		stationService:     stationService,
		watercourseService: watercourseService,
		structureService:   structureService,
		maintenanceService: maintenanceService,
		journalService:     journalService,
		orgService:         orgService,
		readingService:     readingService,
		moduleService:      moduleService,
		support:            support,
		followRepo:         followRepo,
		onFollowChange:     onFollowChange,
		peersService:       peersService,
		recorder:           recorder,
		sseBroker:          sseBroker,
		templates:          templates,
		mux:                http.NewServeMux(),
	}

	s.setupRoutes()
	return s, nil
}

func (s *Server) setupRoutes() {
	authH := NewAuthHandler(s.authService, s.templates["login.html"])
	authH.SetSupport(s.support)
	authH.SetAdminContact(s.userService.GlobalAdminContact)
	usersH := NewUsersHandler(s.userService, s.templates["users.html"])
	usersH.SetPageTemplates(s.templates["user_detail.html"], s.templates["user_form.html"], s.templates["duty_form.html"], s.templates["profile.html"])
	dashH := NewDashboardHandler(s.userService, s.templates["dashboard.html"], s.templates["registri.html"])
	sectionsH := NewSectionsHandler(s.sectionService, s.userService, s.templates["sections.html"])
	sectionsH.SetPageTemplates(s.templates["section_detail.html"], s.templates["section_form.html"], s.stationService, s.territoryService)
	territoriesH := NewTerritoriesHandler(s.territoryService, s.templates["territories.html"])
	territoriesH.SetPageTemplates(s.templates["county_form.html"], s.templates["municipality_form.html"], s.templates["municipality_detail.html"])
	stationsH := NewStationsHandler(s.stationService, s.templates["stations.html"])
	stationsH.SetPageTemplates(s.templates["station_detail.html"], s.templates["station_form.html"], s.sectionService, s.watercourseService)
	watercoursesH := NewWatercoursesHandler(s.watercourseService, s.sectionService, s.templates["watercourses.html"])
	structuresH := NewStructuresHandler(s.structureService, s.stationService, s.sectionService, s.userService,
		s.templates["structures.html"], s.templates["structure_detail.html"], s.templates["structure_form.html"])
	sectionsH.SetStructureService(s.structureService)
	sectionsH.SetWatercourseService(s.watercourseService)
	modulesH := NewModulesHandler(s.moduleService, s.templates["moduli.html"])
	s.mux.Handle("GET /moduli", s.authMiddleware(http.HandlerFunc(modulesH.ShowMatrix)))
	s.mux.Handle("POST /moduli/save", s.authMiddleware(http.HandlerFunc(modulesH.HandleSave)))
	usersH.SetModuleService(s.moduleService)
	s.mux.Handle("POST /users/{id}/modules", s.authMiddleware(http.HandlerFunc(usersH.HandleUserModules)))
	fieldH := NewFieldHandler(s.readingService, s.userService, s.templates["teren.html"])
	s.mux.Handle("GET /teren", s.authMiddleware(http.HandlerFunc(fieldH.ShowField)))
	readingsH := NewReadingsHandler(s.readingService, s.stationService, s.structureService, s.userService,
		s.templates["readings.html"], s.templates["reading_history.html"], s.templates["reading_form.html"])
	readingsH.SetFollow(s.followRepo, s.onFollowChange)
	watercoursesH.SetPageTemplates(s.templates["watercourse_detail.html"], s.templates["watercourse_form.html"], s.stationService)
	watercoursesH.SetMaintenanceService(s.maintenanceService)
	maintenanceH := NewMaintenanceHandler(s.maintenanceService, s.userService, s.watercourseService, s.structureService, s.templates["odrzavanje.html"])
	s.mux.Handle("GET /odrzavanje", s.authMiddleware(http.HandlerFunc(maintenanceH.ShowMaintenance)))
	s.mux.Handle("POST /odrzavanje/stavke", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleSaveItem)))
	s.mux.Handle("POST /odrzavanje/stavke/{id}", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleSaveItem)))
	s.mux.Handle("POST /odrzavanje/stavke/{id}/delete", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleDeleteItem)))
	s.mux.Handle("POST /odrzavanje/lokacije/{id}/veza", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleLinkWater)))
	s.mux.Handle("POST /odrzavanje/lokacije/{id}/delete", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleDeleteWater)))
	s.mux.Handle("POST /odrzavanje/lokacije", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleAddWater)))
	s.mux.Handle("POST /odrzavanje/uvoz", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleImportUpload)))
	s.mux.Handle("POST /odrzavanje/uvoz/upisi", s.authMiddleware(http.HandlerFunc(maintenanceH.HandleImportWrite)))
	journalsH := NewJournalsHandler(s.journalService, s.userService, s.maintenanceService, s.sectionService, s.stationService,
		s.templates["dnevnici.html"], s.templates["dnevnik_form.html"], s.templates["dnevnik.html"], s.templates["dnevnik_list.html"], s.templates["dnevnik_ispis.html"])
	s.mux.Handle("GET /dnevnici", s.authMiddleware(http.HandlerFunc(journalsH.ShowJournals)))
	s.mux.Handle("GET /dnevnici/new", s.authMiddleware(http.HandlerFunc(journalsH.ShowJournalForm)))
	s.mux.Handle("POST /dnevnici", s.authMiddleware(http.HandlerFunc(journalsH.HandleSaveJournal)))
	s.mux.Handle("GET /dnevnici/{id}", s.authMiddleware(http.HandlerFunc(journalsH.ShowJournal)))
	s.mux.Handle("GET /dnevnici/{id}/edit", s.authMiddleware(http.HandlerFunc(journalsH.ShowJournalForm)))
	s.mux.Handle("POST /dnevnici/{id}", s.authMiddleware(http.HandlerFunc(journalsH.HandleSaveJournal)))
	s.mux.Handle("GET /dnevnici/{id}/ispis", s.authMiddleware(http.HandlerFunc(journalsH.ShowPrint)))
	s.mux.Handle("POST /dnevnici/{id}/listovi", s.authMiddleware(http.HandlerFunc(journalsH.HandleOpenSheet)))
	s.mux.Handle("GET /dnevnici/{id}/listovi/{sheet}", s.authMiddleware(http.HandlerFunc(journalsH.ShowSheet)))
	s.mux.Handle("POST /dnevnici/{id}/listovi/{sheet}/uvjeti", s.authMiddleware(http.HandlerFunc(journalsH.HandleSheetConditions)))
	s.mux.Handle("POST /dnevnici/{id}/listovi/{sheet}/vrijeme", s.authMiddleware(http.HandlerFunc(journalsH.HandleSheetWeather)))
	s.mux.Handle("POST /dnevnici/{id}/listovi/{sheet}/potvrdi", s.authMiddleware(http.HandlerFunc(journalsH.HandleConfirmSheet)))
	s.mux.Handle("POST /dnevnici/{id}/listovi/{sheet}/upisi", s.authMiddleware(http.HandlerFunc(journalsH.HandleAddEntry)))
	s.mux.Handle("POST /dnevnici/{id}/upisi/{entry}/storno", s.authMiddleware(http.HandlerFunc(journalsH.HandleVoidEntry)))
	s.mux.Handle("POST /dnevnici/{id}/upisi/{entry}/stanje", s.authMiddleware(http.HandlerFunc(journalsH.HandleTaskStatus)))
	settingsH := NewSettingsHandler(s.peersService, s.recorder, s.templates["settings.html"])
	sseH := NewSSEHandler(s.sseBroker)

	// Statičke datoteke (CSS, JS) poslužene iz embed.FS
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err == nil {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
		s.mux.HandleFunc("GET /logo", ServeLogo)
	}

	// Uparivanje: prijavljenima uvijek, neprijavljenima dok je čvor svjež
	pairH := NewPairHandler(s.peersService, s.authService, s.userService, s.templates["uparivanje.html"])
	authH.SetFresh(pairH.Fresh)
	s.mux.Handle("GET /uparivanje", pairH.Gate(http.HandlerFunc(pairH.ShowWizard)))
	s.mux.Handle("GET /api/uparivanje/status", pairH.Gate(http.HandlerFunc(pairH.HandleStatus)))
	s.mux.Handle("POST /api/uparivanje/listen", pairH.Gate(http.HandlerFunc(pairH.HandleListen)))
	s.mux.Handle("POST /api/uparivanje/stop", pairH.Gate(http.HandlerFunc(pairH.HandleStop)))
	s.mux.Handle("POST /api/uparivanje/dial", pairH.Gate(http.HandlerFunc(pairH.HandleDial)))
	s.mux.Handle("POST /api/uparivanje/confirm", pairH.Gate(http.HandlerFunc(pairH.HandleConfirm)))
	s.mux.Handle("GET /api/uparivanje/discover", pairH.Gate(http.HandlerFunc(pairH.HandleDiscover)))
	s.mux.Handle("POST /api/uparivanje/sync", pairH.Gate(http.HandlerFunc(pairH.HandleSync)))

	// Javne rute za prijavu
	s.mux.HandleFunc("GET /login", authH.ShowLogin)
	s.mux.HandleFunc("POST /login", authH.HandleLogin)
	s.mux.HandleFunc("POST /logout", authH.HandleLogout)

	// Real-time Server-Sent Events stream
	s.mux.HandleFunc("GET /api/events", sseH.ServeSSE)

	// API za dinamička područja
	s.mux.HandleFunc("GET /api/areas", usersH.HandleGetAreasAPI)

	// Zaštićene rute (zahtijevaju prijavu)
	s.mux.Handle("GET /{$}", s.authMiddleware(http.HandlerFunc(dashH.ShowDashboard)))
	s.mux.Handle("GET /dashboard", s.authMiddleware(http.HandlerFunc(dashH.ShowDashboard)))
	s.mux.Handle("GET /registri", s.authMiddleware(http.HandlerFunc(dashH.ShowRegisters)))

	// Administracija: ulazna stranica i sve što radi samo administrator
	adminH := NewAdminHandler(s.orgService, s.userService, s.peersService, s.templates["administracija.html"], s.templates["uvozi.html"])
	s.mux.Handle("GET /administracija", s.authMiddleware(http.HandlerFunc(adminH.ShowAdmin)))
	s.mux.Handle("GET /administracija/uvozi", s.authMiddleware(http.HandlerFunc(adminH.ShowImports)))

	// Organizacija obrane: sektori i branjena područja
	orgH := NewOrgHandler(s.orgService, s.templates["organizacija.html"], s.templates["sector_form.html"], s.templates["area_form.html"])
	s.mux.Handle("GET /organizacija", s.authMiddleware(http.HandlerFunc(orgH.ShowOrganization)))
	s.mux.Handle("GET /organizacija/sektori/new", s.authMiddleware(http.HandlerFunc(orgH.ShowSectorForm)))
	s.mux.Handle("GET /organizacija/sektori/{id}/edit", s.authMiddleware(http.HandlerFunc(orgH.ShowSectorForm)))
	s.mux.Handle("POST /organizacija/sektori/save", s.authMiddleware(http.HandlerFunc(orgH.HandleSaveSector)))
	s.mux.Handle("POST /organizacija/sektori/delete", s.authMiddleware(http.HandlerFunc(orgH.HandleDeleteSector)))
	s.mux.Handle("GET /organizacija/podrucja/new", s.authMiddleware(http.HandlerFunc(orgH.ShowAreaForm)))
	s.mux.Handle("GET /organizacija/podrucja/{id}/edit", s.authMiddleware(http.HandlerFunc(orgH.ShowAreaForm)))
	s.mux.Handle("POST /organizacija/podrucja/save", s.authMiddleware(http.HandlerFunc(orgH.HandleSaveArea)))
	s.mux.Handle("POST /organizacija/podrucja/delete", s.authMiddleware(http.HandlerFunc(orgH.HandleDeleteArea)))
	s.mux.Handle("POST /organizacija/nazivi", s.authMiddleware(http.HandlerFunc(orgH.HandleSaveTerms)))

	s.mux.Handle("GET /users", s.authMiddleware(http.HandlerFunc(usersH.ShowUsers)))
	s.mux.Handle("GET /users/new", s.authMiddleware(http.HandlerFunc(usersH.ShowUserForm)))
	s.mux.Handle("GET /users/{id}", s.authMiddleware(http.HandlerFunc(usersH.ShowUser)))
	s.mux.Handle("GET /users/{id}/edit", s.authMiddleware(http.HandlerFunc(usersH.ShowUserForm)))
	s.mux.Handle("GET /users/{id}/duties/new", s.authMiddleware(http.HandlerFunc(usersH.ShowDutyForm)))
	s.mux.Handle("GET /profile", s.authMiddleware(http.HandlerFunc(usersH.ShowProfile)))
	s.mux.Handle("POST /users/create", s.authMiddleware(http.HandlerFunc(usersH.HandleCreateUser)))
	s.mux.Handle("POST /users/update", s.authMiddleware(http.HandlerFunc(usersH.HandleUpdateUser)))
	s.mux.Handle("POST /users/duty/add", s.authMiddleware(http.HandlerFunc(usersH.HandleAddDuty)))
	s.mux.Handle("POST /users/duty/revoke", s.authMiddleware(http.HandlerFunc(usersH.HandleRevokeDuty)))
	s.mux.Handle("POST /users/delete", s.authMiddleware(http.HandlerFunc(usersH.HandleDeleteUser)))
	s.mux.Handle("POST /users/{id}/reset-password", s.authMiddleware(http.HandlerFunc(usersH.HandleResetPassword)))
	s.mux.Handle("POST /profile/change-password", s.authMiddleware(http.HandlerFunc(authH.HandleChangePassword)))
	s.mux.Handle("POST /profile/update", s.authMiddleware(http.HandlerFunc(usersH.HandleUpdateProfile)))

	// Pregled tuđim očima — administrator vidi program kao odabrani djelatnik
	s.mux.Handle("POST /view-as/stop", s.authMiddleware(http.HandlerFunc(authH.HandleStopViewAs)))
	s.mux.Handle("POST /view-as/{userID}", s.authMiddleware(http.HandlerFunc(authH.HandleViewAs)))

	// Štićene dionice i objekti
	s.mux.Handle("GET /sections", s.authMiddleware(http.HandlerFunc(sectionsH.ShowSections)))
	s.mux.Handle("GET /sections/new", s.authMiddleware(http.HandlerFunc(sectionsH.ShowSectionForm)))
	s.mux.Handle("GET /sections/{code}", s.authMiddleware(http.HandlerFunc(sectionsH.ShowSection)))
	s.mux.Handle("GET /sections/{code}/edit", s.authMiddleware(http.HandlerFunc(sectionsH.ShowSectionForm)))
	s.mux.Handle("GET /api/sections/{code}", s.authMiddleware(http.HandlerFunc(sectionsH.HandleGetSectionAPI)))
	s.mux.Handle("POST /sections/create", s.authMiddleware(http.HandlerFunc(sectionsH.HandleCreateSection)))
	s.mux.Handle("POST /sections/update", s.authMiddleware(http.HandlerFunc(sectionsH.HandleUpdateSection)))

	// Teritorijalne jedinice (županije, gradovi, općine, naselja)
	s.mux.Handle("GET /territories", s.authMiddleware(http.HandlerFunc(territoriesH.ShowTerritories)))
	s.mux.Handle("GET /territories/counties/new", s.authMiddleware(http.HandlerFunc(territoriesH.ShowCountyForm)))
	s.mux.Handle("GET /territories/counties/{id}/edit", s.authMiddleware(http.HandlerFunc(territoriesH.ShowCountyForm)))
	s.mux.Handle("GET /territories/municipalities/new", s.authMiddleware(http.HandlerFunc(territoriesH.ShowMunicipalityForm)))
	s.mux.Handle("GET /territories/municipalities/{id}", s.authMiddleware(http.HandlerFunc(territoriesH.ShowMunicipality)))
	s.mux.Handle("GET /territories/municipalities/{id}/edit", s.authMiddleware(http.HandlerFunc(territoriesH.ShowMunicipalityForm)))
	s.mux.Handle("GET /api/counties", s.authMiddleware(http.HandlerFunc(territoriesH.HandleGetCountiesAPI)))
	s.mux.Handle("GET /api/counties/{countyID}/municipalities", s.authMiddleware(http.HandlerFunc(territoriesH.HandleGetMunicipalitiesAPI)))
	s.mux.Handle("GET /api/municipalities/{municipalityID}/settlements", s.authMiddleware(http.HandlerFunc(territoriesH.HandleGetSettlementsAPI)))
	s.mux.Handle("GET /api/sections/{code}/territories", s.authMiddleware(http.HandlerFunc(territoriesH.HandleGetSectionTerritoriesAPI)))

	// Upravljanje jedinicama (CRUD)
	s.mux.Handle("POST /territories/county/create", s.authMiddleware(http.HandlerFunc(territoriesH.HandleCreateCounty)))
	s.mux.Handle("POST /territories/county/update", s.authMiddleware(http.HandlerFunc(territoriesH.HandleUpdateCounty)))
	s.mux.Handle("POST /territories/county/delete", s.authMiddleware(http.HandlerFunc(territoriesH.HandleDeleteCounty)))
	s.mux.Handle("POST /territories/municipality/create", s.authMiddleware(http.HandlerFunc(territoriesH.HandleCreateMunicipality)))
	s.mux.Handle("POST /territories/municipality/update", s.authMiddleware(http.HandlerFunc(territoriesH.HandleUpdateMunicipality)))
	s.mux.Handle("POST /territories/municipality/delete", s.authMiddleware(http.HandlerFunc(territoriesH.HandleDeleteMunicipality)))
	s.mux.Handle("POST /api/settlements/create", s.authMiddleware(http.HandlerFunc(territoriesH.HandleCreateSettlementAPI)))
	s.mux.Handle("POST /api/settlements/update", s.authMiddleware(http.HandlerFunc(territoriesH.HandleUpdateSettlementAPI)))
	s.mux.Handle("POST /api/settlements/delete", s.authMiddleware(http.HandlerFunc(territoriesH.HandleDeleteSettlementAPI)))

	// Registar vodomjernih postaja
	s.mux.Handle("GET /stations", s.authMiddleware(http.HandlerFunc(stationsH.ShowStations)))
	s.mux.Handle("GET /stations/new", s.authMiddleware(http.HandlerFunc(stationsH.ShowStationForm)))
	s.mux.Handle("GET /stations/{id}", s.authMiddleware(http.HandlerFunc(stationsH.ShowStation)))
	s.mux.Handle("GET /stations/{id}/edit", s.authMiddleware(http.HandlerFunc(stationsH.ShowStationForm)))
	s.mux.Handle("GET /api/stations", s.authMiddleware(http.HandlerFunc(stationsH.HandleListStationsAPI)))
	s.mux.Handle("POST /api/stations/create", s.authMiddleware(http.HandlerFunc(stationsH.HandleCreateStationAPI)))
	s.mux.Handle("POST /api/stations/update", s.authMiddleware(http.HandlerFunc(stationsH.HandleUpdateStationAPI)))
	s.mux.Handle("POST /api/stations/delete", s.authMiddleware(http.HandlerFunc(stationsH.HandleDeleteStationAPI)))

	// Registar vodnih tijela
	s.mux.Handle("GET /watercourses", s.authMiddleware(http.HandlerFunc(watercoursesH.ShowWatercourses)))
	s.mux.Handle("GET /structures", s.authMiddleware(http.HandlerFunc(structuresH.ShowStructures)))
	s.mux.Handle("GET /readings", s.authMiddleware(http.HandlerFunc(readingsH.ShowOverview)))
	s.mux.Handle("GET /readings/new", s.authMiddleware(http.HandlerFunc(readingsH.ShowForm)))
	s.mux.Handle("GET /readings/edit/{id}", s.authMiddleware(http.HandlerFunc(readingsH.ShowForm)))
	s.mux.Handle("GET /readings/station/{id}", s.authMiddleware(http.HandlerFunc(readingsH.ShowHistory)))
	s.mux.Handle("GET /readings/structure/{id}", s.authMiddleware(http.HandlerFunc(readingsH.ShowHistory)))
	s.mux.Handle("POST /readings/create", s.authMiddleware(http.HandlerFunc(readingsH.HandleCreate)))
	s.mux.Handle("POST /readings/update", s.authMiddleware(http.HandlerFunc(readingsH.HandleUpdate)))
	s.mux.Handle("POST /readings/delete", s.authMiddleware(http.HandlerFunc(readingsH.HandleDelete)))
	s.mux.Handle("POST /readings/follow", s.authMiddleware(http.HandlerFunc(readingsH.HandleFollow)))
	s.mux.Handle("GET /structures/new", s.authMiddleware(http.HandlerFunc(structuresH.ShowStructureForm)))
	s.mux.Handle("GET /structures/{id}", s.authMiddleware(http.HandlerFunc(structuresH.ShowStructure)))
	s.mux.Handle("GET /structures/{id}/edit", s.authMiddleware(http.HandlerFunc(structuresH.ShowStructureForm)))
	s.mux.Handle("POST /structures/create", s.authMiddleware(http.HandlerFunc(structuresH.HandleCreate)))
	s.mux.Handle("POST /structures/update", s.authMiddleware(http.HandlerFunc(structuresH.HandleUpdate)))
	s.mux.Handle("POST /structures/delete", s.authMiddleware(http.HandlerFunc(structuresH.HandleDelete)))
	s.mux.Handle("GET /watercourses/new", s.authMiddleware(http.HandlerFunc(watercoursesH.ShowWatercourseForm)))
	s.mux.Handle("GET /watercourses/{code}", s.authMiddleware(http.HandlerFunc(watercoursesH.ShowWatercourse)))
	s.mux.Handle("GET /watercourses/{code}/edit", s.authMiddleware(http.HandlerFunc(watercoursesH.ShowWatercourseForm)))
	s.mux.Handle("GET /api/watercourses", s.authMiddleware(http.HandlerFunc(watercoursesH.HandleListWatercoursesAPI)))
	s.mux.Handle("POST /api/watercourses/create", s.authMiddleware(http.HandlerFunc(watercoursesH.HandleCreateWatercourseAPI)))
	s.mux.Handle("POST /api/watercourses/update", s.authMiddleware(http.HandlerFunc(watercoursesH.HandleUpdateWatercourseAPI)))
	s.mux.Handle("POST /api/watercourses/delete", s.authMiddleware(http.HandlerFunc(watercoursesH.HandleDeleteWatercourseAPI)))
	s.mux.Handle("POST /api/stations/watercourse", s.authMiddleware(http.HandlerFunc(watercoursesH.HandleAssignStationWatercourseAPI)))

	// Što ovo računalo prati: pretplate na kanale, za svakog prijavljenog
	subsH := NewSubscriptionsHandler(s.peersService, s.orgService, s.templates["pretplate.html"])
	s.mux.Handle("GET /pretplate", s.authMiddleware(http.HandlerFunc(subsH.ShowSubscriptions)))
	s.mux.Handle("POST /pretplate/dodaj", s.authMiddleware(http.HandlerFunc(subsH.HandleAdd)))
	s.mux.Handle("POST /pretplate/ukloni", s.authMiddleware(http.HandlerFunc(subsH.HandleRemove)))
	s.mux.Handle("POST /pretplate/obrisi", s.authMiddleware(http.HandlerFunc(subsH.HandlePurge)))

	// Održavanje baze: brojke, sažimanje, VACUUM, izvoz i uvoz kanala
	dbH := NewDBMaintHandler(func() *sql.DB { return s.db }, s.recorder, s.peersService, func() string { return s.dbPath }, s.templates["baza.html"])
	s.mux.Handle("GET /administracija/baza", s.authMiddleware(http.HandlerFunc(dbH.ShowMaintenance)))
	s.mux.Handle("POST /administracija/baza/sazmi", s.authMiddleware(http.HandlerFunc(dbH.HandleCompact)))
	s.mux.Handle("POST /administracija/baza/vacuum", s.authMiddleware(http.HandlerFunc(dbH.HandleVacuum)))
	s.mux.Handle("GET /administracija/baza/izvoz", s.authMiddleware(http.HandlerFunc(dbH.HandleExport)))
	s.mux.Handle("POST /administracija/baza/uvoz", s.authMiddleware(http.HandlerFunc(dbH.HandleImport)))

	// Nadzorna ploča sinkronizacije
	syncH := NewSyncHandler(s.peersService, s.templates["sinkronizacija.html"])
	s.mux.Handle("GET /sinkronizacija", s.authMiddleware(http.HandlerFunc(syncH.ShowDashboard)))
	s.mux.Handle("GET /api/sync/status", s.authMiddleware(http.HandlerFunc(syncH.HandleStatus)))
	s.mux.Handle("POST /api/sync/all", s.authMiddleware(http.HandlerFunc(syncH.HandleSyncAll)))

	// Postavke: čvor, uparivanje, pronalaženje, sinkronizacija, povijest
	s.mux.Handle("GET /settings", s.authMiddleware(http.HandlerFunc(settingsH.ShowSettings)))
	s.mux.Handle("GET /api/peers/pair/status", s.authMiddleware(http.HandlerFunc(settingsH.HandlePairStatus)))
	s.mux.Handle("POST /api/peers/pair/listen", s.authMiddleware(http.HandlerFunc(settingsH.HandlePairListen)))
	s.mux.Handle("POST /api/peers/pair/stop", s.authMiddleware(http.HandlerFunc(settingsH.HandlePairStop)))
	s.mux.Handle("POST /api/peers/pair/dial", s.authMiddleware(http.HandlerFunc(settingsH.HandlePairDial)))
	s.mux.Handle("POST /api/peers/pair/confirm", s.authMiddleware(http.HandlerFunc(settingsH.HandlePairConfirm)))
	s.mux.Handle("GET /api/peers/discover", s.authMiddleware(http.HandlerFunc(settingsH.HandleDiscover)))
	s.mux.Handle("POST /api/peers/{node}/sync", s.authMiddleware(http.HandlerFunc(settingsH.HandleSyncNow)))
	s.mux.Handle("POST /api/peers/{node}/forget", s.authMiddleware(http.HandlerFunc(settingsH.HandleForgetPeer)))
	s.mux.Handle("POST /api/peers/{node}/bootstrap", s.authMiddleware(http.HandlerFunc(settingsH.HandleSetBootstrap)))
	s.mux.Handle("POST /api/peers/{node}/adrese", s.authMiddleware(http.HandlerFunc(settingsH.HandlePublicAddress)))
	s.mux.Handle("GET /api/history/{entity}/{id}", s.authMiddleware(http.HandlerFunc(settingsH.HandleHistory)))
	s.mux.Handle("POST /api/network/create", s.authMiddleware(http.HandlerFunc(settingsH.HandleCreateNetwork)))
	s.mux.Handle("GET /api/network/members", s.authMiddleware(http.HandlerFunc(settingsH.HandleMembers)))
	s.mux.Handle("POST /api/network/members/{node}/revoke", s.authMiddleware(http.HandlerFunc(settingsH.HandleRevokeMember)))

	// Mjerodavni vodomjeri dionice
	s.mux.Handle("GET /api/sections/{code}/stations", s.authMiddleware(http.HandlerFunc(stationsH.HandleGetSectionStationsAPI)))
}

// authMiddleware provjerava sesijski kolačić i postavlja korisnika u context
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gocop_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		sessionID, err := uuid.Parse(cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		view, err := s.authService.AuthenticateSessionView(sessionID)
		if err != nil || view.User == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Zadana lozinka piše u dokumentaciji, pa račun s njom nije račun
		// nego otvorena vrata. Dok je osoba ne promijeni, otvara se samo
		// stranica profila; provjera je ovdje da je nijedna stranica ne zaobiđe.
		if view.RealUser.MustChangePassword && !passwordChangeAllowed(r.URL.Path) {
			http.Redirect(w, r, "/profile?force=1#lozinka", http.StatusSeeOther)
			return
		}

		// Tuđim se očima samo gleda. Zapis pod tuđim imenom ne smije nastati
		// ni omaškom, pa se zaustavlja ovdje, prije svakog rukovatelja.
		if view.Viewing && !readOnlyRequest(r) {
			http.Error(w, "Dok gledaš tuđim očima program samo čita. Vrati se sebi pa ponovi.", http.StatusForbidden)
			return
		}

		// Moduli: što račun vidi. Skriveni modul ne otvara se ni izravnom
		// adresom; pri pregledu tuđim očima vrijedi tuđi skup.
		visible, err := s.moduleService.Visibility(r.Context(), view.User, view.Perms)
		if err != nil {
			http.Error(w, "Greška pri čitanju vidljivosti modula: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if mod := moduleForPath(r.URL.Path); mod != "" && !visible.Sees(mod) {
			http.Error(w, "Modul „"+models.ModuleLabel(mod)+"“ nije uključen za vaš račun. Ako vam treba, javite se administratoru.", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, view.User)
		ctx = context.WithValue(ctx, contextKeyModules, visible)
		ctx = context.WithValue(ctx, contextKeyPerms, view.Perms)
		ctx = context.WithValue(ctx, contextKeyRealUsr, view.RealUser)
		ctx = context.WithValue(ctx, contextKeyViewing, view.Viewing)
		ctx = context.WithValue(ctx, contextKeySession, sessionID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// modulePaths preslikava početak putanje na modul; što nije navedeno
// (početna, profil, pregled tuđim očima, statika) otvoreno je svima
var modulePaths = []struct{ prefix, module string }{
	{"/teren", models.ModuleField},
	{"/readings", models.ModuleReadings},
	{"/registri", models.ModuleRegisters},
	{"/sections", models.ModuleRegisters}, {"/stations", models.ModuleRegisters},
	{"/structures", models.ModuleRegisters}, {"/watercourses", models.ModuleRegisters},
	{"/territories", models.ModuleRegisters}, {"/odrzavanje", models.ModuleRegisters},
	{"/dnevnici", models.ModuleJournals},
	{"/api/sections", models.ModuleRegisters}, {"/api/stations", models.ModuleRegisters},
	{"/api/watercourses", models.ModuleRegisters}, {"/api/settlements", models.ModuleRegisters},
	{"/api/counties", models.ModuleRegisters}, {"/api/municipalities", models.ModuleRegisters},
	{"/api/areas", models.ModuleRegisters},
	{"/users", models.ModuleUsers},
	{"/administracija", models.ModuleAdmin}, {"/organizacija", models.ModuleRegisters},
	{"/sinkronizacija", models.ModuleAdmin}, {"/api/sync", models.ModuleAdmin},
	{"/moduli", models.ModuleAdmin}, {"/settings", models.ModuleAdmin},
	{"/api/peers", models.ModuleAdmin}, {"/api/network", models.ModuleAdmin},
	{"/api/history", models.ModuleAdmin},
}

func moduleForPath(path string) string {
	for _, mp := range modulePaths {
		if path == mp.prefix || strings.HasPrefix(path, mp.prefix+"/") {
			return mp.module
		}
	}
	return ""
}

// readOnlyRequest javlja smije li zahtjev proći dok se gleda tuđim očima:
// samo čitanje, i izlaz iz pregleda natrag k sebi.
func readOnlyRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/view-as")
}

func (s *Server) Start() error {
	return http.ListenAndServe(s.addr, s.mux)
}

// SetAddr mijenja adresu prije (ponovnog) pokretanja — za pad s porta 80 na 8080
// SetDatabase daje poslužitelju bazu i njezinu putanju, za održavanje
func (s *Server) SetDatabase(db *sql.DB, path string) {
	s.db, s.dbPath = db, path
}

func (s *Server) SetAddr(addr string) {
	s.addr = addr
}
