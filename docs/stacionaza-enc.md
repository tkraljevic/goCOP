# Riječni kilometri prema ENC kartama

Tok rijeke na karti programa je linija iz OpenStreetMapa. Riječni kilometri
(rkm) na toj liniji usklađuju se sa službenim oznakama kilometara s ENC
karata plovnih putova Dunava i Drave. Stanje je od 25. 9. 2026.

## Kako program računa

Službene oznake kilometara služe kao sidra. Između dvaju susjednih sidara
program rkm dijeli **po duljini linije toka**, a ne zračnom udaljenošću.
Svaki dio između dva sidra ima svoj omjer. Linija toka se pri tome ne
pomiče i ne pojednostavljuje. Položaji uređaja i upisane stacionaže postaja
ostaju kakvi jesu.

## Pokrivenost i izvor

| Rijeka | Raspon rkm | Sidra | Izdanje ENC-a |
|---|---:|---:|---|
| Dunav | 1295,600–1432,900 | 139 | 11. 9. 2018. |
| Drava | 0,100–23,200 | 25 | 29. 11. 2018. |

Izvor: [službene ENC karte](https://www.vodniputovi.hr/ris/enc-karte/),
[Dunav ZIP](https://www.vodniputovi.hr/enc/dunav/dunav.zip) i
[Drava ZIP](https://www.vodniputovi.hr/enc/drava/drava.zip). Uzimaju se
oznake cijelih kilometara i krajnje dostupne oznake. Obalne oznake druge
vrste ne miješaju se s njima. Svako sidro ima izvornu koordinatu i ENC
ćeliju, a zapis sidara nosi adresu, datum izdanja i otisak (SHA-256)
preuzete datoteke.

To je **izračunata kartografska stacionaža prema službenom izvoru**, a ne
novi službeni geodetski podatak, i **nije za navigaciju**. Karte su iz
2018., a linija iz OpenStreetMapa može ići drugim dijelom korita nego plovni
put. Najveći poprečni odmak sidra od linije toka je oko 79 m na Dravi i
200 m na Dunavu. To nije procjena točnosti stacionaže.

Izvan pokrivenosti program ništa ne produljuje. Ondje stare oznake ostaju
vidljive kao **orijentacijske**, ali ih uzdužni profil ne koristi za
računanje ušća. Za ostatak Drave i međunarodne dionice Dunava trebaju
dodatna provjerena sidra s koordinatama. Koordinate postaja ne koriste se
umjesto neovisnih sidara dok nisu provjerene.

## Provjere

- Sidra na liniji moraju ići strogo jedno za drugim. Ponovljeno sidro ili
  sidro koje ide unatrag program odbija. Sidro dalje od 1 km od linije
  zaustavlja usklađivanje, a program javlja da treba pregled.
- Ručno uređena linija koju sidra ne pokrivaju ili koja im ne odgovara
  ostaje na karti uz upozorenje, bez tvrdnje da je usklađena.
- Iz ENC-a Dunava odbačena su dva zapisa koji se ne slažu s ostalima.
  Ispravno sidro za 1429,0 ostaje.
- Usklađeni kilometri računaju se pri prikazu i ne upisuju se u bazu, pa se
  ni ne šalju drugim računalima. Kad se linija toka promijeni, račun se
  ponovi.
- Testovi provjeravaju da se linija ne mijenja, da se sva sidra vraćaju na
  svoj kilometar, nejednake omjere, granice pokrivenosti, odbačene zapise i
  ponovnu primjenu.

## Stacionaže postaja se ne prepisuju

Usklađeni kilometri ne prepisuju upisanu stacionažu postaje. Usporedba za
tri postaje:

| Postaja | Prema ENC sidrima | Upisano |
|---|---:|---:|
| Osijek (Drava) | 19,113 | 19,100 |
| Dalj (Dunav) | 1354,023 | 1355,100 |
| Batina (Dunav) | 1424,497 | 1424,850 |

Razlike nisu dovoljne da bi se stacionaže prepisale. Prije toga treba
provjeriti položaj uređaja i odakle dolazi njegov rkm koji se koristi u
radu.
