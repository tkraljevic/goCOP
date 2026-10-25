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
