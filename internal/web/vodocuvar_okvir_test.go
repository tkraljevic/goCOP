package web

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/pdfw"
)

// naredbe je n izmišljenih naredbi rukovoditelja, svaka od tri retka
func naredbeZaOkvir(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%d. Obrana od poplava: OBAVIJEST o uspostavi pripremnog stanja obrane od poplava (B-%d/2026), branjeno područje 99, dionice B.99.%d, vrijedi od 23.11.2026. u 06:00; vodomjer Primjerovo 194 cm s tendencijom daljnjeg porasta. Ovjerio Ivo Ivić.\n", i+1, i+1, i+1)
	}
	return strings.TrimSpace(b.String())
}

// Rubrika dnevnog lista raste s tekstom: šest naredbi ne izlazi iz okvira,
// a tekst dulji od stranice nastavlja se na sljedećoj.
func TestOkvirListaRasteSTekstom(t *testing.T) {
	sirina := 500.0
	redaka := func(tekst string) int {
		n := 0
		for _, o := range strings.Split(tekst, "\n") {
			n += len(pdfw.Prelomi(o, sirina-20, slovoRubrike, false))
		}
		return n
	}
	t.Run("kratak tekst drži zadanu visinu", func(t *testing.T) {
		d := pdfw.Novi("proba", "proba")
		d.Y = 200
		okvirLista(d, sirina, "Naredbe rukovoditelja:", "1. Obići nasip.", 120, false)
		if d.Y != 320 || d.Stranica() != 1 {
			t.Fatalf("Y %.1f na stranici %d, očekivano 320 na 1", d.Y, d.Stranica())
		}
	})
	t.Run("šest naredbi", func(t *testing.T) {
		d := pdfw.Novi("proba", "proba")
		d.Y = 200
		tekst := naredbeZaOkvir(6)
		okvirLista(d, sirina, "Naredbe rukovoditelja:", tekst, 120, false)
		potrebno := 30 + float64(redaka(tekst))*redakRubrike
		if potrebno <= 120 {
			t.Fatalf("proba ne vrijedi: %d redaka stane u 120", redaka(tekst))
		}
		if d.Stranica() != 1 || d.Y < 200+potrebno-0.01 {
			t.Fatalf("okvir do %.1f na stranici %d, tekst traži do %.1f", d.Y, d.Stranica(), 200+potrebno)
		}
	})
	t.Run("ne stane do dna, ide na sljedeću", func(t *testing.T) {
		d := pdfw.Novi("proba", "proba")
		d.Y = d.H - d.Dolje - 100
		okvirLista(d, sirina, "Naredbe rukovoditelja:", naredbeZaOkvir(6), 120, false)
		if d.Stranica() != 2 {
			t.Fatalf("okvir je na stranici %d, očekivana 2", d.Stranica())
		}
	})
	t.Run("dulji od stranice", func(t *testing.T) {
		d := pdfw.Novi("proba", "proba")
		d.Y = 200
		okvirLista(d, sirina, "Naredbe rukovoditelja:", naredbeZaOkvir(40), 120, false)
		if d.Stranica() < 2 || d.Y > d.H-d.Dolje {
			t.Fatalf("Y %.1f na stranici %d", d.Y, d.Stranica())
		}
	})
	t.Run("prenesen list po sadržaju", func(t *testing.T) {
		d := pdfw.Novi("proba", "proba")
		d.Y = 200
		okvirLista(d, sirina, "Posebna zapažanja:", "", 160, true)
		if d.Y != 230 {
			t.Fatalf("prazna rubrika prenesenog lista do %.1f, očekivano 230", d.Y)
		}
	})
}

// Cijeli list sa šest naredbi i dugim opisom vremena: rubrike rastu, pa
// potpisi prelaze na drugu stranicu umjesto da ih tekst prekrije.
func TestDnevniListSaSestNaredbi(t *testing.T) {
	kratki := &models.VodocuvarskiList{Ime: "Ivo Ivić", Broj: 7, Datum: time.Date(2026, 11, 23, 0, 0, 0, 0, models.Zagreb)}
	dugi := *kratki
	dugi.Naredbe = naredbeZaOkvir(6)
	dugi.Prilike = strings.Repeat("sunčano, kiša, vjetrovito, 5 °C, oborina 14 mm, ", 4)
	stranica := func(l *models.VodocuvarskiList) int {
		return bytes.Count(PDFVodocuvarskiList(l, models.OrgTerms{}, nil, nil), []byte("/Type /Page /Parent"))
	}
	if n := stranica(kratki); n != 1 {
		t.Fatalf("prazan list ima %d stranica, očekivana 1", n)
	}
	if n := stranica(&dugi); n != 2 {
		t.Fatalf("list sa šest naredbi ima %d stranica, očekivane 2", n)
	}
}
