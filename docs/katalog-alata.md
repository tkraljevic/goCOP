# Katalog alata

Stanje 3. 10. 2026.; razdvajanje aplikacije i alata provedeno je 26. 9. (vidi
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
| Dostava paketa arhive drugim čvorovima | sama, razmjenom: kazalo drže svi čvorovi, a paket dohvaća čvor koji prati sve ili ima pravilo „Sve vrste” bez godine početka i s punim sadržajem (pretplatu „Arhiva vodostaja” program zasad odbija spremiti) |
| Skupljanje kiše sa stvarnih kišomjera (DHMZ s letva.voda.hr, pljusak.com), dnevno ulaganje u arhivu i izdavanje paketa promijenjenih kišomjera | samo, svaki krug, na čvoru koji izdaje prognozu (ostali kišu primaju razmjenom); u arhivu se ulaže samo na čvoru sa stablom izvornih datoteka |
| Satno izdavanje prognoze | samo, svaki krug, na čvoru s ulogom „Izdaje prognozu” (Administracija → Čvor, mreža i sinkronizacija → Uloge ovog čvora); ostali izdanje primaju razmjenom. Gumb Generiraj: Prognoze → Postavke |
| Priprema modela: namještanje lanca, modeli ispuštanja elektrana, zapis promašaja, ponovno izdavanje | Prognoze → Postavke → Pripremi model (globalni administrator, na čvoru koji izdaje prognozu) |

Svi uvozi idu redom pregled pa potvrda, uz provjeru prava na svaku letvu.
Uvoz posebnih izvora dijeli kod s alatom `uvoz-izvora` (paket
`internal/uvoz/izvori`), a priprema modela s alatima `namjesti-prognozu` i
`provjeri-prognozu` (`prognoza.NamjestiLanac`, `prognoza.ProvjeriUnatrag`).
Na pravim uzorcima novi kod zapisuje iste datoteke i iste tablice kao stari
programi; godišnjaci RHMZ-a koje imamo su skenovi bez teksta, pa taj čitač na
stvarnom godišnjaku nije provjeren.

## tools/admin — administracija poslužitelja

Uglavnom isti posao kao u aplikaciji, iz terminala, za masovnu pripremu ili
rad na poslužitelju. `zamolba-podaci` i `pismo-rhmz` slažu nacrte dopisa
službama, što aplikacija ne radi.

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
| `arso-dopuna` | Jednom dopunjuje očitanja slovenskih letava iz ARSO-ovih tablica za 7 ili 30 dana; postojeća preskače | `data/gocop.db` |
| `izdaj-letvu` | Isto što `paket-arhive` (jedna letva ili cijela arhiva), ali bez `-probno=false` samo provjeri | `pakete/` |
| `zamolba-podaci` | Nacrt zamolbe mađarskoj strani za hidrološke podatke, s memorandumom centra | `.docx` (`-o`) |
| `pismo-rhmz` | Nacrt neslužbenog pisma RHMZ-u Srbije, s istim memorandumom | `.docx` (`-o`) |
| `potpis-izdanja` | Ključ izdanja (`kljuc`, jednom) i potpis `SHA256SUMS` nacrta izdanja na GitHubu (`potpisi v0.0.x-alfa -objavi`); bez potpisa Postava izdanje ne vidi | `~/.config/gocop/kljuc-izdanja`, izdanje na GitHubu |

## tools/migrations — jednokratni prijenosi i upisi registra

Upisi kroz servis ostavljaju zapis u knjizi verzija.

| Alat | Posao | Piše u |
|---|---|---|
| `selidba-arhive` | Miče povijesne nizove iz operativne baze u arhivu; `-izvedi` briše | obje baze |
| `spoji-datoteke` | Spaja godišnje datoteke niza u jednu; `-stvarno` | `vodostaji/` |
| `prijepis-dionica` | Iz wikija slaže `data/sections.json` | `data/` |
| `upis-dionice` | Upisuje dionice iz prijepisa | `data/gocop.db` |
| `upis-letve` | Popunjava karticu postaje iz plana i elaborata | `data/gocop.db` |
| `upis-pljusak-kisomjera` | Upisuje postaje pljusak.com iz međuslivova Drave i Mure kao stvarne kišomjere, iz pripremljenog JSON popisa | `data/gocop.db` |
| `upis-stvarnih-kisomjera` | Upisuje DHMZ-ove kišomjere u slivu Drave kao stvarne meteorološke postaje, izvedenim točkama upisuje izvor | `data/gocop.db` |
| `upis-djelatnika` | Upisuje djelatnike | `data/gocop.db` |
| `uvoz-vodostaja` | CSV uz bazu u operativna očitanja | `data/gocop.db` |
| `uvoz-mts` | Skladišta i početno stanje sredstava | `data/gocop.db` |
| `uvoz-dnevnika-cop` | Digitalizirani dnevnici COP-a (osobni podaci) | `data/gocop.db` |
| `uvoz-iors` | Obrasci IORS u plan dežurstava (osobni podaci) | `data/gocop.db` |
| `upis-letava-iz-plana` | Letve koje plan obrane navodi uz dionice, a registar ih nema; postojeće ne dira | `data/gocop.db` |
| `upis-stranih-letava` | Strane letve iz mađarske i srpske prognoze te uzvodne slovenske i austrijske, s javnim izvorom i napomenom za pregled | `data/gocop.db` |
| `kote-iz-ehyd` | Austrijskim letvama kota nule iz eHYD-a; letvu koja kotu ima ne dira | `data/gocop.db` |
| `koordinate-letve` | Koordinate letve i napomena odakle su | `data/gocop.db` |
| `polozaj-letava` | Koordinate službi (ARSO, DanubeHIS) i riječni kilometar po toku iz OSM-a, za letve na Muri upisane u alatu | `data/gocop.db` |
| `napomena-pregleda` | Napomena za pregled zadanim letvama | `data/gocop.db` |
| `letva-voda` | Veže letve na vodu iz registra; `-dodatne` kao mjerodavne s druge vode | `data/gocop.db` |
| `tok-vode` | Tok vode iz GeoJSON-a, uz istu provjeru kao na stranici vode | `data/gocop.db` |
| `ispravak-dionice` | Izvozi dionicu s objektima i letvama u JSON i vraća ispravak kroz servis | `data/gocop.db` |
| `upis-danubehis-kisomjera` | Kišomjeri AT, HU, SI i SK s DanubeHIS-a kao stvarni kišomjeri, dnevna oborina u stablo (izvor `kisomjer-danubehis`); živo se ne preuzimaju | `data/gocop.db`, `vodostaji/` |
| `uvoz-his2000-letve` | Dopunjuje stablo izvozima HIS-2000 jedne letve (satni ili dnevni nizovi, krivulje protoka) i gradi letvu; isti čitač kao aplikacija | `vodostaji/`, `data/vodostaji.db` |
| `arhiviraj-izvjesca` | Arhivira zadana dnevna i sektorska izvješća | `data/gocop.db` |
| `arhiviraj-probne-dionice` | Arhivira probne dionice („(probna dionica)”) s njihovim izvješćima | `data/gocop.db` |

Bez potvrdne zastavice odmah pišu `koordinate-letve`, `letva-voda`,
`tok-vode`, `upis-dionice`, `upis-letve`, `upis-djelatnika`, `uvoz-vodostaja`
i `uvoz-dnevnika-cop` (za probu `-probno`) te `prijepis-dionica`.
`ispravak-dionice` piše s `-upis`, `selidba-arhive` s `-izvedi`,
`spoji-datoteke` sa `-stvarno`, a `uvoz-mts` i `uvoz-iors` s `-upisi`. Ostali
zadano samo ispisuju i pišu tek s `-probno=false`.

## tools/diagnostics — provjere i usporedbe

| Alat | Posao |
|---|---|
| `provjeri-valove` | Prognoza kroz zadane poplavne valove; piše sažetak koji stranica O prognozi pokazuje |
| `usporedi-prognoze` | Naš model u trenucima tuđih prognoza |
| `provjeri-dnevnu`, `usporedi-dnevnu` | Dnevni model na izdvojenom razdoblju i prema mađarskoj prognozi |
| `usporedi-rkm` | Stacionaže postaja prema geometriji i ENC sidrima |
| `proba-hidroview` | Što HydroView nudi za postaju, samo čitanje |
| `makni-nizove` | Miče iz arhive zadane nizove kojima je datoteka nestala iz stabla i ponovno spaja letvu. **Piše** u `data/vodostaji.db`; jedan po jedan isto radi Administracija → Unos u arhivu → Nizovi bez datoteke u stablu → Makni |
| `popis-stranih` | Postaje iz mađarske i srpske prognoze i šifra koju im dajemo |
| `proba-javnih` | Čita zadane adrese javnih stranica čitačima programa, ništa ne zapisuje |
| `proba-danubehis` | Zadnje satno očitanje mađarskih rezervi s DanubeHIS-a |
| `proba-vizugy-protok` | Spremljene stranice vizugy.hu: koliko sati vodostaja i protoka |
| `proba-godisnje` | Godišnji vodostaji letve iz arhive i trajanje upita |
| `proba-kisa-slivova`, `proba-kisa-slivova-povijest` | Pragovi kiše po međuslivovima i razine pločice na naslovnoj, sada ili zadanih dana |
| `proba-pricuvno` | Veze pričuvnog izračuna bez poslužitelja; s putanjom zapiše i Excel |
| `proba-prijenosa` | Naučeni odnosi srpskih letvi prema našima |
| `proba-razine` | Model ispuštanja HE s razinom akumulacije i bez nje |
| `protok-u-cm` | Parove iz `provjeri-prognozu -csv` preračunava iz protoka u vodostaj |
| `proba-sesije` | Ispiše sesiju prijave iz baze po ID-u |
| `tunel-proba` | Spaja se na čvor kroz web tunel ključem ovog čvora i javlja je li druga strana dokazala upareni ključ |
| `ikona-proba` | Ploča ikone u svim stanjima i veličinama, uvećane sitne ikone i `.ico` u zadanu mapu, za pregled; `<putanja.png> -png` zapiše ikonu 256 px (`web/static/img/gocop-256.png`, ikona spremnika na Unraidu; pokretati s `GOARCH=amd64`) |
| `proba-nadogradnje` | Nadogradnja na kopiji baze: shema, jesu li ID-ovi korisnika ostali isti, novosti, popravci i obnova površine. **Piše** u zadanu bazu; pokretati samo nad kopijom |

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
izvornici eHYD, ARSO, GKD, PEGELONLINE, viadonau, DanubeHIS, pljusak.com,
DHMZ-ova meteorologija, oborine Open-Meteo, geometrija, studije) stoje
izvan projekta u `~/Downloads/goCOP-izvori/`, složeni po izvoru, s opisom u
tamošnjem README-u.
