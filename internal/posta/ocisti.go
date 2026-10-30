package posta

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Čišćenje oblikovanog pisma i potpisa e-pošte. HTML se čita pravim
// parserom (kao preglednik) i iznova ispisuje: ostaju samo oznake i
// atributi s popisa, a tekst i vrijednosti atributa ispisuju se s
// escapiranjem. Što nije na popisu otpada; oznake koje nose kod ili tuđu
// stranicu (skripta, stil, okvir) otpadaju sa sadržajem, a ostale nepoznate
// oznake ostavljaju samo svoj tekst.

// OcistiHTML miče iz oblikovanog pisma što u pismo ne spada: skripte,
// stilove, okvire, rukovatelje događaja i poveznice koje nisu web ili
// e-pošta. Prazno tijelo (bez teksta i slike) vraća kao prazno.
func OcistiHTML(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	cvorovi, err := html.ParseFragment(strings.NewReader(h), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return ""
	}
	var b strings.Builder
	sadrzaj := false
	for _, c := range cvorovi {
		sadrzaj = ispisiCvor(&b, c) || sadrzaj
	}
	if !sadrzaj {
		return ""
	}
	return b.String()
}

// dopusteneOznake su oznake koje uređivač pisma i potpisa stvara ili koje
// stoje u zadanom potpisu, s atributima koje smiju nositi (uz zajedničke)
var dopusteneOznake = map[string][]string{
	"a": {"href"}, "b": nil, "blockquote": nil, "br": nil, "code": nil, "div": nil, "em": nil,
	"font": {"color", "face", "size"}, "h1": nil, "h2": nil, "h3": nil, "h4": nil, "hr": nil, "i": nil,
	"img": {"src", "alt", "width", "height"}, "li": nil, "ol": nil, "p": nil, "pre": nil, "s": nil,
	"small": nil, "span": nil, "strike": nil, "strong": nil, "sub": nil, "sup": nil,
	"table": {"cellspacing", "cellpadding", "border", "width"}, "tbody": nil, "thead": nil, "tfoot": nil,
	"td": {"colspan", "rowspan", "width", "valign"}, "th": {"colspan", "rowspan", "width", "valign"},
	"tr": nil, "u": nil, "ul": nil,
}

// zajednickiAtributi smije nositi svaka dopuštena oznaka; klase ne, jer bi
// na stranici programa dobile izgled njegovih gumba i prozora
var zajednickiAtributi = []string{"style", "align", "title"}

// prazneOznake nemaju sadržaj ni završnu oznaku
var prazneOznake = map[string]bool{"br": true, "hr": true, "img": true}

// odbaceneSaSadrzajem: oznake čiji sadržaj nije tekst pisma (kod, stil,
// okvir, obrazac); otpadaju cijele
var odbaceneSaSadrzajem = map[string]bool{
	"script": true, "style": true, "head": true, "title": true, "template": true, "noscript": true,
	"iframe": true, "frame": true, "frameset": true, "object": true, "embed": true, "applet": true,
	"svg": true, "math": true, "form": true, "textarea": true, "select": true, "button": true,
	"input": true, "meta": true, "link": true, "base": true, "audio": true, "video": true,
}

// ispisiCvor ispisuje očišćen čvor; javlja ima li u njemu teksta ili slike
func ispisiCvor(b *strings.Builder, n *html.Node) bool {
	switch n.Type {
	case html.TextNode:
		b.WriteString(html.EscapeString(n.Data))
		return strings.TrimSpace(n.Data) != ""
	case html.ElementNode:
		return ispisiOznaku(b, n)
	}
	return false // komentari i doctype otpadaju
}

func ispisiDjecu(b *strings.Builder, n *html.Node) bool {
	sadrzaj := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sadrzaj = ispisiCvor(b, c) || sadrzaj
	}
	return sadrzaj
}

// ispisiOznaku ispisuje dopuštenu oznaku s dopuštenim atributima; od
// nepoznate ostaje samo sadržaj
func ispisiOznaku(b *strings.Builder, n *html.Node) bool {
	ime := strings.ToLower(n.Data)
	if odbaceneSaSadrzajem[ime] || n.Namespace != "" {
		return false
	}
	atributi, dopustena := dopusteneOznake[ime]
	if !dopustena {
		return ispisiDjecu(b, n)
	}
	if ime == "img" && !imaSliku(n) {
		return false // slika bez dopuštene adrese ne prikazuje ništa
	}
	b.WriteString("<" + ime)
	for _, a := range n.Attr {
		if v, ok := dopustenAtribut(a, atributi); ok {
			b.WriteString(" " + strings.ToLower(a.Key) + `="` + html.EscapeString(v) + `"`)
		}
	}
	b.WriteString(">")
	if prazneOznake[ime] {
		return ime == "img"
	}
	sadrzaj := ispisiDjecu(b, n)
	b.WriteString("</" + ime + ">")
	return sadrzaj
}

// dopustenAtribut vraća vrijednost atributa ako ga oznaka smije nositi
func dopustenAtribut(a html.Attribute, svoji []string) (string, bool) {
	if a.Namespace != "" {
		return "", false
	}
	k := strings.ToLower(a.Key)
	if !sadrziAtribut(svoji, k) && !sadrziAtribut(zajednickiAtributi, k) {
		return "", false
	}
	switch {
	case k == "href":
		return a.Val, dopustenaPoveznica(a.Val)
	case k == "src":
		return a.Val, dopustenaSlika(a.Val)
	case k == "style":
		return ocistiStil(a.Val)
	}
	return a.Val, true
}

// imaSliku javlja nosi li slika adresu koja smije ostati
func imaSliku(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, "src") && dopustenaSlika(a.Val) {
			return true
		}
	}
	return false
}

func sadrziAtribut(popis []string, k string) bool {
	for _, x := range popis {
		if x == k {
			return true
		}
	}
	return false
}

// dopustenaPoveznica: web (http, https), e-pošta (mailto) i telefon (tel)
func dopustenaPoveznica(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, p := range []string{"https://", "http://", "mailto:", "tel:"} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}

// dopustenaSlika: logo organizacije, slika ugrađena u pismo (data: PNG,
// JPEG, GIF, WebP) i već ugrađena slika poruke (cid:)
func dopustenaSlika(v string) bool {
	v = strings.TrimSpace(v)
	return v == logoPotpisa || reDataSlikaSrc.MatchString(v) || strings.HasPrefix(strings.ToLower(v), "cid:")
}

// ocistiStil ostavlja obično oblikovanje (boja, veličina, razmak). Stil
// koji učitava adresu ili izraz otpada cijeli, a smještaj na stranici
// (position) se izbacuje, da potpis ne prekrije ostatak stranice.
func ocistiStil(v string) (string, bool) {
	if !dopustenStil(v) {
		return "", false
	}
	var ostaje []string
	for _, d := range strings.Split(v, ";") {
		d = strings.TrimSpace(d)
		svojstvo, _, _ := strings.Cut(d, ":")
		if d == "" || strings.EqualFold(strings.TrimSpace(svojstvo), "position") {
			continue
		}
		ostaje = append(ostaje, d)
	}
	return strings.Join(ostaje, ";"), len(ostaje) > 0
}

// dopustenStil: stil koji učitava adresu ili izraz, ili skriva dio sebe u
// komentaru ili zapisu znaka kodom, ne smije ostati
func dopustenStil(v string) bool {
	l := strings.ToLower(v)
	for _, x := range []string{"url(", "expression", "javascript:", "@import", "\\", "<", ">", "behavior", "-moz-binding", "/*"} {
		if strings.Contains(l, x) {
			return false
		}
	}
	return true
}

// logoPotpisa je adresa loga organizacije u potpisu (service.LogoZaPotpis)
const logoPotpisa = "/posta/logo.png"

var reDataSlikaSrc = regexp.MustCompile(`^(?i)data:image/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\s]+$`)
