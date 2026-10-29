package web

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"gocop/internal/models"
	"gocop/internal/peers"
)

// Karta bez interneta u Administraciji › Karte: učitava se izvezena PMTiles
// karta ili paket karte (.cop), čvor je drži u mapi karte/ uz bazu i
// poslužuje je pregledniku (/karta/vektor/<otisak>.pmtiles), a odande se
// preuzima potpisani paket za druga računala.

// mapaKarata je mapa karata ovog čvora (uz bazu); prazna dok baza nije
// postavljena
var mapaKarata atomic.Pointer[string]

func postaviMapuKarata(dbPath string) {
	m := filepath.Join(filepath.Dir(dbPath), "karte")
	mapaKarata.Store(&m)
}

func mapaKarataCvora() string {
	if m := mapaKarata.Load(); m != nil {
		return *m
	}
	return ""
}

// adresaVektorskeKarte je adresa zadane karte, ili prazno kad je ovaj čvor
// nema na disku
func adresaVektorskeKarte(k *models.VektorskaKarta) string {
	if k == nil || !kartaOvdje(k) {
		return ""
	}
	return "/karta/vektor/" + k.Otisak + ".pmtiles"
}

func kartaOvdje(k *models.VektorskaKarta) bool {
	mapa := mapaKarataCvora()
	if k == nil || mapa == "" {
		return false
	}
	fi, err := os.Stat(peers.PutKarte(mapa, k.Otisak))
	return err == nil && fi.Size() == k.Bajtova
}

// imaVektorskuKartu javlja treba li stranici biblioteka za vektorsku kartu
func imaVektorskuKartu() bool {
	return adresaVektorskeKarte(models.PostavkeKartePrograma().Vektorska) != ""
}

// SetPeers daje rukovatelju razmjenu: njome se potpisuju i provjeravaju
// paketi karte
func (h *AktiHandler) SetPeers(p *peers.Service) { h.peers = p }

// ServeVektorskaKarta poslužuje zadanu kartu, s dijelovima (Range) koje
// preglednik traži za svaku pločicu
func ServeVektorskaKarta(w http.ResponseWriter, r *http.Request) {
	k := models.PostavkeKartePrograma().Vektorska
	if k == nil || r.PathValue("ime") != k.Otisak+".pmtiles" || !kartaOvdje(k) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // adresa nosi otisak
	http.ServeFile(w, r, peers.PutKarte(mapaKarataCvora(), k.Otisak))
}

// HandleUcitajKartu prima PMTiles kartu ili paket karte (.cop), sprema je
// na disk i postavlja kao zadanu kartu bez interneta na svim čvorovima.
// Tijelo se čita kako stiže, bez cijele karte u memoriji.
func (h *AktiHandler) HandleUcitajKartu(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	k, err := h.ucitajKartu(r)
	if err == nil {
		err = s.SpremiKartuBezInterneta(r.Context(), perms, &k)
	}
	if err != nil {
		redirectWith(w, r, "/administracija/karte#karta-bez-interneta", "error", "Karta nije učitana: "+err.Error())
		return
	}
	redirectWith(w, r, "/administracija/karte#karta-bez-interneta", "success",
		"Karta „"+k.Naziv+"” je učitana i zadana je karta bez interneta na svim čvorovima. Računala koja je nemaju trebaju njezin .cop paket.")
}

func (h *AktiHandler) ucitajKartu(r *http.Request) (models.VektorskaKarta, error) {
	mapa := mapaKarataCvora()
	if mapa == "" || h.peers == nil {
		return models.VektorskaKarta{}, errors.New("ovaj čvor nema mjesto za karte")
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return models.VektorskaKarta{}, errors.New("datoteka karte nije stigla")
	}
	polja := map[string]string{}
	for {
		dio, err := mr.NextPart()
		if err != nil {
			return models.VektorskaKarta{}, errors.New("izaberite kartu (.pmtiles ili .cop)")
		}
		if dio.FormName() != "datoteka" {
			b, _ := io.ReadAll(io.LimitReader(dio, 1<<10)) // kratko polje obrasca; prekinuto čitanje daje prazno
			polja[dio.FormName()] = string(b)
			continue
		}
		return h.kartaIzDatoteke(r, bufio.NewReaderSize(dio, 64<<10), mapa, polja)
	}
}

// kartaIzDatoteke: paket karte (ZIP, „PK”) se provjeri i raspakira, a
// PMTiles se sprema kako jest, uz naziv i izvor iz obrasca
func (h *AktiHandler) kartaIzDatoteke(r *http.Request, br *bufio.Reader, mapa string, polja map[string]string) (models.VektorskaKarta, error) {
	pocetak, _ := br.Peek(2) // kraća datoteka nije ni paket ni karta, pa je odbije SpremiKartu
	if string(pocetak) != "PK" {
		return peers.SpremiKartu(br, mapa, polja["naziv"], polja["izvor"])
	}
	tmp, err := peers.PrivremenaDatoteka(br, mapa, "paket-*.cop", peers.NajvecaKarta+1<<20)
	defer os.Remove(tmp) //nolint:errcheck // paket je samo prijenosnik; prazna putanja ne briše ništa
	if err != nil {
		return models.VektorskaKarta{}, err
	}
	return h.peers.UveziKartu(r.Context(), tmp, mapa)
}

// HandleIzveziKartu daje potpisani paket zadane karte za druga računala
func (h *AktiHandler) HandleIzveziKartu(w http.ResponseWriter, r *http.Request) {
	k := models.PostavkeKartePrograma().Vektorska
	if k == nil || !kartaOvdje(k) || h.peers == nil {
		redirectWith(w, r, "/administracija/karte#karta-bez-interneta", "error", "Ovaj čvor nema zadanu kartu, pa nema ni paketa.")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+peers.ImePaketaKarte(*k)+`"`)
	if _, err := h.peers.IzveziKartu(*k, peers.PutKarte(mapaKarataCvora(), k.Otisak), w); err != nil {
		// zaglavlje je već poslano: preglednik dobije krnju datoteku, a
		// uvoz je odbije po otisku
		return
	}
}

// HandleMakniKartu miče zadanu kartu bez interneta sa svih čvorova (opis) i
// s diska ovog čvora
func (h *AktiHandler) HandleMakniKartu(w http.ResponseWriter, r *http.Request) {
	_, perms, _ := h.base(r)
	s := h.svc(w)
	if s == nil {
		return
	}
	stara := s.KartaBezInterneta(r.Context())
	if err := s.SpremiKartuBezInterneta(r.Context(), perms, nil); err != nil {
		redirectWith(w, r, "/administracija/karte#karta-bez-interneta", "error", err.Error())
		return
	}
	if stara != nil && mapaKarataCvora() != "" && strings.TrimSpace(stara.Otisak) != "" {
		_ = os.Remove(peers.PutKarte(mapaKarataCvora(), stara.Otisak)) // karte možda ni nije bilo na ovom računalu
	}
	redirectWith(w, r, "/administracija/karte#karta-bez-interneta", "success", "Karta bez interneta je maknuta.")
}
