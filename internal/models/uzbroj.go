package models

// UzBrojHR bira oblik riječi uz broj po hrvatskoj sklonidbi: 1 vrijednost,
// 2 vrijednosti, 5 vrijednosti, ali 11 vrijednosti i 21 vrijednost. Oblik
// može nositi i glagol koji se slaže s brojem („letva čeka”, „letve čekaju”,
// „letvi čeka”). Bez toga na stranici piše „21 očitanja” ili „2 ispravaka”.
// Isto pravilo koriste predlošci (uzBroj) i poruke u Go kodu.
func UzBrojHR(n int, jednina, dvojina, mnozina string) string {
	if n < 0 {
		n = -n
	}
	if n%100 >= 11 && n%100 <= 14 {
		return mnozina
	}
	switch n % 10 {
	case 1:
		return jednina
	case 2, 3, 4:
		return dvojina
	}
	return mnozina
}
