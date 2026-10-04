# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-korisnici`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu (Linux, kao CI). Nacrt PR-a nije otvoren.
- **„Sumnjivo ponašanje” nije dovršeno.** Ispod su tragovi zabilježeni tijekom rada. Prije PR-a treba ih provjeriti u kodu i dopuniti (`datoteka:redak`, što se događa, kako treba), a tablicu testova dopuniti opisom što svaki test tvrdi.

## Što preostaje
1. Pokrenuti `make quality` za granu (postupak u `PREDAJA.md` na `stabilizacija-plan`) i provjeriti da nema regresija. Nikad ne prihvaćati novi baseline i ne dodavati iznimke.
2. Dovršiti „Sumnjivo ponašanje” i opis testova.
3. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod.

## Nacrt opisa PR-a

### Testovi (`internal/service/korisnici_racun_test.go`)
| Test | Što tvrdi |
|---|---|
| `TestNoviRacunOdbijanja` | (dopuniti) |
| `TestNoviRacun` | (dopuniti) |
| `TestIzmjenaVlastitogRacuna` | (dopuniti) |
| `TestIzmjenaTudjegRacuna` | (dopuniti) |
| `TestUkljucenjeRacunaSTudjomAdresom` | (dopuniti) |
| `TestZastavicaGlobalnogAdministratoraPriIzmjeni` | (dopuniti) |
| `TestLozinkaOperateraPriIzmjeni` | (dopuniti) |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality` (Linux, go1.27.1). „Poslije” je coverage paketa `service` s grane (`go test -covermode=atomic`), a CRAP je izračunat istom formulom: CC² · (1 − cov)³ + CC. Navedene su samo funkcije kojima se coverage promijenio. `drugi_korak.go` je izostavljen jer mu coverage varira od pokretanja do pokretanja (vidi `PREDAJA.md` na `stabilizacija-plan`). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/auth_service.go:151` · `(*AuthService).Login` | 3 | 71.4 % | 85.7 % | 3.2 | 3.0 | da |
| `internal/service/user_rules.go:22` · `actorRank` | 5 | 83.3 % | 100.0 % | 5.1 | 5.0 | da |
| `internal/service/user_rules.go:243` · `normalizeScope` | 21 | 87.5 % | 90.6 % | 21.9 | 21.4 | da |
| `internal/service/user_service.go:133` · `(*UserService).CreateUser` | 24 | 84.7 % | 93.2 % | 26.0 | 24.2 |  |
| `internal/service/user_service.go:291` · `(adresaZauzeta).Error` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/user_service.go:393` · `(*UserService).UpdateUser` | 47 | 90.0 % | 93.0 % | 49.2 | 47.8 |  |

### Sumnjivo ponašanje (tragovi, za provjeru)
1. `ograniciRok` (rok privremene uprave) ograničava samo uloge iste razine: privremeni zamjenik na sektoru dužnost na razini sektora daje s rokom, a dužnost na razini područja trajno.
2. Privremena uprava organizacije skida zastavicu globalnog administratora, i stalnom administratoru: pravilo vrijedi samo za davanje (test oko retka 291).
3. Uprava sektora pri izmjeni tiho zadrži zatečenu zastavicu umjesto da odbije zahtjev (test oko retka 202).
4. Provjeriti `TestUkljucenjeRacunaSTudjomAdresom` (uključenje računa čija je adresa zauzeta) i `TestLozinkaOperateraPriIzmjeni`.
