package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Pražnjenje kroz obrazac zahvata: nečitljiv broj oštećenih vraća obrazac s
// upisanim, a ispravan upis daje pražnjenje i otpis oštećenih pod istom vezom.
func TestPraznjenjeKrozObrazac(t *testing.T) {
	o := novaOkolinaSredstava(t)
	put := "/sredstva/skladista/" + o.sk.ID + "/promet"
	preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put,
		url.Values{"vrsta": {models.PrometPunjenje}, "sredstvo": {"vrece-50x80"}, "kolicina": {"40"}}), "success")

	w := o.zovi(t, upravaSredstava(), http.MethodPost, put,
		url.Values{"vrsta": {models.PrometPraznjenje}, "sredstvo": {"vrece-50x80"}, "kolicina": {"20"}, "osteceno": {"pet"}})
	mora(t, w.Body.String(), "nečitljivo oštećeno", "VratiUpisano=true", "Greska=„Od toga oštećeno” nije broj", "Vrsta="+models.PrometPraznjenje)

	_, veza := preusmjerenjeSredstava(t, o.zovi(t, upravaSredstava(), http.MethodPost, put,
		url.Values{"vrsta": {models.PrometPraznjenje}, "sredstvo": {"vrece-50x80"}, "kolicina": {"20"}, "osteceno": {"5"}}), "potvrda")
	redci, err := o.svc.Promet(context.Background(), repository.FiltarPrometa{VezaID: veza})
	if err != nil {
		t.Fatal(err)
	}
	otpis := 0.0
	for _, r := range redci {
		if r.Vrsta == models.PrometOtpis {
			otpis += r.Kolicina
		}
	}
	if len(redci) != 3 || otpis != -5 {
		t.Errorf("pražnjenje kroz obrazac: otpis %v, redci %+v", otpis, redci)
	}
}
