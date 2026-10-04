# Početni baseline — 4. listopada 2026.

> **Ponovno izmjereno istog dana** s analizatorom 2 (isti commit `f5adb84`):
> mjere se samo datoteke koje git vidi, metrički lint nalazi uspoređuju se po
> funkciji, a errcheck ne traži provjeru čišćenja (vidi CODE_QUALITY.md).
> Coverage, CC, CRAP i duplikacije su isti; upozorenja su pala s 1.709 na
> 1.008, od toga errcheck s 1.199 na 498.

Mjeren je čisti commit `f5adb84` (0.0.31-alfa), na macOS/arm64 i Go 1.27.1.
U radnoj kopiji paralelno nastaju druge izmjene; ovaj izvještaj ih ne verificira.
Prije mjerenja nije refaktorirana nijedna aplikacijska funkcija.

## Što je pregledano

- Aplikacija: `cmd/gocop`, instalacijska/postavna aplikacija `cmd/gocop-postava`,
  poslovni paketi u `internal/`, ugrađeni web resursi u `web/`, dokumentacija u `docs/`.
- U referentnom commitu postoje **331 testna Go datoteka**. Izmjerene su **4.687
  imenovane funkcije/metode** aktivne platforme.
- Postojeći CI imao je `go vet`, `go test` i zasebni `govulncheck`; nije bilo
  verzionirane golangci konfiguracije, coverage/CRAP baselinea ni race gatea.
- Kritični odabir uključuje i ovjeru/proglašenja obrane (`akt_service.go`), uz
  obradu vodostaja, prognozu, autentikaciju/RBAC, uvoz, arhivu i sinkronizaciju.

## Kako čitati crveni rezultat

Testovi i race **prolaze**. Tri primarna staticcheck nalaza ostaju blokirajuća:

1. `internal/service/akt_service.go:665` — SA4009, ulazni `perms` prepiše se
   prije prve uporabe. Lokalni kod potom koristi ovlasti izvedene iz ovjerenog
   akta; treba razjasniti potpis metode i testirati taj ugovor, ne naslijepo mijenjati RBAC.
2. `internal/web/handlers_sections_popis_izvoz.go:214` — SA4006,
   dodijeljena vrijednost `podrucje` nikada se ne koristi.
3. `internal/web/prijava_pdf.go:138` — SA4006,
   dodijeljena vrijednost `org` nikada se ne koristi.

To nisu tri dokazane sigurnosne ranjivosti, nego nalazi koje stroga konfiguracija
ne prešućuje. Baseline ih bilježi, ali ih **ne oslobađa gatea**. Dva namjerna
testna obrasca imaju obrazložene, vremenski ograničene iznimke. Ostalih 1.008 lint
upozorenja nije 1.008 različitih bugova: isti kod može aktivirati više metrika,
a najveću skupinu čine neprovjereni povrati (`errcheck`, uključujući testove,
zapis u HTTP odgovor i zatvaranje resursa).

## Izmjereni rezultati

- Commit: `f5adb844a2b92d2cc182dfeb055f7fd343ace4b1` (dirty: false)
- Vrijeme: 2026-10-04T11:01:25Z
- Platforma: darwin/arm64, go1.27.1
- Konfiguracija: `f116dd66ad093d048bd4e7895d2ffaf394dcf370e95ea47321bf7829b75b41f0`
- Status: **FAIL**

| Metrika | Rezultat |
|---|---:|
| Testovi | PASS |
| Race | PASS |
| Statičke greške / upozorenja | 3 / 1008 |
| Coverage | 51.81 % |
| Kritični coverage | 50.46 % |
| Prosječni / najveći CRAP | 43.35 / 48180.00 |
| Prosječna / najveća kompleksnost | 5.41 / 219 |
| Exact duplikacija (tokeni tijela funkcija) | 1.01 % |
| Fuzzy kandidati (uključuju exact) | 0.68 % |
| Mutation | NOT_RUN |

## Top 10 CRAP

| Funkcija | CC | Coverage | CRAP | Kritična |
|---|---:|---:|---:|:---:|
| `cmd/gocop/main.go:99` · `main` | 219 | 0.00 % | 48180.00 | false |
| `internal/importer/bp16/journals.go:242` · `RunJournals` | 82 | 0.00 % | 6806.00 | true |
| `internal/service/zid_service.go:363` · `(*opisivac).opisi` | 104 | 26.98 % | 4314.35 | false |
| `internal/db/seed.go:64` · `SeedInitialData` | 59 | 0.00 % | 3540.00 | false |
| `internal/web/prognoze_sazetak.go:344` · `(*PrognozeHandler).listSazetka` | 58 | 0.00 % | 3422.00 | false |
| `internal/importer/bp16/bp16.go:368` · `Run` | 55 | 0.00 % | 3080.00 | true |
| `internal/service/akt_service.go:395` · `(*AktService).primatelji` | 52 | 0.00 % | 2756.00 | true |
| `internal/prognoza/provjera.go:45` · `ProvjeriUnatrag` | 49 | 0.00 % | 2450.00 | true |
| `internal/importer/csvlevels/csvlevels.go:104` · `Run` | 47 | 0.00 % | 2256.00 | true |
| `internal/pdfw/dodatak.go:102` · `Dodaj` | 47 | 0.00 % | 2256.00 | false |

## Top 10 kompleksnost

| Funkcija | CC | Coverage | CRAP | Kritična |
|---|---:|---:|---:|:---:|
| `cmd/gocop/main.go:99` · `main` | 219 | 0.00 % | 48180.00 | false |
| `internal/repository/apply.go:235` · `applyOne` | 134 | 78.55 % | 311.19 | true |
| `internal/web/uzduzni_profil.go:246` · `crtajUzduzni` | 107 | 96.18 % | 107.64 | false |
| `internal/service/zid_service.go:363` · `(*opisivac).opisi` | 104 | 26.98 % | 4314.35 | false |
| `internal/importer/bp16/journals.go:242` · `RunJournals` | 82 | 0.00 % | 6806.00 | true |
| `internal/importer/bp16/prijave.go:123` · `RunPrijave` | 70 | 81.98 % | 98.69 | true |
| `internal/posta/proba.go:107` · `PokreniProbniEWS` | 65 | 22.75 % | 2012.36 | false |
| `internal/repository/apply.go:971` · `removeFromSurface` | 64 | 95.40 % | 64.40 | true |
| `internal/prognoza/osvjezavanje.go:156` · `(*Osvjezivac).Osvjezi` | 63 | 73.17 % | 139.65 | true |
| `internal/db/seed.go:64` · `SeedInitialData` | 59 | 0.00 % | 3540.00 | false |

## Coverage po paketu

| Paket | Coverage | Pokrivene / ukupne naredbe |
|---|---:|---:|
| `gocop/cmd/gocop` | 19.08 % | 241 / 1263 |
| `gocop/cmd/gocop-postava` | 0.00 % | 0 / 274 |
| `gocop/internal/arhiva` | 72.22 % | 1310 / 1814 |
| `gocop/internal/config` | 83.52 % | 76 / 91 |
| `gocop/internal/db` | 29.87 % | 339 / 1135 |
| `gocop/internal/dhmz` | 87.43 % | 153 / 175 |
| `gocop/internal/docx` | 93.18 % | 205 / 220 |
| `gocop/internal/geometrija` | 83.94 % | 183 / 218 |
| `gocop/internal/hidroview` | 65.07 % | 95 / 146 |
| `gocop/internal/hydro` | 81.34 % | 218 / 268 |
| `gocop/internal/ikona` | 0.00 % | 0 / 105 |
| `gocop/internal/imecvora` | 100.00 % | 32 / 32 |
| `gocop/internal/importer/arso` | 93.48 % | 86 / 92 |
| `gocop/internal/importer/bp16` | 16.63 % | 168 / 1010 |
| `gocop/internal/importer/csvlevels` | 12.54 % | 35 / 279 |
| `gocop/internal/importer/ehyd` | 83.33 % | 55 / 66 |
| `gocop/internal/importer/gkd` | 79.22 % | 61 / 77 |
| `gocop/internal/importer/pegelonline` | 75.76 % | 25 / 33 |
| `gocop/internal/importer/seba` | 85.87 % | 79 / 92 |
| `gocop/internal/importer/ugovor` | 2.72 % | 9 / 331 |
| `gocop/internal/importer/xlsx` | 84.05 % | 195 / 232 |
| `gocop/internal/izdanje` | 90.48 % | 57 / 63 |
| `gocop/internal/javnivodostaji` | 62.25 % | 691 / 1110 |
| `gocop/internal/kisomjeri` | 82.44 % | 216 / 262 |
| `gocop/internal/ledger` | 78.81 % | 331 / 420 |
| `gocop/internal/mletva` | 37.80 % | 31 / 82 |
| `gocop/internal/models` | 32.32 % | 769 / 2379 |
| `gocop/internal/oborine` | 68.85 % | 84 / 122 |
| `gocop/internal/obracun` | 70.70 % | 111 / 157 |
| `gocop/internal/pdfw` | 47.99 % | 275 / 573 |
| `gocop/internal/peers` | 61.40 % | 1015 / 1653 |
| `gocop/internal/poslovi` | 85.15 % | 86 / 101 |
| `gocop/internal/posta` | 60.92 % | 714 / 1172 |
| `gocop/internal/postava` | 65.41 % | 467 / 714 |
| `gocop/internal/potpis` | 79.25 % | 168 / 212 |
| `gocop/internal/prognoza` | 54.53 % | 1956 / 3587 |
| `gocop/internal/qr` | 99.18 % | 241 / 243 |
| `gocop/internal/razmjena` | 78.84 % | 395 / 501 |
| `gocop/internal/repository` | 44.52 % | 3170 / 7121 |
| `gocop/internal/sadrzaj` | 64.65 % | 128 / 198 |
| `gocop/internal/service` | 40.31 % | 2847 / 7062 |
| `gocop/internal/slike` | 80.75 % | 130 / 161 |
| `gocop/internal/ulaganje` | 29.74 % | 116 / 390 |
| `gocop/internal/uvoz/godisnjak` | 0.00 % | 0 / 313 |
| `gocop/internal/uvoz/his2000` | 47.88 % | 271 / 566 |
| `gocop/internal/uvoz/hvpovijest` | 0.00 % | 0 / 127 |
| `gocop/internal/uvoz/izvori` | 46.80 % | 95 / 203 |
| `gocop/internal/weather` | 62.33 % | 91 / 146 |
| `gocop/internal/web` | 57.50 % | 14058 / 24450 |
| `gocop/internal/xlsxw` | 74.37 % | 206 / 277 |
| `gocop/web` | N/A | 0 / 0 |

## Statička analiza

| Linter | Nalazi |
|---|---:|
| dupl | 4 |
| errcheck | 498 |
| gocognit | 145 |
| gocyclo | 218 |
| ineffassign | 5 |
| maintidx | 38 |
| nestif | 76 |
| revive | 1 |
| staticcheck | 11 |
| unused | 15 |

## Exact blokovi — primjeri

- `internal/arhiva/gradnja.go::popisi` ↔ `internal/arhiva/ulaz.go::Letve` (100.0%, najmanje 100 tokena).
- `internal/db/sections_link.go::watercourseIndexTx` ↔ `internal/db/watercourses_seed.go::watercourseIndex` (100.0%, najmanje 100 tokena).
- `internal/importer/bp16/obilasci.go::RunObilasci` ↔ `internal/importer/bp16/prijave.go::RunPrijave` (100.0%, najmanje 100 tokena).
- `internal/javnivodostaji/hidmet.go::CitajHidmet` ↔ `internal/javnivodostaji/javnivodostaji.go::CitajTablicu` (100.0%, najmanje 100 tokena).
- `internal/peers/peers.go::(*Service).exchange` ↔ `internal/peers/peers.go::(*Service).exchange` (100.0%, najmanje 100 tokena).
- `internal/repository/journal_repo.go::(*JournalRepository).ListJournals` ↔ `internal/repository/journal_repo.go::(*JournalRepository).ListCOPJournals` (100.0%, najmanje 100 tokena).
- `internal/repository/journal_repo.go::(*JournalRepository).SaveSheet` ↔ `internal/repository/mts_repo.go::(*MtsRepository).SaveSkladiste` (100.0%, najmanje 100 tokena).
- `internal/repository/station_repo.go::(*StationRepository).CreateStation` ↔ `internal/repository/station_repo.go::(*StationRepository).UpdateStation` (100.0%, najmanje 100 tokena).
- `internal/service/maintenance_service.go::(*MaintenanceService).LinkWater` ↔ `internal/service/maintenance_service.go::(*MaintenanceService).AddWater` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_akti.go::(*AktiHandler).ShowSpranca` ↔ `internal/web/handlers_akti.go::(*AktiHandler).ShowPrimatelji` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_mts_izvoz.go::(*MtsHandler).IzvoziInventuru` ↔ `internal/web/handlers_mts_izvoz.go::(*MtsHandler).IzvoziSkladiste` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_mts_izvoz.go::KnjigaSkladista` ↔ `internal/web/handlers_mts_izvoz.go::KnjigaMts` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_dezurstva.go::(*JournalsHandler).ShowIORS` ↔ `internal/web/handlers_obracun_izvoz.go::(*JournalsHandler).IzvoziIORS` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_arhiva_csv.go::citajIspravke` ↔ `internal/web/handlers_ocitanja_csv.go::citajOcitanja` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_arhiva_csv.go::(*ReadingsHandler).HandleArhivaUvoz` ↔ `internal/web/handlers_ocitanja_csv.go::(*ReadingsHandler).HandleOcitanjaUvoz` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_izvjesca_izvoz.go::KnjigaIzvjesca` ↔ `internal/web/handlers_sections_izvoz.go::KnjigaDionice` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_sections_izvoz.go::KnjigaDionice` ↔ `internal/web/handlers_sektorsko_izvoz.go::KnjigaSektorskog` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_izvjesca_izvoz.go::KnjigaIzvjesca` ↔ `internal/web/handlers_sektorsko_izvoz.go::KnjigaSektorskog` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_pairing.go::(*PairHandler).HandleDial` ↔ `internal/web/handlers_settings.go::(*SettingsHandler).HandlePairDial` (100.0%, najmanje 100 tokena).
- `internal/web/handlers_stations.go::(*StationsHandler).HandleCreateStationAPI` ↔ `internal/web/handlers_stations.go::(*StationsHandler).HandleUpdateStationAPI` (100.0%, najmanje 100 tokena).

## Fuzzy funkcije — kandidati za pregled

- `internal/web/handlers_admin.go::(*AdminHandler).pageData` ↔ `internal/web/handlers_izvori.go::(*IzvoriHandler).pageData` (100.0%, najmanje 101 tokena).
- `internal/web/handlers_pairing.go::(*PairHandler).HandleDiscover` ↔ `internal/web/handlers_settings.go::(*SettingsHandler).HandleDiscover` (100.0%, najmanje 89 tokena).
- `internal/repository/izvjesca_repo.go::(*IzvjescaRepository).Save` ↔ `internal/repository/sektorska_izvjesca_repo.go::(*SektorskaIzvjescaRepository).Save` (95.1%, najmanje 247 tokena).
- `internal/repository/prijava_repo.go::(*PrijavaRepository).Save` ↔ `internal/repository/vodocuvar_repo.go::(*VodocuvarRepository).Save` (93.7%, najmanje 168 tokena).
- `internal/repository/akti_repo.go::(*AktiRepository).SavePrimatelj` ↔ `internal/repository/sluzbe_repo.go::(*TerritoryRepository).SaveSluzba` (93.1%, najmanje 164 tokena).
- `internal/repository/izvjesca_repo.go::(*IzvjescaRepository).Arhiviraj` ↔ `internal/repository/sektorska_izvjesca_repo.go::(*SektorskaIzvjescaRepository).Arhiviraj` (91.9%, najmanje 148 tokena).
- `internal/repository/akti_repo.go::(*AktiRepository).SaveAkt` ↔ `internal/repository/mts_repo.go::(*MtsRepository).SaveSkladiste` (90.5%, najmanje 185 tokena).
- `internal/repository/journal_repo.go::(*JournalRepository).SaveSheet` ↔ `internal/repository/journal_repo.go::(*JournalRepository).SaveEntry` (90.4%, najmanje 238 tokena).
- `internal/ledger/ledger.go::(*Recorder).CountByChannel` ↔ `internal/repository/journal_repo.go::(*JournalRepository).BrojPoVrstama` (89.9%, najmanje 105 tokena).
- `internal/ledger/ledger.go::(*Recorder).Count` ↔ `internal/ledger/ledger.go::(*Recorder).CountByChannel` (89.9%, najmanje 105 tokena).
- `internal/ledger/ledger.go::(*Recorder).Count` ↔ `internal/repository/journal_repo.go::(*JournalRepository).BrojPoVrstama` (89.9%, najmanje 105 tokena).
- `internal/repository/prijava_repo.go::(*PrijavaRepository).Izvornik` ↔ `internal/repository/vodocuvar_repo.go::(*VodocuvarRepository).GetIzvornik` (89.7%, najmanje 97 tokena).
- `internal/repository/akti_repo.go::(*AktiRepository).KorisniciSRacunomPoste` ↔ `internal/sadrzaj/sadrzaj.go::(*Spremiste).Sirocad` (88.9%, najmanje 96 tokena).
- `internal/web/handlers_admin.go::(*AdminHandler).pageData` ↔ `internal/web/handlers_stations_pages.go::(*StationsHandler).pageData` (88.9%, najmanje 101 tokena).
- `internal/web/handlers_admin.go::(*AdminHandler).pageData` ↔ `internal/web/handlers_territories_pages.go::(*TerritoriesHandler).pageData` (88.9%, najmanje 101 tokena).
- `internal/web/handlers_izvori.go::(*IzvoriHandler).pageData` ↔ `internal/web/handlers_stations_pages.go::(*StationsHandler).pageData` (88.9%, najmanje 101 tokena).
- `internal/web/handlers_izvori.go::(*IzvoriHandler).pageData` ↔ `internal/web/handlers_territories_pages.go::(*TerritoriesHandler).pageData` (88.9%, najmanje 101 tokena).
- `internal/web/handlers_stations_pages.go::(*StationsHandler).pageData` ↔ `internal/web/handlers_territories_pages.go::(*TerritoriesHandler).pageData` (88.9%, najmanje 101 tokena).
- `internal/web/handlers_settings.go::(*SettingsHandler).HandleRevokeMember` ↔ `internal/web/handlers_settings.go::(*SettingsHandler).HandleForgetPeer` (88.0%, najmanje 100 tokena).
- `internal/importer/arso/arso.go::Citaj` ↔ `internal/importer/seba/seba.go::Citaj` (87.8%, najmanje 237 tokena).

## Blokade i regresije

- lint:staticcheck:internal/service/akt_service.go:SA4009: argument perms is overwritten before first use
- lint:staticcheck:internal/web/handlers_sections_popis_izvoz.go:SA4006: this value of podrucje is never used
- lint:staticcheck:internal/web/prijava_pdf.go:SA4006: this value of org is never used
- static analysis: FAIL

## Upozorenja / zatečeni dug

- coverage ispod cilja 85%
- kritični coverage ispod cilja 95%
- 1006 funkcija iznad CC/CRAP cilja
- 1008 lint upozorenja

## Primijenjene obrazložene iznimke

- lint:staticcheck:internal/poslovi/poslovi_test.go:SA5000: assignment to nil map: TestPanikaUPosluNeRusiProgram namjerno izaziva paniku upisom u nil mapu kako bi provjerio oporavak pozadinskog posla; pregledano 2026-10-04.
- lint:staticcheck:internal/posta/svjeza_veza_test.go:SA4000: identical expressions on the left and right side of the '==' operator: TestProvjeraLozinkeNovomVezom uspoređuje dva poziva tvorničke metode: uz SvjezaVeza=true svaki poziv mora vratiti drugi HTTP klijent. Izrazi jesu tekstualno isti, ali namjerno ne smiju imati istu vrijednost.

Sve funkcije i njihova pokrivenost nalaze se u `code-health.json`; izvorni Go izvještaj u `coverage-functions.txt`, a nepokrivene naredbe u `coverage.html`. Fuzzy nije dokaz iste odgovornosti. Pogledati `docs/CODE_QUALITY.md` za metodologiju i ograničenja.

## Što popravljati prvo

1. **Tri statička nalaza iznad**: mali zasebni zahvati i regresijski testovi.
   Cilj je maknuti stvarne blokade bez prepravljanja cijele aplikacije.
2. **Kritični operativni putovi**: `repository.applyOne` (CC 134, coverage
   78,55%, CRAP 311,19), `prognoza.Osvjezi` (63; 73,17%; 139,65), uvoznici i
   ovjere. Prvo testovi odbijanja, rubnih uvjeta, neispravnog unosa i prekida
   transakcije/sinkronizacije; tek zatim izdvajanje koherentnih odgovornosti.
3. **Velike netestirane funkcije**: `RunJournals`, `bp16.Run`,
   `ProvjeriUnatrag`, `csvlevels.Run`, `SeedInitialData` i sastavljanje
   prognoznog sažetka. `main` ima najveći CRAP, ali je to velikim dijelom
   spajanje sustava i životni ciklus; prioritet ne treba određivati samo brojka.
4. **Zid aktivnosti i poslovne odluke**: `opisivac.opisi` ima CC 104 i coverage
   26,98%; testirati reprezentativne vrste događaja i nedostajuće reference.
5. **Errcheck po riziku**: najprije upisi u baze/arhive, provjera potpisa i
   operativni uvoz; bez masovnog dodavanja `_ =` samo radi utišavanja lintera.
6. **Duplikacije tek nakon toga**: exact udio je oko 1%, a fuzzy kandidati oko
   0,7% tijela funkcija prema ovom detektoru. Nisu trenutno najveći dug.
   Usporediti `IzvjescaRepository.Save` i `SektorskaIzvjescaRepository.Save`,
   dva spremanja listova u `journal_repo.go`, indeks vodotokova u DB paketu te
   kopirane `HandleDiscover` handlere. Različiti tipovi izvještaja mogu opravdano
   ostati odvojeni. `dupl` javlja četiri usmjerena nalaza za dva para blokova.
7. **Mutation pilot** na malom skupu kritičnih pravila nakon poboljšanja testova;
   score još nije izmjeren. Ne pokretati sve mutacije cijelog sustava u svakom PR-u.

## Granice ove provjere

Ovo je pregled Go koda i testova, ne sigurnosni audit ni hidraulička validacija
prognoze. Coverage znači izvršene naredbe, ne ispravnost tvrdnji u testovima.
Prosjek CRAP-a snažno podižu netestirane velike funkcije. Visoko pokriveni
`crtajUzduzni` (96,18%) i dalje ima CC 107 i CRAP 107,64: to je signal za
održavanje, ne dokaz neispravnog crteža.

Nije mijenjan aplikacijski Go kod, baza, uvozi, localhost ni paralelni rad na
0.0.32. GitHub workflow je pripremljen, ali nije ovdje izvršen na hosted runneru.
Gremlins je opcionalna priprema, a ne izvršeno mjerenje. Lokalni testovi quality
alata pokrivaju formulu, ponderiranje, identitete metoda, klonove, regresije,
iznimke, pogrešnu platformu i nevaljane/nedovršene rezultate.

Metodologija, naredbe, pragovi i obnova baselinea: [CODE_QUALITY.md](CODE_QUALITY.md).

