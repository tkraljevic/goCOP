package web

import (
	"context"
	"html/template"
	"net/http"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/service"
)

// Administracija je ulazna stranica za posao koji radi samo administrator:
// ustroj obrane (sektori i branjena područja), ovlasti i vidljivost modula,
// čvor i mreža, te uvozi podataka. Sve to postoji i na svojim stranicama;
// ovdje stoji na jednom mjestu, da administrator ne traži po izborniku.

type AdminHandler struct {
	org        *service.OrgService
	users      *service.UserService
	peers      *peers.Service
	tmplAdmin  *template.Template
	tmplImport *template.Template
	// drugiKorak daje drugi korak prijave izvana, za upozorenje kad PIN
	// izvana nema tko slati; nil dok se ne spoji
	drugiKorak func() *service.DrugiKorak
	izvori     izvoriStanja // opcije, telemetrija i e-pošta za raspored i Prve korake
}

// SetDrugiKorak spaja ulaznu stranicu s drugim korakom prijave izvana
func (h *AdminHandler) SetDrugiKorak(f func() *service.DrugiKorak) { h.drugiKorak = f }

func NewAdminHandler(org *service.OrgService, users *service.UserService, peersSvc *peers.Service, admin, imports *template.Template) *AdminHandler {
	return &AdminHandler{org: org, users: users, peers: peersSvc, tmplAdmin: admin, tmplImport: imports}
}

// AdminPageData je ulazna stranica administracije sa stanjem ustroja
type AdminPageData struct {
	CSV         []CSVRegister // registri s CSV izvozom i uvozom, za stranicu Uvozi
	CurrentUser *models.User
	Permissions *models.UserPermissions

	Sectors   int
	Areas     int
	Users     int
	Locked    int // računi koji čekaju promjenu lozinke
	Inactive  int
	NoContact bool // administrator nema kontakt, pa ga stranica prijave ne pokazuje
	// PINBezPosiljatelja: PIN izvana je uključen, a ovaj čvor nema
	// ispravnog računa za slanje PIN-a, pa se izvana ulazi samo kodovima
	PINBezPosiljatelja bool

	SyncOnline, SyncTotal, SyncAlerts int // sažetak razmjene za karticu Stanje razmjene

	Djelatnika int // računi bez zastavice globalnog administratora

	// Raspored (administracija_raspored.go): ključ kartice ili skupine → vidi
	// li je ovaj korisnik, i gdje vrijedi ono što se na kartici mijenja
	Vidi    map[string]bool
	Skupina map[string]bool
	Doseg   map[string]DosegKartice
	// PrviKoraci: samo administratoru, dok ustroj nije gotov
	PrviKoraci []KorakUstroja
	// TestnihUkljuceno: koliko je prekidača na stranici Opcije uključeno
	TestnihUkljuceno int

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

func (h *AdminHandler) pageData(r *http.Request) AdminPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return AdminPageData{
		CurrentUser: currUser, Permissions: perms,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
	}
}

// ShowAdmin prikazuje ulaznu stranicu administracije
func (h *AdminHandler) ShowAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	h.dodajUstroj(ctx, &data)
	if _, _, _, ok := h.users.GlobalAdminContact(); !ok {
		data.NoContact = true
	}
	if h.drugiKorak != nil {
		data.PINBezPosiljatelja = h.drugiKorak().BezPosiljatelja(ctx)
	}
	if st, err := h.peers.Status(ctx, false); err == nil {
		data.SyncOnline, data.SyncTotal, data.SyncAlerts = st.Online, st.Total, len(st.Alerts)
	}
	h.dodajRaspored(ctx, &data)

	if err := h.tmplAdmin.ExecuteTemplate(w, "administracija.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// dodajUstroj broji sektore, područja i račune
func (h *AdminHandler) dodajUstroj(ctx context.Context, data *AdminPageData) {
	if sectors, err := h.org.ListSectors(ctx); err == nil {
		data.Sectors = len(sectors)
	}
	if areas, err := h.org.ListAreas(ctx, ""); err == nil {
		data.Areas = len(areas)
	}
	users, err := h.users.ListUsers("", 0, "", "", "")
	if err != nil {
		return
	}
	data.Users = len(users)
	for _, u := range users {
		if u.MustChangePassword {
			data.Locked++
		}
		if !u.IsActive {
			data.Inactive++
		}
		if !u.IsGlobalAdmin {
			data.Djelatnika++
		}
	}
}

// dodajRaspored slaže Prve korake (samo administratoru) i raspored kartica
func (h *AdminHandler) dodajRaspored(ctx context.Context, data *AdminPageData) {
	moduli, _ := ctx.Value(contextKeyModules).(models.Visibility)
	p := pravaKartice{perms: data.Permissions, moduli: moduli}
	if p.admin() {
		if k := prviKoraci(h.stanjeUstroja(ctx, data)); !ustrojGotov(k) {
			data.PrviKoraci = k
		}
		if h.izvori.opcije != nil {
			data.TestnihUkljuceno = ukljucenihTestnih(h.izvori.opcije(ctx))
		}
	}
	data.Vidi, data.Skupina, data.Doseg = rasporedKartica(p, data.PrviKoraci != nil)
}

// stanjeUstroja čita ono po čemu se vidi koji su Prvi koraci gotovi
func (h *AdminHandler) stanjeUstroja(ctx context.Context, data *AdminPageData) stanjeUstroja {
	s := stanjeUstroja{sektori: data.Sectors > 0, djelatnici: data.Djelatnika > 0, uparen: data.SyncTotal > 0,
		telemetrija: izvorStanja(ctx, h.izvori.telemetrija), posta: izvorStanja(ctx, h.izvori.posta)}
	if t, err := h.org.Terms(ctx); err == nil { // naziva se ne da pročitati: stavka ostaje bez kvačice
		s.nazivi = !t.UpdatedAt.IsZero()
	}
	return s
}

// ShowImports prikazuje što se i odakle uvozi u ovaj čvor
// CSVRegister je jedan registar koji ide u CSV i natrag. Popis stoji na jednom
// mjestu da se izvoz i uvoz ne traže po registrima; oblik datoteke je isti u
// oba smjera.
type CSVRegister struct {
	Name   string
	Note   string
	Export string
	Import string // prazno kad uvoza (još) nema
	Kind   string // vrijednost polja "kind" za uvoze koji jednim putem primaju više registara
}

// csvRegisters su registri s CSV izvozom; uvoz postoji za sve osim gdje piše.
func csvRegisters() []CSVRegister {
	t := models.Terms()
	return []CSVRegister{
		{Name: "Županije", Export: "/territories/zupanije.csv", Import: "/territories/uvoz"},
		{Name: "Gradovi i općine", Export: "/territories/gradovi-i-opcine.csv", Import: "/territories/uvoz", Kind: "opcine"},
		{Name: "Naselja", Export: "/territories/naselja.csv", Import: "/territories/uvoz", Kind: "naselja"},
		{Name: t.Sectors, Export: "/organizacija/sektori.csv", Import: "/organizacija/uvoz"},
		{Name: t.Areas, Export: "/organizacija/podrucja.csv", Import: "/organizacija/uvoz", Kind: "podrucja"},
		{Name: "Licencirane firme", Note: "firma se prepoznaje po OIB-u, bez njega po nazivu", Export: "/firme.csv", Import: "/firme/uvoz"},
	}
}

func (h *AdminHandler) ShowImports(w http.ResponseWriter, r *http.Request) {
	data := h.pageData(r)
	data.CSV = csvRegisters()
	if err := h.tmplImport.ExecuteTemplate(w, "uvozi.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
