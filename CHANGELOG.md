# Popis izmjena

Verzije prate [shemu iz administratorskih uputa](docs/INSTALACIJA.md#8-verzije): alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

## 0.0.29-alfa — u pripremi

**Ime čvora, Postavljanje bez javne lozinke i Postava 1.1.0.** Postojeći
čvorovi ništa ne osjete: zadržavaju svoja imena i račune.

- **Jedinstveno ime čvora.** Dosad je svaki čvor kojem nitko ne upiše ime
  bio `gocop-cvor`, pa bi dva takva računala u mreži tiho prepisivala ključ
  jedno drugome i miješala zapise. Ime se sada zadaje prije prvog pokretanja
  (instalacijski program, `gocop.toml`, `-node`), a inače ga svjež čvor
  izabere sam (ime računala i četiri nasumična znaka) i upiše u `gocop.toml`.
  Postojeća baza bez upisanog imena zadržava `gocop-cvor`.
- **Uparivanje odbija dvojnike**: računalo koje nosi ime ovog čvora ili ime
  poznatog čvora s drugim ključem, uz poruku što učiniti; ključ poznatog
  čvora više se nigdje ne prepisuje tiho.
- **Postavljanje svježeg čvora** (`/postavljanje`): nova mreža s vlastitim
  računom globalnog administratora (početni `admin` se isključuje), uz
  izričitu potvrdu da je to prvo računalo nove mreže, ili put do uparivanja
  s postojećom. Radi samo s tog računala ili uz jednokratni kod iz dnevnika
  (poslužitelji, npr. Unraid); kroz tunel ne.
- **Početna lozinka pod Postavom samo s tog računala**: čvor sluša za cijelu
  lokalnu mrežu, pa u uredu javnu lozinku iz uputa više ne može upotrijebiti
  nitko osim vlasnika.
- **`gocop -pripremi`**: Postava prije prvog pokretanja upiše ime čvora iz
  instalacijskog programa; postojeće ime nikad ne mijenja. Dio ugovora s
  Postavom, uz test.
- **Postava 1.1.0** (zasebno izdanje `postava-v1.1.0`): u instalacijskom
  programu ime računala u mreži i važan izbor nove ili postojeće mreže (uz
  potvrdu za novu), pri prvom pokretanju otvara *Postavljanje*; *Ukloni goCOP*
  u izborniku Start i u izborniku ikone; ponovno pokrenut instalacijski
  program nudi *Popravi ili nadogradi* ili *Ukloni goCOP*.

## 0.0.28-alfa — 3. 10. 2026.

**goCOP Postava: instalacija, nadogradnja i ikona u traci.** Prvo izdanje s
gotovim programima na GitHubu; od njega Postava može instalirati i nadograditi
čvor.

- **Programi uz izdanje.** Uz svako izdanje na GitHubu stoje programi za
  Windows (amd64), Linux (amd64) i macOS (arm64, amd64), `SHA256SUMS` i
  potpis `SHA256SUMS.sig` ključem izdanja. Ključ ne odlazi na GitHub: izdanje
  nastaje kao nacrt i objavljuje se tek nakon potpisa.
- **goCOP Postava** (`gocop-postava.exe`, svoja izdanja `postava-v…`): mali
  program koji instalira goCOP za korisnika bez administratorskih prava,
  preuzme najnovije potpisano izdanje (ili ga uzme s USB-a), pali i gasi
  čvor, drži ikonu valova u traci, stavlja `gocop` u PATH i u pokretanje pri
  prijavi. Nadogradnja je na klik: provjera potpisa, kopija baze, zamjena,
  provjera da novo izdanje odgovara, a ako ne odgovori, vraćanje prethodnog.
  Postava nikad ne mijenja samu sebe, pa na Windowsu ne treba prepisivati
  program koji radi. Instalacijski program za Windows (`goCOP-postava-….exe`,
  Inno Setup) i upute u poglavlju 6 administratorskih uputa.
- **Za Postavu u čvoru:** `gocop -version` ispiše samo izdanje,
  `gocop -upravitelj` se uredno gasi kad mu se zatvori standardni ulaz
  (Windows nema SIGTERM), a `GET /zdravlje` javlja izdanje samo zahtjevu s
  istog računala (kroz tunel i iz mreže je 404). Taj ugovor čuva test.
- **Ikona programa** (valovi) i podaci o izdanju u programima za Windows.

## 0.0.27-alfa — 3. 10. 2026.

**Ovlasti, oporavak administratora i novi izgled.** Nadograditi najprije čvor
dostupan kroz tunel. Nakon nadogradnje dio djelatnika treba ponovno upisati
lozinku e-pošte (vidi Sandučić), a pravo pisanja suženo je na doseg dužnosti
(vidi Ovlasti).

- **Adresa koju potvrdi administrator.** PIN za prijavu izvana ide i na adresu
  izvan dopuštene domene (npr. djelatnika licencirane firme) kad je globalni
  administrator na tuđem računu, ne tuđim očima, u uređivanju profila označi
  *Adresa je provjerena*. Potvrda pamti tko ju je dao i kada, stiže na sve
  čvorove i vrijedi samo za tu adresu; vlastita promjena adrese je briše.
  Adresu koju ima još netko PIN ne dobiva ni potvrđenu.
- **Oporavak lozinke s konzole.** `gocop -ponisti-lozinku ime` na računalu na
  kojem čvor radi (na Unraidu kroz `docker exec`, vidi upute) postavlja
  privremenu lozinku i ispiše je jednom; gasi prijave, zapamćena računala,
  prijave na čekanju i privremene kodove, uklanja potpisni ključ i spremljenu
  lozinku e-pošte. Prava računa ne mijenja; isključen račun uključuje tek
  izričita zastavica `-aktiviraj`. Kroz web i razmjenu se ne može pokrenuti.
- **Lozinka koju postavi netko drugi** (poništenje, lozinka upisana u obrascu
  djelatnika, oporavak s konzole) uklanja osobni potpisni ključ, zaključan
  starom lozinkom: dosad je obavezna promjena lozinke na njemu zapinjala.
  Već potpisani dokumenti ostaju provjerljivi; novi ključ osoba napravi na
  profilu. Lozinka iz obrasca sada se mora zamijeniti pri prvoj prijavi.
- **Sandučić.** Spremljena lozinka e-pošte briše se uz poništenje i lozinku
  od administratora, a na svim čvorovima prestaje vrijediti čim se promijeni
  lozinka za goCOP (i vlastitom promjenom). Pri prvom pokretanju ovog
  izdanja brišu se i lozinke e-pošte za koje se ne može potvrditi da su
  spremljene uz sadašnju lozinku za goCOP; te treba upisati ponovno. Tko
  poništi tuđu lozinku više ne čita tuđu poštu, a tuđim očima sandučić se
  ne otvara.
- **Ovlasti.**
  - Pravo pisanja ide po dosegu dužnosti: dužnost sektora piše u sektoru,
    dužnost područja u svom području, dužnost na dionicama na tim dionicama,
    njihovim objektima i aktima te u dnevnicima svog područja i COP-a.
    Dosad je dužnost područja ili dionice pisala po cijelom sektoru.
  - Sektor dužnosti uvijek se uzima iz područja, a dionice moraju biti iz
    tog područja; uprava ne dodjeljuje dužnost sama sebi.
  - Račun bez aktivne dužnosti, isključen račun i globalnog administratora
    zadužuje samo globalni administrator; uprava koja ne smije uređivati
    cijeli račun dodaje samo ispomoć (bez primarnosti i vlastitog naziva).
  - Lozinku i privremeni kod uprava daje samo osobama niže razine kojima
    smije uređivati cijeli račun; osobama s dužnošću na njezinoj razini
    uprave ili višoj daje ih viša razina ili globalni administrator.
    Dužnost koja daje upravu na njezinoj razini privremena uprava dodjeljuje
    najdulje do isteka vlastite uprave nad tim sektorom ili područjem, a
    spremanje tuđe postojeće dužnosti joj ne skraćuje rok.
  - Zastavicu globalnog administratora postavlja samo stalna uprava
    organizacije, ne privremena.
  - Tuđim očima tuđa spremljena lozinka e-pošte ne koristi se nigdje (ni
    sandučić, ni adresar i njegova usporedba s imenikom, ni slanje akta).
  - Adresa e-pošte koju već ima drugi aktivni račun odbija se svima;
    korisničko ime koje postoji i drugim slovima se odbija.
  - Uklanjanje potpisnog ključa traži lozinku; tuđim očima ključ se ne
    pravi ni ne uklanja. Novi certifikati nose „Ime Prezime (korisničko)”.
- **Izgled u bojama Hrvatskih voda.** Jedan jezik ploča u cijeloj aplikaciji,
  sređena tamna tema i *Administracija › Tema*: glavna boja, naglasak i gumb
  za svijetlu i tamnu temu, s pregledom i provjerom čitljivosti (ispod 3:1
  tema se ne sprema). Tema putuje razmjenom. Karta u tamnoj temi, ikone koje
  su nedostajale i čitljiviji sitni tekst.
- **Uzdužni profil na mobitelu.** Crtež je visok za čitljive natpise i lista
  se vodoravno od nizvodnog kraja; preko cijelog zaslona popuni vidljivi dio
  zaslona (i na iPhoneu), a oblačić s vrijednostima radi i na dodir.

## 0.0.26-alfa — 3. 10. 2026.

**PIN za prijavu izvana.** Nadograditi najprije čvor dostupan kroz tunel.
Prekidač je zadano isključen: dok ga administrator ne uključi, PIN se ne
traži. Odmah vrijede pravila za vlastitu adresu e-pošte, brojanje prijava na
Exchange i gašenje prijava uz lozinku od administratora (stavke ispod).

- **Drugi korak prijave izvana.** Prijava kroz posrednika ili s javne adrese
  nakon točne lozinke traži šesteroznamenkasti PIN poslan na službenu
  e-poštu osobe (zadano samo `@voda.hr`), rezervni kod s profila (deset
  jednokratnih) ili privremeni kod administratora (24 h, jednom). Prijava iz
  lokalne mreže PIN nikad ne traži. PIN vrijedi 10 minuta i jednom; pet
  krivih upisa poništi prijavu na čekanju, deset u satu zaključa upis kodova
  na sat. Preglednik se može zapamtiti na 30 dana; promjena lozinke, na bilo
  kojem čvoru, to poništi. Radi samo preko HTTPS-a (posrednik mora slati
  `X-Forwarded-Proto: https`, što cloudflared radi sam); izvana preko
  nešifriranog http-a prijava se odbija jasnom porukom.
- **Pošiljatelj PIN-a.** Račun u domeni upisuje se na čvoru iza tunela
  (Administracija → E-pošta), provjeri prijavom na Exchange i čuva šifriran
  ključem čvora. PIN se šalje bez kopije u Poslanim stavkama i ne piše u
  dnevnik. Kad Exchange odbije lozinku, slanje staje dok je administrator ne
  upiše ponovno; kad poslužitelj nije dostupan, stane na pet minuta. Najviše
  tri PIN-a osobi u 15 minuta i 60 na sat po čvoru.
- **Prekidač** je zajednička postavka, a uključuje se samo izvana preko
  HTTPS-a, na čvoru koji ima pošiljatelja i nakon uspješnog probnog PIN-a;
  isključuje se odasvud. Čvor koji je u zadnjih sedam dana (otkad program
  radi) primio prijavu izvana, a PIN nema čime slati, diže uzbunu na
  stranicama administracije i u dnevniku; čvor samo u lokalnoj mreži je ne
  diže.
- **Prijave računa u domeni** (lozinka osobnog sandučića i pošiljatelj PIN-a)
  broje se po osobi i računu: najviše tri u pola sata, da goCOP ne zaključa
  račun u domeni. Prijava koju poslužitelj primi briše brojač tog računa i
  osobi ne troši upis; uz to vrijede najviše tri neprihvaćena upisa lozinke
  osobe u 15 minuta po obrascu, pa je pogađanje tuđih lozinki kroz goCOP
  sporo (vlastita točna lozinka više ne briše tu granicu).
- **Veza prema Exchangeu po imenu i lozinci.** Prijavljena veza dosad se
  dijelila po imenu računa, pa je druga lozinka istog imena (i druga osoba
  koja upiše tuđe ime) prolazila vezom vlasnika: kriva lozinka spremala se
  kao provjerena, a tuđi sandučić se mogao čitati. Sada svaka lozinka ima
  svoju vezu, a upis lozinke uvijek se provjerava novom prijavom.
- **Profil i Korisnici.** Osoba vidi svoja zapamćena računala i rezervne
  kodove; administrator izdaje privremeni kod. Vlastitu adresu e-pošte osoba
  mijenja samo iz lokalne mreže, uz trenutnu lozinku i tek nakon zamjene
  početne lozinke; adresa mora biti u
  dopuštenoj domeni (zadano `voda.hr`) i ne smije pripadati drugom aktivnom
  djelatniku, a promjena se javlja na staru adresu.
- **Lozinka koju administrator postavi** kroz obrazac korisnika gasi i
  otvorene prijave te osobe, kao poništenje lozinke.
- **Vremena iz razmjene** s pomakom koji mjesna zona nema (npr. čvor s
  `TZ=UTC`) spremaju se čitljivo u svim tablicama, ne samo u dnevnicima.

## 0.0.25-alfa — 2. 10. 2026.

**Sigurnosno učvršćivanje.** Nadograditi najprije čvor dostupan kroz tunel.

- **Uparivanje i primanje u mrežu samo administrator.** Dosad je svaki
  prijavljeni korisnik, i kroz tunel, mogao upariti svoje računalo i dobiti
  članstvo u mreži, a s njim sve podatke. Sada uparivanje pokreće i potvrđuje
  globalni administrator; svjež čvor bez računa nudi uparivanje samo iz
  lokalne mreže, nikad kroz tunel.
- **Zadana lozinka izvana ne vrijedi.** Javna je, pa prva prijava njome ide iz
  lokalne mreže; izvana s privremenom lozinkom od administratora.
- **Prijava.** Poruka ne otkriva postoji li račun ni je li deaktiviran (ni
  vremenom odgovora). Pokušaji se broje po imenu s adrese, po adresi i, za
  prijave izvana, po imenu, pa napadač više ne zaključava tuđi račun s pet
  pokušaja, a napad s interneta ne zaključava prijavu iz ureda. Ponovni upis
  lozinke (potpis, ovjera, e-pošta) ograničen je na tri pokušaja u 15 min.
  Promjena lozinke gasi ostale prijave te osobe na istom čvoru.
- **Sesija.** Kolačić je iza HTTPS-a (tunel) i `Secure`; identifikator sesije
  je nasumičan (UUIDv4, 122 bita) umjesto vremenskog.
- **Zaštita od tuđih stranica.** Izmjene (POST) koje pokrene tuđa web-stranica
  odbijaju se; usporedba imenika s Exchangeom, koja mijenja podatke, ide
  preko POST-a umjesto GET-a.
- **XSS.** Geometrija vodotoka, nazivi iz registra u obrascu dionice i nazivi
  koje javljaju računala s lokalne mreže (uparivanje) idu u stranicu samo kao
  tekst. Privici e-pošte i slike poslužuju se u pješčaniku; u pregledniku se
  otvaraju samo PNG, JPEG, GIF i WebP, a SVG i HTML se preuzimaju.
- **Zaglavlja.** `nosniff`, `Referrer-Policy: same-origin`, zabrana ugradnje u
  okvir, osnovni CSP i `Permissions-Policy`; iza HTTPS-a HSTS.
- **Pouzdani posrednici.** Adresa klijenta iz `CF-Connecting-IP` vrijedi samo
  od pouzdanog posrednika (zadano ovo računalo i privatne mreže; suzi se u
  `gocop.toml` `[web] pouzdani_posrednici`); zahtjev sa zaglavljem posrednika
  uvijek je vanjski. Krivo zadan posrednik zapisuje se u dnevnik.
- **Ograničenja.** Rokovi po ruti (obična stranica minuta, uvozi i izvozi 30
  min, tok događaja bez roka uz ping), tijelo zahtjeva zadano 2 MB, rute s
  datotekama svoje granice; prevelika datoteka odbija se umjesto da se reže.
  Poruka razmjene najviše 256 MiB, delta 32 MiB (ostatak odmah u nastavku),
  tunel 32 veze i 2 po klijentu uz rukovanje od 5 s, port razmjene 32
  rukovanja, a razmjena najviše 4 odjednom (višak dobije „čvor je zauzet”).
- **`/api/areas`** samo prijavljenima; sigurne povratne adrese.
- **GitHub provjera.** `go vet`, testovi i `govulncheck` na svako slanje i
  tjedno; slika za Unraid gradi se tek kad provjera prođe.
- **Vremena dnevnika iz razmjene** čitljiva su i na čvoru čija zona nije
  hrvatska.

## 0.0.24-alfa — 2. 10. 2026.

**Nadogradnja redom** — najprije stalni čvor, odmah zatim ostali. Dok svi ne
rade na ovom izdanju, ne mijenjati djelatnike ni zaduženja: 0.0.23-alfa i
starija izdanja pri pokretanju još prekodiraju korisnike i zaduženja.

**Tok obavijesti samo prijavljenima** — `/api/events` bio je otvoren bez
prijave i drugim web-stranicama, a pri izmjeni djelatnika slao je njegov cijeli
zapis, s telefonima i e-poštom; na čvoru dostupnom kroz tunel mogao ga je
čitati bilo tko. Sada ga dobivaju samo prijavljeni, s iste stranice, a
obavijest o djelatniku ili zaduženju nosi samo identifikator.

**Razmjena javlja izdanje programa** — čvorovi u razmjeni javljaju na kojem
izdanju rade. Pločica „Razmjena s čvorovima” i stranica Sinkronizacija
pokazuju izdanje svakog čvora. Za čvor na drugom izdanju stoji upozorenje da
treba ažurirati stariji od dvaju čvorova, a za čvor koji izdanje ne javlja da
njega treba ažurirati. Pločica tada ne javlja da je sve usklađeno.

**Izmjena čuva polja novijeg programa** — kad čvor izmijeni zapis, polja koja
njegov program ne poznaje (dodao ih je noviji program) prepisuju se iz
prethodne verzije. Dosad ih je čvor sa starijim programom svakom izmjenom
tiho brisao. Polje koje je korisnik ispraznio ne vraća se. Isto vrijedi za
popravke podataka pri pokretanju, koji sad polaze od zadnje verzije u knjizi.

**Zapis novije sheme se ne prepisuje** — svaka verzija nosi shemu svog
entiteta (zasad je svima 1). Zapis koji je zadnji izmijenio program s novijom
shemom ovaj ne prepisuje, nego javlja da prije uređivanja treba ažurirati
goCOP. Popravci podataka, postavke obračuna i katalog sredstava takav zapis
pri pokretanju preskoče i to zapišu, a čvor se normalno pokrene. Zapisi
novije sheme i entiteti koje program ne poznaje spremaju se i prenose dalje,
a zapisnik i pločica razmjene javljaju da treba ažurirati goCOP. Paket `.cop`
starije inačice i dalje se ugrađuje, a paket novijeg programa odbija se uz istu
poruku.

**Arhivirano nestaje i na drugim čvorovima** — obrisana bilješka uz arhivsku
vrijednost ostajala je na čvorovima koji su je primili; sada se i tamo
uklanja. Isto vrijedi za vezu dionice s obrisanom letvom te za epizode
obrane, ispravke arhive i nazive razina ustroja kad stignu arhivirani (nazivi
se tada vraćaju na zadane).

**Korisnici i zaduženja se više ne prekodiraju** — novi korisnik odmah dobije
stalni identifikator iz korisničkog imena, kao i korisnici iz početnih
podataka; kad je taj već zauzet (obrisan ili preimenovan račun), dobije
nasumični. Pokretanje zato više ne prekodira ni korisnike ni zaduženja.
Prekodiranje je mijenjalo već razmijenjene verzije na mjestu, pa je ista
verzija na dva čvora imala različit sadržaj, a brojanje zaduženja jednom je
spriječilo pokretanje čvora (0.0.17-alfa).

**Sažimanje knjige čuva granicu svakog čvora** — „Sažmi knjigu” (Administracija
→ Održavanje baze) više ne briše zadnju verziju koju je neki čvor upisao u
kanal, ni kad je zapis poslije izmijenio drugi čvor. Bez nje bi granica tog
čvora pala, a drugi čvorovi slali bi te verzije natrag svakom razmjenom.

## 0.0.23-alfa — 2. 10. 2026.

**Arhiva kiše raste i na čvoru koji ne preuzima** — čvor nije primao pakete
za letve koje je nekad sam sagradio iz svog stabla, pa je laptop, otkad
vodostaje i kišu preuzima Unraid, ostao bez novih zapisa 35 Pljuskovih i 6
DHMZ-ovih kišomjera. Čvor koji sam preuzima i dalje čuva svoje letve; čvor
koji ne preuzima ugrađuje paket kad on nosi sve što lokalna letva ima (do
istog ili kasnijeg dana i barem jednako zapisa). Paket koji seže kraće se ne
ugrađuje, pa se ništa ne gubi.

## 0.0.22-alfa — 2. 10. 2026.

**Ploča razmjene više ne broji poslano kao neposlano** — čvor je pamtio dokle
drugi čvor zna onako kako mu je ovaj javio na početku razmjene, pa je ono što
mu je u istoj razmjeni poslao do sljedeće razmjene brojao kao da još čeka
(Unraid je laptopu stalno „slao još 101 verziju”, a laptop je bio usklađen).
Zapamćena granica sada uključuje i poslane verzije.

## 0.0.21-alfa — 2. 10. 2026.

**Prva razmjena odmah nakon pokretanja** — pola minute nakon pokretanja, a ne
nakon punog razmaka od pet minuta: ažuriranje ili nekoliko pokretanja zaredom
više ne ostavljaju čvorove neusklađene četvrt sata.

**Kazalo arhive bez starih verzija** — pri pokretanju i svakih šest sati iz
knjige se brišu zamijenjene verzije kazala arhive (vrijedi samo zadnje izdanje
letve). Uklanja i oko 4.700 zapisa nakupljenih dok su se čvorovi nadglasavali.

## 0.0.20-alfa — 2. 10. 2026.

**Naziv čvora stiže do drugih čvorova** — naziv upisan u `gocop.toml` nakon
uparivanja čvor pri pokretanju sam objavi, pa ga drugi čvorovi vide umjesto
imena računala iz uparivanja. Mijenja se samo naziv; javne adrese ostaju.

## 0.0.19-alfa — 2. 10. 2026.

**Kazalo arhive bez nadglasavanja** — čvor objavljuje paket arhive samo kad je
njegovo izdanje novije od onoga što kazalo već ima. Kad je stalni čvor kao novi
izdavač izdao novija izdanja kišomjera, laptop je svake dvije minute ponovno
objavljivao svoja starija, a stalni čvor svoja: knjiga je rasla po 1.200
zapisa na sat, a pločica je stalno javljala „šalje se još 40 verzija”.

**„Javlja se sam” umjesto „ne odgovara”** — čvor s nedavnom uspješnom
razmjenom prikazuje se kao na mreži i kad ga ovaj čvor ne može nazvati (laptop
izvan kuće sam zove kroz tunel), uz tu napomenu umjesto greške mreže.

## 0.0.18-alfa — 2. 10. 2026.

**Izvoz prognoze uvijek svjež** — poveznice „Izvoz u Excel” i „Pričuvni
izračun” pri svakom učitavanju stranice dobivaju novu adresu, pa preglednik
ne može dati staru spremljenu datoteku (kopija spremljena dok je Cloudflare
slao „čuvaj 4 sata” inače bi i dalje stizala iz preglednika).

## 0.0.17-alfa — 2. 10. 2026.

**Čvor se pokreće bez obzira na redoslijed zaduženja** — pri pokretanju se
zaduženja prekodiraju na stalne identifikatore „korisnik + redni broj”, a broj
se uzimao iz redoslijeda redaka u lokalnoj bazi. Razmjena upiše zaduženja kako
stignu, pa je na drugom čvoru redoslijed bio obrnut: prekodiranje je zamijenilo
dva zaduženja, palo na jedinstvenosti i čvor se nije dao pokrenuti. Zaduženje
koje već nosi stalni identifikator svog korisnika sad ostaje kakvo jest, a
staro nasumično dobije prvi slobodan broj.

## 0.0.16-alfa — 2. 10. 2026.

**Ništa u priručnu memoriju posrednika** — svaki odgovor osim `/static/` nosi
`Cache-Control: private, no-store`. Cloudflare je izvoz prognoze (`.xlsx`)
prema nastavku čuvao do 4 sata i davao ga svakome, i bez prijave: drugi
korisnik dobivao je staru prognozu, a izvoz se dao preuzeti bez računa. Uz
ovo je na Cloudflareu dodano pravilo da se za cop-osijek.com ništa ne sprema.

## 0.0.15-alfa — 2. 10. 2026.

**Upozorenja DHMZ-a pregledno** — zeleno upozorenje (DHMZ-ovo „nema
upozorenja”) više se ne prikazuje kao upozorenje: umjesto sedam istih kartica
stoji jedan redak „županija: nema upozorenja” s razdobljem, a „drugdje u
Hrvatskoj” broji samo prava upozorenja. Redak unutar kartice nosio je klasu
crvenog upozorenja (ružičasta traka u svakoj kartici), a obojena upozorenja
dobila su tamnu temu, pa su čitljiva.

## 0.0.14-alfa — 2. 10. 2026.

**Letva razmjenom sa svim poljima** — primljena letva nije upisivala šest
polja koja zapis nosi: uvoz s Geolux HydroViewa (`telemetrija_site`,
`telemetrija_uvoz`), ograde niza, povijest, opis vodokaza i datum osnivanja.
Stalni čvor zato nije preuzimao 31 HydroView letvu, među njima dva vrha lanca
(Beničanci – Prkos, Kapelna), i prognoza je stala. Čvor pri pokretanju letve
obnovi iz knjige, pa se polja popune sama. Novi test prenosi letvu sa svim
poljima s čvora na čvor i pada čim letva dobije polje koje razmjena ne prenosi.

## 0.0.13-alfa — 2. 10. 2026.

**Razmjena kroz tunel bez rušenja** — WebSocket na strani poslužitelja nema
adresu druge strane, a bilješka o razmjeni ju je ispisivala: prva veza kroz
tunel srušila je čvor. Veza kroz tunel sad ima svoju adresu, a greška u jednoj
vezi razmjene više ne može srušiti cijeli čvor — veza se zatvori i zapiše.

## 0.0.12-alfa — 2. 10. 2026.

**Razmjena kroz web tunel** — čvor koji izvana smije samo na web (Cloudflare
tunel, bez otvorenog porta) prima razmjenu i na `https://<adresa>/razmjena/tunel`,
kroz WebSocket. Unutra teče isti TLS s ključevima čvorova kao na portu razmjene:
nepoznati ključ ne dobiva ni bajt, a tunel vidi samo šifrirane bajtove. Druga
strana se onamo spaja kad je adresa čvora `https://domena` (Postavke →
Domenski čvorovi); u istoj mreži prvo se pokušava port razmjene.

**Razmjena na naslovnoj** — pločica „Razmjena s čvorovima”: za svaki upareni
čvor zadnja razmjena, primljeno i poslano i koliko se još šalje ili prima;
napredak arhive (ugrađeno od koliko paketa, što se upravo ugrađuje, koliko se
još dohvaća); sadržaj koji čeka dohvat; tko izdaje prognozu i kad je stiglo
zadnje izdanje, s upozorenjem kad je starije od tri sata. Kad je sve usklađeno,
jedan redak. Osvježava se sama.

## 0.0.11-alfa — 2. 10. 2026.

**Kiša razmjenom** — uz svako izdanje prognoze idu mjerenja kišomjera zadnjih
48 sati i satna kiša Open-Meteo po točkama slivova s prognozom (oko 80 kB po
izdanju). Čvor koji ne izdaje prognozu kišu upiše u svoju bazu oborina, pa i
on ima svježa očitanja kišomjera, ne samo arhivu.

**Arhiva kiše raste sama** — nakon noćnog ulaganja kiše izdavač odmah izda
pakete promijenjenih kišomjera i kazalo pošalje razmjenom, bez ručnog
izdavanja arhive.

**Izdavanje nastavlja primljeni niz** — čvor koji je letvu primio paketom, a
sam je još nije izdavao, nastavlja broj izdanja onoga od koga ju je primio:
isti sadržaj zadrži broj, novi dobije sljedeći. Inače bi čvor koji preuzme
izdavanje krenuo od v1, a ostali bi ga odbili kao starije izdanje.

## 0.0.10-alfa — 2. 10. 2026.

**Kiša se ne ulaže bez stabla izvornih datoteka** — čvor koji izdaje prognozu
jednom dnevno ulaže izmjerenu kišu u arhivu i letvu iznova gradi iz stabla.
Čvor koji je arhivu dobio paketima stabla nema, pa bi gradnja meteorološkim
postajama povijest svela na zadnja četiri dana. Bez stabla se ulaganje sad
preskače i javlja u ispisu kruga.

**Telemetrija jasnije** — računi nose naziv sustava i adresu (Geolux HydroView,
hdv.voda.hr; mobilna stranica Hrvatskih voda, mletva.voda.hr), a stranica kaže
treba li ih ovaj čvor: računi trebaju samo čvoru koji preuzima vodostaje.

## 0.0.9-alfa — 2. 10. 2026.

Popravci koje je pokazao prvi stalni čvor (Unraid) pri prvoj razmjeni.

**Prva razmjena bez rupa na površini** — novi čvor registar prima u paketima
po 5000 verzija, pa je naselje znalo stići prije svoje općine i ostati samo u
knjizi (2205 naselja i jedna dionica). Neuspjeli zapisi sad se pokušavaju
iznova u istom prijenosu i sa svakom sljedećom razmjenom, dok ne prođu.

**Prva razmjena odjednom** — dok je razgovor pun (5000 verzija), razmjena
nastavlja odmah, do 200 razgovora zaredom, umjesto po 5000 svakih pet minuta.

**Brava arhive u spremniku** — program je u spremniku uvijek proces 1, pa je
brava prekinute ugradnje paketa nakon ponovnog pokretanja izgledala živom i
zaustavila bi ugradnju arhive. Brava sad razlikuje dva života istog procesa.

**Osnivanje mreže uz zatečeni ključ** — datoteka ključa mreže bez zapisa u
bazi više ne zaustavlja osnivanje: ključ se preuzme, ne pregazi.

## 0.0.8-alfa — 2. 10. 2026.

**Novi čvor s praznom bazom se pokreće** — jednokratni popravak registra
(gradovi Ivanec i Vrbovec) padao je na praznoj bazi jer županija još nema dok
registar ne stigne razmjenom, pa se novi čvor u spremniku nije dao pokrenuti.
Sad se preskače; gradovi stižu razmjenom s čvora na kojem je popravak izveden.

## 0.0.7-alfa — 2. 10. 2026.

**Uloge čvora** — u Postavkama čvora (Uloge ovog čvora) uključuje se preuzima
li čvor vodostaje s izvora i izdaje li prognozu. Vodostaje dovoljno je da
preuzima jedan čvor: očitanja putuju razmjenom. Čvor od prije zadržava što je
radio; novi ne radi ni jedno dok mu se uloga ne uključi.

**Prognoza razmjenom** — svako izdanje (satno i dnevno, izbor rezervi, kiša po
međuslivovima i tuđe prognoze zadnjih 48 sati) ide u knjigu verzija, a
namješteni model kad se promijeni. Ostali čvorovi izdanje upišu u svoju bazu
prognoza i prikazuju ga s oznakom čvora koji ga je izdao; kiša po slivovima na
naslovnoj kod njih dolazi iz izdanja. U razmjeni izdanja stoje sedam dana (oko
100 kB po izdanju), u bazi prognoza ostaju.

**Arhiva razmjenom** — kazalo .cop paketa (letva, izdanje, razdoblje, veličina)
drže svi čvorovi; sam paket dohvaća čvor kojemu ga pokriva pretplata (nova
vrsta „Arhiva vodostaja”, po području letve) ili koji prati sve, od bilo kojeg
čvora koji ga ima, i ugradi ga nakon provjere otiska. Starije izdanje se ne
ugrađuje, a letva sagrađena na samom čvoru se ne gazi.

**Slika za spremnik** — `ghcr.io/tkraljevic/gocop` gradi se za svako izdanje;
`/data` za bazu i postavke, `/arhiva` za arhivu, stablo, skenove i pakete.

**Izvoz prognoze** — redak „model” (satni/dnevni) skriven je u grupi i otkriva
se gumbom „+” uz rub. Gornja Radgona maknuta je s uzdužnog profila Mure jer za
nju nema prognoze.

## 0.0.6-alfa — 1. 10. 2026.

**Vukovar sa svojim protokom** — iz HIS-2000 uvezeni su protoci Vukovara
2001.–2025. (satni i, prvi put, dnevni; dotad satni samo do 2018.) i krivulja
2025.–2026. od −100 cm (dotad −80), pa sažetak više ne uzima protok Iloka;
godišnji protoci Vukovara imaju i 2019.–2025. Izdan je `vukovar_v6.cop`.

## 0.0.5-alfa — 1. 10. 2026.

**Čitljivost izvoza** — usporedba s mađarskom prognozom na sažetku dobiva
zaglavlje u dva retka (termin preko triju stupaca, ispod naša · HU · razlika),
a razlika je pravi broj s predznakom iz formata ćelije (bez Excelova upozorenja
„broj kao tekst”); provjera na poplavnim valovima na listu „O prognozi” ima
naslov skupine preko cijele širine (rijeka, model, broj valova) umjesto
odrezanog naziva u uskom stupcu; napomene uz godišnje vodostaje i protoke
dobivaju zalihu visine na uskim listovima.

## 0.0.4-alfa — 1. 10. 2026.

**Sažetak prognoze u Excelu** — novi prvi list „Sažetak” za čitatelje kojima je
puni izvoz previše, A4 položeno: rečenice „Ukratko” koje program sam slaže iz
brojki (rijeka pada, raste ili je stabilna, najveća promjena s protokom, mogući
novi najniži ili najviši zabilježeni vodostaj, pragovi obrane, kiša po
slivovima, razlike prema mađarskoj prognozi od 10 cm naviše), tablica naših
postaja s vodostajem i protokom za sada, sutra, za 3 dana i zadnji dan
prognoze, kretanjem, stanjem obrane i napomenom, te usporedba s mađarskom
prognozom. Protok Vukovara u sažetku je protok Iloka (mjerenih protoka
Vukovara nema od 2019.), označen kurzivom. Najniži i najviši zabilježeni
vodostaj čitaju se iz arhive prije sažetka, da odmah nakon pokretanja ne
stoje samo ovogodišnji.

**Prelamanje teksta i visina redaka u izvozu** — podnaslov svakog lista,
odlomci i tablice lista „O prognozi” te napomene uz godišnje vodostaje i
protoke dobivaju visinu prema duljini teksta (Excel spojenim ćelijama visinu
ne prilagodi sam), pa se tekst ne reže.

**Protok dnevnog modela** — ostaje i kad granica raspona ispadne iz krivulje
protoka (Vukovar 5. i 6. dan).

## 0.0.3-alfa — 1. 10. 2026.

**Ispravak brojki na naslovnoj (Podaci u sustavu)** — pravi kišomjeri broje se
kao različite postaje (109, prije 107: DHMZ-ove postaje sa samo satnim ili samo
dnevnim nizom brojale su se po većem nizu); popuna kratkih rupa pravih
kišomjera iz ERA5 više se ne broji kao oborina po slivovima nego stoji uz
kišomjere; oborina po slivovima navodi obje reanalize (ERA5 i CERRA); padeži
uz brojeve.

## 0.0.2-alfa — 1. 10. 2026.

**Kiša po slivovima na naslovnoj** — za svaki međusliv koji ulazi u prognozu
kiša pala u zadnja 24 i 72 sata i očekivana u sljedećih 48 sati, prema onome
što je za taj međusliv uobičajeno (ERA5 od 1990.): žuto kad toliko padne
prosječno tri puta godišnje, narančasto jednom godišnje, crveno jednom u pet
godina. Upozorenje kaže na kojim će letvama porasti voda, redom niz tok, s
najvećim porastom i danom iz zadnje dnevne prognoze; gleda i kišu koja tek
dolazi, pa se može pojaviti dan-dva prije kiše. Provjereno na kolovozu 2023. i
rujnu 2024. (crveno na Dravi i Muri dva dana prije vrha kiše).

## 0.0.1-alfa — 1. 10. 2026.

Prvo označeno izdanje. Program se koristi i provjerava u COP-u Osijek, ali
nije za operativnu upotrebu bez nadzora: sve se još mijenja.

**Operativa obrane** — teren i očitanja, pragovi i akti o stupnjevima obrane,
dnevnik COP-a, dežurstva i obračun IORS, vodočuvarska knjiga, prijave s
terena, dnevna izvješća dionica i sektora, dnevnici usluga A.02 i A.03.

**Registri** — ustroj organizacije, dionice, vodomjerne postaje, vodotoci,
objekti, teritorijalne jedinice s kartom sektora i branjenih područja,
djelatnici, izvođači, međuslivovi s izvedenim točkama i pravim kišomjerima
(DHMZ, pljusak.com, nacionalne službe s DanubeHIS-a), materijalno-tehnička
sredstva.

**Hidrološka prognoza** — satni lanac do 96 sati i dnevni model do 6 dana s
kišom po međuslivovima; model ispuštanja HE Dubrava, Čakovec i Varaždin (i iz
razine akumulacija); rezerve za svaki izvor (letve na suprotnoj obali,
DanubeHIS za mađarske letve, ponovni pokušaj kad upit istekne); uzdužni profil;
satni, dnevni ili kombinirani prikaz po izboru; pričuvni izračun u Excelu za
dane kad prognoza ne radi; izvoz u Excel; stranica O prognozi s metodom i
provjerom (doprinos kiše izmjeren s kišom poznatom u trenutku izdanja).

**Hidrološka arhiva** — dnevni i satni nizovi vodostaja, protoka i oborine iz
više izvora, spojeni po točnosti; arhiva se može držati na zasebnom disku
(postavka `arhiva`).

**Dokumenti i razmjena** — PDF i Excel obrasci, PAdES potpisi, žig, slanje
e-poštom, Exchange sandučić; knjiga verzija, sinkronizacija uparenih čvorova
(TLS s ključevima čvora), potpisana `.cop` izdanja.

**Poznato** — veliki dravski val od 3. dana prognoza podcjenjuje (dotok iz
Slovenije i Austrije, prognoza kiše u Alpama); Dunav iznad Komároma oslanja se
na mađarsku prognozu dok je svježa. Ostala ograničenja su u
[administratorskim uputama](docs/INSTALACIJA.md) (odjeljak 4) i na stranici
O prognozi.
