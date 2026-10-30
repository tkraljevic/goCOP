package postava

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gocop/internal/izdanje"
)

// Vraćanje na starije izdanje (Odluke (30. 10. 2026.)). Postava sama
// postavlja samo novije izdanje. Starije postavi tek kad ga globalni
// administrator dopusti na čvoru, na stranici Ovo računalo i mreža; čvor
// dopuštenje drži 30 minuta i javlja ga u /zdravlje (dopusteno_izdanje).
// Vlastitom čvoru vjeruje samo uz potpis ključem koji mu je dala pri
// pokretanju, a usluzi samo s porta ispod 1024 (OdgovorZdravlja). Postava
// dopuštenje pročita još dvaput: prije preuzimanja i neposredno prije
// zamjene programa; uz uslugu rok još provjeri skripta kao root, nakon
// lozinke. Potpis, SHA256SUMS i -version provjeravaju se kao i pri
// nadogradnji. Uz uslugu se Postava iz starijeg izdanja ne preuzima, pa
// ostaje ista. Na izdanja starija od izdanje.NajstarijeVracanje ne vraća.

// Vrati postavlja starije izdanje koje je dopustio globalni administrator
// i vraća ga kad je postavljeno
func (p *Postava) Vrati(ctx context.Context) (string, error) {
	s := p.Stanje()
	if s.Starije == "" {
		return "", errors.New("starije izdanje nije dopušteno. Dopušta ga globalni administrator na stranici Ovo računalo i mreža")
	}
	if err := p.zauzmi("Vraćanje na " + s.Starije); err != nil {
		return "", err
	}
	defer p.oslobodi()
	p.Pisi("Vraćanje na starije izdanje %s, uz dopuštenje s čvora", s.Starije)
	iz, err := p.Ugradnja.Izdanja.PoOznaci(ctx, p.Ugradnja.GOOS, p.Ugradnja.GOARCH, s.Starije)
	if err != nil {
		p.Pisi("Vraćanje odbijeno: %v", err)
		return "", err
	}
	staro, _, err := p.dopustenoVracanje(ctx, iz)
	if err != nil {
		return "", err
	}
	vracanje := posaoUgradnje{ime: "Vraćanje", odbijeno: "Vraćanje odbijeno",
		jos: func(ctx context.Context) (time.Time, error) {
			_, do, err := p.dopustenoVracanje(ctx, iz)
			return do, err
		}}
	novo, err := p.ugradiIzdanje(ctx, iz, staro, vracanje)
	if err != nil {
		return "", err
	}
	p.Pisi("Vraćeno na starije izdanje %s (prije je bilo %s)", novo, staro)
	return novo, nil
}

// dopustenoVracanje ponovno pita čvor za dopuštenje i vraća izdanje koje je
// sada postavljeno i do kad dopuštenje vrijedi, ili grešku kad iz više nije
// dopušteno
func (p *Postava) dopustenoVracanje(ctx context.Context, iz Izdanje) (string, time.Time, error) {
	o := p.pitajCvor(ctx)
	staro := p.postavljenoIzdanje(ctx, o.Izdanje)
	if err := dopustenoIzdanje(iz.Verzija, staro, o.Dopusteno); err != nil {
		p.Pisi("Vraćanje odbijeno: %v", err)
		return "", time.Time{}, err
	}
	return staro, o.Do, nil
}

// dopustenoIzdanje je novijeOd s jednom iznimkom: izdanje koje je globalni
// administrator dopustio na čvoru smije biti starije od postavljenoga, dok
// dopuštenje vrijedi, ali ne starije od izdanje.NajstarijeVracanje. Bilo
// koje drugo starije izdanje i dalje se ne postavlja.
func dopustenoIzdanje(novo izdanje.Verzija, trenutno, dopusteno string) error {
	if !starijeOd(novo.String(), trenutno) {
		return novijeOd(novo, trenutno)
	}
	if izdanje.PrestaroZaVracanje(novo) {
		return fmt.Errorf("izdanje %s starije je od %s, a na tako staro izdanje Postava ne vraća. Na njega se goCOP vraća ručno",
			novo, izdanje.NajstarijeVracanje)
	}
	if d, ok := izdanje.ParsirajOznaku(dopusteno); !ok || d != novo {
		return fmt.Errorf("izdanje %s starije je od postavljenog %s, a na čvoru više nije dopušteno, pa ga Postava ne postavlja. "+
			"Globalni administrator ga može ponovno dopustiti na stranici Ovo računalo i mreža", novo, trenutno)
	}
	return nil
}

// ponuditiStarije: dopušteno izdanje nudi se dok je starije od
// postavljenoga, a nije starije od izdanje.NajstarijeVracanje
func ponuditiStarije(dopusteno, postavljeno string) bool {
	v, ok := izdanje.ParsirajOznaku(dopusteno)
	return ok && starijeOd(dopusteno, postavljeno) && !izdanje.PrestaroZaVracanje(v)
}
