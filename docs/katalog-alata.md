# Katalog alata

Stanje 26. 9. 2026., nakon razdvajanja aplikacije i alata (vidi
[plan](plan-razdvajanje-aplikacije-i-alata.md)). Repozitorij nosi samo
aplikaciju: `cmd/gocop`, `internal` i `web`. Sve ostalo stoji lokalno u
`tools/` i ne ulazi u repozitorij (`.gitignore`). Raspoređenih poslova na
računalu nema, pa alate pokreće samo čovjek.

## Što radi aplikacija

Ovo više ne traži nikakav alat:

| Posao | Gdje u aplikaciji |
|---|---|
| Uvoz tablice u arhivu | Administracija → Unos u arhivu → vrata arhive |
| Uvoz posebnih izvora: HIS-2000 (izvoz postaje ili ZIP), ARSO, eHYD, GKD, PEGELONLINE, Geolux SEBA, godišnjaci RHMZ Srbije | Administracija → Unos u arhivu → Posebni izvori |
| Povijest letve s Geolux HydroViewa | Administracija → Unos u arhivu → Povijest s HydroViewa |
| Račun za HydroView i mletva | Administracija → Telemetrija; letva svoj račun u kartici |
| Krivulje protoka i snimke korita iz HIS-2000 | Administracija → Unos u arhivu |
| Gradnja letve nakon uvoza | sama, u pozadini, nakon svakog upisa |
| Ulaganje operativnih očitanja u arhivu i pospremanje | Administracija → Unos u arhivu → Očitanja iz programa |
| Izdavanje paketa arhive | Administracija → Unos u arhivu → Izdanja arhive |
| Satno izdavanje prognoze | samo, svaki krug; gumb Generiraj na stranici Prognoze |
| Priprema modela: namještanje lanca, modeli ispuštanja elektrana, zapis promašaja, ponovno izdavanje | Prognoze → Pripremi model (globalni administrator) |

Svi uvozi idu redom pregled pa potvrda, uz provjeru prava na svaku letvu.
Uvoz posebnih izvora dijeli kod s alatom `uvoz-izvora` (paket
`internal/uvoz/izvori`), a priprema modela s alatima `namjesti-prognozu` i
`provjeri-prognozu` (`prognoza.NamjestiLanac`, `prognoza.ProvjeriUnatrag`).
Na pravim uzorcima novi kod zapisuje iste datoteke i iste tablice kao stari
programi; godišnjaci RHMZ-a koje imamo su skenovi bez teksta, pa taj čitač na
stvarnom godišnjaku nije provjeren.

## tools/admin — administracija poslužitelja

Isti posao kao u aplikaciji, iz terminala, za masovnu pripremu ili rad na
poslužitelju.

| Alat | Posao | Piše u |
|---|---|---|
| `arhiva-vodostaja` | Gradi arhivsku bazu iz stabla `vodostaji/`, cijelu ili jednu letvu | `data/vodostaji.db` |
| `paket-arhive` | Izdaje arhivu kao `.cop` pakete s katalogom | `pakete/` |
| `ulozi-ocitanja` | Ulaže operativna očitanja u arhivski niz | `vodostaji/` |
| `izracunaj-prognozu` | Izdaje satnu prognozu | `data/prognoze.db` |
| `namjesti-prognozu` | Namješta lanac; `-proba "letva = ulaz + ulaz"` isprobava postavku | `data/prognoze.db` |
| `provjeri-prognozu` | Provjera unatrag; `-zapisi` upisuje promašaje, `-csv` parove | `data/prognoze.db` |
| `uvoz-izvora` | Posebni izvori; zamjenjuje sedam nekadašnjih uvoznika | `vodostaji/` |
| `uvoz-hidroview` | Povijest s HydroViewa | `vodostaji/` |
| `upis-hidroview-racuna` | Upis računa čvora, lozinka iz okoline | `data/gocop.db` |

## tools/migrations — jednokratni prijenosi i upisi registra

Upisi kroz servis ostavljaju zapis u knjizi verzija.

| Alat | Posao | Piše u |
|---|---|---|
| `selidba-arhive` | Miče povijesne nizove iz operativne baze u arhivu; `-izvedi` briše | obje baze |
| `spoji-datoteke` | Spaja godišnje datoteke niza u jednu; `-stvarno` | `vodostaji/` |
| `prijepis-dionica` | Iz wikija slaže `data/sections.json` | `data/` |
| `upis-dionice` | Upisuje dionice iz prijepisa | `data/gocop.db` |
| `upis-letve` | Popunjava karticu postaje iz plana i elaborata | `data/gocop.db` |
| `upis-djelatnika` | Upisuje djelatnike | `data/gocop.db` |
| `uvoz-vodostaja` | CSV uz bazu u operativna očitanja | `data/gocop.db` |
| `uvoz-mts` | Skladišta i početno stanje sredstava | `data/gocop.db` |
| `uvoz-dnevnika-cop` | Digitalizirani dnevnici COP-a (osobni podaci) | `data/gocop.db` |
| `uvoz-iors` | Obrasci IORS u plan dežurstava (osobni podaci) | `data/gocop.db` |

## tools/diagnostics — provjere i usporedbe

| Alat | Posao |
|---|---|
| `provjeri-valove` | Prognoza kroz zadane poplavne valove; piše sažetak koji stranica O prognozi pokazuje |
| `usporedi-prognoze` | Naš model u trenucima tuđih prognoza |
| `provjeri-dnevnu`, `usporedi-dnevnu` | Dnevni model na izdvojenom razdoblju i prema mađarskoj prognozi |
| `usporedi-rkm` | Stacionaže postaja prema geometriji i ENC sidrima |
| `proba-hidroview` | Što HydroView nudi za postaju, samo čitanje |

Tuđe prognoze za usporedbe čitaju se iz izvorne građe u
`~/Downloads/goCOP-izvori/tude-prognoze/`.

## Ostalo u tools/

| Mapa | Sadržaj |
|---|---|
| `tools/testdata/probna-izvjesca` | S `-upisi` upisuje probne dionice i izvješća u lokalnu bazu |
| `tools/analysis` | Istraživačke analize (akumulacije) |
| `tools/geo` | Priprema geometrije iz OSM-a i ENC-a |
| `tools/internal/hvracun` | Otključavanje HydroView računa čvora za alate |

## Izvorna građa

Preuzeti izvori (izvozi HIS-2000, pristupi letva.voda.hr, godišnjaci RHMZ-a,
izvornici eHYD, ARSO, GKD, PEGELONLINE, viadonau, geometrija, studije) stoje
izvan projekta u `~/Downloads/goCOP-izvori/`, složeni po izvoru, s opisom u
tamošnjem README-u.
