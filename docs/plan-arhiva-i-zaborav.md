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

Arhiva se **preuzima**, ne razmjenjuje. U redovnu sinkronizaciju ide samo
katalog: koja letva, koji izvor, koje razdoblje, koliki otisak.

## Redoslijed posla

### 1. Izdanje arhive  *(nije napravljeno)*

Arhiva dobiva broj izdanja, zaključni datum i otisak. Objavljuje se jednom
godišnje, kao godišnjak.

Bez izdanja i otiska nema se što potvrđivati, pa je ovo prvo.

Novije izdanje **mijenja i stare godine**, ne samo dodaje nove: HIS-2000 kasni
s ovjerom godinu do dvije, pa izdanje 2027. za 2026. ima samo operativne i
telemetrijske vrijednosti, a tek izdanje 2029. donosi ovjerene. Preuzimanje
zato nije „dodaj razliku" nego „zamijeni izdanje".

### 2. Katalog koji objavljuje izdanje  *(nije napravljeno)*

Katalog putuje redovnom sinkronizacijom, pa svaki čvor vidi da postoji novije
izdanje i ponudi preuzimanje. Pola te mehanike već stoji u `nizovi`.

### 3. Godišnje ulaganje operative u arhivu  *(nije napravljeno)*

Kad se godina zatvori, operativna očitanja te godine postaju izvor u arhivi,
uz oznaku `gocop` — odvojeno od HIS-a i telemetrije, jer to nije isto.

Time operativna baza prestaje rasti bez kraja: drži tekuću godinu i otvorene
epizode. Dvadeset letava puta 365 dnevnih očitanja je 7.300 zapisa godišnje.

**Ulaganje nije brisanje.** Zapisi se označe kao izdani i prestaju se
sinkronizirati, ali ostaju dok se ne zaborave (korak 5).

Oznaka mora nešto značiti upitu. Zapis koji ostane u `readings` bez oznake
koju upit poštuje i dalje se prikazuje i dalje raste — dakle treba stupac
„ovo je ušlo u izdanje X" i upiti koji ga u operativnom pogledu preskaču.

### 4. Automatsko povlačenje s letva.voda.hr  *(nije napravljeno)*

Pristup, pozivi i vjerodajnice opisani su u `vodostaji/PRISTUP letva-voda-hr/README.md`.
Kad program sam povlači, lijepljenje ispisa postaje iznimka umjesto pravila.

### 5. Zaborav  *(nije napravljeno)*

Označeni zapisi se brišu — i sami i njihove verzije — tek kad je sigurno da ih
arhiva doista nosi svugdje.

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
