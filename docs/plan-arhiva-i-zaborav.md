# Arhiva, izdanja i zaborav

Zapis odluka o tome gdje koji podatak živi, kako se objavljuje i kako se
prestaje čuvati. Nastalo iz rada na Batini, rujan 2026.

## Zašto uopće

Operativna baza nosi uz svaki zapis i verziju u knjizi, jer se sinkronizira.
Očitanje tako stoji oko 1.360 bajta. Povijesni niz u arhivi stoji oko 20.
Batina je sama, s 45.547 očitanja, zauzimala 61 % glavne baze; sve što čeka na
Dunavu i Dravi značilo bi u tom obliku oko šest gigabajta koje bi svaki čvor
morao prenijeti i držati.

Zato su razdvojene, i zato treba pravilo kad se što seli i kad se briše.

## Podjela

| | sadržaj | svojstva |
|---|---|---|
| `gocop.db` → `readings` | ono što ured radi: dnevni vodostaji koje operater prikuplja, satni **dok traje obrana**, očitanje s terena u bilo koje doba | verzionirano, sinkronizira se |
| `data/vodostaji.db` | povijest: HIS-2000, telemetrija, mađarski nizovi, rekonstrukcije, protok, temperatura, nanos | bez verzija, ne sinkronizira se, obnovljivo iz `vodostaji/` |

Arhiva se **preuzima**, ne razmjenjuje kroz knjigu verzija. Cilj je da redovna
sinkronizacija nosi samo katalog — koja letva, koje izdanje, koje razdoblje i
koliki otisak — dok se `.cop` paket dohvaća zasebno. Lokalni katalog i paketi
postoje; mrežna objava kataloga još ne.

## Stanje izvedbe

### 1. Izdanje arhive  *(napravljeno)*

Historijat svake letve izdaje se kao zaseban `.cop` paket. Paket nosi broj
izdanja, razdoblje, izdavača, otisak sadržaja, otiske dijelova i Ed25519
potpis. Broj raste samo kad se sadržaj promijeni; ponovno izdavanje istog
sadržaja zadržava isti broj i otisak.

Stara izdanja ostaju sačuvana jer su dokaz onoga što je korisnik u određenom
trenutku imao pred sobom. Primatelj pamti zadnje ugrađeno izdanje, odbija
nenamjerni povratak na starije i isto izdanje s drukčijim sadržajem. Namjerni
povratak postoji, ali traži razlog.

Novije izdanje **može mijenjati i stare godine**, ne samo dodavati nove:
HIS-2000 kasni s ovjerom godinu do dvije. Ugradnja zato zamjenjuje cjelovitu
izjavu o letvi, a ne samo dodaje razliku.

### 2. Katalog koji objavljuje izdanje  *(djelomično)*

`katalog.json` već vodi zadnje izdanje svake letve, otisak, razdoblje, broj
nizova i zapisa te ime paketa. Izdavanje je dostupno iz administratorskog
sučelja i naredbenog retka, a paket se može preuzeti i ugraditi ručno.

Još nije napravljen distribucijski dio: katalog ne putuje redovnom
sinkronizacijom, čvorovi sami ne nude novije izdanje i `.cop` paketi se ne
preuzimaju izravno s drugog čvora.

### 3. Ulaganje završene operative u arhivu  *(napravljeno, ručno pokretanje)*

Kad se godina zatvori, operativna očitanja te godine postaju izvor u arhivi,
uz oznake `cop` i `cop-rucno` — odvojeno od HIS-a i telemetrije, jer to nije
isto.

Time operativna baza prestaje rasti bez kraja: drži tekuću godinu i otvorene
epizode. Dvadeset letava puta 365 dnevnih očitanja je 7.300 zapisa godišnje.

Administrator prvo vidi pregled, zatim program zapisuje očitanja u izvorno
stablo, ponovno gradi pogođenu letvu i provjerava svaku vrijednost po izvoru,
veličini, vrsti, vremenu i vrijednosti. Tek nakon uspješne provjere očitanja
dobivaju oznaku ulaganja. Sumnjiva očitanja i zapisi bez vrijednosti ne ulažu
se.

**Ulaganje nije brisanje.** Označeni zapisi ostaju u operativnoj bazi dok se
posebno ne pokrene zaboravljanje (korak 5). Periodično automatsko pokretanje
nakon zatvaranja godine još nije uvedeno.

### 4. Automatsko preuzimanje javnih vodostaja  *(napravljeno)*

Vodomjerna postaja može biti povezana s javnim izvorom i označena za
automatsko preuzimanje. Program prvi put pokušava minutu nakon pokretanja, a
zatim svaki sat. Preuzima samo nova očitanja, bilježi izvor i ponovljenim
preuzimanjem ne stvara duplikate.

Na kartici postaje vidi se stanje zadnjeg pokušaja, a preuzimanje se može
pokrenuti i ručno. Ako nema interneta ili izvor ne odgovara, lokalni rad se
nastavlja i program pokušava ponovno u sljedećem ciklusu. Čitač se bira prema
adresi izvora, pa isti mehanizam podržava hrvatske, mađarske i srpske javne
postaje.

### 5. Zaborav  *(lokalno napravljen, mrežni dogovor nije)*

Lokalni postupak postoji: neposredno prije brisanja ponovno provjerava da
arhiva ovog čvora sadrži svako označeno očitanje u točnom izvornom nizu, pa u
jednoj transakciji briše očitanja i njihove verzije. Ako ijedna vrijednost
nedostaje ili se razlikuje, ne briše ništa.

To još nije puni raspodijeljeni zaborav. Nema potvrde da izdanje drže svi
aktivni čvorovi ni zapisa o zaboravu koji bi spriječio da drugi čvor kasnije
vrati obrisane verzije. Zato je sadašnji postupak namijenjen kontroliranom
pospremanju na čvoru koji drži provjerenu arhivu, a sljedeća pravila ostaju
cilj mrežne izvedbe.

**Uvjet je „svi", ne „većina".** Ako obriše većina, manjina je možda upravo
čvor koji izdanje nikad nije preuzeo. Briše se kad **svaki aktivan član javi
da drži izdanje** koje te zapise sadrži.

**Brisanje mora biti zapis koji putuje.** Ako čvor lokalno obriše očitanja, a
drugi ih još ima, pri sljedećoj razmjeni dobije ih natrag — podaci uskrsnu, i
to tiho. Zato se izdaje zapis o zaboravu („očitanja letve Batina do 31.12.2026.
ušla su u izdanje 2027.1, otisak X"), koji se sinkronizira i ostaje kao jedini
trag zašto ih više nema.

**Svaki čvor provjerava sam.** Prije brisanja provjeri da arhiva koju **on**
drži doista pokriva svaki zapis koji odlazi — ne vjeruje tuđoj tvrdnji. Isto
pravilo već ima `cmd/selidba-arhive`, koji odbija posao ako pokrivenost ne
vrijedi.

Knjiga verzija ima `archived`, kojim zapis nestaje s površine a ostaje u
knjizi. To je dobro za povlačenje, ali ne oslobađa prostor — za zaborav se
brišu i verzije.

### 6. Umirovljenje čvora  *(nije napravljeno)*

Bez ovoga korak 5 nikad ne krene: jedan ugašen prijenosnik zamrzne zaborav
cijeloj mreži.

Čvor prestaje raditi iz običnih razloga — djelatnik otišao u mirovinu,
računalo zamijenjeno, ispostava se ugasila. Članstvo već ima `expires_at`, pa
potvrda koja istekne prestaje vrijediti. To je dobra osnova, ali nije dovoljno:

**Umirovljenje je odluka čovjeka, ne istek vremena.** Automatsko izbacivanje
nakon devedeset dana tišine izbacilo bi i onoga tko je bio na bolovanju. Zato
ga donosi nositelj ključa mreže, i zapisuje se s razlogom i datumom — jednako
kao proglašenje obrane.

**Prije umirovljenja program mora reći što se gubi.** Kad je čvor zadnji put
razmijenio, koja izdanja drži, i ima li verzija koje nitko drugi nema. Ako
ima, umirovljenje ih briše — i to operater mora vidjeti prije nego što
potvrdi, a ne otkriti poslije.

**Umirovljeni čvor ne broji se u „svi".** Time zaborav opet može teći.

**Povratak nije nastavak.** Čvor koji se javi nakon umirovljenja mora se
primiti iznova, kao nov. Mreža je u međuvremenu zaboravila zapise koje on
možda još drži; da nastavi gdje je stao, gurnuo bi ih natrag.

**Upozorenje prije zastoja.** Program treba javiti „čvor Osijek nije potvrdio
izdanje 2027.1 šest mjeseci", da se čvor potjera dok je to još sitnica — a ne
da se otkrije tek kad zaborav stane.

## Što je već napravljeno

- razdvajanje arhive od operative, `cmd/arhiva-vodostaja` i `cmd/selidba-arhive`
- spojeni niz: jedan satni i jedan dnevni po veličini, sa znanom točnošću
- ispravci arhive uz obvezan pregled, kroz knjigu verzija
- uvoz očitanja iz zalijepljenog ispisa, CSV-a i Excela
- vremenske zone po izvoru: hrvatski izvori u lokalnom, mađarski u UTC-u
- potpisana `.cop` izdanja po letvi, katalog i zaštita od nenamjernog povratka
  na starije izdanje
- pregled, ulaganje i stroga provjera operativnih očitanja prije označavanja
- lokalno zaboravljanje uloženih očitanja i njihovih verzija tek nakon ponovne
  provjere arhive
- automatsko satno preuzimanje javnih vodostaja, uz ručno pokretanje i zaštitu
  od duplikata
