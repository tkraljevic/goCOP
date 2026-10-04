# Sigurnost goCOP-a

> **goCOP je u alfa fazi i nije za operativnu upotrebu.** Ovaj dokument
> opisuje kako sigurnost radi u kodu zadnjeg izdanja i što nije zaštićeno.
> Provedene zaštite nisu potvrda spremnosti za operativni rad.

Upute za postavljanje su u [docs/INSTALACIJA.md](docs/INSTALACIJA.md), za
Linux u [docs/linux.md](docs/linux.md).

## Prijava ranjivosti

Ranjivost se ne prijavljuje javnim zahtjevom (issue) ni pull requestom na
GitHubu, jer bi tako bila vidljiva svima prije ispravka.

> **Kontakt za prijavu ranjivosti:**
> `[UPIŠITE KONTAKT — adresa e-pošte ili drugi privatni kanal]`
>
> **Rok potvrde primitka i objave:** `[UPIŠITE ROKOVE]`

U prijavi navedite:

- izdanje (`gocop -version`) i sustav na kojem čvor radi;
- što ste napravili i što se dogodilo, korak po korak;
- što napadač time dobiva i treba li mu za to prijava, pristup lokalnoj
  mreži ili članstvo u mreži čvorova;
- je li problem već negdje objavljen.

Ne šaljite stvarne podatke iz baze, ključeve čvora ni lozinke. Za primjere
su dovoljni izmišljeni podaci (npr. korisnik `pperic`, čvor `pperic-thinkpad`).

## Podržane verzije

| Izdanje | Sigurnosni ispravci |
|---|---|
| zadnje alfa izdanje (`0.0.x-alfa`) | da |
| sva starija izdanja | ne |

Ispravak izlazi samo kao novo izdanje; starija se ne krpaju. Čvorove treba
držati na zadnjem izdanju, stalni čvor prvi (redoslijed je u
[INSTALACIJA.md](docs/INSTALACIJA.md)).

## Model povjerenja

Ukratko: **čvor vjeruje sebi, svojim prijavljenim korisnicima prema
njihovim ovlastima, i svakom primljenom članu svoje mreže, potpuno.**
Sve ostalo (lokalna mreža, internet, posrednici, pronalaženje čvorova) je
nepouzdano.

### Ključ čvora

- Svaki čvor pri prvom pokretanju napravi Ed25519 ključ i spremi ga u
  datoteku `node-key` uz bazu (PEM, PKCS#8, prava `0600`). Postojeći ključ
  se nikad ne prepisuje. Ključ nije u bazi, pa kopija baze ne nosi
  identitet stroja.
- Ime čvora (`[node] id`) nije izvedeno iz ključa. Na mreži se čvor
  dokazuje ključem; drugo ime s istim ključem ili isto ime s drugim
  ključem čvor odbija.
- Isti ključ potpisuje `.cop` pakete arhive i ovjerene akte, a iz njega se
  izvode ključevi za šifriranje lokalnih tajni i ključ izdavatelja
  potpisnih certifikata (vidi dolje).
- Zamjena ključa ne postoji. Računalo s novim ključem ponovno se uparuje;
  staro ime treba najprije zaboraviti i opozvati mu članstvo.

### Mreža, ključ mreže i potvrda članstva

- Mrežu osniva prvi čvor (na stranici `/postavljanje`). On napravi Ed25519
  ključ mreže i spremi ga u datoteku `network-key` uz bazu (`0600`); u bazu
  ide samo javni dio i ne putuje razmjenom.
- **Potvrda članstva** je potpis ključem mreže nad javnim ključem čvora,
  imenom mreže i rokom. Vrijedi 365 dana (osnivaču deset puta dulje).
  Izdaje je samo čvor koji ima `network-key`, i to pri uparivanju, kad
  osoba koja uparuje primi drugi čvor u mrežu.
- Čvor bez valjane potvrde je **poznat, ali mu se razmjena odbija**. Pri
  svakoj vezi čvor provjerava potpis potvrde i rok, ne samo postoji li
  čvor na popisu.
- Povjerenje vrijedi za cijelu mrežu: svaki valjani član smije razmjenjivati
  sa svakim čvorom te mreže, ne samo s onim s kojim se uparivao.
- **Opoziv** (Administracija → Čvor, mreža i sinkronizacija → „Opozovi
  članstvo”, samo globalni administrator) briše zapis o članstvu i šalje
  to brisanje razmjenom svim čvorovima. Popisa opozvanih potvrda nema.
- Automatske obnove potvrde nema; obnavlja se ponovnim uparivanjem.

### Uparivanje kodom od 6 znamenki

- Uparivanje ide preko TCP porta 4711, kroz TLS 1.3 s ključevima obaju
  čvorova. Port je otvoren samo dok uparivanje čeka (najviše 10 minuta),
  a odjednom može čekati samo jedno uparivanje.
- Obje strane prikazuju isti kod od 6 znamenki, izračunat iz javnih ključeva
  dokazanih u TLS-u. Osobe na obje strane uspoređuju kod i potvrđuju;
  uparivanje uspijeva samo ako obje potvrde (najviše 2 minute). Prije toga
  se ništa ne sprema.
- Sve ostalo što drugi čvor javi (ime, izdanje, port razmjene) samo je
  njegova tvrdnja.
- Uparivati smije **globalni administrator** s promijenjenom lozinkom (ne
  dok gleda tuđim očima). Bez prijave smije samo dok je čvor **svjež**
  (ima najviše jedan korisnički račun) i samo zahtjev koji nije došao kroz
  posrednika.

### Razmjena

- Razmjena ide preko TCP porta 4710 (TLS 1.3) ili kroz web: WebSocket na
  `/razmjena/tunel` u kojem je ista TLS veza. Tunel nema prijavu
  korisnika; tko je s druge strane, dokazuje se ključem unutar TLS-a, a
  vanjski HTTPS provjerava se certifikatima sustava.
- Certifikati čvorova su samopotpisani; ne provjerava ih tijelo za
  certifikate, nego se uspoređuje javni ključ (očekivani ključ pri pozivu,
  valjana potvrda članstva pri primanju).
- Ograde: najviše 32 rukovanja i 4 razmjene odjednom (kroz tunel najviše
  16 rukovanja, a s jednog klijenta 2 veze), rokovi čitanja i pisanja,
  poruka najviše 256 MiB, u jednom razgovoru najviše 5000 verzija i 32 MiB.
- Pronalaženje (UDP 4712, IPv4 broadcast) je samo imenik: odgovor nosi ime,
  oznaku čvora i portove. Lažan odgovor može potrošiti pokušaj veze, ali
  ne donosi povjerenje.
- Veliki sadržaji (privici, PDF-ovi) adresirani su SHA-256 otiskom i
  provjeravaju se pri primitku. `.cop` paketi arhive potpisani su ključem
  čvora koji ih izdaje i provjeravaju se pri čitanju.

### Izdanja i potpis

- Uz svako izdanje na GitHubu stoje `SHA256SUMS` (u obliku koji ispisuje
  `sha256sum`) i `SHA256SUMS.sig`: Ed25519 potpis nad tekstom
  `goCOP izdanje v1` (s prelaskom u novi red) i sadržajem `SHA256SUMS`,
  jedan redak base64.
- Potpis se stavlja ručno, ključem izdanja koji nije na GitHubu; GitHub
  Actions prevodi program i objavljuje izdanje kao nacrt, bez potpisa.
- Javni ključ izdanja ugrađen je u kod (`internal/izdanje/izdanje.go`):
  `qFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=`. Provjera na Linuxu
  OpenSSL-om opisana je u [docs/linux.md](docs/linux.md).
- Potpis automatski provjerava Postava pri nadogradnji. Sam `gocop` se ne
  nadograđuje i ništa ne preuzima.

### Prijava

- Lozinke su spremljene kao bcrypt sažetak (zadana cijena). Nova lozinka
  ima barem 6 znakova; prvi administrator s `/postavljanje` barem 10.
  Nepostojeće korisničko ime provjerava se jednako dugo kao postojeće.
- Sesija traje 24 sata od prijave. Kolačić `gocop_session` je `HttpOnly`,
  `SameSite=Lax`, a `Secure` kad je zahtjev stigao preko HTTPS-a. Promjena
  lozinke gasi ostale sesije te osobe na tom čvoru.
- Zaključavanje nakon krivih lozinki: isto ime s iste adrese 5 puta u
  15 minuta, ista adresa 20 puta u 15 minuta, a izvana isto ime 30 puta na
  sat. Brojači su u memoriji i nestaju s ponovnim pokretanjem.
- **Početni račun** `admin` s lozinkom `gocop2026` nastaje pri prvom
  pokretanju i traži promjenu lozinke. Ta lozinka piše u dokumentaciji, pa
  je čvor odbija kad zahtjev dolazi kroz posrednika, a pod Postavom i sa
  svakog drugog računala osim ovoga.
- **Stranica `/postavljanje`** postoji samo dok je čvor svjež. Na njoj se
  postavlja vlastiti administrator (račun `admin` se tada isključuje) i
  osniva mreža, ili se čvor uparuje s postojećom. S tog računala radi bez
  koda; iz lokalne mreže traži jednokratni kod od 8 znakova koji čvor
  ispiše u dnevnik pri pokretanju (nakon 10 krivih kodova ne radi do
  ponovnog pokretanja). Kroz posrednika ne radi.
- **PIN za prijavu izvana** (zadano isključen; uključuje ga globalni
  administrator, izvana i preko HTTPS-a, tek nakon uspješnog probnog
  PIN-a): kad je uključen, prijava izvana nakon točne lozinke traži PIN od
  6 znamenki poslan e-poštom na službenu adresu. PIN vrijedi 10 minuta i
  jednom; dopušteno je 5 krivih unosa po prijavi i 10 na sat po osobi.
  „Izvana” znači: zahtjev sa zaglavljem posrednika ili s adrese koja nije
  privatna, ovo računalo ni lokalna veza. Postoje i zapamćena računala
  (30 dana), rezervni kodovi i privremeni kod koji daje administrator.
  Sve to vrijedi samo na čvoru na kojem je nastalo i spremljeno je samo
  kao HMAC.
- Čvor iza posrednika (Cloudflare tunel, nginx) adresu klijenta uzima iz
  zaglavlja samo ako veza dolazi s pouzdanog posrednika
  (`[web] pouzdani_posrednici` u `gocop.toml`; zadano ovo računalo i
  privatne mreže).

### Zaštite web sučelja

- **Tuđe stranice:** izmjene (POST) koje je pokrenula druga stranica
  odbijaju se po zaglavljima `Sec-Fetch-Site` i `Origin`.
- **Zaglavlja:** `Content-Security-Policy` s `frame-ancestors 'none'`,
  `object-src 'none'`, `base-uri 'self'` i `form-action 'self'`;
  `X-Frame-Options: DENY`; `X-Content-Type-Options: nosniff`;
  `Referrer-Policy: same-origin`; HSTS samo kad je zahtjev stigao preko
  HTTPS-a.
- **Tuđe datoteke** (privici, fotografije, skenovi) poslužuju se u
  pješčaniku (`sandbox`); u pregledniku se prikazuju samo PNG, JPEG, GIF i
  WebP, a sve ostalo (SVG, HTML…) se preuzima.
- **Primljena e-pošta** prikazuje se u okviru bez skripti, s vlastitim CSP-om.
- **Ograničenja veličine i rokova** za svaku vrstu zahtjeva (zadano 2 MiB),
  ograničena zaglavlja i rokovi veze.

### Tajne na disku

- `node-key` i `network-key` su nešifrirani, s pravima `0600`.
- Lozinke računa e-pošte, HydroViewa, mLetve i pošiljatelja PIN-a te sken
  vlastoručnog potpisa šifrirani su AES-256-GCM ključem izvedenim iz ključa
  čvora. Ne putuju razmjenom.
- Osobni potpisni ključ (PAdES) šifriran je lozinkom osobe (scrypt,
  AES-256-GCM) i putuje razmjenom šifriran. Potpis je napredni elektronički
  potpis po eIDAS-u, **ne kvalificirani**.

## Što nije zaštićeno

- **Primljeni član mreže može sve što se usklađuje.** Zapisi u knjizi
  verzija nisu potpisani po autoru i čvor ne provjerava tko smije mijenjati
  koji zapis. Kompromitiran ili zlonamjeran član može izmijeniti ili
  obrisati bilo koji usklađeni podatak na svim čvorovima, uključujući
  korisničke račune i ovlasti, zajedničke postavke (npr. PIN izvana),
  popis čvorova, članstva i izdavatelje potpisnih certifikata. Svaki član
  smije objaviti prognozu i arhivu. Potpisani zapisi i uloge izdavanja su
  u planu ([plan-povezivost.md](docs/plan-povezivost.md)).
- **Opoziv nije trenutan ni potpun.** Opozvana potvrda kriptografski
  vrijedi do isteka roka, a opoziv stiže tek razmjenom.
- **Baze nisu šifrirane.** Tko ima mapu podataka (ili njezinu kopiju) ima
  sve: osobne podatke, sažetke lozinaka, oznake sesija i ključ čvora, a s
  njim i ključeve kojima su šifrirane lokalne tajne.
- **Web sučelje je obični HTTP.** HTTPS postoji samo kroz tunel ili
  posrednika ispred čvora. U lokalnoj mreži bez HTTPS-a lozinke i kolačići
  putuju nešifrirano. PIN izvana radi samo preko HTTPS-a.
- **Početna lozinka i svjež čvor.** Čvor provjerava dolazi li zahtjev kroz
  posrednika, ali ne i je li izravan klijent iz lokalne mreže. Svjež čvor
  zato ne smije biti izravno dostupan s interneta dok se na
  `/postavljanje` ne postavi vlastiti administrator.
- **Sesije:** oznaka sesije je u bazi spremljena kao čisti tekst; sesija
  ne istječe zbog neaktivnosti; promjena lozinke ne gasi sesije na drugim
  čvorovima.
- **Skripte u stranici:** CSP ne ograničava skripte (`script-src`), pa
  zaštita od ubačenog koda ovisi o ispravnom kodiranju ispisa u predlošcima.
- **E-pošta:** slike iz primljenih poruka učitavaju se s udaljenih adresa,
  pa pošiljatelj može saznati da je poruka otvorena. HTML u porukama i
  potpisima čisti se regularnim izrazima, a ne pravim raščlanjivačem.
- **Pronalaženje čvorova** odgovara svakome tko dohvati UDP 4712 i otkriva
  ime čvora i portove.
- **Potpisi programa:** izvršna datoteka nema Authenticode ni drugi potpis
  sustava; Docker slika i instalacijski program Postave nemaju potpis
  izdanja (Postava ima samo SHA-256).
- **Paketi arhive:** paket bez valjanog potpisa ipak se ugrađuje (označen
  kao nepotpisan), a potpisnik paketa ne mora biti član mreže.
- **Izgubljeno ili ukradeno računalo** i administrator s pristupom
  datotekama izvan su ovog modela.
- **Uskraćivanje usluge:** ograde ograničavaju broj veza i veličinu
  zahtjeva, ali čvor nije zaštićen od preopterećenja s mreže.
