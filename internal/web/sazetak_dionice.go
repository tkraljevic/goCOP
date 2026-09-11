package web

// Što ide u sažetak na vrhu kartice dionice.
//
// Sažetak ima smisla kad sažima nešto što se ne vidi odjednom. Kod dionice s
// jednom poddionicom ne sažima ništa: opis se slaže IZ poddionica, pa ispadne
// doslovan prijepis onoga što piše dva retka niže, a sve četiri brojke stoje
// kao značke uz same stavke. Tada samo odgađa prvi pravi podatak za jedan
// ekran.

import "gocop/internal/models"

// SazetakDionice kaže koji dijelovi sažetka nose nešto novo.
type SazetakDionice struct {
	Opis          bool
	Duljina       bool
	NasipiDuljina bool
	Poddionica    bool
	Vodomjera     bool
	Objekata      bool
	NasipaIBrana  bool
}

// Ima javlja ima li sažetak išta za pokazati.
func (s SazetakDionice) Ima() bool {
	return s.Opis || s.Duljina || s.NasipiDuljina || s.Poddionica ||
		s.Vodomjera || s.Objekata || s.NasipaIBrana
}

// sazetakDionice bira što se pokazuje. Pravilo je jedno: brojka ulazi u sažetak
// samo ako je veća od jedan ili ako dionica ima više poddionica — inače je ista
// ta brojka već značka uz stavku na koju se odnosi.
func sazetakDionice(sec models.Section, parts []PartView) SazetakDionice {
	vise := len(parts) > 1
	return SazetakDionice{
		// Vlastiti opis je čovjekov tekst i uvijek se pokazuje; složeni ima
		// smisla samo kad spaja više poddionica.
		Opis:          sec.DescriptionCustom || (vise && sec.EffectiveDescription() != ""),
		Duljina:       vise && sec.Length() > 0,
		NasipiDuljina: sec.EmbankmentLength() > 0 && (vise || sec.EmbankmentCount() > 1),
		Poddionica:    vise,
		Vodomjera:     vise && sec.GaugeCount() > 0,
		Objekata:      vise && sec.ObjectCount() > 0,
		NasipaIBrana:  vise && sec.EmbankmentCount() > 0,
	}
}
