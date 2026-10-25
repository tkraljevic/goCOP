package razmjena

// QUIC kao prijenos razmjene (plan WEB synca, proba prije F1). Pozivi
// quic-go-a su samo ovdje i u quic_slusalica.go; ostatak razmjene radi s
// prijenosom, pa razgovor preko toka QUIC-a ide bajt po bajt kao na portu.

import (
	"net"

	"github.com/quic-go/quic-go"
)

// alpnWeb je ALPN WEB synca za ime protokola, npr. gocop-web/1; izvodi se samo
// iz imena protokola, da simulacija i prava mreža ne dijele isti greškom
func alpnWeb(protocol string) string { return protocol + "-web/1" }

// alpnProbe je ALPN probe QUIC-a (ServeExchangeQUIC, DialExchangeQUIC),
// npr. gocop-proba/1: drukčiji od ALPN-a WEB synca, pa proba bez vrata
// WEB synca (Retry, udarci, ključ iz pripreme) nikad ne primi poziv WEB
// synca, a ni obrnuto
func alpnProbe(protocol string) string { return protocol + "-proba/1" }

// tokQUIC je tok QUIC-a kao prijenos razmjene. Adrese su adrese veze kojoj
// tok pripada, zapamćene pri otvaranju, pa se čitaju i nakon zatvaranja.
// Rokovi su toka (SetReadDeadline, SetWriteDeadline).
type tokQUIC struct {
	*quic.Stream
	veza              *quic.Conn
	lokalna, udaljena net.Addr
}

var (
	_ prijenos    = (*tokQUIC)(nil)
	_ odbijaKodom = (*tokQUIC)(nil)
)

func noviTokQUIC(veza *quic.Conn, tok *quic.Stream) *tokQUIC {
	return &tokQUIC{Stream: tok, veza: veza, lokalna: veza.LocalAddr(), udaljena: veza.RemoteAddr()}
}

// odbijKodom zatvori cijelu vezu kodom (0x0104 za nečlana): druga strana
// tako zna zašto, a ne samo da je veza pukla
func (t *tokQUIC) odbijKodom(k KodRazloga) {
	_ = t.veza.CloseWithError(quic.ApplicationErrorCode(k), "") // veza ionako odlazi; greške zatvaranja nema
}

// Close prestane čitati i zatvori pisanje: druga strana tako ne čeka da ovaj
// kraj pročita što mu je još poslala, a ono što je ovaj kraj napisao stigne
// do nje. Pisanje koje je već završeno (druga strana je prestala čitati, ili
// je tok već zatvoren) nije greška, kao ni zatvaranje TCP veze koju je druga
// strana već zatvorila. Veza ostaje otvorena; zatvara je onaj tko ju je
// otvorio.
func (t *tokQUIC) Close() error {
	t.CancelRead(0)
	if t.Context().Err() != nil {
		return nil
	}
	return t.Stream.Close()
}

func (t *tokQUIC) LocalAddr() net.Addr  { return t.lokalna }
func (t *tokQUIC) RemoteAddr() net.Addr { return t.udaljena }
