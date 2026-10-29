package peers

// Dijelovi karte u razgovoru (karta_dijelovi.go): u koraku želja i
// sadržaja, uz PDF-ove, i samo uz dijelovi-karte/1 kod obje strane. PDF-ovi
// imaju prednost; dijelovi popune ostatak iste ograde od 48 MiB i 200
// stavki, najviše do ograde puta (ogradaDijelovaPoPutu), koju primatelj
// primijeni na svoje želje, a pošiljatelj istom funkcijom na ono što daje.
//
// Dok je na ovom računalu proglašen stadij obrane, dijelovi idu samo u
// lokalnoj mreži (putULokalnojMrezi), u oba smjera: računalo u obrani preko
// interneta ne traži i ne daje dijelove (Odluke (29. 10. 2026.)). Svaka
// strana gleda svoju obranu. Lokalna mreža je ono što i krug zove LAN
// syncom: pronalaženje i adresa druge strane u privatnoj mreži.

import (
	"context"
	"errors"
	"log"
	"net"
	"net/netip"
	"slices"

	"gocop/internal/razmjena"
)

// Ograde dijelova karte po razgovoru, po putu veze; var, da ih test smanji.
// Uz tunel i probijanje poruka od 8 MiB (kao base64 oko 11 MB) stane u rok
// slanja od 60 s već uz oko 1,5 Mbit/s.
var (
	ogradaDijelovaLAN       int64 = 48 << 20 // lokalna mreža
	ogradaDijelovaInternet  int64 = 16 << 20 // Tailscale i javna adresa
	ogradaDijelovaTunel     int64 = 8 << 20  // tunel i probijanje
	najviseRazgovoraZaKartu       = 8        // dodatnih razgovora zbog karte u jednom krugu
)

// putULokalnojMrezi: veza iz pronalaženja, ili na privatnu adresu druge
// strane (i dolazna na portu), kao korak LAN synca u krugu (krug.dodaj).
// Dolazna veza s adrese ovog računala (127.0.0.1, ::1) nije lokalna mreža:
// tako stiže i ono što s interneta na port prosljeđuje posrednik na ovom
// računalu. zvao: ovo računalo je nazvalo.
func putULokalnojMrezi(c *razmjena.Conn, zvao bool) bool {
	if c.Put != razmjena.PutAdresa {
		return c.Put == razmjena.PutLAN
	}
	adresa := c.RemoteAddr().String()
	return privatnaAdresa(adresa) && (zvao || !adresaOvogRacunala(adresa))
}

// adresaOvogRacunala: adresa, s portom ili bez, je loopback
func adresaOvogRacunala(adresa string) bool {
	host, _, err := net.SplitHostPort(adresa)
	if err != nil {
		host = adresa // adresa bez porta
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// ogradaDijelovaPoPutu je koliko bajtova dijelova karte ide jednim
// razgovorom tim putem
func ogradaDijelovaPoPutu(c *razmjena.Conn, zvao bool) int64 {
	switch {
	case putULokalnojMrezi(c, zvao):
		return ogradaDijelovaLAN
	case c.Put == razmjena.PutTailscale || c.Put == razmjena.PutAdresa:
		return ogradaDijelovaInternet
	}
	return ogradaDijelovaTunel
}

// smijeKartu: ovaj razgovor prenosi dijelove karte. Treba prijenos karte
// (PostaviKarte), oznaku kod obje strane i, dok traje obrana, lokalnu mrežu.
func (r *razgovorSCvorom) smijeKartu(ctx context.Context) *dijeloviKarte {
	d := r.s.karte.Load()
	if d == nil || razmjena.Dogovor(r.moja.Mogucnosti, r.theirs.Mogucnosti)[razmjena.ImeDijeloviKarte] != 1 {
		return nil
	}
	if !putULokalnojMrezi(r.c, r.initiator) && d.obrana(ctx) {
		return nil
	}
	return d
}

// mojeZelje su otisci PDF-ova koje ovaj čvor traži, a iza njih dijelovi
// karte koji mu nedostaju, do onoga što od ograde ostane
func (r *razgovorSCvorom) mojeZelje(ctx context.Context) []string {
	otisci, bajtova := r.s.zeljeniSBajtovima(ctx, r.myWants)
	if r.karta == nil {
		return otisci
	}
	budzet := min(ogradaDijelovaPoPutu(r.c, r.initiator), int64(najviseBajtovaPoRazmjeni-bajtova))
	r.mojiDijelovi = r.karta.zeljeni(budzet, najviseSadrzajaPoRazmjeni-len(otisci))
	return append(otisci, r.mojiDijelovi...)
}

// sadrzajZaDrugu: od traženoga najprije PDF-ovi, pa dijelovi karte do
// onoga što od ograde ostane
func (r *razgovorSCvorom) sadrzajZaDrugu(ctx context.Context, otisci []string) []stavkaSadrzaja {
	if r.karta == nil {
		return r.s.sadrzajZa(ctx, otisci, r.theirs.Wants)
	}
	dijelovi, ostali := r.karta.razdvoji(otisci)
	out := r.s.sadrzajZa(ctx, ostali, r.theirs.Wants)
	bajtova := 0
	for _, st := range out {
		bajtova += st.Bajtova
	}
	budzet := min(ogradaDijelovaPoPutu(r.c, r.initiator), int64(najviseBajtovaPoRazmjeni-bajtova))
	stavke := dijeloviZaSlanje(r.karta, dijelovi, budzet, najviseSadrzajaPoRazmjeni-len(out))
	r.poslanoDijelova = len(stavke)
	return append(out, stavke...)
}

// dijeloviZaSlanje su traženi dijelovi koji su ovdje, do budžeta bajtova i
// najviše stavki
func dijeloviZaSlanje(d *dijeloviKarte, otisci []string, budzet int64, najvise int) []stavkaSadrzaja {
	var out []stavkaSadrzaja
	for _, o := range otisci {
		b, ok := d.procitaj(o)
		if !ok {
			continue
		}
		if int64(len(b)) > budzet || len(out) >= najvise {
			break
		}
		budzet -= int64(len(b))
		out = append(out, stavkaSadrzaja{Otisak: o, Vrsta: VrstaDijelaKarte, Bajtova: len(b), Podaci: b})
	}
	return out
}

// razdvojiPrimljeno odvoji primljene dijelove karte od ostalog sadržaja:
// dio karte nikad ne ide u spremište sadržaja
func razdvojiPrimljeno(stavke []stavkaSadrzaja) (dijelovi, ostalo []stavkaSadrzaja) {
	for _, st := range stavke {
		if st.Vrsta == VrstaDijelaKarte {
			dijelovi = append(dijelovi, st)
		} else {
			ostalo = append(ostalo, st)
		}
	}
	return dijelovi, ostalo
}

// upisiDijelove upiše primljene dijelove karte koje je ovaj razgovor
// tražio; netraženi i krivi se odbace, a dnevnik dobije jedan redak po
// razgovoru
func (r *razgovorSCvorom) upisiDijelove(dijelovi []stavkaSadrzaja) {
	var odbaceno int
	var zadnja error
	for _, st := range dijelovi {
		if r.karta == nil || !slices.Contains(r.mojiDijelovi, st.Otisak) {
			odbaceno, zadnja = odbaceno+1, errNetrazenDio
			continue
		}
		if err := r.karta.upisi(st.Otisak, st.Podaci); err != nil {
			odbaceno, zadnja = odbaceno+1, err
			continue
		}
		r.primljenoDijelova++
	}
	if odbaceno > 0 {
		log.Printf("karta: %d primljenih dijelova karte nije upisano (zadnji: %v)", odbaceno, zadnja)
	}
}

// errNetrazenDio: druga strana je poslala dio karte koji ovaj razgovor nije
// tražio
var errNetrazenDio = errors.New("dio nije tražen")

// pustiDijelove oslobodi dijelove koje je ovaj razgovor zatražio, i kad je
// pukao
func (r *razgovorSCvorom) pustiDijelove() {
	if r.karta != nil {
		r.karta.pusti(r.mojiDijelovi)
	}
}

// zapamtiKartu: razgovor je prenio dijelove, a nekoj strani još nedostaju,
// pa se razgovor s istim čvorom ponavlja odmah (josKarte, syncPeers)
func (r *razgovorSCvorom) zapamtiKartu() {
	if r.poslanoDijelova > 0 || r.primljenoDijelova > 0 && r.karta.nedostaje() {
		r.s.kartaTece.Store(r.c, true)
	}
}
