package config

import (
	"reflect"
	"strings"
)

// Kljuc je jedna postavka iz gocop.toml, pročitana iz same strukture
// Config: odjeljak, ime, objašnjenje iz oznake comment i mjesto polja u
// strukturi. Postavka koju netko doda u Config pojavi se ovdje sama.
type Kljuc struct {
	Odjeljak     string       // "" za vrh datoteke, inače npr. "sync"
	Ime          string       // npr. "auto_sync"
	Opis         string       // oznaka comment postavke
	OpisOdjeljka string       // oznaka comment odjeljka, npr. [kontakt]
	Tip          reflect.Type // vrsta vrijednosti
	polje        []int
}

// Puni je ključ s odjeljkom, kako ga zove TOML: "sync.auto_sync", "addr"
func (k Kljuc) Puni() string {
	if k.Odjeljak == "" {
		return k.Ime
	}
	return k.Odjeljak + "." + k.Ime
}

// Oznaka je ključ kako ga čovjek nađe u datoteci: "[sync] auto_sync"
func (k Kljuc) Oznaka() string {
	if k.Odjeljak == "" {
		return k.Ime
	}
	return "[" + k.Odjeljak + "] " + k.Ime
}

// Kljucevi su sve postavke, redom kao u strukturi Config
func Kljucevi() []Kljuc {
	return kljuceviIz(reflect.TypeFor[Config](), "", "", nil)
}

// kljuceviIz prolazi poljima strukture; ugniježđena struktura je odjeljak
func kljuceviIz(t reflect.Type, odjeljak, opisOdjeljka string, put []int) []Kljuc {
	var out []Kljuc
	for i := range t.NumField() {
		f := t.Field(i)
		ime := imePolja(f)
		if ime == "" {
			continue
		}
		indeks := append(append([]int{}, put...), i)
		if f.Type.Kind() == reflect.Struct {
			out = append(out, kljuceviIz(f.Type, spoji(odjeljak, ime), f.Tag.Get("comment"), indeks)...)
			continue
		}
		out = append(out, Kljuc{Odjeljak: odjeljak, Ime: ime, Opis: f.Tag.Get("comment"),
			OpisOdjeljka: opisOdjeljka, Tip: f.Type, polje: indeks})
	}
	return out
}

// imePolja je ime polja u datoteci, kao što ga čita go-toml: oznaka toml,
// a bez nje ime polja. Prazno za polje koje se ne čita.
func imePolja(f reflect.StructField) string {
	ime, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	if !f.IsExported() || ime == "-" {
		return ""
	}
	if ime == "" {
		return f.Name
	}
	return ime
}

func spoji(odjeljak, ime string) string {
	if odjeljak == "" {
		return ime
	}
	return odjeljak + "." + ime
}

// NadjiKljuc vraća postavku po punom ključu ("sync.auto_sync")
func NadjiKljuc(puni string) (Kljuc, bool) {
	for _, k := range Kljucevi() {
		if k.Puni() == puni {
			return k, true
		}
	}
	return Kljuc{}, false
}

// Vrijednost je vrijednost postavke k u c
func (c Config) Vrijednost(k Kljuc) any {
	return reflect.ValueOf(c).FieldByIndex(k.polje).Interface()
}

// Postavi upisuje v u postavku k; v mora biti vrste te postavke
func (c *Config) Postavi(k Kljuc, v any) {
	reflect.ValueOf(c).Elem().FieldByIndex(k.polje).Set(reflect.ValueOf(v))
}
