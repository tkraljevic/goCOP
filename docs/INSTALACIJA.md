# Postavljanje i održavanje goCOP čvora

Administratorske upute za izdanje 0.0.41-alfa. Pregledane 15. 10. 2026.,
odjeljak o nadogradnji dopunjen 22. 10. 2026.
Kratki pregled projekta: [README](../README.md). Korisnički postupci su u Pomoći aplikacije.

Operativni program za obranu od poplava Hrvatskih voda: povezuje organizaciju,
teren, vodostaje, dokumentaciju obrane, službene akte, ljude i sredstva. Radi
i bez interneta; kopije na različitim računalima međusobno se usklađuju.
Repozitorij nosi program i praznu shemu baze, a podatke unosi ili uvozi
organizacija koja ga koristi.

> **Status: alfa, izdanje 0.0.41-alfa (22. 10. 2026.), za testiranje i daljnji
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
  Stanje dionice proizlazi iz ovjerenih akata i njihova vremena stupanja na
  snagu; storno čuva trag poništenja i isključuje akt iz izračuna stanja.
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
  | `gocop.toml` | postavke, s komentarima; program je zapiše pri prvom pokretanju, a poslije je mijenja samo kad administrator spremi postavke u programu |
  | `gocop.toml.20261012-153000` | prethodne inačice `gocop.toml` koje program spremi prije svake izmjene; čuva zadnjih pet |
  | `gocop.toml.primjer` | opisi i primjeri svih postavki tekuće inačice; program ga piše pri svakom pokretanju i ne čita ga |
  | `node-key` | privatni ključ ovog računala (Ed25519), prava 0600 |
  | `network-key` | ključ mreže, na čvoru koji drži ključ: osnivač ili čvor koji je učitao kopiju ključa |

  U `data/` je i `skenovi/` sa skenovima prijava s terena. Uz `data/`, u istoj
  radnoj mapi, zadano žive `vodostaji/`, stablo izvornih datoteka, i `pakete/`,
  mapa izdanih `.cop` paketa s `katalog.json`. Pod Postavom i kad je čvor
  usluga sustava radna mapa je mapa baze, pa `vodostaji/` i `pakete/` nastaju
  u njoj. Putanje se mijenjaju u `gocop.toml` (`db`, `arhiva`, `podaci`,
  `skenovi`, `pakete`) ili zastavicama.

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
| **4713** | UDP (STUN, kasnije QUIC) | odlazno; dolazno samo od članova mreže | WEB sync, zadano isključen (`[povezivost] web_sync_prijava`) |

Razmjena izvana može ići kroz `https://<domena>/razmjena/tunel` (WebSocket).
Unutar nje ostaje TLS s ključevima uparenih čvorova. U Domenske čvorove upisuje
se `https://<domena>`; u lokalnoj mreži prvo se pokušava izravna veza.
Tunel nije automatsko otkrivanje čvorova niti zamjena za uparivanje.

**Tailscale (od 0.0.37-alfa).** Na računalu s Tailscaleom razmjena koristi i
Tailscale adrese drugih vaših čvorova, na istom portu 4710. Te adrese nikad ne
ulaze u popis adresa čvora, jer taj popis dobivaju svi čvorovi mreže, i ne
upisuju se ručno pod Domenske čvorove. Čvor ih javlja samo u razgovoru koji
ide preko Tailscalea, pa ih čvor izvan vaše Tailscale mreže ne dobiva, ma što
o sebi javio. Svaki čvor ih pamti samo kod sebe.

Da dva vaša čvora počnu razmjenjivati preko Tailscalea, uparite ih jednom
preko Tailscale adrese: na jednom kliknite **Čekaj drugi čvor**, a na drugom
upišite Tailscale adresu prvoga (npr. `100.101.0.2`) i kliknite **Nazovi
čvor**. Isto vrijedi za čvorove koji su već upareni. Nakon toga prvi
razgovor preko Tailscalea donese i ostale adrese, u oba smjera. Razmjena ih
zove iza adresa iz lokalne mreže, a ispred tunela; ploča razmjene ih pokazuje
uz oznaku „(Tailscale)”. Čvor bez Tailscalea ih ne šalje, ne prima i ne zove,
pa mu je razgovor isti kao prije.

Na macOS-u program Tailscale prepoznaje po kartici utunN s Tailscaleovom IPv6
adresom (`fd7a:115c:a1e0::/48`), jer utun karticu imaju i drugi VPN-ovi. Mac u
Tailscale mreži u kojoj je IPv6 isključen radi kao računalo bez Tailscalea.

`[sync] tailscale = false` u `gocop.toml` isključuje Tailscale u razmjeni,
npr. za probu razmjene preko interneta. Čvor tada ne javlja svoje Tailscale
adrese i ne zove tuđe, a razmjenu koja stigne na njegovu Tailscale adresu
odbije; drugi čvor zove njegovu sljedeću adresu. Na prijavu ne utječe.
U Dockeru program vidi Tailscale samo uz mrežu računala (host);
uz most (bridge) čvor radi kao da Tailscalea nema.

**WEB sync (u izradi).** Uz `[povezivost] web_sync_prijava = true` računalo
pri pokretanju otvori UDP utičnicu na portu 4713 i provjeri mrežu preko dva
STUN poslužitelja različitih operatera. Kad je port zauzet, uzme nasumičan i
to javi u dnevniku. Razmjena zasad ide samo dosadašnjim putovima, a u
dnevniku stoji redak `povezivost:` s ishodom, npr. „NAT je lak i čuva port”.
Ako se program dvaput zaredom prekine dok WEB sync radi, iduće pokretanje
kreće bez njega i to kaže na pločici razmjene; `gocop.toml` ostaje kakav
jest. Gumb *Ponovno uključi WEB sync* u kartici *Ovaj čvor* to poništi, od
idućeg pokretanja. Deset minuta rada bez prekida vraća brojač prekida na nulu.

Gumb *Provjeri vezu* u istoj kartici izmjeri mrežu kad god administrator
želi. Dok WEB sync radi, mjeri na njegovoj utičnici; kad ne radi, program za
to kratko otvori UDP utičnicu na portu 4713 i odmah je zatvori. Kartica
pokazuje sažetak, npr. „UDP radi na :4713. NAT je lak i čuva port.”, a blok
*Dohvatljivost ovog računala* na stranici Sinkronizacija pojedinosti: vlastitu
javnu adresu, adrese koje bi WEB sync nudio drugima i kuda promet prema
STUN-u izlazi (obično, kroz Tailscale ili kroz VPN). Taj blok vidi samo
administrator. Spremnik u mostu treba `lan_adresa`: dok je nema, na to
upozorava blok nakon provjere, a pločica razmjene uz uključenu prijavu za
WEB sync. Spremnik s mrežom računala (host) adresu nađe sam.

Da bi se dva računala našla preko interneta, WEB sync se prijavljuje kod
sastajališta, stalnog čvora koji je dostupan s interneta. Na ostalim
računalima upisuje se `[povezivost] sastajaliste = 'https://cop.primjer.hr'`
i `sastajaliste_cvor`, ime tog čvora u mreži; njegov ključ program će
uzimati iz članstva, ne iz zapisa čvora. Na samom stalnom čvoru postavlja se
`posluzi_sastajaliste = true`, uz `web_sync_prijava = true`. Tek tada
odgovara na `https://<domena>/razmjena/signal` (WebSocket, najviše 64 veze,
s jedne adrese 8), a inače vraća 404. Sve tri postavke mijenjaju se samo u
datoteci. Same prijave još nema: dolazi u idućem koraku, a pločica razmjene
tada kaže „WEB sync: prijavljen kod cop.primjer.hr od 12:01”, a na
sastajalištu koliko je računala prijavljeno.

**Što ide izvan računala:** program nema telemetriju o uporabi ni obvezni cloud;
sučelje, fontovi i skripte ugrađeni su u program. Poslovni podaci sinkroniziraju se samo
s goCOP čvorovima iste mreže (članstvo potpisano ključem mreže pri uparivanju),
obostrano autentificiranim TLS-om 1.3.
Bez ikakve postavke program dohvaća javno vrijeme i upozorenja DHMZ-a za ploču
na naslovnoj i vrijeme s Open-Meteo za novi list dnevnika, a pločice karte
dolaze s OpenStreetMapa (`plocice` u `gocop.toml`; prazno isključuje kartu,
osim karte teritorijalnih jedinica; vidi [kartu položaja](karta.md)).
Pločice za kartu u PDF-u dohvaća i sam čvor. Kad globalni administrator traži
koordinate branjenog područja, naziv mjesta ili adresa ide OpenStreetMapovu
Nominatimu. Dohvat vremena s DHMZ-a i Open-Meteo zasad se ne može
isključiti. Tek
kad ih administrator uključi, postoje veze prema poslužitelju e-pošte
(Administracija → E-pošta ili `[posta]` u `gocop.toml`) te, na čvoru s ulogom
preuzimanja vodostaja ili izdavanja prognoze, satno preuzimanje s javnih i
prijavljenih izvora vodostaja (HydroView i mLetva), izvora kiše i vanjskih
prognoza. I bez uloge obrazac letve dohvaća javni popis postaja s
vodostaji.voda.hr, a ručno preuzimanje na stranici letve čita njezin izvor.
Lozinke vanjskih izvora ostaju šifrirane na tom čvoru i ne sinkroniziraju se.
Uz `[povezivost] web_sync_prijava = true` računalo pri pokretanju s UDP porta
4713 pita STUN poslužitelje `stun.l.google.com` i `stun.cloudflare.com` kojom
ga javnom adresom vide, a isto napravi i kad administrator pritisne *Provjeri
vezu*, i uz isključenu prijavu. Oni tada vide javnu IP adresu i port
računala, i ništa drugo. `stun = []` isključuje te upite.
Bez tih veza osnovni rad ostaje dostupan.

**Za vatrozid:** pristup ograničiti na računala koja sudjeluju u testu.
Port 4711 otvarati samo tijekom uparivanja, a 4712 zadržati u lokalnoj mreži.
UDP 4713 treba samo uz uključen WEB sync. goCOP sam ne mijenja pravila
vatrozida: dopuštenje daje čovjek, na službenim računalima informatika.
Odgovori na odlazne STUN upite prolaze i bez posebnog pravila.
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
  lokalne mreže PIN nikad ne traži. Na računalu s Tailscaleom lokalan je i
  izravan zahtjev na Tailscale adresu tog računala, npr.
  `http://100.x.y.z`; to isključuje `[web] tailscale_lokalno = false`, i tada
  takav zahtjev traži PIN, i preko IPv6. Na računalu bez Tailscalea to ne
  mijenja ništa, a u Dockeru vrijedi samo uz mrežu računala (host), jer
  kontejner s mostom ne vidi Tailscale adresu. Na macOS-u se Tailscale
  prepozna samo uz njegovu IPv6 adresu (`fd7a:…`), jer adresu 100.x daju i
  drugi VPN-ovi; ako je u Tailscale mreži IPv6 isključen, prijava na Macu
  ide kao i prije.
  PIN ide na adresu u dopuštenoj domeni
  (zadano `voda.hr`) ili na adresu izvan nje koju je globalni administrator
  potvrdio na tuđem računu, svojim očima (Korisnici → Uredi → *Adresa je
  provjerena*, npr. za djelatnike tvrtke izvođača). Potvrda putuje razmjenom
  s imenom i danom, vrijedi samo dok je adresa ista, a vlastita promjena adrese je briše; na
  adresu koju ima još jedan aktivni račun PIN ne ide ni potvrđenu. PIN
  vrijedi 10 minuta i jednom; pet krivih
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
  staje dok je administrator ne upiše ponovno, da se račun pošiljatelja u
  domeni ne zaključa; prijave računa u domeni broje se po osobi i računu, najviše
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
  *Ispitaj* na stranici E-pošta).
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
  sredini ne može namjestiti traženjem ključa. Razmjenu dobiva samo član mreže: računalo upareno s onim koje drži
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
  Svjež čvor (baze još nema ili u njoj nema nijednog zapisa) ključ napravi
  sam. Čvor čija baza već ima zapise, a ključa uz nju nema,
  **ne kreće**: u dnevnik ispiše koja datoteka nedostaje i gdje se očekuje.
  Pod Postavom ne izlazi, nego ostaje upaljen dok ga ona ne zaustavi, a isti
  razlog piše na ploči (`http://localhost/`) na tom računalu; ikona u traci
  tada pokazuje da čvor radi.
  Novi ključ dao bi mu novi identitet, a upareni čvorovi odbili bi razmjenu
  („dokazao je drugi ključ od uparenog”); ni tajne šifrirane starim ključem
  (lozinka Exchangea, pristupni računi izvora) više se ne bi dale pročitati.
  Izvornu datoteku treba vratiti iz sigurnosne kopije ili s diska na koji je
  premještena; kad uz nju nema ni `gocop.toml` (u njemu je i ime čvora),
  poruka to kaže, pa se vraćaju obje. Samo kad izvornog ključa više nema, a
  novi identitet je namjeran, zaustavljeni čvor se pokrene **jednom** sa
  zastavicom `-novi-kljuc-cvora`, uz istu bazu i kao korisnik pod kojim
  inače radi, pa se zastavica ukloni:
  - **pod Postavom:** u izborniku ikone *Zaustavi*, pa u naredbenom retku
    (na Windowsu `cmd`) `cd /d "%LOCALAPPDATA%\goCOP\data"` i
    `gocop -novi-kljuc-cvora -db "%LOCALAPPDATA%\goCOP\data\gocop.db"`
    (na macOS-u i Linuxu isto, u mapi `data` instalacije i s programom iz
    mape `program`); kad dnevnik javi da je poslužitelj spreman, Ctrl+C, pa
    u Postavi *Pokreni*;
  - **kao usluga sustava (Linux):** naredbe u [linux.md](linux.md),
    poglavlje 6 (sigurnosna kopija);
  - **u Dockeru:** poglavlje 7 ovih uputa (montirane mape i ključ
    čvora).

  Kad nema ni `gocop.toml`, a ni njega se ne može vratiti, prije toga ga
  treba napisati uz bazu s dosadašnjim imenom čvora (redak `id = "…"` pod
  `[node]`); bez njega čvor s novim ključem kreće pod zadanim imenom
  `gocop-cvor` i to javlja u dnevniku. Zatim ga čvor koji ga je primio u
  mrežu zaboravi, opozove mu članstvo i ponovno ga primi. Zastavica se ne
  čita iz `gocop.toml` i nikad se ne podrazumijeva.
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
  na dionicama na tim dionicama i njihovim objektima te u dnevnicima svog
  područja i COP-a (terenska dužnost bez dionica u cijelom području; samo uz
  sektor terenska se dužnost ne upisuje). Doseg dužnosti ne daje pravo na
  akt o stupnju obrane: nacrt rješenja ili obavijesti sastavlja samo Centar
  obrane od poplava sektora (voditelj centra, zamjenik i dežurni operater),
  a ovjerava se kao i dosad (Pomoć → Rješenja i obavijesti). Objekt drugog
  područja koji stoji na dionici vodi njegovo područje, i vezu dionice na
  objekt ili vodomjer drugog područja upisuje samo tko ondje piše. Sektor upisan uz dužnost
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
  sektora za branjeno područje područjem. Dužnosti su stalne dok ih uprava
  ne izmijeni ili opozove, osim privremenog imenovanja (privremena ispomoć kad
  za obranu nedostaje ljudi): ono vrijedi dok na njegovim dionicama, odnosno u
  branjenom području, traje redovna ili izvanredna obrana prema ovjerenim
  aktima, ili zaključno sa zadanim danom ako je on raniji. Privremena uprava (privremeno
  imenovanje ili dužnost s rokom) dužnost koja daje upravu na njezinoj ili
  nižoj razini dodjeljuje kao privremenu, koja ističe zajedno s njezinom (i
  kad je opozvana); zadani rok ne smije biti dulji od njezina, a tuđu
  postojeću dužnost izmjenom ne skraćuje ni ne produljuje. Terenske dužnosti
  ostaju kako su dodijeljene. Istek privremenih dužnosti čvor preračunava pri
  ovjeri i poništenju akta, pri opozivu i svakih deset minuta te ga bilježi u
  knjigu, pa vrijedi i na čvorovima starijeg izdanja.
  Zastavicu globalnog administratora dodjeljuje i uklanja samo
  stalna uprava organizacije. Nepoznata uloga ne daje pravo pisanja i ne može
  se dodijeliti. Isključenom računu poništava se lokalna sesija pri sljedećem
  zahtjevu; ponovno uključenje računa ne vraća tu prijavu. Lozinku koju
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
   preuzet s GitHub stranice izdanja. Za potpis besplatnim certifikatom za
   otvoreni kod prijava je poslana SignPath Foundationu 12. 10. 2026. i čeka
   odgovor. Kad potpis bude uključen, kao izdavač pisat će „SignPath
   Foundation”. Pravila potpisa su u [CODE_SIGNING.md](CODE_SIGNING.md).
2. **Sigurnosno učvršćivanje je u tijeku.** Od 0.0.25-alfa postoje zaštita od
   tuđih stranica, `Secure` kolačić iza HTTPS-a, podesivi pouzdani posrednici,
   ograničenja HTTP-a i razmjene, a uparivanje i primanje u mrežu smije samo
   globalni administrator. Od 0.0.26-alfa prijava izvana traži PIN poslan na
   službenu e-poštu (prekidač zadano isključen, vidi
   [poglavlje 3](#3-podaci-i-sigurnost)). Otvoreno: potpisane uloge izdavanja
   (svaki član mreže zasad smije objaviti prognozu i arhivu) i trenutačna
   dostava opoziva izgubljenog računala. Potpisani opoziv članstva i ovlasti
   za primanje radi od 0.0.33, ali vrijedi na drugom čvoru tek kad mu stigne
   razmjenom. Rezervni i privremeni kodovi vrijede samo na
   čvoru na kojem su nastali, što je dovoljno dok je javni čvor jedan. Čvor
   dostupan kroz tunel treba držati na zadnjem izdanju. Zadani popis
   posrednika još uključuje privatne mreže: prije javnog postavljanja suziti
   ga na stvarne adrese posrednika i provjeriti pristup bez njih.
3. **Automatsko pronalaženje preko interneta nije uvedeno.** Razmjena preko
   ručno zadane domene i WebSocket tunela radi, a od 0.0.37-alfa i preko
   Tailscalea, kad ga imaju oba računala ([poglavlje 2](#2-mreža)); lokalno
   pronalaženje ostaje na LAN-u. Izravna veza dvaju računala preko interneta,
   bez poslužitelja između njih, je u izradi.
4. **Shema se još mijenja.** Sve što se unese u alfi može se izgubiti pri
   promjeni sheme između verzija. Čvorove držati na istom izdanju i
   nadograđivati ih redom iz [poglavlja 7](#7-stalni-čvor-spremnik-i-sigurnosna-kopija).
   Pločica razmjene pokazuje na kojem izdanju radi koji čvor, a što se u
   kojem izdanju promijenilo, piše u [popisu izmjena](../CHANGELOG.md).

## 5. Preporuka za prva računala

- Dva do tri računala u istoj lokalnoj mreži, unutar mreže Hrvatskih voda,
  bez izlaganja na internet.
- Jedan administrator osniva mrežu (Administracija → Čvor, mreža i
  sinkronizacija) i upravlja lozinkama. Ključ mreže ostaje na njegovu
  računalu, a svako računalo upareno s njim postaje član mreže. Uparivanje
  pokreće i potvrđuje globalni administrator (od 0.0.25-alfa); na svježem
  računalu, dok na njemu nema računa, čarobnjak stoji na stranici prijave, ali
  samo za pristup iz lokalne mreže, nikad kroz tunel.
- Program prevesti iz označenog izdanja (npr. `git checkout v0.0.41-alfa`) ili
  koristiti sliku s oznakom izdanja. Uz izdanje na GitHubu stoje `SHA256SUMS`
  i potpis `SHA256SUMS.sig`; kako se provjeravaju, piše u
  [uputama za Linux](linux.md#2-preuzimanje-i-provjera-izdanja).
- Program pokretati kao običan korisnik, iz vlastite mape.
- Sigurnosna kopija mora obuhvatiti cijelu mapu `data/` i izvorno stablo
  vodostaja, i stajati ondje gdje joj mreža ne može pisati
  ([Kopija u koju mreža ne piše](#kopija-u-koju-mreža-ne-piše)). Povrat
  treba probno izvesti prije operativnog rada; ključeve
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
   `pperic-laptop`; nakon instalacije se ne mijenja), **važan izbor
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
   To pravilo vrijedi za program, pa vjerojatno pokriva i UDP 4713 za WEB
   sync; na Windowsima to još nije provjereno. goCOP sam ne dodaje pravila
   vatrozida, a na službenim računalima postavlja ih informatika.

```
%LOCALAPPDATA%\goCOP\
  postava\   gocop-postava.exe   (Postava)
  program\   gocop.exe           (čvor; ova mapa je u korisničkom PATH-u)
  data\      gocop.db, gocop.toml, dnevnici gocop.log i postava.log, kopije\
```

Na svakom sustavu Postava čvor pokreće iz mape `data`, pa se relativne
putanje `podaci` i `pakete` iz `gocop.toml` (zadano `vodostaji` i `pakete`)
računaju od nje. Na novom čvoru stablo arhive i paketi zato nastaju u mapi
`data`. Stablo i paketi koji stoje drugdje, npr. uz mapu `data` jer se čvor
prije pokretao ručno iz mape iznad nje, upisuju se punom putanjom
(`podaci = 'D:\goCOP\vodostaji'` na Windowsu, `podaci = '/Users/…/goCOP/vodostaji'`
na macOS-u). Čvor pri pokretanju u dnevnik upiše pune putanje. Kad relativne
mape nema, a mapa istog imena stoji u mapi iznad, čvor ne počinje novo stablo
ni novu mapu paketa: *Unos u arhivu* kaže koju je mapu tražio i predloži onu
iznad.

**Ikona u traci:** valovi u bojama znaka kad čvor radi, sivi kad je
zaustavljen, crveni kad je pao ili ne odgovara, a narančasta točka znači da
postoji novije izdanje. Dvoklik otvara goCOP u pregledniku. Desni klik: *Otvori
goCOP*, *Pokreni/Zaustavi*, *Nadogradi na …*, *Provjeri nadogradnje*, *Otvori
mapu s podacima*, *Dnevnik čvora*, *Pokreni pri prijavi*, *O programu*,
*Ukloni goCOP…*, *Izlaz* (gasi i čvor).

**Nadogradnja** je uvijek na klik. Postava jednom pri pokretanju i svakih
šest sati pita GitHub za popis javnih izdanja (ne šalje ništa o čvoru ni
podacima). Pri nadogradnji: preuzme i provjeri potpis, zaustavi čvor,
kopira bazu u `data\kopije\` (zadnje tri), zamijeni program, pokrene ga i
čeka da odgovori s novim izdanjem. Ne odgovori li za 90 sekundi, vraća
prethodni program i javlja grešku; bazu ne vraća sama, jer ju je novo izdanje
možda već promijenilo. Prethodni program ostaje kao `gocop.prethodni.exe`,
neuspjeli kao `gocop.neuspjeli.exe`. Čvor bez ključa uz postojeću bazu
(poglavlje 3) od 0.0.35-alfa odgovori i ostane upaljen, pa se prethodni
program ne vraća: on bi uz takvu bazu napravio novi ključ.

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
njih dolaze kasnije ([Postava](postava.md)). Na Linuxu, kad je čvor
usluga sustava, Postavu postavlja instalacijska skripta, a Postava tada ne
pokreće svoj čvor, nego radi s uslugom ([linux.md](linux.md), poglavlje 12).

### Ručno

```
gocop.exe            (Windows)
./gocop              (Linux, macOS)
```

**Ime čvora.** Mreža razlikuje računala po imenu (`[node] id` u
`gocop.toml`, npr. `pperic-thinkpad`, `ured-posluzitelj`): pod njim čvor
upisuje svoje zapise, a drugi ga pamte uz ključ. Ime se zadaje **prije prvog
pokretanja**: u instalacijskom programu, u `gocop.toml` ili zastavicom
`-node`. Bez toga svjež čvor sam izabere jedinstveno ime (ime računala i
četiri nasumična znaka) i upiše ga u `gocop.toml`. Nakon prvog pokretanja ime
se ne mijenja. Postojeća baza bez upisanog imena (npr. kad je `gocop.toml`
premješten) uzima ime pod kojim pamti ključ ovog čvora, iz članstva u mreži
ili zapisa čvora; kad ga ne zna, zadržava dosadašnje `gocop-cvor`. Od
0.0.35-alfa uz postojeću bazu bez `gocop.toml` program ne zapisuje novi
primjer, koji bi tiho zamijenio izvorne postavke, nego pri svakom pokretanju
u dnevniku javlja da datoteke nema i gdje se očekuje; isto i
`gocop -pripremi`, koji instalacijska skripta na Linuxu pokreće pri svakoj
instalaciji i nadogradnji. Uparivanje odbija
računalo koje nosi ime ovog čvora ili ime poznatog čvora s drugim ključem
(dvojnik), isto i primanje na daljinu i
članstvo koje čvor pokaže pri spajanju; računalo koje je samo dobilo novi
ključ najprije se zaboravi i opozove mu se članstvo.

**Postavljanje svježeg čvora.** Prvo pokretanje stvori praznu bazu i zapiše
`data/gocop.toml`. Na stranici prijave svježeg čvora stoji poveznica
**Postavite ga** (`/postavljanje`), s dva puta:

- **nova mreža**, samo za prvo računalo, jednom po mreži: administrator
  napravi svoj račun s pravima globalnog administratora (lozinka najmanje
  10 znakova), a čvor
  osnuje mrežu i postane nositelj njezina ključa. Početni račun `admin` se
  isključuje. Gumb radi tek uz izričitu potvrdu, jer je nova mreža zaseban
  svijet i kasnije se ne može spojiti s drugima;
- **postojeća mreža**, za svako sljedeće računalo: čarobnjak uparivanja s
  računalom koje je već u mreži, ili primanje na daljinu zahtjevom i
  potvrdom (vidi niže).

Stranica *Postavljanje* radi samo s računala na kojem čvor radi, ili iz
lokalne mreže (privatna adresa) uz jednokratni kod koji čvor pri pokretanju
ispiše u dnevnik (`Postavljanje: … ?kod=7KQ4-M2XD`; u spremniku u dnevniku
spremnika). Kroz tunel i s javne adrese je nema; deset krivih kodova ga gasi
do ponovnog pokretanja.

Početni račun `admin` s lozinkom iz ovih uputa ostaje za prijelaz (ručno
pokretanje, poslužitelji): vrijedi iz lokalne mreže (privatna adresa), a kroz
tunel i s javne adrese ne — ni kad je čvor izravno izložen kroz proslijeđen
port. Svjež čvor ipak ne izlagati internetu prije postavljanja. Na
čvoru koji pokreće Postava vrijedi **samo s tog računala**, jer čvor sluša za
cijelu lokalnu mrežu, pa bi ga u uredu inače mogao preuzeti bilo tko prije
administratora.

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
i ovlast, pa ga radi samo nositelj ključa mreže.

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
koju je svaki čvor upisao u kanal, a akt i zadnju verziju svake svoje
ovjere), vraća prostor na disku, te izdaje kanal
kao potpisan `.cop` paket (npr. `gocop-ocitanja-bp16-2024_v1.cop`, po izboru s PDF-ovima i slikama;
stari oblik `.db` i dalje se može izvesti) i ugrađuje ga u bilo koji čvor:
arhiva na disku ili prijenos bez mreže. Na stranici prijave stoji kontakt
glavnog administratora iz registra (mobitel, e-pošta) i centar iz
`gocop.toml` (odjeljak `[kontakt]`).

`gocop.toml` — adresa web sučelja; putanje baze, arhive vodostaja, izvornog
stabla, skenova i paketa; identifikator i naziv računala; portovi i razmak
automatske sinkronizacije; prati li čvor sve (`sve`) i koliko mjeseci
očitanja drži iz razmjene (`povijest_mjeseci`); u kojoj minuti svakog sata
preuzima vodostaje s izvora (`minuta_preuzimanja`, zadano 45, kad objavi i
DHMZ); javna adresa za QR kod;
centar na stranici prijave (`[kontakt]`); izvor pločica karte (`[karta]`);
zadani poslužitelj e-pošte (`[posta]`; postavke spremljene u Administraciji →
E-pošta imaju prednost). Ključ `bootstrap` program zasad ne čita; domenski
čvorovi upisuju se u Administraciji → Čvor, mreža i sinkronizacija →
Domenski čvorovi.
Zastavice na naredbenom retku (`-addr`, `-db`, `-arhiva`, `-podaci`,
`-skenovi`, `-pakete`, `-node`, `-name`,
`-sync-port`, `-pair-port`, `-discovery-port`, `-auto-sync`, `-config`)
imaju prednost pred datotekom.

**Postavke u programu (od 0.0.37-alfa).** Dio `gocop.toml` globalni
administrator mijenja i u Administraciji, svaku postavku na stranici kojoj
pripada: javnu adresu, centar i telefon sa stranice prijave te minutu
preuzimanja vodostaja u *Čvor, mreža i sinkronizacija → Ovaj čvor*, razmak automatske sinkronizacije i razmjenu
preko Tailscalea pod *Mreža* (na toj stranici obje stoje sklopljene), kao i
prijavu za WEB sync i sam WEB sync (`[povezivost] web_sync_prijava` i
`web_sync`), `sve`
i `povijest_mjeseci` pod *Što ovo računalo prati*, izvor karte pod *Opcije*. *Održavanje baze*
pokazuje gdje su baza, arhiva i mape na disku, gdje je datoteka, kad je
mijenjana i njezine kopije. `[posta]` i dalje vodi stranica *E-pošta*. Radi samo s
računala na kojem je čvor ili iz lokalne mreže, nikad izvana ni kroz tunel.
Program novu datoteku prije spremanja pročita i provjeri kao pri pokretanju;
neispravno ne zapisuje, nego javi uz polje. Ručni komentari i ključevi koje
program ne zna ostaju, jer se mijenja samo redak promijenjene postavke, a
nova postavka dobije objašnjenje kao u primjeru. Staru datoteku spremi kao
`gocop.toml.<datum-vrijeme>` (zadnjih pet) i u dnevnik upiše tko je što
promijenio. Naziv čvora, adresu web sučelja i portove, putanje, pouzdane
posrednike i `bootstrap` program pokazuje, ali ih mijenja samo datoteka:
krivim portom ili putanjom čvor bi se zaključao izvan dosega, a posrednici
određuju što je zahtjev izvana. Isto vrijedi za UDP port WEB synca i STUN
poslužitelje (`[povezivost] udp_port`, `stun`): tuđi STUN poslužitelj saznao
bi javnu adresu računala i mogao vratiti lažnu, a krivi port odsiječe
računalo. Samo u datoteci mijenja se i adresa u lokalnoj mreži za spremnik
(`lan_adresa`, samo IPv4), koju bi WEB sync nudio drugim računalima umjesto
adrese spremnika; zasad se samo prikazuje u bloku Dohvatljivost. Isto vrijedi
za sastajalište (`sastajaliste`, `sastajaliste_cvor`, `posluzi_sastajaliste`):
krivom adresom ili krivim čvorom WEB sync bi se prijavljivao kod tuđeg
računala ili ni kod koga. Ime čvora u mreži (`[node] id`) poslije
prvog pokretanja ne mijenja se ni u datoteci. Kad je zastavica pokretanja jača od datoteke (npr. u
spremniku `-addr`, `-arhiva`), to piše uz postavku.

Promjena vrijedi nakon ponovnog pokretanja; dok čeka, stranice s postavkama
to javljaju. Gumb *Ponovno pokreni čvor* (kartica *Ovaj čvor*) ugasi čvor,
koji izađe s kodom 75, a podigne ga onaj tko ga je pokrenuo: Postava za
nekoliko sekundi, usluga sustava za pet sekundi (`Restart=on-failure`),
spremnik samo uz `--restart` (poglavlje 7). Postava odustaje kad se čvor
ugasi tri puta u deset minuta; tada ga pokrenite u izborniku ikone. Čvor
pokrenut ručno u terminalu gumba nema: zaustavite ga (Ctrl+C) i pokrenite
istom naredbom.

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
# ručno pokrenut čvor, iz korijena projekta (baza data/gocop.db)
./bin/gocop -ponisti-lozinku pperic

# u spremniku gocop (korisnik slike 99:100, bez -u root)
docker exec gocop gocop -config /data/gocop.toml -db /data/gocop.db -ponisti-lozinku pperic
```

Ime se traži kao pri prijavi (velika i mala slova svejedno). Ako na čvoru
postoje računi koji se razlikuju samo u slovima (npr. `pperic` i
`PPeric`), vrijedi samo ime upisano točno; inače naredba ispiše popis i
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
Za ponovljivo postavljanje birati oznaku izdanja, npr. `:0.0.41-alfa`,
umjesto promjenjive `:latest`. Spremnik sluša web na 8080, razmjenu na 4710,
uparivanje na 4711 i pronalaženje na 4712/UDP, a WEB sync, kad se uključi,
na 4713/UDP. Radi kao UID/GID `99:100`; mape moraju biti dostupne tom
korisniku. Slika najavljuje portove (`EXPOSE`), ali ih ne objavljuje: 4713/udp
uz uključen WEB sync treba dodati u predložak spremnika na NAS-u, kao i ostale.

- `/data`: baza operative, sadržaji, prognoze, oborine, postavke i ključevi.
- `/arhiva`: `vodostaji.db`, izvorno stablo `vodostaji/`, `skenovi/` i `pakete/`.

Obje mape moraju biti trajno montirane izvan spremnika. SQLite držati na
lokalnom disku. Na NAS-u koristiti izravnu putanju diska ili skupa diskova,
a ne dijeljenu mapu koja spaja više diskova, ni mrežni disk. Ne brisati volumene pri zamjeni slike.

Pločica razmjene upozori kad na disku s tim mapama ostane manje od 10 GB ili
manje od 10 % diska. Disk se prepoznaje po uređaju, pa dva ZFS skupa podataka
(dataseta) ili btrfs podsveska na istom poolu daju dva upozorenja s istim
slobodnim mjestom, a postotak se računa od skupa podataka, ne od poola. Na
ZFS-u zato gledati koliko je GB ostalo.

Isto upozorenje stiže e-poštom, jednom na dan, osobama koje su na tom čvoru
spremile lozinku sandučića, svakoj sa svoje adrese na svoju. Vrijeme zadnje
poruke čvor pamti u datoteci `obavijest-o-disku.txt` uz bazu.

NAS koji sam premješta datoteke s jednog diska na drugi (npr. s brzog diska
na polje diskova) ne smije premjestiti datoteke iz `/data` dok spremnik i
dalje gleda staru putanju. Prije i nakon nadogradnje provjeriti da montirani
`/data` sadrži postojeće `gocop.toml`, `node-key` i bazu; čvor koji drži
ključ mreže (osnivač ili čvor koji je učitao kopiju ključa) čuva i
`network-key`. Spremnik bez ključa uz postojeću bazu ne kreće, a u
dnevniku spremnika piše koja datoteka nedostaje (npr. `/data/node-key`). Bez
`gocop.toml`, a s ključem, kreće, ali ime uzima iz baze, ne zapisuje novi
primjer i u dnevniku javlja da datoteke nema. U oba slučaja zaustaviti
spremnik, provjeriti montaže i vratiti izvornike u `/data`.
Ponovnim osnivanjem mreže ne obnavlja se stari čvor. Samo kad izvornog ključa
više nema, a novi identitet je namjeran: spremnik jednom pokrenuti sa
zastavicom `-novi-kljuc-cvora` na kraju naredbe (`docker run …
ghcr.io/tkraljevic/gocop:<oznaka> -novi-kljuc-cvora`; u grafičkom sučelju
NAS-a to su dodatni argumenti naredbe u predlošku spremnika), pa zastavicu
ukloniti. Ako se ni `gocop.toml` ne može vratiti,
prije toga ga napisati u `/data` s dosadašnjim imenom čvora (redak
`id = "…"` pod `[node]`), inače čvor kreće pod imenom `gocop-cvor`.

Ponovno pokretanje iz programa (*Čvor, mreža i sinkronizacija → Ovaj čvor*)
spremnik izlazi s kodom 75. Docker ga vraća samo kad je spremnik pokrenut s
`--restart unless-stopped` (ili `always`, `on-failure`); u grafičkom sučelju
NAS-a to se upisuje u dodatne parametre predloška spremnika. Iznutra se to ne vidi, pa stranica
upozori prije gašenja. Bez toga spremnik ostaje zaustavljen dok ga ne
pokrenete u kartici Docker (`docker start gocop`). Slika zadaje `-addr`,
`-arhiva`, `-podaci`, `-skenovi` i `-pakete`, pa te postavke iz `gocop.toml`
u spremniku ne vrijede; stranice to pišu uz njih.

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
zaduženja. Na kojem izdanju radi koji čvor, pokazuju pločica „Razmjena s
čvorovima” i stranica Sinkronizacija.

0.0.41-alfa radi uz 0.0.37–0.0.40-alfa i razmjena s njima ide kao i dosad.
Komárom i Budimpešta dobivaju dnevni model s kišom koji ulazi u satni lanac
Dunava. Računalo koje izdaje prognozu nauči ga samo pri prvom izračunu
nakon nadogradnje, pa *Pripremi model* nije potreban.

0.0.40-alfa radi uz 0.0.37–0.0.39-alfa i razmjena s njima ide kao i dosad.
Novo je ime protokola mreže (`[sync] protokol` u `gocop.toml`, zadano
`gocop`): računala s različitim imenom ne razmjenjuju podatke, pa se prava
mreža i testna simulacija (`gocop-sim`) ne mogu pomiješati, a kopirana mapa
podataka s drugim imenom ne kreće. Prije povratka na 0.0.39-alfa zaustaviti
sve instance `gocop-sim`, jer 0.0.39-alfa ime ne provjerava. Nakon
nadogradnje računala koje izdaje prognozu jednom pokrenuti *Pripremi model*:
model se sad uči na istim satima na kojima radi. Tri izbora prikaza
prognoze (graf, tablica, Excel) spojena su u jedan; vrijedi onaj koji je bio
spremljen za tablicu.

0.0.39-alfa radi uz 0.0.38-alfa. Nove poruke razmjene idu samo između dva
računala s 0.0.39-alfa, a 0.0.38-alfa nova polja zanemari, pa razmjena s
njom ide kao i dosad. Ispravak ključa (član mreže više ne može podmetnuti
ključ drugog računala) štiti samo računalo s 0.0.39-alfa, pa stalna računala
nadograditi odmah, a ostala čim mogu. *Pripremi model* ne treba ponovno
pokretati; računalo koje preskače s 0.0.37-alfa i dalje treba korake za
0.0.38-alfa opisane niže.

Dok u mreži ima računala na 0.0.38-alfa, nijedno računalo ne smije imati
upisanu postavku „Povijest tuđih očitanja (mjeseci)” (`[vodostaji]
povijest_mjeseci` u `gocop.toml`). Računalo s tom postavkom tuđa očitanja
starija od zadanog broja mjeseci ne čuva, a novija predaje dalje. 0.0.38-alfa
ne zna koja su izostavljena, pa takav niz primi kao cijeli i preda ga dalje,
a ono što nedostaje više ne traži. To vrijedi i za novi terenski laptop, iako
mu program uz polje i u zaglavlju `gocop.toml` nudi 12 mjeseci. Postavku
isključuje globalni administrator upisom 0 na stranici „Što ovo računalo
prati”, s tog računala ili iz lokalne mreže, i zatim ponovno pokrene program;
do tada postavka i dalje vrijedi. Je li uključena, vidi se u dnevniku
programa pri pokretanju, u retku „Vodostaji: razmjenom se preuzimaju
očitanja zadnjih … mjeseci”. Nadograđeno računalo s tom postavkom na pločici
razmjene navodi računala sa starijim programom i ona s kojima od nadogradnje
još nije razmijenilo podatke.

Računalo koje tu postavku već ima ne isključivati prije nadogradnje.
0.0.38-alfa nije bilježila što je odbila, pa 0.0.39-alfa pri prvom pokretanju
s još upisanom postavkom zapiše da za tuđa očitanja koja je dotad primilo ne
jamči. Zato ga nadograditi nakon stalnih računala, s još upisanom postavkom,
i pokrenuti.
Zatim upisati 0 i ponovno pokrenuti program: računalo tada od računala s
0.0.39-alfa koja niz imaju cijeli zatraži starija očitanja koja prati, a dotad
ih je izostavljalo. Isključi li se postavka još na 0.0.38-alfa, prvo
pokretanje 0.0.39-alfa to ne zapiše. Tada tom računalu ono što je dotad
izostavilo trajno nedostaje, a da to nigdje ne piše, i taj niz drugima predaje
kao cijeli.

Kad .cop paket kanala nosi rupe u nizu, inačice je 4, pa ga 0.0.38-alfa
odbije porukom da je paket izdao noviji program i da treba ažurirati goCOP;
paket bez rupa i dalje je inačice 3. Staru .db arhivu 0.0.38-alfa uveze i
kad nosi zapis o rupama, ali ga ne pročita, pa niz primi kao cijeli.
Računalu na 0.0.38-alfa zato slati .cop paket, a ne staru .db arhivu.
Paketi arhive vodostaja nisu se promijenili.

Ako nakon nadogradnje dnevnik programa javi da pod nekim imenom valjano
članstvo imaju dva računala, razmjena ide s onim čije je članstvo prvo
izdano. Opoziv tog imena ukida članstvo obaju računala. Zato poslije opoziva
ponovno primiti računalo koje zadržava to ime, a drugo pod drugim imenom.
Opoziv može napraviti računalo s ključem mreže, a ovlašteni primatelj samo
kad je oba članstva izdao on.

Na računalu koje gradi arhivu ili izdaje pakete nakon nadogradnje pogledati u
dnevniku programa retke „Stablo arhive: …” i „Mapa paketa: …”. Kad relativne
mape nema, a mapa istog imena stoji jednu razinu iznad mape iz koje je goCOP
pokrenut, uvoz u stablo ne zapisuje ništa i paketi se ne izdaju dok se to
ne riješi. Redak kaže kako: upisati punu putanju u postavke ili napraviti
mapu.

HIS-2000 kotu nule letve zna pisati decimalnim zarezom, npr. 85,21. Gradnja
arhive na 0.0.38-alfa i starijima takvu je kotu snimke korita upisivala bez
decimala, pa su na crtežu korita voda i stupnjevi obrane stajali prenisko.
Takve snimke prepoznaju se po prvom retku datoteke u mapi `profil` letve u
stablu: iza „kota nule” stoji broj sa zarezom. 0.0.39-alfa kotu čita ispravno,
ali snimke koje su već u arhivi ostaju krive dok se njihova letva ponovno ne
izgradi. Na računalu koje gradi arhivu zato nakon nadogradnje ponovno izgraditi
letve s takvim snimkama. Zaseban gumb za to ne postoji: letvu ponovno izgradi
svaki upis za nju pod Administracija → Unos u arhivu, npr. ponovni uvoz
njezinih snimki korita iz HIS-2000. Zatim tim letvama izdati nove pakete, da
ispravak stigne i na računala koja ih primaju paketom. Gradnja sad stane i na
snimci u čijem se zaglavlju vodostaj ili kota nule ne da pročitati kao broj, a
poruka kaže koja je to datoteka; dosad se tada upisivala nula. Takvo zaglavlje
treba ispraviti, inače se letva ne gradi i u arhivu joj ne ulaze novi podaci.
Zato je prve retke snimki korita najbolje pregledati prije nadogradnje.

Od 0.0.39-alfa svaki upis u bazu čeka da ga disk zapiše, pa potvrđen upis
ostaje i kad se računalo sruši. Na Linuxu i Windowsu baza bi tako trebala
podnijeti i nestanak struje, ako disk zapisuje odmah kad to program traži;
nestankom struje to nije ispitano. Na Macu baza ne podnosi nestanak struje ni
prisilno gašenje: tada i dalje mogu nestati zadnji upisi, a baza se može i
pokvariti, jer Mac pri zapisu baze ne prazni predmemoriju samog diska. Mac
zato gasiti uredno, iz izbornika, a ne držanjem tipke za gašenje. Ključeve
računala i mreže, pakete i popis izdanih paketa program spremi na disk prije
nego ih upotrijebi. Na Windowsu to nije sasvim zajamčeno: nestanak struje
odmah nakon izdavanja još može vratiti stari popis izdanih paketa.

Na Linuxu i Windowsu pojedinačni upis u bazu zato traje dulje; koliko, nije
izmjereno. Na Macu se razlika jedva osjeti, jer se predmemorija diska ne
prazni. Gradnja arhive upisuje u velikim skupinama, a shemu nove baze program
upisuje odjednom, pa ni gradnja ni stvaranje nove baze ne bi trebali biti
primjetno sporiji; ni to nije izmjereno. Ništa od toga ne treba uključivati.

Povratak na 0.0.38-alfa je moguć bez kopije baze: nove tablice i stupci u
bazi služe samo računalu s 0.0.39-alfa, a 0.0.38-alfa ih ne čita. Postava ne
ugrađuje starije izdanje, pa se povratak radi ručno: zaustaviti goCOP, na
mjesto programa vratiti potpisani program 0.0.38-alfa i pokrenuti ga. Kad je
nadogradnju na Windowsu napravila Postava, prethodni program stoji u mapi
programa kao `gocop.prethodni.exe`. U spremniku se vraća oznaka slike
`:0.0.38-alfa`. Postava sama vrati prethodni program samo kad novo izdanje ne
odgovori za 90 sekundi. Vraćeno računalo opet je računalo na 0.0.38-alfa, pa
vrijedi isto pravilo o postavci „Povijest tuđih očitanja (mjeseci)”. Što mu
nedostaje, ostaje zapisano u bazi, ali 0.0.38-alfa to ne čita i takav niz
drugima predaje kao cijeli. Ako vraćeno računalo ipak ima tu postavku, ni
nakon ponovne nadogradnje neće znati koja je očitanja dotad izostavilo, pa ih
ni ono ni računala koja od njega primaju neće tražiti. 0.0.38-alfa ne čita ni
popis kvarova, pa bi gradnja arhive na njoj kriva očitanja koja su ostala u
izvornim datotekama vratila u niz, a snimkama korita s kotom nule pisanom
zarezom opet upisala kotu bez decimala. Paketi koje bi tada izdala odnijeli bi
to i na računala koja su ispravak već primila. 0.0.38-alfa ni upis u bazu ne
potvrđuje tek kad ga disk zapiše.

0.0.38-alfa radi uz 0.0.37-alfa i nadograđuje se redom, po mogućnosti istoga
dana: najprije stalni čvor koji izdaje prognozu, zatim ostala računala.
Odmah nakon nadogradnje tog čvora, ne čekajući ostala računala, globalni
administrator na njemu pokrene Prognoze → Postavke → *Pripremi model*, izvan
poplavnog vala: ispravak prognoze izmjeren je uz grešku u ispuštanju
elektrana koja je u 0.0.38-alfa ispravljena, pa se mijenja svaka prognoza.
Traje desetak minuta i više. Na ostalim računalima to ne treba: gumb se
ondje vidi, ali priprema javi da ovaj čvor ne izdaje prognozu i da se model
priprema na čvoru koji je izdaje.

Pri prvom pokretanju 0.0.38-alfa pregleda primljene dnevne listove i zatraži
fotografije koje nedostaju; u dnevniku programa piše „Fotografije uz dnevne
listove: …”, a koliko ukupno čeka, u retku „Spremište sadržaja: … za
dohvat”. Te fotografije ima samo računalo na kojem su upisane, npr. laptop s
kojeg je rađen uvoz iz BP16, pa neka bude uključeno i dohvatljivo dok ih
stalni čvor ne preuzme. To se vidi na stalnom čvoru: na pločici „Razmjena s
čvorovima” broj uz „Sadržaj čeka dohvat” padne za onoliko fotografija koliko
ih dnevnik programa tog čvora navodi iza „na popisu za dohvat”, ili redak
nestane. Na računalu koje fotografije ima taj se broj ne mijenja. U jednoj
razmjeni, svakih pet minuta, stigne najviše 200 fotografija i dokumenata ili
48 MB. Fotografija lista ima oko 130 kB, pa na sat stigne oko 2.400
fotografija, a gigabajt traje oko tri sata; kad i stalni čvor može nazvati
to računalo, otprilike upola kraće. Kad ih stalni čvor ima, ostala računala
ih dobiju i od njega. Do nule broj ne mora pasti: u njemu je i ono što
računalo po pretplati ne dohvaća, npr. PDF-ovi uz „Samo pregledi” ili
fotografije uz „Samo kazalo”, te paketi arhive vodostaja dok se još
dohvaćaju.

Dok sva računala nisu na 0.0.38-alfa, ne brisati područja, sektore, općine,
naselja, županije, korisnike ni dionice, ni letve i objekte koje neka
dionica još navodi. Pravilo da brisanje ima prednost pred istodobnim upisom
vrijedi samo na računalu s 0.0.38-alfa; računalo s 0.0.37-alfa i dalje čeka
novi zapis ili ne može provesti brisanje. Računalo koje se tako razišlo
prije nadogradnje pri prvom pokretanju 0.0.38-alfa makne obrisani zapis i
sve što je uz njega upisano, i ono upisano poslije brisanja. Što je upisano
uz područje ili sektor, vraća se kad se područje ponovno upiše pod istim
brojem ili sektor pod istom oznakom. Što je upisano uz obrisanu općinu,
županiju ili korisnika, u pravilu se ne vraća: ponovno upisana općina ili
županija dobije novi broj, a korisnik novi identitet.

Je li se računalo tako razišlo, vidi se prije nadogradnje u dnevniku
programa: pri svakom pokretanju ponavlja se poruka „Obnova površine iz
knjige (… zapisa): … verzija nije primijenjeno na površinu …; prva: …”. Iza
„prva:” stoji vrsta i oznaka samo prvog zapisa koji nije prošao: areas
(područje), sectors (sektor), counties (županija), municipalities (općina)
ili users (korisnik), ili zapis upisan uz njih, npr. structures (objekt),
settlements (naselje) ili duties (dužnost). Kad iza „prva:” stoji nešto
drugo, npr. defense_episodes (epizoda obrane) ili sections (dionica), iz
poruke se ne vidi je li se računalo razišlo i oko nekog od tih zapisa. Taj
redak dnevnika treba prepisati prije nadogradnje: nakon nje taj zapis i ono
upisano uz njega neće biti na popisu. Ono uz područje ili sektor vratit će
se kad se područje ponovno upiše pod istim brojem ili sektor pod istom
oznakom; ono uz općinu, županiju ili korisnika u pravilu se ne vraća, pa ga
treba upisati iznova.

Na računalu koje je još na 0.0.37-alfa na stranici „Što ovo računalo prati”
ne koristiti ni „Obriši s računala” ni „Makni i obriši”: ondje brišu i ono
što je nastalo na tom računalu, a što još nije stiglo na druga računala,
nestaje zauvijek. Pravilo se na 0.0.37-alfa miče samo gumbom „Makni”.

Pravilo „Sve vrste” za sva područja dosad nije donosilo arhivu vodostaja kad
je imalo godinu početka, kakvu je upisivao zadani obrazac, ili sadržaj „Samo
pregledi” ili „Samo kazalo”. Nakon nadogradnje takvo pravilo samo od sebe
počne dohvaćati cijelu arhivu: oko 300 paketa i oko 50 MB razmjene, a
ugrađena na računalu zauzima oko 10 GB. Na računalu s malo mjesta na disku
prije nadogradnje dodati pravilo za svoje područje, a staro maknuti gumbom
„Makni”, ne „Makni i obriši”. Tek nakon nadogradnje gumbom „Obriši s
računala” obrisati ono što više ne treba. Pakete za pravilo s godinom
početka računala razmjenjuju tek kad su oba na 0.0.38-alfa ili novijem.

Na Windowsu nakon nadogradnje provjeriti da se na stranici letve i dalje
vide historijat i graf prognoze, ako su se vidjeli i prije, i da se u
dnevniku programa nije pojavila poruka „Arhiva vodostaja nije pronađena” ili
„Baza prognoza nije pronađena” koje prije nadogradnje nije bilo. Poruku
„Arhiva vodostaja nije pronađena” računalo koje arhive nema pisalo je i na
0.0.37-alfa. Program sad arhivu, bazu očitanja i bazu prognoza, kad ih samo
čita, otvara samo za čitanje. Na Windowsu je to dosad provjereno na jednom
računalu, i ondje historijat i graf prognoze rade. Ako se poruka
pojavi tek nakon nadogradnje, računalo radi bez historijata i prognoze, a
ostali podaci i dalje se razmjenjuju; to treba prijaviti kao grešku.

Ove upute vrijede za mrežu u kojoj su sva računala na 0.0.37-alfa ili
novijem. Napomene za nadogradnju sa starijih izdanja stoje u
[popisu izmjena](../CHANGELOG.md), uz izdanje na koje se nadograđuje.

Na javnom posredniku isključiti cache za aplikacijske odgovore i nakon
promjene očistiti stare kopije. Program zadano šalje `private, no-store`
izvan `/static/`, ali pojedini prikazi imaju svoje cache postavke. Nakon
postavljanja provjeriti da odjavljeni korisnik ne može preuzeti zaštićeni
izvoz. TLS tunel sam ne rješava ovlasti, CSRF ni sigurnost sesija.

### Kopija u koju mreža ne piše

Mreža čuva podatke kad jedno računalo otkaže, ali ih ne vraća kad se
pokvare. Kriv ili zlonamjerno izmijenjen zapis razmjena odnese na sva
računala mreže. Mreža daje kopije u prostoru, a sigurnosna kopija kopije u
vremenu: pomaže samo kopija od prije nezgode.

Zato sigurnosnoj kopiji ne smije moći pisati ništa što je na mreži. Kopija
na stalno priključenom disku ili u dijeljenoj mapi u koju čvor smije pisati
propadne zajedno s čvorom, npr. s prvim ucjenjivačkim programom. Uvjet
ispunjava jedan od ovih načina:

- **odspojeni disk**, koji se priključi samo dok traje kopiranje;
- **snimke koje se ne mogu prepisati**, npr. snimke ZFS-a ili btrfs-a, ili
  spremište koje upisani sadržaj zaključa;
- **kopija koju povlači drugo računalo**: ono samo dohvaća kopiju, a
  računalo s goCOP-om mu ne može ni pisati ni brisati.

Stalno upaljeno računalo dobro je odredište samo ako na njemu kopije čuvaju
takve snimke, jer je stalno dostupno i napadaču.

Mapu `data/` kopirati često, npr. svaki dan, kako je opisano gore. Paket
arhive vodostaja (`.cop`) dovoljno je kopirati jednom: svako izdanje paketa
je nova datoteka, koja se poslije ne mijenja.

## 8. Verzije

| faza | verzija | git oznaka | značenje |
|---|---|---|---|
| **alfa** | `0.0.x` | `v0.0.1-alfa`, `v0.0.2-alfa`… | razvoj, sve se mijenja; x raste sa svakim izdanjem |
| **beta** | `0.y.x`, od `0.1.0` | `v0.1.0-beta`… | funkcionalnosti zaokružene, oblik stabilan, provjera na terenu; y nova funkcionalnost, x ispravci |
| **stabilno** | `z.y.x`, od `1.0.0` | `v1.0.0`… | operativna upotreba; z samo za nekompatibilnu promjenu (shema baze, razmjena između čvorova, postavke), y nova funkcionalnost, x ispravci |

Čvorovi različitih verzija međusobno se sinkroniziraju, pa je nekompatibilna
promjena ona zbog koje stari čvor ne može raditi s novim. Kako stariji
program postupa sa zapisima novijega, piše u odjeljku
[Različite verzije programa na čvorovima](#različite-verzije-programa-na-čvorovima).
Alfa i beta izdanja na GitHubu označena su kao *pre-release*.

Izdanje:

1. na računalu s cijelim izvozom registara u `data/` (šest datoteka;
   odakle je koja, piše u odjeljku
   [Podaci koji nisu u repozitoriju](#podaci-koji-nisu-u-repozitoriju))
   pokrenuti `make provjera-registara` (provjere registara GitHub ne može
   izvesti). Bez izvoza provjera ne prolazi, pa se izvoz prije izdanja
   vraća iz pričuve ili slaže iznova; ako se izvoz promijenio, brojke u
   provjerama mijenjaju se svjesno, s razlogom u commitu. Zatim promijeniti
   `verzijaPrograma`, upisati novo u [CHANGELOG.md](../CHANGELOG.md), commit
   i oznaka `v0.0.x-alfa`;
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
treba; ono na što se oslanja (`gocop -version`, `-pripremi`, `-upravitelj`,
`/zdravlje`, imena datoteka izdanja i potpis) ne smije se promijeniti, a čuva
ga test `TestUgovorSPostavom`.

Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.
Verzija stoji u kodu (`verzijaPrograma` u `cmd/gocop/main.go`) i mijenja se pri
izdavanju; program je ispisuje u podnožju stranice i u dnevniku, s kratkom
oznakom commita iz kojega je preveden (i zvjezdicom kad stablo ima nespremljenih
izmjena). Izdanje u gitu nosi oznaku oblika `v0.0.41-alfa`; iz svake takve
oznake GitHub gradi Docker sliku `ghcr.io/tkraljevic/gocop:0.0.41-alfa` i `:latest`.

## 9. Za razvoj

Go 1.27.1 ili noviji (prema `go.mod`), bez CGO-a; SQLite (modernc), sučelje
`html/template` ugrađeno u binary.
Rezultate provjera navoditi uz konkretno izdanje; prolaz starog izdanja nije
potvrda za novu verziju.

```bash
go build -o bin/gocop ./cmd/gocop
go test ./...
go vet ./...
```

Za provjeru kvalitete prije predaje promjene koristiti `make quality`;
alati, mjerenje i usporedba s baselineom opisani su u
[CODE_QUALITY.md](CODE_QUALITY.md). `dev/quality` je zaseban Go modul:
`go test ./...` glavnog modula ga ne provjerava, a `make quality` ga uključuje.

Repozitorij nosi aplikaciju (`cmd/gocop`, `internal`, `web`), upravitelj
instalacije **Postava** (`cmd/gocop-postava`, `internal/postava`), resurse i
instalaciju u `build/` te razvojnu infrastrukturu `dev/quality`, `quality/`,
`Makefile` i CI. Postava je zaseban program, a razvojni alati nisu potrebni
za redovan rad instaliranog čvora.
Uvoz u arhivu iz svih podržanih izvora, arhiviranje,
izdavanje paketa i priprema modela prognoze rade iz same aplikacije. Pomoćni
alati (administracija poslužitelja, jednokratne migracije, dijagnostika,
analize, priprema geometrije) stoje lokalno u `tools/` i ne ulaze u
repozitorij.
Lokalne izgradnje idu u `bin/`, a baze, arhiva vodostaja i paketi u `data/`,
`vodostaji/` i `pakete/`, također izvan repozitorija.

Sinkronizacijski transport — ključevi, uparivanje, TLS razmjena i
pronalaženje na lokalnoj mreži — stoji u `internal/razmjena`, odvojen od
ostatka programa da se mreža može mijenjati bez diranja operative.
Zapisi koji se razmjenjuju moraju podnijeti računala na različitim
izdanjima; pravila su niže, u odjeljku
[Različite verzije programa na čvorovima](#različite-verzije-programa-na-čvorovima).

Kote nule vodomjera vode se u sustavu Trst, a HVRS71 i baltičke kote zasebno
(vidi [baltičke kote](balticke-kote.md)).
Testovi djelatnika i ovlasti, uvoza i razmjene među čvorovima rade na
izmišljenom skupu podataka [`testdata/sektor-p`](../testdata/sektor-p/README.md)
(sektor P, pet izmišljenih djelatnika), pa se izvode svugdje, i na GitHubu.
Samo 11 provjera registara u `internal/db` (Batina i Vukovar na Dunavu, kota
nule iz elaborata, cjelovitost registara) treba stvarne registre. Dvije opće
provjere prvog punjenja (drugo punjenje ništa ne udvostručuje, tekstovi su
ispravan UTF-8) rade na izmišljenom skupu svugdje, a uz stvarne registre
ponavljaju se i na njima. Tako i provjera registra teritorija (sve
županije, gradovi i općine, naselja, sjedište županije, dionice jednog
sektora i ugroženo područje jedne dionice): na izmišljenom skupu provjerava upite, a
brojke izvoza tek na stvarnim registrima. Stvarni registri su izvoz registara u mapi
`data/` u korijenu repozitorija (`sections.json`, `watercourses.json`,
`territories.json`, `section_territories.json`, `objekti_bp16.json`,
`organizacija.json`); odakle je koja, piše u odjeljku
[Podaci koji nisu u repozitoriju](#podaci-koji-nisu-u-repozitoriju), a
`sections.json` slaže administratorski alat `prijepis-dionica`, izvan
repozitorija. Brojke u provjerama su brojke izvoza s kojim je napunjen
prvi čvor; uz noviji izvoz razlike se pregledaju i brojke mijenjaju
svjesno, s razlogom u commitu. Bez cijelog izvoza provjere se preskaču, uz
poruku koje datoteke nema i što treba. `go test` bez `-v` preskakanje ne
ispisuje, pa to na kraju kaže `make quality` (zasebno `make
stanje-registara`), a na GitHubu stoji kao upozorenje u sažetku provjere.
Pokreću se prije svakog izdanja
naredbom `make provjera-registara`, koja ih bez cijelog izvoza ruši umjesto
da ih preskoči.
Kad je u `data/` i baza čvora (`gocop.db`), ista naredba pregleda testove
i testne podatke prema imenima i korisničkim imenima iz nje (djelatnici,
upisivači u dnevnike, dežurstva, vodočuvarske listove i prijave) i prema
brojevima mobitela djelatnika: nijedan ne smije nositi stvarno ime ni broj,
osim imena koja program sam prikazuje u pomoći, i to samo u testu koji
pomoć čita. Pregled nalazi ime i prezime i u padežu i obrnutim redom, a
korisničko ime i u složenici (naziv računala ili čvora, adresa e-pošte) i
pisano velikim slovima. Ne nalazi samo prezime, samo ime, ime od tri slova
u padežu ni ime i prezime pisano velikim slovima, pa ne zamjenjuje
čitanje testa. Baza se otvara samo za čitanje; bez nje se pregled preskače.
Stvarne baze čvora čitaju još samo dvije ručne probe, koje ništa ne
provjeravaju: proba stranice kišomjera (`GOCOP_KISA_PROBA=<mapa>`) iscrta
stranice iz baza u `data/` za pregled okom, a mjera veličine izdanja
prognoze u `internal/prognoza` (`GOCOP_PROGNOZE=<put do prognoze.db>`)
ispiše veličinu zadnjeg izdanja. Mjera bazu prognoza otvara kao program, ne
samo za čitanje (shemu može doraditi), pa se pokreće na kopiji baze.

### Različite verzije programa na čvorovima

Računala se ne nadograđuju u isti trenutak, pa zapise koje razmjenjuju čitaju
i stariji i noviji programi. Zato vrijede tri načela:

1. **Nepoznata polja se čuvaju.** Kad stariji program izmijeni zapis, polja
   koja je dodao noviji program prepiše iz prethodne verzije, pa ne nestanu.
   Iznimka su potvrde članstva te izdanja i modeli prognoze: potpis ili paket
   pokriva samo ono što je u njima zapisano.
2. **Shema je po vrsti zapisa.** Svaka verzija nosi shemu svoje vrste zapisa
   (entiteta), zadano 1. Zapis koji je zadnji izmijenio program s novijom
   shemom stariji program sprema, prenosi i prikazuje, ali ga ne mijenja i
   ne briše, nego javlja da treba nadograditi goCOP. Shemu 2 zasad imaju
   samo dnevna izvješća dionica i sektora.
3. **Što program ne razumije, javlja.** Zapise novije sheme i vrste zapisa
   koje ne poznaje javlja u dnevniku i na pločici razmjene, uz poruku da
   treba nadograditi goCOP. Zapis vrste koju ne poznaje ostaje u knjizi
   verzija, a nakon nadogradnje program ga pri pokretanju upiše
   (Odluke (11. 10. 2026.)).

Pri izmjeni zapisa koji se razmjenjuju iz toga slijedi:

- nova polja dodaju se na vrh zapisa, ne unutar ugniježđenih struktura, jer
  program nepoznata polja čuva samo na prvoj razini zapisa;
- shema se podiže samo kad se mijenja značenje polja, ne za novo polje; polje
  koje se namjerno ukida navodi se u `ledger.UmiroviPolja`;
- popravci podataka polaze od zadnje verzije zapisa u knjizi i novu verziju
  pišu samo kad se nešto stvarno promijenilo, bez ičega što ovisi o čvoru ili
  satu, inače bi svaki čvor upisao svoju verziju istog ispravka;
- nova vrsta zapisa upisuje se među poznate (`repository.PoznatiEntiteti`).

Čuvanje polja ne znači da stari program razumije novo ponašanje. Kad izdanje
uvodi pravilo koje mora vrijediti na svim računalima, u popisu izmjena piše
da se sva računala nadograđuju odmah. Razlozi su u komentarima
`internal/ledger/shema.go` i `internal/repository/fixups.go`.

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
  ni ona se ne objavljuju.

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
