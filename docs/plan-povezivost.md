# Povezivost, otpornost i raspačavanje

Stanje izvedbe pregledano 5. 10. 2026. prema 0.0.34-alfa. Arhitekturni ciljevi u
nastavku nisu tvrdnja da su svi mehanizmi već ugrađeni.

Uparivanje i primanje u mrežu odobrava globalni administrator; svjež čvor
pristupa postavljanju samo lokalno. HTTP i razmjena imaju ograničenja
veličine, trajanja i broja veza. Od 0.0.33 rade ovlašteni primatelji,
članstvo pokazano u TLS certifikatu, primanje na daljinu i potpisani opozivi.
Ovlast za primanje čvorova razlikuje se od planirane ovlasti za izdavanje
prognoza i arhiva. Opoziv vrijedi na drugom čvoru tek kad mu stigne razmjenom;
trenutačna dostava opoziva ostaje otvorena. Granice povjerenja su u
[SECURITY.md](../SECURITY.md), a postavljanje u
[administratorskim uputama](INSTALACIJA.md#3-podaci-i-sigurnost).

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

Iz toga slijedi i kako program treba opisivati: ne kao „aplikacija sa SQLite
bazom i P2P sinkronizacijom", nego kao **raspodijeljeni sustav operativnih
podataka**. P2P nije mogućnost koja mu je dodana — proizlazi iz zahtjeva da
sustav nastavi raditi kad dijelovi infrastrukture nestanu.

```
                       IZVORI
                          │
                 unos + provenijencija
                          ↓
                ┌─────────────────┐
                │  LOKALNI ČVOR   │
                │  SQLite         │
                │  knjiga verzija │
                │  potpisi        │
                └────────┬────────┘
                         │
                   PeerTransport
                         │
           ┌─────────────┼─────────────┐
           ČVOR        ČVOR          ČVOR
           └────── anti-entropy ──────┘

              nijedan čvor nije nužan

                         +
              nepromjenjive arhive
                         +
              nepromjenjive kopije
```

## Što već radi

Ovo nije početak iz prazna:

| dio | stanje |
|---|---|
| lokalna SQLite baza na svakom čvoru | radi |
| knjiga verzija s revizijama | radi (`internal/ledger`) |
| ključ mreže, potpisana članstva s rokom | radi (`memberships`, ima `expires_at`) |
| ovlašteni primatelj i primanje na daljinu | radi od 0.0.33: ovlast potpisana ključem mreže; zahtjev i potvrda vezani zasebno dojavljenim kodom |
| potpisani opoziv članstva i ovlasti | radi od 0.0.33, nakon primitka opoziva razmjenom |
| jedinstveno ime i par ključeva po čvoru | radi; ime nije izvedeno iz ključa, dvojnici s drugim ključem odbijaju se |
| šifrirana veza među čvorovima | radi (TLS, `internal/razmjena`) |
| pronalaženje na LAN-u | radi (UDP broadcast) |
| otisak niza za provjeru pri preuzimanju | radi (`nizovi.otisak`) |
| rad bez interneta | radi |
| dohvatljivost preko interneta | izravna javna adresa (port razmjene, zadano 4710) ili HTTPS/WebSocket `/razmjena/tunel`, oboje s unutarnjim TLS-om i ključem čvora; javna adresa upiše se jednom, na bilo kojem čvoru (Domenski čvorovi), i putuje razmjenom; nema automatskog internetskog pronalaženja |
| selektivna replikacija | radi za očitanja, dnevnike, prijave i vodočuvarski dnevnik po vrsti, sektoru ili branjenom području, godinama, razini sadržaja i roku držanja sadržaja |
| prijenos bez mreže | radi potpisanim `.cop` v3 paketima za očitanja, dnevnike, prijave i vodočuvarski dnevnik; stari SQLite izvoz ostaje radi kompatibilnosti |
| izravni prijenos datoteka i blobova među čvorovima | djelomično: sadržaj iz `sadrzaj.db` prenosi se po SHA-256 otisku u ograničenim porukama; nema nastavka po dijelovima ni dohvata na klik |
| opseg kao granica replikacije | djelomično: očitanja, dnevnici, prijave i vodočuvarski dnevnik imaju kanale i pretplate; zajednički registri dolaze svima |
| izdanja hidrološke arhive | potpisani `.cop` paketi po letvi; kazalo putuje knjigom verzija, paketi se dohvaćaju prema pretplati |
| prognoze i kiša | izdanja i promjene modela putuju razmjenom; primatelj puni lokalne baze prognoza i oborina |
| provenijencija i potpisi po zapisu | djelomično |

## Slojevi

### 1. Identitet i članstvo

Svaki čvor ima jedinstveno ime (`[node] id`) i vlastiti Ed25519 par ključeva.
Ime ne dokazuje identitet; vezu dokazuje posjedovanje privatnog ključa.
Privatni ključ mreže nastaje pri osnivanju i ostaje na osnivaču u
`network-key` uz bazu. Ne putuje razmjenom.

Nove članove prima nositelj ključa mreže ili **ovlašteni primatelj**. Njegova
ovlast vrijedi dvije godine, a članstvo koje izda najdulje do isteka te
ovlasti (uobičajeni rok članstva je godinu dana). Ovlast za primanje ne može
prenositi dalje. Novo računalo može se primiti i izvan lokalne mreže:
razmjenjuju se zahtjev i potvrda, a kod se javlja drugim putem. Privatni
ključ novog čvora pritom ostaje na njemu.

Čvor pri spajanju pokaže članstvo i ovlast primatelja u TLS certifikatu;
provjeravaju se potpis, rok i poznati opozivi. Zato ga nije potrebno posebno
uparivati sa svakim članom. Opoziv članstva ili ovlasti putuje razmjenom,
potpisan ovlaštenim ključem, i ne može se poništiti arhiviranjem zapisa.
Oduzimanje ovlasti poništava sva članstva koja je taj primatelj izdao.

Buduće sastajalište služi pronalaženju i ne treba privatni ključ mreže.

### 2. Pronalaženje

Na LAN-u mDNS ili postojeće pronalaženje. Za internet malen rendezvous
servis čija je uloga **samo**: prijava prisutnosti, pronalaženje čvorova,
razmjena kandidata za vezu, i objava sposobnosti.

Tracker **ne prenosi bazu ni korisničke podatke**.

Napomena: „tko što ima" ne treba tracker — to je zapis od stotinjak bajta koji
može putovati knjigom verzija, istim putem kao sve ostalo.

### 3. Povezivost — `PeerTransport`

Sada je ugrađena izravna TLS veza i **HTTPS/WebSocket tunel**, primjerice iza
Cloudflarea. Oba koriste isti unutarnji TLS i provjeru ključa čvora. Tunel
prenosi šifrirane bajtove, nije izdavatelj identiteta niti zamjena za uparivanje.
Tailscale nije ovisnost aplikacije; ostaje moguća vanjska mrežna alternativa.
Opće sučelje `PeerTransport` ostaje arhitekturni prijedlog, ne uvjet sadašnje izvedbe.

Napomena: **sam WireGuard ne rješava NAT** — on je tunel i traži poznatu
dohvatljivu adresu. Ako se ide na otvoreno, to je **Headscale** (vlastiti
poslužitelj za spajanje) i **tsnet** (Tailscale kao Go knjižnica u samom
programu, pa korisnik ne instalira ništa).

Buduća vlastita izvedba: QUIC, STUN/ICE, izravni IPv6 gdje ga ima. TLS 1.3
je već u upotrebi: izravna veza i tunel traže najmanje tu inačicu.

Sada čvor pokušava redom: adresu koju je upravo našao na LAN-u, zapamćene
izravne adrese, pa adrese tunela (`https://`).

Mogući budući redoslijed povezivanja (nije trenutačna implementacija):

```
LAN izravno → IPv6 izravno → internet P2P → relay preko čvora → središnji relay
```

### 4. Sinkronizacija baze

U živoj mrežnoj razmjeni **SQLite se ne prenosi kao datoteka**. Prenose se
logičke verzije zapisa s čvorom podrijetla, vremenom, vrstom zahvata i kanalom.

Svaki čvor radi offline. Kad opet nađe druge, radi anti-entropy usklađivanje i
dohvaća ono što mu nedostaje.

Očitanja, dnevnici, prijave s terena i vodočuvarski dnevnik već su podijeljeni
u kanale `vrsta/područje/godina`. Uredski čvor može pratiti sve, a laptop samo
odabrani sektor ili branjeno područje i godine. Kad pretplata više ne pokriva
kanal, korisnik ga može lokalno obrisati. Zajednički ustroj, registri i
djelatnici ostaju izvan tih ograda i dolaze svim čvorovima.

Za prijenos bez mreže odabrani se kanali izdaju kao potpisan `.cop` paket
(Administracija → Održavanje baze → „Izdaj .cop") i ugrađuju na drugom čvoru
(„Ugradnja paketa"). Stari SQLite izvoz („Stari oblik (.db)") ostaje radi
kompatibilnosti. To je prijenos knjige verzija, ne kopiranje aktivne baze, pa
ugradnja prolazi istim pravilima kao mrežna razmjena.

**Sažimanje ne pomiče granicu razmjene.** Granica je najnovija verzija svakog
autora u svakom kanalu. Automatsko prorjeđivanje izdanja prognoze (od
0.0.7-alfa) i zamijenjenih verzija kazala arhive (od 0.0.21-alfa) tu verziju
ne briše. Ručno sažimanje („Sažmi knjigu" u Održavanju baze) to pravilo
poštuje od 0.0.24-alfa. Prije je tu
verziju brisalo kad je isti zapis poslije izmijenio drugi čvor, pa su je drugi
čvorovi slali natrag.

Na desetke čvorova sadašnja izravna razmjena sa svim poznatim čvorovima neće
biti dovoljna. Tada treba gossip s ograničenim brojem aktivnih susjeda:

```
A → B, C        B → D, E        C → F, G
```

#### Različite verzije programa na čvorovima

Čvorovi se ne ažuriraju istodobno. Od 0.0.24-alfa (2. 10. 2026.) zato
vrijede tri pravila:

1. **Nepoznata polja se čuvaju.** Kad program izmijeni zapis, polja koja je
   dodao noviji program prepišu se iz prethodne verzije i ne nestaju tiho.
   Iznimka su potvrde članstva te izdanja i modeli prognoze: potpis ili paket
   pokriva samo ono što je zapisano.
2. **Shema je po entitetu.** Verzija nosi shemu svog entiteta (zadano 1).
   Zapis koji je zadnji izmijenio program s novijom shemom stariji program
   sprema, prenosi i prikazuje. Ne smije ga izmijeniti ni obrisati, nego
   javlja da treba ažurirati goCOP.
3. **Što program ne razumije, javlja.** Primljene zapise novije sheme i
   entitete kojih nema u programu javi u ispisu programa i kao upozorenje na
   pločici razmjene, uz poruku da treba ažurirati goCOP.

Uz to razmjena nosi izdanje programa. Pločica razmjene i stranica
Sinkronizacija pokazuju na kojem izdanju radi koji čvor i upozore kad se
razlikuje. Čvor koji izdanje ne javlja radi na programu starijem od ove
izmjene.

Nijedan entitet zasad nema shemu veću od 1, pa drugo pravilo djeluje tek kad
je neki budući program podigne. Prvo pravilo štiti samo među čvorovima koji
imaju ovu izmjenu: 0.0.23-alfa i starije inačice pri izmjeni zapisa i dalje
gube polja koja ne poznaju.

Za buduće izmjene iz toga slijedi:

- **Nova polja idu na vrh zapisa**, ne unutar ugniježđenih struktura. Program
  nepoznata polja čuva samo na prvoj razini zapisa. Ugniježđenu strukturu
  zapiše cijelu, onakvu kakvu poznaje, pa bi novo polje u njoj stariji program
  obrisao.
- **Shema se podiže samo kad se mijenja značenje polja** (`ledger.PostaviShemu`),
  ne za novo polje. Polje koje se namjerno ukida navodi se u
  `ledger.UmiroviPolja`, a novi entitet ulazi među poznate
  (`repository.PoznatiEntiteti`).
- **Popravci podataka čitaju knjigu i pišu samo promjenu.** Polaze od zadnje
  verzije zapisa u knjizi, ne od površine, a novu verziju pišu samo kad se
  nešto stvarno promijenilo. U tijelo ne ide ništa što ovisi o čvoru ili satu,
  inače bi svaki čvor upisao svoju verziju istog ispravka.
- **Nadogradnja ide redom: stalni čvor, odmah zatim ostali.** U međuvremenu se
  ne mijenjaju djelatnici ni zaduženja. 0.0.23-alfa i starije inačice pri
  pokretanju prekodiraju korisnike i zaduženja, a novije ih ne diraju, pa bi
  isti zapis na dva čvora dobio različit identifikator.

Razlozi su opisani u komentarima `internal/ledger/shema.go` i
`internal/repository/fixups.go`.

Čuvanje nepoznatih polja nije jamstvo da stari program razumije novo
ponašanje. U 0.0.33 promijenjeno je uparivanje i uvedeni su ovlašteni
primatelji; u 0.0.34 poništeni akti više ne određuju stanje obrane. Za te
prijelaze treba nadograditi sve čvorove prije nastavka ovjera i storna.

### 5. Sukobi i provenijencija — kritični dio

**Posljednji upis ne smije biti jedini mehanizam** za operativno važne podatke.

Uz svaki podatak: izvor, vrijeme mjerenja, vrijeme primitka, čvor koji ga je
primio, revizija, i po potrebi potpis.

Ako dva izvora daju različitu vrijednost za isto mjerenje, **oba se čuvaju i
sukob se označi**. Kod obrane od poplava tiho prepisan prag nije estetski nego
operativni problem.

Sada je to tek djelomično izvedeno. Kad dva čvora izmijene isti zapis, obje
verzije ostaju u knjizi verzija. Na površini je novija (veći `version_id`), a
sukob se nigdje ne označava.

#### Stanje podatka, ne samo vrijednost

Operateru nije dovoljno:

```
VODOSTAJ = 521 cm
```

nego:

```
521 cm
  izvor        telemetrija postaje
  izmjereno    07:00
  primljeno    07:02
  čvor         node-17
  potpis       valjan
  starost      2 min
  stanje       POTVRĐENO
```

Razlika između **„vodostaj je 521 cm"** i **„posljednji poznati vodostaj je
521 cm, ali podatak je star 47 minuta"** u obrani može biti važnija od same
vrijednosti. Prva rečenica navodi na odluku, druga na provjeru.

Stanja: `POTVRĐENO`, `NEPOTVRĐENO`, `SPORNO`, `ZASTARJELO`, `PONIŠTENO`.

**Ali stanje se izvodi, ne pohranjuje.** Ovo je bitno:

- **zastarjelost** je razlika između `measured_at` i sada — pohranjena bi bila
  netočna već u trenutku upisa
- **spornost** ovisi o tome postoji li druga vrijednost za isto mjerenje
- **poništenost** ovisi o tome je li u međuvremenu stigla odluka o poništenju

Pohranjuju se **činjenice** — izvor, vremena, čvor, valjanost potpisa — a
stanje se računa pri prikazu. Pohranjeno stanje tiho zastari, a upravo je
tiho zastarjeli podatak ono od čega se ovdje branimo.

Prag zastarijevanja **nije jedan broj**. Ovisi o očekivanom ritmu tog niza,
koji program već zna (`nizovi.vrsta`: satni, dvokratni, jutarnji, dnevni).
Podatak star 47 minuta je uredan kod dnevnog očitanja, a alarmantan kod
satnog za vrijeme vala.

### 6. Telemetrija

Za primanje novih mjerenja može se razmotriti MQTT ili sličan protokol, ali
**ne kao jedini izvor istine ni kao središnja ovisnost**. Više čvorova ili
pristupnika prima iz više neovisnih izvora i propagira dalje mrežom, pa kvar
jednog ne znači gubitak mjerenja.

### 7. Arhive

Hidrološka povijest već je izdvojena iz operativne baze u
`data/vodostaji.db`, a obnovljivi izvori ostaju u stablu `vodostaji/`. Ona se
ne sinkronizira kroz knjigu verzija.

`.cop` je **transportni paket historijata jedne letve**: ZIP s manifestom,
komprimiranim nizovima, krivuljama, profilima, promjenama kote nule i
postavkama izvora. Paket nosi otisak sadržaja, otiske dijelova i Ed25519
potpis. Primatelj ga provjeri, ugradi u svoju lokalnu arhivu i ponovno izgradi
izvedeni spojeni niz.

Lokalni `katalog.json` vodi zadnje izdanje svake letve. Od 0.0.7-alfa čvor
koji izdaje arhivu zapisuje u knjigu verzija kazalo svakog paketa: letvu,
izdanje, otisak, razdoblje i veličinu. Sam paket stavlja u spremište sadržaja.
Kazalo drže svi čvorovi. Paket dohvaća čvor kojemu ga pokriva pretplata, od
bilo kojeg čvora koji ga ima, i ugrađuje ga tek nakon provjere. Objavljuje se
samo novije izdanje. Pojedinosti su u
[plan-arhiva-i-zaborav.md](plan-arhiva-i-zaborav.md).

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

Sadašnja izvedba od toga odstupa kod dviju uloga. Od 0.0.7-alfa one se
uključuju ručno: Administracija → Čvor, mreža i sinkronizacija → Uloge ovog
čvora, **Preuzima vodostaje s izvora** i **Izdaje prognozu**. Tako izvore ne
pita svaki čvor, a za isti sat ne nastaju dvije različite prognoze. Uloge su
lokalne i ne putuju razmjenom. Novi čvor nema nijednu dok se ne uključi. Ako
čvor s ulogom ne radi, vodostaji s izvora i prognoza ne obnavljaju se dok
netko ne uključi ulogu na drugom čvoru. Samo prebacivanje još ne postoji.

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

Zato mreži trebaju **izmješteni čvorovi u različitim domenama otkazivanja**:
druga mreža, drugi pružatelj pristupa, druga struja, izvan ustanovske domene i
izvan njezina ciklusa ažuriranja. Takav čvor ne dijeli sudbinu s ostalima i
preživi ono što obori sve unutar kuće.

Kako se to izvede je zasebno pitanje. Kućna instalacija je **jedna moguća
izvedba** i tehnički najlakša, ali sa sobom nosi upravljanje uređajima,
zaštitu podataka i odluku ustanove — vidi „Što se ne smije zaboraviti". Druge
izvedbe su čvor na drugoj lokaciji ustanove, kod ugovornog partnera, ili na
zakupljenom poslužitelju izvan iste domene.

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

Stanje od 20. 9. 2026.: za izvornike je odluka donesena. PDF-ovi, fotografije
prijava i prilozi vodočuvarskog lista čuvaju se u spremištu `data/sadrzaj.db`
kao nepromjenjivi objekti po SHA-256 otisku. Knjiga verzija nosi samo otisak,
vrstu i veličinu, a bajtovi putuju zasebno, prema razini pretplate. Privitak
uz upis u dnevnik još ne postoji. Pojedinosti su u
[plan-sadrzaj-i-cop.md](plan-sadrzaj-i-cop.md).

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
- **drugi čvor** — za pakete arhive radi od 0.0.7-alfa: razmjenom, prema
  pretplati, od bilo kojeg čvora koji paket ima
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
| **1** | *(djelomično)* **model podataka**: kanali i selektivna replikacija rade za očitanja, dnevnike, prijave i vodočuvarski dnevnik; od 0.0.24-alfa knjiga čuva polja novijeg programa i vodi shemu po entitetu; opća provenijencija, potpis svakog zapisa i izrazivo poništavanje nisu dovršeni | najskuplje naknadno — mijenja svaki zapis i svaku razmjenu |
| **2** | *(transport izveden)* izravni TLS i HTTPS/WebSocket tunel; opće sučelje `PeerTransport` ostaje prijedlog | mreža se mijenja bez promjene podataka |
| **3** | *(napravljeno)* potpisani manifest, otisci, `.cop` po letvi, mrežna objava kazala i automatski dohvat prema pretplati | rješava veliku arhivu bez slanja mjerenja kroz knjigu verzija |
| **4** | **stanje podatka u sučelju** — koliko je star, odakle je, je li sporan | bez toga se pogreška ne vidi dok ne zaboli |
| **5** | **gossip** umjesto razmjene sa svima | tek kad čvorova bude dovoljno da smeta |
| **6** | *(djelomično)* **prijenos blobova** među čvorovima: prijenos po otisku s provjerom radi; nastavak i ranged prijenos nisu izvedeni | privitci su tu (PDF-ovi, fotografije prijava); veliki sadržaji traže nastavak |
| **7** | **provođenje poništavanja** — sučelje, širenje, automatika | mehanizam može čekati, ali samo ako je model iz koraka 1 to predvidio |
| **8** | *(možda nikad)* vlastiti transport QUIC/STUN, swarm s komadima | tek ako postojeći transport postane ograničenje ili čvorova bude stotine |

Svaki korak je upotrebljiv sam za sebe. Korak 3 vrijedi i bez mreže — arhiva
se preuzme s Drivea. Korak 1 vrijedi i bez ijednog drugog čvora, jer se zna
odakle podatak dolazi.

### Korak 1 razrađeno — jer se poslije ne popravlja

Ovo je jedini korak koji se ne može odgoditi bez cijene. Transport se mijenja
iza sučelja, sučelje se prepravlja, ali **model podataka se naknadno mijenja
samo prepisivanjem svakog zapisa i svake razmjene**.

Prvi dio je uveden: knjiga verzija nosi kanal, a pretplate taj kanal koriste
kao granicu razmjene za očitanja, dnevnike, prijave i vodočuvarski dnevnik. Sljedeći popis zato opisuje ono
što još treba ujednačiti na svim vrstama zapisa, ne ono što danas svaki zapis
već nosi.

Uz svaki zapis:

```
record_uuid      koji je to zapis
revision         koja mu je verzija
origin_node      koji ga je čvor stvorio
source           odakle podatak dolazi (mjerenje, prijepis, rekonstrukcija)
measured_at      kad je izmjeren
received_at      kad je stigao u sustav
signed_at        kad ga je čvor potpisao
signature        potpis nad sadržajem i vremenom
scope            čiji je — organizacija, sektor, područje, dionica, privatno
```

**`signed_at` je odvojen od `received_at` i od vremena primjene.** To izgleda
kao sitnica, a upravo o tome ovisi može li se poništavanje uopće izraziti.
Ako se pamti samo „zadnja izmjena", nikad se neće moći reći „sve od čvora X
nakon 03:17".

Poništavanje se tada izražava ovako, i ne traži nikakvu promjenu sheme kad
zatreba:

```
REVOKE
  node        = 7c29…
  valid_until = 2026-09-10T03:17:00

⇒ sve što je taj čvor potpisao poslije 03:17 → NEPOUZDANO
```

**Sam mehanizam — sučelje, širenje odluke, automatika — može čekati korak 7.
Ali mogućnost da se to izrekne mora postojati od koraka 1.** Zapis koji ne
nosi tko ga je potpisao i kad, ne može se poslije proglasiti sumnjivim ni
ručno.

Isto vrijedi za **opseg**: mora biti granica **replikacije**, a ne prikaza.
Ako svaki čvor ionako dobije sve pa filtrira pri ispisu, opseg je ukras, a
ukraden ili kompromitiran čvor ima sve.

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

**Službeni podaci na privatnim uređajima.** Izmješteni čvor je ono što mreži
daje raznolikost otkazivanja, i vrijedi ga imati — ali ako je izveden kao
kućna instalacija, otvara upravljanje uređajima, zaštitu podataka i sigurnosnu
politiku. Uz uzak opseg i šifriran disk, i uz odluku ustanove, ne prešutno.
Gdje to nije prihvatljivo, ista se svrha postiže čvorom na drugoj lokaciji
ustanove ili kod ugovornog partnera.
