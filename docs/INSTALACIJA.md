# Postavljanje i održavanje goCOP čvora

Administratorske upute, usklađene s 0.0.34-alfa (4. 10. 2026.).
Kratki pregled projekta: [README](../README.md). Korisnički postupci su u Pomoći aplikacije.

Operativni program za obranu od poplava Hrvatskih voda: povezuje organizaciju,
teren, vodostaje, dokumentaciju obrane, službene akte, ljude i sredstva. Radi
i bez interneta; kopije na različitim računalima međusobno se usklađuju.
Repozitorij nosi program i praznu shemu baze, a podatke unosi ili uvozi
organizacija koja ga koristi.

> **Status: alfa, izdanje 0.0.34-alfa (4. 10. 2026.), za testiranje i daljnji
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

- **Jedna izvršna datoteka.** Nema Windows servisa ni drugih ovisnosti.
  Ručno se kopira u mapu i pokrene, bez ikakvog unosa u sustav. Na Windowsu
  je uobičajen put instalacijski program **goCOP Postava** (poglavlje 6):
  instalira za trenutnog korisnika, bez administratora, a jedini unosi u
  sustav su pokretanje pri prijavi i mapa programa u korisničkom PATH-u
  (oba u `HKCU`) te unos za deinstalaciju.
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
- **Platforme:** od 0.0.28-alfa uz svako izdanje na GitHubu stoje programi
  za Windows (amd64), Linux (amd64) i macOS (arm64, amd64), datoteka
  `SHA256SUMS` i njezin potpis `SHA256SUMS.sig` (ključ izdanja, poglavlje 8).
  Program se može i prevesti iz označenog izdanja (Go, bez CGO-a), a za Linux
  amd64 uz svako izdanje izlazi Docker slika (poglavlje 7).

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
dolaze s OpenStreetMapa (`plocice` u `gocop.toml`; prazno isključuje kartu). Tek
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
  Na istom čvoru briše i zapamćena računala, prijave koje čekaju PIN i
  privremene kodove te osobe.
- **Zadana lozinka mora se promijeniti.** Do tada račun može otvoriti samo
  vlastiti profil i odjavu. Zadana lozinka je javna, pa izvana (kroz tunel)
  ne vrijedi: prva prijava njome ide iz lokalne mreže, a izvana s privremenom
  lozinkom koju izda administrator (Korisnici → Poništi lozinku).
- **Pogađanje lozinki je ograničeno.** Pet neuspjeha za isto ime s iste adrese
  u 15 minuta, ili dvadeset s iste adrese, blokira na 15 minuta; trideset za
  isto ime izvana u satu blokira to ime izvana na 30 minuta (prijava iz lokalne
  mreže tada i dalje radi). IPv6 adrese broje se po mreži /64. Poruka prijave
  ne otkriva postoji li račun ni je li deaktiviran.
- **PIN za prijavu izvana (od 0.0.26-alfa).** Prijava izvana, kroz posrednika
  ili s adrese koja nije privatna, loopback ni link-local, nakon lozinke traži
  šesteroznamenkasti PIN poslan na službenu e-poštu korisnika. Prijava iz
  lokalne mreže PIN nikad ne traži. PIN ide na adresu u dopuštenoj domeni
  (zadano `voda.hr`) ili na adresu izvan nje koju je globalni administrator
  potvrdio na tuđem računu, svojim očima (Korisnici → Uredi → *Adresa je
  provjerena*, npr. za djelatnike tvrtke izvođača). Potvrda putuje razmjenom
  s imenom i danom, vrijedi samo dok je adresa ista, a vlastita promjena adrese je briše; na
  adresu koju ima još jedan aktivni račun PIN ne ide ni potvrđenu. Čvor s
  0.0.26-alfa potvrdu čuva, ali na takvu adresu PIN ne šalje. PIN vrijedi 10 minuta i jednom; pet krivih
  upisa poništi prijavu na čekanju, a deset u satu zaključa upis kodova tom
  računu na sat. Čvor šalje najviše tri pisma s PIN-om osobi u 15 minuta i
  60 na sat ukupno. Umjesto PIN-a vrijede rezervni kodovi s profila (deset
  jednokratnih) i privremeni kod administratora (24 h, jednom). Preglednik se
  može zapamtiti na 30 dana; zapamćenje prestaje promjenom lozinke, i na drugom
  čvoru. Prijave na čekanju, zapamćena računala i kodovi postoje samo na čvoru
  na kojem su nastali: ne ulaze u knjigu verzija i ne sinkroniziraju se, a
  tokeni i kodovi čuvaju se samo kao HMAC ključem izvedenim iz ključa čvora.
  Prekidač (Administracija → E-pošta) zadano je isključen i uključuje se tek
  nakon uspješnog probnog PIN-a na tom čvoru, sa stranice otvorene izvana
  preko HTTPS-a. Račun pošiljatelja upisuje se na
  čvoru iza tunela, šifriran ključem čvora; PIN ne ostaje u Poslanim stavkama
  i ne piše se u dnevnik. Kad Exchange odbije lozinku pošiljatelja, slanje
  staje dok je administrator ne upiše ponovno, da se vlasnikov račun u domeni
  ne zaključa; prijave računa u domeni broje se po osobi i računu, najviše
  tri u pola sata, a svaka osoba ima najviše tri neprihvaćena upisa lozinke u
  15 minuta po obrascu (upis koji poslužitelj primi ne broji se). Veza prema Exchangeu dijeli se samo za isto ime i istu lozinku.
  PIN radi samo preko HTTPS-a: posrednik mora javiti shemu (cloudflared sam
  šalje `X-Forwarded-Proto` i `Cf-Visitor`, a iza nginxa treba
  `proxy_set_header X-Forwarded-Proto $scheme;`). Bez toga se prijava izvana s
  PIN-om odbija kao da je preko nešifriranog http-a. Uzbunu da PIN nema čime
  slati diže samo čvor koji je u zadnjih sedam dana primio prijavu izvana; to
  se pamti samo dok program radi, pa nakon ponovnog pokretanja uzbuna čeka
  prvu prijavu izvana. Prije uključivanja provjeriti da čvor dohvaća `owa.voda.hr`:
  `curl -X POST https://owa.voda.hr/EWS/Exchange.asmx` mora vratiti 401 (ili
  *Ispitaj* na stranici E-pošta). Svaki čvor dostupan kroz tunel mora imati
  0.0.26-alfa ili novije izdanje, jer starije PIN ne traži.
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
  ni bajt. Kod se dogovara s obvezom unaprijed (strana koja zove obveže se
  na nasumičan broj prije nego što vidi broj druge strane), pa ga napadač u
  sredini ne može namjestiti traženjem ključa; sa starijim programom (prije
  0.0.33-alfa) se ne uparuje. Razmjenu dobiva samo član mreže: računalo upareno s onim koje drži
  ključ mreže ili ovlast za primanje dobije potpisano članstvo, koje vrijedi
  godinu dana i obnavlja se ponovnim primanjem. Svaka kasnija veza dokazuje
  ključ unutar TLS-a; ključ bez važećeg članstva u mreži odbija se na vratima,
  i kad je računalo upareno.
- **Članstvo pri spajanju (od 0.0.33-alfa).** Čvor u certifikatu razmjene
  pokaže svoju potvrdu članstva i, ako je ima, ovlast za primanje. Druga
  strana prihvati samo lanac koji vodi do ključa mreže (ključ mreže → ovlast
  primatelja → članstvo), neopozvan i s imenom koje u mreži nema drugo
  računalo. Tako se primljeno računalo sinkronizira sa svim članovima bez
  uparivanja sa svakim. Opoziv članstva ili ovlasti putuje knjigom; opozvana
  potvrda ne vrijedi ni kad je čvor pokaže sam, a oduzeta ovlast poništava
  članstva svih računala koja je primatelj primio. Opoziv vrijedi samo
  potpisan: opoziv ovlasti ključem mreže, opoziv članstva ključem mreže ili
  primatelja koji je to članstvo izdao. Opoziv se ne poništava. Članstvo
  koje primatelj izda ne traje dulje od njegove ovlasti, što provjerava svaki
  član, pa ni ukraden ključ primatelja nakon isteka ovlasti ne izdaje
  valjana članstva.
- **Ključ računala** (`node-key`) je njegov identitet. Kopija baze bez
  ključa nije to računalo. Ključ se ne sinkronizira. Čuvati ga u zaštićenoj
  sigurnosnoj kopiji za oporavak istog čvora; ne koristiti ga za osnivanje drugog.
- **Tajne i službeni izvornici.** Lozinka Exchangea i sken vlastoručnog potpisa
  šifrirani su ključem ovog čvora. Osobni potpisni ključ zaključan je lozinkom
  korisnika; kad lozinku postavi netko drugi (poništenje, lozinka upisana u
  obrascu djelatnika ili oporavak s konzole), ključ se uklanja, jer ga nova
  lozinka ne otvara, a osoba na profilu napravi novi. Već potpisani dokumenti
  ostaju provjerljivi. Ključ se pravi i uklanja samo uz lozinku računa (s
  istim ograničenjem krivih upisa) i nikad tuđim očima. Certifikat uz puno
  ime nosi korisničko ime, „Ime Prezime (korisnicko)”, jer puno ime osoba
  mijenja sama; zagrade iz punog imena se izostavljaju, pa zagrada na kraju
  uvijek nosi korisničko ime. Stariji certifikati nose samo ime i dalje se
  provjeravaju. Potpisani PDF, a ne nezaštićeni sken, postaje izvornik koji
  se razmjenjuje. Žig centra dostupan je samo upravi sektora, ali je dio baze
  i sigurnosne kopije pa mapu `data/` treba štititi kao službenu evidenciju.
- **Lozinka sandučića e-pošte** koju osoba spremi na profilu ostaje samo na
  tom čvoru, šifrirana ključem čvora, a uz nju stoji otisak lozinke računa
  (HMAC ključem čvora). Vrijedi samo dok je lozinka računa ista: poništenje,
  lozinka upisana u obrascu djelatnika i oporavak s konzole brišu je na tom
  čvoru odmah. Na drugim čvorovima prestaje vrijediti čim razmjenom stigne
  nova lozinka računa, a briše se pri prvoj sljedećoj uporabi (profil,
  sandučić, slanje akta) ili, najkasnije, pri sljedećem pokretanju čvora. I
  nakon vlastite promjene lozinke upisuje se ponovno. Tko zna privremenu
  lozinku, tako ne čita i tuđu poštu. Lozinke sandučića spremljene u
  starijem izdanju dobiju otisak pri prvom pokretanju ovim izdanjem, ali samo
  kad knjiga verzija pokazuje da se lozinka računa otada nije mijenjala;
  ostale se brišu i upisuju ponovno.
- **Ovlasti po dosegu.** Pravo pisanja ide po dosegu dužnosti: dužnost
  sektora piše u cijelom sektoru, dužnost područja u svom području, a dužnost
  na dionicama na tim dionicama, njihovim objektima i aktima te u dnevnicima
  svog područja i COP-a (terenska dužnost bez dionica u cijelom području;
  samo uz sektor terenska se dužnost ne upisuje). Akt vodomjera piše tko
  piše na bilo kojoj njegovoj dionici. Objekt drugog područja koji stoji na
  dionici vodi njegovo područje, i vezu dionice na objekt ili vodomjer
  drugog područja upisuje samo tko ondje piše. Sektor upisan uz dužnost
  područja ili dionice ne daje pisanje po sektoru; popisi i zadani izbor
  sektora i dalje se ravnaju po svim dužnostima osobe. Račun bez aktivne
  dužnosti, isključen račun i globalnog administratora zadužuje samo
  globalni administrator; primarnu funkciju i vlastiti naziv dužnosti daje
  i mijenja samo onaj tko smije uređivati cijeli račun. Lozinku i
  privremeni kod uprava daje samo osobama niže razine; osobama s dužnošću na
  njezinoj razini uprave ili višoj (koje i dalje uređuje i zadužuje) daje ih
  viša razina ili globalni administrator, jer poništenje uklanja potpisni
  ključ. Razina uprave je razina s koje uloga upravlja računima: zamjenik
  glavnog rukovoditelja za sektor upravlja sektorom, a zamjenik rukovoditelja
  sektora za branjeno područje područjem. Privremena uprava (dužnost s rokom)
  dužnost koja daje upravu na njezinoj razini dodjeljuje najdulje do isteka
  vlastite uprave nad tim sektorom ili područjem; tuđu postojeću dužnost
  izmjenom ne skraćuje. Zastavicu globalnog administratora postavlja samo
  stalna uprava organizacije. Lozinku koju
  administrator upiše u obrascu djelatnika osoba pri prvoj prijavi mora
  zamijeniti, kao i poništenu. Adresa e-pošte koju već ima drugi aktivni
  račun ne upisuje se nikome, ni globalnom administratoru (velika slova i
  razmaci se ne razlikuju), a isključen račun s takvom adresom kroz program
  se ne uključuje (`-aktiviraj` s konzole, alat za oporavak, uključi ga i
  upozori; adresu tada uskladite ručno); prazna adresa je dopuštena.
  Korisničko ime koje već ima drugi račun, i drugim slovima, ne dobiva se ni
  preimenovanjem.
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

1. **Programi još nemaju Authenticode potpis.** Izdanja nose potpis ključem
   izdanja (`SHA256SUMS.sig`), koji Postava provjerava prije svake instalacije
   i nadogradnje, ali Windows taj potpis ne poznaje: SmartScreen i neki
   antivirusi mogu upozoriti na nepotpisan program. Dopustiti ga samo kad je
   preuzet s GitHub stranice izdanja. Potpis besplatnim certifikatom za
   otvoreni kod (SignPath Foundation) je u pripremi; tada će kao izdavač
   pisati „SignPath Foundation”.
2. **Sigurnosno učvršćivanje je u tijeku.** Od 0.0.25-alfa postoje zaštita od
   tuđih stranica, `Secure` kolačić iza HTTPS-a, podesivi pouzdani posrednici,
   ograničenja HTTP-a i razmjene, a uparivanje i primanje u mrežu smije samo
   globalni administrator. Od 0.0.26-alfa prijava izvana traži PIN poslan na
   službenu e-poštu (prekidač zadano isključen, vidi
   [poglavlje 3](#3-podaci-i-sigurnost)). Otvoreno: potpisane uloge izdavanja
   (svaki član mreže zasad smije objaviti prognozu i arhivu) i opoziv
   izgubljenog računala uživo. Rezervni i privremeni kodovi vrijede samo na
   čvoru na kojem su nastali, što je dovoljno dok je javni čvor jedan. Čvor
   dostupan kroz tunel treba držati na zadnjem izdanju. Zadani popis
   posrednika još uključuje privatne mreže: prije javnog postavljanja suziti
   ga na stvarne adrese posrednika i provjeriti pristup bez njih.
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
- Program prevesti iz označenog izdanja (npr. `git checkout v0.0.34-alfa`) ili
  koristiti sliku s oznakom izdanja. Uz izdanje na GitHubu stoje `SHA256SUMS`
  i potpis `SHA256SUMS.sig`; kako se provjeravaju, piše u
  [uputama za Linux](linux.md#2-preuzimanje-i-provjera-izdanja).
- Program pokretati kao običan korisnik, iz vlastite mape.
- Sigurnosna kopija mora obuhvatiti cijelu mapu `data/` i izvorno stablo
  vodostaja. Povrat treba probno izvesti prije operativnog rada; ključeve
  čvora čuvati odvojeno i ne pretvarati kopiju baze u drugi čvor kopiranjem
  tuđeg identiteta.

## 6. Pokretanje

### Windows: instalacijski program goCOP Postava

Postava je mali program koji instalira goCOP, pali ga i gasi, drži ikonu u
traci i nadograđuje ga. Mijenja se rijetko i ima svoja izdanja (oznake
`postava-v…`); goCOP sam uvijek preuzme najnoviji.

1. S GitHub stranice izdanja preuzeti `goCOP-postava-<izdanje>.exe` i
   pokrenuti ga. Administratorska prava nisu potrebna.
2. Čarobnjak: licenca, mapa (zadano `%LOCALAPPDATA%\goCOP`), **ime ovog
   računala u mreži** (predloženo iz korisnika i računala, npr.
   `pperic-thinkcentre-5`; nakon instalacije se ne mijenja), **važan izbor
   mreže** (nova mreža samo za prvo računalo, uz dodatnu potvrdu; inače
   postojeća) i kvačica *Pokreni goCOP pri prijavi u Windows*. Pri ponovnoj
   instalaciji preko postojećih podataka ime i mreža se ne pitaju.
3. Postava s GitHuba preuzme najnovije izdanje goCOP-a i provjeri mu potpis
   ključem izdanja i SHA-256. Bez interneta: uz instalacijski program staviti
   `gocop-windows-amd64.exe`, `SHA256SUMS` i `SHA256SUMS.sig` iz izdanja (npr.
   s USB-a); Postava ih uzme odande, uz istu provjeru.
4. Na kraju se pokreće Postava: ikona valova u traci uz sat, a ona pali čvor
   i, kad čvor prvi put odgovori, u pregledniku otvori *Postavljanje* (nova
   mreža ili povezivanje s postojećom).
   Pri prvom pokretanju čvora Windows vatrozid pita smije li program na mrežu:
   dopustiti samo **privatne** mreže, a kućnu ili uredsku mrežu u Windowsima
   označiti kao privatnu (inače se čvorovi u lokalnoj mreži ne nalaze).

```
%LOCALAPPDATA%\goCOP\
  postava\   gocop-postava.exe   (Postava)
  program\   gocop.exe           (čvor; ova mapa je u korisničkom PATH-u)
  data\      gocop.db, gocop.toml, dnevnici gocop.log i postava.log, kopije\
```

**Ikona u traci:** valovi u bojama znaka kad čvor radi, sivi kad je
zaustavljen, crveni kad je pao ili ne odgovara, a narančasta točka znači da
postoji novije izdanje. Dvoklik otvara goCOP u pregledniku. Desni klik: *Otvori
goCOP*, *Pokreni/Zaustavi*, *Nadogradi na …*, *Provjeri nadogradnje*, *Otvori
mapu s podacima*, *Dnevnik čvora*, *Pokreni pri prijavi*, *O programu*,
*Ukloni goCOP…*, *Izlaz* (gasi i čvor).

**Nadogradnja** je uvijek na klik. Postava jednom pri pokretanju i svakih
šest sati pita GitHub za popis javnih izdanja (ne šalje ništa o čvoru ni
podacima). Pri nadogradnji: preuzme i provjeri potpis, uredno zaustavi čvor,
kopira bazu u `data\kopije\` (zadnje tri), zamijeni program, pokrene ga i
čeka da odgovori s novim izdanjem. Ne odgovori li za 90 sekundi, vraća
prethodni program i javlja grešku; bazu ne vraća sama, jer ju je novo izdanje
možda već promijenilo. Prethodni program ostaje kao `gocop.prethodni.exe`,
neuspjeli kao `gocop.neuspjeli.exe`.

**Ako čvor padne,** Postava ga podiže ponovno, uz rastući razmak; nakon tri
pada u deset minuta odustaje, ikona je crvena, a razlog je u dnevniku čvora.

**Deinstalacija** na tri mjesta: *Aplikacije i značajke* → goCOP, izbornik
Start → goCOP → *Ukloni goCOP*, ili *Ukloni goCOP…* u izborniku ikone u traci.
Isto se nudi kad se instalacijski program pokrene na računalu na kojem je
goCOP već instaliran (*Popravi ili nadogradi* / *Ukloni goCOP*). Gasi Postavu
i čvor, miče pokretanje pri prijavi i mapu iz PATH-a, briše program. Mapa
`data` ostaje, osim ako se na kraju izričito potvrdi i njezino brisanje.

Postava radi i na macOS-u i Linuxu (ikona u traci izbornika ili u području
obavijesti, pokretanje pri prijavi kroz LaunchAgent ili `~/.config/autostart`,
poveznica `~/.local/bin/gocop` umjesto PATH-a), ali instalacijski paketi za
njih dolaze kasnije (docs/plan-instalacija.md).

### Ručno

```
gocop.exe            (Windows)
./gocop              (Linux, macOS)
```

**Ime čvora.** Mreža razlikuje računala po imenu (`[node] id` u
`gocop.toml`, npr. `pperic-thinkpad`, `cop-osijek-unraid`): pod njim čvor
upisuje svoje zapise, a drugi ga pamte uz ključ. Ime se zadaje **prije prvog
pokretanja**: u instalacijskom programu, u `gocop.toml` ili zastavicom
`-node`. Bez toga svjež čvor sam izabere jedinstveno ime (ime računala i
četiri nasumična znaka) i upiše ga u `gocop.toml`. Nakon prvog pokretanja ime
se ne mijenja. Postojeća baza bez upisanog imena zadržava dosadašnje
`gocop-cvor`. Uparivanje odbija računalo koje nosi ime ovog čvora ili ime
poznatog čvora s drugim ključem (dvojnik), isto i primanje na daljinu i
članstvo koje čvor pokaže pri spajanju; računalo koje je samo dobilo novi
ključ najprije se zaboravi i opozove mu se članstvo.

**Postavljanje svježeg čvora.** Prvo pokretanje stvori praznu bazu i zapiše
`data/gocop.toml`. Na stranici prijave svježeg čvora stoji poveznica
**Postavite ga** (`/postavljanje`), s dva puta:

- **nova mreža**, samo za prvo računalo, jednom po mreži: vlasnik napravi
  svoj račun globalnog administratora (lozinka najmanje 10 znakova), a čvor
  osnuje mrežu i postane nositelj njezina ključa. Početni račun `admin` se
  isključuje. Gumb radi tek uz izričitu potvrdu, jer je nova mreža zaseban
  svijet i kasnije se ne može spojiti s drugima;
- **postojeća mreža**, za svako sljedeće računalo: čarobnjak uparivanja s
  računalom koje je već u mreži, ili primanje na daljinu zahtjevom i
  potvrdom (vidi niže).

Stranica *Postavljanje* radi samo s računala na kojem čvor radi, ili iz
lokalne mreže (privatna adresa) uz jednokratni kod koji čvor pri pokretanju
ispiše u dnevnik (`Postavljanje: … ?kod=7KQ4-M2XD`; na Unraidu u dnevniku
spremnika). Kroz tunel i s javne adrese je nema; deset krivih kodova ga gasi
do ponovnog pokretanja.

Početni račun `admin` s lozinkom iz ovih uputa ostaje za prijelaz (ručno
pokretanje, poslužitelji): vrijedi iz lokalne mreže (privatna adresa), a kroz
tunel i s javne adrese ne — ni kad je čvor izravno izložen kroz proslijeđen
port. Svjež čvor ipak ne izlagati internetu prije postavljanja. Na
čvoru koji pokreće Postava vrijedi **samo s tog računala**, jer čvor sluša za
cijelu lokalnu mrežu, pa bi ga u uredu inače mogao preuzeti bilo tko prije
vlasnika.

Prvi korak u programu je registar Administrativna organizacija: sektori, pa
branjena područja. Ako uz bazu stoje datoteke registara i imenika, učita i
njih. Otvoriti `http://localhost` (ili `http://localhost:8080`).
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

**Ovlašteni primatelj.** Računala u mrežu prima čvor koji drži ključ mreže
(`network-key`) ili član kojem je nositelj ključa dao **ovlast za primanje**
(Administracija → Čvor, mreža i sinkronizacija, uz člana *Daj ovlast za
primanje*). Tako uredski poslužitelj prima uredska računala, a ključ mreže
ostaje kod nositelja, npr. na USB-u. Ovlast vrijedi dvije godine i putuje
razmjenom; članstvo koje primatelj izda vrijedi najviše dok vrijedi njegova
ovlast. *Oduzmi ovlast za primanje* poništava članstva svih računala koja je
taj primatelj primio, a ekran ih nabroji da ih se po potrebi primi ponovno.
*Opozovi članstvo* nudi samo čvor koji drži ključ mreže, i primatelj za
članstva koja je sam izdao. Opoziv članstva ovlaštenog primatelja oduzima mu
i ovlast, pa ga radi samo nositelj ključa mreže. Sve čvorove mreže
treba nadograditi na 0.0.33-alfa prije prve ovlasti: stariji program
članstvo koje je izdao primatelj ne prepoznaje.

**Primanje na daljinu.** Kad novo računalo i čvor koji ga prima nisu u istoj
lokalnoj mreži (laptop kod kuće, ured na drugom kraju), umjesto uparivanja
putuju dvije datoteke, npr. e-poštom, vezane tajnim kodom:

1. na novom računalu *Postavljanje → Postojeća mreža → Napravi zahtjev*:
   računalo pokaže **kod za primanje** (8 znakova, npr. `7KQ4-M2XD`) i ponudi
   datoteku `gocop-zahtjev-<ime>.json` (ime i javni ključ, potpisano tim
   ključem). Datoteka ide primatelju e-poštom, a **kod mu čovjek pročita
   telefonom** — nikad u istoj poruci;
2. primatelj (globalni administrator na čvoru s ključem mreže ili ovlašću)
   učita zahtjev pod *Primanje na daljinu*, upiše kod i preuzme **potvrdu**
   (`gocop-potvrda-<ime>.json`). Nositelj ključa mreže može uz to dati i
   ovlast za primanje;
3. novo računalo učita potvrdu na *Postavljanju* i time ulazi u mrežu.

Zahtjev i potvrda nose dokaz (HMAC) ključem izvedenim iz koda (scrypt).
Primatelj prima samo zahtjev koji odgovara kodu koji mu je pročitan, a novo
računalo prihvaća samo potvrdu koja odgovara njegovom kodu, pa se podmetnuta
datoteka odbija na obje strane. Kod iz presretnute datoteke nije moguće
pogoditi. Novi zahtjev poništava prethodni. Ništa drugo u datotekama nije
tajno: članstvo vrijedi samo uz privatni ključ koji nikad ne napušta novo
računalo. Potvrda nosi i čvorove s adresom (stalno izložene prvi), pa
sinkronizacija kreće odmah; kad stignu djelatnici, *Postavljanje* samo vodi
na prijavu.

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

### Oporavak administratora

Kad globalni administrator zaboravi lozinku, a nema drugog globalnog
administratora koji bi mu je poništio, lozinka se poništava
s konzole računala na kojem čvor radi; kroz web i razmjenu to nije moguće.
Naredba radi i dok čvor radi (ista baza), ne pokreće web ni razmjenu i
završava čim obavi posao. Zadaju joj se iste `-config` i `-db` kao čvoru, da
otvori pravu bazu i upiše promjenu u ime pravog čvora; baza koje nema javlja
grešku, ne stvara se nova.

```bash
# laptop, iz korijena projekta (baza data/gocop.db)
./bin/gocop -ponisti-lozinku tkraljevic

# Unraid, iz terminala Unraida (spremnik gocop, korisnik slike 99:100, bez -u root)
docker exec gocop gocop -config /data/gocop.toml -db /data/gocop.db -ponisti-lozinku tkraljevic
```

Ime se traži kao pri prijavi (velika i mala slova svejedno). Ako na čvoru
postoje računi koji se razlikuju samo u slovima (npr. `tkraljevic` i
`TKraljevic`), vrijedi samo ime upisano točno; inače naredba ispiše popis i
ništa ne mijenja. Naredba postavi privremenu lozinku od četiri skupine po
četiri znaka i ispiše je jednom; nigdje se ne zapisuje, a pri prvoj prijavi
traži se nova. U dnevnik
naredbe ide samo da je lozinka računa poništena s konzole, a trajni trag je
nova verzija računa u knjizi verzija. Poništenje radi isto što i Korisnici →
Poništi lozinku: na tom čvoru gasi otvorene prijave osobe, zapamćena
računala, prijave koje čekaju PIN i privremene kodove (rezervni kodovi
ostaju), a sažetak nove lozinke razmjenom stiže na druge čvorove. Prijave na
drugim čvorovima traju do isteka. Privremena lozinka vrijedi i izvana, ali
uz uključen PIN izvana nakon nje treba i PIN sa službene e-pošte ili kod; iz
lokalne mreže dovoljna je lozinka. Prava računa se ne mijenjaju: isključen
račun dobije lozinku, ali se ne prijavljuje dok ga netko ne uključi, a s
konzole ga uključuje dodatna zastavica `-aktiviraj`. Račun se uključuje tek
nakon poništene lozinke i ugašenih prijava; zapne li nešto prije, ostaje
isključen, a naredba javlja grešku i može se ponoviti. Osobni potpisni ključ
zaključan je starom lozinkom, a promjena lozinke ga prekljucava trenutnom,
ovdje privremenom, koja ga ne otvara; zato ga naredba uklanja (kroz knjigu,
pa i na drugim čvorovima) i to ispiše. Već potpisani dokumenti ostaju
provjerljivi, a novi ključ osoba napravi u profilu nakon promjene lozinke.
Spremljenu lozinku sandučića e-pošte osobe naredba na tom čvoru briše i to
ispiše; na drugim čvorovima ona prestaje vrijediti čim stigne nova lozinka,
a briše se pri prvoj uporabi ili sljedećem pokretanju čvora. Ima li račun
uključen s `-aktiviraj` adresu e-pošte koju već ima drugi aktivni račun,
naredba ga svejedno uključi i to ispiše kao upozorenje: zajednička adresa
gasi PIN objema osobama, pa jednome od njih treba upisati drugu.
Blokade zbog previše krivih lozinki ili kodova žive samo u memoriji čvora,
pa ih naredba ne skida: same isteknu (najviše za sat) ili nestanu ponovnim
pokretanjem čvora. Da do ovoga ne dođe, neka mreža ima barem dva globalna
administratora. Rezervni kodovi tu ne pomažu: zamjenjuju PIN, ne lozinku.

## 7. Stalni čvor, spremnik i sigurnosna kopija

Stalni čvor na Linuxu bez spremnika, kao usluga systemd, opisan je u
[uputama za Linux](linux.md).

Docker slika je `ghcr.io/tkraljevic/gocop`, trenutačno za Linux amd64.
Za ponovljivo postavljanje birati oznaku izdanja, npr. `:0.0.34-alfa`,
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
na GitHubu označena su kao *pre-release*.

Izdanje:

1. promijeniti `verzijaPrograma`, upisati novo u [CHANGELOG.md](../CHANGELOG.md),
   commit i oznaka `v0.0.x-alfa`;
2. GitHub (`.github/workflows/izdanje.yml`) nakon provjere izgradi programe
   za sve sustave, `SHA256SUMS` i **nacrt** izdanja s tekstom iz popisa
   izmjena; oznaka mora odgovarati `verzijaPrograma`;
3. na računalu onoga tko izdaje: `go run ./tools/admin/potpis-izdanja potpisi
   v0.0.x-alfa -objavi` pokaže `SHA256SUMS`, izgradi isto izdanje iz čiste
   kopije oznake i provjeri da je svaki program bajt po bajt isti kao u
   nacrtu (Go gradi ponovljivo; uz istu inačicu Go-a), tek tada ga potpiše
   ključem izdanja (`~/.config/gocop/kljuc-izdanja`, nikad na GitHubu), doda
   `SHA256SUMS.sig` i objavi nacrt. Potpis tako jamči da je program točno
   kod iz oznake, a ne samo da je izašao s GitHuba.

Postava vidi samo objavljena i potpisana izdanja; nepotpisano ili tuđim
ključem potpisano izdanje odbija. Ključ izdanja nije ključ čvora ni mreže.
Bez njega nova izdanja traže novu Postavu s novim javnim ključem, pa ga treba
čuvati i izvan tog računala.

Postava ima svoja izdanja: oznaka `postava-v1.0.0` → `.github/workflows/postava.yml`
gradi `gocop-postava.exe` i instalacijski program `goCOP-postava-1.0.0.exe`
(Inno Setup, `build/postava.iss`) kao nacrt. Postava se mijenja samo kad
treba; ono na što se oslanja (`gocop -version`, `-upravitelj`, `/zdravlje`,
imena datoteka izdanja i potpis) ne smije se promijeniti, a čuva ga test
`TestUgovorSPostavom`.

Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.
Verzija stoji u kodu (`verzijaPrograma` u `cmd/gocop/main.go`) i mijenja se pri
izdavanju; program je ispisuje u podnožju stranice i u dnevniku, s kratkom
oznakom commita iz kojega je preveden (i zvjezdicom kad stablo ima nespremljenih
izmjena). Izdanje u gitu nosi oznaku oblika `v0.0.33-alfa`; iz svake takve
oznake GitHub gradi Docker sliku `ghcr.io/tkraljevic/gocop:0.0.33-alfa` i `:latest`.

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
skup podataka može jednom stajati uz izdanje za isprobavanje. Oblik svake
datoteke, s nekoliko izmišljenih redaka, pokazuju
[predlošci uvoza](predlosci/README.md).

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
