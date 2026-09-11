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
`go test -race` za `internal/arhiva`, `internal/ulaganje`, `internal/poslovi` i
`internal/web`.

Od prethodnog presjeka zatvorene su ključne prepreke za pouzdanu distribuciju:
arhivske rute imaju serversku administratorsku ogradu, provjera prije brisanja
dokazuje točan niz i vrijednost, ugradnja paketa je atomska, arhivski poslovi su
serijalizirani, ZIP ulaz ima granice, paketi se potpisuju, a primljena izdanja
imaju evidenciju i zaštitu od povratka unatrag. Preostali posao više nije
spašavanje osnovnog integriteta nego dovršavanje politike povjerenja, oporavka i
distribucije između čvorova.

**Ocjena aktualnog smjera:** vrlo dobar i domenski zreo.  
**Ocjena spremnosti za kontroliranu internu betu:** tehnička jezgra je spremna
za ozbiljno beta-pilotsko testiranje.
**Ocjena spremnosti za automatsku distribuciju među nepouzdanim čvorovima:**
znatno bliže, ali još treba dovršiti upravljanje pouzdanim ključevima,
opozivima, oporavkom i transportom.

---

## 1. Opseg i zatečeno stanje

Analiza obuhvaća commitanu osnovu do `4a4291a`. Od presjeka `8ed248f` dodan je 21
commit. Uz doradu kartica dionica i dnevnika, šest završnih commitova sustavno
zatvara sigurnosnu jezgru `.cop` toka: zaključavanje poslova, atomsku ugradnju,
granice raspakiravanja, potpis, sadržajno stabilan otisak te evidenciju primljenih
izdanja s pravilom protiv vraćanja unatrag. Commitana osnova je pri završnoj
provjeri usklađena s `origin/master`.

Projekt trenutačno ima 83.830 redaka Go koda, predložaka, JavaScripta i CSS-a te
515 Go testnih funkcija. `master` ima 261 commit. Razlika od `8ed248f` zahvaća 57
datoteka s 3.934 dodana i 495 uklonjenih redaka.

Glavne cjeline su:

| Sloj | Odgovornost |
|---|---|
| `cmd/gocop` | sastavljanje procesa, konfiguracija i pokretanje čvora |
| `internal/web` | HTTP rute, HTML prikaz, uvoz, izvješća i korisnički tokovi |
| `internal/service` | poslovna pravila i provjera ovlasti |
| `internal/repository` | glavna SQLite baza i knjiga verzija |
| `internal/arhiva` | gradnja hidrološke arhive, spajanje izvora i `.cop` paket |
| `internal/ulaganje` | ulaganje završene operative u arhivu i kontrolirani zaborav |
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
stara izdanja ostaju. Aktualna implementacija drži izdavanje izvan CLI omotača u
`internal/arhiva`, pa istu implementaciju koriste terminal i web-sučelje.^4

To je pravi korak prema distribucijskom protokolu, ali još nije torrent:
katalog zasad nema pronalaženje peerova, dijeljenje blokova, nastavak prekinutog
prijenosa, potpis niti politiku povjerenja. Trenutačno je riječ o sadržajno
adresiranom paketu s ručnim ili datotečnim transportom.

### 3.4. Ulaganje operative i kontrolirani zaborav

Poslovna logika ulaganja sada živi u `internal/ulaganje`, a koriste je i CLI
`ulozi-ocitanja` i administratorsko sučelje. Postupak premješta završena
operativna očitanja u arhivsko stablo, gradi letvu, označava zapise izdanjem i
tek u odvojenom koraku nudi brisanje izvornika i njegovih verzija. Ručno
očitanje, dojava i rekonstrukcija ostaju različiti izvori. Sumnjiva očitanja i
zapisi bez vrijednosti ne ulažu se.^5

Filozofija je dobra: prvo trajna i obnovljiva kopija, zatim provjera, zatim
zaborav. Novi prikaz, pregled uloženoga i zasebna akcija čišćenja smanjuju
operativnu mogućnost pogreške, a provjera prije brisanja sada dokazuje točan niz,
vrijeme i vrijednost; detalj je u poglavlju 6.

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

### 3.7. Zaštita postojećih nizova i kontrola izvornog stabla

Uvoz više ne može tiho skratiti stariji niz. Kada su postaja, izvor i vrsta
poznati, sučelje odmah prikazuje zatečeni raspon i broj vrijednosti. Zadani je
postupak nadopuna, dok zamjena zahtijeva izričit odabir i upozorenje koliko bi
vrijednosti i koji raspon nestali. To je važna zaštita od jedne od najskupljih
klasa pogreške: urednog, ali nepotpunog ulaza koji izgleda vjerodostojno.^16

Arhiva sada pri svakom otvaranju traži i **sirotane**: nizove zapisane u bazi za
koje u izvornom stablu više nema datoteke. Ne briše ih automatski, jer izvor može
biti privremeno nedostupan; administrator ih vidi i može ih namjerno ukloniti.
Uklanjanje zatim ponovno gradi cijeli spoj letve, čime je zatvorena greška u
kojoj je uklanjanje jednog niza moglo odnijeti spoj sestrinskog niza istog
izvora.^17

Dodani su i domenski regresijski testovi za dvostruki lokalni sat pri povratku na
zimsko vrijeme te za izbor rekonstrukcije unutar dopuštenog raspona nad
varijantom izvan njega. Ti testovi potvrđuju da arhiva čuva oba fizički različita
očitanja i da granica valjanosti ulazi u odabir najbolje vrijednosti.^18

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

### Zatvoreno — serverske ovlasti arhivskih ruta

Izvoz, pregled i ugradnja `.cop` paketa sada prolaze kroz zajednički middleware
`samoAdmin`. Provjera je na ruti, ne samo na vidljivosti gumba, a regresijski
testovi običnom korisniku očekuju HTTP 403.^8

### Zatvoreno — integritet i kriptografski potpis

Paket uz SHA-256 sada nosi Ed25519 potpis kanonskog manifesta i pojedinačne
otiske dijelova. Vrijeme gradnje uklonjeno je iz sadržajnog identiteta, pa isti
sadržaj daje isti otisak. Matematički valjan potpis ipak još nije isto što i
organizacijsko povjerenje: mreža mora povezati javni ključ s odobrenim čvorom i
imati pravilo opoziva kompromitiranog ključa.^9

### Zatvoreno — povratak na starije izdanje

Arhiva vodi `primljena_izdanja` i odbija niže izdanje te isti broj s drukčijim
otiskom. Evidencija se zapisuje u istoj transakciji kao sadržaj paketa, pa se
pravilo prihvata ne može razići sa stvarno ugrađenim stanjem.^10

### Zatvoreno — granice raspakiravanja

Čitač paketa ograničava strukturu i raspakiranu veličinu prije nekontrolirane
alokacije. Posebni testovi pokrivaju prevelike i nepravilne ZIP dijelove.^11

### Postojeći alfa rizici

Projekt i README ispravno priznaju da još nema sustavne CSRF zaštite. Javni SSE
stream i javni `/api/areas` također trebaju autentifikaciju prije šireg
izlaganja. Sesijski kolačić nema `Secure`; to se mora riješiti konfiguracijski
jer ga lokalni HTTP i TLS terminacija na tunelu ne mogu tretirati jednako.

---

## 6. Pouzdanost, transakcije i konkurentnost

### Zatvoreno — atomska ugradnja i uklanjanje niza

Ugradnja sada briše staro stanje, zapisuje sve dijelove, ponovno gradi spoj i
evidentira izdanje unutar jedne transakcije. Namjerno izazvan kvar spajanja čuva
staro izdanje. `MakniNiz` također vodi brisanje i ponovnu gradnju kroz
transakciju.^12

### Zatvoreno — stroga provjera prije zaborava

`ProvjeriUArhivi` više ne gleda izvedeni `spoj`. Za svaki operativni zapis traži
točan sirovi niz prema letvi, izvoru, veličini i vrsti te isto vrijeme i istu
vrijednost. Ako ijedna vrijednost nedostaje ili se razlikuje, ništa se ne briše;
to je pokriveno zasebnim testovima.^13

### Zatvoreno — istodobni arhivski poslovi i aktivni čitač

Sve mutacije iste arhivske datoteke prolaze kroz zajedničku procesnu i datotečnu
bravu, pa se konfliktni poslovi ne izvršavaju usporedno. Aktivni čitač više se ne
zatvara i ne zamjenjuje ispod HTTP zahtjeva, a dodan je paralelni race-test tog
scenarija.^14

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
- `go test -race ./internal/arhiva ./internal/ulaganje ./internal/poslovi
  ./internal/web` prolazi.

### Što ti prolazi ne dokazuju

Race detector otkriva samo utrke koje izvršeni test stvarno aktivira. Novi test
doista pokreće arhivsku operaciju uz paralelno čitanje, što je bitan napredak,
ali ni on ne simulira prekid procesa ili nestanak diska. Kriptografski test
dokazuje valjanost potpisa, ne i to da organizacija vjeruje tom javnom ključu.

### Statička upozorenja

Raniji nalazi o mrtvom kodu (`prorijedi`, `sviSuVrijeme`, `sviSuBroj`,
`Linker.names`, `sectionStructureLink`) ostaju kandidati za ciljano čišćenje;
nisu bili predmet ovog ponovnog prolaza `staticcheckom`. `koritoOpis` ne treba
brisati bez odluke pripada li presjek korita kartici postaje u Word izvješću.
To je funkcionalna odluka, ne lint-popravak.

### Novi regresijski pokrivači

Od prethodne analize dodani su testovi za:

- nadopunu nasuprot izričitoj zamjeni postojećeg niza;
- očuvanje oba očitanja u ponovljenom satu pri povratku na zimsko vrijeme;
- otkrivanje sirotana pri svakom otvaranju arhive;
- uklanjanje niza bez gubitka spoja sestrinskog niza;
- prednost rekonstrukcije unutar dopuštenog raspona;
- zabranu arhivskih ruta običnom korisniku;
- strogu provjeru vrijednosti prije zaborava;
- rollback cijele ugradnje kada spajanje padne;
- serijalizaciju arhivskih poslova i paralelno čitanje;
- granice ZIP paketa, Ed25519 potpis i anti-rollback izdanja.

To su dobro odabrane provjere jer svaka čuva konkretan podatkovni incident od
ponavljanja. Ne zamjenjuju, međutim, sljedeće sigurnosne i transakcijske testove.

### Testovi koje još treba dodati

1. paket valjano potpisan ključem neaktivnog ili opozvanog čvora odbija se;
2. prekid procesa i nestanak prostora tijekom izdavanja imaju determinističan
   oporavak;
3. dvije zasebne instance procesa nad istom arhivom potvrđuju ponašanje
   datotečne brave na svim podržanim operacijskim sustavima.

---

## 9. Dokumentacija i operativna spremnost

README dobro opisuje alfa status, instalaciju, mrežne portove i osnovni model
ovlasti. Ugrađena pomoć sada bolje opisuje ulaganje, čišćenje i sirotane, pa je
operativni tok vidljiv i administratoru koji ne koristi CLI. Ubrzani razvoj ipak
je ostavio razilaženja u statičkim dokumentima:

- README još govori da su svi podaci u jednoj SQLite datoteci, dok hidrološka
  arhiva sada živi u `data/vodostaji.db`;
- tablica datoteka instalacije ne navodi arhivu, katalog ni mapu paketa;
- `docs/plan-arhiva-i-zaborav.md` označava izdanje, katalog i ulaganje kao
  „nije napravljeno”, iako implementacija sada postoji;
- dokumentacija još treba objasniti potpis, pouzdane ključeve i evidenciju
  primljenih izdanja iz perspektive administratora.

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

Ova faza je zatvorena: uvedene su serverske ovlasti, jaka provjera prije
zaborava, atomska ugradnja, koordinacija poslova i sigurno korištenje aktivnog
čitača.

### Faza B — vjerodostojno izdanje

Prve četiri stavke su implementirane: Ed25519 potpis, evidencija primljenih
izdanja, anti-rollback, ZIP granice i otisci dijelova. Preostaje organizacijski
status autoriteta, vezanje ključa uz odobreni čvor i opoziv ključa.

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

Sigurnosna i transakcijska jezgra postojećeg paketa sada je stvarno zatvorena na
razini koda i regresijskih testova. Najveća opasnost pomaknula se na operativnu
razinu: upravljanje pouzdanim ključevima, oporavak nakon prekida procesa,
backup/restore probe i dokumentiranje odgovornosti pojedinog čvora.

Projekt sada ima vrlo uvjerljiv temelj za gotovu betu: ne samo vizualno
dojmljivu, nego operativno objašnjivu, obnovljivu i provjerljivu. Upravo ta
kombinacija može proizvesti stvarni „WOW efekt” kod
vodoprivrednih stručnjaka — jer sustav razumije njihov posao, a ne samo njihove
obrasce.

---

## Izvori

1. [`go.mod`](../go.mod), popis izravnih i neizravnih ovisnosti.
2. [`internal/web/handlers_uvoz_niza.go`](../internal/web/handlers_uvoz_niza.go), pregled, potvrda i upis niza; [`internal/web/uvoz_niza.go`](../internal/web/uvoz_niza.go), prepoznavanje i normalizacija.
3. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), format paketa, otisak, izvori i ugradnja.
4. [`internal/arhiva/katalog.go`](../internal/arhiva/katalog.go), katalog i izdavanje; [`cmd/paket-arhive/main.go`](../cmd/paket-arhive/main.go), CLI omotač.
5. [`internal/ulaganje/ulaganje.go`](../internal/ulaganje/ulaganje.go), ulaganje; [`internal/ulaganje/citanje.go`](../internal/ulaganje/citanje.go), čitanje i provjera; [`internal/ulaganje/zaboravljanje.go`](../internal/ulaganje/zaboravljanje.go), čišćenje; [`internal/web/handlers_ulaganje.go`](../internal/web/handlers_ulaganje.go), web-sučelje.
6. [`internal/models/biljeska.go`](../internal/models/biljeska.go); [`internal/repository/biljeska_repo.go`](../internal/repository/biljeska_repo.go).
7. [`internal/poslovi/poslovi.go`](../internal/poslovi/poslovi.go); [`internal/poslovi/poslovi_test.go`](../internal/poslovi/poslovi_test.go); [`web/static/js/app.js`](../web/static/js/app.js).
8. [`internal/web/server.go`](../internal/web/server.go), middleware `samoAdmin` i zaštićene rute; [`internal/web/ovlasti_rute_test.go`](../internal/web/ovlasti_rute_test.go), provjere HTTP 403.
9. [`internal/arhiva/potpis.go`](../internal/arhiva/potpis.go), kanonski Ed25519 potpis; [`internal/arhiva/potpis_test.go`](../internal/arhiva/potpis_test.go), potpis i sadržajno stabilan otisak.
10. [`internal/arhiva/primljena.go`](../internal/arhiva/primljena.go), evidencija i pravilo prihvata; [`internal/arhiva/primljena_test.go`](../internal/arhiva/primljena_test.go), anti-rollback scenariji.
11. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), ograničeno čitanje paketa; [`internal/arhiva/paket_granice_test.go`](../internal/arhiva/paket_granice_test.go), granice ZIP strukture i veličine.
12. [`internal/arhiva/paket.go`](../internal/arhiva/paket.go), atomska ugradnja; [`internal/arhiva/ugradnja_atomska_test.go`](../internal/arhiva/ugradnja_atomska_test.go), rollback pri kvaru; [`internal/arhiva/gradnja.go`](../internal/arhiva/gradnja.go), transakcijski `MakniNiz`.
13. [`internal/ulaganje/citanje.go`](../internal/ulaganje/citanje.go), strogi `ProvjeriUArhivi`; [`internal/ulaganje/zaboravljanje.go`](../internal/ulaganje/zaboravljanje.go), brisanje tek nakon potpune provjere; [`internal/ulaganje/provjera_test.go`](../internal/ulaganje/provjera_test.go), negativni scenariji.
14. [`internal/arhiva/brava.go`](../internal/arhiva/brava.go), koordinator arhive; [`internal/arhiva/brava_test.go`](../internal/arhiva/brava_test.go); [`internal/web/arhiva_utrka_test.go`](../internal/web/arhiva_utrka_test.go), paralelno čitanje.
15. [`docs/rekonstrukcija-nizova.md`](rekonstrukcija-nizova.md), metodologija rekonstrukcije, kota nule i profili korita.
16. [`internal/web/handlers_uvoz_niza.go`](../internal/web/handlers_uvoz_niza.go), zatečeni niz, dopuna i zamjena; [`internal/web/izdavanje_stranica_test.go`](../internal/web/izdavanje_stranica_test.go), upozorenje na izgubljeni raspon.
17. [`internal/arhiva/gradnja.go`](../internal/arhiva/gradnja.go), otkrivanje sirotana i `MakniNiz`; [`internal/arhiva/sirotani_test.go`](../internal/arhiva/sirotani_test.go), regresijski scenariji sirotana i sestrinskih nizova.
18. [`internal/arhiva/dvostruki_sat_test.go`](../internal/arhiva/dvostruki_sat_test.go), ponovljeni sat; [`internal/arhiva/sirotani_test.go`](../internal/arhiva/sirotani_test.go), prioritet rekonstrukcije unutar raspona.
