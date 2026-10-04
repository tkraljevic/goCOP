# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-citanja`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu (Linux, kao CI). Nacrt PR-a nije otvoren.
- **„Sumnjivo ponašanje” nije dovršeno.** Ispod su tragovi zabilježeni tijekom rada. Prije PR-a treba ih provjeriti u kodu i dopuniti (`datoteka:redak`, što se događa, kako treba), a tablicu testova dopuniti opisom što svaki test tvrdi.

## Što preostaje
1. Pokrenuti `make quality` za granu (postupak u `PREDAJA.md` na `stabilizacija-plan`) i provjeriti da nema regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
2. Dovršiti „Sumnjivo ponašanje” i opis testova.
3. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod.

## Nacrt opisa PR-a

### Testovi (`internal/service/ocitanja_servis_test.go`)
| Test | Što tvrdi |
|---|---|
| `TestProvjeraOcitanja` | (dopuniti) |
| `TestUpisOcitanjaIPrava` | (dopuniti) |
| `TestPravoUpisaNaObjekt` | (dopuniti) |
| `TestIzmjenaIBrisanjeOcitanja` | (dopuniti) |
| `TestZalijepljenaOcitanja` | (dopuniti) |
| `TestPregledSvihLetvi` | (dopuniti) |
| `TestTerenskiPogled` | (dopuniti) |
| `TestTerenskiPogledVrijemeOkoPonoci` | (dopuniti) |
| `TestFazaZaOcitanje` | (dopuniti) |
| `TestPravoUpisaPrekoDionice` | (dopuniti) |
| `TestCitanjeOcitanja` | (dopuniti) |
| `TestPregledBezDijelaBaze` | (dopuniti) |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality` (Linux, go1.27.1). „Poslije” je coverage paketa `service` s grane (`go test -covermode=atomic`), a CRAP je izračunat istom formulom: CC² · (1 − cov)³ + CC. Navedene su samo funkcije kojima se coverage promijenio. `drugi_korak.go` je izostavljen jer mu coverage varira od pokretanja do pokretanja (vidi `PREDAJA.md` na `stabilizacija-plan`). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/reading_service.go:34` · `(*ReadingService).Get` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 | da |
| `internal/service/reading_service.go:38` · `(*ReadingService).List` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 | da |
| `internal/service/reading_service.go:42` · `(*ReadingService).Stats` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 | da |
| `internal/service/reading_service.go:46` · `hasAnyWriteRight` | 7 | 0.0 % | 100.0 % | 56.0 | 7.0 | da |
| `internal/service/reading_service.go:56` · `(*ReadingService).PrvoOcitanje` | 3 | 0.0 % | 100.0 % | 12.0 | 3.0 | da |
| `internal/service/reading_service.go:64` · `(*ReadingService).Krajnosti` | 3 | 0.0 % | 100.0 % | 12.0 | 3.0 | da |
| `internal/service/reading_service.go:71` · `(*ReadingService).CanRecordStation` | 10 | 0.0 % | 100.0 % | 110.0 | 10.0 | da |
| `internal/service/reading_service.go:99` · `(*ReadingService).CanRecordStructure` | 11 | 90.0 % | 100.0 % | 11.1 | 11.0 | da |
| `internal/service/reading_service.go:120` · `(*ReadingService).CanEdit` | 11 | 0.0 % | 100.0 % | 132.0 | 11.0 | da |
| `internal/service/reading_service.go:143` · `(*ReadingService).validate` | 25 | 0.0 % | 100.0 % | 650.0 | 25.0 | da |
| `internal/service/reading_service.go:189` · `(*ReadingService).Create` | 14 | 0.0 % | 100.0 % | 210.0 | 14.0 | da |
| `internal/service/reading_service.go:231` · `(*ReadingService).Update` | 5 | 0.0 % | 100.0 % | 30.0 | 5.0 | da |
| `internal/service/reading_service.go:250` · `(*ReadingService).Delete` | 4 | 0.0 % | 100.0 % | 20.0 | 4.0 | da |
| `internal/service/reading_service.go:266` · `(*ReadingService).Overview` | 21 | 0.0 % | 96.5 % | 462.0 | 21.0 | da |
| `internal/service/reading_service.go:344` · `fill` | 3 | 0.0 % | 100.0 % | 12.0 | 3.0 | da |
| `internal/service/reading_service.go:358` · `(*ReadingService).PhaseFor` | 3 | 0.0 % | 100.0 % | 12.0 | 3.0 | da |
| `internal/service/reading_service.go:379` · `(*ReadingService).FieldOverview` | 34 | 0.0 % | 98.3 % | 1190.0 | 34.0 | da |
| `internal/service/reading_service.go:477` · `(*ReadingService).UveziZalijepljena` | 5 | 0.0 % | 100.0 % | 30.0 | 5.0 | da |
| `internal/service/section_service.go:33` · `(*SectionService).ListSections` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/section_service.go:38` · `(*SectionService).GetSectionWithDetails` | 4 | 77.8 % | 88.9 % | 4.2 | 4.0 |  |
| `internal/service/user_service.go:835` · `(*UserService).ListAreas` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |

### Sumnjivo ponašanje (tragovi, za provjeru)
1. `ListAreas` pada kad je `areas.subcenter` NULL. Okolina testa zato upisuje `subcenter ''`. *Treba:* čitati NULL kao prazno.
2. `FieldOverview` ne provjerava zadano područje prema izboru područja osobe: Pero Perić vidi područje 2 iako ga nema u izboru (test oko retka 549). *Treba:* odbiti ili vratiti na dopušteno područje.
3. Izmjena očitanja tiho zanemaruje letvu, podrijetlo, oznaku izvora i autora iz zahtjeva (test oko retka 333). Treba provjeriti je li to namjera ili bi trebalo odbiti.
4. `splitNote` ostavlja zagrade u bilješci „(voda mutna).”.
5. Pregled bez dijela baze vraća samo grešku, bez djelomičnog popisa (vjerojatno u redu; `TestPregledBezDijelaBaze`).
