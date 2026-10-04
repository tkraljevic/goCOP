# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-dionice`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu (Linux, kao CI). Nacrt PR-a nije otvoren.
- **„Sumnjivo ponašanje” nije dovršeno.** Ispod su tragovi zabilježeni tijekom rada. Prije PR-a treba ih provjeriti u kodu i dopuniti (`datoteka:redak`, što se događa, kako treba), a tablicu testova dopuniti opisom što svaki test tvrdi.

## Što preostaje
1. Pokrenuti `make quality` za granu (postupak u `PREDAJA.md` na `stabilizacija-plan`) i provjeriti da nema regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
2. Dovršiti „Sumnjivo ponašanje” i opis testova.
3. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod.

## Nacrt opisa PR-a

### Testovi (`internal/service/dionice_spremanje_test.go`)
| Test | Što tvrdi |
|---|---|
| `TestNovaDionicaUlaz` | (dopuniti) |
| `TestNovaDionicaUpis` | (dopuniti) |
| `TestPravoNaNovuDionicu` | (dopuniti) |
| `TestSifraDioniceNeProvjeravaPodrucje` | (dopuniti) |
| `TestIzmjenaDionice` | (dopuniti) |
| `TestNoveVezeDionice` | (dopuniti) |
| `TestPravaNaDionicu` | (dopuniti) |
| `TestSljedecaSifraDionice` | (dopuniti) |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality` (Linux, go1.27.1). „Poslije” je coverage paketa `service` s grane (`go test -covermode=atomic`), a CRAP je izračunat istom formulom: CC² · (1 − cov)³ + CC. Navedene su samo funkcije kojima se coverage promijenio. `drugi_korak.go` je izostavljen jer mu coverage varira od pokretanja do pokretanja (vidi `PREDAJA.md` na `stabilizacija-plan`). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/section_service.go:33` · `(*SectionService).ListSections` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/section_service.go:38` · `(*SectionService).GetSectionWithDetails` | 4 | 77.8 % | 88.9 % | 4.2 | 4.0 |  |
| `internal/service/section_service.go:56` · `(*SectionService).CanEditSection` | 9 | 90.9 % | 100.0 % | 9.1 | 9.0 |  |
| `internal/service/section_service.go:80` · `(*SectionService).CanCreateSectionInArea` | 9 | 88.9 % | 100.0 % | 9.1 | 9.0 |  |
| `internal/service/section_service.go:98` · `(*SectionService).SaveSection` | 21 | 77.8 % | 100.0 % | 25.8 | 21.0 |  |
| `internal/service/section_service.go:169` · `provjeriNoveVeze` | 18 | 88.9 % | 100.0 % | 18.4 | 18.0 |  |
| `internal/service/section_service.go:217` · `validateParts` | 18 | 61.1 % | 100.0 % | 37.1 | 18.0 |  |
| `internal/service/section_service.go:254` · `(*SectionService).SljedecaSifra` | 5 | 0.0 % | 100.0 % | 30.0 | 5.0 |  |

### Sumnjivo ponašanje (tragovi, za provjeru)
1. Šifra dionice ne provjerava se prema sektoru i području u koje se dionica sprema (`TestSifraDioniceNeProvjeravaPodrucje`).
2. Rukovoditelj dionice koji uređuje svoju dionicu i pošalje drugo područje ili sektor: izmjena prolazi, a područje i sektor tiho ostaju zatečeni (test oko retka 228). *Treba:* odbiti ili javiti.
