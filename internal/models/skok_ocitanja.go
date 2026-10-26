package models

// PoljeSkok je razlog zapisa o odbačenoj vrijednosti kad vodostaj nije
// izvan raspona, nego je lažni skok: odskočio je od zadnjeg prihvaćenog
// više od praga letve, a iduće očitanje se vratilo na staru razinu
// (Odluke (25. 10. 2026.), paket skok). Takav zapis putuje razmjenom kao i
// ostali odbačeni (entitet odbacena_ocitanja); računalo sa starijim
// programom ga broji među odbačenima izvan raspona, a opis („skok na
// 830 cm”) kaže što je.
const PoljeSkok = "skok"

// OdbacenSkok je zapis o vodostaju očitanja odbačenom kao lažni skok, za
// stranicu letve. Očitanje mora imati vodostaj.
func OdbacenSkok(rd Reading) OdbacenaVrijednost {
	return OdbacenaVrijednost{Letva: rd.GaugeKey(), Kad: rd.MeasuredAt, Polje: PoljeSkok,
		Opis: "skok na " + cijeliHR(*rd.LevelCm) + " cm", Izvor: rd.Origin}
}

// Česti skokovi (Odluke (25. 10. 2026.)): letva kojoj je u zadnjih
// CestiSkokoviDana dana odbačeno barem CestiSkokovi lažnih skokova dobiva
// upozorenje na stranici letve i znak u popisu. Na arhivi 2012.–2026. filtar
// skoka na 88 letvi u petnaest godina odbaci 46 vrijednosti, a obična letva
// nijednu do četiri; tri u tjednu bila su samo mjerila u kvaru (Vízvár u
// proljeće 2017., radar Orešca u rujnu 2026.).
const (
	CestiSkokovi     = 3
	CestiSkokoviDana = 7
)
