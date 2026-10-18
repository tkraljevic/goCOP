# Kvaliteta koda

goCOP služi obrani od poplava, pa kod mora ostati takav da ga se može
sigurno mijenjati. Ovdje piše koji je cilj, kakvo je stanje danas, po kojim
se pravilima piše kod i kako se provjerava.

## Cilj i stanje

### Cilj

Svaka izmjena provjerava se prije nego što uđe u program, i nijedna ne
smije pogoršati izmjereno stanje. Dijelovi o kojima ovisi obrana trebaju
biti gotovo potpuno pokriveni testovima: razmjena među računalima, prijava
i ovlasti, akti i stanje obrane, uvoz, arhiva vodostaja i prognoza. Za njih
je cilj 95 % naredbi, a za cijeli kod 85 %.

### Što se mjeri i zašto

- Pokrivenost testovima: koliki se dio naredbi izvrši barem jednom u
  testovima. Kod koji nijedan test ne izvrši nitko nije provjerio.
- Složenost funkcije: broj različitih putova kroz funkciju. Svaki uvjet,
  petlja ili grana dodaje jedan. Funkciju s mnogo putova teško je pročitati
  i potpuno ispitati.
- Rizik izmjene spaja složenost i pokrivenost. Jednostavna i testirana
  funkcija ima mali rizik, a složena funkcija bez testova velik. Kad su sve
  naredbe funkcije pokrivene, rizik je jednak njezinoj složenosti. U struci
  se ta mjera zove CRAP.
- Uz to se gleda što nađe statička analiza (npr. greška koju kod ne
  provjeri), ponovljeni kod i istodobni pristup istim podacima iz više
  dretvi u testovima.

Nova funkcija u dijelovima o kojima ovisi obrana smije imati složenost i
rizik izmjene najviše 7, a drugdje 10 (Odluke (11. 10. 2026.)). Postojeća
funkcija iznad granice smije se mijenjati ako joj složenost ne naraste, a
rizik ne naraste više od 0,5.

### Stanje

Zadnje referentno mjerenje je od 15. 10. 2026. Uz njega su brojke prvog
mjerenja:

| | 4. 10. 2026. | 15. 10. 2026. |
|---|---:|---:|
| pokrivenost cijelog koda | 51,8 % | 76,2 % |
| pokrivenost dijelova o kojima ovisi obrana | 50,4 % | 99,2 % |
| prosječna složenost funkcije | 5,4 | 4,7 |
| prosječni rizik izmjene | 43,4 | 12,3 |
| funkcije iznad granice u dijelovima o kojima ovisi obrana | 559 | 286 |

Prosječna funkcija ima oko pet putova, a tri od četiri funkcije imaju pet
ili manje. Polovica funkcija ima rizik 4 ili manji. Prosječni rizik od 12,3
podiže oko 380 funkcija s rizikom iznad 30. Većina njih ima malo testova, a
samo 13 ih je u dijelovima o kojima ovisi obrana.

U dijelovima o kojima ovisi obrana je 2.018 funkcija, a 286 ih prelazi
granicu. Gotovo sve su dobro pokrivene testovima, ali su prevelike: imaju
previše grananja. Najviše ih je u prognozi i uvozu. Takve se
funkcije ne dijele odjednom. Izmjena ih ne smije pogoršati, a dijele se
postupno, uz iste testove prije i poslije. Uvoz iz stare evidencije BP16 je
privremen, pa se ne preuređuje: njegove su složene funkcije izuzete do
13. 4. 2027. (Odluke (13. 10. 2026.)).

Statička analiza ne nalazi nijednu grešku. Ostaje 938 upozorenja, od toga
487 mjesta na kojima kod ne provjerava vraćenu grešku. Još nije pokrenuto
mutacijsko testiranje, koje provjerava bi li testovi primijetili namjerno
ubačenu grešku.

Brojke pokazuju gdje treba pogledati, a ne dokazuju da je program bez
grešaka. Pokrivenost kaže da se naredba izvršila u testu, a ne da je test
provjerio pravu stvar.

### Prvo mjerenje

Napravljeno je 4. 10. 2026. na izdanju 0.0.31-alfa
(commit `f5adb84`, macOS, Go 1.27.1), na 4.687 funkcija. Provjera je tada
pala zbog tri nalaza statičke analize. Sva tri uklonjena su istog dana
(`4382ef4`), bez promjene ponašanja. Istog dana mjerenje je prestalo brojati
zatvaranje datoteka i veza kao neprovjerenu grešku, pa je takvih mjesta od
1.199 ostalo 498. Cijeli izvještaj prvog mjerenja i njegovi podaci čuvaju se
u povijesti repozitorija.

## Pravila pisanja koda

Pravila vrijede za svakoga tko mijenja kod (Odluke (11. 10. 2026.)). Kod
mora biti čitljiv onome tko ga sutra mijenja, a ne samo proći provjeru.

Mijenja se najmanji smisleni dio, uz test koji provjerava ponašanje, a
zatim se pokrene provjera i pregleda razlika prema referentnom mjerenju.
Funkcije se ne dijele radi same brojke. Provjere se ne gase, granice se ne
snižavaju, a referentno mjerenje ne pomiče se bez objašnjenja. U obrani od
poplava ispravna odluka, trag o tome tko je što napravio i čitljivost
važniji su od prosjeka.

1. **Prvo test.** Ponašanje koje se mijenja najprije se zaključa testom.
   Preuređenje bez promjene ponašanja dokazuje se istim testovima prije i
   poslije, a brojke koje dežurni čita usporedbom na istoj bazi ili arhivi.
2. **Male funkcije s imenom koje kaže što rade.** Jedna funkcija, jedan
   korak. Ako se dio može nazvati („provjeri pravo”, „složi primatelje”),
   to je zasebna funkcija. Funkcija se ne dijeli radi brojke kad to otežava
   čitanje; tada ide zapisana iznimka.
3. **Greška se ne guta.** Svaka greška se vraća s opisom gdje je nastala
   (`fmt.Errorf("…: %w", err)`) ili se izričito zabilježi uz objašnjenje
   zašto se ide dalje. Nova neprovjerena greška ruši provjeru.
4. **Nema globalnih tablica postavki.** Što se mijenja ili razlikuje po
   računalu, toku ili sektoru predaje se kao parametar, a ne čita iz
   globalne varijable koju testovi podmeću.
5. **Komentar kaže zašto, a ne što.** Odluka, izvor podatka, mjerenje ili
   slučaj iz prakse koji je pravilo uveo, s datumom. Što kod radi kaže kod.
6. **Imena na hrvatskom**, kao i dosad, osim ustaljenih Go i tehničkih
   izraza.
7. **Procjena nikad kao mjerenje.** Popunjen, preračunat ili rekonstruiran
   podatak nosi oznaku izvora i ne ulazi tamo gdje se mjerenje dokazuje.
8. **Tekst koji korisnik čita piše se cijelim rečenicama** (Odluke
   (13. 10. 2026.)). Rečenica se ne slaže od dijelova: bez `{{if}}` usred
   rečenice i bez rečenice složene od izlaza dviju funkcija. Riječ uz broj
   bira samo `uzBroj` u predlošku, a u Go kodu `models.UzBrojHR` (ili
   postojeći `dan` i `sat` u `models`), uvijek sa sva tri oblika:
   `{{uzBroj .N "list" "lista" "listova"}}`. Glagol i pridjev koji se slažu
   s brojem idu u isti oblik („pismo je premješteno”, „pisma su
   premještena”, „pisama je premješteno”), a iza prijedloga oblik je u
   padežu koji prijedlog traži („u 21 navratu”). Na `{{term}}` i `{{terml}}`
   ne lijepi se nastavak („`{{terml "sektor"}}u`”), jer organizacija naziv
   može promijeniti. Rečenica se složi tako da naziv stoji u nominativu, npr.
   iza dvotočke ili u zagradi, a ne iza prijedloga. Naziv razine ustroja
   (sektor, branjeno područje, centar) uzima se iz nazivlja organizacije
   (`OrgTerms`), ne piše se rukom. Testovi provjeravaju da naziv u predlošku
   postoji, da uz njega nema slova i da su napisana sva tri oblika. Jesu li
   oblici i padež dobri, testovi ne vide; to se provjerava čitanjem.

## Kako provjeriti (make quality)

Potrebni su Go iz `go.mod`, Git i `make`, a na Linuxu i C prevoditelj (za
provjeru istodobnog pristupa). Prvi put alati se preuzimaju s interneta:

```sh
make quality-tools
make quality
```

`make quality` pokreće sve testove s mjerenjem pokrivenosti i provjerom
istodobnog pristupa, statičku analizu (postavke u `.golangci.yml`) i mjeri
svaku funkciju. Rezultat uspoređuje s referentnim mjerenjem u
`quality/baseline.json`. Provjera pada kad padne test, kad statička analiza
nađe grešku ili kad izmjena pogorša neku funkciju ili ukupnu pokrivenost.
Nova funkcija mora odmah biti unutar granica. Granice i popis dijelova o
kojima ovisi obrana su u `quality/config.json`, a alat u `dev/quality/`.

Sažetak je u `.quality/code-health.md`, a naredbe koje nijedan test ne
izvrši vide se u `.quality/coverage.html`. Na kraju `make quality` kaže jesu
li izvedene i provjere registara na stvarnim podacima (vidi
`make provjera-registara` u [INSTALACIJA.md](INSTALACIJA.md)).

Ista provjera radi na GitHubu, na svakom pull requestu, na svakom slanju u
glavnu granu i jednom tjedno, uz provjeru poznatih ranjivosti. Ne treba
vanjska usluga, a izvorni kod ne šalje se nikome na analizu.

Referentno mjerenje pomiče se samo namjerno, nakon pregleda
(`make quality-baseline ACCEPT_BASELINE=yes`), u zasebnom pull requestu koji
riječima kaže što se promijenilo i zašto.

Iznimke su u `quality/config.json`, svaka s razlogom i rokom. Istekla
iznimka ruši provjeru. Iznimka se ne dodaje samo da bi provjera prošla, a
posebno ne za stvarnu grešku u ovlastima ili uvozu.

Mutacijsko testiranje traje dugo i ne ide u običnu provjeru. Alat mijenja
izvorne datoteke, pa treba zasebnu čistu kopiju repozitorija i jedan paket:
`make quality-deep MUTATION_ROOT=/put/do/kopije MUTATION_PACKAGE=internal/models`.
Pripremljeno je, ali još nije pokrenuto.

### Provjera istodobnog pristupa

Testovi idu s Go-ovom provjerom istodobnog pristupa (race). Ona javi kad
dvije dretve u isto vrijeme, bez zaključavanja, pristupe istim podacima, a
bar jedna ih mijenja. Provjera usporava testove nekoliko puta, a dva tuđa
dijela mnogo više: pogonski program baze SQLite (`modernc.org/...`) i
bcrypt, kojim se provjeravaju lozinke (`golang.org/x/crypto/...`). Zato
`make quality` i provjera na GitHubu ta dva dijela prevode bez provjere
(Odluke (18. 10. 2026.)). Lokalno svi testovi tako traju tri i pol minute
umjesto šest, uz istu pokrivenost.

Sav kod goCOP-a i standardna biblioteka Go-a, pa i `database/sql`, preko
kojeg goCOP radi s bazom, i dalje su pod provjerom. Ne vidi se samo utrka
koja zahvati kod SQLite-a ili bcrypta, npr. kad naš kod mijenja podatak koji
pogonski program baze u isto vrijeme čita. Test u `dev/quality` na probnom
paketu s namjernom utrkom provjerava da se utrka u kodu goCOP-a i dalje
javlja i da iznimka vrijedi samo za ta dva dijela.

Zato jednom tjedno, i kad se pokrene ručno, GitHub pokreće sve testove s
punom provjerom, bez iznimke (`make quality-puni-race`). Taj posao javlja
samo utrku ili pad testa i ništa ne uspoređuje s referentnim mjerenjem.

### Što piše u izvještaju

Izvještaj je na engleskom, jer su to nazivi alata. Ovo su riječi koje se u
njemu ponavljaju:

- STATUS je `HEALTHY` kad je sve unutar granica i ciljeva, `BASELINE_DEBT`
  kad izmjena ništa nije pogoršala, ali ostaje stari dug (funkcije iznad
  granice, pokrivenost ispod cilja, upozorenja statičke analize), a `FAIL`
  kad je izmjena nešto pogoršala ili je pao test.
- CRAP je rizik izmjene, opisan gore. Funkcije se broje po razredima rizika:
  do 10, od 10 do 30, od 30 do 100 i iznad 100.
- Exact duplication je udio koda koji se doslovno ponavlja u tijelima
  funkcija. Fuzzy candidates su funkcije koje su samo slične. To je popis za
  pregled, a ne dokaz da rade isti posao, pa samo upozorava.
- Premještene funkcije su dijelovi izdvojeni iz postojeće funkcije pri
  preuređenju. Upisuju se u `quality/config.json` uz funkciju iz koje su
  izdvojeni. Zajedno smiju nositi najviše onoliko složenosti koliko je ta
  funkcija izgubila, da se nov kod ne sakrije kao premješten.
- Mutation kaže je li pokrenuto mutacijsko testiranje i s kojim ishodom.
