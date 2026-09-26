# Plan razdvajanja aplikacije i pomoćnih alata

Datum: 26. 9. 2026. Status: provedeno. Repozitorij nosi samo aplikaciju
(`cmd/gocop`, `internal`, `web`); alati su lokalno u `tools/`, izvan Gita.
Stanje i namjena svakog alata u [katalogu alata](katalog-alata.md).

## Dnevnik provedbe

- 26. 9., šesti korak (korak 3 i 4 plana): uvoz i priprema prognoze
  prebačeni u aplikaciju, alati izvan repozitorija.
  - Posebni izvori u Administracija → Unos u arhivu: HIS-2000 (izvoz postaje
    ili ZIP), ARSO, eHYD, GKD, PEGELONLINE, SEBA i godišnjaci RHMZ-a, redom
    pregled pa potvrda, uz pravo na svaku letvu i gradnju letve u pozadini.
    Zajednički paket `internal/uvoz/izvori`; posao HIS-2000 preseljen iz
    naredbe u `internal/uvoz/his2000`, godišnjaci u `internal/uvoz/godisnjak`.
  - Povijest s HydroViewa u istom odjeljku (`internal/uvoz/hvpovijest`),
    računom iz Administracija → Telemetrija.
  - Prognoze → Pripremi model (globalni administrator): konfiguracija lanca i
    namještanje preseljeni u `internal/prognoza/lanac.go`, provjera unatrag
    u `internal/prognoza/provjera.go`; posao namjesti lanac, zapiše promašaje
    i odmah ponovno izda prognozu. Namještanje ima vlastitu bravu, jer rijetke
    letve na trenutak mijenjaju zajedničke granice.
  - Provjera na pravim uzorcima, stari prema novom programu: ARSO (Ptuj),
    eHYD (Angern), GKD (Hofkirchen), PEGELONLINE (Kienstock), HIS-2000 (Dalj,
    iz mape i iz ZIP-a, i ponovljeni uvoz) daju identične datoteke, 21 od 21;
    povijest s HydroViewa (Popovac) identična. Namještanje i provjera unatrag
    na dvije kopije baze prognoza daju identične tablice (354 pojasa, 439
    ulaza, modeli elektrana, 3524 promašaja) i identičan ispis. SEBA nema
    pravog uzorka, pokriva je test; godišnjaci koje imamo su skenovi bez
    teksta, pa ih ni stari program nije znao pročitati.
  - Svi programi osim `cmd/gocop` preseljeni pod `tools/admin`,
    `tools/migrations`, `tools/diagnostics` i `tools/testdata`; `tools/` je
    na `.gitignore`. Sedam uvoznika posebnih izvora zamijenio je jedan alat
    `uvoz-izvora`; kompatibilni omotači u `cmd/` i `scripts/` obrisani, kao i
    privremene provjere rasporeda. Paket `internal/hidroview/racun`, koji su
    koristili samo alati, preseljen u `tools/internal/hvracun`.
  - Obrisane prazne mape `cmd/tmp-*` i `cmd/privremeno-*`. Preuzeta izvorna
    građa (1,2 GB) premještena iz `vodostaji/` i `data/tude` u
    `~/Downloads/goCOP-izvori/`, složena po izvoru; arhiva je nije čitala.

- 26. 9., peti korak: dovršen inventar, izlaz je `docs/katalog-alata.md`.
  Svi programi iz `cmd/` razvrstani su po poslu, cilju upisa i tome zna li
  isti posao aplikacija. Raspoređenih poslova na računalu nema.
  Glavni nalaz za korak 3: šest uvoza u arhivu (ARSO, eHYD, GKD,
  PEGELONLINE, SEBA, povijest HydroViewa) postoji samo kao zaseban program,
  a HIS-2000 nizove i godišnjake RHMZ-a aplikacija također ne zna. Priprema
  prognoze (`namjesti-prognozu`, `provjeri-prognozu -zapisi`) nije moguća iz
  aplikacije, a živa prognoza o njoj ovisi.
- `proba-hidroview` premješten pod `tools/diagnostics/` uz paket
  `hidroviewproba` i kompatibilni stari ulaz. Stari, novi i kompatibilni
  program daju iste opcije i identičan izlaz na živom HydroViewu (postaja
  Tikveš, samo čitanje).
- `provjeri-dnevnu` i `usporedi-dnevnu` dodatno provjereni na pravoj arhivi
  (učenje do 2012., provjera do 2025. s vrhovima valova; usporedba s
  mađarskim prognozama): izlaz identičan staroj izgradnji iz Gita, osim
  redoslijeda ispisa međuslivova u prvom retku, koji je nasumičan i u staroj
  izgradnji. Baze otvorene samo za čitanje, hash nepromijenjen.
- Pad `TestKodiranjeIDekodiranje` riješen u testu, ne u okruženju: dekoder
  se više ne traži na čvrsto upisanoj putanji iz stare radne mape, nego iz
  `GOCOP_QR_PYTHON`. Bez varijable provjerava se oblik koda i to se ispiše;
  kad je zadana, a OpenCV se ne učita, test pada s razlogom. Provjereno sva
  tri slučaja; s ispravnim OpenCV-om svih pet kodova dekodira se natrag.
  Staroj virtualnoj okolini nedostaje `pyvenv.cfg`, zato nije vidjela `cv2`.

- 26. 9., četvrti korak: `provjeri-dnevnu` i `usporedi-dnevnu` premješteni
  pod `tools/diagnostics/`, uz zajedničke pakete `dnevnaprovjera` i
  `dnevnausporedba` te kompatibilne stare `cmd/` ulaze. Izračuni nisu mijenjani.
- Dodani testovi CSV čitača i ponovljivi `tools/test_dnevni_cli.py`.
  Izlaz i CLI opcije originalnih, novih i kompatibilnih izvršnih datoteka
  jednaki su na praznoj sintetičkoj arhivi; hash baze ostao je isti.
  To nije puna numerička provjera modela na bogatom nizu podataka.
- Izgradnja, vet i ciljani testovi prolaze, goCOP nema `tools` ovisnosti.
  Puna provjera preko RTK-a: 812 prošlih, 1 neuspjeli, 56 preskočenih testova.
  Pad `TestKodiranjeIDekodiranje`: stari privremeni Python iz `qr_test.go`
  postoji, ali nema `cv2` (potvrđen `ModuleNotFoundError`). QR kod i okruženje
  nisu mijenjani; puni skup trenutačno nije zelen. Razriješiti prije nastavka
  većih zahvata, bez skrivanja pada isključivanjem testa.
- `provjeri-valove`, `provjeri-prognozu` i `usporedi-prognoze` koriste
  `prognoza.Otvori`, koji izvršava shemu i usklađivanje: nisu strogo read-only.
  Ostaju nepremješteni do dovršenog pregleda; ne pokretati nad živom bazom.

- 26. 9., treći korak: `usporedi-rkm` ima novi ulaz u
  `tools/diagnostics/usporedi-rkm`, zajedničku implementaciju i testove u
  `tools/diagnostics/rkm`. Stari `cmd/usporedi-rkm` ostaje tanak ulaz u isti
  paket. Izračuni i zadane putanje nisu mijenjani.
- Izgrađeni program prije premještanja uspoređen s oba nova ulaza na
  sintetičkoj SQLite bazi, iz druge radne mape: obični i ENC izlaz identični,
  hash baze nepromijenjen. `go list -deps ./cmd/gocop` ne sadrži `gocop/tools/`.
  Izgradnja aplikacije i `go vet ./...` prolaze; puni Go testovi također.

- 26. 9., drugi korak: `prepare_geometrija.py` premješten u `tools/geo/`,
  `analiza_akumulacija.py` u `tools/analysis/`, uz identične implementacije
  i stare CLI preusmjerivače. Natural Earth postupak označen kao stari i
  rizičan za postojeću detaljniju geometriju; nije pokretan. Osobna putanja
  analize zasad sačuvana radi odvajanja premještanja od promjene ponašanja.
- `tools/test_layout.py` provjerava četiri preusmjerivača i Python sintaksu
  bez pristupa bazi/mreži; pokretanje: `.venv/bin/python tools/test_layout.py`.
  Izgradnja glavnog programa i `go vet ./...` prošli su i nakon ovog koraka.

- 26. 9.: `extract_enc_rkm.py` i `update_osm_waterway.py` premješteni iz
  `scripts/` u `tools/geo/`, sadržaj identičan izvorniku iz Gita. Stare
  putanje zadržane kao CLI preusmjerivači zbog mogućih vanjskih pozivatelja.
- Ažurirane ENC upute i dodani README-i pomoćnog dijela. Ugrađeni resursi,
  Go kod, baze i živi poslužitelj nisu mijenjani.
- Početni `go test ./...` prolazi uz dopuštenje za lokalne testne HTTP
  poslužitelje; sandbox bez toga blokira mrežne testove.
- Provjerena oba CLI ulaza iz druge radne mape i identičan OSM izlaz na
  sintetičkom lokalnom uzorku. ENC puni izvoz nije ponovno pokretan.
- Inventar vanjskih raspoređenih poslova i preostalih alata još nije dovršen;
  stari ulazi se zato ne uklanjaju. Ostali Go alati još nisu premješteni.

## Cilj i granice

Jedan repozitorij i zajednička poslovna logika, ali jasna razlika između
proizvoda, administratorskih alata i razvojnih/migracijskih postupaka.
Korisnički čvor ne treba Python ni razvojne alate za svakodnevni rad.
Automatsko preuzimanje, prognoziranje, arhiviranje i sinkronizacija ostaju
funkcije aplikacije. Ne izdvajati ih samo zato što imaju i CLI naredbu.

Korisnik je dodatno odredio: sve potrebno glavnom programu, uključujući
uvoz/izvoz pokrenut iz aplikacije, pripada goCOP-u. Takvi postupci ne smiju
zahtijevati zaseban pomoćni program. Dijeljena logika ostaje u `internal`,
sučelje u `web`, a pokretanje u `cmd/gocop`; to ne znači spajanje svega u
jednu datoteku. Samo postupci izvan aplikacijskog tijeka idu pod `tools`,
razvrstani po namjeni, ne po programskom jeziku.

Ovim planom ne odobrava se brisanje starih podataka, prebacivanje privatnih
uvoznika u javni Git, promjena baza, restart ni prekid aktivnih uvoza.

## Zatečeno stanje

- `cmd/gocop` pokreće aplikaciju; Docker gradi i isporučuje samo taj program.
- `internal` sadrži logiku aplikacije i dijeljene biblioteke; `web` sučelje.
- Ostali `cmd` programi zasebni su Go alati, nisu Python skripte.
- Glavni program još ima posebne BP16/Directus, CSV i ugovorne uvozne opcije.
- `namjesti-prognozu` priprema bazu potrebnu prognoziranju, nije samo proba.
- `scripts` sadrži pripremu geometrije, OSM, ENC i analizu akumulacija.
- `data`, `vodostaji` i `pakete` radni su podaci, ne izvorni kod.
- `.gitignore` namjerno isključuje uvoznike COP dnevnika i IORS-a.
- U korijenu postoje izvršne datoteke; buduće izgradnje usmjeriti u `bin`.

## Ciljno stablo projekta

Ovo je dogovoreni cilj, ne tvrdnja da su datoteke već premještene.

```text
goCOP/
├── cmd/gocop/           # jedini glavni ulaz aplikacije
├── internal/            # logika aplikacije, uvoz/izvoz i zajednički servisi
├── web/                 # ugrađeno sučelje i statički resursi
├── tools/               # neobvezni pomoćni postupci, prema namjeni
│   ├── admin/           # samostalni servisni omotači
│   ├── migrations/      # jednokratni prijenosi izvan aplikacijskog tijeka
│   ├── diagnostics/     # provjere i usporedbe
│   ├── analysis/        # istraživanja i analize
│   ├── geo/             # priprema geografskih podataka
│   └── testdata/        # generatori sintetičkih probnih podataka
├── docs/                # tehnička dokumentacija, plan i katalog alata
├── bin/                 # lokalne izgradnje, izvan Gita
├── data/                # lokalne baze i postavke, izvan Gita
├── vodostaji/           # lokalna izvorna građa, izvan Gita
└── pakete/              # lokalni .cop paketi, izvan Gita
```

Go moduli, Dockerfile, licence i kratki README ostaju u korijenu.
Testovi i mali sigurni testni uzorci ostaju uz pripadajuće pakete; `tools/testdata`
nije skladište stvarnih osobnih podataka. Podaci potrebni za `go:embed`
ostaju uz pakete aplikacije. Ne uvoditi zaseban Go modul za alate bez potrebe.

## Predložena podjela

| Cjelina | Sadržaj i primjeri | Pravilo |
|---|---|---|
| Aplikacija | `cmd/gocop`, pripadajući `internal`, `web` | Sve za redovan rad čvora |
| Operativna administracija | Priprema i izračun prognoze, arhiviranje, izdavanje paketa, unos registara | Sve potrebno aplikacijskom tijeku integrirati u goCOP; samostalni servisni omotači mogu u `tools/admin/` |
| Uvoz/izvoz aplikacije | Podržani formati i izvori, uključujući HIS2000, eHYD, GKD, ARSO, SEBA, MTS i HydroView prema inventaru | Servis i aplikacijski tijek u goCOP-u; bez obvezne vanjske izvršne datoteke |
| Migracije/priprema | `selidba-arhive`, `prijepis-dionica`, posebni BP16 postupci koji nisu dio aplikacijskog uvoza | `tools/migrations/`; preduvjeti, sigurnosna kopija i povrat |
| Dijagnostika/istraživanje | `provjeri-*`, `usporedi-*`, `proba-hidroview`, `probna-izvjesca`, `analiza_akumulacija.py` | Izvan korisničke distribucije; probni upisi samo u testnu bazu |
| Geografski alati | `prepare_geometrija.py`, `update_osm_waterway.py`, `extract_enc_rkm.py` | Priprema verzioniranih podataka, ne ovisnost poslužitelja |
| Za pojedinačni pregled | `spoji-datoteke`, lokalni `tmp-*` i `privremeno-*` | Utvrditi sadržaj, korisnike i potrebu prije razvrstavanja |

Podjela je početna: svaki alat treba pregledati prije preseljenja. Ne zaključivati
da je mapa aktivan alat samo po nazivu ili da je sadržaj siguran za objavu.

## Redoslijed provedbe

### 1. Inventar i ovisnosti — prvo

- [x] Popisati praćene, ignorirane i lokalne alate, uključujući prazne mape.
- [x] Za svaki navesti svrhu, ulaze/izlaze, mijenja li bazu, koristi li knjigu
  verzija, vanjske ovisnosti, tajne, vlasnika postupka i testove.
- [x] Pronaći pozive iz dokumentacije, automatizacija, raspoređenih poslova i
  drugih alata; zabilježiti radnu mapu i relativne putanje.
- [ ] Provjeriti trenutačno aktivne uvoze i dogovoriti termin promjene.
- [x] Zabilježiti početni rezultat testova i izgradnje.

Izlaz: katalog alata s konačnom klasifikacijom i popisom pozivatelja
([katalog-alata.md](katalog-alata.md)). Aktivne uvoze i termin promjene
treba dogovoriti prije premještanja alata koji pišu.

### 2. Odvojena izgradnja i distribucija — bez promjene putanja koda

- [ ] Uvesti odvojene ciljeve za aplikaciju, podržane alate i provjere;
  izlaz u `bin/`, bez automatskog uključivanja svih naredbi u izdanje.
- [ ] Izdanje aplikacije sadrži program, potrebne licence i kratke upute;
  samo neobvezni servisni alati izdaju se zasebno uz kompatibilnu verziju.
- [ ] Provjeriti Docker kontekst i izuzimanje baza, ključeva, arhiva,
  virtualnih okruženja, lokalnih binarnih datoteka i privatnih uvoznika.
- [ ] Provjeriti rad aplikacije iz čiste instalacijske mape, ne samo iz repozitorija.

### 3. Zaokružiti uvoz/izvoz i operativne postupke u goCOP-u

- [x] Za svaki uvoz/izvoz utvrditi je li dio aplikacijskog tijeka. Ako jest,
  zadržati ili integrirati njegovo pokretanje i logiku u goCOP.
- [x] CSV, ugovorni i BP16/Directus uvoz ne izdvajati automatski: samo dokazano
  jednokratne postupke izvan aplikacije premjestiti među migracije.
- [x] Ukloniti potrebu ručnog pokretanja vanjskog programa za nužnu pripremu
  prognoze ili drugi redovni postupak; definirati pokretanje u goCOP-u s
  odgovarajućim ovlastima, statusom posla i obradom pogrešaka.
- [x] Zadržati jednu implementaciju poslovnih pravila u `internal`; ne kopirati
  logiku između aplikacije i alata.
- [x] Osigurati zamjenske naredbe i ažurirati sve pozivatelje prije uklanjanja
  starih opcija. Po potrebi prijelazno upozorenje umjesto naglog prekida.

### 4. Urediti pomoćni dio

- [x] Ciljno zadržati `cmd/gocop` kao glavni ulaz proizvoda. Stare putanje
  pomoćnih naredbi po potrebi privremeno zadržati kao kompatibilne omotače.
- [x] Pomoćne alate, neovisno o jeziku, rasporediti prema vrsti:
  `tools/admin/`, `tools/migrations/`, `tools/diagnostics/`,
  `tools/analysis/`, `tools/geo/` i `tools/testdata/`.
- [ ] Svaka skupina ima kratki README sa svrhom, pokretanjem, ovisnostima
  i upozorenjem piše li u bazu. Ne stvarati prazne kategorije bez potrebe.
- [ ] Migracije držati u jasno označenom pomoćnom dijelu; privatne uvoznike
  zadržati izvan javnog repozitorija. Prilagoditi ignore pravila prije premještanja.
- [ ] Parametrizirati osobne apsolutne putanje (postoje u analizi akumulacija).
- [ ] Za staru Natural Earth pripremu provjeriti odnos prema novom OSM/ENC
  postupku; ne dopustiti slučajno prepisivanje kvalitetnije geometrije.
- [ ] `tmp-*`/`privremeno-*` pregledati pojedinačno. Brisanje tek uz zasebno
  odobrenje i provjeru da rezultat nije jedini sačuvani primjerak.

### 5. Sigurnost i operativna upotrebljivost alata

- [ ] Alati koji pišu moraju jasno prikazati ciljnu bazu i opseg izmjene;
  podržati probni način gdje je izvedivo. Probni način ne smije potajno
  mijenjati shemu bez jasne najave.
- [ ] Registri se mijenjaju kroz podržane servise i knjigu verzija;
  iznimke za migracije posebno dokumentirati i testirati.
- [ ] Testirati ponovljeni uvoz, prekid i nastavak, sukob s aktivnim
  poslužiteljem te ponašanje na starijoj shemi baze.
- [x] Priprema prognoze mora biti dokumentiran postupak unutar goCOP-a;
  pomoćni CLI može ostati neobvezan, ali ne jedini način pripreme aplikacije.

### 6. Provjera i dokumentacija

- [ ] `go test ./...`, `go vet ./...`, izgradnja aplikacije i podržanih alata;
  ciljane provjere Python alata nad uzorcima, ne živim podacima.
- [ ] Provjeriti prijavu, javna očitanja, prognozu, arhiviranje i sinkronizaciju
  u testnom okruženju bez `tools` i Pythona.
- [ ] Usporediti rezultate odabranih uvoza prije/poslije na kopijama baze.
- [ ] Provjeriti sve automatizacije nakon promjene putanja.
- [ ] README ostaje kratak za instalaciju; pomoć opisuje radne postupke,
  katalog alata održavanje, a migracijske upute zasebne preduvjete i povrat.
- [ ] Pregledati Git diff zbog privatnih podataka; ne raditi automatski push.

## Završni kriterij

### Obvezna zaštita glavne aplikacije pri svakom premještanju

Korisnikov izričit zahtjev: posebno provjeriti da reorganizacija ne pokvari
glavnu aplikaciju. Uspješna kompilacija sama nije dovoljna.

- [ ] Prije promjene napraviti mapu ovisnosti: Go importi (`go list -deps
  ./cmd/gocop`), `go:embed`, učitavanje datoteka u radu, vanjske naredbe,
  relativne putanje, konfiguracija, testni uzorci i raspoređeni poslovi.
- [ ] Za svako premještanje zapisati staru/novu putanju, pozivatelje i provjeru
  koja dokazuje da funkcija i dalje radi. Ako ovisnost nije razjašnjena,
  taj dio zasad ne premještati.
- [ ] Odvojiti čisto premještanje od promjene ponašanja/integracije novih
  funkcija; provjeravati male cjeline, ne jedan veliki zahvat.
- [ ] Zabilježiti početno stanje testova; nakon svake cjeline pokrenuti
  ciljane testove i izgradnju goCOP-a, na kraju cijeli skup provjera.
- [ ] Pokrenuti izgrađeni goCOP iz čiste mape bez izvornog koda, `tools`
  i Pythona, na izoliranoj konfiguraciji/bazi i drugim portovima. Isključiti
  stvarne obavijesti i vanjsku sinkronizaciju; ne koristiti identitet živog čvora.
- [ ] Provjeriti prijavu i ovlasti, stranice i ugrađene resurse, karte,
  aplikacijski uvoz i izvoz te preuzimanje nastalih datoteka. Za pogođene
  formate usporediti sadržaj, broj zapisa, jedinice i metapodatke prije/poslije.
- [ ] Provjeriti pozadinske poslove, pripremu/izračun prognoze, izdavanje i
  čitanje arhivskih paketa te sinkronizaciju između izoliranih testnih čvorova.
- [ ] Ako nedostaje test za pogođeni radni tijek, dodati regresijski test
  prije premještanja. Neprovjerene vanjske integracije izričito označiti.
- [ ] Prije prihvaćanja pregledati diff radi nenamjernih promjena SQL-a,
  formata podataka, identiteta čvora, ovlasti i zadanih postavki.
- [ ] U slučaju regresije zaustaviti sljedeća premještanja i vratiti samo
  vlastitu posljednju cjelinu ili je popraviti; ne dirati paralelne tuđe izmjene.

Reorganizacija nije završena dok pogođeni tijekovi nisu provjereni.
Živi poslužitelj ne restartati kao zamjenu za izolirano testiranje.

Običan čvor radi iz isporučenog programa bez pomoćnih skripti. Administrator
ima dokumentiran, verzioniran skup alata. Jednokratne migracije i istraživanja
ne ulaze u korisničko izdanje. Postojeći podaci, automatizacije i poslovna
pravila ostaju očuvani; ništa se ne briše samo zato što izgleda staro.
