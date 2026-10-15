package web

import (
	"html/template"
	"io/fs"
	"testing"
	"text/template/parse"
	"unicode"
	"unicode/utf8"

	"gocop/internal/models"
	webassets "gocop/web"
)

// Naziv kojeg nema predložak ne javlja greškom, nego ispiše sam ključ. Na
// kartici dnevnika A.02 tako je pisalo „Po sektoru i podrucju.”, jer
// ključa „podrucju” nema. Test prolazi kroz sve predloške i za svaki poziv
// naziva sa stalnim ključem provjerava da ga program poznaje.
func TestPredlosciTrazeNaziveKojiPostoje(t *testing.T) {
	provjere := map[string]func(string) bool{
		"term":      nazivPostoji,
		"terml":     nazivPostoji,
		"roleLabel": ulogaPostoji,
	}
	poziva := 0
	obidiPredloske(t, func(mjesto string, c *parse.CommandNode) {
		ident, ok := c.Args[0].(*parse.IdentifierNode)
		if !ok || provjere[ident.Ident] == nil || len(c.Args) < 2 {
			return
		}
		kljuc, ok := c.Args[1].(*parse.StringNode)
		switch {
		case !ok && ident.Ident != "roleLabel":
			t.Errorf("%s: %s traži ključ napisan u predlošku, a dobio je %s", mjesto, ident.Ident, c.Args[1])
		case ok && !provjere[ident.Ident](kljuc.Text):
			t.Errorf("%s: %s %q ne postoji", mjesto, ident.Ident, kljuc.Text)
		case ok:
			poziva++
		}
	})
	// bez ovoga bi test prošao i kad obilazak ne nađe nijedan poziv
	if poziva < 100 {
		t.Errorf("provjereno samo %d poziva naziva; obilazak predložaka vjerojatno ne radi", poziva)
	}
}

// uzBroj uz broj traži sva tri oblika riječi napisana u predlošku (1, 2–4,
// 5 i više). Krivi broj argumenata javlja se tek kad se stranica otvori.
func TestUzBrojImaTriOblika(t *testing.T) {
	poziva := 0
	obidiPredloske(t, func(mjesto string, c *parse.CommandNode) {
		if ident, ok := c.Args[0].(*parse.IdentifierNode); !ok || ident.Ident != "uzBroj" {
			return
		}
		poziva++
		if len(c.Args) != 5 {
			t.Errorf("%s: uzBroj traži broj i tri oblika, a dobio je %d argumenata", mjesto, len(c.Args)-1)
			return
		}
		for _, a := range c.Args[2:] {
			if _, ok := a.(*parse.StringNode); !ok {
				t.Errorf("%s: oblik uz broj mora biti napisan u predlošku, a dobio je %s", mjesto, a)
			}
		}
	})
	if poziva < 50 {
		t.Errorf("nađeno samo %d poziva uzBroj; obilazak predložaka vjerojatno ne radi", poziva)
	}
}

// Na naziv iz postavki ne lijepi se nastavak: „po {{terml "sektor"}}u” daje
// „po sektoru”, ali s nazivom „branjeno područje” ispadne „po branjeno
// područjeu”. Test javlja svako slovo odmah iza naziva ili odmah ispred njega.
func TestNazivBezNastavka(t *testing.T) {
	naziva := 0
	obidiStabla(t, func(stablo *parse.Tree) {
		obidiListe(stablo.Root, func(l *parse.ListNode) {
			for i, n := range l.Nodes {
				if !jeNaziv(n) {
					continue
				}
				naziva++
				mjesto, _ := stablo.ErrorContext(n)
				if i+1 < len(l.Nodes) && tekstPocinjeSlovom(l.Nodes[i+1]) {
					t.Errorf("%s: nastavak na naziv", mjesto)
				}
				if i > 0 && tekstZavrsavaSlovom(l.Nodes[i-1]) {
					t.Errorf("%s: slovo zalijepljeno ispred naziva", mjesto)
				}
			}
		})
	})
	if naziva < 100 {
		t.Errorf("nađeno samo %d naziva; obilazak predložaka vjerojatno ne radi", naziva)
	}
}

// jeNaziv: ispisuje li čvor naziv iz postavki ({{term …}} ili {{terml …}})
func jeNaziv(n parse.Node) bool {
	a, ok := n.(*parse.ActionNode)
	if !ok || a.Pipe == nil || len(a.Pipe.Decl) > 0 || len(a.Pipe.Cmds) == 0 {
		return false
	}
	zadnja := a.Pipe.Cmds[len(a.Pipe.Cmds)-1]
	ident, ok := zadnja.Args[0].(*parse.IdentifierNode)
	return ok && (ident.Ident == "term" || ident.Ident == "terml")
}

func tekstPocinjeSlovom(n parse.Node) bool {
	x, ok := n.(*parse.TextNode)
	if !ok || len(x.Text) == 0 {
		return false
	}
	r, _ := utf8.DecodeRune(x.Text)
	return unicode.IsLetter(r)
}

func tekstZavrsavaSlovom(n parse.Node) bool {
	x, ok := n.(*parse.TextNode)
	if !ok || len(x.Text) == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRune(x.Text)
	return unicode.IsLetter(r)
}

// obidiListe zove f za svaki niz čvorova u stablu, i u granama if, range i with
func obidiListe(n parse.Node, f func(*parse.ListNode)) {
	switch x := n.(type) {
	case *parse.ListNode:
		if x == nil {
			return
		}
		f(x)
		for _, c := range x.Nodes {
			obidiListe(c, f)
		}
	case *parse.IfNode:
		obidiListe(x.List, f)
		obidiListe(x.ElseList, f)
	case *parse.RangeNode:
		obidiListe(x.List, f)
		obidiListe(x.ElseList, f)
	case *parse.WithNode:
		obidiListe(x.List, f)
		obidiListe(x.ElseList, f)
	}
}

// nazivPostoji: za nepoznat ključ Get vraća sam ključ
func nazivPostoji(kljuc string) bool { return models.DefaultTerms().Get(kljuc) != kljuc }

// ulogaPostoji: za nepoznatu šifru DefaultLabel vraća samu šifru
func ulogaPostoji(sifra string) bool { return models.Role(sifra).DefaultLabel() != sifra }

// obidiPredloske čita sve predloške i za svaku naredbu u njima zove f s
// mjestom u obliku „templates/x.html:redak:stupac”
func obidiPredloske(t *testing.T, f func(mjesto string, c *parse.CommandNode)) {
	t.Helper()
	obidiStabla(t, func(stablo *parse.Tree) {
		obidiNaredbe(stablo.Root, func(c *parse.CommandNode) {
			mjesto, _ := stablo.ErrorContext(c)
			f(mjesto, c)
		})
	})
}

// obidiStabla čita sve predloške i zove f za stablo svakog dijela, i onog
// iz {{define}}
func obidiStabla(t *testing.T, f func(stablo *parse.Tree)) {
	t.Helper()
	predlosci, err := fs.Glob(webassets.Files, "templates/*.html")
	if err != nil || len(predlosci) == 0 {
		t.Fatalf("predlošci nisu nađeni: %v", err)
	}
	for _, ime := range predlosci {
		izvor, err := fs.ReadFile(webassets.Files, ime)
		if err != nil {
			t.Fatal(err)
		}
		tp, err := template.New(ime).Funcs(templateFuncs()).Parse(string(izvor))
		if err != nil {
			t.Fatalf("%s: %v", ime, err)
		}
		for _, dio := range tp.Templates() {
			if dio.Tree != nil {
				f(dio.Tree)
			}
		}
	}
}

// obidiNaredbe prolazi stablom predloška i zove f za svaku naredbu, i onu u
// zagradama unutar druge naredbe
func obidiNaredbe(n parse.Node, f func(*parse.CommandNode)) {
	switch x := n.(type) {
	case *parse.ListNode:
		if x != nil {
			for _, c := range x.Nodes {
				obidiNaredbe(c, f)
			}
		}
	case *parse.ActionNode:
		obidiNaredbe(x.Pipe, f)
	case *parse.IfNode:
		obidiGranu(&x.BranchNode, f)
	case *parse.RangeNode:
		obidiGranu(&x.BranchNode, f)
	case *parse.WithNode:
		obidiGranu(&x.BranchNode, f)
	case *parse.TemplateNode:
		obidiNaredbe(x.Pipe, f)
	case *parse.PipeNode:
		if x != nil {
			for _, c := range x.Cmds {
				obidiNaredbe(c, f)
			}
		}
	case *parse.CommandNode:
		f(x)
		for _, a := range x.Args {
			obidiNaredbe(a, f)
		}
	}
}

func obidiGranu(b *parse.BranchNode, f func(*parse.CommandNode)) {
	obidiNaredbe(b.Pipe, f)
	obidiNaredbe(b.List, f)
	obidiNaredbe(b.ElseList, f)
}
