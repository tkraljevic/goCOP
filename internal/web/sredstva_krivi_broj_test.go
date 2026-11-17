package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Krivo upisan broj u inventuri i potrebama više ne postaje tiho nula:
// obrazac se vraća s upisanim, poruka kaže koji je redak kriv, a ništa se
// ne sprema.
func TestSredstvaKriviBroj(t *testing.T) {
	o := novaOkolinaSredstava(t)

	w := o.zovi(t, upravaSredstava(), http.MethodPost, "/sredstva/popisi", url.Values{
		"skladiste": {o.sk.ID}, "dan": {"2025-12-31"}, "u:lopata:": {"12a"}, "k:lopata:": {"12"},
		"u:vrece-50x80:PRAZNO": {"100"}, "k:vrece-50x80:PRAZNO": {"100"}})
	tijelo := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(tijelo, "VratiUpisano=true;") || !strings.Contains(tijelo, "Lopata") ||
		!strings.Contains(tijelo, "ništa nije spremljeno") {
		t.Fatalf("inventura s krivim brojem: %d %s", w.Code, tijelo)
	}
	if strings.Contains(tijelo, "Vreće") {
		t.Errorf("ispravan redak ne ide u poruku: %s", tijelo)
	}
	popisi, err := o.svc.Popisi(context.Background(), "B", o.sk.ID, 0)
	if err != nil || len(popisi) != 0 {
		t.Errorf("inventura s krivim brojem spremljena: %d %v", len(popisi), err)
	}

	osnova := "/sredstva/skladista/" + o.sk.ID + "/potrebe"
	w = o.zovi(t, upravaSredstava(), http.MethodPost, osnova, url.Values{"godina": {"2027"}, "pt:lopata": {"-3"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "„Potrebno” mora biti broj") || !strings.Contains(w.Body.String(), "Godina=2027;") {
		t.Errorf("potreba s negativnim brojem: %d %s", w.Code, w.Body.String())
	}
}

func TestRedakVrste(t *testing.T) {
	o := novaOkolinaSredstava(t)
	vrste, err := o.svc.SveVrste(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vrste {
		if v.ID == "vrece-50x80" {
			if got := redakVrste(v, "PRAZNO"); !strings.Contains(got, "(") {
				t.Errorf("redak vreća bez oblika: %q", got)
			}
			if got := redakVrste(v, ""); got != v.Naziv {
				t.Errorf("redak bez oblika: %q", got)
			}
		}
	}
}
