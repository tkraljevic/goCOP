# Faza 3 u mjesec dana

Plan od 11. 10. 2026., za mjesec rada s većim limitom (od 12. 10. do 13. 11.
2026.). Slijedi redoslijed iz Odluka (11. 10. 2026.): prvo čitljivost i
pojednostavljenje (faza 3 iz [STABILIZACIJA.md](STABILIZACIJA.md)), zatim
mreža, a tek onda proširenja. Polazište je mjerenje od 11. 10. 2026.:

| | 4. 10. | 11. 10. |
|---|---:|---:|
| pokrivenost | 51,8 % | 64,9 % |
| kritična pokrivenost | 50,4 % | 69,3 % |
| prosječni CRAP | 43,4 | 18,0 |
| funkcije iznad cilja (kritične / ostale) | 476 / 530 | 373 / 466 |

## Pravila za cijeli mjesec

- Svaki korak je zaseban PR s testovima. Prvo se pišu testovi koji zaključaju
  današnje ponašanje, a tek onda se mijenja kod.
- Preuređuje se samo kad to poboljšava čitljivost. Gdje ne pomaže, zapiše se
  iznimka s razlogom.
- Korak koji mijenja brojeve koje dežurni čita ide uz usporedbu prije i
  poslije na istoj bazi ili arhivi.
- Posao koji se dijeli (testovi po paketima, lagana pojednostavljenja) ide
  usporedo, u zasebnim radnim stablima. Rizični koraci idu jedan za drugim.
- Granice kvalitete za nove i dirane funkcije su strože od prvog dana
  (odluka 11. 10.).

## Po danima

### 1. tjedan (12.–16. 10.): pravila, dorade, testovi

| dan | posao | ishod |
|---|---|---|
| 1 | Strože granice: nove kritične funkcije CRAP ≤ 7, ostale ≤ 10, dirana ne smije postati gora; pravila pisanja koda u `CODE_QUALITY.md`. Popis 42 commita drugog kruga dorada i što je od njih već ispravljeno drugim putem. | PR granica; popis dorada |
| 2 | Dorade bez sukoba: `odluke/ocitanja-raspon`, `odluke/testovi-izmisljeni`, `odluke/zid`. | PR |
| 3 | Dorade sa sukobom: `odluke/akti`, `odluke/knjiga-ovlasti`, `odluke/uprava`; pregled. | PR |
| 4–5 | Testovi za 205 kritičnih funkcija kojima trebaju samo testovi, usporedno po paketima (prognoza, peers, arhiva, uvoz, repository, prijava). | PR po paketu |

### 2. tjedan (19.–23. 10.): `applyOne` i protokol razmjene

| dan | posao | ishod |
|---|---|---|
| 6 | Testovi koji zaključaju primjenu po entitetu: novija i starija verzija, spomenik, površina, sukobi. | testovi |
| 7–8 | Razdvajanje `applyOne` i `removeFromSurface` po entitetu, bez promjene ponašanja. Usporedba iste baze nakon primjene istih verzija prije i poslije, na kopiji stvarne baze. | PR |
| 9–10 | Protokol na toj granici: inačica entiteta, nepoznati entitet (prema odluci), zapis odbijenih i djelomično podržanih poruka, pločica razmjene to pokazuje. Dokumentacija protokola. | PR |

Rok za `applyOne` je 15. 11. 2026.; plan ga stavlja ranije, da ostane vremena.

### 3. tjedan (26.–30. 10.): koraci 1–3 faze 3 i greške

| dan | posao | ishod |
|---|---|---|
| 11 | Korak 1: odluka po dionici pri ovjeri akta kao čista funkcija, provjerena cijelom tablicom (radnja × stupanj × stanje). | PR |
| 12 | Korak 2: uvoz ugovora: provjera područja prije upisa, uparivanje odvojeno od upisa. | PR |
| 13–14 | Korak 3: provjera unatrag: mjerenje odvojeno od ispisa i spremanja. Usporedba tablice promašaja prije i poslije na istoj arhivi. | PR |
| 15 | Pregled 496 errcheck nalaza: stvarne greške se popravljaju, a namjerno zanemarene se označe. | PR |

### 4. tjedan (2.–6. 11.): osvježavanje, lagana pojednostavljenja, ovlasti

| dan | posao | ishod |
|---|---|---|
| 16–17 | Korak 4: osvježavanje: budućnost vrhova i primjena promašaja na jednom mjestu. Testna arhiva (`spoj`, `hq_krivulje`); usporedba izdanja prije i poslije. | PR |
| 18 | 86 laganih pojednostavljenja (CC 11–15) u kritičnim paketima, usporedno po paketima. | PR po paketu |
| 19–20 | Korak 5: ovlasti, prvi komadi: metode s imenom namjere, preseljenje izravnih čitanja datoteku po datoteku. | PR po komadu |

### Rezerva (9.–13. 11.): ovlasti, mjerenje, izdanje

- Ovlasti, nastavak.
- Srednja pojednostavljenja (CC 16–30) u kritičnim paketima.
- Novo mjerenje kvalitete i zapisane iznimke.
- **Izdanje 0.0.36:** rekonstrukcija iz susjeda, drugi krug dorada, `applyOne` s protokolom. Nadogradnja svih čvorova, i Windowsa.

## Poslije mjeseca

- **Na manjem limitu:** srednja pojednostavljenja u webu, pokrivenost cijelog
  koda do 85 %, stare teške funkcije (`RunJournals`, `opisi`).
- **Transport veza/1** ([plan-protokol-povezivanja.md](plan-protokol-povezivanja.md)).
- **Tek onda proširenja:** prognoza po branjenim područjima
  ([plan-prognoza-po-tokovima.md](plan-prognoza-po-tokovima.md)) i karte
  (nasipi, objekti, obuhvati poplave).

## Odluke potrebne za plan

Već riješeno:
- **Doseg 0 i glačanje u provjeri unatrag:** u 0.0.34 i odlukom 16.
- **Prekid koji nije pripremni:** Odluke (4. 10. 2026.) o stadijima obrane;
  ukida se samo najviši stadij koji traje.

Otvoreno, s rokom:

| pitanje | potrebno do |
|---|---|
| Što čvor radi sa zapisom entiteta koji ne poznaje (novija inačica)? | 2. tjedan |
| Dnevni list otvoren prije ponoći, a prvi put spremljen poslije nje: danas se odbija i tekst se gubi. | 1. tjedan |
| Tko smije učitati sken ovjere i poslati ovjereni akt? Danas svatko tko piše na dionicama akta. | 3. tjedan |
| Stare otvorene epizode obrane bez akta i dalje se računaju kao obrana koja traje, a zatvoriti ih se nema čime. | 3. tjedan |
| Ovlasti: smije li dužnost uprave biti bez sektora ili s područjem 0? Smije li privremena uprava skinuti zastavicu globalnog administratora? | 4. tjedan |
