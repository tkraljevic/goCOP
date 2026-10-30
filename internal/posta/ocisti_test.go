package posta

import (
	"strings"
	"testing"
)

// Čistač ostavlja samo dopuštene oznake i atribute: rukovatelji događaja,
// skripte, okviri i poveznice koje nisu web ili e-pošta otpadaju, bez obzira
// na to kako je HTML napisan.
func TestOcistiHTMLOstavljaSamoDopusteno(t *testing.T) {
	for _, s := range []struct {
		naziv, ulaz, izlaz string
	}{
		{"događaj u atributu iza kose crte", `<b>x</b><img src="x"/onerror="alert(1)">`, `<b>x</b>`},
		{"događaj u atributu s razmakom", `x<img src=x onerror=alert(1)>`, `x`},
		{"događaj na dopuštenoj oznaci", `<p onclick="x()" onmouseover='y()'>a</p>`, `<p>a</p>`},
		{"skripta složena od dijelova", `x<scr<script></script>ipt>alert(1)</script>`, `xipt&gt;alert(1)`},
		{"skripta i okvir", `<p>a</p><script>x()</script><iframe src="x">b</iframe>`, `<p>a</p>`},
		{"vektorska slika s događajem", `<b>x</b><svg/onload=alert(1)><circle/></svg>`, `<b>x</b>`},
		{"poveznica na kod", `<a href="javascript:alert(1)">x</a>`, `<a>x</a>`},
		{"poveznica na kod s razmacima i velikim slovima", `<a href=" JaVaScRiPt:alert(1)">x</a>`, `<a>x</a>`},
		{"poveznica s kodiranim znakovima", `<a href="jav&#x61;script:alert(1)">x</a>`, `<a>x</a>`},
		{"stil s adresom", `<span style="background:url(https://x.test/a.png)">a</span>`, `<span>a</span>`},
		{"stil s izrazom", `<span style="width:expression(alert(1))">a</span>`, `<span>a</span>`},
		{"obrazac", `<form action="https://x.test"><input name="a">a</form>b`, `b`},
		{"komentar", `a<!-- <script>x()</script> -->b`, `ab`},
		{"nepoznata oznaka ostavlja tekst", `<center>a</center>`, `a`},
		{"navodnik u atributu", `<span title='a"b'>x</span>`, `<span title="a&#34;b">x</span>`},
		{"slika s tuđe adrese", `x<img src="https://x.test/a.png">`, `x`},
		{"smještaj preko cijele stranice", `<div style="position:fixed;top:0;left:0;color:red">a</div>`, `<div style="top:0;left:0;color:red">a</div>`},
		{"smještaj velikim slovima i s razmakom", `<div style=" POSITION : absolute ">a</div>`, `<div>a</div>`},
		{"stil s komentarom", `<div style="/*x*/position:fixed">a</div>`, `<div>a</div>`},
		{"klase programa", `<div class="btn btn-danger modal">a</div>`, `<div>a</div>`},
		{"poveznica na telefon", `<a href="tel:+38531123456">031 123 456</a>`, `<a href="tel:+38531123456">031 123 456</a>`},
	} {
		if got := OcistiHTML(s.ulaz); got != s.izlaz {
			t.Errorf("%s: %q, a treba %q", s.naziv, got, s.izlaz)
		}
	}
}

// Ono što uređivač i zadani potpis stvaraju prolazi nepromijenjeno.
func TestOcistiHTMLCuvaPotpis(t *testing.T) {
	for _, ulaz := range []string{
		`<p>Srdačan pozdrav,</p><table cellspacing="0" cellpadding="0" style="border-collapse:collapse"><tbody><tr><td style="padding:6px 14px 0 0;vertical-align:top"><img src="/posta/logo.png" alt="Hrvatske vode" width="64" style="width:64px;height:auto"></td><td style="padding:6px 0 0 0"><div style="color:#003366"><b>HRVATSKE VODE</b></div><div><a href="mailto:pero@voda.hr" style="color:#003366">pero@voda.hr</a></div></td></tr></tbody></table>`,
		`<div><font color="#1a1a1a">Pero</font><br><i>u</i> <u>v</u> <a href="https://voda.hr">voda.hr</a></div>`,
		`<p>a</p><img src="data:image/png;base64,iVBORw0KGgo=">`,
		`<p>a</p><img src="cid:logo@gocop">`,
		`<blockquote>navod &lt;tekst&gt; &amp; više</blockquote>`,
	} {
		if got := OcistiHTML(ulaz); got != ulaz {
			t.Errorf("promijenjeno:\n%s\n%s", ulaz, got)
		}
	}
}

// Tijelo bez teksta i slike je prazno; neispravan HTML ne ruši čistač.
func TestOcistiHTMLPrazno(t *testing.T) {
	for _, ulaz := range []string{"", "   ", "<p><br></p>", "<script>x()</script>", `<img src="x">`, "<!-- a -->"} {
		if got := OcistiHTML(ulaz); got != "" {
			t.Errorf("%q: %q", ulaz, got)
		}
	}
	if got := OcistiHTML("<p>a<b>b"); !strings.Contains(got, "<b>b</b>") {
		t.Errorf("nezatvorene oznake: %q", got)
	}
}
