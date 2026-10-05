# Stanje obrane iz ovjerenih akata — pravila i izvedba

Prvotni nacrt od 4. 10. 2026. proveden je u **0.0.34-alfa** (`3194e39`).
Dokument je usklađen s izvedbom 5. 10. 2026.; naziv datoteke ostaje radi
postojećih poveznica iz koda i planova. Polazi od odluka i nalaza
11–14 i 20 u [planu stabilizacije](STABILIZACIJA.md).

## Pravila (odluke od 4. 10. 2026.)

1. Četiri stadija (pripremno stanje, redovna obrana, izvanredna obrana,
   izvanredno stanje) proglašavaju se **prema gore**, kad postoje uvjeti, a
   ukidaju **obrnutim redom**: viši stadij ide preko nižeg, a niži u pozadini
   i dalje vrijedi. Kad se ukine izvanredno stanje, vrijedi izvanredna
   obrana; kad se ukine ona, redovna; pa pripremno stanje.
2. Veći stadij smije se proglasiti **odmah**, bez prethodnih. Kad se on
   ukine, a uvjeti za niži postoje, niži se proglašava **novim aktom**
   (zakašnjelo pripremno stanje) i poslije ukida kao svaki drugi. Niži
   stadij koji nije bio proglašen ne nastaje sam.
3. **Ne prekida se stadij koji ne traje**, i **ne ukida se niži dok viši
   traje** — ukida se samo najviši aktivni stadij.
4. Akt stupa na snagu **prema vremenu koje u njemu piše** (`Vrijedi`), i kad
   je ovjeren ranije.
5. Stanje obrane mijenja **samo ovjeren akt** (izravne rute uklonjene, PR #16).
6. Ovjeren akt može se **poništiti (storno)**: kad je pogreška to što je akt
   uopće izdan. Kad akt treba ispraviti, ispravlja ga novi akt.

## Što je zamijenjeno u 0.0.34

- Stara epizoda (`defense_episodes`) pamtila je samo najviši dosegnuti
  stupanj, bez skupa stadija koji trenutačno vrijede.
- Ovjera je izravno mijenjala epizodu: prekid višeg stadija nije mijenjao
  stanje, a akt s budućim početkom nije kasnije otvarao obranu.
- Ovjera u programu i učitavanje skena nisu provjeravali iste preduvjete.

To je opis prethodne izvedbe, a ne popis otvorenih grešaka.

## Sadašnji izračun

**Izvor istine su ovjereni, neponišteni akti.** Svaki čvor iz istog skupa
akata za isti trenutak izračuna isto stanje — bez raspoređenog zadatka i
posebnog zapisa o trenutačnom stadiju.

Stanje dionice u trenutku *t* slaže se iz ovjerenih, neponištenih akata s
`Vrijedi ≤ t`, redom po `Vrijedi` (u istom trenutku prekid prije
proglašenja):

- **uspostava S** dodaje stadij S među aktivne — smije samo kad S ne traje i
  kad ne traje viši stadij (niži se proglašava tek kad viši završi);
- **prekid S** miče S — smije samo kad je S **najviši** aktivni stadij;
- **stanje obrane** je najviši aktivni stadij; bez aktivnih stadija obrane
  nema;
- akt s `Vrijedi > t` još ne vrijedi — prikazuje se kao „stupa na snagu
  2. 11. u 20:00”.

Primjer (dionica P.1.1), postupno:

| Vrijedi | Akt | Aktivni stadiji | Stanje |
|---|---|---|---|
| 1. 11. 08:00 | uspostava pripremnog | pripremno | pripremno |
| 2. 11. 14:00 | uspostava redovne | pripremno, redovna | redovna |
| 4. 11. 09:00 | prekid redovne | pripremno | pripremno |
| 5. 11. 07:00 | prekid pripremnog | — | nema obrane |

Odmah redovna, pa zakašnjelo pripremno:

| Vrijedi | Akt | Aktivni stadiji | Stanje |
|---|---|---|---|
| 3. 11. 02:00 | uspostava redovne | redovna | redovna |
| 6. 11. 10:00 | prekid redovne | — | nema obrane |
| 6. 11. 10:00 | uspostava pripremnog | pripremno | pripremno |
| 8. 11. 07:00 | prekid pripremnog | — | nema obrane |

**Ovjera provjerava slijed.** Akt se ne ovjerava kad bi stanje u trenutku
njegova `Vrijedi` (uz sve ranije ovjerene akte) bilo nemoguće: prekid stadija
koji ne traje, prekid nižeg dok viši traje, uspostava stadija koji već traje,
uspostava nižeg dok viši traje.
Poruka kaže koji stadij tada traje. Ista provjera vrijedi i za ovjeru skenom.

**Storno.** Ovjeren akt poništava onaj tko ga je pripremio ili tko ga smije
ovjeriti, uz obrazloženje;
poništenje se bilježi (tko, kada, zašto), potpisuje ključem čvora i razmjenjuje
kao i akt. Poništen akt ostaje u popisu i ispisu, označen, ali ne ulazi u
stanje. Poništava se samo akt na kojem ne stoji kasniji akt iste dionice (npr.
uspostava redovne nakon koje je već ovjeren njezin prekid): najprije se
poništava kasniji, pa raniji — inače bi kasniji akt prekidao stadij koji nije
proglašen.

**Čista funkcija `models.StanjeDionice(akti, dionica, t)`** vraća stanje
(`StanjeObrane`) i greške slijeda. `StanjaDionica` računa sve dionice,
`NajavljeniAkti` izdvaja buduće akte, a `RazdobljaObrane` izvodi povijest.
Sve su u `internal/models/stanje_obrane.go`, bez pristupa bazi. Koriste ih:

- kartica dionice i letve, zid sektora, izvješća (stadij dionice);
- ovjera akta — `ProvjeriSlijed` odbija novi akt ako bi stvorio nemoguć
  slijed, uključujući već ovjerene akte koji stupaju na snagu poslije njega;
- popis obrana u povijesti.

**Epizoda ostaje zapis razdoblja**, za povijest i vrhove vodostaja: počinje
kad skup aktivnih stadija postane neprazan, završava kad se isprazni, a
`Phase` je najviši dosegnuti stadij. `AktService.uskladiEpizode` nakon
ovjere ili storna usklađuje izvedenu povijest sa stalnim identifikatorima.
Trenutačno stanje računa se pri čitanju; spremljena povijest za budući akt
osvježava se pri idućem izvođenju povijesti. Ako nema mjerodavnih akata,
prikazi zadržavaju potporu zatečenim epizodama.

**Oba puta ovjere** (`Ovjeri` i `UcitajSkenirani`) provjeravaju aktivnu
obranu u sektoru i slijed stadija kroz `provjeriPrijeOvjere`, prije spremanja
skena, te završavaju u `zakljuciOvjeru`.

## Provedeni koraci

1. Čiste funkcije i tablični testovi za slaganje stadija, buduće akte,
   nemoguće slijedove i storno.
2. Kartica dionice, dnevno izvješće, zid sektora i knjiga dionice koriste
   stanje iz akata.
3. Zajednički preduvjeti ovjere u programu i skenom te izvođenje epizoda.
4. Storno s razlogom, osobom, vremenom i potpisom čvora; razmjena i prikaz.
5. Uklonjene izravne HTTP rute proglašenja i prekida mimo akta. Usluga
   epizoda ostaje za povijest, zatečene podatke i račun iz očitanja.

Provjere su u `internal/models/stanje_obrane_test.go`,
`internal/service/stanje_obrane_akti_test.go` i
`internal/service/stanje_obrane_prikaz_test.go` te u web testovima akata.

**Nadogradnja:** svi čvorovi trebaju najmanje 0.0.34-alfa. Stariji čvor ne
razumije poništenje pa bi poništen akt i dalje uključio u stanje obrane.

## Odluke o otvorenim pitanjima (4. 10. 2026.)

1. Prekid stadija koji ne traje nije moguć — akt se ne ovjerava.
2. Niži stadij ne ukida se dok viši traje — ukida se samo najviši.
3. Ista logika slaganja vrijedi za sva četiri stadija, i za izvanredno
   stanje koje u hitnom slučaju proglašava rukovoditelj područja.
4. Storno treba: pogrešku ispravlja novi akt, a kad je pogreška to što je akt
   uopće izdan, akt se poništava.

5. Niži stadij ne proglašava se dok viši traje („u pozadini”): proglašava se
   čim viši završi (u istom trenutku: prekid višeg, pa uspostava nižeg).
