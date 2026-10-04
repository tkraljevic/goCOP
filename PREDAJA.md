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
| `TestNovaDionicaUlaz` | Odbijaju se: dionica bez šifre i bez područja, šifra izvan oblika A–F.NN.NNN, dionica bez poddionica, poddionica bez vodotoka i opisa, kriva obala, objekt i nasip bez naziva te nova voda koju ne upisuje globalni administrator. Odbijeno se ne upisuje i ne objavljuje događaj. |
| `TestNovaDionicaUpis` | Šifra se svodi na velika slova bez razmaka, sektor se uzima iz šifre, poddionice se prenumeriraju, a stacionaža ide od manje prema većoj. Objavljuje se `section_created` i upisuje jedna verzija u knjigu. Ista šifra drugi put i nepostojeće područje se odbijaju. |
| `TestPravoNaNovuDionicu` | Novu dionicu smiju dodati uprava sektora, uprava područja u svom području i globalni administrator. Uprava područja u tuđem području, rukovoditelj dionice i osoba bez ovlasti ne smiju. |
| `TestSifraDioniceNeProvjeravaPodrucje` | Bilježi zatečeno: područje iz šifre ne provjerava se (F.42.7 u području 41), sektor iz šifre ne mora biti sektor područja (E.42.1 u području 42 sektora F), a zadani sektor pobjeđuje šifru. |
| `TestIzmjenaDionice` | Uprava drugog područja ne uređuje. Rukovoditelj dionice uređuje svoju, ali je ne premješta: područje i sektor tiho ostaju zatečeni. Objavljuje se `section_updated`. Izmjena nepostojeće daje ErrSectionNotFound, a neispravna izmjena ne mijenja ništa. |
| `TestNoveVezeDionice` | `provjeriNoveVeze`: bez ovlasti ne; globalni sve. Rukovoditelj dionice veže objekt svog područja, a tuđeg ne (uprava sektora smije). Objekt kojeg nema u registru se odbija. Slobodan vodomjer i vodomjer koji je i na vlastitoj dionici smiju se vezati, a vodomjer samo tuđe dionice ne, osim onome tko ondje piše. Poruka imenuje vodomjer bez naziva po oznaci. |
| `TestPravaNaDionicu` | `CanEditSection` po ulozi: globalni, uprava sektora, pisanje u sektoru, uprava područja i rukovoditelj te dionice smiju; pisanje u drugom području, rukovoditelj druge dionice i osoba bez ovlasti ne. Za nepostojeću dionicu ne smije nitko. `CanCreateSectionInArea` traži sektor i ovlast. |
| `TestSljedecaSifraDionice` | Prazno područje predlaže F.41.1, a inače sljedeći broj iza najvećeg u području sa šifrom tog područja. Bez sektora ili područja nema prijedloga. Bez tablice dionica prijedlog je F.41.1, a spremanje pada. |

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

### Sumnjivo ponašanje
1. **`internal/service/section_service.go:118–119`: šifra dionice ne slaže se nužno s područjem i sektorom.** Broj područja iz šifre ne provjerava se prema `AreaID` (F.42.7 upisana u područje 41). Sektor se uzima iz šifre i kad područje pripada drugom sektoru (E.42.1 u području sektora F), a zadani sektor pobjeđuje i šifru i područje (`TestSifraDioniceNeProvjeravaPodrucje`). Pravo se priznaje po području, pa onaj tko piše u području 42 upiše dionicu sektora E. *Treba:* sektor i područje iz šifre moraju biti sektor i područje dionice, inače odbiti.
2. **`internal/service/section_service.go:131`: premještanje dionice tiho se zanemaruje.** Kad rukovoditelj dionice u izmjeni pošalje drugo područje ili sektor, izmjena prolazi, a područje i sektor ostaju zatečeni, bez poruke (`TestIzmjenaDionice`). *Treba:* odbiti ili javiti da se dionica ne premješta.
3. **`internal/service/section_service.go:254` (`SljedecaSifra`): greška čitanja se guta.** Kad dionice ne mogu biti pročitane, prijedlog je prvi broj (F.41.1), iako takva dionica možda postoji (`TestSljedecaSifraDionice`). Manja stvar: spremanje kasnije odbije dvostruku šifru.
