# Code Health & Quality Gate

Razvojna provjera, odvojena od goCOP aplikacije. Ne pokreće poslužitelj, ne čita
živu bazu, ne uvozi podatke i ne refaktorira kod. Mjeri stanje i uspoređuje ga s
verzioniranim baselineom. Brojke su signal za pregled, ne dokaz operativne sigurnosti.

## Pokretanje

Potreban je Go iz `go.mod`, Git, `make` i okruženje koje podržava Go race detector
(na Linuxu i C prevoditelj). Prva instalacija razvojnih alata treba internet:

```sh
make quality-tools
make quality
```

`make quality` provjerava i testove samog mjernog alata, zatim pokreće:

```sh
go test -race -covermode=atomic -coverprofile=… -count=1 -timeout=10m <paketi>
```

`<paketi>` su paketi glavnog modula koje git vidi (praćene ili nove datoteke,
ne ignorirane): lokalne mape iz `.gitignore`, npr. `tools/`, ne ulaze ni u
testove, ni u lint, ni u mjerenje, pa lokalna provjera mjeri isto što i CI.

Slijede golangci-lint, funkcijski CC/CRAP, exact/fuzzy analiza i regresijski gate.
Kombinirana naredba istodobno provjerava izgradnju testiranih paketa, testove,
race i coverage. Nije provjera svih OS/build-tag kombinacija. Prvi prolaz većeg
projekta traje nekoliko minuta; izlaz testova postupno dolazi u `.quality/tests.log`.

- `make quality-report`: isti izvještaji; regresije ne određuju izlazni kod,
  ali neuspjelo mjerenje/test/race/ozbiljna statika i dalje vraćaju grešku.
- `make quality-test`: brzi testovi i race samog mjernog alata.
- `make quality-deep`: zasebna, opcionalna mutation provjera; vidi niže.
- Bez `make`: iz `dev/quality` pokrenuti `go run . -root ../..`.

Izlaz u `.quality/` nije za Git:

| Datoteka | Namjena |
|---|---|
| `code-health.md` | Sažetak, top 10 CC/CRAP, paketi, duplikacije, blokade |
| `code-health.json` | Sve funkcije, kritični odabir, sirove metrike i regresije |
| `coverage.out`, `coverage-functions.txt`, `coverage.html` | Standardni Go profil, funkcije, nepokriveni retci |
| `lint.json`, `lint.log`, `tests.log` | Izvorni dokazi i dijagnostika |
| `test-evidence.json` | Otisci izvora/profila/loga za izričitu ponovnu analizu |

`HEALTHY` znači da nema nalaza ove konfiguracije; `BASELINE_DEBT` znači da gate
prolazi, ali postoje zatečena odstupanja od ciljeva; `FAIL` znači blokadu.
`measurements_status != PASS` znači da brojčane metrike nisu valjane — nule u
nedovršenom izvještaju nisu izmjereni rezultati. Mutation koji nije izvršen ima
`mutation_score: null`, ne 0 ili 100.

## Alati i opseg

- **golangci-lint v2.14.0**, konfiguracija u `.golangci.yml`: govet,
  staticcheck (SA provjere), errcheck, ineffassign, unused, odabrani revive,
  gocyclo, gocognit, dupl, maintidx i nestif. Testovi nisu isključeni iz
  provjera ispravnosti; samo njihove metrike složenosti/duplikacije nisu lint dug.
  errcheck ne traži provjeru čišćenja čija greška nema što reći: `Rollback`
  nakon `Commit`, `Rows.Close` (greške iteracije javlja `rows.Err`),
  `Stmt.Close`, zatvaranje mrežne veze i učitane datoteke iz obrasca, te `Close`
  na kraju testa. Upisi, `Commit`, `Flush` i zatvaranje zapisane datoteke i dalje
  se provjeravaju.
- **gocyclo v0.6.0**: jedna izravna ovisnost zasebnog `dev/quality/go.mod`.
  Glavni `go.mod`/`go.sum` i aplikacijska arhitektura nisu mijenjani.
- Go parser/scanner i mali lokalni analizator daju coverage/CRAP, exact blokove,
  fuzzy kandidate i JSON. Nema Node/npm/jscpd ovisnosti.
- **Gremlins v0.6.0** samo za izričiti `quality-deep`, ne obični build.

`dev/quality/` je verzionirana razvojna infrastruktura, za razliku od lokalnih
administratorskih/uvoznih skripti u ignoriranom `tools/`. `go test ./...` glavnog
modula ne uključuje odvojeni quality modul; zato ga `make quality` zasebno testira.

## Metrike i pragovi

| Metrika | Cilj / kategorije |
|---|---|
| Coverage | Ukupno ≥85%; kritično ≥95% |
| CC | ≤6 GOOD; 7–10 WARNING; 11–15 HIGH; >15 zahtijeva pregled |
| CRAP | ≤6 EXCELLENT; (6,10] GOOD; (10,15] WARNING; (15,30] HIGH; >30 FAIL cilj |
| Kritične funkcije | CC ≤10 i CRAP ≤10; bez statičkih grešaka |
| Exact duplikacija | <2% GOOD; [2,5)% WARNING; ≥5% HIGH |
| Mutation efficacy | Kritični odabrani paket ≥80%, početno upozorenje |

CRAP je:

```text
CRAP = CC² × (1 − coverage)³ + CC
coverage je u toj formuli udio 0–1, a ne postotak 0–100.
```

Primjer: CC=10 uz coverage=50% daje CRAP=22,5; uz 100% daje 10.
Visok CC ostaje vidljiv čak i kada se svi retci izvrše u testu.

Coverage je **statement coverage**, ne branch coverage. Ukupni i paketni rezultat
ponderirani su brojem naredbi, a ne prosjekom postotaka funkcija. Zadano Go
mjerenje pripisuje coverage testiranom paketu (bez `-coverpkg=./...`).
Funkcijski coverage dobiva se pridruživanjem Go blokova rasponu funkcije; tijela
anonimnih funkcija ulaze u roditeljsku funkciju, kao i u gocyclo. Funkcije bez
izvršivih naredbi imaju coverage/CRAP `null`, a ne izmišljenu stopostotnu pokrivenost.

CC i klonovi obuhvaćaju imenovane funkcije/metode aktivne platforme iz `go list`,
bez testova i standardno označenog generiranog koda. Generirani kod ostaje u
standardnom ukupnom Go coverageu. Globalni inicijalizatori nisu zasebne funkcije.
Identitet funkcije je `relativna/datoteka.go::(*Tip).Metoda`, ne broj retka.
Premještanje/preimenovanje zato se konzervativno smatra novom funkcijom; pregledom
se odlučuje treba li obrazložena iznimka.

## Kritični kod

`quality/config.json` sadrži regex popis `critical` koji odgovara identitetima
funkcija. Može obuhvatiti paket, datoteku ili jednu metodu. Početni odabir pokriva
arhivu/sinkronizaciju, uvoze, prognozu, obradu očitanja i pragova, autentikaciju,
korisničke ovlasti i zajednički `authMiddleware`. Nije tvrdnja da su time pronađeni
svi sigurnosno važni tokovi; popis se proširuje s novim odgovornostima.

Primjeri dodatnog unosa u JSON-u:

```json
"^internal/novi_kriticni_paket/"
"^internal/models/models\\.go::\\(Station\\)\\.CalculateDefensePhase$"
```

Sve odabrane funkcije označene su `critical: true` u JSON-u. Njihov zajednički
coverage ponderiran je naredbama; stroži prag vrijedi i za svaku novu funkciju,
ne samo za prosjek skupine.

## Regresija nije isto što i apsolutni cilj

Odmah blokiraju neuspjela izgradnja/testovi, utrka, nedostupni alati, nevaljani
izvještaji, neusporediv baseline te govet/typecheck i staticcheck SA problemi.
SA1019 (zastarjeli API) ostaje vidljivo upozorenje; nova uporaba takvog API-ja
ipak je novi lint nalaz i regresija. Informativne lokacije istog SA nalaza ne
broje se kao dodatne greške. Test ne dokazuje odsutnost svih mogućih utrka.

Za postojeći dug dopušten je **samo zatečeni opseg**:

- Ukupni, kritični i coverage pojedine postojeće funkcije smije pasti najviše
  **0,1 postotni bod** (podesivo). Nova funkcija cilja 85%, kritična 95%.
- CC ne smije prijeći `max(stari CC, cilj)`; CRAP ne smije prijeći
  `max(cilj, stari CRAP + 0,5)`. Prate se pojedine funkcije, tako da mnogo novih
  jednostavnih funkcija ne može sakriti pogoršanje jedne velike.
- Nepromijenjena funkcija (isti broj tokena, naredbi i CC kao u baselineu)
  ne uspoređuje se po coverageu ni CRAP-u: razlika tada dolazi od testova koji
  ovise o vremenu, a ne od promjene. Ukupni i kritični coverage i dalje se
  uspoređuju, a nestabilan test treba popraviti (npr. test koji grane pogađa
  namjerno, a ne slučajno).
- Exact udio ne smije narasti za više od **0,1 postotni bod**.
- Novi/dodatni lint nalaz blokira i kada je riječ o linteru kategorije warning;
  ključ je linter + datoteka + poruka, s brojem pojavljivanja, bez broja retka.
  Metrički linteri (gocyclo, gocognit, maintidx, nestif) u poruku upišu izmjerenu
  vrijednost; u ključu je ona zamijenjena znakom `#`, pa smanjenje složenosti
  iste funkcije nije „novi nalaz”, a rast hvata usporedba CC/CRAP po funkciji.
- Iznimke iz `quality/config.json` ne ulaze u otisak konfiguracije: mijenjaju
  propusnicu, ne mjerenje, pa nova iznimka ne čini baseline neusporedivim.
  Istekla iznimka i dalje ruši provjeru.
- Fuzzy je samo izvještaj/upozorenje, nikada hard gate.

Prosjeci i maksimumi ostaju u JSON-u za kasnije trendove. Uz prosjek CRAP-a
izvještaj daje medijan, P90, P95 i broj funkcija po razredima (≤ 10, 10–30,
30–100, > 100): prosjek podigne i jedna golema funkcija, a medijan i percentili
pokazuju je li loš cijeli kod ili rep netestiranih funkcija. To je samo
izvještaj, ne prag. Nema automatskog
refaktoriranja ni automatskog ažuriranja baselinea nakon crvene provjere.
Mali/nedeterministični testovi mogu uzrokovati promjenu coveragea: prvo ponoviti
iste izvore i popraviti nestabilan test, ne proizvoljno sniziti cilj.

## Baseline i iznimke

Početna snimka: [izvještaj](CODE_QUALITY_BASELINE.md), podaci u
[`quality/baseline.json`](../quality/baseline.json). Izmjerena je čista verzija
`f5adb84` (0.0.31), jer su u zajedničkoj radnoj kopiji istodobno nastajale
nepovezane izmjene za 0.0.32. Ne predstavlja verifikaciju tih nezavršenih izmjena.

Za namjerno pomicanje referentne točke, nakon pregleda:

```sh
make quality-baseline ACCEPT_BASELINE=yes
git diff -- quality/baseline.json
```

Potrebna je čista verzionirana kopija. Baseline je **zapis mjerenja, ne potvrda da
je aplikacija bez grešaka**: može sadržavati postojeće SA greške i status FAIL.
Snimanje takvog baselinea ne daje im amnestiju; normalni `make quality` i dalje
vraća grešku. Neuspjele testove/race ili nepotpuno mjerenje nije moguće snimiti
kao valjan baseline. Svaku obnovu opisati i pregledati u PR-u, zajedno s promjenom
top funkcija i razloga zbog kojeg je obnova opravdana.

Neusporedivi su različiti OS/arhitektura, Go verzija, verzija analizatora i hash
konfiguracije. Nakon promjene pravila ili alata **ponovno izmjeriti prethodni
baseline commit novim pravilima**, pa usporediti novi kod. Ne prihvatiti novi kod
kao vlastiti baseline. Za mjerenje čiste odvojene kopije:

```sh
cd dev/quality
go run . -root /put/do/ciste/kopije -policy ../.. \
  -out /put/do/izvjestaja -baseline /put/do/baseline.json -record-baseline
```

Samo za ponovnu analizu istog uspješnog lokalnog testa može se dodati
`-reuse-tests`. Alat provjerava otiske izvora, profila, loga, Go i platforme;
jasno ispisuje da testovi nisu ponovno pokrenuti. Obični `make quality` i CI
ne koriste tu opciju. Nije potpisani dokaz protiv zlonamjerne izmjene artefakta.

Iznimke su u `exceptions`: točan ključ iz izvještaja, razlog, odgovorna osoba i
datum isteka. Nema wildcarda. Istekla iznimka blokira. Početno su izuzeta samo
dva namjerna testna obrasca: izazivanje panike nil mapom i usporedba dva poziva
tvorničke metode koja mora vratiti različite HTTP klijente. Ne dodavati iznimku
samo da bi CI bio zelen; posebno ne za stvarne greške autorizacije/uvoza.

## Duplikacija: što točno mjerimo

Tri različita signala, koji se ne zbrajaju:

1. **Exact**: identični leksički blokovi od najmanje 100 Go tokena unutar tijela
   funkcija; imena i literali se čuvaju, komentari/razmaci zanemaruju. Postotak
   je unija svih ponovljenih tokena, uključujući originale, podijeljena svim
   tokenima tijela funkcija. Preklapanja se ne broje dvaput. Nije postotak redaka
   cijelog repozitorija, tablica podataka, predložaka ili testova.
2. **dupl**: normalizirani AST blokovi (prag 150), prikazani kao lint nalazi.
   To nije potpuno tekstualna jednakost pa ga ne nazivamo exact postotkom.
3. **Fuzzy**: tijela funkcija s najmanje 80 tokena; lokalni identifikatori
   normalizirani po prvom pojavljivanju, selektori i literali sačuvani; Jaccard
   sličnost skupova od 5 uzastopnih tokena ≥0,85. Postotak je udio tokena u
   cijelim funkcijama označenim kao kandidati. Ne mjeri identične fragmente
   kraćih funkcija niti dokazuje da dva algoritma imaju istu semantiku.

Exact i fuzzy imaju različite jedinice obuhvata, pa fuzzy može biti manji od
exacta. JSON čuva do 200 primjera parova po vrsti; postotak obuhvaća sve pronađene
kandidate. Prije spajanja koda provjeriti zajedničku odgovornost, ne samo izgled.

## Mutation testing (opcijski, pripremljen)

Gremlins može vrlo dugo raditi na velikom modulu. Ne ulazi u uobičajeni CI.
Naredba zahtijeva **odvojenu čistu Git kopiju**, izvan živog projekta, te jedan
paket (`MUTATION_PACKAGE`). Ne pokretati u radnoj kopiji drugih agenata.

```sh
# Prvo make quality nad istim čistim commitom, zatim pripremiti zasebnu kopiju.
make quality-deep MUTATION_ROOT=/put/do/odvojene/ciste/kopije \
  MUTATION_PACKAGE=internal/models
```

Rezultat: `mutation.json`, `mutation.log`, te `mutation_score` u postojećem
`code-health.json` samo ako commit odgovara mjerenju. `mutation_score` označava
Gremlins **test efficacy = KILLED/(KILLED+LIVED)** za taj paket, ne za cijelu
aplikaciju. Nepokrivene/neizvedive mutante treba dodatno pregledati u sirovom
izvještaju; 80% efficacy nije isto što i 80% ukupne pokrivenosti mutanata.

Ova integracija je pripremljena, **nije potvrđena izvršenim mutation prolazom**.
Lokalno je preuzimanje Gremlinsa zapelo na provjeri TLS certifikata Go proxyja;
provjera certifikata nije isključivana. Treba potvrditi njegov rad s Go 1.27.1
prije obveznog uključivanja. Početni score zato ostaje `null / NOT_RUN`.

## CI

`.github/workflows/provjera.yml` zadržava postojeći govulncheck, a testni posao
pokreće quality gate. Linux ponovno mjeri isti prihvaćeni commit sa svojim Go
toolchainom i konfiguracijom, te kešira taj referentni JSON. Ključ keša uključuje
platformu, Go, baseline i izvore analizatora. Tek zatim uspoređuje aktualni kod.
Prvi prolaz traje dulje jer testira dvije verzije. Izvještaji se prilažu i pri
padu provjere; nemaju pristup lokalnim produkcijskim bazama.

Workflow je pripremljen u repozitoriju; njegovo izvršavanje na GitHub runneru
treba potvrditi nakon slanja. Ne treba vanjska usluga ni slanje izvora trećem
pružatelju analize. Artefakti i povijest baselinea omogućuju kasnije trendove;
grafički dashboard i automatski trend storage nisu dio ove prve faze.

## Pravilo za ljude i AI agente

Mijenjati najmanji smisleni dio, dodati test koji provjerava ponašanje, pokrenuti
gate i pregledati razliku prema baselineu. Ne dijeliti funkcije radi same brojke,
ne gasiti lintere, ne smanjivati pragove i ne osvježavati baseline bez objašnjenja.
Kod obrane od poplava važniji su ispravna odluka, audit i čitljivost nego prosjek.

Izvori: [golangci konfiguracija](https://golangci-lint.run/docs/configuration/file/),
[gocyclo](https://github.com/fzipp/gocyclo),
[Gremlins naredbe i JSON](https://gremlins.dev/latest/usage/commands/unleash/),
[Gremlins ograničenja](https://github.com/go-gremlins/gremlins/tree/v0.6.0).
