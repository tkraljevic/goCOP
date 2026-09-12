# goCOP — Centar obrane od poplava

Operativni program za obranu od poplava Hrvatskih voda: registri štićenih
dionica, vodomjernih postaja, vodnih tijela i objekata, djelatnici i
zaduženja, teritorijalne jedinice, očitanja vodostaja i građevinski
dnevnici. Radi i bez interneta; kopije na različitim računalima se same
usklađuju. Repozitorij nosi program i praznu shemu baze; podatke unosi ili
uvozi organizacija koja program koristi.

> **Status: alfa (0.0.x), za testiranje i daljnji razvoj.** Nije za
> operativnu upotrebu. Sve se još mijenja.
>
> Otvoreni kod, neprofitno. Za program je odgovoran Tomislav Kraljević.

Ovaj dokument je pisan za **administratore** koji program postavljaju na
prva računala. Kaže što program radi na računalu i mreži, što ne radi, i
što su poznate slabosti u ovoj fazi.

## Što već radi

- **Registri:** štićene dionice, vodomjerne postaje s pragovima obrane,
  vodna tijela, objekti (crpne stanice, ustave, sifoni, nasipi, brane),
  teritorijalne jedinice te djelatnici i njihova zaduženja. Registri imaju
  pretraživanje, detalje, unos i uređivanje, uz ovlasti prema području
  odgovornosti. Program dolazi prazan.
- **Administrativna organizacija:** sektori (vodnogospodarski odjeli s centrom
  obrane) i branjena područja (mali slivovi s ispostavom) upisuju se prvi,
  jer se na njih vežu ovlasti, dionice i zaduženja. Pregled i izlistanje su
  u Registrima za svakoga; upisuje ih globalni administrator, program ih ne
  zna sam. Nazivi razina su postavka mreže: druga organizacija istu podjelu
  može zvati regija i slivno područje, a šifre i ovlasti rade isto.
- **Dionica i poddionice:** dionica se sastoji od poddionica, a poddionica
  je jedna voda iz registra s jednim obuhvatom: stacionaža (rkm, pkm, bkm,
  kkm, po nasipu nkm) od–do, obala, obuhvat riječima, duljina. Na poddionici
  se biraju ugroženo područje (grad, općina ili naselje), mjerodavni
  vodomjeri, objekti (crpne stanice, ustave i sifoni vezani na registar
  objekata; mostovi i propusti kao slobodni redak) te nasipi i brane iz
  registra objekata s odsjecima. Brane nose naziv retencije ili akumulacije
  koju zatvaraju. Opis dionice slaže se iz poddionica, uz mogućnost ručnog
  opisa. Kazala veza (postaja–dionica, objekt–dionica, teritorij–dionica)
  izvode se iz poddionica i ne razmjenjuju se zasebno.
- **Održavanje:** popis lokacija izvršenja usluga po branjenom području
  (što se održava iz programa A.02 i pod kojom kategorijom: red vode,
  skupina, vrsta) i stavke radova bez cijena, koje operateri dopunjuju.
  Popis se uvozi iz ugovora o održavanju (na stranici Održavanje ili
  zastavicom `-ugovor`) ili dopunjuje ručno; pozicije plana i cjenici se ne
  uvoze jer se mijenjaju sa svakim okvirnim sporazumom.
- **Dnevnik COP-a:** dežurni zapisnik centra obrane od poplava, jedan po
  centru (sektoru) i po obrani — traje koliko i dežurstva. Otvara ga
  voditelj ili zamjenik centra; upisuju svi koji rade u sektoru, iz centra
  i s branjenih područja, u isti dnevnik. Zapis nosi vrijeme događaja i
  vrijeme upisa, tko je javio i tko je upisao, vrstu (dojava, obavijest,
  napomena, dežurstvo) i branjeno područje na koje se odnosi, pa se dnevnik
  čita i po području. Zapisi se ne brišu nego storniraju uz razlog. Stari,
  digitalizirani dnevnici uvoze se kao prijepis: ondje se krivo pročitano
  ispravlja na mjestu, jer je dokument uvez na papiru; u živom dnevniku
  ispravak je novi zapis.
- **Plan dežurstava i obračun sati:** uz dnevnik COP-a vodi se plan — tko
  dežura kad, gdje (ured ili teren), za koje branjeno područje ili za cijeli
  sektor, s opisom rada iz obrasca IORS. Svatko upisuje sebe, od vodočuvara
  do glavnog rukovoditelja; uprava centra upisuje bilo koga i potvrđuje tuđe
  upise. Isti zapisi poslije obrane daju obračun sati: po zidnom satu, po
  vrsti dana i pojasu (radni dan, subota, nedjelja i blagdan; redovno,
  dnevni, noćni), pomnoženo koeficijentima za ured i teren, zaokruženo na
  pola sata u korist djelatnika. Obračun se slaže po branjenom području s
  rekapitulacijom; klik na ime otvara izvješće o radnim satima te osobe, a
  na profilu svatko vidi svoje planove i izvješća. Blagdani (kao pravila, s
  razdobljem važenja) i koeficijenti su podatak organizacije i uređuju se u
  Administraciji, ne novom verzijom programa.
- **Dnevnici usluga A.02 i A.03:** dnevnik održavanja voda I. i II. reda i
  kanala III. i IV. reda, po branjenom području. Naslovnica (izvođač,
  voditelj usluga, ovlaštenik ili nadzorni inženjer, akti), za svaki dan
  list s uvjetima (vremenske prilike s Open-Meteo dok ima interneta,
  vodostaji iz očitanja, ocjena uvjeta, osoblje i strojevi) i upisi: rad
  izvođača, napomene, nalozi s rokom i ocjene nadzora. Upisi nose redni broj
  bez rupa, razlikuju vrijeme događaja od vremena unosa te osobu koja je
  javila od osobe koja je upisala. Ne brišu se nego storniraju; list potvrđuju
  izvođač i nadzor, a ispisuje se u obliku obrasca. Izvođač (voditelj usluga /
  poslovođa) vidi samo dnevnike.
- **Rad bez interneta:** cijeli program, sučelje i podaci rade lokalno;
  sučelje je prilagođeno i radu na mobitelu.
- **Usklađivanje računala:** čvorovi se pronalaze u lokalnoj mreži, ručno
  uparuju i razmjenjuju podatke šifriranom i obostrano autentificiranom vezom.
- **Povijest:** svaka izmjena ostaje zabilježena kao nova verzija, a brisanje
  je arhiviranje. Administratori mogu provjeriti ovlasti pogledom kroz račun
  drugog djelatnika, bez mogućnosti izmjene u tom načinu rada.
- **Hidrološka arhiva:** veliki nizovi vodostaja odvojeni su od operativne baze.
  Administrator kroz sučelje uvozi CSV/XLSX, uspoređuje ga sa zatečenim nizom,
  zadano ga nadopunjuje, gradi spojeni historijat te ulaže završena operativna
  očitanja. Arhiva prepoznaje niz bez izvorne datoteke i ne briše ga sama.
- **`.cop` izdanja:** historijat pojedine postaje izdaje se kao prijenosni paket.
  Paket je ograničen pri raspakiravanju, ima otiske dijelova i Ed25519 potpis,
  ugrađuje se atomski, a starije izdanje ne može automatski pregaziti novije.
  Arhivske mutacije dostupne su samo globalnom administratoru i izvode se jedna
  po jedna.

---

## 1. Što instalacija znači na računalu

- **Jedan izvršni file (~18 MB), bez instalatera.** Nema pokretačkih
  programa, nema Windows servisa, nema unosa u registry, nema drugih
  ovisnosti. Kopira se u mapu i pokrene.
- **Ne traži administratorska prava** (iznimka: port 80 na Linuxu i
  macOS-u; na Windowsu ne). Ako port 80 nije dostupan, program sam prelazi
  na 8080 i to ispiše.
- **Piše samo u vlastitu mapu `data/`** pored sebe:

  | datoteka | što je |
  |---|---|
  | `gocop.db` (+ `-wal`, `-shm`) | SQLite baza — operativa, registri, korisnici i knjiga verzija |
  | `vodostaji.db` (+ `-wal`, `-shm`) | obnovljiva hidrološka arhiva i evidencija primljenih izdanja |
  | `gocop.toml` | postavke, s komentarima; program je zapiše pri prvom pokretanju |
  | `node-key` | privatni ključ ovog računala (Ed25519), prava 0600 |
  | `network-key` | ključ mreže, kod čvora koji ju je osnovao |

  Uz `data/` zadano žive `vodostaji/`, stablo izvornih datoteka, i `pakete/`,
  mapa izdanih `.cop` paketa s `katalog.json`. Putanje se mogu promijeniti.

  U istu mapu administrator može staviti datoteke registara i imenika
  (`*.json`); program ih pročita samo pri prvom punjenju prazne baze
  (poglavlje „Podaci koji nisu u repozitoriju”).

- **Uklanjanje:** obrisati mapu. Ne ostaje ništa.
- **Platforme izdanja:** Windows x64, Linux x64, macOS Apple Silicon.

## 2. Mreža

Program je web aplikacija koja poslužuje sama sebe — otvara se u
pregledniku na tom računalu ili s drugih računala u mreži. Javni čvor
izlaže se kroz tunel koji prema korisniku završava TLS vezu; sam program
iza tunela i dalje sluša HTTP.

| port | protokol | smjer | čemu služi |
|---|---|---|---|
| **80** (ili 8080) | TCP, HTTP | dolazno | web sučelje za ljude |
| **4710** | TCP, TLS 1.3 | dolazno i odlazno | razmjena podataka između uparenih računala |
| **4711** | TCP, TLS 1.3 | dolazno | uparivanje — samo dok uparivanje traje |
| **4712** | UDP, broadcast | lokalna mreža | pronalaženje drugih goCOP računala u istom segmentu |

**Što ide izvan računala:** isključivo prema drugim goCOP računalima koja je
administrator izričito uparen. Nema telemetrije, nema provjere verzije, nema
cloud servisa, nema vanjskih fontova ni skripti — sučelje je ugrađeno u
program. Sav promet između računala je obostrano autentificiran TLS 1.3.

**Za vatrozid:** dopustiti dolazne TCP 80/8080, 4710 i 4711 te UDP 4712
između računala koja sudjeluju u testu. Portovi se mijenjaju u `gocop.toml`.

## 3. Podaci i sigurnost

- **Podaci su na računalu.** Operativa je u `gocop.db`, a velika hidrološka
  povijest u `vodostaji.db` i izvornom stablu. Ne šalju se nikamo osim na
  uparena računala ili u `.cop` paket koji administrator izričito izda.
- **Osobni podaci.** Registar djelatnika sadrži imena, funkcije, telefone i
  e-mail adrese djelatnika i sudionika obrane od poplava, kako ih
  organizacija unese ili uveze iz svog imenika. Tretirati mapu `data/` kao
  takvu.
- **Lozinke** se čuvaju kao bcrypt hash. Sesija je HttpOnly kolačić,
  SameSite Lax, traje do isteka ili odjave.
- **Zadana lozinka mora se promijeniti.** Do tada račun može otvoriti samo
  vlastiti profil i odjavu; sve ostale stranice i radnje ostaju zaključane.
- **Pogađanje lozinki je ograničeno.** Nakon pet neuspjelih pokušaja u 15
  minuta prijava se blokira na 15 minuta, i po korisničkom imenu i po adresi.
  Adrese iz zaglavlja prihvaćaju se samo od lokalnog posrednika ili tunela.
- **Uparivanje računala** traži čovjeka na oba ekrana: oba pokažu isti
  šesteroznamenkasti kod i oba ga potvrde. Bez toga drugo računalo ne dobiva
  ni bajt. Svaka kasnija veza dokazuje ključ unutar TLS-a; ključ koji ne
  odgovara uparenom se odbija na vratima.
- **Ključ računala** (`node-key`) je njegov identitet. Kopija baze bez
  ključa nije to računalo. Ključ se ne sinkronizira i ne smije u backup koji
  ide na drugo računalo.
- **Poslovni zapisi ne nestaju običnim brisanjem.** Izmjena je nova verzija,
  a brisanje arhiviranje. Starije tehničke verzije i već uložena operativna
  očitanja mogu se pospremiti samo administratorskim postupkom koji najprije
  provjerava da je točan niz, vrijeme i vrijednost u arhivi.

## 4. Poznata ograničenja alfa faze — pročitati prije odobrenja

1. **Izvršni file još nije potpisan.** Windows SmartScreen će upozoriti, a
   neki antivirusi označe nepotpisane Go programe. Potpisivanje besplatnim
   certifikatom za otvoreni kod je u planu prije bete. Do tada: provjeriti
   SHA-256 izdanja i dopustiti ručno.
2. **Nema CSRF tokena** za obrasce i druge zahtjeve koji mijenjaju podatke.
3. **Pronalaženje preko interneta** (stalno izložena računala s domenom)
   još ne radi — samo lokalna mreža i ručni upis adrese.
4. **Shema se još mijenja.** Sve što se unese u alfi može se izgubiti pri
   promjeni sheme između verzija.

## 5. Preporuka za prva računala

- Dva do tri računala u istoj lokalnoj mreži, unutar mreže Hrvatskih voda,
  bez izlaganja na internet.
- Jedan administrator osniva mrežu i drži njezin ključ (Administracija →
  Čvor i mreža); on prima nova računala i upravlja lozinkama. Svoje
  računalo uparuje svatko sam: na svježem računalu čarobnjak stoji na
  stranici prijave, a prijavljenima u profilu.
- Provjeriti SHA-256 preuzetog izdanja prema `SHA256SUMS` uz izdanje.
- Program pokretati kao običan korisnik, iz vlastite mape.

## 6. Pokretanje

```
gocop.exe            (Windows)
./gocop              (Linux, macOS)
```

Prvo pokretanje stvori praznu bazu i račun `admin` s početnom lozinkom
koja se mijenja pri prvoj prijavi, te zapiše `data/gocop.toml`. Prvi korak
u programu je registar Administrativna organizacija: sektori, pa branjena područja. Ako uz bazu stoje datoteke registara i imenika,
učita i njih. Otvoriti `http://localhost` (ili `http://localhost:8080`).
Ustroj, registri i djelatnici stižu na svako računalo. Očitanja i dnevnici
idu po kanalima „vrsta/područje/godina“ i računalo ih prima samo za ono
što prati: na profilu, pod **Što ovo računalo prati**, osoba označi sektor
ili područje i godine, a što joj više ne treba obriše s računala. Uredski
poslužitelj prati sve (`sve = true` u `gocop.toml`) i tako je arhiva iz
koje se svaki laptop može ponovno napuniti.

Novo računalo prvo treba povezati s uredom. Dok u njemu nema djelatnika,
stranica prijave nudi čarobnjak **Poveži ovo računalo s uredom**: pronaći
ured u lokalnoj mreži ili upisati adresu, usporediti kod s osobom u uredu,
preuzeti podatke. Kad stigne imenik, osoba se prijavljuje svojim računom;
čarobnjak bez prijave tada se zatvara, a prijavljenima ostaje u profilu za
dodatna računala i ručnu razmjenu.

Sve što radi samo administrator stoji u modulu **Administracija**: ustroj
obrane (sektori i branjena područja), moduli i ovlasti, čvor, mreža i
sinkronizacija, postavke obračuna sati (blagdani i koeficijenti) te pregled
uvoza podataka. Vidi ga zadano samo globalni
administrator. Nadzorna ploča **Sinkronizacija** pokazuje tko je na mreži,
koliko računala odgovara, s kim je zadnja razmjena uspjela, tko zaostaje i
što ne štima; razmjena ide s više čvorova istodobno, a čvorovi koji redom
šute zovu se sve rjeđe. **Održavanje baze** pokazuje koliko je baza velika
i od čega, sažima knjigu verzija (svaki zapis zadržava zadnju verziju, a
obrisani svoj nadgrobni spomenik; starije verzije brišu se nakon zadanog
roka), vraća prostor na disku, te izvozi kanal u datoteku
(`gocop-ocitanja-bp16-2024.db`) i uvozi je u bilo koji čvor: arhiva na
disku ili prijenos bez mreže. Na stranici prijave stoji kontakt glavnog
administratora iz registra (mobitel, e-pošta) i centar iz postavki čvora.

`gocop.toml` — adresa web sučelja, putanja baze, identifikator i naziv
računala, portovi, razmak automatske sinkronizacije, stalno izložene
domene. Zastavice na naredbenom retku (`-addr`, `-db`, `-podaci`, `-pakete`,
`-node`, `-name`,
`-sync-port`, `-pair-port`, `-discovery-port`, `-auto-sync`, `-config`)
imaju prednost pred datotekom.

Uvoz evidencija radova iz vanjske evidencije kao **rekonstruiranih**
dnevnika: `gocop -import-bp16-dnevnici` (bez `-upisi` samo izvješće). Po
programu i godini nastaje jedan dnevnik, listovi se slažu po danu i po šest
izvođačevih upisa, a prvi upis na svakom listu i oznaka dnevnika kažu da je
to rekonstrukcija: stvarni listovi vođeni su izvan aplikacije i ovjereni
potpisima, pa ih ovi ne zamjenjuju.

Uvoz ugovora o održavanju (radna knjiga iz Excel dodatka Hrvatskih voda,
program A.02): `gocop -ugovor <datoteka.xlsx>` ispiše izvješće — koje su
lokacije prepoznate u registru, koje bi bile nove, gdje treba ručna veza
(`-ugovor-veze "naziv iz popisa=sifra"`). Upis tek uz `-upisi`;
`-ugovor-sve-stavke` uz korištene stavke upiše i cijeli ponudbeni
troškovnik (opisi i jedinice, bez cijena). Ponovni uvoz istog ili
sljedećeg ugovora ne udvostručuje: postojeće lokacije i stavke ostaju kako
jesu.

## 7. Verzije

| oznaka | značenje |
|---|---|
| `0.0.x` | **alfa** — razvoj, sve se mijenja |
| `0.x.0` | **beta** — oblik je stabilan, provjerava se na terenu |
| `x.0.0` | **stabilno** — u operativnoj upotrebi |

## 8. Za razvoj

Go, bez CGO-a; SQLite (modernc), sučelje `html/template` ugrađeno u binary.
Projekt trenutačno ima više od 500 Go testova; puni testovi, `go vet` i ciljani
race-testovi arhive i weba prolaze na aktualnom stanju.

```bash
go build -ldflags "-X main.version=0.0.1" -o gocop ./cmd/gocop
go test ./...
```

Sinkronizacijski transport (ključevi, uparivanje, TLS razmjena, LAN
pronalaženje) je zasebna biblioteka
[syncnet](https://github.com/tkraljevic/syncnet), zajednička s projektom
goEMM. Kote nule vodomjera vode se u sustavu Trst, a HVRS71 kote zasebno.
Testovi koji trebaju stvarne registre i imenik preskaču se kad tih datoteka
nema u mapi `data/`.

Program razvija Tomislav Kraljević, uz pomoć kolega iz Hrvatskih voda.
Kao i uređivač koda i drugi razvojni alati, u radu se koriste i alati
umjetne inteligencije; sav kod prolazi ručni pregled i automatske testove
prije nego što uđe u program, a odgovornost za njega je isključivo ljudska.

## 9. Suradnici i doprinosi

Program nastaje uz pomoć kolega iz Hrvatskih voda i sudionika obrane od
poplava. Tko je što pridonio — uključujući unos podataka za Baranju u
aplikaciju app.bp16.xyz — piše u datoteci [`ZAHVALE.md`](ZAHVALE.md).

### Evidencija VGI Baranja (app.bp16.xyz)

Uvoz očitanja vodostaja te stanja crpnih stanica i ustava branjenog
područja 16 (Baranja) od 2013. do 2026. nastao je iz evidencije koju je
Tomislav Kraljević vodio na privatnom poslužitelju (app.bp16.xyz) i koju je
VGI Baranja punila svako jutro. Ti podaci nisu dio programa; uvoze se na
čvorove Hrvatskih voda. Zahvala svim djelatnicima koji su ih trinaest godina
unosili u evidenciju i očitavali na terenu. Njihova imena nisu objavljena u
javnom repozitoriju radi zaštite osobnih podataka.

## Podaci koji nisu u repozitoriju

Repozitorij nosi program i shemu baze, bez podataka, i tako ostaje: baza
napunjena podacima Hrvatskih voda nikad ne ide u repozitorij, ni kad su ti
podaci javno objavljeni. Sve stoji uz bazu, u mapi `data/`, i čita se
samo pri prvom punjenju prvog čvora u mreži; svaki sljedeći čvor podatke
dobiva sinkronizacijom. Zaseban, izmišljen testni
skup podataka može jednom stajati uz izdanje za isprobavanje.

- **organizacija** — `organizacija.json` (sektori i branjena područja),
  ako se ne upisuju ručno;
- **registri** — `sections.json` (dionice s poddionicama, vodomjerima i
  pragovima, objektima, nasipima i branama; prijepis Privitka 1 Glavnog
  provedbenog plana obrane od poplava nastaje alatom `cmd/prijepis-dionica`),
  `watercourses.json` (vode I. reda iz Odluke o popisu voda I. reda, NN
  79/2010, i opisni podaci iz Wikipedije), `territories.json` i
  `section_territories.json` (županije, gradovi, općine, naselja i njihove
  veze na dionice), `objekti_bp16.json` (objekti Baranje iz evidencije VGI);
- **imenik djelatnika** — osobni podaci; čita se iz `data/imenik.json` uz
  bazu, samo pri prvom punjenju čvora;
- **očitanja vodostaja** — mjerenja Hrvatskih voda, koja na letvama
  očitavaju vodočuvari i strojari. Povijest vodostaja stoji samo na
  čvorovima Hrvatskih voda; program je zna uvesti iz datoteke uz bazu i
  razmijeniti s drugim čvorovima mreže, ali je ne nosi u sebi. Isto vrijedi
  za mjerenja Državnog hidrometeorološkog zavoda, ako se poslije uključe:
  Hrvatske vode ih koriste po ugovoru o uzajamnom korištenju i ne
  objavljuju ih.

Zbog toga svaki uvoz ide iz datoteke koja stoji uz bazu, nikad iz
`internal/db`, jer se sve odande ugrađuje u program. Test to i provjerava:
u `internal/db` ne smije biti nijedna podatkovna datoteka.

## Licenca

goCOP je otvoreni, neprofitni projekt namijenjen Hrvatskim vodama i drugim
vodoprivrednim organizacijama kojima je primjenjiv. Program je licenciran
pod European Union Public Licence, verzija 1.2 (EUPL-1.2). Tekst licence
je u datoteci `LICENSE`; sve jezične inačice EUPL-a, uključujući hrvatsku,
jednako su vjerodostojne.

Nositelj autorskih prava na program: Hrvatske vode.
Program je osmislio i izgradio Tomislav Kraljević; to navođenje je uvjet
korištenja i ostaje u svakoj izvedenici.

Grafički znakovi ugrađeni u program i podaci koje program čita uz bazu
imaju vlastito podrijetlo i prava, opisana u datoteci `NOTICE`.
