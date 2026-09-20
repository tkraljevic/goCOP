package web

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
)

// Održavanje baze: koliko je velika i od čega, sažimanje knjige verzija,
// VACUUM, te izvoz i uvoz kanala u datoteku. Sve radi samo administrator,
// jer sažimanje i uvoz mijenjaju knjigu ovog čvora.

// Baza i putanja stižu poslužitelju tek nakon što su rute složene, pa ih
// rukovatelj čita kroz zatvarače, ne pri stvaranju
type DBMaintHandler struct {
	db     func() *sql.DB
	rec    *ledger.Recorder
	peers  *peers.Service
	dbPath func() string
	tmpl   *template.Template
	opcije func(ctx context.Context) models.Opcije // nil = sve isključeno
}

// SetOpcije daje rukovatelju uvid u opće prekidače
func (h *DBMaintHandler) SetOpcije(f func(ctx context.Context) models.Opcije) { h.opcije = f }

func (h *DBMaintHandler) brisanjePovijesti(ctx context.Context) bool {
	return h.opcije != nil && h.opcije(ctx).BrisanjePovijestiVerzija
}

// HandlePurgeTombstones briše spomenike obrisanih zapisa, uz uključen prekidač
func (h *DBMaintHandler) HandlePurgeTombstones(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if !h.brisanjePovijesti(r.Context()) {
		redirectWith(w, r, "/administracija/baza", "error", "Brisanje povijesti verzija je isključeno (Administracija › Opcije)")
		return
	}
	n, err := h.rec.PurgeArchived(r.Context())
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	redirectWith(w, r, "/administracija/baza", "success", fmt.Sprintf("Obrisano %d spomenika obrisanih zapisa.", n))
}

func NewDBMaintHandler(db func() *sql.DB, rec *ledger.Recorder, peersSvc *peers.Service, dbPath func() string, tmpl *template.Template) *DBMaintHandler {
	return &DBMaintHandler{db: db, rec: rec, peers: peersSvc, dbPath: dbPath, tmpl: tmpl}
}

type DBMaintPageData struct {
	CurrentUser *models.User
	Permissions *models.UserPermissions

	DBPath            string
	SizeBytes         int64
	WALBytes          int64
	Stats             ledger.Stats
	Channels          []peers.ChannelStat
	Compactable       int // verzija koje bi sažimanje maknulo uz zadani rok
	KeepDays          int
	Spomenika         int  // arhiviranih verzija (spomenika)
	BrisanjePovijesti bool // prekidač uključen
	Readings          int
	Journals          int
	Sessions          int
	Import            *peers.ImportReport

	SuccessMessage string
	ErrorMessage   string
	ActiveNav      string
	ViewAsBanner
}

// DefaultKeepDays je koliko dana povijesti izmjena sažimanje zadano ostavlja
const DefaultKeepDays = 90

func (h *DBMaintHandler) pageData(r *http.Request) DBMaintPageData {
	ctx := r.Context()
	currUser, _ := ctx.Value(contextKeyUser).(*models.User)
	perms, _ := ctx.Value(contextKeyPerms).(*models.UserPermissions)
	return DBMaintPageData{
		CurrentUser: currUser, Permissions: perms, KeepDays: DefaultKeepDays,
		SuccessMessage: r.URL.Query().Get("success"), ErrorMessage: r.URL.Query().Get("error"),
		ActiveNav: "admin", ViewAsBanner: viewBanner(r),
	}
}

// ShowMaintenance prikazuje stanje baze i radnje
func (h *DBMaintHandler) ShowMaintenance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.pageData(r)
	data.BrisanjePovijesti = h.brisanjePovijesti(ctx)
	if data.BrisanjePovijesti {
		_ = h.db().QueryRowContext(ctx, `SELECT COUNT(*) FROM record_versions WHERE archived = 1`).Scan(&data.Spomenika)
	}
	if days, _ := strconv.Atoi(r.URL.Query().Get("days")); days > 0 || (days == 0 && r.URL.Query().Get("days") == "0" && data.BrisanjePovijesti) {
		data.KeepDays = days
	}
	data.DBPath = h.dbPath()
	if fi, err := os.Stat(data.DBPath); err == nil {
		data.SizeBytes = fi.Size()
	}
	if fi, err := os.Stat(data.DBPath + "-wal"); err == nil {
		data.WALBytes = fi.Size()
	}
	if st, err := h.rec.Stats(ctx); err == nil {
		data.Stats = st
	} else {
		data.ErrorMessage = err.Error()
	}
	data.Channels, _ = h.peers.Channels(ctx)
	data.Compactable, _ = h.rec.Compactable(ctx, time.Now().AddDate(0, 0, -data.KeepDays))
	if db := h.db(); db != nil {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM readings`).Scan(&data.Readings)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journals`).Scan(&data.Journals)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&data.Sessions)
	}

	if err := h.tmpl.ExecuteTemplate(w, "baza.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleCompact sažima knjigu: ostaju zadnje verzije i spomenici
func (h *DBMaintHandler) HandleCompact(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/administracija/baza", "error", "Neispravan zahtjev")
		return
	}
	days, _ := strconv.Atoi(r.FormValue("days"))
	if days < 1 && !(r.FormValue("days") == "0" && h.brisanjePovijesti(r.Context())) {
		days = DefaultKeepDays
	}
	n, err := h.rec.Compact(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	redirectWith(w, r, "/administracija/baza", "success", fmt.Sprintf("Knjiga je sažeta: obrisano %d starijih verzija, zadnje verzije i spomenici ostaju.", n))
}

// HandleReplay obnavlja površinu iz zadnjih verzija u knjizi
func (h *DBMaintHandler) HandleReplay(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	n, err := repository.ReplaySurface(r.Context(), h.db(), h.rec)
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", fmt.Sprintf("Površina obnovljena iz %d zapisa, uz preskočene: %v", n, err))
		return
	}
	redirectWith(w, r, "/administracija/baza", "success", fmt.Sprintf("Površina je obnovljena iz knjige: %d zapisa.", n))
}

// HandleVacuum vraća prostor obrisanih redaka operacijskom sustavu
func (h *DBMaintHandler) HandleVacuum(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	before := int64(0)
	if fi, err := os.Stat(h.dbPath()); err == nil {
		before = fi.Size()
	}
	db := h.db()
	if db == nil {
		redirectWith(w, r, "/administracija/baza", "error", "Baza nije dostupna")
		return
	}
	if _, err := db.ExecContext(r.Context(), `VACUUM`); err != nil {
		redirectWith(w, r, "/administracija/baza", "error", "VACUUM nije uspio: "+err.Error())
		return
	}
	after := int64(0)
	if fi, err := os.Stat(h.dbPath()); err == nil {
		after = fi.Size()
	}
	redirectWith(w, r, "/administracija/baza", "success", fmt.Sprintf("Baza je sažeta na disku: %s → %s.", humanBytes(before), humanBytes(after)))
}

// HandleExport šalje kanale kao potpisano .cop izdanje na preuzimanje; uz
// oblik=db daje staru SQLite arhivu bez potpisa i sadržaja
func (h *DBMaintHandler) HandleExport(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	area, _ := strconv.Atoi(r.URL.Query().Get("area"))
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if area == 0 && year == 0 {
		redirectWith(w, r, "/administracija/baza", "error", "Za izvoz odaberite bar područje ili godinu")
		return
	}
	channels, err := h.peers.ChannelsFor(r.Context(), kind, area, year)
	if err != nil || len(channels) == 0 {
		redirectWith(w, r, "/administracija/baza", "error", "Nema nijednog kanala za taj odabir na ovom čvoru")
		return
	}
	if r.URL.Query().Get("oblik") != "db" {
		obuhvat := peers.CopObuhvat{Vrsta: kind, AreaID: area, Od: year, Do: year}
		var b bytes.Buffer
		m, err := h.peers.IzveziCop(r.Context(), obuhvat, channels, r.URL.Query().Get("sadrzaj") != "0", &b)
		if err != nil {
			redirectWith(w, r, "/administracija/baza", "error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+peers.CopIme(m)+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b.Bytes())
		return
	}
	name := peers.ArchiveFileName(kind, area, year)
	tmp := h.peers.TempArchivePath(name)
	defer os.Remove(tmp)
	if _, err := h.peers.ExportFile(r.Context(), tmp, channels); err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	f, err := os.Open(tmp)
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = io.Copy(w, f)
}

// HandleImport prima arhivu i primjenjuje je kao razmjenu
func (h *DBMaintHandler) HandleImport(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		redirectWith(w, r, "/administracija/baza", "error", "Datoteka nije primljena: "+err.Error())
		return
	}
	file, header, err := r.FormFile("archive")
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", "Odaberite datoteku arhive")
		return
	}
	defer file.Close()
	tmp := h.peers.TempArchivePath(header.Filename)
	defer os.Remove(tmp)
	out, err := os.Create(tmp)
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	out.Close()
	if jeCop(tmp) {
		rep, err := h.peers.UveziCop(r.Context(), tmp)
		if err != nil {
			redirectWith(w, r, "/administracija/baza", "error", err.Error())
			return
		}
		poruka := fmt.Sprintf("Ugrađeno izdanje %d (%s) čvora %s: %d zapisa u paketu, %d novih; sadržaja upisano %d, za dohvat %d.",
			rep.Manifest.Izdanje, rep.Manifest.Obuhvat.Kljuc(), rep.Manifest.Izdao, rep.Verzija, rep.Novih, rep.SadrzajaUpisano, rep.SadrzajaZeljeno)
		if rep.Napomena != "" {
			poruka += " " + rep.Napomena + "."
		}
		redirectWith(w, r, "/administracija/baza", "success", poruka)
		return
	}
	rep, err := h.peers.ImportFile(r.Context(), tmp)
	if err != nil {
		redirectWith(w, r, "/administracija/baza", "error", err.Error())
		return
	}
	var parts []string
	for ch, n := range rep.Channels {
		parts = append(parts, fmt.Sprintf("%s: %d", ch, n))
	}
	redirectWith(w, r, "/administracija/baza", "success",
		fmt.Sprintf("Uvezena arhiva čvora %s: %d verzija u datoteci, %d novih (%s).", rep.From, rep.Versions, rep.Applied, strings.Join(parts, "; ")))
}

// jeCop prepoznaje .cop paket po ZIP zaglavlju; stara arhiva je SQLite
func jeCop(put string) bool {
	f, err := os.Open(put)
	if err != nil {
		return false
	}
	defer f.Close()
	var glava [4]byte
	if _, err := io.ReadFull(f, glava[:]); err != nil {
		return false
	}
	return glava[0] == 'P' && glava[1] == 'K'
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
