# Predlošci uvoza

Predlošci za organizacije koje goCOP pune svojim podacima, a nemaju čvor od
kojeg bi ih dobile razmjenom. Svaki predložak ima nekoliko redaka izmišljenih
podataka. Sve osobe, adrese, telefoni, OIB-i i nazivi u njima su izmišljeni.
Jedina osoba je Pero Perić (`pperic`), adrese su „Ulica primjera”, a e-pošta
ide na `example.com`.

Program ne nosi podatke u sebi. U prazan čvor ulaze na tri načina: razmjenom s
uparenim čvorom, ručnim upisom ili uvozom iz datoteke. Uvoz treba samo prvom
čvoru u mreži. Ostali čvorovi podatke dobiju sinkronizacijom. Pregled svih
uvoza u programu nalazi se na stranici **Administracija › Uvozi podataka**.

Svaki predložak provjerava test, kroz isti kod kojim se uvozi. Kad se uvoz
promijeni, a predložak ne, test padne.

| Predložak | Kamo ide | Test |
|---|---|---|
| [`registri/sektori.csv`](registri/sektori.csv) | Administracija › Uvozi podataka › CSV | `internal/web/predlosci_uvoza_test.go` |
| [`registri/branjena-podrucja.csv`](registri/branjena-podrucja.csv) | isto | isto |
| [`registri/licencirane-firme.csv`](registri/licencirane-firme.csv) | isto | isto |
| [`registri/zupanije.csv`](registri/zupanije.csv) | isto | isto |
| [`registri/gradovi-i-opcine.csv`](registri/gradovi-i-opcine.csv) | isto | isto |
| [`registri/naselja.csv`](registri/naselja.csv) | isto | isto |
| [`prvo-pokretanje/*.json`](prvo-pokretanje/) | mapa uz bazu, pri prvom pokretanju | `internal/db/predlosci_test.go` |
| [`ocitanja/ocitanja-letve.csv`](ocitanja/ocitanja-letve.csv) | stranica letve › Zalijepi očitanja | `internal/web/predlosci_uvoza_test.go` |
| [`ocitanja/niz-za-arhivu.csv`](ocitanja/niz-za-arhivu.csv) | Administracija › Unos u arhivu | isto |
| [`ocitanja/tablica-dnevnih-vodostaja.csv`](ocitanja/tablica-dnevnih-vodostaja.csv) | naredba `gocop -tablica` | `internal/importer/csvlevels/predlozak_test.go` |
| [`odrzavanje/ugovor-a02.xlsx`](odrzavanje/ugovor-a02.xlsx) | Održavanje › uvoz ugovora, ili `gocop -ugovor` | `internal/importer/ugovor/predlozak_test.go` |
| [`geometrija/tok-vodotoka.geojson`](geometrija/tok-vodotoka.geojson) | Registri › vodotok › Učitaj GeoJSON | `internal/service/predlozak_geometrije_test.go` |

Predlošci se međusobno slažu. Tablica vodostaja i ugovor pozivaju se na
postaju, objekt i vode iz predložaka prvog pokretanja, a test ih uvozi upravo
na bazu napunjenu iz tih predložaka.

## Općenito o CSV-u

- **Kodiranje je UTF-8.** Predlošci počinju oznakom BOM, da ih Excel otvori s
  ispravnim slovima. U Excelu spremajte kao „CSV UTF-8”. Obični „CSV” na
  hrvatskom Windowsu sprema u Windows-1250. Registri i očitanja tada dobiju
  iskrivljena slova (č, ć, š, ž, đ), a uvoz to ne prijavi. Windows-1250 sam
  prepoznaje samo tablica dnevnih vodostaja.
- **Razdjelnik je točka-zarez.** Uvoz registara uzima zarez samo kad ga u
  prvom retku ima više nego točaka-zareza.
- **Decimalni zarez:** `123,45`. Prima se i decimalna točka.
- **Prvi redak je zaglavlje.** Stupci se čitaju po redoslijedu, ne po nazivu.
  Ne premještajte ih i ne brišite one u sredini. Prazan stupac je dopušten.
- **Najviše 4 MiB** po datoteci registra.

## Registri (CSV)

Uvoze se na stranici **Administracija › Uvozi podataka**, u odjeljku „CSV:
izvoz i uvoz registara”. Uvoz sektora i branjenih područja nalazi se i na
stranici Organizacija. Za sve treba globalni administrator. Licencirane firme
smije uvoziti i ovlaštenik za praćenje ugovora, za svoje područje.

Oblik je isti u oba smjera: izvezite tablicu, uredite je u Excelu i uvezite
natrag. Predlošci imaju iste stupce kao izvoz, a test to provjerava.

**Redoslijed je važan:** sektori, pa branjena područja, pa firme. Zatim idu
županije, pa gradovi i općine, pa naselja. Svaki sljedeći poziva se na brojeve
prethodnog.

**Greške:** svaki redak se upisuje zasebno. Redak s greškom se preskače, a
ostali ostaju upisani. Nakon uvoza poruka kaže koliko je redaka novih, koliko
osvježenih i koji su preskočeni, s razlogom. Za teritorij i firme poruka
navodi prvih pet preskočenih. Uvoz ništa ne briše. Ispravite preskočene retke
i pošaljite istu datoteku ponovno. Redak koji je već ušao samo se osvježi.

### sektori.csv

`Oznaka;Naziv;Vodnogospodarski odjel;Centar obrane od poplava;Adresa;Telefon;E-pošta;Razina;Telefon odjela`

- **Oznaka** je obavezna: velika slova, brojke, `-` ili `_`, najviše 20
  znakova (`A`, `B`, `DIREKCIJA`). Postojeća oznaka se osvježi, nova se doda.
- **Naziv** je obavezan.
- **Razina:** `1` je krovna jedinica, `2` je sektor.
- Krovnoj jedinici dajte oznaku `DIREKCIJA`. Program je na nekoliko mjesta
  prepoznaje po toj oznaci (popisi sektora, zid, račun `admin`).
- Nazivi trećeg i četvrtog stupca mijenjaju se s nazivljem organizacije
  (Administracija › Nazivi). Izvoz piše nazive koje organizacija koristi.

### branjena-podrucja.csv

`Sektor;BP;Naziv;Vodnogospodarska ispostava;Podcentar obrane;Licencirane firme;Izravno pod razinom 2;Telefon ispostave`

- **Sektor** je obavezan i mora već postojati.
- **BP** je obavezan: cijeli broj, jedinstven u cijeloj organizaciji.
  Postojeći broj se osvježi.
- **Naziv** je obavezan.
- **Licencirane firme:** izvoz ga piše, a uvoz zanemaruje. Firme se vežu
  uvozom firmi.
- **Izravno pod razinom 2:** `da`, `1` ili `x` znači da područje nema
  ispostavu i pripada izravno sektoru.
- **Telefon ispostave** je osmi stupac. Uvoz ga čita, a izvoz ga piše, ali bez
  naziva u zaglavlju. Predložak naziv ima.
- Osvježavanje postojećeg područja preko CSV-a briše mu točku za vremenske
  prilike (širinu i dužinu) i naziv ugovorne pravne osobe. Te podatke nakon
  uvoza upišite ponovno u obrascu područja.

### licencirane-firme.csv

`Naziv;Kratki naziv;OIB;Adresa;Telefon;E-pošta;Osoba za obranu;Gdje radi;Aktivan;Napomena`

- **Naziv** je obavezan. Redak bez naziva se preskače bez poruke.
- **OIB:** 11 znamenki. Smije biti u omotu `="12345678900"`, kako ga piše
  izvoz, da Excel ne pojede vodeće nule. Kontrolna znamenka se ne provjerava.
- Firma se prepoznaje po OIB-u, a bez njega po nazivu. Prepoznata se
  osvježi, ostale se dodaju.
- **Gdje radi:** popis odvojen zarezom, npr. `BP 1, Sektor P`. Područje i
  sektor moraju postojati, inače se cijeli redak preskače. Ono što nije ni
  područje ni sektor zanemaruje se. Popis zamjenjuje sva dotadašnja mjesta
  rada firme.
- **Aktivan:** sve osim `ne` znači da je firma aktivna.

### zupanije.csv, gradovi-i-opcine.csv, naselja.csv

- **Broj:** prazno znači novi zapis, a program sam dodijeli broj. Broj
  postojećeg zapisa osvježava taj zapis. Izvezite tablicu nakon uvoza, da
  vidite koje je brojeve program dodijelio.
- **Naziv** je obavezan.
- Gradovi i općine trebaju **Županija broj**. Naselja trebaju **Grad/općina
  broj**, a županija se tada uzima od grada ili općine. Stupci s nazivom
  županije ili grada samo pomažu čitanju i ne uvoze se.
- **Vrsta:** `GRAD` ili `OPCINA`. Sve drugo tiho postaje `OPCINA`.
- **Web** je adresa stranice. Kad joj nedostaje `https://`, program ga doda
  sam.
- **Površina** prima decimalni zarez. Stanovnici su cijeli broj, a tisućice
  smiju biti odvojene točkom (`12.345`).

## Prvo pokretanje (JSON)

Datoteke iz [`prvo-pokretanje/`](prvo-pokretanje/) stavljaju se u **istu mapu
u kojoj je baza**. Na Linuxu je to `/var/lib/gocop/`, a pri ručnom pokretanju
`data/`, ako se baza ne zada drukčije. Program ih čita pri prvom pokretanju
praznog čvora, i to samo za tablicu koja je još prazna. Kasnije ih ne čita,
da ne pregazi ono što su ljudi u međuvremenu upisali. Svaka datoteka je
neobavezna. Datoteka koje nema znači da će se ti podaci upisati ručno ili
stići razmjenom.

Iznimka je `objekti_bp16.json`, koji se čita pri **svakom** pokretanju. Iz
njega se dodaju objekti čije šifre u registru nema. Objekt koji obrišete iz
registra vratit će se zato pri sljedećem pokretanju, sve dok je datoteka uz
bazu. Nakon prvog pokretanja maknite je iz mape.

**Greške:** neispravan JSON ili zapis koji se ne može upisati zaustavlja
pokretanje porukom „Greška pri unosu početnih podataka”, s imenom datoteke ili
zapisa. Tablice koje su se do tada upisale ostaju upisane. Najsigurnije je
ispraviti datoteku, obrisati novu bazu i pokrenuti ponovno.

| Datoteka | Što nosi | Važno |
|---|---|---|
| `organizacija.json` | `sectors` i `areas` | polja kao u CSV-u. `vgo_phone`, `vgi_phone`, `latitude` i `longitude` se pri prvom punjenju ne čitaju |
| `imenik.json` | djelatnici s dužnostima | `sector_id` i `area_id` dužnosti moraju postojati u `organizacija.json` |
| `territories.json` | županije › gradovi i općine › naselja, ugniježđeno | `id` se upisuje kako piše, a na njega se pozivaju ostale datoteke |
| `sections.json` | dionice s poddionicama, vodomjerima, objektima i nasipima | `watercourse_code` je šifra vode, vidi niže |
| `section_territories.json` | ugrožena naselja dionica | ide ovdje, ne u `parts[].territories` |
| `watercourses.json` | vodotoci | šifra se izvodi iz `official_name` |
| `objekti_bp16.json` | crpne stanice, ustave i drugi objekti | `kind`: `CRPNA_STANICA`, `USTAVA`, `SIFON`, `PREGRADA`, `NASIP`, `BRANA`, `OSTALO` |

Napomene:

- **Svi računi iz `imenik.json` dobiju početnu lozinku `gocop2026`** i moraju
  je promijeniti pri prvoj prijavi. Ako u imeniku nema računa `admin`, program
  ga doda sam. Prije nego što čvor izložite mreži, pročitajte
  [SECURITY.md](../../SECURITY.md).
- `imenik.json` nosi osobne podatke. Ne stavljajte ga u repozitorij ni u
  dijeljene mape.
- **Šifra vode** je službeni naziv malim slovima, bez dijakritika, s crticama:
  `rijeka Primjerica` → `rijeka-primjerica`. Na tu šifru pozivaju se
  poddionice (`watercourse_code`).
- **Vodomjer** se zapisuje kao u dokumentaciji dionica:
  `"Primjerovo, rkm 12+300 (81,25)"`. Iz toga nastaje postaja „Primjerovo” sa
  stacionažom i kotom nule 81,25. Isti naziv na više dionica daje jednu
  postaju. Pragovi su centimetri na letvi: `"+300"`.
- **Objekt na poddionici** veže se na registar objekata po nazivu. Ne
  upisujte mu `watercourse_code`, jer ga program tada smatra vodom, a ne
  objektom.
- **Nasip na poddionici** kojeg nema u registru program upisuje sam, kao novi
  objekt vrste `NASIP`.
- Dionice se upisuju prije županija. Zato ugrožena naselja idu u
  `section_territories.json`. Isti podaci u `parts[].territories` ruše prvo
  punjenje greškom stranog ključa.

## Očitanja

### ocitanja-letve.csv: očitanja jedne letve

Na stranici letve, u povijesti očitanja, sadržaj se zalijepi ili se odabere
datoteka (tekst, CSV ili Excel `.xlsx`; stari `.xls` ne). Treba pravo upisa
očitanja na toj letvi.

- Svaki redak je vrijeme i vodostaj u centimetrima, npr. `2026-03-01 07:00;245`.
- Prima i ispis s letva.voda.hr: `07.09.2026. 00 h    -118`,
  `08.09.2026. 00:00  -123` ili samo datum za dnevnu vrijednost.
- **Vrijeme je lokalno, po zagrebačkom vremenu**, s ljetnim i zimskim
  računanjem.
- Zaglavlja i prazni redci se preskaču. Redak koji počinje datumom, a ne da
  se pročitati, pokazuje se kao greška.
- Prije upisa program pokaže koji su redci novi, koji isti, a koji se
  razlikuju od zatečenih. Ništa se ne upisuje bez potvrde.

### niz-za-arhivu.csv: niz za hidrološku arhivu

**Administracija › Unos u arhivu.** Program sam pogodi razdjelnik (`;`, tab,
`,` ili `|`), stupac vremena i stupac vrijednosti, a čovjek to prije upisa
potvrdi ili ispravi.

- Vrijeme: `2026-03-01 07:00`, `2026-03-01T07:00:00`, `01.03.2026. 07:00`,
  samo datum ili Excelov serijski broj.
- **Vremensku zonu** bira čovjek pri potvrdi. Zapisi sa samim datumom ne
  ovise o zoni.
- Vrijednost prima decimalni zarez ili točku. Vrijednost izvan razumnih
  granica veličine preskače se i broji (vodostaj od −1500 do 2000 cm).
- Redci koji počinju s `#` su komentari. Isto vrijeme dvaput se upisuje samo
  jednom.
- Najviše 64 MiB.
- Način **Dopuni** čuva zatečeni niz. Način **Zamijeni** baca ga i ostavlja
  samo datoteku.

### tablica-dnevnih-vodostaja.csv: dnevna tablica centra

Naredba u terminalu, nad bazom čvora:

```sh
gocop -db /var/lib/gocop/gocop.db -tablica tablica-dnevnih-vodostaja.csv \
      -tablica-izvor "COP Primjerovo — dnevna tablica"
```

- Prvi stupac je datum (`01.03.2026.`, `2026-03-01` ili Excelov broj dana).
  Ostali stupci su postaje i objekti, s nazivom kao u registru.
- Vrijednost je jutarnje očitanje u centimetrima. Sat je zadano 7, a mijenja
  se s `-tablica-sat`. Decimale se zaokružuju na centimetar.
- Prazna ćelija, `-`, `x`, `nema`, `led` i `suho` znače da očitanja nema.
  To nije greška.
- Kodiranje (UTF-8 ili Windows-1250) i razdjelnik prepoznaju se sami.
- **Bez `-upisi` ništa se ne upisuje**, nego se ispisuje izvješće:
  - koji je stupac vezan na koju letvu;
  - koje stupce program ne prepoznaje ili su dvoznačni;
  - gdje se tablica razlikuje od zatečenog očitanja istog dana.
  Zatečeno očitanje se nikad ne prepisuje.
- Ručna veza stupca na letvu: `-tablica-veze "stupac=šifra"`. Stupci koje
  treba preskočiti (npr. protoci): `-tablica-preskoci`.

## Održavanje: ugovor-a02.xlsx

Radna knjiga ugovora o održavanju (program A.02), u obliku koji uvoz čita.
Uvozi se na stranici **Održavanje**, za odabrano branjeno područje, ili
naredbom `gocop -ugovor datoteka.xlsx` (bez `-upisi` samo izvješće). Uvozi se
popis lokacija s razvrstavanjem i stavke radova bez cijena.

**Predložak je izmišljen. Nije ugovor ni troškovnik Hrvatskih voda.** Vode,
područje, pozicije, iznosi te oznake i opisi stavki (`PR-1` do `PR-3`) ne
potječu iz stvarnog ugovora. Od oblika radne knjige zadržano je samo ono što
uvoz traži doslovno:

- imena listova;
- oznake redaka u stupcu A;
- oblik pozicije `A.02.01.NN.`;
- zaglavlja reda vode;
- redni broj vrste.

Test provjerava da stavke ostanu označene kao primjer.

| List | Što uvoz čita |
|---|---|
| `PPI_POSTAVKE` | B1: broj branjenog područja; B2: naziv. U predlošku je u B3 napomena da je izmišljen |
| `TROŠKOVNIK` | stupac A je oznaka retka: `#P` pozicija (I: pozicija plana `A.02.01.NN.…`, K: redni broj), `#V` voda, `#O` vrsta objekta, `#L` lokacija, `#Z` županija, `#N` vrijednost (sve u stupcu I), `#S` stavka (H: opis, I: oznaka, J: jedinica), `#E` kraj bloka |
| `LOKACIJE_BP_NN` | A: zaglavlje reda vode, počinje s `VODE ` i sadrži `I. REDA` ili `II. REDA` (uz `MEĐUDRŽAVNE` ili `OSTALE` za skupinu); B: vrsta, a odlučuje redni broj na početku (`1.` vodotoci, `2.` akumulacije, retencije i jezera, `3.` bujice, `4.` melioracijska odvodnja); C: redni broj `1.1.`; D: naziv vode ili nasipa |
| `PREVENTIVNA` | ponudbeni troškovnik: A redni broj, B oznaka, C opis, D jedinica |

- Broj područja iz radne knjige mora odgovarati području za koje se uvozi.
- Naziv lokacije traži se među vodama i nasipima registra. Izvješće prije
  upisa kaže što je prepoznato, što bi bilo novo, a gdje treba ručna veza.
- Prima se samo `.xlsx`.

## Geometrija: tok-vodotoka.geojson

Na stranici vodotoka, gumbom **Učitaj GeoJSON**.

- Crta toka je `LineString` ili `MultiLineString`. Prima se zbirka značajki,
  jedna značajka ili gola geometrija.
- **Koordinate su WGS84 (EPSG:4326), dužina pa širina:** `[16.0, 45.5]`.
  Datoteka u HTRS96/TM ili sa zamijenjenim redoslijedom odbija se s
  objašnjenjem. Točke moraju biti između 5° i 30° istočne dužine i između 40°
  i 52° sjeverne širine.
- Svojstvo `stacionaza_napomena` prve crte karta pokazuje ispod naziva.

## Uvozi bez predloška

Za ove uvoze predložak nema smisla. Datoteku daje izvorni sustav ili sam
goCOP:

- **Ispravak očitanja letve** i **ispravak arhive**: izvezite CSV sa stranice
  letve, ispravite ga i vratite. Redak se prepoznaje po identifikatoru ili
  trenutku iz izvoza.
- **Izvozi drugih sustava** na stranici Unos u arhivu: HIS-2000 (nizovi,
  krivulje protoka, poprečni profili), ARSO, eHYD, GKD, PEGELONLINE, SEBA,
  godišnjaci DHMZ-a i HydroView.
- **Paketi `.cop`** hidrološke arhive, koje potpisuje izdavač.
- **Ranije evidencije BP16** iz Directusa (`gocop -import-bp16…`).
- Slike žiga i potpisa, skenovi akata i arhiva baze (vraćanje kopije).
