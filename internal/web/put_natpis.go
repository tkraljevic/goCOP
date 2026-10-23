package web

// Kako se o putu razmjene govori na pločici (plan WEB synca, F1b). Naziv
// puta daje razmjena.NazivPuta, a ovdje je jedino mjesto s rečenicama
// razloga. Pločicu vidi svaki prijavljeni korisnik, pa u rečenicama nema
// adresa ni imena sustava na kojem radi poslužitelj.

import (
	"strings"

	"gocop/internal/razmjena"
)

// frazaPuta je kuda podaci idu, po nazivu puta
var frazaPuta = map[string]string{
	razmjena.NazivLAN:       "u lokalnoj mreži (LAN sync)",
	razmjena.NazivServer:    "preko poslužitelja (SERVER sync)",
	razmjena.NazivTailscale: "preko Tailscalea",
	razmjena.NazivWeb:       "preko interneta (WEB sync)",
}

// recenicaRazloga je rečenica za razlog puta, s %s na mjestu fraze puta
// kad je treba
var recenicaRazloga = map[razmjena.RazlogPuta]string{
	razmjena.RazlogPutaNijeSeJavio:    "Računalo se ne javlja u lokalnoj mreži; podaci idu %s.",
	razmjena.RazlogPutaBezOdgovora:    "Računalo se ne javlja ni na jednoj poznatoj adresi.",
	razmjena.RazlogPutaUDPNeProlazi:   "UDP ne prolazi, pa WEB sync ne radi; podaci idu %s.",
	razmjena.RazlogPutaNATOvisan:      "Oba računala su iza usmjerivača koji mijenjaju port prema odredištu, pa WEB sync ne prolazi; podaci idu %s.",
	razmjena.RazlogPutaUDPDoDrugih:    "UDP do drugog računala ne prolazi; podaci idu %s.",
	razmjena.RazlogPutaVatrozid:       "Vatrozid na ovom računalu možda ne pušta UDP; podaci idu %s.",
	razmjena.RazlogPutaNijeClan:       "Računalo nije član mreže ili mu je članstvo opozvano. Razmjena s njim ne ide dok ga ne primite ponovno.",
	razmjena.RazlogPutaStarijiProgram: "Na tom računalu je stariji program. Nadogradite ga.",
	razmjena.RazlogPutaPriprema:       "WEB sync se priprema; dotad podaci idu %s.",
	razmjena.RazlogPutaPauza:          "WEB sync čeka nakon neuspjelog pokušaja; dotad podaci idu %s.",
	razmjena.RazlogPutaIstiKljuc:      "Isti ključ ima još jedno računalo, vjerojatno kopija iste mape podataka. Jednom od njih treba novi ključ.",
}

// razlogRijecima je razlog puta rečenicom; prazno kad razloga nema
func razlogRijecima(r razmjena.RazlogPuta, naziv string) string {
	fraza, ok := frazaPuta[naziv]
	if !ok {
		fraza = "drugim putem"
	}
	if tekst, ok := recenicaRazloga[r]; ok {
		return strings.Replace(tekst, "%s", fraza, 1)
	}
	return ""
}
