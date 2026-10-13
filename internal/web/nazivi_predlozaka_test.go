package web

import (
	"html/template"
	"io/fs"
	"testing"
	"text/template/parse"

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

// nazivPostoji: za nepoznat ključ Get vraća sam ključ
func nazivPostoji(kljuc string) bool { return models.DefaultTerms().Get(kljuc) != kljuc }

// ulogaPostoji: za nepoznatu šifru DefaultLabel vraća samu šifru
func ulogaPostoji(sifra string) bool { return models.Role(sifra).DefaultLabel() != sifra }

// obidiPredloske čita sve predloške i za svaku naredbu u njima zove f s
// mjestom u obliku „templates/x.html:redak:stupac”
func obidiPredloske(t *testing.T, f func(mjesto string, c *parse.CommandNode)) {
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
			if dio.Tree == nil {
				continue
			}
			stablo := dio.Tree
			obidiNaredbe(stablo.Root, func(c *parse.CommandNode) {
				mjesto, _ := stablo.ErrorContext(c)
				f(mjesto, c)
			})
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
