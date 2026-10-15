package razmjena

// Kodovi razloga odbijanja. Isti broj znači isto na svakom putu: u
// odbijenici razgovora (Envelope.Kod) na portu razmjene i kroz tunel, a
// kasnije i kao kod greške QUIC toka ili veze (docs/plan-web-sync.md, F3).
// Isti brojevi stoje u docs/plan-protokol-povezivanja.md §5.4, po kojem F3
// dodjeljuje kodove, pa novi kod ide i ovdje i tamo.
//
// Popis je stalan: kod se ne mijenja, ne prenumerira i ne daje drugom
// razlogu, ni kad ga program prestane slati. Na žici je uvijek cijeli broj
// bez predznaka, a 0 znači da koda nema. Čvor 0.0.38 polje ne poznaje i
// zanemari ga, pa uz kod uvijek ide i razlog (Odbijenica).
type KodRazloga uint64

const (
	// KodOpce je greška bez posebnog razloga (QUIC: zatvaranje veze ili
	// toka zbog unutarnje greške)
	KodOpce KodRazloga = 0x0100
	// KodNepoznatKanal: QUIC tok na kanalu koji ovaj čvor ne poznaje
	KodNepoznatKanal KodRazloga = 0x0101
	// KodZauzet: sva mjesta za dolazne razmjene su zauzeta (RazlogZauzet)
	KodZauzet KodRazloga = 0x0102
	// KodOdbijeno je odbijanje bez točnijeg koda
	KodOdbijeno KodRazloga = 0x0103
	// KodNijeClan: druga strana nije član mreže ili je opozvana. Slat će ga
	// QUIC (F3); port i tunel takvu vezu i dalje zatvore bez odbijenice.
	KodNijeClan KodRazloga = 0x0104
	// 0x0105 je zauzet za proračun posrednika; posrednik je odgođen, a kod
	// se ne daje drugom razlogu

	// KodUpisZauzet: pozvani nije dočekao bravu upisa, pa odbija ostatak
	// razgovora; poslano stiže sljedećom razmjenom
	KodUpisZauzet KodRazloga = 0x0106
	// KodBezTailscalea: veza je stigla na Tailscale adresu čvora kojem je
	// Tailscale u razmjeni isključen; pozivatelj zove sljedeću adresu
	KodBezTailscalea KodRazloga = 0x0107
)

// Odbijenica je poruka kojom čvor odbija razmjenu: kod za program, razlog
// za čovjeka i za čvor 0.0.38, koji koda ne poznaje. Odbijenica bez razloga
// nije odbijenica ni starom ni novom čvoru, pa je ovo jedini način da se
// složi.
func Odbijenica(kod KodRazloga, razlog string) Envelope {
	return Envelope{Kind: VrstaOdbijeno, Reason: razlog, Kod: kod}
}
