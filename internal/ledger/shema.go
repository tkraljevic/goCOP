package ledger

// Čvorovi ne rade uvijek na istoj verziji programa: Unraid se ažurira slikom,
// laptop binarkom, uredski čvor kad stigne. Knjiga zato mora podnijeti zapis
// koji je napisao noviji program, a da ga stariji ne pokvari.
//
// Tri pravila:
//
//  1. Nepoznata polja se čuvaju. Čvor mijenja zapis tako da zapiše cijelu
//     strukturu kakvu poznaje; polja koja je dodao noviji program prepisuju se
//     iz prethodne verzije, inače bi stariji čvor svakom izmjenom tiho brisao
//     tuđa nova polja (najnovija verzija pobjeđuje). Poznata polja odlučuje
//     tip u programu, ne ono što je u tijelu: polje s omitempty koje je
//     korisnik ispraznio ne smije oživjeti iz stare verzije.
//  2. Shema je po entitetu. Verzija nosi shemu svog entiteta; podiže se samo
//     kad stariji program zapis više ne smije uređivati (promjena značenja
//     polja), ne za svako novo polje, jer nova polja čuva pravilo 1. Zapis
//     novije sheme stariji čvor sprema, prenosi i prikazuje, ali ga ne uređuje.
//  3. Što čvor ne razumije, javlja: zapise novije sheme i entitete kojih nema
//     u programu, da se zna da treba ažurirati.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var (
	shemeMu      sync.RWMutex
	sheme        = map[string]int{}             // entitet → shema; bez upisa SchemaVersion
	bezPrijenosa = map[string]bool{}            // entiteti kojima se polja ne prenose
	umirovljena  = map[string]map[string]bool{} // entitet → polja koja je program namjerno ukinuo
	poznatiEnt   = map[string]bool{}            // entiteti koje program zna; prazno znači sve
)

// PostaviShemu zadaje shemu entiteta. Podiže se samo kad stariji program
// zapis ne smije uređivati; za novo polje ne treba.
func PostaviShemu(entity string, shema int) {
	shemeMu.Lock()
	defer shemeMu.Unlock()
	sheme[entity] = shema
}

// ShemaEntiteta je shema s kojom ovaj program piše i razumije entitet
func ShemaEntiteta(entity string) int {
	shemeMu.RLock()
	defer shemeMu.RUnlock()
	if n, ok := sheme[entity]; ok {
		return n
	}
	return SchemaVersion
}

// BezPrijenosaPolja isključuje entitete kojima se nepoznata polja ne
// prepisuju iz prethodne verzije: potpisane zapise (potpis pokriva ono što je
// potpisano sada, a ne staro polje) i neprozirne pakete.
func BezPrijenosaPolja(entities ...string) {
	shemeMu.Lock()
	defer shemeMu.Unlock()
	for _, e := range entities {
		bezPrijenosa[e] = true
	}
}

// UmiroviPolja navodi polja koja je program namjerno ukinuo: ona se više ne
// prepisuju iz starih verzija, inače bi zauvijek putovala kao nepoznata.
func UmiroviPolja(entity string, polja ...string) {
	shemeMu.Lock()
	defer shemeMu.Unlock()
	if umirovljena[entity] == nil {
		umirovljena[entity] = map[string]bool{}
	}
	for _, p := range polja {
		umirovljena[entity][p] = true
	}
}

// PoznatiEntiteti zadaje entitete koje program zna obraditi. Verzija
// entiteta izvan popisa sprema se i prenosi, ali se javlja kao nepoznata.
// Bez popisa (testovi) svi su poznati.
func PoznatiEntiteti(entities ...string) {
	shemeMu.Lock()
	defer shemeMu.Unlock()
	for _, e := range entities {
		poznatiEnt[e] = true
	}
}

func poznatEntitet(entity string) bool {
	shemeMu.RLock()
	defer shemeMu.RUnlock()
	return len(poznatiEnt) == 0 || poznatiEnt[entity]
}

// NovijaShemaError: zapis je zadnji izmijenio noviji program, a ovaj ga ne
// smije prepisati dok se ne ažurira
type NovijaShemaError struct {
	Entity, EntityID string
	Shema, Lokalna   int
}

func (e *NovijaShemaError) Error() string {
	return fmt.Sprintf("zapis %s/%s izmijenio je noviji program (shema %d, ovaj program zna %d) — ažurirajte goCOP prije uređivanja",
		e.Entity, e.EntityID, e.Shema, e.Lokalna)
}

// Is omogućuje errors.Is(err, ErrNovijaShema)
func (e *NovijaShemaError) Is(target error) bool { return target == ErrNovijaShema }

// ErrNovijaShema je zajednička oznaka za NovijaShemaError
var ErrNovijaShema = &NovijaShemaError{}

// spojiNepoznata vraća tijelo nove verzije dopunjeno poljima prethodne
// verzije koja program ne poznaje. Kad nema što dopuniti, vraća tijelo kako je.
func spojiNepoznata(entity string, prethodna, tijelo []byte, payload any) []byte {
	if len(prethodna) == 0 {
		return tijelo
	}
	shemeMu.RLock()
	iskljucen := bezPrijenosa[entity]
	ukinuta := umirovljena[entity]
	shemeMu.RUnlock()
	if iskljucen {
		return tijelo
	}
	poznata, ok := poznataPolja(reflect.TypeOf(payload))
	if !ok {
		// mapa ili nešto što nije struktura: sva polja su poznata
		return tijelo
	}
	var stara, nova map[string]json.RawMessage
	if json.Unmarshal(prethodna, &stara) != nil || json.Unmarshal(tijelo, &nova) != nil || nova == nil {
		return tijelo
	}
	dopunjeno := false
	for k, v := range stara {
		if _, ima := nova[k]; ima || poznata[k] || ukinuta[k] {
			continue
		}
		nova[k] = v
		dopunjeno = true
	}
	if !dopunjeno {
		return tijelo
	}
	b, err := json.Marshal(nova)
	if err != nil {
		return tijelo
	}
	return b
}

var poljaPoTipu sync.Map // reflect.Type → map[string]bool

// poznataPolja su JSON imena polja koja tip zapisuje, s ugrađenim
// strukturama kao što ih slaže encoding/json. ok je false kad tip nije
// struktura (mapa, popis), pa se ne zna što je poznato.
func poznataPolja(t reflect.Type) (map[string]bool, bool) {
	if t == nil {
		return nil, false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	if v, ok := poljaPoTipu.Load(t); ok {
		return v.(map[string]bool), true
	}
	out := map[string]bool{}
	skupiPolja(t, out, map[reflect.Type]bool{})
	poljaPoTipu.Store(t, out)
	return out, true
}

func skupiPolja(t reflect.Type, out map[string]bool, vidjeno map[reflect.Type]bool) {
	if vidjeno[t] {
		return
	}
	vidjeno[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		ime, _, _ := strings.Cut(tag, ",")
		if tag == "-" {
			continue
		}
		if f.Anonymous && ime == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				skupiPolja(ft, out, vidjeno)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if ime == "" {
			ime = f.Name
		}
		out[ime] = true
	}
}

// Novost je ono što je stiglo, a ovaj program ne razumije do kraja
type Novost struct {
	Entity   string
	Shema    int  // najveća shema koja je stigla
	Lokalna  int  // shema koju ovaj program zna
	Nepoznat bool // entitet kojeg program uopće nema
	Verzija  int  // koliko takvih verzija, otkad je čvor pokrenut ili od početka
}

func (n Novost) String() string {
	if n.Nepoznat {
		return fmt.Sprintf("%s: entitet koji ovaj program ne poznaje (%d verzija)", n.Entity, n.Verzija)
	}
	return fmt.Sprintf("%s: zapisi sheme %d, ovaj program zna %d (%d verzija)", n.Entity, n.Shema, n.Lokalna, n.Verzija)
}

type novosti struct {
	mu   sync.Mutex
	po   map[string]*Novost
	javi func(Novost) // prvi put kad se pojavi, za dnevnik
}

func (n *novosti) zabiljezi(entity string, shema, koliko int) {
	lokalna := ShemaEntiteta(entity)
	nepoznat := !poznatEntitet(entity)
	if shema <= lokalna && !nepoznat {
		return
	}
	n.mu.Lock()
	if n.po == nil {
		n.po = map[string]*Novost{}
	}
	x, bilo := n.po[entity]
	if !bilo {
		x = &Novost{Entity: entity, Lokalna: lokalna, Nepoznat: nepoznat}
		n.po[entity] = x
	}
	if shema > x.Shema {
		x.Shema = shema
	}
	x.Verzija += koliko
	kopija := *x
	javi := n.javi
	n.mu.Unlock()
	if !bilo && javi != nil {
		javi(kopija)
	}
}

// NaNovost postavlja što se radi kad prvi put stigne nešto što program ne
// razumije (npr. zapis u dnevnik)
func (r *Recorder) NaNovost(f func(Novost)) {
	r.nov.mu.Lock()
	r.nov.javi = f
	r.nov.mu.Unlock()
}

// Novosti vraća zapise novije sheme i nepoznate entitete u knjizi
func (r *Recorder) Novosti() []Novost {
	r.nov.mu.Lock()
	defer r.nov.mu.Unlock()
	out := make([]Novost, 0, len(r.nov.po))
	for _, x := range r.nov.po {
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
	return out
}

// UcitajNovosti pregleda knjigu pri pokretanju: zapise novije sheme (po
// djelomičnom indeksu, pa je brzo) i entitete kojih program nema.
func (r *Recorder) UcitajNovosti(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, `SELECT entity, schema_version, COUNT(*) FROM record_versions
		WHERE schema_version > 1 GROUP BY entity, schema_version`)
	if err != nil {
		return err
	}
	type red struct {
		entity string
		shema  int
		n      int
	}
	var redovi []red
	for rows.Next() {
		var x red
		if err := rows.Scan(&x.entity, &x.shema, &x.n); err != nil {
			rows.Close()
			return err
		}
		redovi = append(redovi, x)
	}
	rows.Close()
	ents, err := r.db.QueryContext(ctx, `SELECT entity, COUNT(*) FROM record_versions GROUP BY entity`)
	if err != nil {
		return err
	}
	for ents.Next() {
		var e string
		var n int
		if err := ents.Scan(&e, &n); err != nil {
			ents.Close()
			return err
		}
		if !poznatEntitet(e) {
			redovi = append(redovi, red{e, 0, n})
		}
	}
	ents.Close()
	// Nepoznat entitet broji se jednom, svim verzijama (zato prvi); njegove
	// verzije novije sheme iz prvog upita su već u tom broju i dižu samo shemu.
	sort.SliceStable(redovi, func(i, j int) bool { return redovi[i].shema == 0 && redovi[j].shema != 0 })
	for _, x := range redovi {
		if x.shema > 0 && !poznatEntitet(x.entity) {
			r.nov.zabiljezi(x.entity, x.shema, 0)
			continue
		}
		r.nov.zabiljezi(x.entity, x.shema, x.n)
	}
	return nil
}
