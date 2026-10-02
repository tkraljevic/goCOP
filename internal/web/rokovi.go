package web

// Rokovi i veličine po ruti. Poslužitelj nema zajedničkih ReadTimeout ni
// WriteTimeout: tok događaja i tunel razmjene traju satima, izvoz i uvoz
// minutama, a obična stranica sekundama. Zato ih ovaj sloj postavlja po
// ruti, na vezi (http.ResponseController), a tijelo zahtjeva ograđuje s
// http.MaxBytesReader.
//
// Rok čitanja je ujedno rok cijelog rukovatelja: kad je tijelo pročitano,
// net/http čita s veze u pozadini (da primijeti prekid), a istek roka tamo
// otkaže kontekst zahtjeva. Zato dugi poslovi dobiju dug rok i čitanja, a
// tok događaja nema nijedan.
//
// Na javnom čvoru Cloudflare ionako prije prekine odgovor koji kasni 100 s
// i odbije tijelo veće od 100 MB; ovi rokovi čuvaju prije svega laptop i
// izravan pristup u lokalnoj mreži.

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// rokovi su varijable samo zato da ih test skrati
var (
	rokCitanja    = 60 * time.Second // zadano: tijelo i rukovatelj
	rokPisanja    = 5 * time.Minute  // zadano: odgovor
	rokDugogPosla = 30 * time.Minute // uvoz, izvoz, održavanje baze, razmjena
)

const (
	najveceTijelo    = 2 << 20  // zadano tijelo zahtjeva
	tijeloGeometrije = 24 << 20 // GeoJSON vodotoka i slivova: obrazac dopušta datoteku do 20 MB, a JSON je uz navodnike veći (najveći danas ~0,7 MB)
)

// pravilo je ono što ruta smije: dug posao, veće tijelo, ili tok bez rokova
type pravilo struct {
	dugo      bool  // rok čitanja i pisanja rokDugogPosla
	tijelo    int64 // najveće tijelo; 0 = najveceTijelo
	tok       bool  // bez rokova (tok događaja); tijelo zadano
	netaknuto bool  // ni rokova ni ograde tijela (tunel razmjene: veza se preuzima)
}

// pravilaRuta su iznimke od zadanog, po uzorku rute točno kako je
// registriran u server.go (s.mux.Handle). Test rokovi_test.go traži da
// svaka ruta koja prima datoteku i svaki izvoz datoteke ovdje imaju svoj
// red. Granice tijela su granica datoteke iz rukovatelja i 1 MiB za ostala
// polja obrasca.
var pravilaRuta = map[string]pravilo{
	// tok događaja i tunel razmjene
	"GET /api/events":           {tok: true},
	"GET " + razmjena.PutTunela: {netaknuto: true},

	// primanje datoteka
	"POST /administracija/baza/uvoz":              {dugo: true, tijelo: 2<<30 + 1<<20}, // .cop do 2 GiB (peers.najveciCop) ili cijela baza
	"POST /stations/{id}/paket/pregled":           {dugo: true, tijelo: najveciPaket + 1<<20},
	"POST /administracija/uvoz-niza/pregled":      {dugo: true, tijelo: najveciUvoz + 1<<20},
	"POST /administracija/uvoz-izvora/pregled":    {dugo: true, tijelo: najveciUvozIzvora + 1<<20},
	"POST /administracija/uvoz-krivulja/pregled":  {dugo: true, tijelo: 8<<20 + 1<<20},
	"POST /administracija/uvoz-profila/pregled":   {dugo: true, tijelo: 32 << 20},
	"POST /akti/{id}/sken":                        {dugo: true, tijelo: 32<<20 + 1<<20},
	"POST /posta/novo":                            {dugo: true, tijelo: 40 << 20},
	"POST /prijave":                               {dugo: true, tijelo: models.NajviseSlika*(20<<20) + 1<<20}, // šest fotografija do 20 MB i polja obrasca
	"POST /readings/station/{id}/uvoz":            {dugo: true, tijelo: 32<<20 + 1<<20},
	"POST /readings/station/{id}/ocitanja/uvoz":   {dugo: true, tijelo: 32<<20 + 1<<20},
	"POST /readings/structure/{id}/ocitanja/uvoz": {dugo: true, tijelo: 32<<20 + 1<<20},
	"POST /readings/station/{id}/zalijepi":        {dugo: true, tijelo: 16<<20 + 1<<20},
	"POST /territories/uvoz":                      {dugo: true, tijelo: 4<<20 + 1<<20},
	"POST /organizacija/uvoz":                     {dugo: true, tijelo: 4<<20 + 1<<20},
	"POST /firme/uvoz":                            {dugo: true, tijelo: 4<<20 + 1<<20},
	"POST /odrzavanje/uvoz":                       {dugo: true, tijelo: 32 << 20},
	"POST /administracija/nazivi":                 {tijelo: 3 << 20},  // grb do models.LogoMaxBytes
	"POST /administracija/zig":                    {tijelo: 3 << 20},  // žig do models.ZigMaxBytes
	"POST /profile/potpis-slika":                  {tijelo: 2 << 20},  // potpis do 1 MiB
	"POST /profile/potpis":                        {tijelo: 10 << 20}, // HTML potpisa e-pošte može nositi slike

	// geometrija u JSON-u
	"POST /api/watercourses/create":       {tijelo: tijeloGeometrije},
	"POST /api/watercourses/update":       {tijelo: tijeloGeometrije},
	"POST /api/watercourses/geometry":     {tijelo: tijeloGeometrije},
	"POST /api/slivovi/sliv":              {tijelo: tijeloGeometrije},
	"POST /api/slivovi/kisomjer/polozaji": {tijelo: tijeloGeometrije},

	// potvrde uvoza, ulaganje i izdavanje arhive
	"POST /administracija/uvoz-niza/pregled-opet":    {dugo: true},
	"POST /administracija/uvoz-niza/upisi":           {dugo: true},
	"POST /administracija/uvoz-niza/zatecen":         {dugo: true},
	"POST /administracija/uvoz-niza/makni-niz":       {dugo: true},
	"POST /administracija/uvoz-izvora/upisi":         {dugo: true},
	"POST /administracija/uvoz-izvora/hidroview":     {dugo: true},
	"POST /administracija/uvoz-krivulja/upisi":       {dugo: true},
	"POST /administracija/uvoz-profila/upisi":        {dugo: true},
	"POST /administracija/ulaganje/pregled":          {dugo: true},
	"POST /administracija/ulaganje":                  {dugo: true},
	"POST /administracija/ulaganje/pospremi":         {dugo: true},
	"POST /administracija/izdavanje/provjera":        {dugo: true},
	"POST /administracija/izdavanje":                 {dugo: true},
	"POST /stations/{id}/paket/ugradi":               {dugo: true},
	"POST /readings/station/{id}/uvoz/potvrdi":       {dugo: true},
	"POST /readings/station/{id}/zalijepi/potvrdi":   {dugo: true},
	"POST /readings/station/{id}/ocitanja/potvrdi":   {dugo: true},
	"POST /readings/structure/{id}/ocitanja/potvrdi": {dugo: true},
	"POST /readings/station/{id}/javni":              {dugo: true},
	"POST /odrzavanje/uvoz/upisi":                    {dugo: true},
	"POST /administracija/telemetrija/mletva":        {dugo: true},
	"POST /dnevnici/novi-cop":                        {dugo: true},
	"POST /akti/{id}/posalji":                        {dugo: true},

	// održavanje baze
	"POST /administracija/baza/sazmi":     {dugo: true},
	"POST /administracija/baza/vacuum":    {dugo: true},
	"POST /administracija/baza/obnovi":    {dugo: true},
	"POST /administracija/baza/spomenici": {dugo: true},
	"GET /administracija/baza/izvoz":      {dugo: true},

	// prognoze
	"POST /prognoze/generiraj":      {dugo: true},
	"POST /prognoze/pripremi-model": {dugo: true},

	// razmjena i uparivanje koje čeka drugi čvor ili čovjeka
	"POST /api/uparivanje/dial":        {dugo: true},
	"POST /api/uparivanje/confirm":     {dugo: true},
	"POST /api/uparivanje/sync":        {dugo: true},
	"POST /api/peers/pair/dial":        {dugo: true},
	"POST /api/peers/pair/confirm":     {dugo: true},
	"POST /api/sync/all":               {dugo: true},
	"POST /api/peers/{node}/sync":      {dugo: true},
	"POST /api/peers/{node}/bootstrap": {dugo: true},

	// izvoz datoteka
	"GET /dogadjanja.xlsx":                        {dugo: true},
	"GET /prijave/{id}/sken.pdf":                  {dugo: true},
	"GET /prijave/{id}/prijava.pdf":               {dugo: true},
	"GET /vodocuvar/knjiga.pdf":                   {dugo: true},
	"GET /vodocuvar/{id}/list.pdf":                {dugo: true},
	"GET /dnevnici/{id}/dnevnik.xlsx":             {dugo: true},
	"GET /dnevnici/{id}/dnevnik.pdf":              {dugo: true},
	"GET /dnevnici/{id}/obracun.xlsx":             {dugo: true},
	"GET /dnevnici/{id}/obracun/{user}/iors.xlsx": {dugo: true},
	"GET /prognoze.xlsx":                          {dugo: true},
	"GET /prognoze/pricuvno.xlsx":                 {dugo: true},
	"GET /prognoze/izdanja.csv":                   {dugo: true},
	"GET /organizacija/sektori.csv":               {dugo: true},
	"GET /organizacija/podrucja.csv":              {dugo: true},
	"GET /firme.csv":                              {dugo: true},
	"GET /sections/{code}/dionica.xlsx":           {dugo: true},
	"GET /sections/dionice.xlsx":                  {dugo: true},
	"GET /territories/zupanije.csv":               {dugo: true},
	"GET /territories/gradovi-i-opcine.csv":       {dugo: true},
	"GET /territories/naselja.csv":                {dugo: true},
	"GET /readings/station/{id}/izvjesce.xlsx":    {dugo: true},
	"GET /stations/{id}/paket.cop":                {dugo: true},
	"GET /stations/{id}/izvjesce.xlsx":            {dugo: true},
	"GET /readings/station/{id}/izvoz.csv":        {dugo: true},
	"GET /readings/station/{id}/ocitanja.csv":     {dugo: true},
	"GET /readings/structure/{id}/ocitanja.csv":   {dugo: true},
	"GET /akti/{id}/akt.pdf":                      {dugo: true},
	"GET /akti/{id}/za-ispis.pdf":                 {dugo: true},
	"GET /sredstva/mts.xlsx":                      {dugo: true},
	"GET /sredstva/promet.xlsx":                   {dugo: true},
	"GET /sredstva/skladista/{id}/skladiste.xlsx": {dugo: true},
	"GET /sredstva/promet/{veza}/potvrda.xlsx":    {dugo: true},
	"GET /sredstva/popisi/{id}/inventura.xlsx":    {dugo: true},
	"GET /izvjesca/{id}/izvjesce.xlsx":            {dugo: true},
	"GET /sektorsko-izvjesce/{id}/izvjesce.xlsx":  {dugo: true},
}

// praviloZa traži pravilo rute koju bi mux odabrao za zahtjev
func (s *Server) praviloZa(r *http.Request) pravilo {
	if s.mux == nil {
		return pravilo{}
	}
	_, uzorak := s.mux.Handler(r)
	return pravilaRuta[uzorak]
}

// rokovi postavlja rokove čitanja i pisanja i najveću veličinu tijela po ruti
func (s *Server) rokovi(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.praviloZa(r)
		if p.netaknuto {
			// tunel preuzima vezu i sam postavlja rokove po poruci; rok
			// pisanja ostao od prethodnog zahtjeva na istoj vezi se briše
			rc := http.NewResponseController(w)
			_ = rc.SetReadDeadline(time.Time{})
			_ = rc.SetWriteDeadline(time.Time{})
			next.ServeHTTP(w, r)
			return
		}

		najvise := p.tijelo
		if najvise <= 0 {
			najvise = najveceTijelo
		}
		if r.ContentLength > najvise {
			odbijPreveliko(w, najvise)
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, najvise)
		}

		// greška znači da omotač odgovora ne zna za rokove; tada vrijedi
		// samo ReadHeaderTimeout i IdleTimeout poslužitelja
		rc := http.NewResponseController(w)
		sad := time.Now()
		switch {
		case p.tok:
			_ = rc.SetReadDeadline(time.Time{})
			_ = rc.SetWriteDeadline(time.Time{})
		case p.dugo:
			_ = rc.SetReadDeadline(sad.Add(rokDugogPosla))
			_ = rc.SetWriteDeadline(sad.Add(rokDugogPosla))
		default:
			_ = rc.SetReadDeadline(sad.Add(rokCitanja))
			_ = rc.SetWriteDeadline(sad.Add(rokPisanja))
		}
		next.ServeHTTP(w, r)
	})
}

// odbijPreveliko odgovara 413 kad tijelo prelazi granicu rute
func odbijPreveliko(w http.ResponseWriter, najvise int64) {
	w.Header().Set("Connection", "close")
	http.Error(w, fmt.Sprintf("Zahtjev je prevelik: najviše %s.", opisVelicine(najvise)), http.StatusRequestEntityTooLarge)
}

// procitajDatoteku čita poslanu datoteku do najvise bajtova. Veća se ne
// reže potiho (odrezana bi se spremila ili poslala kao da je cijela), nego
// se odbija greškom koja kaže granicu.
func procitajDatoteku(r io.Reader, najvise int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, najvise+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > najvise {
		return nil, fmt.Errorf("datoteka je veća od %s", opisVelicine(najvise))
	}
	return b, nil
}

// opisVelicine piše veličinu za poruku korisniku
func opisVelicine(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d kB", n>>10)
	}
}
