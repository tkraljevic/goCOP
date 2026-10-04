# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-korisnici`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod mijenjan je samo u zasebnom commitu s popravkom (točka 2 pod „Sumnjivo ponašanje”).
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
| `TestNoviRacunOdbijanja` | Bez ovlasti i promatrač ne otvaraju račun. Zastavicu globalnog administratora ne daju uprava sektora ni privremena uprava države („stalna uprava”). Uprava sektora ne otvara račun bez dužnosti ni dužnost u tuđem sektoru. Odbijaju se: prazna lozinka, ime od razmaka, zauzeto ime (bez obzira na velika i mala slova), zauzeta adresa (s razmacima i velikim slovima) i nepostojeća dionica. |
| `TestNoviRacun` | Ime i puno ime se čiste, račun je uključen i traži zamjenu lozinke, a dužnost dobiva naziv iz uloge te sektor iz područja i primarna je. Novim računom prijava prolazi. Privremena uprava sektora upravu sektora daje s rokom do svog isteka, a upravu područja trajno. Globalni administrator otvara račun bez dužnosti, sa zastavicom. Zadani naziv dužnosti ostaje. |
| `TestIzmjenaVlastitogRacuna` | Osoba na sebi ne mijenja korisničko ime, uključenost ni zastavicu (tiho se zadržava zatečeno), a ostala polja da. Vlastita nova lozinka ne traži zamjenu. Nepostojeći račun daje ErrUserNotFound. |
| `TestIzmjenaTudjegRacuna` | Uprava drugog područja ne uređuje. Uprava sektora mijenja ime i titulu, a zastavicu tiho zadržava zatečenu. Tuđe ime (i drugim slovima), prazno ime i tuđa adresa se odbijaju. Lozinka koju upiše uprava traži zamjenu i gasi otvorene prijave. |
| `TestUkljucenjeRacunaSTudjomAdresom` | Isključen račun čiju je adresu u međuvremenu dobio drugi aktivni račun ne uključuje se (ErrAdresaZauzeta, „račun se ne uključuje”). S drugom adresom se uključuje. |
| `TestZastavicaGlobalnogAdministratoraPriIzmjeni` | Privremena uprava organizacije zastavicu ne daje i ne skida (popravak), a stalna je daje i skida. |
| `TestLozinkaOperateraPriIzmjeni` | Uprava sektora uređuje operatera, ali mu ne postavlja lozinku (ErrUnauthorized), i odbijena lozinka nije upisana. |

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

### Sumnjivo ponašanje
1. **`internal/service/user_rules.go:90` (`ograniciRok`), poziv u `user_service.go:228`: rok privremene uprave vrijedi samo za dužnosti na njezinoj razini.** Privremeni zamjenik rukovoditelja sektora upravu sektora daje s rokom do svog isteka, a upravu područja (razinu niže) trajno (`TestNoviRacun`). Kad mu istekne ovlast, dužnosti koje je dao ostaju zauvijek. *Treba:* svaku dužnost koju dodijeli privremena uprava ograničiti njezinim rokom, ili to izričito potvrditi kao pravilo.
2. **`internal/service/user_service.go:464–467`: privremena uprava organizacije skida zastavicu globalnog administratora, i stalnom administratoru.** Provjera vrijedi samo za davanje (`TestZastavicaGlobalnogAdministratoraPriIzmjeni`). Privremeni zamjenik tako je mogao stalnoj upravi oduzeti administraciju. **Popravljeno** u zasebnom commitu: zastavicu daje i skida samo stalna uprava organizacije.
3. **`internal/service/user_service.go:432` i `:435`: zabranjena promjena zastavice (i, za vlastiti račun, imena i uključenosti) tiho se zanemaruje.** Zahtjev prolazi bez poruke (`TestIzmjenaTudjegRacuna`, `TestIzmjenaVlastitogRacuna`). To nije greška u pravima, ali onaj tko je zahtjev poslao misli da je promjena upisana. *Treba:* odbiti zahtjev ili javiti što nije promijenjeno.

### Otvorena pitanja
- Je li namjera da privremena uprava daje trajne dužnosti razinu niže (točka 1)?
