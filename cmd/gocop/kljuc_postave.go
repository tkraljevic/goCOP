package main

import "gocop/internal/izdanje"

// kljucPostave čita ključ kojim čvor potpisuje dopuštenje za starije
// izdanje. Daje ga Postava kroz okolinu (izdanje.OkolinaKljucaPostave) kad
// pokreće čvor s -upravitelj. Iz okoline se odmah briše, da ga ne
// naslijede programi koje čvor pokreće. Bez -upravitelj ključa nema.
func kljucPostave(podPostavom bool, citaj func(string) string, brisi func(string) error) string {
	k := citaj(izdanje.OkolinaKljucaPostave)
	_ = brisi(izdanje.OkolinaKljucaPostave) // os.Unsetenv pada samo uz neispravno ime, a ovo je stalno
	if !podPostavom {
		return ""
	}
	return k
}
