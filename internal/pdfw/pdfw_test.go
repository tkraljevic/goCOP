package pdfw

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Dokument s hrvatskim znakovima i slikom mora biti valjan PDF; kad je
// pdftotext pri ruci, tekst se mora vratiti sa svim dijakriticima.
func TestPDFSHrvatskimZnakovimaISlikom(t *testing.T) {
	d := Novi("Rješenje o uspostavi", "goCOP")
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		img.Set(x, x, color.RGBA{0, 0, 255, 255})
	}
	var p bytes.Buffer
	_ = png.Encode(&p, img)
	if err := d.SlikaPNG(p.Bytes(), d.Lijevo, d.Y, 24, 24); err != nil {
		t.Fatal(err)
	}
	d.Razmak(30)
	d.Odlomak("RJEŠENJE o uspostavi izvanredne obrane od poplava", 14, true, Sredina)
	d.Odlomak("Na temelju Zakona o vodama, članak 130., a vezano na visinu vodostaja r. Dunav na mjerodavnom vodomjeru Batina, na kojem je zabilježen vodostaj od 652 cm u 12:00 sati, s tendencijom daljnjeg porasta, donosim", 10, false, Lijevo)
	d.Odlomak("Đurđevac, Čačinci, Ćuprija – 25 m³/s · „navodnici“", 10, false, Lijevo)
	for i := 0; i < 80; i++ {
		d.Odlomak("Redak koji gura na drugu stranicu.", 10, false, Lijevo)
	}
	b := d.Bajtovi()
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) || !bytes.Contains(b, []byte("/Count 2")) || !bytes.HasSuffix(bytes.TrimSpace(b), []byte("%%EOF")) {
		t.Fatalf("PDF nije sastavljen kako treba:\n%.300s", b)
	}
	// xref: svaki pomak mora pokazivati na "N 0 obj"
	i := bytes.LastIndex(b, []byte("xref\n"))
	for n, redak := range strings.Split(string(b[i:]), "\n")[2:] {
		if !strings.HasSuffix(redak, " n ") {
			break
		}
		var off int
		if _, err := fmtSscanf(redak, &off); err != nil {
			t.Fatalf("xref redak %q", redak)
		}
		if !bytes.HasPrefix(b[off:], []byte(fmtInt(n+1)+" 0 obj")) {
			t.Errorf("pomak objekta %d pokazuje na %q", n+1, b[off:off+12])
		}
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext nije dostupan, tekst se ne provjerava")
	}
	put := filepath.Join(t.TempDir(), "proba.pdf")
	_ = os.WriteFile(put, b, 0o644)
	out, err := exec.Command("pdftotext", put, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, zeli := range []string{"RJEŠENJE", "članak", "Đurđevac", "Čačinci", "Ćuprija", "652 cm", "m³/s", "„navodnici“"} {
		if !strings.Contains(string(out), zeli) {
			t.Errorf("u tekstu PDF-a nema %q:\n%s", zeli, out)
		}
	}
}

func fmtSscanf(redak string, off *int) (int, error) {
	n := 0
	for _, c := range redak[:10] {
		n = n*10 + int(c-'0')
	}
	*off = n
	return 1, nil
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
