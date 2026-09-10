# Povezivost čvorova i raspačavanje arhiva

Zapis odluka o tome kako čvorovi dolaze jedan do drugoga i kako se do njih
raznosi ono što ne ide redovnom sinkronizacijom. **Ovo nije raspored posla
nego okvir**: v1 ostaje na onome što već radi. Nastalo iz razgovora u rujnu
2026.

Arhiva, izdanja i zaborav imaju svoj zapis u
[plan-arhiva-i-zaborav.md](plan-arhiva-i-zaborav.md); ovdje se ne ponavljaju.

## Zamisao od koje se krenulo

Vlastiti poslužitelj (`sync.cop-osijek.com`) koji pronalazi čvorove na
internetu i pomaže im uspostaviti privremene tunele, šifrirana P2P mreža nad
time, i raznošenje velikih arhiva protokolom nalik torrentu — svaki čvor koji
ima dio odmah ga nudi dalje. Uz to `.cop` paketi: šifrirani, tako da izvana
nitko ne zna što su, a čvor povlači samo ono što ga zanima — samo Batinu, samo
Vukovar.

Podjela slojeva iz te skice je točna i otprilike odgovara Syncthingovu
ustroju:

```
Tracker    = pronalazi čvorove
Transport  = sigurna veza među njima
Sync       = sinkronizira bazu
Blob       = raznosi velike nepromjenjive datoteke
Relay      = zadnji izlaz
```

### Što od toga već postoji

Više nego što se čini:

| dio | stanje |
|---|---|
| ključ mreže, potpisana članstva | radi (`internal/peers/network.go`) |
| UUID i par ključeva po čvoru | radi |
| šifrirana veza među čvorovima | radi (TLS, `syncnet`) |
| pronalaženje na LAN-u | radi (UDP broadcast) |
| sinkronizacija po revizijama | radi (`internal/ledger`) |
| otisak niza za provjeru pri preuzimanju | radi (`nizovi.otisak`) |
| dohvatljivost preko interneta | nema |
| prijenos datoteka među čvorovima | nema |

## Mjerenje koje je promijenilo zaključak

**471 MB arhive nije količina podataka nego način na koji ih SQLite drži** —
48 bajta po očitanju za nešto što je razlika vremena i vodostaj u
centimetrima.

Batina, izmjereno na 933.686 zapisa u 16 nizova:

| oblik | veličina | po zapisu |
|---|---|---|
| u SQLiteu | ~44 MB | 48 B |
| sirovo (vrijeme + vrijednost) | 14,2 MB | 16 B |
| razlike + varint | 2,8 MB | 3,15 B |
| + gzip | **0,5 MB** | 0,51 B |
| + xz | **0,3 MB** | 0,35 B |

Stotinu puta manje. Ako se ostatak arhive ponaša slično — a nema razloga da ne
bi, jer je isti oblik podatka — cijela arhiva od 10,4 milijuna zapisa stane u
**svega nekoliko megabajta**.

**Posljedica:** za historijate ne postoji problem raspačavanja. Torrent,
swarm, dijeljenje na komade i tracker rješavaju problem koji te brojke nemaju.
Batina je privitak u e-pošti.

## Gdje to ne vrijedi

Onih sto puta vrijedi za **nizove brojeva**. Ne vrijedi ni za što od ovoga:

- **fotografije s terena** — već komprimirane, pakiranje ne daje ništa
- **video** — isto, samo gore
- **potpisani PDF-ovi** — komprimirani, i moraju ostati **bajt u bajt**, jer
  potpis inače pada

Tekst dnevnika održavanja je malen: oko 1000 upisa godišnje puta radovi A.02 i
A.03 puta 34 branjena područja je oko 68.000 upisa, stotinjak megabajta sirovo
i malo nakon pakiranja. **Ali privitci uz te upise rastu bez granice** — deset
fotografija po upisu je red veličine terabajta godišnje.

Dakle: za nizove ne treba, za terenski materijal treba.

Zatečeno stanje kad se ovo pisalo: `journal_entries` je prazan, a model
privitka u programu **ne postoji**. Odluka se donosi prije nego išta postoji,
što je pravi trenutak.

## Dvije vrste sadržaja, dvije mjere

| | jedinica | zašto |
|---|---|---|
| **nizovi** | paket po letvi i izdanju, `historijat_batina_v1.cop` | mnogo sitnih zapisa; pojedinačno adresiranje se ne isplati |
| **fotografije, video, PDF** | pojedinačan nepromjenjiv objekt adresiran otiskom | malo velikih stvari; nema izdanja ni prepakiravanja |

Drugi red usput daje tri stvari besplatno: ista fotografija ne čuva se
dvaput, cjelovitost se provjerava sama, a djelomična replikacija postaje
prirodna — svaki čvor drži ono što je tražio.

### `.cop` je format prijenosa, ne pohrane

Paket se preuzme, raspakira i **uloži u arhivsku SQLite bazu**. Baza ostaje
obična i upitna.

To je bitno jer je prvi prigovor na šifrirane pakete bio da se kose s
`CGO_ENABLED=0` — `modernc.org/sqlite` ne poznaje SQLCipher, pa se šifrirana
baza ne bi mogla otvoriti. Taj prigovor **pada** čim je `.cop` samo omot za
put, a ne oblik u kojem podatak živi.

## Šifriranje — kad zarađuje svoje mjesto

**Ne** zato da se izvana ne zna da je datoteka arhiva: šifriranje skriva
sadržaj, ne postojanje. Nastavak `.cop` umjesto `.zip` ne znači ništa jer se
gledaju prvi bajtovi, ne ime. Slobodno tako nazvati radi urednosti, ali to ne
broji kao mjera sigurnosti.

**Ne** ako paketi kruže samo unutar mreže koja je ionako šifrirana — tada je
ceremonija.

**Da** u jednom slučaju: **izvođačev čvor raznosi ono što ne smije čitati.**
To je jedini razlog koji šifriranje ovdje opravdava, i ujedno mehanizam za
„tko što dobiva".

Iz toga slijedi zahtjev: **ključ po vrsti sadržaja, ne jedan po mreži.** Jedan
ključ za sve znači da svaki član, uključujući izvođača, otključava sve.

## Transport

**Sam WireGuard ne rješava NAT.** On je tunel — šifriranje i usmjeravanje. Da
bi se dva čvora spojila, jedan mora biti dohvatljiv na poznatoj adresi. Iza
kućnog rutera i CGNAT-a to je upravo problem koji postoji.

| | što je | cijena |
|---|---|---|
| **Tailscale** | WireGuard + koordinacija + probijanje NAT-a + DERP relay | poslužitelj koji spaja je komercijalan |
| **Headscale** | otvorena izvedba tog poslužitelja, vrti se sam | mora pratiti inačice klijenta |
| **tsnet** | Tailscale kao Go knjižnica u samom programu | velika ovisnost za program koji ih nema |
| **go-libp2p** | cijeli generički P2P sloj, već napisan | vrlo velika ovisnost |

**tsnet + Headscale** znači da korisnik u Slavonskom Brodu **ne instalira
ništa** i da nema komercijalne ovisnosti — program se sam pridruži mreži
ključem koji mu administrator izda. To je najbolji odgovor ako se ide na
mrežu.

Zamka: kad probijanje NAT-a ne uspije, treba **DERP relay** — ili Tailscaleov
javni, čime se komercijalna ovisnost vraća na mala vrata, ili vlastiti, što je
još jedna usluga za održavanje.

### Ali pri ovim veličinama možda ništa od toga

**Odlazna HTTPS veza prolazi kroz svaki vatrozid i svaki NAT, bez ijedne
postavke kod korisnika.** To je jedina varijanta u kojoj čovjek u Brodu doista
ne radi ništa.

Argument za čvorove koji **preživljava** male brojke nije propusnost nego
**raspoloživost**: središnji poslužitelj otkazuje upravo kad treba, za velike
vode kad padne veza. To je već pokriveno — pronalaženje na LAN-u i razmjena
preko `syncnet` rade bez interneta.

**Oblik koji iz toga slijedi:**

- **LAN mreža ostaje** — nosi izvanredno stanje, već je napisana
- **glup HTTPS izvor** — nosi „različiti gradovi, različite mreže", nula
  postavljanja kod korisnika
- **P2P preko interneta se ne piše** dok se ne pojavi sadržaj koji ta dva ne
  pokrivaju, a to su fotografije i video

### Tracker vjerojatno ne treba uopće

„Imam izdanje v1, otisak taj-i-taj" je zapis od stotinjak bajta koji može
putovati knjigom verzija, istim putem kao sve ostalo. Time nestaje javni
poslužitelj i sav teret uz njega: raspoloživost, certifikat, zloporaba, i
podaci o prisutnosti — tko je kad na mreži — koji su sami po sebi osjetljivi
za ustanovu.

## Odakle se paketi preuzimaju

### Pravilo koje sve drži na okupu

> **Adresa nikad nije identitet.** Sadržaj se prepoznaje po otisku, a izvori
> su popis natuknica — i troše se.

Ako katalog kaže „batina, izdanje v1, otisak `abc…`", onda je adresa samo
jedan način da se do toga dođe, a preuzeto se u svakom slučaju provjerava
otiskom. Time privremeno rješenje prestaje biti dug: prelazak s jednog izvora
na drugi je izmjena u postavkama, ne selidba.

Pola toga već stoji: `nizovi.otisak` s komentarom „sadržajni otisak, za
provjeru pri preuzimanju", i katalog koji je u planu arhive predviđen da putuje
redovnom sinkronizacijom. **Fali samo popis izvora.**

### Google Drive — prvi izvor

Za nizove nosi do kraja: cijela arhiva je nekoliko megabajta, a 15 GB je
besmisleno velika rezerva. Prednosti pred GitHubom:

- **za to je i napravljen**, pa nema razgovora o uvjetima korištenja
- **link se može opozvati** — javni repozitorij ne može, jer postoje forkovi i
  predmemorije

Dijeljeni link **jest vjerodajnica**: tko ga ima, skida. To je u redu jer bez
ključa nema sadržaja — ovdje se šifriranje i model dijeljenja poklapaju.

Zamke koje treba znati unaprijed:

- preuzimanje dijeljenog linka programski nije službeno podržano; adresa
  `uc?export=download` radi, ali je neslužbena i mijenjala se
- datoteke iznad ~100 MB vraćaju **HTML stranicu s upozorenjem o virusima**
  umjesto datoteke
- postoji **„download quota exceeded"** — kad isti sadržaj povuče dovoljno
  ljudi, zaključa se na 24 sata, i kvar je nejasan kad se dogodi

### GitHub — moguć, ali s uvjetima

- **Releases, ne git povijest.** Git čuva svaku inačicu zauvijek, a šifrirani
  sadržaj se ne razlikuje od šuma — dvije inačice istog paketa nemaju ništa
  zajedničko, pa se svaka sprema cijela. Repozitorij godišnjih izdanja raste
  jednosmjerno i ne može se smanjiti bez prepisivanja povijesti.
- granica u gitu je 100 MB po datoteci; u Releasesima 2 GB, i ne broje se u
  veličinu repozitorija
- po uvjetima korištenja GitHub **nije CDN**; za stotine megabajta nitko neće
  trepnuti, za terabajte račun može biti označen
- **nepoznata adresa nije zaštita** — javni repozitoriji se indeksiraju i
  postoje servisi koji prate svaki novi javni repozitorij

### Pitanje koje nije tehničko

Oboje otvara isto: osobni Gmail račun i američki servis kao mjesto gdje stoje
službeni dokumenti javnog tijela. Za nizove vodostaja nikoga neće zanimati; za
potpisana rješenja i fotografije s ljudima hoće. **Pitati prije nego se
navikne** — šifrirano jest branjivo, ali branjivo nije isto što i odobreno.

## Posljedice koje se ne smiju prešutjeti

**Djelomične arhive tiho lome usporedbe.** Batina je rekonstruirana iz
**Bezdana**; čvor koji ima samo Batinu tu analizu ne može napraviti. Program
to mora reći — „ova analiza traži i Bezdan, nije preuzet" — a ne izračunati
nešto na manjem uzorku i prešutjeti.

**Širenje izvan Hrvatskih voda mijenja opseg iz udobnosti u sigurnosno
pitanje.** Sada ključ mreže znači puno članstvo. Kad uđu licencirane firme,
treba odlučiti dobiva li izvođačev čvor sve letve i cijelu arhivu ili samo
dionice na kojima radi. **Riješiti prije prvog vanjskog čvora** — lakše je
suziti prije nego oduzeti poslije.

**Korisnici.** Administrator ih otvara, bez masovnog uvoza. Tisuću korisnika
nije tehnički teret — teret je tisuću lozinki koje netko mora dostaviti i
resetirati. Zato odlučiti **kontakt ili račun** prije prvih stotinu: većini
sudionika treba da budu pronađeni, ne da se prijavljuju.

## Što je odlučeno, a što odgođeno

**Odlučeno sada** — jer se poslije teško mijenja:

1. format paketa i katalog
2. otisak kao identitet, adresa kao natuknica
3. ključ po vrsti sadržaja
4. dvije mjere za dvije vrste sadržaja
5. Drive kao prvi izvor

**Odgođeno** dok se ne pojavi sadržaj koji to traži:

- P2P preko interneta, tracker, relay
- swarm i dijeljenje na komade
- tsnet, Headscale, vlastiti DERP
- izdvajanje generičkog sloja u vlastiti modul — sloj pisan kao općenit prije
  nego postoji drugi korisnik gotovo uvijek ispadne kao prvi korisnik s više
  parametara
