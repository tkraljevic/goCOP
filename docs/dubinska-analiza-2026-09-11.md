# Dubinska analiza projekta goCOP

## Sažetak

goCOP je prerastao početni CRUD sustav i postao ozbiljna domenska platforma za
obranu od poplava: povezuje registre, operativna očitanja, obrambene epizode,
građevinske dnevnike, hidrološku arhivu i razmjenu između samostalnih čvorova.
Njegova najveća vrijednost nije pojedinačna funkcija nego dosljedna podjela na
operativne podatke, obnovljivu hidrološku arhivu i prenosiva izdanja.

Aktualna arhitektura ima dobre temelje za betu: mali broj vanjskih ovisnosti,
čisti Go i SQLite, rad bez interneta, verzioniranje poslovnih zapisa, granularne
ovlasti, determinističke identitete i testove koji pokrivaju velik dio domenske
logike. Na pregledanom stanju prolaze `go test ./...`, `go vet ./...` i ciljani
`go test -race` za `internal/poslovi`, `internal/arhiva` i `internal/web`.

Prije nego `.cop` paketi i pozadinski poslovi postanu automatski distribucijski
sustav, postoje četiri obvezne točke: stroga serverska provjera ovlasti za paket,
atomska ugradnja arhive, vjerodostojna provjera prije brisanja operative te
potpisivanje i zaštita od povratka na starije izdanje. Najnoviji sloj
dodaje izdavanje iz sučelja i praćenje dugih poslova, ali otvara i pitanje
međusobnog zaključavanja arhivskih poslova.

**Ocjena aktualnog smjera:** vrlo dobar i domenski zreo.  
**Ocjena spremnosti za kontroliranu internu betu:** blizu, nakon sigurnosnog
učvršćivanja.  
**Ocjena spremnosti za automatsku distribuciju među nepouzdanim čvorovima:** još
ne; nedostaju autentikacija paketa, pravilo prihvata izdanja i transakcijska
zaštita.

---

## 1. Opseg i zatečeno stanje

Analiza obuhvaća commitanu osnovu do `97a3a23`. Tijekom pregleda dovršena su dva
nova lokalna commita: `b608f5b` dodaje katalog u paket `internal/arhiva`, paket
`internal/poslovi`, administratorsko izdavanje arhive i prikaz napretka dugih
poslova; `97a3a23` osigurava da vrata uvoza imaju registar poslova i izvan pune
serverske inicijalizacije. Lokalni `master` je nakon toga dva commita ispred
`origin/master`.

Projekt trenutačno ima približno 78.000 redaka Go koda, predložaka, JavaScripta i
CSS-a te 437 Go testnih funkcija. Nakon navedenih commitova `master` ima 231
commit. Petnaest commitova od prvih vrata arhive do `c4cbfe6` zahvaća 85 datoteka
s približno 4.200 dodanih redaka:
to je funkcionalna ekspanzija, ne malo održavanje.

Glavne cjeline su:

| Sloj | Odgovornost |
|---|---|
| `cmd/gocop` | sastavljanje procesa, konfiguracija i pokretanje čvora |
| `internal/web` | HTTP rute, HTML prikaz, uvoz, izvješća i korisnički tokovi |
| `internal/service` | poslovna pravila i provjera ovlasti |
| `internal/repository` | glavna SQLite baza i knjiga verzija |
| `internal/arhiva` | gradnja hidrološke arhive, spajanje izvora i `.cop` paket |
| `internal/peers` + `syncnet` | identitet čvora, uparivanje i razmjena |
| `internal/poslovi` | novi memorijski registar dugih pozadinskih poslova |
| `internal/docx` i uvoznici | dokumenti i migracija vanjskih podataka |

Ovisnosti su suzdržane: `google/uuid`, TOML, Goldmark, `x/crypto`, čisti Go
SQLite i izdvojeni `syncnet`. To je dobar izbor za prijenosni Windows program i
rad bez instalacijskog ekosustava.^1

---

## 2. Arhitektonska slika

```text
korisnik / preglednik
        │
        ▼
  HTTP + html/template
        │
        ├──────────────► service ─► repository ─► data/gocop.db
        │                                  │
        │                                  └─ knjiga verzija / P2P razmjena
        │
        └─ administracija arhive
                │
                ├─ ulaz CSV/XLSX ─► vodostaji/<sliv>/<letva>/
                │                           │
                │                           ▼
                │                    data/vodostaji.db
                │                           │
                └─ izdavanje ──────────────┴─► *.cop + katalog.json
```

Najvažnija odluka jest razdvajanje dvije vrste istine:

- `gocop.db` čuva ono što organizacija operativno radi i što se verzionira;
- `vodostaji.db` čuva veliku, obnovljivu hidrološku povijest izvedenu iz
  izvornih datoteka ili primljenih paketa.

To je ispravnije od baza po modulu ili sektoru. Granice modula i sektora su
poslovne projekcije koje se s vremenom mijenjaju; operativa i arhiva imaju
različit životni ciklus, volumen i pravilo sinkronizacije. Odvojene datoteke po
vrsti životnog ciklusa smanjuju glavnu bazu, a ne razbijaju referencijalnu sliku
organizacije.

---

## 3. Što je posljednjim promjenama stvarno izgrađeno

### 3.1. Kontrolirana vrata hidrološke arhive

Administratorski unos više nije „kopiraj CSV pa se nadaj”. Sustav:

1. prima CSV ili XLSX uz ograničenje veličine;
2. prepoznaje delimiter, početak tablice, vremenski i brojčani stupac;
3. prikazuje uzorak i dopušta čovjeku ispraviti postaju, sliv, izvor, veličinu,
   vrstu i vremensku zonu;
4. normalizira podatke u kanonski CSV;
5. sprema ga u stablo `vodostaji/`;
6. ponovno gradi samo pogođenu letvu.

Posebno je dobro što program ne tretira prepoznavanje kao istinu. Stroj daje
prijedlog, a čovjek potvrđuje. Novi izvor ulazi isključen dok administrator ne
odluči smije li sudjelovati u spojenom nizu.^2

### 3.2. `.cop` inačica 2

Paket prenosi sirove nizove, očitanja, HQ krivulje, profile, promjene kote nule
i postavke izvora. Spojeni niz ne putuje jer je izveden; primatelj ga ponovno
gradi. To sprječava dupliciranje velikog izvedenog sadržaja i čuva mogućnost
ponovne provjere rezultata.

Dodavanje `izvori.json` u inačicu 2 rješava važan problem inačice 1: isti sirovi
podaci na dva čvora mogli su dati različit spoj, a paket nije govorio zašto.
Primatelj sada vidi razlike u točnosti, redoslijedu i uključenosti izvora prije
ugradnje.^3

### 3.3. Katalog i izdavanje

Katalog vodi zadnje izdanje svake letve, otisak, razdoblje, broj nizova i zapisa
te ime paketa. Paket se zapisuje sa strane i preimenuje tek kad je cijeli, a
stara izdanja ostaju. Radni sloj seli izdavanje iz CLI omotača u
`internal/arhiva`, pa istu implementaciju koriste terminal i web-sučelje.^4

To je pravi korak prema distribucijskom protokolu, ali još nije torrent:
katalog zasad nema pronalaženje peerova, dijeljenje blokova, nastavak prekinutog
prijenosa, potpis niti politiku povjerenja. Trenutačno je riječ o sadržajno
adresiranom paketu s ručnim ili datotečnim transportom.

### 3.4. Ulaganje operative i kontrolirani zaborav

Alat `ulozi-ocitanja` premješta završena operativna očitanja u arhivsko stablo,
gradi letvu, označava zapise izdanjem i tek uz posebnu zastavicu briše izvornik
i njegove verzije. Ručno očitanje, dojava i rekonstrukcija ostaju različiti
izvori. Sumnjiva očitanja i zapisi bez vrijednosti ne ulažu se.^5

Filozofija je dobra: prvo trajna i obnovljiva kopija, zatim provjera, zatim
zaborav. Implementacijska provjera još nije dovoljno jaka; to je jedan od
ključnih nalaza u poglavlju 7.

### 3.5. Bilješke uz arhivsku vrijednost

Bilješke ostaju u glavnoj bazi i sinkroniziraju se kroz knjigu verzija, dok
arhiva ostaje vjerna izvornim mjerenjima. Vrsta bilješke je zatvoren domenski
popis: vrh, dno, granica mjerenja, procjena, nepouzdano očitanje ili događaj.
Program iz teksta samo predlaže vrstu; čovjek donosi tvrdnju koja utječe na
izračun ekstrema.^6

Ovo je iznimno kvalitetno modeliranje. Razdvaja „broj koji je izvor objavio” od
„onoga što operater zna o tom broju” bez prepisivanja povijesti.

### 3.6. Pozadinski poslovi

Najnovija verzija uvodi registar poslova u memoriji, vlasništvo posla po korisniku,
hvatanje panike, dnevnik, korake i traku napretka. Stranica status pita jednom u
sekundi i tolerira kratki prekid veze. Testovi pokrivaju stanje, vlasništvo,
paniku, neodređen napredak i rezultat posla.^7

To rješava stvaran UX problem višeminutne gradnje. Istodobno mijenja model
izvršavanja: arhivske operacije sada se lakše mogu preklopiti, pa je potrebno
uvesti koordinaciju poslova po resursu.

---

## 4. Najjače strane projekta

### Domenski model

Projekt razumije razliku između postaje, vodotoka, dionice, poddionice,
branjenog područja, praga obrane, kote nule, profila korita, HQ krivulje,
operativnog očitanja i arhivskog niza. Ta razlika nije ostala samo u nazivima;
ugrađena je u pravila prikaza, izračuna, uvoza i sinkronizacije.

### Offline-first kao stvarna osobina

Sučelje, baza, izvješća i osnovna razmjena rade bez cloud servisa. Vanjski
servisi poput vremena nadopunjuju rad, ali nisu uvjet za osnovnu operativu.
Jedan izvršni program i čisti Go SQLite prikladni su za terenska i uredska
Windows računala.

### Provenijencija

Izvor, vrsta niza, kvaliteta, rekonstrukcija, bilješka i čvor nastanka nisu
stopljeni u jedan neobjašnjiv broj. To je preduvjet za obranu rezultata pred
stručnim ili upravnim pregledom.

### Dokumenti bez vanjske uredske infrastrukture

Vlastito generiranje DOCX/XLSX izlaza smanjuje instalacijske ovisnosti. Za
službeni sustav to je praktičnije od obveznog Office automation procesa, pod
uvjetom da vizualni regresijski testovi ostanu dio izdavanja.

### Testiranje poslovnih pravila

Testovi nisu samo CRUD smoke-testovi. Pokrivaju vremenske zone, vrste izvora,
rekonstrukcije, prikaz historijata, promjene kote, katalog i rubne uvjete
poslova. Broj testova nije dokaz kvalitete sam po sebi, ali sadržaj testova
pokazuje da se greške iz stvarnih podataka pretvaraju u trajne regresijske
provjere.

---

## 5. Sigurnosna analiza

### P0 — ugradnja paketa mora imati serversku administratorsku ogradu

Gumb za ugradnju `.cop` paketa prikazuje se samo globalnom administratoru, ali
HTTP rute koriste samo opći autentifikacijski middleware. `PregledPaketa` i
`UgradiPaket` dohvaćaju postaju i korisnika, ali ne odbijaju korisnika koji nije
globalni administrator.^8 Skrivanje gumba nije ovlast. Prijavljeni korisnik može
izravno poslati POST i zamijeniti kompletan historijat letve.

**Preporuka:** zajednička serverska funkcija `requireGlobalAdmin` na pregledu i
ugradnji; zaseban test koji izravno poziva obje rute kao običan korisnik i
očekuje HTTP 403. Razmotriti treba li i izvoz kompletnog historijata biti
ograničen, osobito ako budu dodani osjetljivi prilozi.

### P0 — integritet nije autentičnost

SHA-256 otisak dokazuje da sadržaj odgovara manifestu i otkriva slučajno
oštećenje. Ne dokazuje identitet izdavača: napadač može promijeniti sadržaj,
izračunati novi hash i upisati proizvoljan `Izdao`. Plan povezivosti već
predviđa potpis, ali paket ga još nema.^9

**Preporuka:** potpisati kanonski manifest koji uključuje hash svih dijelova,
izdanje, postaju, vrijeme i identitet čvora. Prihvaćati samo ključ aktivnog
člana mreže te pamtiti rezultat provjere i razlog eventualnog ručnog izuzeća.

### P1 — povratak na starije izdanje

Pregled pokazuje broj izdanja, ali lokalna arhiva ne pamti zadnje ugrađeno
izdanje i nema pravilo koje odbija stariji paket. Staro, ali valjano potpisano
izdanje bilo bi klasičan replay/rollback napad.

**Preporuka:** u arhivskoj bazi voditi `primljena_izdanja(letva, izdanje,
otisak, izdao, primljeno, potpis_valjan)`. Niže ili isto izdanje s različitim
hashom odbiti; namjerni rollback dopustiti samo administratoru uz posebno
obrazloženje i zapis u glavnoj knjizi.

### P1 — ograničenje raspakirane veličine

Ulazni `.cop` ograničen je na 50 MB, ali se svaki ZIP dio čita neograničenim
`io.ReadAll`. Mali komprimirani paket može se raspakirati u mnogo memorije.^10

**Preporuka:** dopustiti samo očekivana imena dijelova, odbiti duplikate,
ograničiti ukupnu `UncompressedSize64`, broj nizova i broj zapisa prije
alokacije te provjeriti da `ocitanja.bin` nema višak nepročitanih bajtova.

### Postojeći alfa rizici

Projekt i README ispravno priznaju da još nema sustavne CSRF zaštite. Javni SSE
stream i javni `/api/areas` također trebaju autentifikaciju prije šireg
izlaganja. Sesijski kolačić nema `Secure`; to se mora riješiti konfiguracijski
jer ga lokalni HTTP i TLS terminacija na tunelu ne mogu tretirati jednako.

---

## 6. Pouzdanost, transakcije i konkurentnost

### P0 — ugradnja `.cop` paketa nije jedna transakcija

`Ugradi` obriše staru letvu, upiše sirove dijelove i potvrdi transakciju. Tek
zatim izvan transakcije dodaje postavke izvora i gradi `spoj`.^11 Ako taj drugi
dio padne, stari spoj je već obrisan, novi sirovi nizovi su trajno upisani, a
korisnik dobiva grešku. Stanje je djelomično i suprotno komentaru funkcije.

**Preporuka:** ili omogućiti `spojiTx` i sve potvrditi zajedno, ili izgraditi
novu arhivsku datoteku sa strane, potpuno je validirati i atomskim renameom
zamijeniti aktivnu. Drugi pristup je sigurniji za višemilijunske arhive i lakše
omogućuje rollback.

### P0 — provjera prije zaborava ne dokazuje ono što tvrdi

`provjeriUArhivi` provjerava samo postoji li u izvedenoj tablici `spoj` bilo
kakav zapis iste letve i vremena. Ne uspoređuje vrijednost ni izvor. Uz to,
rekonstruirani zapisi nisu dodani u skup `svi`, ali jesu u `ulozeniID`, pa se
mogu označiti i obrisati.^12

**Preporuka:** provjeravati svaki zapis u sirovoj tablici `ocitanja` preko
točnog identiteta niza `(letva, izvor, veličina, vrsta)`, vremena i vrijednosti.
Provjera mora obuhvatiti dojave, ručna i rekonstruirana očitanja. Tek zapis s
potpunim podudaranjem smije dobiti oznaku izdanja i ući u skup za brisanje.

### P1 — istodobni arhivski poslovi

Novi registar poslova štiti vlastita polja mutexom, što ciljani race-testovi
potvrđuju. Ne postoji, međutim, brava nad poslovnim resursima. Dva administratora
mogu istodobno pokrenuti gradnju iste letve, izdavanje cijele arhive ili
gradnju i izdavanje. Oba izdavanja koriste ista privremena imena `*.novo` i
čitanje-pa-zapis kataloga, pa je moguć izgubljen katalog ili neusklađen paket.

**Preporuka:** jedan koordinator arhive s read/write semantikom:

- gradnja/ugradnja letve: ekskluzivna brava za letvu i arhivsku bazu;
- izdavanje: stabilan read-snapshot ili globalna read brava;
- zapis kataloga: ekskluzivna brava nad mapom izdanja;
- UI treba odbiti ili staviti u red konfliktan posao, a ne samo pokušati.

### P1 — zamjena aktivnog čitača arhive

Nakon gradnje ili ugradnje poslužitelj zatvara `s.arhiva` i zamjenjuje pokazivač
novim repozitorijem. Pozadinski posao to sada radi iz druge goroutine, dok HTTP
zahtjevi mogu istodobno čitati stari pokazivač. Ciljani race-test nije aktivirao
taj stvarni serverski scenarij.

**Preporuka:** `sync.RWMutex` ili atomarni holder za repozitorij. Stari čitač se
ne smije zatvoriti dok aktivni zahtjevi ne otpuste referencu. Još čišći pristup
je dugovječni read pool nad istom datotekom ako promjene sheme to dopuštaju.

### P2 — životni ciklus memorijskih poslova

Gotovi poslovi čiste se tek kada se pokrene novi posao. Ako se nakon velikog
broja poslova više ništa ne pokrene, ostaju u memoriji do restarta. To nije
trenutačno visok rizik, ali jednostavan periodični cleanup ili čišćenje pri
`Nadi` učinio bi granicu stvarnom.

---

## 7. Semantika izdanja i „mali torrent”

Otisak sadržaja dobar je stabilan identitet. Broj izdanja, međutim, nije
globalno determinističan samo iz trenutačnog sadržaja. On ovisi o lokalnoj
povijesti kataloga. Dva čvora mogu imati isti sadržaj, ali različit broj
izdanja ako su do njega došla različitim redoslijedom promjena.

Zato su potrebna dva odvojena pojma:

- **otisak sadržaja** — globalni identitet točnih bajtova/podataka;
- **izdanje izdavača** — monotoni broj unutar jedne autoritativne linije
  izdanja.

Za budući protokol preporučuje se:

```text
package_type     station-history | journal-archive | registry-snapshot
scope            station:batina | area:34 | sector:B
edition          monotoni broj autoritativnog izdavača
content_hash     identitet sadržaja
previous_hash    lanac izdanja
issuer_node      identitet čvora
created_at       vrijeme potpisa
signature        Ed25519 nad kanonskim manifestom
parts[]          ime, veličina, hash, vrsta sadržaja
```

Tada se `.cop` može proširiti na dnevnike i registre bez pretvaranja u kopiju
cijele baze. Svaka vrsta paketa treba vlastiti validator i pravilo spajanja.
Vodostaji zamjenjuju cjelovito izdanje letve; zaključani dnevnici vjerojatno se
dodaju kao nepromjenjivi dokumenti; registri se primjenjuju kroz poslovne
entitete i konflikte. Zajednička je samo omotnica, ne semantika ugradnje.

Torrent-sličan prijenos dolazi tek kasnije: katalog može oglašavati hash i
dijelove, čvor bira pretplate, a blokove preuzima od bilo kojeg uparenog čvora.
Potpis autoriteta ostaje odvojen od peerova koji samo prenose bajtove.

---

## 8. Kvaliteta koda i testova

### Potvrđeno

- `go build` je pokriven posredno punim testnim prolazom svih paketa.
- `go test ./...` prolazi na aktualnom radnom stanju.
- `go vet ./...` prolazi.
- `go test -race ./internal/poslovi ./internal/arhiva ./internal/web` prolazi.

### Što ti prolazi ne dokazuju

Race detector otkriva samo utrke koje izvršeni test stvarno aktivira. Ne dokazuje
sigurnost istodobne gradnje i posluživanja arhive bez testa koji te operacije
pokreće paralelno. Jedinični test kataloga ne dokazuje atomsku konzistentnost
više datoteka pri prekidu procesa. Obični hash test ne dokazuje autentičnost.

### Statička upozorenja

Nalazi o mrtvom kodu (`prorijedi`, `sviSuVrijeme`, `sviSuBroj`, `Linker.names`,
`sectionStructureLink`) imaju smisla kao čišćenje. `koritoOpis` ne treba brisati
bez odluke pripada li presjek korita kartici postaje u Word izvješću. To je
funkcionalna odluka, ne lint-popravak.

### Testovi koje treba dodati

1. običan korisnik dobiva 403 na pregled i ugradnju `.cop` paketa;
2. kvar `spoji` ostavlja staru arhivu potpuno čitljivom;
3. izmijenjena vrijednost istog timestampa blokira `-zaboravi`;
4. rekonstruirani zapis mora biti provjeren prije brisanja;
5. dva istodobna izdavanja ne gube katalog i ne dijele `.novo` datoteku;
6. gradnja u pozadini uz paralelno čitanje historijata prolazi pod `-race`;
7. ZIP s prevelikom raspakiranom veličinom odbija se prije alokacije;
8. starije ili jednako izdanje s drugim hashom odbija se;
9. paket neaktivnog ili opozvanog čvora odbija se;
10. prekid procesa između paketa i kataloga ima determinističan oporavak.

---

## 9. Dokumentacija i operativna spremnost

README dobro opisuje alfa status, instalaciju, mrežne portove i osnovni model
ovlasti. Međutim, ubrzani razvoj stvorio je razilaženja:

- README još govori da su svi podaci u jednoj SQLite datoteci, dok hidrološka
  arhiva sada živi u `data/vodostaji.db`;
- tablica datoteka instalacije ne navodi arhivu, katalog ni mapu paketa;
- `docs/plan-arhiva-i-zaborav.md` označava izdanje, katalog i ulaganje kao
  „nije napravljeno”, iako implementacija sada postoji;
- plan opisuje potpisan `.cop`, a aktualni format ima samo hash.

To nije kozmetika. Administrator iz dokumentacije mora moći zaključiti što se
backupira, što se može ponovno izgraditi, što se sinkronizira i što se smije
obrisati. Prije bete treba napraviti jedan operativni dokument „Podaci i
oporavak” s matricom:

| Sadržaj | Datoteka | Autoritativan | Sinkronizira se | Može se obnoviti |
|---|---|---:|---:|---:|
| operativa i registri | `gocop.db` | da | selektivno | iz mreže/backupa |
| hidrološka arhiva | `vodostaji.db` | izvedena | paketima | iz CSV/.cop |
| izvorno stablo | `vodostaji/` | da za izdavača | ne automatski | iz izvora/backupa |
| paketi i katalog | `pakete/` | objavljeno izdanje | budući distribucijski sloj | ponovno izdavanje |
| ključevi čvora | `node-key`, mrežni ključ | da | nikada kao podatak | samo kontrolirani oporavak |

---

## 10. Preporučeni put do bete

### Faza A — zaštita podataka

1. serverske administratorske ovlasti za sve arhivske mutacije;
2. jaka provjera ulaganja prije `-zaboravi`;
3. atomska ugradnja paketa ili gradnja nove arhive sa strane;
4. koordinacija istodobnih arhivskih poslova;
5. sigurna zamjena aktivnog arhivskog čitača.

### Faza B — vjerodostojno izdanje

1. potpis manifesta Ed25519 ključem čvora;
2. evidencija primljenih izdanja i anti-rollback pravilo;
3. ograničenja ZIP strukture i raspakirane veličine;
4. kanonski manifest s hashom svakog dijela;
5. status autoriteta: službeno, interno, rekonstruirano, neprovjereno.

### Faza C — operativna beta

1. CSRF, autentificirani SSE i sigurni deployment kolačići;
2. pravi `http.Server` s timeoutima i graceful shutdownom;
3. backup/restore proba na čistom Windows računalu;
4. migracijska politika između beta verzija;
5. ažuriran README i administratorski runbook;
6. CI za Windows/Linux/macOS, testove, vet, race ciljeve i reproducibilne
   artefakte sa SHA-256.

### Faza D — selektivna distribucija

1. katalog dostupan uparenim čvorovima;
2. pretplata po vrsti, području, postaji i razdoblju;
3. preuzimanje samo nedostajućih hashiranih dijelova;
4. više transportnih izvora, ali jedan potpisani autoritet;
5. kvote prostora, lokalno zaboravljanje i mogućnost ponovnog dohvaćanja.

---

## 11. Završna prosudba

goCOP više nije demonstracija ideje. Ima dovoljno stvarne domene, podataka i
neugodnih rubnih slučajeva da se može procjenjivati kao sustav, a ne prototip.
Najveći uspjeh je što arhiva nije postala samo još jedna velika tablica:
definirani su izvori, način spajanja, rekonstrukcija, ljudske bilješke, izdanja
i prijenosni paket.

Najveća opasnost sada nije manjak funkcija nego brzina kojom se pouzdane ručne
operacije pretvaraju u automatske. Svaka automatizacija umnožava posljedicu
slabe ovlasti, neatomskog upisa ili pogrešne provjere brisanja. Zato sljedeći
korak ne bi trebao biti više vrsta `.cop` sadržaja, nego dovršavanje sigurnosne
i transakcijske jezgre postojećeg paketa.

Ako se navedeni P0 i P1 nalazi zatvore, projekt ima vrlo uvjerljiv temelj za
gotovu betu: ne samo vizualno dojmljivu, nego operativno objašnjivu, obnovljivu
i provjerljivu. Upravo ta kombinacija može proizvesti stvarni „WOW efekt” kod
vodoprivrednih stručnjaka — jer sustav razumije njihov posao, a ne samo njihove
obrasce.

---

## Izvori

1. [`go.mod`](../go.mod), popis izravnih i neizravnih ovisnosti.
2. [`internal/web/handlers_uvoz_niza.go`](../internal/web/handlers_uvoz_niza.go), pregled, potvrda i upis niza; [`internal/web/uvoz_niza.go`](../internal/web/uvoz_niza.go), prepoznavanje i normalizacija.
3. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), format paketa, otisak, izvori i ugradnja.
4. [`internal/arhiva/katalog.go`](../internal/arhiva/katalog.go), katalog i izdavanje; [`cmd/paket-arhive/main.go`](../cmd/paket-arhive/main.go), CLI omotač.
5. [`cmd/ulozi-ocitanja/main.go`](../cmd/ulozi-ocitanja/main.go), ulaganje, provjera i zaborav operative.
6. [`internal/models/biljeska.go`](../internal/models/biljeska.go); [`internal/repository/biljeska_repo.go`](../internal/repository/biljeska_repo.go).
7. [`internal/poslovi/poslovi.go`](../internal/poslovi/poslovi.go); [`internal/poslovi/poslovi_test.go`](../internal/poslovi/poslovi_test.go); [`web/static/js/app.js`](../web/static/js/app.js).
8. [`internal/web/server.go`](../internal/web/server.go), rute paketa; [`internal/web/handlers_paket.go`](../internal/web/handlers_paket.go), pregled i ugradnja.
9. [`docs/plan-povezivost.md`](plan-povezivost.md), plan potpisa i distribucije; [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), aktualni hash bez potpisa.
10. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), `Procitaj` i neograničeni `io.ReadAll` ZIP dijelova.
11. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), `Ugradi`: commit prije izvora i funkcije `spoji`.
12. [`cmd/ulozi-ocitanja/main.go`](../cmd/ulozi-ocitanja/main.go), skup `svi`, `provjeriUArhivi`, označavanje i brisanje.
13. [`README.md`](../README.md), deklarirano stanje, instalacija, sigurnost i alfa ograničenja.
14. [`docs/plan-arhiva-i-zaborav.md`](plan-arhiva-i-zaborav.md), plan životnog ciklusa arhive.
15. [`docs/rekonstrukcija-nizova.md`](rekonstrukcija-nizova.md), metodologija rekonstrukcije, kota nule i profili korita.
