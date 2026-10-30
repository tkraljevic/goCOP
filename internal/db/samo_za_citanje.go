package db

// SamoZaCitanje je adresa baze koju SQLite otvara samo za čitanje. Program
// tako otvara svaku vezu koja smije samo čitati: arhivu, bazu očitanja i bazu
// prognoza.
//
// „?mode=ro” iza običnog puta driver (modernc) odbaci i bazu otvori i za
// pisanje; SQLite ga poštuje tek u adresi koja počinje s „file:”. Veze koje
// su trebale samo čitati tako su mogle i pisati, a baza koje nema nastajala
// je prazna (provjereno 13. 10. 2026.). U adresi „?” počinje postavke, „#”
// završava put, a „%” uvodi šifrirani znak, pa se ti znakovi u imenu mape
// šifriraju. Ostale postavke dopisuju se s „&”, npr.
// SamoZaCitanje(put)+"&_pragma=busy_timeout(3000)".
func SamoZaCitanje(put string) string {
	return "file:" + sifrirajPut.Replace(put) + "?mode=ro"
}
