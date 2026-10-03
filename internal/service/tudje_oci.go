package service

import "context"

type kljucTudjihOciju struct{}

// TudjimOcima označi zahtjev koji administrator šalje gledajući program
// tuđim očima: tuđa spremljena lozinka e-pošte tada se ne otključava (ni za
// sandučić, ni za adresar, ni za slanje akta), jer bi program pod tuđim
// računom radio na poslužitelju e-pošte tvrtke.
func TudjimOcima(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucTudjihOciju{}, true)
}

// gledaTudjimOcima javlja je li zahtjev označen s TudjimOcima
func gledaTudjimOcima(ctx context.Context) bool {
	v, _ := ctx.Value(kljucTudjihOciju{}).(bool)
	return v
}
