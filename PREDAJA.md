# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-dnevnici`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu (Linux, kao CI). Nacrt PR-a nije otvoren.
- **„Sumnjivo ponašanje” nije dovršeno.** Ispod su tragovi zabilježeni tijekom rada. Prije PR-a treba ih provjeriti u kodu i dopuniti (`datoteka:redak`, što se događa, kako treba), a tablicu testova dopuniti opisom što svaki test tvrdi.

## Što preostaje
1. Pokrenuti `make quality` za granu (postupak u `PREDAJA.md` na `stabilizacija-plan`) i provjeriti da nema regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
2. Dovršiti „Sumnjivo ponašanje” i opis testova.
3. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod.

## Nacrt opisa PR-a

### Testovi (`internal/service/dezurstva_obracun_test.go`)
| Test | Što tvrdi |
|---|---|
| `TestDezurstvoUpisSebe` | (dopuniti) |
| `TestDezurstvoGranice` | (dopuniti) |
| `TestDezurstvoPotvrdaIIzmjena` | (dopuniti) |
| `TestDezurstvoMicanje` | (dopuniti) |
| `TestObracunDezurstava` | (dopuniti) |
| `TestObracunJedneOsobe` | (dopuniti) |
| `TestPreuzimanjeIPredajaDezurstva` | (dopuniti) |
| `TestPreuzimanjeDezurstvaOdbijeno` | (dopuniti) |
| `TestPredajaDugogDezurstvaBezGranice` | (dopuniti) |
| `TestPredajaPrijePocetka` | (dopuniti) |
| `TestPredajaBezZapisaDnevnika` | (dopuniti) |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality` (Linux, go1.27.1). „Poslije” je coverage paketa `service` s grane (`go test -covermode=atomic`), a CRAP je izračunat istom formulom: CC² · (1 − cov)³ + CC. Navedene su samo funkcije kojima se coverage promijenio. `drugi_korak.go` je izostavljen jer mu coverage varira od pokretanja do pokretanja (vidi `PREDAJA.md` na `stabilizacija-plan`). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/dezurstva_service.go:31` · `(*JournalService).MozeSebeUPlan` | 7 | 37.5 % | 100.0 % | 19.0 | 7.0 |  |
| `internal/service/dezurstva_service.go:47` · `(*JournalService).BrojDezurstava` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/dezurstva_service.go:52` · `(*JournalService).Dezurstva` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/dezurstva_service.go:60` · `(*JournalService).SpremiDezurstvo` | 27 | 41.3 % | 97.8 % | 174.4 | 27.0 |  |
| `internal/service/dezurstva_service.go:131` · `(*JournalService).PotvrdiDezurstvo` | 7 | 0.0 % | 91.7 % | 56.0 | 7.0 |  |
| `internal/service/dezurstva_service.go:152` · `(*JournalService).MakniDezurstvo` | 9 | 0.0 % | 90.0 % | 90.0 | 9.1 |  |
| `internal/service/dezurstva_service.go:207` · `(*JournalService).Obracun` | 20 | 0.0 % | 97.1 % | 420.0 | 20.0 |  |
| `internal/service/dezurstva_service.go:333` · `(IORSRedak).Sat` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/dezurstva_service.go:336` · `(IORS).Obr` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/dezurstva_service.go:344` · `(IORS).Sat` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/dezurstva_service.go:352` · `(*JournalService).ObracunOsobe` | 13 | 0.0 % | 97.5 % | 182.0 | 13.0 |  |
| `internal/service/dezurstva_service.go:411` · `(*JournalService).PlanoviOsobe` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/dezurstva_service.go:423` · `(IORSRedak).PoRazredima` | 3 | 0.0 % | 100.0 % | 12.0 | 3.0 |  |
| `internal/service/dezurstva_service.go:440` · `(*JournalService).PreuzmiDezurstvo` | 10 | 0.0 % | 94.4 % | 110.0 | 10.0 |  |
| `internal/service/dezurstva_service.go:474` · `(*JournalService).PredajDezurstvo` | 6 | 0.0 % | 100.0 % | 42.0 | 6.0 |  |
| `internal/service/dezurstva_service.go:490` · `(*JournalService).zakljuciDezurstvo` | 5 | 0.0 % | 92.3 % | 30.0 | 5.0 |  |
| `internal/service/dezurstva_service.go:517` · `satiTekst` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |

### Sumnjivo ponašanje (tragovi, za provjeru)
1. Dežurstvo zaboravljeno tri dana predaja upiše kao jedan razmak od 72 sata, iako ručni upis ne prima dulje od 36 sati (`TestPredajaDugogDezurstvaBezGranice`).
2. Najmanje trajanje od 1 minute pri predaji vrijedi samo kad je kraj ≤ početak. Trenutna predaja zato daje razmak od oko 1 ms.
3. `internal/service/journal_service.go:216` računa kraj dana kao početak + 24 sata. Na dan pomicanja sata to nije ponoć. Ista greška u vodočuvarskom dnevniku popravljena je na grani `stabilizacija-vodocuvar`.
