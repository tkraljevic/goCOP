# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila i postupak mjerenja su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-mts`
- Commit s testovima je gotov i poslan. `go test ./internal/service/` prolazi, a lint ne daje novih nalaza. Produkcijski kod nije mijenjan.
- **Nije napravljeno:** `make quality` za granu prema baselineu.

## Nacrt opisa PR-a

Testovi rubnih slučajeva za `MtsService.Provedi` u `internal/service/mts_provedi_rubovi_test.go`. Okolina je postojeći `pripremiMts`, a novi podaci su izmišljeni: skladištar Pero Perić (pperic) i „Skladište Primjerovo”.

### Testovi
| Test | Što tvrdi |
|---|---|
| `TestProvediOdbijanja` | Odbija se 22 slučaja: bez prijave, količina nula ili negativna, nepoznata vrsta sredstva, nepoznato skladište, uprava drugog sektora ili bez ovlasti, datum sutra, nepoznata vrsta prometa. Odbijaju se i: otpis, izdavanje, povrat i utrošak bez zalihe na tom mjestu, izdavanje bez mjesta, nepoznata dionica, dionica drugog područja, punjenje sredstva bez oblika, punjenje u isti oblik, punjenje bez praznih (u skladištu i na terenu) te prijenos u isto ili nepoznato skladište. Nijedno odbijanje ne ostavlja redak u knjizi prometa. |
| `TestProvediGranicaZalihe` | Zaliha se skida točno do nule, a ne preko nje. Poruka kaže koliko stoji i koliko se skida („2,5” i „2,501”). Napomena se obrezuje. |
| `TestProvediRedci` | Bez datuma je danas. Sredstvo bez oblika vodi se u osnovnom obliku i kad zahvat nosi drugi. Nalog, preuzimatelj i dokument se obrezuju, a redak nosi osobu i sektor. Prijenos daje dva retka iste veze s napomenom „u …” i „iz …”, a zadana napomena ostaje na oba. Izdavanje na dionicu uzima područje iz dionice, a redak na terenu nema skladišta. Utrošak daje jedan redak s terena. Povrat s mjesta na kojem ništa ne stoji se odbija. |

### Prije i poslije (paket service)
CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality`. „Poslije” je coverage paketa `service` s grane, a CRAP je izračunat istom formulom (CC² · (1 − cov)³ + CC). Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/mts_service.go:40` · `(*MtsService).SmijePisati` | 4 | 66.7 % | 100.0 % | 4.6 | 4.0 |  |
| `internal/service/mts_service.go:298` · `(*MtsService).mjestoTerena` | 14 | 57.9 % | 73.7 % | 28.6 | 17.6 |  |
| `internal/service/mts_service.go:357` · `(*MtsService).Provedi` | 45 | 81.5 % | 95.2 % | 57.9 | 45.2 |  |

`Provedi` je i s 95 % coveragea na CC 45 i CRAP-u 45. Dijeli se prirodno na provjeru zahvata i po jednu funkciju za svaku vrstu prometa (`switch`), pa bi to bio kandidat za fazu 3: svaka vrsta prometa čitala bi se zasebno. Promjena bi zahvatila samo `mts_service.go`.

### Sumnjivo ponašanje
Testovi nisu otkrili grešku. Pitanja za pregled:
1. **`internal/service/mts_service.go:357` (prijenos): pravo se provjerava samo za izvorno skladište.** Uprava sektora B smije prenijeti sredstvo u skladište drugog sektora, a ciljno skladište se ne provjerava (`SmijePisati` se zove samo za izvor). Je li to namjera (pomoć drugom sektoru)? Testom nije pokriveno.
2. **`internal/service/mts_service.go:298` (`mjestoTerena`): objekt koji se ne može pročitati tiho se zanemaruje.** Neispravan ili nepostojeći `StructureID` uz zadano područje ne javlja grešku i ostaje upisan u retku. Objekt se čita s `context.Background()` umjesto s kontekstom zahtjeva. Testom nije pokriveno.
3. **Grešku upisa u bazu (`SavePromet`) nije moguće izazvati iz paketa `service`.** `pripremiMts` ne vraća vezu na bazu, a kontekst nema rubnih slučajeva koji bi je izazvali. Taj put ostaje nepokriven.
