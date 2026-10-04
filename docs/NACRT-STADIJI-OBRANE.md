# Nacrt: stanje obrane iz ovjerenih akata

Nacrt za pregled prije pisanja koda. Polazi od odluka vlasnika od 4. 10. 2026.
(`docs/STABILIZACIJA.md`, „Odluke vlasnika”) i nalaza 11–14 i 20 iz faze 1.

## Pravila (odluke vlasnika, 4. 10. 2026.)

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

## Kako je danas

- Obrana na dionici je jedna epizoda (`defense_episodes`) s jednim poljem
  `Phase` — najvišim dosegnutim stupnjem. Koji su stadiji proglašeni, a koji
  ukinuti, ne pamti se.
- Ovjera akta odmah mijenja epizodu (`zakljuciOvjeru`):
  - uspostava → `Declare` ili `Raise`;
  - prekid pripremnog → `End` (zatvara cijelu obranu, i kad viši stadij traje);
  - prekid višeg stadija → `Raise` s istim stupnjem, što uvijek javi „obrana
    je već na stupnju” — **stanje se ne mijenja** (nalaz 11);
  - akt koji vrijedi više od sat unaprijed ne otvori obranu ni tada ni
    kasnije (nalaz 12).
- Ovjera skenom (`UcitajSkenirani`) ide drugim putem od `Ovjeri` (nalaz 20).

## Prijedlog

**Izvor istine su ovjereni akti.** Oni se već razmjenjuju i potpisani su, pa
svaki čvor iz istih akata izračuna isto stanje — bez rasporeda i bez posebnog
zapisa o stanju.

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

**Storno.** Ovjeren akt poništava onaj tko ga smije ovjeriti, uz obrazloženje;
poništenje se bilježi (tko, kada, zašto), potpisuje ključem čvora i razmjenjuje
kao i akt. Poništen akt ostaje u popisu i ispisu, označen, ali ne ulazi u
stanje. Poništava se samo akt na kojem ne stoji kasniji akt iste dionice (npr.
uspostava redovne nakon koje je već ovjeren njezin prekid): najprije se
poništava kasniji, pa raniji — inače bi kasniji akt prekidao stadij koji nije
proglašen.

**Jedna čista funkcija** računa stanje:
`StanjeObrane(akti, dionica, t) → aktivni stadiji, najviši, od kada`.
Bez baze, s tabličnim testom svih slučajeva. Koriste je:

- kartica dionice i letve, zid sektora, izvješća (stadij dionice);
- ovjera akta — samo da javi neobično (npr. prekid stadija koji nije
  aktivan), a ne da mijenja epizodu;
- popis obrana u povijesti.

**Epizoda ostaje zapis razdoblja**, za povijest i vrhove vodostaja: otvara se
kad skup aktivnih stadija postane neprazan, zatvara kad se isprazni, a
`Phase` je najviši dosegnuti stadij. Izvodi se iz akata (ponovljivo, kao
današnji `Rebuild` iz očitanja), pa se više ne piše izravno pri ovjeri.

**Oba puta ovjere** (`Ovjeri` i ovjera skenom) završavaju u istoj funkciji, s
istim preduvjetima.

## Koraci (svaki zaseban PR)

1. `StanjeObrane` i provjera slijeda kao čiste funkcije s tabličnim testom
   (postupno gore i dolje, odmah viši stadij, zakašnjelo niže, nemoguć
   prekid i uspostava, akt unaprijed, isti trenutak, poništen akt). Ništa se
   još ne mijenja.
2. Prikazi (kartica dionice i letve, zid, izvješća) čitaju stanje iz
   akata. Uz to test koji na istim podacima uspoređuje staro i novo.
3. Ovjera (i skenom) provjerava slijed i više ne mijenja epizodu izravno;
   epizode se izvode iz akata.
4. Storno: status poništenog akta, tko/kada/zašto, potpis i razmjena,
   oznaka u popisu i ispisu.
5. Čišćenje: `Raise` i `End` ostaju samo za obrane računate iz očitanja,
   ako ih još treba.

Podatke ne treba seliti: na čvoru COP Osijek (laptop) postoje dvije ručno
upisane obrane, obje zatvorene, i nijedan ovjeren akt.

## Odgovori vlasnika (4. 10. 2026.)

1. Prekid stadija koji ne traje nije moguć — akt se ne ovjerava.
2. Niži stadij ne ukida se dok viši traje — ukida se samo najviši.
3. Ista logika slaganja vrijedi za sva četiri stadija, i za izvanredno
   stanje koje u hitnom slučaju proglašava rukovoditelj područja.
4. Storno treba: pogrešku ispravlja novi akt, a kad je pogreška to što je akt
   uopće izdan, akt se poništava.

5. Niži stadij ne proglašava se dok viši traje („u pozadini”): proglašava se
   čim viši završi (u istom trenutku: prekid višeg, pa uspostava nižeg).
