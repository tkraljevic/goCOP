# Službeni zapisi, arhiva, izdanja i zaborav

Zapis odluka o tome gdje koji podatak živi, kako iz nacrta postaje služben,
što se čuva trajno i kako se prestaje čuvati. Nastalo iz rada na Batini,
rujan 2026.

## Zašto uopće

Operativna baza nosi uz svaki zapis i verziju u knjizi, jer se sinkronizira.
Očitanje tako stoji oko 1.360 bajta. Povijesni niz u arhivi stoji oko 20.
Batina je sama, s 45.547 očitanja, zauzimala 61 % glavne baze; sve što čeka na
Dunavu i Dravi značilo bi u tom obliku oko šest gigabajta koje bi svaki čvor
morao prenijeti i držati.

Zato su razdvojene, i zato treba pravilo kad se što seli i kad se briše.

## Životni ciklus zapisa

Pojam **arhiva** sam nije dovoljno širok za sve što goCOP čuva. Temeljni
pojam je **repozitorij službenih zapisa**: uređeno spremište svega što je
objavljeno ili ovjereno i zato više nije nacrt.

`nacrt → objava ili ovjera → službeni zapis → trajno čuvanje ili kontrolirano izlučivanje`

- **Nacrt (Draft)** pripada radnom prostoru. Može se mijenjati i nije dio
  službene građe.
- **Objavljen ili ovjeren zapis** zaključan je zajedno s prilozima, potpisom i
  prikazom koji je korisnik potvrdio. Od tog trenutka pripada repozitoriju
  službenih zapisa.
- **Ispravak** ne prepisuje službeni zapis. Nastaje novi povezani zapis, a
  prethodni ostaje čitljiv i označen kao zamijenjen ili povučen.
- **Arhivska građa** dio je službenih zapisa određenih za dugoročno ili trajno
  čuvanje.
- **Službeni zapis s rokom čuvanja** jednako je nepromjenjiv, ali se nakon
  propisanog roka može kontrolirano izlučiti. Zaborav zato nije obično
  brisanje nego evidentirana odluka prema pravilima čuvanja.

U tehničkom smislu svi objavljeni i ovjereni sadržaji mogu odmah prijeći iz
radnog prostora u isto nepromjenjivo spremište. Njihov rok čuvanja ipak nije
nužno isti: status službenog zapisa i odluka o trajnoj arhivskoj vrijednosti
dvije su odvojene stvari.

## Podjela

| | sadržaj | svojstva |
|---|---|---|
| `gocop.db` → `readings` | ono što ured radi: dnevni vodostaji koje operater prikuplja, satni **dok traje obrana**, očitanje s terena u bilo koje doba | verzionirano, sinkronizira se |
| `data/vodostaji.db` | povijest: HIS-2000, telemetrija, mađarski nizovi, rekonstrukcije, protok, temperatura, nanos | bez verzija; razmjenjuje se potpisanim `.cop` izdanjima, odvojeno od knjige verzija |

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

**Uvjet je „svi određeni čuvari arhive", ne „većina".** Nakon uvođenja
selektivnih pretplata ne mora svaki prijenosnik držati svaku arhivu. Za svaki
kanal unaprijed se određuju čvorovi koji su njegovi čuvari; briše se tek kad
svaki aktivan čuvar potvrdi da drži izdanje koje zapise sadrži. Svi ostali
aktivni čvorovi moraju primiti katalog i zapis o zaboravu, ali ne i veliki
sadržaj. Tako prijenosnik vodočuvara ne zaustavlja zaborav arhive drugog
sektora, a odluka o tome tko čuva jedini primjerak nije prepuštena slučaju.

**Brisanje mora biti zapis koji putuje.** Ako čvor lokalno obriše očitanja, a
drugi ih još ima, pri sljedećoj razmjeni dobije ih natrag — podaci uskrsnu, i
to tiho. Zato se izdaje zapis o zaboravu („očitanja letve Batina do 31.12.2026.
ušla su u izdanje 2027.1, otisak X"), koji se sinkronizira i ostaje kao jedini
trag zašto ih više nema.

**Svaki čvor provjerava sam.** Prije brisanja provjeri da arhiva koju **on**
drži doista pokriva svaki zapis koji odlazi — ne vjeruje tuđoj tvrdnji. Isto
pravilo već ima `selidba-arhive` (sad u `tools/migrations/`), koji odbija posao ako pokrivenost ne
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

### 7. Objava i ovjera kao ulaz u repozitorij  *(djelomično napravljeno)*

**Stanje 25. 9. 2026.** Spremište velikih sadržaja u `sadrzaj.db` radi, a
objavljene prijave s terena i njihovi PDF-ovi već se zaključavaju i odvajaju
od operativne baze. Pojedini ovjereni tokovi (dnevni listovi, dnevnici COP-a i
akti) također izrađuju nepromjenjivi potpisani izvornik. Još nije napravljeno
jedinstveno kazalo svih službenih zapisa ni isti atomski prijelaz za svaki
modul, pa tekst ispod ostaje cilj zajedničkog modela.

Starost nije glavni okidač za dokumente. **Objava ili ovjera trenutak je u
kojem radni zapis postaje službeni zapis.** Do tada se nacrt i prilozi
mijenjaju u operativnoj bazi. Objavom ili ovjerom izrađuje se konačni prikaz,
zaključavaju sadržaj i prilozi, izračunava otisak te predmet ulazi u
repozitorij službenih zapisa. Politika čuvanja zatim određuje čuva li se
trajno kao arhivska građa ili do isteka propisanog roka.

Postupak mora biti atomski:

1. zapisati predmet i velike sadržaje u repozitorij;
2. ponovno ih pročitati i provjeriti otiske;
3. u glavnoj bazi ostaviti malo kazalo — identitet, vrstu, datum, autora,
   doseg, stanje, otisak i mjesto arhive;
4. tek tada ukloniti velike sadržaje iz glavne baze i njezine knjige verzija.

Ako bilo koji korak ne uspije, predmet ne smije ostati napola ovjeren ili bez
sadržaja. Knjiga verzija ne nosi kopije fotografija, skenova i PDF-ova, nego
samo njihove identitete, veličine i kriptografske otiske.

Službeni predmet ne otvara se za uređivanje. Pogreška se ispravlja novim
objavljenim ili ovjerenim predmetom koji navodi što zamjenjuje; izvorni ostaje
čitljiv i označen kao zamijenjen. Naknadno se mogu dodavati vanjske oznake za
pretragu i pravila čuvanja, ali one ne mijenjaju zapečaćeni sadržaj.

Prvo se ovako arhiviraju sadržajno veliki završeni predmeti:

- ovjereni dnevni listovi, uključujući fotografije upisa vodočuvara i
  rukovoditelja;
- ovjerene prijave i izvješća s terena;
- potpisani akti;
- izvorne fotografije, skenovi, konačni PDF-ovi i drugi veliki prilozi.

`gocop.db` ostaje operativna baza i zajedničko kazalo. Posebna arhivska baza
uvodi se po modulu samo kad količina i način čitanja to opravdavaju — kao što
je već slučaj s vodostajima. Veliki nepromjenjivi sadržaji čuvaju se jednom,
po SHA-256 otisku, u zajedničkom spremištu; isti prilog se ne umnaža zato što
ga prikazuju dnevnik, prijava i PDF.

### 8. `.cop` kanali i selektivna sinkronizacija  *(djelomično napravljeno)*

**Stanje 25. 9. 2026.** `.cop` inačica 3 već radi za kanale očitanja,
dnevnika i prijava: paket nosi potpisani manifest, zapise, popis sadržaja i po
izboru same sadržaje, a uvoz provjerava potpis, otiske i red izdanja. Pretplate
po vrsti, području, godinama i razini sadržaja rade i u mrežnoj razmjeni.
Hidrološka arhiva ostaje u svojoj inačici 2 po postaji. Nisu još dovršeni svi
planirani kanali, zajednički katalog na mreži, prijenos sadržaja po dijelovima
ni dohvat na zahtjev.

`.cop` nije nova neovisna aplikacijska baza. To je potpisano izdanje jednog
kanala repozitorija službenih zapisa i njegove zajedničke povijesti, a ne samo
paket stare arhive. Planirani kanali su najmanje jezgra i registri,
vodostaji, dnevnici, prijave i izvješća, akti te održavanje. Paket se može
dodatno suziti na sektor, branjeno područje, postaju i razdoblje, primjerice
`dnevnici-BP34-2026.cop` ili `vodostaji-Batina-1985-2024.cop`.

Paket nosi potpisani manifest, vrstu i verziju kanala, obuhvat, zapise,
popis potrebnih sadržaja, njihove veličine i otiske te vezu na prethodno
izdanje. Danas se sadržaji prenose cijeli, uz ograničenje količine po jednom
krugu razmjene. Plan je velike datoteke dijeliti na provjerljive dijelove kako
bi se prijenos mogao nastaviti nakon prekida i kako se već postojeći sadržaj
ne bi preuzimao ponovno.

Automatska razmjena koristi isti manifest i dijelove kao ručna `.cop`
datoteka. Razlika je samo put: mreža ih prenosi izravno između uparenih i
ovlaštenih čvorova, a `.cop` datoteka omogućuje USB ili drugi izvanmrežni
prijenos. To je zatvoren, potpisan sustav nalik torrentu — nema javnih
trackera ni nepoznatih sudionika, a primljeni sadržaj postaje izvor drugim
ovlaštenim čvorovima tek nakon potpune provjere.

Svaki čvor bira pretplatu:

- module i arhivske kanale;
- sektore, branjena područja i postaje;
- razdoblje;
- samo katalog, umanjene preglede ili pune izvornike;
- koliko dugo pune sadržaje drži lokalno.

Svi čvorovi dobivaju malo zajedničko kazalo pa znaju da predmet postoji.
Puni PDF ili fotografiju čvor bez trajne pretplate može dohvatiti na zahtjev.
Središnji čvorovi i određeni čuvari kanala drže trajne potpune primjerke;
prijenosnici smiju imati samo svoj operativni doseg i privremene sadržaje.

## Što je već napravljeno

- razdvajanje arhive od operative, `arhiva-vodostaja` i `selidba-arhive` (sad u `tools/`)
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
