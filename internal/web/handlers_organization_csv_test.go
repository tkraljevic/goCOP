package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// OIB je niz znamenki, ne broj: Excel bi mu pojeo vodeću nulu, pa bi
// Građevinar d.o.o. iz izvoza izašao s deset znamenki umjesto jedanaest.
func TestCsvTextCuvaVodecuNulu(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"03674958581", `="03674958581"`},
		{"12345678901", `="12345678901"`},
		{"", ""},
		{"HR03674958581", "HR03674958581"}, // nije samo znamenke, Excel ga ionako ne dira
		{"3674958-581", "3674958-581"},
	}
	for _, c := range cases {
		if got := csvText(c.in); got != c.want {
			t.Errorf("csvText(%q) = %q, očekivano %q", c.in, got, c.want)
		}
	}
}

// Omot mora preživjeti i pisanje CSV-a: csv.Writer navodnike udvostručuje,
// pa Excel na kraju vidi ="03674958581" i prikaže tekst.
func TestIzvozZadrzavaOmotOkoOIBa(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCSV(rec, "test.csv", [][]string{{"OIB"}, {csvText("03674958581")}})
	out := rec.Body.String()
	if !strings.Contains(out, `"=""03674958581"""`) {
		t.Errorf("izvoz ne sadrži očekivani zapis, dobiveno: %q", out)
	}
}
