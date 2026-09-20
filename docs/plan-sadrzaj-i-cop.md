# Spremište sadržaja i `.cop` izdanja repozitorija službenih zapisa

Definicija dviju stvari koje plan *Službeni zapisi, arhiva, izdanja i zaborav*
(`plan-arhiva-i-zaborav.md`, točke 7 i 8) traži, a još nisu izvedene:
gdje žive veliki sadržaji i kako ih `.cop` prenosi. Cilj nije brisanje.
Cilj je da glavna baza raste samo s brojem zapisa, ne s njihovim
megabajtima, i da se sadržaj sinkronizira odvojeno od knjige verzija,
po otisku, koliko koji čvor treba. Rujan 2026.

## Zašto sad

Stanje knjige verzija 20. 9. 2026., nakon uvoza 420 obavijesti s terena:

| što | verzija | MB |
|---|---|---|
| `prijave_izvornici` (PDF kao base64 u zapisu) | 371 | 264 |
| svih ostalih 40 vrsta zajedno | 14 256 | 5 |

Isti PDF stoji i u tablici `prijave_izvornici`, pa ga baza nosi dvaput, a
svaki čvor u mreži prima svih 264 MB bez obzira treba li mu. Isti obrazac
čeka `vodocuvarski_izvornici`, `journal_izvornici`, `akti_izvornici` i
`potpisi_slike`. Dnevni listovi s fotografijama upisa bit će brojniji od
prijava.

## Tri stvari, tri mjesta

| | gdje | što nosi | raste s |
|---|---|---|---|
| operativa i kazalo | `data/gocop.db` | zapisi, knjiga verzija, kazalo službenih zapisa | brojem zapisa |
| sadržaj | `data/sadrzaj.db` | PDF, fotografija, sken, potpis, karta: bajtovi po SHA-256 otisku | onim što čvor prati |
| arhiva vodostaja | `data/vodostaji.db` | povijesni nizovi (već postoji) | izdanjima koja čvor drži |

Pravilo koje sve drži: **knjiga verzija nikad ne nosi bajtove sadržaja.**
Zapis u knjizi nosi otisak, veličinu i vrstu. Bajtovi žive u spremištu
sadržaja i prenose se svojim putem. Tako ni migracija ni zaborav ne diraju
knjigu: ona ostaje append-only, što joj daje dokaznu vrijednost.

## 1. Spremište sadržaja: `data/sadrzaj.db`

Zasebna SQLite datoteka uz glavnu, bez knjige verzija. Sadržaj je
nepromjenjiv i imenovan svojim otiskom, pa mu verzije ne trebaju: isti
bajtovi imaju isti otisak na svakom čvoru, a drukčiji bajtovi su drugi
sadržaj.

```sql
CREATE TABLE sadrzaj (
    otisak      TEXT PRIMARY KEY,   -- SHA-256, hex, mala slova
    vrsta       TEXT NOT NULL,      -- application/pdf, image/jpeg, image/png
    bajtova     INTEGER NOT NULL,
    podaci      BLOB NOT NULL,
    primljeno   DATETIME NOT NULL,  -- kad je ovaj čvor sadržaj dobio
    izvor       TEXT NOT NULL       -- 'ovdje', 'cvor:<id>', 'cop:<paket>'
);
CREATE TABLE sadrzaj_veze (          -- tko sadržaj drži živim
    otisak      TEXT NOT NULL REFERENCES sadrzaj(otisak),
    entitet     TEXT NOT NULL,      -- 'prijave', 'vodocuvarski_listovi', 'akti' ...
    entitet_id  TEXT NOT NULL,
    uloga       TEXT NOT NULL,      -- 'izvornik', 'slika', 'sken', 'potpis', 'karta'
    PRIMARY KEY (otisak, entitet, entitet_id, uloga)
);
CREATE TABLE sadrzaj_zeljen (        -- što čvor zna da postoji, a još nema
    otisak      TEXT PRIMARY KEY,
    vrsta       TEXT NOT NULL,
    bajtova     INTEGER NOT NULL,
    trazeno     DATETIME,           -- zadnji pokušaj dohvata
    razlog      TEXT NOT NULL       -- 'pretplata', 'na zahtjev', 'cuvar'
);
```

Pravila:

- **Upis je jedna transakcija s provjerom.** Prije `INSERT` izračuna se
  otisak primljenih bajtova; ako se ne slaže s očekivanim, ništa se ne
  upisuje. Isti otisak drugi put je uspjeh bez upisa.
- **Sadržaj bez veze ne postoji dugo.** Veza nastaje u istoj transakciji kao
  službeni zapis koji ga navodi. Sadržaj bez ijedne veze je siroče i smije
  se ukloniti pri pospremanju, jer ga nijedan zapis ne traži.
- **Uklanjanje s čvora nije zaborav.** Čvor smije ukloniti bajtove sadržaja
  koji nije dužan čuvati (nije čuvar kanala), a kazalo i otisak ostaju:
  sadržaj se opet može dohvatiti od čuvara. Čuvar ne uklanja ništa.
- **Dijelovi za prijenos** računaju se iz bajtova (1 MiB, otisak svakog
  dijela) i ne pohranjuju se; isti sadržaj daje iste dijelove svugdje, pa
  prekinut prijenos nastavlja gdje je stao, a dio koji već imamo ne
  preuzima se ponovno.

Što u spremište ide odmah: PDF izvornici prijava, dnevnih listova, COP
dnevnika i akata; fotografije prijava (smanjene) i, za PRIJAVU koja ide iz
kuće, izvorne fotografije s telefona trajno, jer je u PDF-u njihov otisak;
skenovi; slike potpisa; karte ugrađene u PDF ne, one su dio PDF-a.

## 2. Kazalo službenih zapisa u `gocop.db`

Jedno kazalo za sve module, da repozitorij bude jedan a ne pet. Puni se u
trenutku objave ili ovjere, u istoj transakciji, i putuje knjigom verzija
kao mali zapis (`EntitySluzbeniZapisi`, kanal po vrsti i području).

```sql
CREATE TABLE sluzbeni_zapisi (
    id           TEXT PRIMARY KEY,   -- UUIDv7
    vrsta        TEXT NOT NULL,      -- 'prijava', 'dnevni_list', 'dnevnik_cop', 'akt', ...
    entitet      TEXT NOT NULL,      -- tablica modula
    entitet_id   TEXT NOT NULL,
    naslov       TEXT NOT NULL,
    datum        DATE NOT NULL,      -- datum na koji se zapis odnosi
    objavljeno   DATETIME NOT NULL,
    autor_id     TEXT NOT NULL,
    sektor       TEXT NOT NULL,
    area_id      INTEGER NOT NULL DEFAULT 0,
    stanje       TEXT NOT NULL,      -- 'vazeci', 'zamijenjen', 'povucen'
    zamjenjuje   TEXT,               -- id zapisa koji ovaj ispravlja
    otisak       TEXT NOT NULL,      -- otisak glavnog sadržaja (PDF)
    sadrzaji     TEXT NOT NULL,      -- JSON: [{otisak, vrsta, bajtova, uloga}]
    cuvanje      TEXT NOT NULL,      -- oznaka pravila čuvanja
    cvor         TEXT NOT NULL
);
```

Moduli zadržavaju svoje tablice i logiku; službenost i priloge vodi kazalo.
Popis pravila čuvanja (`cuvanje`: trajno, 10 godina, 5 godina ...) je
tablica koju uređuje administrator i koja putuje razmjenom, jer popis s
rokovima čuvanja u Hrvatskim vodama postoji neovisno o programu.

## 3. Što se mijenja u knjizi verzija

Zapisi vrsta `*_izvornici` i `potpisi_slike` više ne nose bajtove. Oblik
zapisa postaje:

```json
{"id": "...", "otisak": "9d0f...", "vrsta": "application/pdf", "bajtova": 494871,
 "sazetak": "...", "updated_at": "..."}
```

Primjena verzije (`apply.go`) upiše red u tablicu modula s otiskom umjesto
PDF-a i, ako čvor prema pretplati sadržaj želi, doda otisak u
`sadrzaj_zeljen`. Prikaz na čvoru koji sadržaj još nema pokazuje kazalo i
nudi dohvat, kao danas za fotografiju koje više nema.

**Migracija** postojećih zapisa je jednokratna i lokalna na svakom čvoru:
za svaku verziju s bajtovima izračuna se otisak, bajtovi upišu u
`sadrzaj.db`, a verzija se u knjizi zamijeni sažetim oblikom istog
`version_id`. To je jedina iznimka od append-only pravila i radi se jednom,
prije nego mreža dobije drugi čvor. Očekivani učinak danas: `gocop.db`
sa 481 MB na oko 200 MB (ostaje tablica izvornika dok se i ona ne preseli),
zatim na desetak MB; `sadrzaj.db` oko 200 MB.

## 4. Prijenos sadržaja između čvorova

Razmjena knjige verzija ostaje kakva jest, samo bez bajtova. Uz nju ide
drugi, neovisan kanal:

- `GET /sync/sadrzaj/{otisak}` vraća opis: vrsta, veličina, popis dijelova
  s otiscima.
- `GET /sync/sadrzaj/{otisak}/{dio}` vraća jedan dio.
- Primatelj provjeri otisak dijela odmah, cijelog sadržaja na kraju, i tek
  onda upiše. Ništa se ne upisuje napola.
- Čvor nudi samo sadržaj koji ima i koji je smio dati (ista pravila
  ovlasti kao za zapise).

Što čvor dohvaća sam, bez pitanja, određuje pretplata (`peers/subscriptions`
proširena s razinom): **kazalo** (ništa), **pregled** (smanjene slike, ne
PDF-ove), **sve**. Uredski čvor i čuvari kanala su na *sve*; prijenosnik
vodočuvara na *pregled* za svoje područje, ostalo na zahtjev. Zahtjev je
klik na dokument: čvor ga tada dohvati od bilo kojeg uparenog čvora koji ga
ima i zadrži ga onoliko koliko pretplata kaže.

### Pretplata čvora

Arhive se sinkroniziraju automatski prema pretplati svakog čvora, slično
torrentu, ali unutar zatvorene i potpisane goCOP mreže. Svaki čvor određuje:

- module (kanale);
- sektore i branjena područja;
- razdoblje;
- samo kazalo, umanjene prikaze ili pune izvornike;
- koliko dugo sadržaj drži lokalno.

```
Prijenosnik vodočuvara:  BP 16, dnevnici i prijave; pregledi uvijek,
                         izvornici zadnjih 90 dana, stariji na zahtjev
COP Osijek:              cijeli sektor B, svi izvornici i sve arhive; čuvar
Središnji čvor:          svi sektori, trajna potpuna arhiva; čuvar
```

Koraci razmjene:

1. Čvorovi razmjenjuju mala potpisana kazala (kazalo službenih zapisa i
   katalog izdanja) redovnom sinkronizacijom knjige verzija.
2. Čvor uspoređuje otiske iz kazala sa svojom pretplatom i onim što već
   drži; što nedostaje a pretplata traži, ide u `sadrzaj_zeljen`.
3. Nedostajući sadržaj traži od bilo kojeg uparenog i ovlaštenog čvora koji
   ga ima.
4. Velike datoteke prenose se u dijelovima; prijenos se nastavlja nakon
   prekida.
5. Svaki dio i cijeli sadržaj provjeravaju se otiskom; otisak je vezan
   potpisanim zapisom čvora koji je sadržaj objavio, pa se ne potpisuje
   svaki bajt nego zapis koji ga navodi.
6. Tek nakon potpune provjere čvor sadržaj nudi drugim ovlaštenim
   čvorovima.

Što to nije: javni BitTorrent. Nema trackera ni nepoznatih sudionika,
prenose samo upareni čvorovi s važećom potvrdom mreže, veza je TLS s
prikovanim ključevima čvorova (već tako radi u `razmjena`), ovlasti se
provjeravaju po modulu i području, a svaki zapis nosi potpis čvora koji ga
je stvorio.

Dva pravila koja iz pretplate slijede, da "koliko dugo lokalno" ne
postane gubitak:

- **Vlastiti sadržaj se ne uklanja dok ga ne drži čuvar.** Fotografija
  koju je vodočuvar snimio na prijenosniku smije nestati s njega tek kad
  barem jedan čuvar kanala potvrdi da je ima; do tada rok od 90 dana ne
  teče. Inače bi prijenosnik koji tjednima nije bio na mreži bio jedini
  primjerak.
- **Bez mreže vrijedi ono što je preuzeto.** Prijenosnik koji je na terenu
  otvara samo što je pretplata već dovukla; zato je za vodočuvara razina
  *pregled* najmanje što ima smisla, a *samo kazalo* je za čvorove koji su
  stalno na mreži.

Čvor koji drži samo kazalo odmah vidi da dokument postoji, njegov datum,
autora, doseg i veličinu; puni PDF ili fotografiju preuzima kad ga otvori.
Tako mreža ima potpunu zajedničku arhivu, a nijedan prijenosnik ne mora
nositi sve.

## 5. `.cop` izdanje kanala

`.cop` je već ZIP s potpisanim manifestom, otiscima dijelova i podacima jedne
letve (`internal/arhiva/paket.go`, inačica 2). Inačica 3 ga poopćuje na
izdanje bilo kojeg kanala repozitorija, a stara izdanja letava ostaju
čitljiva.

```
manifest.json          potpisani opis (dolje)
zapisi.sqlite          knjiga verzija kanala, ista shema kao record_versions
                       (ono što peers/archive.go ExportFile već piše)
kazalo.json            redci sluzbeni_zapisi obuhvaćeni izdanjem
sadrzaj/<otisak>       bajtovi sadržaja, po jedan unos po otisku (može izostati)
vodostaji/...          dijelovi letve kao u inačici 2 (samo kanal vodostaji)
```

Manifest, uz postojeća polja:

```json
{"inacica": 3,
 "kanal": "prijave",                       // jezgra | vodostaji | dnevnici | prijave | akti | odrzavanje
 "obuhvat": {"sektor": "B", "area_id": 34, "postaja": "", "od": "2026-01-01", "do": "2026-12-31"},
 "izdanje": 4, "prethodno": "<otisak izdanja 3>",
 "izdao": "cop-osijek-node", "nastalo": "...",
 "otisak": "<otisak sadržaja izdanja>",
 "zapisa": 1234, "sluzbenih": 371,
 "sadrzaji": [{"otisak": "...", "vrsta": "application/pdf", "bajtova": 494871, "ukljucen": true}],
 "dijelovi": [...], "potpis": {...}}
```

Pravila, ista kao za letve:

- **Otisak izdanja ovisi samo o sadržaju** (zapisi, kazalo i popis
  sadržaja s otiscima), ne o tome jesu li bajtovi uključeni ni kad je paket
  složen. Isti sadržaj daje isto izdanje i isti otisak; broj raste samo kad
  se sadržaj promijeni.
- **Paket bez bajtova je valjano izdanje.** `ukljucen: false` znači da
  primatelj sadržaj dohvaća mrežom ili iz drugog paketa. Tako katalog i
  kazalo stanu u kilobajte i mogu putovati e-poštom.
- **Ugradnja je ista kao razmjena:** verzije koje čvor nema uđu u knjigu,
  površina se osvježi, sadržaji s `ukljucen: true` upišu se u spremište
  nakon provjere otiska, ostali u `sadrzaj_zeljen`.
- **Zaštita od povratka** (`primljena.go`): novije ugrađeno izdanje kanala se
  ne zamjenjuje starijim bez razloga, isto izdanje s drugim otiskom se
  odbija.
- **Mreža i USB koriste isti manifest.** Čvor koji objavi izdanje u
  katalogu daje drugima isti popis dijelova; preuzimanje po dijelovima i
  provjera su isti kôd kao za `.cop` datoteku.

## 6. Redoslijed izvedbe

1. `sadrzaj.db` i paket `internal/sadrzaj` (upis s provjerom, čitanje,
   dijelovi, veze, siročad). Mali, testabilan sam za sebe.
2. Prijave prelaze na spremište: objava upisuje PDF u `sadrzaj.db`, knjiga
   nosi otisak; migracija 371 postojeća zapisa. Ovdje se vidi dobitak od
   264 MB.
3. Kazalo službenih zapisa; prijave ga pune prve, zatim dnevni listovi,
   COP dnevnici i akti kad prelaze na spremište.
4. Prijenos sadržaja između čvorova s razinom pretplate.
5. `.cop` inačica 3 s kanalom `prijave`, pa ostali kanali; vodostaji ostaju
   kako jesu dok se ne poopće.

Točke 5 i 6 plana o arhivi (mrežni zaborav, umirovljenje čvora) ne ovise o
ovome i mogu čekati; s ovim redoslijedom glavna baza prestaje rasti s
megabajtima već nakon koraka 2.
