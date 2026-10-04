# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila, postupak mjerenja i stanje cijele stabilizacije opisani su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-dnevnici`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a `golangci-lint run --new-from-rev=origin/master` ne daje novih nalaza.
- Produkcijski kod mijenjan je samo u zasebnom commitu s popravkom kraja dana (točka 6 pod „Sumnjivo ponašanje”).
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
| `TestDezurstvoUpisSebe` | Vlastiti upis čeka potvrdu, mjesto dolazi iz opisa, a ime i napomena se obrezuju. Za drugoga se ne upisuje ni kad netko radi u sektoru. Dionica drugog sektora nije rad u sektoru centra, a dionica s prefiksom sektora jest (bilježi zatečeno: i šifra „P.” prolazi). Pisanje u području sektora dopušta upis sebe. |
| `TestDezurstvoGranice` | Odbijaju se: dežurstvo bez dnevnika i uz dnevnik bez centra, kraj jednak početku, trajanje dulje od 36 h (točno 36 h prolazi), kraj preko dana nakon zaključenja dnevnika, područje izvan sektora i opis izvan popisa. Područje 0 znači cijeli sektor. Bez prijave se ne upisuje. |
| `TestDezurstvoPotvrdaIIzmjena` | Upis uprave odmah je potvrđen. Pero mijenja svoje potvrđeno, ono se vraća na čekanje, a autor upisa ostaje uprava. Potvrđuje samo uprava, druga potvrda ne mijenja ništa, a nepostojeće se ne potvrđuje. Tuđi upis se ne preuzima ni vlastitim imenom. Izmjena nepostojećeg i dežurstva drugog dnevnika se odbija. |
| `TestDezurstvoMicanje` | Pero ne miče svoje potvrđeno, a nepotvrđeno miče. Drugo micanje istog i micanje bez prijave se odbijaju. |
| `TestObracunDezurstava` | Obračun po IORS2026 sa zaokruživanjem na 0,5: ured u radno vrijeme 0, teren 8 h → 1,5, noć 8 h → 15, a nedjelja odrezana na razdoblje (10 h) → 22. Nepotvrđeno ide u „čeka potvrdu” (2 h), a izvan razdoblja se ne broji. Grupe idu po područjima, cijeli sektor zadnji. Mjesto koje nije teren računa se kao ured. Prazno razdoblje daje prazan obračun, a bez tablice greška. |
| `TestObracunJedneOsobe` | Noćni razmak dijeli se na ponoći (20–24 pa 0–8) i na DRD, NRD i RRV sate. Obračunski sati: teren 6 / 16,5 / 0, nepotvrđeno odvojeno, a dežurstvo drugoga se ne vidi. Razdoblje reže noć na utorak. |
| `TestPreuzimanjeIPredajaDezurstva` | Preuzimanje upisuje dežurnog i zapis, a drugo preuzimanje se odbija. Predaju radi samo dežurni ili uprava, i ona upisuje nepotvrđen razmak i zapis. Predaja bez dežurnog se odbija. Preuzimanje od drugoga zaključuje njegov razmak („nije predano”), nepotvrđen. Predaja uprave potvrđena je odmah. |
| `TestPreuzimanjeDezurstvaOdbijeno` | Preuzimanje se odbija bez prijave, bez dnevnika, uz zaključen dnevnik i kad osoba ne radi u sektoru. Predaja bez prijave se odbija. |
| `TestPredajaDugogDezurstvaBezGranice` | Bilježi zatečeno: dežurstvo zaboravljeno tri dana predaja upiše kao jedan razmak od 72 h, iako ručni upis ne prima dulje od 36 h. Provjeren je i `satiTekst`. |
| `TestPredajaPrijePocetka` | Kad je početak dežurstva iza sadašnjeg trenutka (razlika satova među čvorovima), razmak se upiše kao minuta od početka. |
| `TestPredajaBezZapisaDnevnika` | Bilježi zatečeno: bez tablice zapisa predaja javi grešku, a razmak je već u planu. Svaki neuspjeli pokušaj preuzimanja ostavi još jedan dvojnik razmaka. |

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

### Sumnjivo ponašanje
1. **`internal/service/dezurstva_service.go:486–504`: predaja nije jedna transakcija.** Razmak se upiše u plan (`SaveDezurstvo`, redak 500), a tek onda zapis u dnevnik (504). Ako zapis ne uspije, predaja javi grešku, a razmak ostane. Kod preuzimanja od drugoga svaki neuspjeli pokušaj ostavi još jedan dvojnik razmaka (`TestPredajaBezZapisaDnevnika`), pa obračun može dvaput platiti iste sate. *Treba:* razmak, zapis i dežurnog u dnevniku upisati u jednoj transakciji.
2. **`internal/service/dezurstva_service.go:490–496`: predaja ne poštuje granicu od 36 sati.** Ručni upis odbija razmak dulji od 36 h, a predaja zaboravljenog dežurstva upiše 72 h kao jedan razmak (`TestPredajaDugogDezurstvaBezGranice`). *Treba:* razmak preko granice podijeliti ili označiti za pregled uprave.
3. **`internal/service/dezurstva_service.go:494–495`: minuta vrijedi samo kad kraj nije poslije početka.** Predaja odmah nakon preuzimanja daje razmak od djelića sekunde (`TestPreuzimanjeIPredajaDezurstva`). Manja stvar, ali takav razmak ulazi u plan i čeka potvrdu.
4. **`internal/service/dezurstva_service.go:39`: rad u sektoru prepoznaje se po prefiksu šifre dionice.** Svaka dodijeljena šifra koja počinje s „P.” daje rad u sektoru P, i ona bez ostatka (`TestDezurstvoUpisSebe`). *Treba:* provjeriti dionicu u registru ili barem puni oblik šifre.
5. **Obračun (`dezurstva_service.go:391`): mjesto koje nije teren obračunava se kao ured, i kad nije ni ured** (`TestObracunDezurstava`). Pitanje: treba li nepoznato mjesto odbiti pri upisu?
6. **`internal/service/journal_service.go:216`: kraj dana je početak + 24 sata.** Na dan pomicanja sata to nije ponoć, pa su vodostaji na listu dnevnika COP-a padali na krivi dan. **Popravljeno** u zasebnom commitu (`AddDate(0, 0, 1)`) s testom `TestVodostajiListaKadSeSatPomice` u `internal/service/dnevnik_vodostaji_test.go`, kao i u PR-u #5.
