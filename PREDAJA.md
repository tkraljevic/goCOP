# PREDAJA — stabilizacija (privremena datoteka, ukloniti prije otvaranja PR-a)

Rad je prekinut zbog budžeta. Ova datoteka je za agenta koji nastavlja. Uz nju idu `PREDAJA-opis-PR.md` (nacrt opisa PR-a za fazu 1) i `PREDAJA-faza3.md` (kandidati za fazu 3). Svaka grana faze 2 ima svoj `PREDAJA.md`.

## Pravila vlasnika (Tomislav Kraljević)
- Svaki PR je NACRT i ne spaja se. Bez AI potpisa (ni u commitu, ni u PR-u, ni u komentaru). Sve na hrvatskom.
- Commitovi idu kao `Tomislav Kraljević <78041730+tkraljevic@users.noreply.github.com>`, s postavljenim GIT_AUTHOR_* i GIT_COMMITTER_*.
- Izmišljeni podaci smiju biti samo „Pero Perić” / `pperic` (sektori, područja i letve su također izmišljeni).
- NE DIRATI: `cmd/gocop/main.go`, `internal/repository/apply.go`, `dev/quality`, `quality/`, `.golangci.yml`. U `internal/peers` i `internal/razmjena` smiju se samo dodavati testovi.
- Prije svake nove grane povući master. Jedno područje po PR-u.
- `make quality` mora proći bez regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
- Test mora tvrditi ponašanje. Ako test otkrije grešku, ne popravljati je tiho: zapisati je pod „Sumnjivo ponašanje”. Očit i mali popravak ide u zaseban commit s testom koji ga pokazuje.
- U opisu svakog PR-a: popis testova, tablica prije/poslije za dirane funkcije (CC, coverage, CRAP) i „Sumnjivo ponašanje”.
- Faza 2: rubni slučajevi, odbijanja i neispravan unos; cilj po funkciji 70 % → 85 %, a za kritične (`quality/config.json`) 95 %; bez refaktoriranja.
- Faza 3: funkcije s CRAP > 30 i coverageom < 80 % (`PREDAJA-faza3.md`). Prvo testovi. Preuređenje se predlaže tek ako CC/CRAP i dalje ostane visok, i ide u zaseban commit bez promjene ponašanja. Ako podjela samo spušta brojku, a otežava čitanje, napisati da je ne treba raditi.

## Stanje grana

| Grana | Područje | Stanje |
|---|---|---|
| `stabilizacija-plan` (PR #4) | faza 1: testovi koji zaključavaju ponašanje, `docs/STABILIZACIJA.md` | testovi gotovi; tablica metrika u `docs/STABILIZACIJA.md` (`<!-- METRIKE -->`) i u `PREDAJA-opis-PR.md` (`__TABLICA__`) nije upisana; PR nije otvoren |
| `stabilizacija-citanja` (PR #6) | ReadingService | testovi, opis i „Sumnjivo ponašanje” gotovi; popravak NaN-a u zasebnom commitu |
| `stabilizacija-dionice` (PR #7) | SectionService.SaveSection | testovi, opis i „Sumnjivo ponašanje” gotovi |
| `stabilizacija-dnevnici` (PR #8) | JournalService (Obracun, SpremiDezurstvo) | testovi, opis i „Sumnjivo ponašanje” gotovi; popravak kraja dana u zasebnom commitu |
| `stabilizacija-izvjesca` (PR #9) | IzvjescaService (Spremi, SpremiSektorsko) | testovi, opis i „Sumnjivo ponašanje” gotovi; popravak prepisivanja tuđeg izvješća u zasebnom commitu |
| `stabilizacija-korisnici` (PR #10) | UserService (CreateUser, UpdateUser) | testovi, opis i „Sumnjivo ponašanje” gotovi; popravak skidanja zastavice u zasebnom commitu |
| `stabilizacija-vodocuvar` (PR #5) | VodocuvarService.Spremi | testovi i jedan popravak gotovi; opis PR-a gotov |
| `stabilizacija-mts` (PR #11) | MtsService.Provedi | testovi gotovi (81,5 % → 95,2 %), opis u `PREDAJA.md` grane |
| — | AktService (primatelji, zakljuciOvjeru) | nije započeto (master: `primatelji` CC 52, 0 %, CRAP 2756; `zakljuciOvjeru` CC 19, 0 %, CRAP 380; obje kritične) |
| — | faza 3 | nije započeto; popis u `PREDAJA-faza3.md` |

Opis svake grane faze 2 je u njezinu `PREDAJA.md`: kad `make quality` prođe, sadržaj ide u opis PR-a, a datoteka se briše. Na nijednoj grani `make quality` još nije izmjeren. Prije svakog PR-a treba ga pokrenuti prema baselineu.

## Mjerenje na Linuxu (kao CI)
CI ponovno mjeri baseline commit `f5adb84` trenutnim alatom pa uspoređuje. Lokalno:

```sh
make quality-tools                       # golangci-lint v2.14.0 u bin/quality
git worktree add ../baseline-f5adb84 f5adb84
cd dev/quality
go run . -root ../../../baseline-f5adb84 -policy ../.. -out /tmp/q-baseline \
  -baseline /tmp/linux-baseline.json -record-baseline
cd ../.. && make quality QUALITY_BASELINE=/tmp/linux-baseline.json
```

Izmjereno 4. 10. 2026. (linux/amd64, go1.27.1, tim alatom):
- baseline `f5adb84`: coverage 51,79 %, kritični 50,43 %, 3 greške / 1009 upozorenja, status FAIL (kao i na macOS-u), oko 13 minuta;
- master `713d9df`: coverage 52,52 %, kritični 52,54 %, 0 grešaka / 1005 upozorenja, status BASELINE_DEBT, oko 17,5 minuta.

Jedan prolaz traje 15–20 minuta.

## Nalaz: coverage `drugi_korak.go` nije stabilan
U paketu `service` coverage nekih kritičnih funkcija u `internal/service/drugi_korak.go` mijenja se od pokretanja do pokretanja, bez promjene koda i testova: `rezervirajUnos` (88,9 % ↔ 77,8 %), `ProvjeriKod` (74–82 %), `JaviPromjenuAdrese` i `posalji`. `make quality` zato može javiti lažnu regresiju kritičnog coveragea. Treba naći test koji ovisi o vremenu ili redoslijedu i učiniti ga determinističkim. Baseline se ne prihvaća.

## Savjeti za testove u paketu `service`
- Postojeća imena u paketu `service`/`service_test` ne smiju se ponoviti, pa nova imena treba nazvati po području (npr. `vd…`, `okolinaVodocuvara`). Zauzeta su, među ostalima:
  - faza 1: `pperic` (konstanta), `sektorPtr`, `ocekujOdbijeno`, `okolinaObrane`/`novaOkolinaObrane`, `okolinaAkta`/`novaOkolinaAkta`;
  - master: `strp`, `intp`, `permsWith`, `dioniceOf`, `sectorsOf`, `prag`, `pragCm`, `letvaBatina`, `dan`, `kolicinaU`, `noviCvorZaOvjeru`, `pripremiMts`, `setupTestTerritoryService`, `strPtr`, `vrijemeProbe`, `sektorP`, `podrucjeP`, `novaOkolinaOvlasti`, `novaOkolinaOporavka`.
- Grane faze 2 su neovisne, pa i među njima treba izbjegavati iste nazive na razini paketa.
- Greška baze izaziva se s `ALTER TABLE x RENAME TO nema_x` (DROP pada na FK) ili okidačem `CREATE TRIGGER … BEGIN SELECT RAISE(ABORT, '…'); END`.
- `areas.subcenter` mora biti `''`, ne NULL. Šifra dionice mora odgovarati `^[A-F]\.\d{1,2}\.\d{1,3}$`.
- Lint samo novog koda: `bin/quality/golangci-lint run --new-from-rev=origin/master ./internal/service/...`.
