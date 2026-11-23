package web

import (
	"net/url"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Umjerenje mjernog uređaja (Odluke (23. 11. 2026.)): obrazac kartice ga nosi,
// zahtjev bez polja ga ne dira, a stranica letve kaže da se uređaj preračunava.
// Brojevi su izmišljeni.
func TestUmjerenjeIzObrascaLetve(t *testing.T) {
	forma := url.Values{"umjerenje_odsjecak": {"120,5"}, "umjerenje_nagib": {"0,85"}}
	f := stationForm{Obrazac: obrazacKartica,
		UmjerenjeOdsjecak: poljeObrasca(forma, "umjerenje_odsjecak"), UmjerenjeNagib: poljeObrasca(forma, "umjerenje_nagib")}
	var st models.Station
	f.primijeni(&st)
	if !st.ImaUmjerenje() || *st.UmjerenjeOdsjecak != 120.5 || *st.UmjerenjeNagib != 0.85 {
		t.Fatalf("kartica prenosi umjerenje: %v %v", st.UmjerenjeOdsjecak, st.UmjerenjeNagib)
	}
	stationForm{Obrazac: obrazacKartica, Name: "Jezero"}.primijeni(&st)
	if !st.ImaUmjerenje() {
		t.Error("zahtjev bez polja umjerenja ga je obrisao")
	}
	prazno := ""
	stationForm{Obrazac: obrazacKartica, UmjerenjeOdsjecak: &prazno, UmjerenjeNagib: &prazno}.primijeni(&st)
	if st.UmjerenjeOdsjecak != nil || st.UmjerenjeNagib != nil {
		t.Errorf("prazna polja trebaju obrisati umjerenje: %v %v", st.UmjerenjeOdsjecak, st.UmjerenjeNagib)
	}
	if poljeObrasca(url.Values{}, "umjerenje_nagib") != nil {
		t.Error("obrazac bez polja")
	}

	if umjerenjeLetve(&st) != nil {
		t.Error("okvir bez umjerenja")
	}
	ods, nag := 120.5, 0.85
	st.UmjerenjeOdsjecak, st.UmjerenjeNagib = &ods, &nag
	o := umjerenjeLetve(&st)
	if o == nil || !o.Teren || !strings.Contains(o.Tekst, "120,5 + 0,8500 · letva") {
		t.Errorf("okvir umjerenja: %+v", o)
	}
}
