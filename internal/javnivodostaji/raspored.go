package javnivodostaji

// Raspored kruga preuzimanja. Krug ide svaki sat u istoj minuti, a ne od
// pokretanja programa. Dok je išao od pokretanja, svako ponovno pokretanje
// pomicalo je i izdavanje prognoze: čvor pokrenut u 13:28 preuzimao je u
// :29, pa je prognoza za 14 h izašla tek u 14:29 (18. 10. 2026.). U obrani
// prognoza mora izlaziti uvijek u isto vrijeme, čim izvori objave vrijednost
// punog sata.
//
// Sat je i korak izvora: Hrvatske vode i većina ostalih daju satne
// vrijednosti. ARSO vrijednost daje svakih deset minuta, ali tablica postaje
// nosi cijeli zadnji dan, a krug upisuje sve čega još nema; tako i izvor
// koji u minuti kruga još nije objavio puni sat to dobije u idućem krugu.

import (
	"context"
	"os"
	"strings"
	"time"
)

// cekanjeKruga je koliko krug iz rasporeda čeka prije nego što opet pogleda
// je li završio krug pokrenut rukom
const cekanjeKruga = 5 * time.Second

// Pokreni vrti krug dok se ctx ne ugasi: svaki sat u minuti Minuta. Pri
// pokretanju krug ide odmah samo kad je krug zadnjeg termina propušten ili
// kad za njegov sat nije gotovo ono što ide nakon preuzimanja (Nedovrseno);
// inače čeka idući termin.
func (u *Uvoznik) Pokreni(ctx context.Context) {
	sada := time.Now()
	termin := iduciTermin(sada, u.Minuta)
	if u.odmah(sada) {
		u.krugPoRasporedu(ctx)
	}
	for {
		// Krug dulji od sata (izvori ne odgovaraju) ne povlači za sobom sve
		// propuštene termine, nego jedan krug odmah, pa dalje po satu.
		if z := zadnjiTermin(time.Now(), u.Minuta); termin.Before(z) {
			termin = z
		}
		t := time.NewTimer(time.Until(termin))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		u.krugPoRasporedu(ctx)
		termin = iduciTermin(termin, u.Minuta)
	}
}

// iduciTermin je prvi termin kruga iza t: pun sat i minuta. Pun sat računa
// se od UTC-a; hrvatsko vrijeme od njega odstupa za cijele sate, pa je i
// lokalni.
func iduciTermin(t time.Time, minuta int) time.Time {
	termin := t.Truncate(time.Hour).Add(time.Duration(minuta) * time.Minute)
	if !termin.After(t) {
		termin = termin.Add(time.Hour)
	}
	return termin
}

// zadnjiTermin je zadnji termin kruga do t, uključivo
func zadnjiTermin(t time.Time, minuta int) time.Time {
	return iduciTermin(t, minuta).Add(-time.Hour)
}

// odmah: krug pri pokretanju ide odmah kad letve u zadnjem terminu nisu
// preuzete, ili jesu, ali za sat tog termina nije gotovo ono što ide nakon
// preuzimanja (prognoza nije izdana, npr. jer je čvor ugašen usred kruga)
func (u *Uvoznik) odmah(sada time.Time) bool {
	termin := zadnjiTermin(sada, u.Minuta)
	if zadnje, ima := u.zadnjePreuzimanje(sada); !ima || zadnje.Before(termin) {
		return true
	}
	return u.Nedovrseno != nil && u.Nedovrseno(termin.Truncate(time.Hour))
}

// krugPoRasporedu je krug iz rasporeda. Kad upravo traje krug pokrenut
// rukom, pričeka ga pa ide: ručni je mogao krenuti prije nego što su izvori
// objavili puni sat, a sat bez svog kruga čekao bi prognozu do idućeg.
func (u *Uvoznik) krugPoRasporedu(ctx context.Context) {
	for !u.krug.TryLock() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cekanjeKruga):
		}
	}
	defer u.krug.Unlock()
	u.preuzmiSve(ctx)
}

// zadnjePreuzimanje čita iz biljega kad su letve zadnji put preuzete.
// Biljeg iz budućnosti (sat računala vraćen unatrag) ne vrijedi.
func (u *Uvoznik) zadnjePreuzimanje(sada time.Time) (time.Time, bool) {
	if u.Biljeg == "" {
		return time.Time{}, false
	}
	b, err := os.ReadFile(u.Biljeg)
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil || t.After(sada) {
		return time.Time{}, false
	}
	return t, true
}

// zapamtiPreuzimanje zapiše u biljeg kad je krug počeo preuzimati letve
func (u *Uvoznik) zapamtiPreuzimanje(pocetak time.Time) {
	if u.Biljeg == "" {
		return
	}
	if err := os.WriteFile(u.Biljeg, []byte(pocetak.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		u.Zapisnik("javni vodostaji: vrijeme preuzimanja nije zapamćeno, pa krug nakon ponovnog pokretanja ide odmah: %v", err)
	}
}
