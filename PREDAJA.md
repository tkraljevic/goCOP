# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-izvjesca`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu (Linux, kao CI). Nacrt PR-a nije otvoren.
- **„Sumnjivo ponašanje” nije dovršeno.** Ispod su tragovi zabilježeni tijekom rada. Prije PR-a treba ih provjeriti u kodu i dopuniti (`datoteka:redak`, što se događa, kako treba), a tablicu testova dopuniti opisom što svaki test tvrdi.

## Što preostaje
1. Pokrenuti `make quality` za granu (postupak u `PREDAJA.md` na `stabilizacija-plan`) i provjeriti da nema regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
2. Dovršiti „Sumnjivo ponašanje” i opis testova.
3. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod.

## Nacrt opisa PR-a

### Testovi (`internal/service/izvjesca_rubovi_test.go`)
| Test | Što tvrdi |
|---|---|
| `TestDnevnoIzvjesceOdbijanja` | (dopuniti) |
| `TestDnevnoIzvjescePredanoIzmjena` | (dopuniti) |
| `TestDnevnoIzvjesceTudjimIdentitetom` | (dopuniti) |
| `TestPredlozakDnevnogIzvjesca` | (dopuniti) |
| `TestPravaNaIzvjesca` | (dopuniti) |
| `TestSektorskoIzvjesceOdbijanja` | (dopuniti) |
| `TestSektorskoIzvjesceIzmjenaIPredaja` | (dopuniti) |
| `TestPregledSektoraStadijVodotoka` | (dopuniti) |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality` (Linux, go1.27.1). „Poslije” je coverage paketa `service` s grane (`go test -covermode=atomic`), a CRAP je izračunat istom formulom: CC² · (1 − cov)³ + CC. Navedene su samo funkcije kojima se coverage promijenio. `drugi_korak.go` je izostavljen jer mu coverage varira od pokretanja do pokretanja (vidi `PREDAJA.md` na `stabilizacija-plan`). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/izvjesca_service.go:37` · `(*IzvjescaService).SmijePisati` | 4 | 66.7 % | 100.0 % | 4.6 | 4.0 |  |
| `internal/service/izvjesca_service.go:46` · `(*IzvjescaService).SmijeVidjeti` | 8 | 0.0 % | 100.0 % | 72.0 | 8.0 |  |
| `internal/service/izvjesca_service.go:62` · `(*IzvjescaService).Dionica` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/izvjesca_service.go:71` · `(*IzvjescaService).Predlozak` | 11 | 81.0 % | 95.2 % | 11.8 | 11.0 |  |
| `internal/service/izvjesca_service.go:105` · `(*IzvjescaService).vodostajiU7` | 14 | 10.3 % | 100.0 % | 155.2 | 14.0 |  |
| `internal/service/izvjesca_service.go:148` · `razmak` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/izvjesca_service.go:174` · `(*IzvjescaService).Spremi` | 21 | 72.2 % | 91.7 % | 30.5 | 21.3 |  |
| `internal/service/izvjesca_service.go:230` · `(*IzvjescaService).Predaj` | 8 | 71.4 % | 92.9 % | 9.5 | 8.0 |  |
| `internal/service/izvjesca_service.go:253` · `(*IzvjescaService).Obrisi` | 8 | 70.0 % | 90.0 % | 9.7 | 8.1 |  |
| `internal/service/izvjesca_service.go:285` · `(*IzvjescaService).DioniceZaPisanje` | 6 | 0.0 % | 90.0 % | 42.0 | 6.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:31` · `(*IzvjescaService).SmijeVidjetiSektor` | 15 | 53.8 % | 92.3 % | 37.1 | 15.1 |  |
| `internal/service/sektorsko_izvjesce_service.go:57` · `(*IzvjescaService).SektoriZaSastavljanje` | 7 | 0.0 % | 92.3 % | 56.0 | 7.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:85` · `(*IzvjescaService).PregledSektora` | 18 | 87.8 % | 95.9 % | 18.6 | 18.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:151` · `areaIzSifre` | 5 | 77.8 % | 100.0 % | 5.3 | 5.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:177` · `spojiTekst` | 3 | 40.0 % | 100.0 % | 4.9 | 3.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:332` · `(*IzvjescaService).otvoreniDnevnik` | 6 | 71.4 % | 85.7 % | 6.8 | 6.1 |  |
| `internal/service/sektorsko_izvjesce_service.go:348` · `(*IzvjescaService).PredlozakSektora` | 5 | 80.0 % | 86.7 % | 5.2 | 5.1 |  |
| `internal/service/sektorsko_izvjesce_service.go:372` · `(*IzvjescaService).SpremiSektorsko` | 21 | 66.7 % | 91.7 % | 37.3 | 21.3 |  |
| `internal/service/sektorsko_izvjesce_service.go:445` · `(*IzvjescaService).PredajSektorsko` | 7 | 64.3 % | 92.9 % | 9.2 | 7.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:468` · `(*IzvjescaService).ObrisiSektorsko` | 5 | 62.5 % | 87.5 % | 6.3 | 5.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:482` · `(*IzvjescaService).GetSektorsko` | 2 | 66.7 % | 100.0 % | 2.1 | 2.0 |  |
| `internal/service/sektorsko_izvjesce_service.go:489` · `(*IzvjescaService).ListSektorska` | 2 | 66.7 % | 100.0 % | 2.1 | 2.0 |  |

### Sumnjivo ponašanje (tragovi, za provjeru)
1. Provjeriti `TestDnevnoIzvjesceTudjimIdentitetom`: što se događa kad izvješće spremi osoba čiji identitet nije onaj u izvješću.
2. Provjeriti `TestDnevnoIzvjescePredanoIzmjena`: izmjena predanog izvješća.
