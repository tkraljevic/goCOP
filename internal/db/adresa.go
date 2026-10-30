package db

import "strings"

// sifrirajPut šifrira znakove koji u „file:” adresi imaju značenje: „?”
// počinje postavke, „#” završava put, a „%” uvodi šifrirani znak
var sifrirajPut = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23")

// Adresa je adresa baze s postavkama, npr.
// Adresa(put, "_pragma=busy_timeout(5000)"). Put se šifrira kao „file:”
// adresa. Uz običan put i „?” iza njega mapa s „?” ili „#” u imenu kvari
// adresu: SQLite otvori drugu datoteku, a postavke se izgube ili ne prođu.
// Baza u memoriji (":memory:") i adresa koja već počinje s „file:” ostaju
// kakve jesu, a postavke se samo dopišu.
func Adresa(put string, postavke ...string) string {
	adresa := put
	if put != ":memory:" && !strings.HasPrefix(put, "file:") {
		adresa = "file:" + sifrirajPut.Replace(put)
	}
	if len(postavke) == 0 {
		return adresa
	}
	znak := "?"
	if strings.Contains(adresa, "?") {
		znak = "&"
	}
	return adresa + znak + strings.Join(postavke, "&")
}

// ZaPisanje je adresa baze u koju program piše: Adresa s trajnim upisom
// (TrajniUpis), s pravom pisanja od početka transakcije (UpisOdPocetka)
// i s čekanjem do pet sekundi kad bazu drži drugi pisac. Bez čekanja bi
// transakcija koja traži pravo pisanja na početku odmah pala.
func ZaPisanje(put string, postavke ...string) string {
	return Adresa(put, append(postavke, "_pragma=busy_timeout(5000)", TrajniUpis, UpisOdPocetka)...)
}
