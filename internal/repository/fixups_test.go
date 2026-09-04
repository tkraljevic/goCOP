package repository

import "testing"

func TestCistaTitulaBezOrganizacijeIZatipaka(t *testing.T) {
	cases := map[string]string{
		"dipl.ing.građ. Hrvatske vode":   "dipl.ing.građ.",
		"mag.ing.amb. Hrvatske vode":     "mag.ing.amb.",
		"dipl.ing.geoteh. Hrvatske vode": "dipl.ing.geoteh.",
		"dipl.ing.grad.":                 "dipl.ing.građ.",
		"ing.grad.":                      "ing.građ.",
		"mag.ing. aedif.":                "mag.ing.aedif.",
		"struč.spec.ing.aedif":           "struč.spec.ing.aedif.",
		"dipl.ing.građ. univ.spec.oec.":  "dipl.ing.građ. univ.spec.oec.", // dvije titule, ostaju
		"dipl.ing.građ., MBA.":           "dipl.ing.građ., MBA.",
		"mag.ing.aedif.":                 "mag.ing.aedif.",
		"":                               "",
		"ing.građ. Vodoprivreda d.o.o.":  "ing.građ.",
		"građ.teh. VGO Osijek":           "građ.teh.",
	}
	for in, want := range cases {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, očekivano %q", in, got, want)
		}
	}
}
