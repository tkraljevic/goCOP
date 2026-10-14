package main

import (
	"gocop/internal/config"
	"gocop/internal/web"
)

// javiMapeArhive zapamti odakle su putanje stabla arhive i mape paketa i
// zapiše pri pokretanju gdje su, punim putanjama. Relativna putanja računa se
// od mape iz koje je goCOP pokrenut, pa pod Postavom, gdje je to mapa baze,
// pokazuje drugamo nego kad se program pokreće ručno iz mape projekta. Kad
// mape nema, redak to kaže, a uz relativnu i što učiniti. Zapamćena datoteka
// postavki i zastavice idu u istu uputu kad se upis u stablo ili izdavanje
// odbije. Prije se to vidjelo tek kao sitna greška na stranici Unos u arhivu.
func javiMapeArhive(pc web.PostavkeCvora, podaci, paketi string, pisi func(string, ...any)) {
	config.ZapamtiIzvorMapa(config.IzvorMapa{Postavke: pc.Put, Zastavice: pc.Zastavice})
	pisi("%s", web.NovaMapaArhive("podaci", podaci, pc).Redak())
	pisi("%s", web.NovaMapaArhive("pakete", paketi, pc).Redak())
}
