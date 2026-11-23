package javnivodostaji

import (
	"context"
	"errors"
)

// SporIzvorNakon je koliko letvi zaredom jednom izvoru smije isteći čekanje
// prije nego što ga krug preskoči do kraja. 23. 11. 2026. vizugy.hu nije
// odgovarao, a krug je na svaku od dvadesetak mađarskih letvi čekao po
// minutu: prognoza je izašla 34 minute nakon početka kruga.
const SporIzvorNakon = 2

// errSporIzvor je stanje letve koju krug preskače jer joj izvor ne odgovara
var errSporIzvor = errors.New("izvor u ovom krugu ne odgovara, letva je preskočena")

// ocitanjaIzvora čita letvu s izvora. Kad čekanje istekne, isti upit ide još
// jednom odmah: spora veza (mobilni internet, prvi krug nakon pokretanja)
// zna isteći i kad izvor radi; 1. 10. 2026. u 00:03 istekli su u istom krugu
// Bruck, Budimpešta, Dalj i crpne stanice, tri izvora odjednom. Izvor kojem
// je u ovom krugu isteklo SporIzvorNakon letvi zaredom više se ne zove do
// sljedećeg kruga; letva tada ide na rezervu, ako je ima.
func (u *Uvoznik) ocitanjaIzvora(ctx context.Context, izvor Izvor, adresa string) ([]Redak, error) {
	naziv := izvor.Naziv()
	if u.sporIzvor(naziv) {
		return nil, errSporIzvor
	}
	redci, err := izvor.Ocitanja(ctx, adresa)
	if err != nil && isteklo(err) && ctx.Err() == nil {
		redci, err = izvor.Ocitanja(ctx, adresa)
	}
	u.zabiljeziOdziv(naziv, err)
	return redci, err
}

// sporIzvor javlja preskače li ovaj krug izvor
func (u *Uvoznik) sporIzvor(naziv string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.spori[naziv] >= SporIzvorNakon
}

// zabiljeziOdziv broji istekle upite izvora zaredom; odgovor, i greška koja
// nije istek, brojanje vraća na nulu. Kad izvor prijeđe prag, dnevnik to
// kaže jednom.
func (u *Uvoznik) zabiljeziOdziv(naziv string, err error) {
	if u.brojiIstek(naziv, err) == SporIzvorNakon {
		u.Zapisnik("javni vodostaji: %s ne odgovara (%d letve zaredom); ostale njegove letve u ovom krugu se preskaču", naziv, SporIzvorNakon)
	}
}

// brojiIstek vraća koliko je izvoru zaredom isteklo čekanje, s ovim upitom
func (u *Uvoznik) brojiIstek(naziv string, err error) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.spori == nil {
		u.spori = map[string]int{}
	}
	if err == nil || !isteklo(err) {
		u.spori[naziv] = 0
	} else {
		u.spori[naziv]++
	}
	return u.spori[naziv]
}
