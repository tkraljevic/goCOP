package main

import "strings"

// samoZaCitanje je adresa baze koju SQLite otvara samo za čitanje.
//
// „?mode=ro” iza običnog puta driver (modernc) odbaci i bazu otvori i za
// pisanje; SQLite ga poštuje tek u adresi koja počinje s „file:”. Veze koje
// su trebale samo čitati arhivu i bazu očitanja tako su mogle i pisati
// (provjereno 13. 10. 2026.). U adresi „?” počinje postavke, „#” završava
// put, a „%” uvodi šifrirani znak, pa se ti znakovi u imenu mape šifriraju.
func samoZaCitanje(put string) string {
	return "file:" + strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(put) + "?mode=ro"
}
