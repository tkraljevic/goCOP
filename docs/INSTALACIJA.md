# Postavljanje i održavanje goCOP čvora

Administratorske upute, usklađene s 0.0.25-alfa (2. 10. 2026.).
Kratki pregled projekta: [README](../README.md). Korisnički postupci su u Pomoći aplikacije.

Operativni program za obranu od poplava Hrvatskih voda: povezuje organizaciju,
teren, vodostaje, dokumentaciju obrane, službene akte, ljude i sredstva. Radi
i bez interneta; kopije na različitim računalima međusobno se usklađuju.
Repozitorij nosi program i praznu shemu baze, a podatke unosi ili uvozi
organizacija koja ga koristi.

> **Status: alfa, izdanje 0.0.25-alfa (2. 10. 2026.), za testiranje i daljnji
> razvoj.** Nije za operativnu upotrebu. Sve se još mijenja. Što je u kojem
> izdanju, piše u [popisu izmjena](../CHANGELOG.md).
>
> Otvoreni kod, neprofitno. Za program je odgovoran Tomislav Kraljević.

Ovaj dokument je pisan za **administratore** koji program postavljaju na
prva računala. Kaže što program radi na računalu i mreži, što ne radi, i
što su poznate slabosti u ovoj fazi.

## Što već radi

- **Operativa obrane:** teren i očitanja, pragovi i akti o stupnjevima obrane,
  dnevnik COP-a, dežurstva i obračun IORS, vodočuvarska knjiga, prijave s
  terena, dnevna izvješća dionica i sektora te dnevnici usluga A.02 i A.03.
- **Registri i resursi:** ustroj organizacije, dionice i poddionice, vodomjerne
  postaje, vodotoci, objekti, teritorijalne jedinice, djelatnici i zaduženja,
  izvođači, održavanje, međuslivovi i meteorološke točke te materijalno-tehnička
  sredstva sa skladištima, prometom, inventurom i potrebama za nabavom.
- **Hidrološka prognoza:** satne i dnevne procjene s rasponom neizvjesnosti,
  uzdužnim profilom vodnog vala, povijesnom provjerom i izvozom u Excel.
- **Službeni dokumenti:** PDF i Excel obrasci, ovjera akata u goCOP-u, osobni
  PAdES potpisi, vlastoručni potpis i žig, sken potpisanog akta sa žigom kao
  izvornik, slanje izvornika e-poštom te Exchange sandučić i adresar tvrtke.
- **Podaci i razmjena:** knjiga verzija, selektivna sinkronizacija između
  čvorova iste mreže, izravno ili kroz web tunel, uloge čvora (preuzima
  vodostaje, izdaje prognozu), prognoza i kiša razmjenom, spremište velikih
  sadržaja po SHA-256 otisku te odvojena hidrološka arhiva s potpisanim `.cop`
  paketima koji se dohvaćaju prema pretplati.
- **Rad bez interneta:** osnovno sučelje, podaci, unos i dokumenti rade lokalno.
  Mrežne karte, vrijeme, javni vodostaji, Exchange i sinkronizacija dostupni su
  samo kada postoji odgovarajuća mrežna veza.

Detaljni postupci i ovlasti opisani su u ugrađenoj stranici **Pomoć**.

---

## 1. Što instalacija znači na računalu

- **Jedna izvršna datoteka, bez instalatera.** Nema pokretačkih
  programa, nema Windows servisa, nema unosa u registry, nema drugih
  ovisnosti. Kopira se u mapu i pokrene.
- **Ne traži administratorska prava** (iznimka: port 80 na Linuxu i
  macOS-u; na Windowsu ne). Ako port 80 nije dostupan, program sam prelazi
  na 8080 i to ispiše.
- **Podatke čuva na podešenim putanjama**, zadano u `data/` u mapi iz koje je
  program pokrenut (radnoj mapi), ne nužno uz izvršnu datoteku:

  | datoteka | što je |
  |---|---|
  | `gocop.db` (+ `-wal`, `-shm`) | SQLite baza — operativa, registri, korisnici i knjiga verzija |
  | `sadrzaj.db` (+ `-wal`, `-shm`) | PDF-ovi i drugi veliki službeni sadržaji, spremljeni jednom po SHA-256 otisku |
  | `vodostaji.db` (+ `-wal`, `-shm`) | obnovljiva hidrološka arhiva i evidencija primljenih izdanja |
  | `prognoze.db` (+ `-wal`, `-shm`) | lokalna i primljena izdanja prognoza, parametri i rezultati provjere |
  | `oborine.db` (+ `-wal`, `-shm`) | mjerenja i prognozirane oborine |
  | `gocop.toml` | postavke, s komentarima; program je zapiše pri prvom pokretanju |
  | `node-key` | privatni ključ ovog računala (Ed25519), prava 0600 |
  | `network-key` | ključ mreže, kod čvora koji ju je osnovao |

  U `data/` je i `skenovi/` sa skenovima prijava s terena. Uz `data/`, u istoj
  radnoj mapi, zadano žive `vodostaji/`, stablo izvornih datoteka, i `pakete/`,
  mapa izdanih `.cop` paketa s `katalog.json`. Putanje se mijenjaju u
  `gocop.toml` (`db`, `arhiva`, `podaci`, `skenovi`, `pakete`) ili zastavicama.

  U istu mapu administrator može staviti datoteke registara i imenika
  (`*.json`); program ih pročita samo pri prvom punjenju prazne baze
  (poglavlje „Podaci koji nisu u repozitoriju”).

- **Uklanjanje:** nakon sigurnosne kopije ukloniti program i njegove podatkovne
  mape; provjeriti i zasebno podešene putanje, spremnike i tunel.
- **Platforme:** izdanja na GitHubu zasad nemaju gotovih izvršnih datoteka.
  Program se prevodi iz označenog izdanja (Go, bez CGO-a) za Windows, Linux ili
  macOS, a za Linux amd64 uz svako izdanje izlazi Docker slika (poglavlje 7).

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

Razmjena izvana može ići kroz `https://<domena>/razmjena/tunel` (WebSocket).
Unutar nje ostaje TLS s ključevima uparenih čvorova. U Domenske čvorove upisuje
se `https://<domena>`; u lokalnoj mreži prvo se pokušava izravna veza.
Tunel nije automatsko otkrivanje čvorova niti zamjena za uparivanje.

**Što ide izvan računala:** program nema telemetriju o uporabi ni obvezni cloud;
sučelje, fontovi i skripte ugrađeni su u program. Poslovni podaci sinkroniziraju se samo
s goCOP čvorovima iste mreže (članstvo potpisano ključem mreže pri uparivanju),
obostrano autentificiranim TLS-om 1.3.
Bez ikakve postavke program dohvaća javno vrijeme i upozorenja DHMZ-a za ploču
na naslovnoj i vrijeme s Open-Meteo za novi list dnevnika, a pločice karte
dolaze s Wikimedije (`plocice` u `gocop.toml`; prazno isključuje kartu). Tek
kad ih administrator uključi, postoje veze prema poslužitelju e-pošte
(Administracija → E-pošta ili `[posta]` u `gocop.toml`) te, na čvoru s ulogom
preuzimanja vodostaja ili izdavanja prognoze, satno preuzimanje s javnih i
prijavljenih izvora vodostaja (HydroView i mLetva), izvora kiše i vanjskih
prognoza. I bez uloge obrazac letve dohvaća javni popis postaja s
vodostaji.voda.hr, a ručno preuzimanje na stranici letve čita njezin izvor.
Lozinke vanjskih izvora ostaju šifrirane na tom čvoru i ne sinkroniziraju se.
Bez tih veza osnovni rad ostaje dostupan.

**Za vatrozid:** pristup ograničiti na računala koja sudjeluju u testu.
Port 4711 otvarati samo tijekom uparivanja, a 4712 zadržati u lokalnoj mreži.
Uz web tunel nije potrebno javno otvarati 4710 ni izvorni HTTP port.
Portovi se mijenjaju u `gocop.toml`.

## 3. Podaci i sigurnost

- **Podaci su na računalu.** Operativa i kazalo službenih zapisa su u
  `gocop.db`, veliki PDF-ovi i drugi sadržaji u `sadrzaj.db`, a hidrološka
  povijest u `vodostaji.db` i izvornom stablu. Datoteka `prognoze.db` ne prenosi
  se cijela: izdanja prognoza, promjene modela i pripadajuća kiša putuju knjigom
  verzija. Izdanja su ondje dostupna sedam dana, a u lokalnoj bazi prognoza ostaju.
  Kazalo arhive također putuje razmjenom, a `.cop` paketi dohvaćaju se prema
  pretplati. Čvor koji izdaje prognozu i drži izvorno stablo nakon noćnog
  ulaganja kiše sam izdaje promijenjene pakete.
- **Osobni podaci.** Registar djelatnika sadrži imena, funkcije, telefone i
  e-mail adrese djelatnika i sudionika obrane od poplava, kako ih
  organizacija unese ili uveze iz svog imenika. Tretirati mapu `data/` kao
  takvu.
- **Lozinke** se čuvaju kao bcrypt hash. Sesija je HttpOnly kolačić,
  SameSite Lax, a iza HTTPS-a (i tunela) i `Secure`; traje do isteka ili
  odjave. Promjena ili poništenje lozinke gasi ostale prijave te osobe na
  istom čvoru; prijave na drugim čvorovima traju do isteka (najviše 24 h).
- **Zadana lozinka mora se promijeniti.** Do tada račun može otvoriti samo
  vlastiti profil i odjavu. Zadana lozinka je javna, pa izvana (kroz tunel)
  ne vrijedi: prva prijava njome ide iz lokalne mreže, a izvana s privremenom
  lozinkom koju izda administrator (Korisnici → Poništi lozinku).
- **Pogađanje lozinki je ograničeno.** Pet neuspjeha za isto ime s iste adrese
  u 15 minuta, ili dvadeset s iste adrese, blokira na 15 minuta; trideset za
  isto ime izvana u satu blokira to ime izvana na 30 minuta (prijava iz lokalne
  mreže tada i dalje radi). IPv6 adrese broje se po mreži /64. Poruka prijave
  ne otkriva postoji li račun ni je li deaktiviran.
- **Pouzdani posrednici.** Adresa klijenta iz zaglavlja (`CF-Connecting-IP`)
  vrijedi samo kad zahtjev stiže od pouzdanog posrednika; zadano su to ovo
  računalo i privatne mreže, a u `gocop.toml` se suze:

  ```toml
  [web]
  pouzdani_posrednici = ["172.17.0.1"]   # odakle cloudflared dolazi u Docker
  zaglavlje_klijenta = ""                 # prazno = CF-Connecting-IP; iza nginxa X-Forwarded-For
  ```

  Zahtjev sa zaglavljem posrednika uvijek se smatra vanjskim (tunel), pa za
  njega ne vrijede iznimke lokalne mreže. Posrednik koji šalje zaglavlje, a
  nije na popisu, zapisuje se u dnevnik jednom na sat.
- **Zaštita od tuđih stranica i zaglavlja.** Izmjene (POST) s tuđe web-stranice
  odbijaju se (Go `CrossOriginProtection`); odgovori nose `nosniff`,
  `Referrer-Policy: same-origin`, zabranu ugradnje u okvir i osnovni CSP, a
  iza HTTPS-a i HSTS. Privici i slike iz e-pošte i s terena poslužuju se u
  pješčaniku; u pregledniku se otvaraju samo PNG, JPEG, GIF i WebP.
- **Ograničenja.** Obična stranica ima minutu za zahtjev i pet minuta za
  odgovor, uvozi i izvozi 30 minuta; tijelo zahtjeva je zadano do 2 MB, a
  rute s datotekama imaju svoje granice. Poruka razmjene najviše 256 MiB,
  tunel razmjene najviše 32 veze i 2 po klijentu, rukovanje kroz tunel 5 s.
- **Uparivanje računala** traži čovjeka na oba ekrana: oba pokažu isti
  šesteroznamenkasti kod i oba ga potvrde. Bez toga drugo računalo ne dobiva
  ni bajt. Razmjenu dobiva samo član mreže: računalo upareno s onim koje drži
  ključ mreže dobije potpisano članstvo, koje vrijedi godinu dana i obnavlja se
  ponovnim uparivanjem s tim računalom. Svaka kasnija veza dokazuje ključ
  unutar TLS-a; ključ bez važećeg članstva u mreži odbija se na vratima, i kad
  je računalo upareno.
- **Ključ računala** (`node-key`) je njegov identitet. Kopija baze bez
  ključa nije to računalo. Ključ se ne sinkronizira. Čuvati ga u zaštićenoj
  sigurnosnoj kopiji za oporavak istog čvora; ne koristiti ga za osnivanje drugog.
- **Tajne i službeni izvornici.** Lozinka Exchangea i sken vlastoručnog potpisa
  šifrirani su ključem ovog čvora. Osobni potpisni ključ zaključan je lozinkom
  korisnika; potpisani PDF, a ne nezaštićeni sken, postaje izvornik koji se
  razmjenjuje. Žig centra dostupan je samo upravi sektora, ali je dio baze i
  sigurnosne kopije pa mapu `data/` treba štititi kao službenu evidenciju.
- **Nacrt nije službeni zapis.** Može se mijenjati ili obrisati dok ne bude
  objavljen ili ovjeren. Objava ili ovjera zaključava sadržaj i priloge te ih
  uvodi u repozitorij službenih zapisa. Ispravak nastaje kao novi povezani
  zapis; izvorni se ne prepisuje. Arhivska građa trajno je čuvani dio tog
  repozitorija, dok drugi službeni zapisi mogu imati propisani rok čuvanja.
- **Pospremanje nije obično brisanje.** Starije tehničke verzije i već uložena
  operativna očitanja mogu se ukloniti samo administratorskim postupkom koji
  najprije provjerava da je točan niz, vrijeme i vrijednost sigurno spremljen.
- **Testne mogućnosti** za upis i simulirani potpis „tuđim očima” služe samo
  uvođenju i testiranju. Simulirani PDF ima veliki žig „BEZVRIJEDNO”; sve
  testne prekidače u operativnom radu treba držati isključenima.

## 4. Poznata ograničenja alfa faze — pročitati prije odobrenja

1. **Izvršna datoteka nije potpisana.** Izdanja zasad nemaju gotovih izvršnih
   datoteka; program se prevodi iz označenog izdanja. Windows SmartScreen i neki
   antivirusi mogu upozoriti na nepotpisan Go program; dopustiti ga ručno samo
   kad je preveden iz provjerenog izvora. Potpisivanje besplatnim certifikatom
   za otvoreni kod je u planu prije bete.
2. **Sigurnosno učvršćivanje je u tijeku.** Od 0.0.25-alfa postoje zaštita od
   tuđih stranica, `Secure` kolačić iza HTTPS-a, izričito pouzdani posrednici,
   ograničenja HTTP-a i razmjene, a uparivanje i primanje u mrežu smije samo
   globalni administrator. Otvoreno: dvofaktorska prijava izvana (PIN na
   službenu e-poštu), potpisane uloge izdavanja (svaki član mreže zasad smije
   objaviti prognozu i arhivu) i opoziv izgubljenog računala uživo. Čvor
   dostupan kroz tunel treba držati na zadnjem izdanju.
3. **Automatsko pronalaženje preko interneta nije uvedeno.** Razmjena preko
   ručno zadane domene i WebSocket tunela radi; lokalno pronalaženje ostaje na LAN-u.
4. **Shema se još mijenja.** Sve što se unese u alfi može se izgubiti pri
   promjeni sheme između verzija. Čvorove držati na istom izdanju i
   nadograđivati ih redom iz [poglavlja 7](#7-stalni-čvor-spremnik-i-sigurnosna-kopija): 0.0.23-alfa
   i starija izdanja pri izmjeni zapisa brišu polja koja je dodao noviji
   program. Od 0.0.24-alfa ta se polja čuvaju, a pločica razmjene pokazuje
   na kojem izdanju radi koji čvor; vidi [popis izmjena](../CHANGELOG.md).

## 5. Preporuka za prva računala

- Dva do tri računala u istoj lokalnoj mreži, unutar mreže Hrvatskih voda,
  bez izlaganja na internet.
- Jedan administrator osniva mrežu (Administracija → Čvor, mreža i
  sinkronizacija) i upravlja lozinkama. Ključ mreže ostaje na njegovu
  računalu, a svako računalo upareno s njim postaje član mreže. Uparivanje
  pokreće i potvrđuje globalni administrator (od 0.0.25-alfa); na svježem
  računalu, dok na njemu nema računa, čarobnjak stoji na stranici prijave, ali
  samo za pristup iz lokalne mreže, nikad kroz tunel.
- Program prevesti iz označenog izdanja (npr. `git checkout v0.0.25-alfa`) ili
  koristiti sliku s oznakom izdanja; `SHA256SUMS` uz izdanja zasad ne postoji.
- Program pokretati kao običan korisnik, iz vlastite mape.
- Sigurnosna kopija mora obuhvatiti cijelu mapu `data/` i izvorno stablo
  vodostaja. Povrat treba probno izvesti prije operativnog rada; ključeve
  čvora čuvati odvojeno i ne pretvarati kopiju baze u drugi čvor kopiranjem
  tuđeg identiteta.

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
ili područje i godine te bira prima li samo kazalo, pregled ili puni sadržaj
i koliko dugo primljene PDF-ove i slike drži. Što joj više ne treba može
obrisati s računala; sadržaj nastao na tom računalu ne otpušta se. Uredski
poslužitelj prati sve (`sve = true` u `gocop.toml`) i drži potpunu kopiju iz
koje se svaki laptop može ponovno napuniti.

Novo računalo prvo treba povezati s uredom. Dok u njemu nema djelatnika,
stranica prijave nudi čarobnjak **Poveži ovo računalo s uredom**: pronaći
ured u lokalnoj mreži ili upisati adresu, usporediti kod s osobom u uredu,
preuzeti podatke. Kad stigne imenik, osoba se prijavljuje svojim računom;
čarobnjak bez prijave tada se zatvara, a prijavljenima ostaje u profilu za
dodatna računala i ručnu razmjenu.

Sve što radi samo administrator stoji u modulu **Administracija**: ustroj i
nazivi, računi, moduli i ovlasti, čvorovi i sinkronizacija, održavanje baze,
arhiva, obračun sati, e-pošta, žig, elektronički potpisi, testne opcije i
uvozi. Vidi ga zadano samo globalni administrator. Nadzorna ploča
**Sinkronizacija** pokazuje tko je na mreži,
koliko računala odgovara, s kim je zadnja razmjena uspjela, tko zaostaje i
što ne štima; razmjena ide s više čvorova istodobno, a čvorovi koji redom
šute zovu se sve rjeđe. **Održavanje baze** pokazuje koliko je baza velika
i od čega, sažima knjigu verzija (svaki zapis zadržava zadnju verziju, a
obrisani svoj nadgrobni spomenik; starije verzije brišu se nakon zadanog
roka; od 0.0.24-alfa čuva i zadnju verziju
koju je svaki čvor upisao u kanal), vraća prostor na disku, te izdaje kanal
kao potpisan `.cop` paket (npr. `gocop-ocitanja-bp16-2024_v1.cop`, po izboru s PDF-ovima i slikama;
stari oblik `.db` i dalje se može izvesti) i ugrađuje ga u bilo koji čvor:
arhiva na disku ili prijenos bez mreže. Na stranici prijave stoji kontakt
glavnog administratora iz registra (mobitel, e-pošta) i centar iz
`gocop.toml` (odjeljak `[kontakt]`).

`gocop.toml` — adresa web sučelja; putanje baze, arhive vodostaja, izvornog
stabla, skenova i paketa; identifikator i naziv računala; portovi i razmak
automatske sinkronizacije; prati li čvor sve (`sve`) i koliko mjeseci
očitanja drži iz razmjene (`povijest_mjeseci`); javna adresa za QR kod;
centar na stranici prijave (`[kontakt]`); izvor pločica karte (`[karta]`);
zadani poslužitelj e-pošte (`[posta]`; postavke spremljene u Administraciji →
E-pošta imaju prednost). Ključ `bootstrap` program zasad ne čita; domenski
čvorovi upisuju se u Administraciji → Čvor, mreža i sinkronizacija →
Domenski čvorovi.
Zastavice na naredbenom retku (`-addr`, `-db`, `-arhiva`, `-podaci`,
`-skenovi`, `-pakete`, `-node`, `-name`,
`-sync-port`, `-pair-port`, `-discovery-port`, `-auto-sync`, `-config`)
imaju prednost pred datotekom.

Uvoz evidencija radova iz vanjske evidencije kao **rekonstruiranih**
dnevnika: `gocop -import-bp16-dnevnici` (bez `-upisi` samo izvješće). Po
programu i godini nastaje jedan dnevnik, listovi se slažu po danu i po šest
izvođačevih upisa, a prvi upis na svakom listu i oznaka dnevnika kažu da je
to rekonstrukcija: stvarni listovi vođeni su izvan aplikacije i ovjereni
potpisima, pa ih ovi ne zamjenjuju.

Pristup vanjskoj evidenciji (`DIRECTUS_URL`, `DIRECTUS_TOKEN`) čita se iz
`~/.config/gocop/directus.env` ili iz datoteke zadane s `-directus-env`;
`-bp16-dir` uvozi iz ranije skinutih JSON datoteka. Isto vrijedi za
`-import-bp16` (očitanja), `-import-bp16-obilasci` i `-import-bp16-prijave`,
koji bez `-upisi` također samo izvještavaju.

Uvoz ugovora o održavanju (radna knjiga iz Excel dodatka Hrvatskih voda,
program A.02): `gocop -ugovor <datoteka.xlsx>` ispiše izvješće — koje su
lokacije prepoznate u registru, koje bi bile nove, gdje treba ručna veza
(`-ugovor-veze "naziv iz popisa=sifra"`). Upis tek uz `-upisi`;
`-ugovor-sve-stavke` uz korištene stavke upiše i cijeli ponudbeni
troškovnik (opisi i jedinice, bez cijena). Ponovni uvoz istog ili
sljedećeg ugovora ne udvostručuje: postojeće lokacije i stavke ostaju kako
jesu.

## 7. Stalni čvor, spremnik i sigurnosna kopija

Docker slika je `ghcr.io/tkraljevic/gocop`, trenutačno za Linux amd64.
Za ponovljivo postavljanje birati oznaku izdanja, npr. `:0.0.25-alfa`,
umjesto promjenjive `:latest`. Spremnik sluša web na 8080, razmjenu na 4710,
uparivanje na 4711 i pronalaženje na 4712/UDP, a radi kao UID/GID `99:100`;
mape moraju biti dostupne tom korisniku.

- `/data`: baza operative, sadržaji, prognoze, oborine, postavke i ključevi.
- `/arhiva`: `vodostaji.db`, izvorno stablo `vodostaji/`, `skenovi/` i `pakete/`.

Obje mape moraju biti trajno montirane izvan spremnika. SQLite držati na
lokalnom disku; na Unraidu koristiti izravnu putanju diska/poola, ne `/mnt/user/`
ni mrežni disk. Ne brisati volumene pri zamjeni slike.

U **Administracija → Čvor, mreža i sinkronizacija → Uloge ovog čvora**
odabrati preuzima li čvor vodostaje i izdaje li prognozu. Uobičajeno to radi
stalni čvor, a laptop prima podatke. Novom čvoru te se uloge moraju izričito
uključiti. Pristupne račune izvora upisati samo na čvorovima koji preuzimaju;
lozinke se ne razmjenjuju.

Prva automatska razmjena kreće oko 30 sekundi nakon pokretanja. Pločica na
naslovnoj prikazuje zadnju razmjenu, napredak arhive i čvor koji izdaje prognozu;
„javlja se sam” znači da se čvor uspješno povezuje izvana, iako ga drugi ne
može izravno nazvati. Provjeriti svježinu očitanja i izdanja, ne samo mrežnu vezu.

Čvor koji preuzima štiti arhivu koju je sam izgradio. Čvor koji samo prima
može je zamijeniti paketom kada njegovo razdoblje doseže barem isti završni
dan i broj zapisa nije manji. To nije dokaz jednakosti svake pojedine vrijednosti.

Prije nadogradnje napraviti konzistentnu kopiju svih podešenih podatkovnih
mapa i ključeva: za datotečnu kopiju zaustaviti čvor, ne kopirati samo otvoreni
SQLite `.db` bez njegova WAL-a. Probno provjeriti obnovu izolirano, bez
istodobnog uključivanja dvaju čvorova s istim ključem.

Nadograđuje se redom: najprije stalni čvor, odmah zatim ostala računala. Dok
sva ne rade na istom izdanju, ne dodavati, mijenjati ni brisati djelatnike i
zaduženja. 0.0.23-alfa i starija izdanja pri pokretanju prekodiraju korisnike
i zaduženja, a novija ih ne diraju, pa bi isti zapis na dva čvora dobio
različit identifikator. Na kojem izdanju radi koji čvor, pokazuju pločica
„Razmjena s čvorovima” i stranica Sinkronizacija (od 0.0.24-alfa; čvor koji
izdanje ne javlja radi na starijem).

Na javnom posredniku isključiti cache za aplikacijske odgovore i nakon
promjene očistiti stare kopije. Program zadano šalje `private, no-store`
izvan `/static/`, ali pojedini prikazi imaju svoje cache postavke. Nakon
postavljanja provjeriti da odjavljeni korisnik ne može preuzeti zaštićeni
izvoz. TLS tunel sam ne rješava ovlasti, CSRF ni sigurnost sesija.

## 8. Verzije

| faza | verzija | git oznaka | značenje |
|---|---|---|---|
| **alfa** | `0.0.x` | `v0.0.1-alfa`, `v0.0.2-alfa`… | razvoj, sve se mijenja; x raste sa svakim izdanjem |
| **beta** | `0.y.x`, od `0.1.0` | `v0.1.0-beta`… | funkcionalnosti zaokružene, oblik stabilan, provjera na terenu; y nova funkcionalnost, x ispravci |
| **stabilno** | `z.y.x`, od `1.0.0` | `v1.0.0`… | operativna upotreba; z samo za nekompatibilnu promjenu (shema baze, razmjena između čvorova, postavke), y nova funkcionalnost, x ispravci |

Čvorovi različitih verzija međusobno se sinkroniziraju, pa je nekompatibilna
promjena ona zbog koje stari čvor ne može raditi s novim. Alfa i beta izdanja
na GitHubu označena su kao *pre-release*. Izdanje: promijeniti `verzijaPrograma`,
upisati novo u [CHANGELOG.md](../CHANGELOG.md), commit, oznaka, Release s tekstom
iz popisa izmjena.

Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.
Verzija stoji u kodu (`verzijaPrograma` u `cmd/gocop/main.go`) i mijenja se pri
izdavanju; program je ispisuje u podnožju stranice i u dnevniku, s kratkom
oznakom commita iz kojega je preveden (i zvjezdicom kad stablo ima nespremljenih
izmjena). Izdanje u gitu nosi oznaku oblika `v0.0.25-alfa`; iz svake takve
oznake GitHub gradi Docker sliku `ghcr.io/tkraljevic/gocop:0.0.25-alfa` i `:latest`.

## 9. Za razvoj

Go 1.27.1 ili noviji (prema `go.mod`), bez CGO-a; SQLite (modernc), sučelje
`html/template` ugrađeno u binary.
Rezultate provjera navoditi uz konkretno izdanje; prolaz starog izdanja nije
potvrda za novu verziju.

```bash
go build -o bin/gocop ./cmd/gocop
go test ./...
```

Repozitorij nosi samo aplikaciju: `cmd/gocop` je jedini ulaz, logika je u
`internal`, sučelje u `web`. Uvoz u arhivu iz svih podržanih izvora, arhiviranje,
izdavanje paketa i priprema modela prognoze rade iz same aplikacije. Pomoćni
alati (administracija poslužitelja, jednokratne migracije, dijagnostika,
analize, priprema geometrije) stoje lokalno u `tools/` i ne ulaze u
repozitorij; popis i namjena su u [katalogu alata](katalog-alata.md).
Lokalne izgradnje idu u `bin/`, a baze, arhiva vodostaja i paketi u `data/`,
`vodostaji/` i `pakete/`, također izvan repozitorija.

Sinkronizacijski transport — ključevi, uparivanje, TLS razmjena i
pronalaženje na lokalnoj mreži — stoji u `internal/razmjena`, odvojen od
ostatka programa da se mreža može mijenjati bez diranja operative.

Zapisi koji se razmjenjuju moraju podnijeti čvorove na različitim izdanjima.
Pri njihovoj izmjeni vrijedi:

- nova polja dodaju se na vrh zapisa, ne unutar ugniježđenih struktura;
- shema entiteta podiže se samo kad se mijenja značenje polja, ne za novo polje;
- popravci podataka čitaju zadnju verziju iz knjige i pišu samo stvarnu promjenu;
- novi entitet upisuje se među poznate (`repository.PoznatiEntiteti`).

Razlozi su u komentarima `internal/ledger/shema.go` i
`internal/repository/fixups.go` te u
[planu povezivosti](plan-povezivost.md#različite-verzije-programa-na-čvorovima).

Kote nule vodomjera vode se u sustavu Trst, a HVRS71 i baltičke kote zasebno
(vidi [baltičke kote](balticke-kote.md)).
Testovi koji trebaju stvarne registre i imenik preskaču se kad tih datoteka
nema u mapi `data/`.

Program razvija Tomislav Kraljević, uz pomoć kolega iz Hrvatskih voda.
Kao i uređivač koda i drugi razvojni alati, u radu se koriste i alati
umjetne inteligencije; sav kod prolazi ručni pregled i automatske testove
prije nego što uđe u program, a odgovornost za njega je isključivo ljudska.

## 10. Suradnici i doprinosi

Program nastaje uz pomoć kolega iz Hrvatskih voda i sudionika obrane od
poplava. Tko je što pridonio — uključujući unos podataka za Baranju u
aplikaciju app.bp16.xyz — piše u datoteci [`ZAHVALE.md`](../ZAHVALE.md).

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
  provedbenog plana obrane od poplava nastaje administratorskim alatom `prijepis-dionica`, koji je izvan repozitorija),
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
je u datoteci `LICENSE`, a hrvatska inačica u `LICENSE_hr.txt`; sve jezične
inačice EUPL-a jednako su vjerodostojne. Iznimka je sloj razmjene
`internal/razmjena`, pod licencom MIT (© Tomislav Kraljević i Mario Kraljević).

Nositelj autorskih prava na program: Hrvatske vode.
Program je osmislio i izgradio Tomislav Kraljević; to navođenje je uvjet
korištenja i ostaje u svakoj izvedenici.

Grafički znakovi i geometrija ugrađeni u program, ovisnosti, vanjske mrežne
usluge i podaci koje program čita ili uvozi uz bazu imaju vlastito podrijetlo
i prava, opisana u datoteci `NOTICE`.
