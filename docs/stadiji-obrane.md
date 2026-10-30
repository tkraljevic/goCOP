# Stadiji obrane

Obranu od poplava proglašava i prekida čovjek, aktom: rješenjem ili
obaviješću o stupnju obrane. Program obranu sam ne proglašava. Iz ovjerenih
akata računa koji stadij vrijedi na kojoj dionici i u kojem trenutku. Kako
se akt sastavlja i ovjerava, piše u programu, na stranici **Pomoć →
Rješenja i obavijesti**.

Stadij se proglašava aktom o uspostavi, a ukida aktom o prekidu. Rješenje
ili obavijest o tome program zove akt o stupnju obrane.

## Pravila (Odluke (4. 10. 2026.))

1. Četiri su stadija: pripremno stanje, redovna obrana, izvanredna obrana i
   izvanredno stanje. Proglašavaju se **prema gore**, kad postoje uvjeti, a
   ukidaju **obrnutim redom**. Viši stadij ide preko nižeg, a niži u
   pozadini i dalje vrijedi. Kad se ukine izvanredno stanje, vrijedi
   izvanredna obrana; kad se ukine ona, redovna; zatim pripremno stanje.
   Isto vrijedi za sva četiri stadija, i za izvanredno stanje koje u hitnom
   slučaju proglašava rukovoditelj branjenog područja.
2. Viši stadij smije se proglasiti **odmah**, bez nižih. Niži stadij koji
   nije bio proglašen ne nastaje sam. Dok viši traje, niži se ne
   proglašava. Kad viši završi, a uvjeti za niži postoje, niži se
   proglašava **novim aktom** (npr. zakašnjelo pripremno stanje) i poslije
   se ukida kao svaki drugi. Kad se to dogodi u istom trenutku, najprije se
   ukida viši, a zatim proglašava niži.
3. Ukida se samo **najviši** stadij koji traje. Ne ukida se stadij koji ne
   traje, ni niži dok viši traje. Akt koji bi to napravio ne može se
   ovjeriti.
4. Akt stupa na snagu u vrijeme koje u njemu piše („Vrijedi od”), i kad je
   ovjeren ranije.
5. Stanje obrane mijenja **samo ovjeren akt**. U programu nema drugog načina
   da se obrana proglasi ili prekine.
6. Ovjeren akt može se **poništiti (storno)** kad je pogreška to što je akt
   uopće izdan. Kad akt treba ispraviti, ispravlja ga novi akt.

## Kako program računa stanje

Stanje obrane računa se samo iz ovjerenih akata koji nisu poništeni. Nema
posebnog zapisa o trenutačnom stadiju koji bi se mogao razići s aktima. Svako
računalo iz istih akata za isti trenutak izračuna isto stanje.

Program slaže akte dionice redom po „Vrijedi od”:

- akt o **uspostavi** dodaje stadij među one koji traju;
- akt o **prekidu** miče stadij;
- **stanje obrane** je najviši stadij koji traje, a kad ne traje nijedan,
  obrane nema;
- akt kojemu „Vrijedi od” još nije došao još ne vrijedi. Kartica dionice ga
  pokazuje kao najavu, npr. „stupa na snagu 2. 11. u 20:00”.

Kartica dionice pokazuje stadij koji vrijedi, niže stadije u pozadini i akte
koji tek stupaju na snagu. Isto stanje koriste zid sektora, izvješća i knjiga
dionice.

Primjer s izmišljenom dionicom P.1.1, kad se obrana podiže postupno:

| Vrijedi od | Akt | Stadiji koji traju | Stanje |
|---|---|---|---|
| 1. 11. u 08:00 | uspostava pripremnog stanja | pripremno | pripremno stanje |
| 2. 11. u 14:00 | uspostava redovne obrane | pripremno, redovna | redovna obrana |
| 4. 11. u 09:00 | prekid redovne obrane | pripremno | pripremno stanje |
| 5. 11. u 07:00 | prekid pripremnog stanja | — | nema obrane |

Odmah redovna obrana, a poslije zakašnjelo pripremno stanje:

| Vrijedi od | Akt | Stadiji koji traju | Stanje |
|---|---|---|---|
| 3. 11. u 02:00 | uspostava redovne obrane | redovna | redovna obrana |
| 6. 11. u 10:00 | prekid redovne obrane | — | nema obrane |
| 6. 11. u 10:00 | uspostava pripremnog stanja | pripremno | pripremno stanje |
| 8. 11. u 07:00 | prekid pripremnog stanja | — | nema obrane |

## Što ovjera provjerava

Akt se ne može ovjeriti kad bi, uz sve ranije ovjerene akte, u trenutku iz
kojeg vrijedi nastalo nemoguće stanje. To su četiri slučaja:

- prekid stadija koji ne traje;
- prekid nižeg stadija dok viši traje;
- uspostava stadija koji već traje;
- uspostava nižeg stadija dok viši traje.

Poruka kaže koji stadij tada traje. Provjera gleda i već ovjerene akte koji
stupaju na snagu poslije novog akta.

Akt o prekidu uvijek prekida određeni akt o uspostavi. Ovjerava se samo dok
je taj akt ovjeren, nije poništen i nije već prekinut. Ako je u međuvremenu
ovjeren drugi prekid iste uspostave ili je uspostava poništena, treba
pripremiti novi prekid.

Uz to, akt se ovjerava samo dok u sektoru traje aktivna obrana, odnosno dok
je otvoren Dnevnik COP-a. „Vrijedi od” ne smije biti prije otvaranja
dnevnika ni više od 7 dana unaprijed. Ako je više od 24 sata unatrag ili više
od 3 dana unaprijed, program traži izričitu potvrdu i bilježi je uz akt
(Odluke (8. 10. 2026.), pitanje 17).

Iste provjere vrijede kad se akt ovjerava u programu i kad se učitava sken
potpisanog akta. Sken učitava samo onaj tko akt smije i sam ovjeriti.

## Poništenje (storno)

Ovjeren akt poništava onaj tko ga je pripremio ili tko ga smije ovjeriti, uz
razlog. Program bilježi tko je akt poništio, kada i zašto. Poništenje se
potpisuje ključem računala i šalje ostalim računalima, kao i sam akt.
Poništen akt ostaje u popisu i na ispisu, označen, ali ne ulazi u stanje
obrane.

Poništava se samo najkasniji akt dionice. Ako je, na primjer, poslije
uspostave redovne obrane ovjeren i njezin prekid, najprije se poništava
prekid, a tek onda uspostava. Inače bi prekid i dalje prekidao obranu koja
nikad nije proglašena.

## Povijest obrane

Iz istih akata program slaže i povijest obrane dionice. Razdoblje obrane
počinje kad počne prvi stadij, a završava kad prestane zadnji. Uz razdoblje
se pamti najviši stadij koji je dosegnut i trenutak u kojem je vodostaj na
mjerodavnoj letvi prešao prag pripremnog stanja. Po tome se poslije vidi je
li obrana proglašena unaprijed ili tek kad je voda već bila visoko.

Povijest se slaže iznova nakon svake ovjere i svakog poništenja. Akt s
kasnijim „Vrijedi od” ulazi u povijest kad stupi na snagu; program to
provjerava svakih deset minuta. Svako računalo slaže povijest iz istih akata
na isti način, pa sva dobiju iste zapise i ništa se ne upisuje dvaput.

Povijest obrane nastaje samo iz akata. Ne upisuje se ručno i ne računa se iz
očitanja. Zapisi iz ranijih izdanja programa, upisani ručno ili izračunati
iz očitanja, ostaju kakvi jesu.

Dionica koja još nema nijedan ovjeren akt pokazuje obranu zapisanu u ranijim
izdanjima programa. Odluke (11. 10. 2026.) traže da program takve stare
otvorene obrane zatvori, uz napomenu da su zatvorene pri prijelazu na stanje
iz akata. To još nije ugrađeno.
