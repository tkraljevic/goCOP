package main

import (
	"fmt"

	"gocop/internal/models"
)

// Redci ispisa satnog kruga („Detalji postupka” na Prognoze → Postavke) i
// završna poruka pripreme modela. Riječ uz broj, i glagol koji se slaže s
// brojem, bira models.UzBrojHR: „1 letva”, „2 letve”, „5 letvi”, ali
// „21 letva”; „izdan 1 paket”, „izdana 2 paketa”, „izdano 5 paketa”.

// opisStranePrognoze je što je donijela jedna tuđa prognoza, npr.
// „2 letve, 3 nove vrijednosti”
func opisStranePrognoze(letvi, novih int) string {
	return fmt.Sprintf("%d %s, %d %s", letvi, models.UzBrojHR(letvi, "letva", "letve", "letvi"),
		novih, models.UzBrojHR(novih, "nova vrijednost", "nove vrijednosti", "novih vrijednosti"))
}

// opisOborina je redak o satnim oborinama preuzetim za kišomjere
func opisOborina(sati int) string {
	return fmt.Sprintf("oborine: %d %s za kišomjere", sati, models.UzBrojHR(sati, "sat", "sata", "sati"))
}

// opisMjerenjaKisomjera je redak o mjerenjima preuzetim sa stvarnih kišomjera
func opisMjerenjaKisomjera(n int) string {
	return fmt.Sprintf("stvarni kišomjeri: %d %s", n, models.UzBrojHR(n, "mjerenje", "mjerenja", "mjerenja"))
}

// opisUlaganjaKisomjera je redak o ulaganju kišomjera u arhivu, npr.
// „uloženo u arhivu za 2 postaje, izdana 2 paketa”
func opisUlaganjaKisomjera(postaja, paketa int) string {
	return fmt.Sprintf("stvarni kišomjeri: uloženo u arhivu za %d %s, %s %d %s",
		postaja, models.UzBrojHR(postaja, "postaju", "postaje", "postaja"),
		models.UzBrojHR(paketa, "izdan", "izdana", "izdano"), paketa, models.UzBrojHR(paketa, "paket", "paketa", "paketa"))
}

// letveIVrijednosti je koliko letvi i vrijednosti nosi izdanje prognoze,
// npr. „1 letva, 21 vrijednost”
func letveIVrijednosti(letvi, vrijednosti int) string {
	return fmt.Sprintf("%d %s, %d %s", letvi, models.UzBrojHR(letvi, "letva", "letve", "letvi"),
		vrijednosti, models.UzBrojHR(vrijednosti, "vrijednost", "vrijednosti", "vrijednosti"))
}

// opisPripremeModela je poruka na kraju pripreme modela, npr. „Namještena
// 2 pojasa, zapisano 5 promašaja; …”
func opisPripremeModela(pojasa, promasaja int, izdano string) string {
	return fmt.Sprintf("%s %d %s, %s %d %s; %s.",
		models.UzBrojHR(pojasa, "Namješten", "Namještena", "Namješteno"), pojasa, models.UzBrojHR(pojasa, "pojas", "pojasa", "pojasa"),
		models.UzBrojHR(promasaja, "zapisan", "zapisana", "zapisano"), promasaja, models.UzBrojHR(promasaja, "promašaj", "promašaja", "promašaja"),
		izdano)
}
