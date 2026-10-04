# Nacrt: stanje obrane iz ovjerenih akata

Nacrt za pregled prije pisanja koda. Polazi od odluka vlasnika od 4. 10. 2026.
(`docs/STABILIZACIJA.md`, „Odluke vlasnika”) i nalaza 11–14 i 20 iz faze 1.

## Pravila (odluke vlasnika)

1. Četiri stadija (pripremno stanje, redovna obrana, izvanredna obrana,
   izvanredno stanje) proglašavaju se i ukidaju **postupno i neovisno**.
   Kad vrijedi pripremno pa se proglasi redovna i kasnije ukine, pripremno
   i dalje vrijedi dok se i ono ne ukine.
2. Veći stadij smije se proglasiti **odmah**, bez prethodnih.
3. Akt stupa na snagu **prema vremenu koje u njemu piše** (`Vrijedi`), i kad
   je ovjeren ranije.
4. Stanje obrane mijenja **samo ovjeren akt** (izravne rute uklonjene, PR #16).

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

Stanje dionice u trenutku *t*:

- stadij **S je aktivan** ako postoji ovjeren akt o uspostavi S za tu
  dionicu s `Vrijedi ≤ t`, a poslije njega nema ovjerenog akta o prekidu S s
  `Vrijedi ≤ t`;
- **stanje obrane** je najviši aktivni stadij; bez aktivnih stadija obrane
  nema;
- akt s `Vrijedi > t` još ne vrijedi — prikazuje se kao „stupa na snagu
  2. 11. u 20:00”.

Primjer (dionica P.1.1):

| Vrijedi | Akt | Aktivni stadiji | Stanje |
|---|---|---|---|
| 1. 11. 08:00 | uspostava pripremnog | pripremno | pripremno |
| 2. 11. 14:00 | uspostava redovne | pripremno, redovna | redovna |
| 4. 11. 09:00 | prekid redovne | pripremno | pripremno |
| 5. 11. 07:00 | prekid pripremnog | — | nema obrane |

I bez prethodnih: uspostava izvanredne 3. 11. 02:00 → stanje izvanredna;
prekid izvanredne → nema obrane.

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

1. `StanjeObrane` kao čista funkcija s tabličnim testom (sva četiri stadija,
   preskakanje, prekid višeg i nižeg, akt unaprijed, dva akta u istom
   trenutku). Ništa se još ne mijenja.
2. Prikazi (kartica dionice i letve, zid, izvješća) čitaju stanje iz
   akata. Uz to test koji na istim podacima uspoređuje staro i novo.
3. Ovjera više ne mijenja epizodu izravno; epizode se izvode iz akata. Ovjera
   skenom ide istim putem.
4. Čišćenje: `Raise` i `End` ostaju samo za obrane računate iz očitanja,
   ako ih još treba.

Podatke ne treba seliti: na čvoru COP Osijek (laptop) postoje dvije ručno
upisane obrane, obje zatvorene, i nijedan ovjeren akt.

## Pitanja za vlasnika

1. **Prekid stadija koji nije aktivan** (npr. prekid redovne, a redovna nije
   proglašena): odbiti ovjeru ili ovjeriti uz upozorenje?
2. **Ukidanje nižeg dok viši traje** (npr. ukine se pripremno dok traje
   redovna): je li to u praksi moguće? Ako jest, redovna i dalje traje, a kad
   se ukine, obrane više nema.
3. **Izvanredno stanje** koje u hitnom slučaju proglašava rukovoditelj
   područja: vrijedi li ista logika slaganja kao za ostale stadije?
4. **Ispravak pogrešnog akta**: program danas nema poništenje (storno)
   ovjerenog akta — akt je nacrt ili ovjeren. Treba li ga? Ako da, poništen
   akt ne ulazi u stanje, a pogrešku ispravlja novi akt.
