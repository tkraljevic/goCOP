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
| `TestProvjeraOcitanja` | Granice provjere: letva ili objekt (ne oboje, ne ništa), vrijeme obavezno, najviše 60 min unaprijed, od 1900. godine. Mora postojati vodostaj, stanje, zapornica ili napomena. Vodostaj od −500 do 3000 cm, temperatura od −5 do 45 °C, protok od 0 do 100 000 m³/s, bez beskonačnog. Poznati načini, stanja i zapornice. Prazan način postaje ručni, a napomena i očitao se obrezuju. Bilježi zatečeno: NaN prolazi, a stanje objekta prolazi i na letvi. |
| `TestUpisOcitanjaIPrava` | Na svojoj dionici upis nosi trag (korisnik, očitao, podrijetlo, način), a zadani očitao i podrijetlo ostaju. Provjera unosa ide prije provjere prava. Bilježi zatečeno: letva bez dionica prima očitanje od svakoga tko igdje piše, i iz drugog sektora. Letva na dvije dionice traži pravo na jednoj. Objekt područja bez dionica prima očitanje rukovoditelja dionice tog područja. |
| `TestPravoUpisaNaObjekt` | Uprava područja upisuje na objekt svog područja, a drugog ne. Rukovoditelj dionice upisuje na objekt svoje dionice i na objekt područja bez dionica, a ne na objekt druge dionice istog područja ni na objekt drugog područja koji stoji na njegovoj dionici. Globalni administrator smije sve, a bez ovlasti ništa. |
| `TestIzmjenaIBrisanjeOcitanja` | Izmjena ne mijenja letvu, podrijetlo, oznaku izvora ni autora iz zahtjeva. Autor mijenja svoje i bez prava na letvi, a tuđe na tuđoj letvi ne. Kod izmjene provjera prava ide prije provjere unosa. Brisanje: tuđe ne, svoje da (i vraća obrisano), drugo brisanje ne. Očitanje s neispravnom letvom dira samo autor i administrator. |
| `TestZalijepljenaOcitanja` | Ponovni uvoz istog zalijepljenog niza ne udvostručuje. Jedno neispravno očitanje odbija cijeli niz, s trenutkom u poruci. Tuđa letva i niz bez letve se odbijaju. |
| `TestPregledSvihLetvi` | Pregled letvi: broj, zadnje i prethodno očitanje te stupanj obrane po pragovima. Objekt uzima pragove svog vodomjera, a nasip ne prima očitanja. Poredak: s očitanjem prije bez, viši stupanj prije nižeg, novije prije starijeg, pa po imenu. |
| `TestTerenskiPogled` | Navike iz zadnjih 90 dana: „moje” letve po uobičajenom vremenu, ostale letve područja, a „danas obavljeno” vrijedi i kad je očitao netko drugi. Područje dolazi iz primarne dužnosti. Bilježi zatečeno: imenjak se broji u navike, a zadano područje izvan izbora prolazi. Administrator, uprava sektora i osoba bez ovlasti dobivaju odgovarajući izbor. |
| `TestTerenskiPogledVrijemeOkoPonoci` | Bilježi zatečeno: letva očitavana oko ponoći (23:50 i 0:10) dobiva uobičajeno vrijeme oko podneva, pa ide iza jutarnje. |
| `TestFazaZaOcitanje` | Bez letve ili vodostaja stupanj se ne zna, a 650 cm na Primjerovu je izvanredna obrana. Servis bez spremišta ne zna prvo očitanje ni krajnosti. |
| `TestPravoUpisaPrekoDionice` | Uprava područja upisuje na letvu dionice svog područja, a ne drugog. Letva na dionicama oba područja je njezina. Nepostojeća dionica ne daje pravo, a prazne ovlasti ne pišu nigdje. Uprava mijenja tuđe očitanje na objektu svog područja. |
| `TestCitanjeOcitanja` | Get, List (najnovije prvo), Stats, PrvoOcitanje i Krajnosti vraćaju upisano. |
| `TestPregledBezDijelaBaze` | Bez bilo koje od tablica očitanja, letava, veza i objekata pregled i terenski pogled vraćaju samo grešku, bez djelomičnog popisa. Izmjena i brisanje bez tablice javljaju grešku spremišta. |

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

### Sumnjivo ponašanje
1. **`internal/service/reading_service.go:164` i `:167`: NaN prolazi provjeru temperature i protoka.** NaN nije ni manji ni veći od granice (`TestProvjeraOcitanja`). *Treba:* odbiti NaN (`math.IsNaN`).
2. **`internal/service/reading_service.go:177`: stanje objekta i zapornica primaju se i na očitanju letve.** Ne provjerava se je li očitanje uopće s objekta (`TestProvjeraOcitanja`). *Treba:* stanje i zapornicu primati samo uz objekt.
3. **`internal/service/reading_service.go:77–78`: letva bez dionica prima očitanje od svakoga tko igdje piše**, i od osobe s dužnošću u drugom sektoru (`TestUpisOcitanjaIPrava`). *Treba:* pitanje je li to namjera (letve koje još nisu vezane uz dionicu) ili treba ograničiti barem na sektor.
4. **`internal/service/reading_service.go:379–405` (`FieldOverview`): zadano područje ne provjerava se prema izboru.** Pero Perić dobije pogled područja 2, iako ga nema u izboru (`TestTerenskiPogled`). Pogled samo čita, ali otkriva navike i letve tuđeg područja. *Treba:* odbiti ili vratiti na prvo dopušteno područje.
5. **`internal/service/reading_service.go:429` (`HabitsFor` po imenu): navike se zbrajaju i po punom imenu.** Imenjak s drugim računom ulazi u navike prvoga (`TestTerenskiPogled`). *Treba:* navike vezati uz korisnički ID, a ime koristiti samo za stara očitanja bez ID-a.
6. **`internal/repository/reading_repo.go:434`: uobičajeno vrijeme je aritmetička sredina minuta u danu.** Letva očitavana oko ponoći (23:50 i 0:10) dobiva vrijeme oko podneva i ide iza jutarnje (`TestTerenskiPogledVrijemeOkoPonoci`). *Treba:* kružna sredina ili medijan.
7. **`internal/service/reading_service.go:124`: autor mijenja svoje očitanje i kad više nema prava na letvi** (`TestIzmjenaIBrisanjeOcitanja`). Pitanje: treba li izmjenu nakon gubitka dužnosti ograničiti rokom?
8. **Redoslijed provjera nije isti:** kod upisa se unos provjerava prije prava (gost dobije grešku unosa), a kod izmjene prava prije unosa (`TestUpisOcitanjaIPrava`, `TestIzmjenaIBrisanjeOcitanja`). Manja stvar.
9. **`internal/repository/organization_repo.go:222` (`ListAreas`): pada kad je `areas.subcenter` NULL**, pa okolina testa upisuje `''`. *Treba:* čitati NULL kao prazno (`COALESCE`).
