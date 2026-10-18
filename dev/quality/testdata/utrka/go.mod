// Probni modul za TestRaceNadzireNasKod: glumi goCOP (isti put modula) s
// namjernom utrkom u vlastitom paketu i u dva tuđa paketa, koja se preko
// replace zovu kao paketi iz tudjiBezRacea. U testdata je da ne uđe ni u
// go test ./... ni u mjerenje.
module gocop

go 1.27.1

require (
	golang.org/x/crypto/utrka v0.0.0
	modernc.org/utrka v0.0.0
)

replace (
	golang.org/x/crypto/utrka => ./tudji/kripto
	modernc.org/utrka => ./tudji/sqlite
)
