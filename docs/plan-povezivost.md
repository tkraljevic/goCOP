# Povezivost, otpornost i raspačavanje

Zapis o tome zašto goCOP ide na mrežu ravnopravnih čvorova, što je krajnji
cilj, i kojim redom se do njega ide. **Ne treba sve odjednom** — ali svaki
korak mora biti upotrebljiv sam za sebe i nijedan ne smije zatvoriti put
prema cilju.

Arhiva, izdanja i zaborav imaju svoj zapis u
[plan-arhiva-i-zaborav.md](plan-arhiva-i-zaborav.md).

## Zašto — i zašto ne zbog propusnosti

Obrana od poplava je kritična djelatnost. Ne smije stati zato što je nešto
palo.

**Ovo nije teorija — dogodilo se više puta.**

Zato pitanje nije je li podatak 1 TB, 400 MB ili 0,5 MB. Pitanje je nastavlja
li obrana raditi u najgorim uvjetima.

> **Osnovno načelo:** nijedan pojedinačni servis, poslužitelj ni mrežna
> lokacija ne smije biti nužan za nastavak rada i za pristup posljednjim
> pouzdanim podacima.

Ušteda propusnosti, raznošenje arhiva i swarm su **nuspojave**, ne razlog.

## Krajnji cilj

Kad je gotovo, ovo vrijedi:

1. Svaki ovlašteni čvor ima **vlastitu lokalnu SQLite bazu** i radi neograničeno
   dugo bez ičije pomoći.
2. Čvorovi se sinkroniziraju **izravno**, čim se mogu dosegnuti — na LAN-u, u
   uredu, preko interneta.
3. **Nijedan čvor nije nužan.** Ni tvoj, ni uredski, ni onaj s arhivom.
4. Povijesni podaci su dostupni na zahtjev i **provjerljivi otiskom**.
5. **Tko što dobiva** provodi se na razini replikacije, ne prikaza.
6. Kompromitirani čvor se može **prepoznati i poništiti**, a ne samo isključiti.

## Što već radi

Ovo nije početak iz prazna:

| dio | stanje |
|---|---|
| lokalna SQLite baza na svakom čvoru | radi |
| knjiga verzija s revizijama | radi (`internal/ledger`) |
| ključ mreže, potpisana članstva s rokom | radi (`memberships`, ima `expires_at`) |
| UUID i par ključeva po čvoru | radi |
| šifrirana veza među čvorovima | radi (TLS, `syncnet`) |
| pronalaženje na LAN-u | radi (UDP broadcast) |
| otisak niza za provjeru pri preuzimanju | radi (`nizovi.otisak`) |
| rad bez interneta | radi |
| dohvatljivost preko interneta | nema |
| prijenos datoteka među čvorovima | nema |
| opseg kao granica replikacije | nema |
| provenijencija i potpisi po zapisu | djelomično |

## Slojevi

### 1. Identitet i članstvo

Svaki čvor ima `device UUID` i vlastiti par ključeva. Mreža ima ključ mreže,
iz kojeg se izvode identifikatori i ključevi za potvrdu. Svaki projekt ima
svoj `project_id`.

**Ključ mreže se upotrebljava jednom, pri pristupanju**, i ne ostaje na
uređaju. Dalje čvor radi svojim parom ključeva i potpisanim članstvom s
rokom. Ako ključ mreže živi na disku svakog čvora, jedan izgubljen uređaj
kompromitira sve.

Tracker nikad ne dobiva ključ mreže.

### 2. Pronalaženje

Na LAN-u mDNS ili postojeće pronalaženje. Za internet malen rendezvous
servis čija je uloga **samo**: prijava prisutnosti, pronalaženje čvorova,
razmjena kandidata za vezu, i objava sposobnosti.

Tracker **ne prenosi bazu ni korisničke podatke**.

Napomena: „tko što ima" ne treba tracker — to je zapis od stotinjak bajta koji
može putovati knjigom verzija, istim putem kao sve ostalo.

### 3. Povezivost — `PeerTransport`

Sada **Tailscale**, jer već radi u poslovnoj mreži i rješava probijanje NAT-a,
izravne veze, WireGuard šifriranje i relay.

Ali aplikacijski sloj **ne smije biti vezan uz njega**. Definira se čisto
sučelje `PeerTransport`, tako da se Tailscale kasnije može zamijeniti bez
ijedne promjene u protokolu sinkronizacije. To je jedina stvar koja „Tailscale
sada" čini sigurnim potezom.

Napomena: **sam WireGuard ne rješava NAT** — on je tunel i traži poznatu
dohvatljivu adresu. Ako se ide na otvoreno, to je **Headscale** (vlastiti
poslužitelj za spajanje) i **tsnet** (Tailscale kao Go knjižnica u samom
programu, pa korisnik ne instalira ništa).

Buduća vlastita izvedba: QUIC, TLS 1.3, STUN/ICE, izravni IPv6 gdje ga ima.

Redoslijed povezivanja:

```
LAN izravno → IPv6 izravno → internet P2P → relay preko čvora → središnji relay
```

### 4. Sinkronizacija baze

**SQLite se ne prenosi kao datoteka.** Prenose se logičke promjene: `record_uuid`,
revizija, čvor podrijetla, vrijeme, vrsta zahvata, otisak i provenijencija.

Svaki čvor radi offline. Kad opet nađe druge, radi anti-entropy usklađivanje i
dohvaća ono što mu nedostaje.

Na desetke čvorova **ne ide puna mreža** gdje svatko priča sa svima. Ide
gossip s ograničenim brojem aktivnih susjeda:

```
A → B, C        B → D, E        C → F, G
```

### 5. Sukobi i provenijencija — kritični dio

**Posljednji upis ne smije biti jedini mehanizam** za operativno važne podatke.

Uz svaki podatak: izvor, vrijeme mjerenja, vrijeme primitka, čvor koji ga je
primio, revizija, i po potrebi potpis.

Ako dva izvora daju različitu vrijednost za isto mjerenje, **oba se čuvaju i
sukob se označi**. Kod obrane od poplava tiho prepisan prag nije estetski nego
operativni problem.

### 6. Telemetrija

Za primanje novih mjerenja može se razmotriti MQTT ili sličan protokol, ali
**ne kao jedini izvor istine ni kao središnja ovisnost**. Više čvorova ili
pristupnika prima iz više neovisnih izvora i propagira dalje mrežom, pa kvar
jednog ne znači gubitak mjerenja.

### 7. Arhive

Povijest koja se više ne mijenja izdvaja se iz aktivne baze u nepromjenjive
SQLite arhive po razdoblju:

```
vodostaji_1900_1949.sqlite
vodostaji_1950_1979.sqlite
```

Nepromjenjive, samo za čitanje, adresirane otiskom, opisane manifestom, i po
potrebi priključene kroz `ATTACH` — pa **ostaju odmah upitljive** za analize
dugih nizova.

`.cop` je **transportni paket**: manifest + SQLite arhiva + otisak i potpis.
Nakon preuzimanja se provjeri i arhiva se ugradi lokalno. Time `.cop` nije
oblik pohrane, pa `CGO_ENABLED=0` i `modernc.org/sqlite` nisu prepreka.

### 8. Prijenos blobova

Za malen broj čvorova dovoljno je: izravno preuzimanje s čvora, nastavak
prekinutog, ranged prijenos, provjera otiskom, i prelazak na drugi čvor.

Swarm s dijeljenjem na komade ima smisla **tek kad mreža naraste na desetke
ili stotine** — tada tracker prati samo tko ima koji resurs, ne svaki komad.

### 9. Politika relaya

Središnji relay je **zadnji izlaz, ne glavni put**. Za male promjene baze
prihvatljiv je. Za velike arhive se izbjegava: gigabajt preko tuđeg relaya je
spor i nepristojan. Ako je dostupan samo relay — **odgodi**, izdanje može
čekati.

### 10. Sigurnost

Sigurnost prijenosa i šifriranje pohranjenog su **odvojene stvari**.

Veza među čvorovima je E2E šifrirana. Tracker i relay **nemaju pristup
sadržaju**. Svaki čvor ima svoj par ključeva; ključ mreže služi za članstvo,
ne za promet.

### 11. Skaliranje na zaposlenike

Svaki zaposlenik s prijenosnikom može imati svoj čvor, pa se računa na desetke
do stotine. Zato: bez pune mreže, ograničen broj aktivnih susjeda, gossip, i
**opsezi** — organizacija, sektor, VGO, područje, dionica, privatno.

## Oblik mreže

### Uloga se ne dodjeljuje nego proizlazi

Netko ima unraid koji je stalno gore. Netko drugi mini-računalo u uredu.
Netko treći ništa osim prijenosnika.

**To se ne konfigurira.** Čvor objavljuje činjenice o sebi — koliko je
dostupan, što ima, kako se do njega dolazi — a ostali ga sami odaberu kad im
odgovara. Time topologija prati stvarnost i preživljava promjenu opreme, a
nitko ne mora održavati popis tko je „poslužitelj".

### Stalni čvor je poželjan put, nikad nužan

Ovo je jedina stvar koja može tiho pojesti cijelu zamisao. Čim se ijedna
radnja osloni na to da je stalni čvor dostupan — „traži katalog od ureda",
„prijavi se preko čvora" — ponovno je sagrađen poslužitelj, samo s više
koraka.

**Provjera:** isključi stalni čvor i vidi rade li dva prijenosnika u istoj
prostoriji međusobno. Ne „rade li offline" nego rade li **jedan s drugim**,
bez ičega u sredini. To treba raditi redovito, ne jednom.

### Otpornost dolazi od raznolikosti, ne od broja

Deset mini-računala u deset ureda, na istoj domeni, s istim ažuriranjima i
istim vjerodajnicama — u praksi je **jedan** čvor. Padne li domena, padnu svi
zajedno.

Prijenosnik koji je te večeri bio kod nekoga doma, i kućni unraid koji nije na
toj domeni — **oni preživljavaju**. Zato su kućne instalacije najvrjedniji
sloj mreže, a ne rub o kojem treba brinuti.

Zaključak: vrijedi imati čvorove koji otkazuju **iz različitih razloga**, ne
samo mnogo njih.

## Otpornost na kompromitaciju nije isto što i na nedostupnost

Ovo je poglavlje koje razlog i rješenje inače ne spajaju do kraja.

Mreža čvorova štiti od toga da nešto **padne**. Od toga da nešto bude
**zauzeto** — ne sama po sebi. Ako je napadač ušao na razini domene, imao je
vjerodajnice i pristup uređajima. Sto čvorova s istim programom je veća
površina napada, a knjiga verzija će savjesno raznijeti otrovane zapise na
sve.

Zato uz mrežu idu tri stvari:

1. **Potpisani zapisi** — kompromitiran čvor ne može krivotvoriti tuđe podatke.
2. **Knjiga samo za dopisivanje, provjerljiva unatrag** — otrovane revizije se
   mogu pronaći.
3. **Poništavanje po čvoru i vremenu** — mora se moći reći „sve od čvora X
   nakon tog trenutka je sumnjivo" i to ukloniti bez ručnog čišćenja stotinu
   baza.

### Kopija u koju mreža ne piše

Prošli put je spasila sigurnosna kopija. Mreža daje **dostupnost, ne
oporavak**.

Razlika je u ovome: **mreža daje kopije u prostoru, backup daje kopije u
vremenu.** Zaraza i otrovani zapisi šire se prostorom trenutačno — sto čvorova
ima sto jednako pokvarenih kopija. Pomaže samo kopija od **prije** incidenta.

Dovoljna je povremena kopija, ali mora zadovoljiti jedan uvjet: **ništa što je
na mreži ne smije joj moći pisati.** Kopija na stalno priključenom disku ili
na dijeljenoj mapi na koju čvor ima pravo pisanja pada s prvim ucjenjivačkim
softverom — tako se backupi i gube.

Tri načina koji uvjet zadovoljavaju:

- **odspojeni disk** — priključi se samo za vrijeme kopiranja
- **samo-dopisivanje / WORM** — snimke koje se ne mogu prepisati (ZFS ili
  btrfs snapshot, spremište s object-lockom)
- **povlačenje izvana** — backup domaćin sam povlači, a izvor mu ne može ni
  pisati ni brisati

Stalno upaljen čvor (unraid, mini-računalo) dobro je odredište **samo** ako su
kopije snimke koje sustav u pogonu ne može prepisati. „Stalno gore i na mreži"
znači i „dohvatljiv napadaču".

Ritam je dvostruk i jeftin:

- **izdanja arhive** — jednom po izdanju, čuvaju se sva; nepromjenjiva su i
  mala
- **operativna baza** — često; 5,6 MB, pa je i dnevna kopija kroz godinu dana
  zanemariva

Jedna posljedica onoga što se ionako gradi: izdanja su adresirana otiskom, pa
se **ispravnost kopije može provjeriti**, a ne samo pretpostaviti. Obična
kopija baze koja je tiho istrunula izgleda jednako kao zdrava; ova se
prepozna.

*Ukradeni i izgubljeni uređaji izuzeti su iz opsega za sada. Mehanizam koji ih
pokriva — članstvo s kratkim rokom koje samo istekne — ionako slijedi iz
opoziva bilo koje vrste, a `expires_at` već stoji u shemi.*

## Veličina podataka — dobra vijest, ne razlog

Izmjereno na Batini (933.686 zapisa, 16 nizova):

| oblik | veličina | po zapisu |
|---|---|---|
| u SQLiteu | ~44 MB | 48 B |
| sirovo | 14,2 MB | 16 B |
| razlike + varint | 2,8 MB | 3,15 B |
| + gzip | **0,5 MB** | 0,51 B |
| + xz | **0,3 MB** | 0,35 B |

Onih 471 MB arhive nije količina podataka nego način na koji ih SQLite drži.
Za cijelu arhivu od 10,4 milijuna zapisa to je **procjena** od nekoliko
megabajta — mjerena je samo Batina.

To ne mijenja razlog za mrežu, ali mijenja **redoslijed**: raznošenje nizova
je lako i može čekati, jer stane u ništa. Ono što je teško dolazi s
fotografijama.

## Dvije vrste sadržaja, dvije mjere

| | jedinica | zašto |
|---|---|---|
| **nizovi** | paket po letvi i izdanju | mnogo sitnih zapisa; pojedinačno adresiranje se ne isplati |
| **fotografije, video, PDF** | pojedinačan nepromjenjiv objekt po otisku | malo velikih stvari; nema izdanja ni prepakiravanja |

Pakiranje od sto puta vrijedi **samo za brojeve**. Fotografije i video su već
komprimirani; potpisani PDF mora ostati bajt u bajt jer potpis inače pada.

Tekst dnevnika je malen — oko 1000 upisa godišnje puta radovi A.02 i A.03 puta
34 branjena područja je oko 68.000 upisa. **Privitci uz te upise rastu bez
granice**: deset fotografija po upisu je red veličine terabajta godišnje.

Zatečeno stanje: `journal_entries` je prazan, model privitka **ne postoji**.
Odluka se donosi prije nego išta postoji.

## Šifriranje

**Ne** zato da se izvana ne zna što je datoteka — šifriranje skriva sadržaj,
ne postojanje. **Ne** ako paketi kruže samo unutar mreže koja je ionako
šifrirana.

**Da** zbog jednog: **izvođačev čvor raznosi ono što ne smije čitati.** To je
razlog koji ga opravdava, i ujedno mehanizam za „tko što dobiva". Iz toga
slijedi **ključ po vrsti sadržaja, ne jedan po mreži**.

### Kako

**Šifrat nema magičnih bajtova** — izlaz dobrog šifriranja je neraspoznatljiv
od šuma, nema se što prerušiti. Ali **„šifrirani zip" ne skriva što je
unutra**: središnji katalog ostaje čitljiv, s imenima datoteka, veličinama i
vremenima.

Zato: **AEAD nad cijelim tokom** (XChaCha20-Poly1305 ili AES-GCM), izlaz
`nonce + šifrat + oznaka`. Od prvog bajta izgleda kao šum, nema kataloga koji
odaje, i sam provjerava ispravnost — ako se ključ ne poklopi, otvaranje padne.

### Ime datoteke govori više od zaglavlja

`historijat_batina_v1.cop` u javnoj mapi kaže sve. Zato se datoteke imenuju
**otiskom**: `a3f9c2e8….cop`. Katalog zna što je što i putuje sinkronizacijom,
ne javnom mapom.

Što svejedno curi: **veličina, kad se pojavila, tko je preuzima.** Razlog više
da se ne računa na tajnost nego na ključ.

## Odakle se paketi preuzimaju

> **Adresa nikad nije identitet.** Sadržaj se prepoznaje po otisku, izvori su
> popis natuknica — i troše se.

Zato izvor može biti bilo što, i mijenja se bez selidbe:

- **Google Drive** — za nizove nosi do kraja; za to je i napravljen, i **link
  se može opozvati**. Zamke: neslužbena adresa za preuzimanje, stranica o
  virusima iznad ~100 MB, i „download quota exceeded".
- **GitHub** — samo kroz **Releases**, ne kroz git povijest: git čuva svaku
  inačicu zauvijek, a šifrirani sadržaj se ne razlikuje od šuma pa se svaka
  sprema cijela. Po uvjetima korištenja GitHub **nije CDN**.
- **drugi čvor** — krajnji cilj
- **USB ključ** — u nuždi, i to je legitiman izvor

Za oboje vrijedi isto pitanje, i nije tehničko: osobni račun i strani servis
kao mjesto gdje stoje službeni dokumenti javnog tijela. Za vodostaje nikoga
neće zanimati; za potpisana rješenja i fotografije s ljudima hoće. **Pitati
prije nego se navikne.**

## Plan po koracima

Ne treba sve odjednom. Redoslijed je određen jednim mjerilom: **što je skupo
naknadno ugraditi, ide prvo.**

Odluke u **modelu podataka** teško se popravljaju — provenijencija i opseg
naknadno znače prepisivanje svakog zapisa. **Mreža** se mijenja kad god, jer
je iza sučelja. Zato podatkovni sloj ide prije mrežnog, iako je mrežni
zanimljiviji.

| korak | što | zašto tim redom |
|---|---|---|
| **0** | *(napravljeno)* lokalna baza, knjiga verzija, LAN, TLS, članstva | temelj već stoji |
| **1** | **provenijencija i potpis po zapisu**; **opseg kao granica replikacije** | najskuplje naknadno — mijenja svaki zapis i svaku razmjenu |
| **2** | **sučelje `PeerTransport`**, Tailscale iza njega | jeftino sada, oslobađa sve kasnije |
| **3** | **izdanja arhive**: manifest, otisak, ime po otisku, popis izvora (Drive prvi) | rješava 471 MB i daje katalog |
| **4** | **sukobi vidljivi u sučelju** — oba zapisa, oznaka, tko je unio | bez toga se pogreška ne vidi dok ne zaboli |
| **5** | **gossip** umjesto razmjene sa svima | tek kad čvorova bude dovoljno da smeta |
| **6** | **prijenos blobova** među čvorovima: nastavak, ranged, provjera | kad se pojave privitci |
| **7** | **poništavanje po čvoru i vremenu** | kad mreža bude dovoljno velika da kompromitacija boli |
| **8** | *(možda nikad)* vlastiti transport QUIC/STUN, swarm s komadima | tek ako Tailscale zasmeta ili čvorova bude stotine |

Svaki korak je upotrebljiv sam za sebe. Korak 3 vrijedi i bez mreže — arhiva
se preuzme s Drivea. Korak 1 vrijedi i bez ijednog drugog čvora, jer se zna
odakle podatak dolazi.

## Što se ne smije zaboraviti

**Djelomične arhive tiho lome usporedbe.** Batina je rekonstruirana iz
**Bezdana**. Čvor koji ima samo Batinu tu analizu ne može napraviti — i to
mora **reći**, a ne izračunati nešto na manjem uzorku i prešutjeti.

**Opseg prije prvog vanjskog čvora.** Kad uđu licencirane firme, općine,
županije i službe za spašavanje, ključ mreže koji daje puno članstvo nije
prihvatljiv. Lakše je suziti prije nego oduzeti poslije. I mora djelovati na
razini replikacije — ako svaki čvor ionako dobije sve, opseg je ukras.

**Kontakt ili račun, prije prvih stotinu korisnika.** Tisuću korisnika nije
tehnički teret; teret je tisuću lozinki. Većini sudionika treba da budu
pronađeni, ne da se prijavljuju.

**Službeni podaci na privatnim uređajima.** Kućni čvor je najotporniji dio
mreže i vrijedi ga poticati — ali s uskim opsegom i šifriranim diskom, i uz
odluku ustanove, ne prešutno.
