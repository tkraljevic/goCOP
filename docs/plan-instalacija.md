# Instalacija goCOP-a: Postava (instalacija, nadogradnja, ikona u traci) i brzo namještanje

Prijedlog, 3. 10. 2026.

**Stanje:** prvi dio izdan u 0.0.28-alfa i Postavi 1.0.0 (3. 10. 2026.): ugovor u
čvoru (`-version`, `-upravitelj`, `/zdravlje`), Postava (`cmd/gocop-postava`,
`internal/postava`), potpis izdanja (`internal/izdanje`,
`tools/admin/potpis-izdanja`), ikona (`internal/ikona`), resursi za Windows
(`build/resursi`), instalacijski program (`build/postava.iss`) i CI
(`izdanje.yml`, `postava.yml`). Brzo namještanje (§3.2, §3.3), paketi za
Linux i macOS te SignPath dolaze poslije.

## 1. Svrha

Djelatnik na Windowsu danas dobije `gocop.exe` i `gocop.toml`, pokreće ga
ručno iz mape i ne zna radi li program. Nadogradnja znači ručno prevođenje ili
kopiranje nove datoteke. Instalacijski program to svodi na jedan čarobnjak,
a dalje se sve radi iz ikone u traci uz sat. Brzo namještanje (paket za
priključenje u mrežu i biblioteka izdanja) je neobavezno.

**Dva programa, dva ritma:**

| | **goCOP Postava** (`gocop-postava.exe`) | **goCOP** (`gocop.exe`) |
|---|---|---|
| što radi | gasi čvor, stavlja novu datoteku, pali novi proces; stavlja `gocop` u PATH i u pokretanje pri prijavi; ikona u traci (pokreni, zaustavi, otvori ploču) | čvor: baza, razmjena, web |
| koliko se mijenja | rijetko; napravi se jednom | svako izdanje |
| izdanja | vlastita (`postava-v1.0.0`) | `v0.0.x-alfa` |
| što instalira | uvijek **najnovije dostupno** izdanje goCOP-a, preuzeto i provjereno pri instalaciji | — |

Razlog je Windows: program koji radi ne može prepisati sam sebe, a goEMM
zato ima poseban pokretač. Ovdje Postava nikad ne mijenja sebe, nego samo
`gocop.exe`, koji je njezino dijete: zaustavi ga, zamijeni i ponovno
pokrene. Instalacijski program ne nosi goCOP u sebi, pa ga ne treba
ponovno graditi ni potpisivati za svako izdanje.

**Opseg:** najprije Windows 10/11, 64-bitni Intel/AMD, jer su korisnici u
Hrvatskim vodama uglavnom na Windowsima. Linux i macOS dolaze poslije (§7),
s istom Postavom i drugim instalacijskim paketom. Unraid ostaje na Dockeru.

## 2. Što korisnik vidi

### 2.1 Instalacijski program `goCOP-postava.exe`

Klasični čarobnjak (Dalje / Natrag), bez administratorskih prava, za
trenutnog korisnika Windowsa:

1. dobrodošlica i licenca (EUPL-1.2, hrvatski tekst);
2. mapa: zadano `%LOCALAPPDATA%\goCOP\`: `postava\` (Postava), `program\` (samo `gocop.exe`, u PATH-u) i `data\` (baza, postavke, dnevnik);
3. **brzo namještanje (neobavezno)**: tri izbora, svaki se može preskočiti:
   - *Paket za priključenje u mrežu* (§3.2), datoteka i lozinka;
   - *Biblioteka izdanja* (§3.3), poveznica i ključ za čitanje;
   - *Pokreni pri prijavi u Windows* (kvačica, zadano uključeno);
4. instalacija: kopira Postavu, prečac u izborniku Start, unos u
   *Aplikacije i značajke* (deinstalacija). Zatim Postava s GitHuba preuzme
   **najnovije izdanje** `gocop.exe`, provjeri potpis (§3.1) i stavi ga u
   `program\`. Bez interneta: *Odaberi preuzetu datoteku* (`gocop.exe` uz
   `SHA256SUMS` i `.sig`, npr. s USB-a), uz istu provjeru;
5. završetak: pokreće Postavu u traci, a ona čvor. Ako je brzo namještanje
   izabrano, čvor prije prvog pokretanja uveze paket za priključenje, pa iz
   biblioteke preuzme i uveze izabrana izdanja. Napredak se vidi u
   pregledniku.

**Bez brzog namještanja** program se instalira i radi kao danas: prazan čvor,
uparivanje u lokalnoj mreži, pretplata u *Što ovo računalo prati*. Brzo
namještanje samo skraćuje posao, ništa ne zamjenjuje.

**Alat:** Inno Setup (otvoreni kod, standardni čarobnjak, instalacija po
korisniku, deinstalacija), gradi se u CI-ju na Windows strojevima, a SignPath
potpisuje i njega, jednom, ne za svako izdanje goCOP-a. Koraci 3 i 5 samo
pozivaju `gocop.exe` s naredbama
`-uvezi-prikljucak` i `-uvezi-biblioteku`, pa isti posao radi i ručno na
Linuxu i macOS-u. Izbor paketa iz biblioteke (koja letva, koje godine) radi
se u pregledniku na stranici *Prvo pokretanje*, jer Inno Setup nema dobar
način za prikaz kataloga.

**Za uredska računala** IT obično traži MSI za raspodjelu (Intune, GPO).
Kasnije: isti sadržaj kao MSI (WiX), kad IT kaže što treba.

### 2.2 Postava u traci

**Ikona u traci** (valovi goCOP-a; boja kaže stanje):

| boja | stanje |
|---|---|
| zelena | čvor radi i odgovara |
| siva | zaustavljen |
| narančasta točka | dostupno je novo izdanje |
| crvena | pao je ili ne odgovara; Postava ga je pokušala podići |

**Izbornik desnim klikom:**

- **Otvori goCOP**: nadzorna ploča u zadanom pregledniku (to je i dvoklik);
- **Pokreni / Zaustavi**;
- **Nadogradi na 0.0.x** (samo kad postoji), uz poveznicu na bilješke izdanja;
- **Provjeri nadogradnje**;
- **Otvori mapu s podacima** i **Dnevnik**;
- **Pokreni pri prijavi** (kvačica);
- **O programu**: izdanje Postave i čvora;
- **Izlaz**: gasi čvor i Postavu.

## 3. Nadogradnja i brzo namještanje

### 3.1 Nadogradnja programa

**Izvor:** GitHub Releases repozitorija `tkraljevic/goCOP`. Uz svako izdanje
CI objavljuje `gocop-windows-amd64.exe` (kasnije i za Linux i macOS),
`SHA256SUMS` i `SHA256SUMS.sig`. Postava ima svoja, rijetka izdanja u istom
repozitoriju (`postava-v*`) i bira samo oznake `v*`.

**Provjera:** pri pokretanju i svakih 6 sati jedan zahtjev prema
`api.github.com`. Ne šalje ništa o čvoru, korisniku ni podacima.

**Tko odlučuje:** korisnik klikne „Nadogradi”. Automatska noćna nadogradnja
može se kasnije dodati kao prekidač, zadano isključen.

**Postupak:**

1. preuzimanje u `program\novo\`;
2. provjera potpisa `SHA256SUMS.sig` javnim ključem izdanja ugrađenim u
   Postavu (ed25519), zatim SHA-256 datoteke. Kad stigne certifikat (§6),
   dodatno i Authenticode potpis (`WinVerifyTrust`). Bilo koja greška: ništa
   se ne mijenja;
3. sigurnosna kopija baze `VACUUM INTO data\kopije\gocop-<staro izdanje>.db`
   (zadržavaju se zadnje tri);
4. uredno zaustavljanje čvora, zamjena: stari postaje `gocop.prethodni.exe`;
5. pokretanje i provjera zdravlja: 90 s za odgovor `/zdravlje` s novim
   izdanjem;
6. ne odgovori li, vraćanje starog `.exe` i obavijest u traci. Baza se ne
   vraća sama: novo izdanje je možda već promijenilo shemu, pa vraćanje kopije
   odlučuje administrator.

Zamjena je jednostavna jer `gocop.exe` u tom trenutku ne radi: Postava ga je
zaustavila. Ipak, do pet pokušaja preimenovanja, jer antivirus zna kratko
držati novu datoteku, a prethodna binarka ostaje uz novu: Defender je
goEMM-u jednom lažno stavio izdanje u karantenu (#135).

**Nadogradnja same Postave:** rijetko i samo ručno. Postava javi da postoji
novo izdanje Postave; korisnik preuzme novi instalacijski program i pokrene
ga preko postojećeg (Inno Setup zatvori staru Postavu i zamijeni je). Podaci
i `gocop.exe` ostaju. Razlozi za novu Postavu su rijetki: promjena ključa
izdanja, nova adresa izdanja, greška u samoj Postavi.

**Kanali:** zadano samo izdanja označena `v*-alfa`/`-beta` iz glavne grane.
Probne gradnje ne idu kroz Postavu.

### 3.1a Ugovor između Postave i goCOP-a

Postava se ne mijenja s goCOP-om, pa ono na što se oslanja mora ostati
stabilno. To je cijeli ugovor; test u goCOP repozitoriju pazi da ga nijedno
izdanje ne prekrši:

- imena datoteka izdanja i oblik `SHA256SUMS` + `SHA256SUMS.sig`;
- `gocop -version` ispiše izdanje i odmah izađe;
- `gocop -upravitelj -db … -config …`: radi kao čvor i uredno se ugasi kad
  mu se zatvori standardni ulaz;
- `GET /zdravlje` (samo s računala samog): `{"izdanje":"…","radi":true}`;
- `-uvezi-prikljucak` i `-uvezi-biblioteku` za brzo namještanje.

Sve ostalo goCOP smije mijenjati bez obzira na Postavu.

### 3.2 Paket za priključenje u mrežu (`.gocop-cvor`)

Danas novi čvor ulazi u mrežu samo uparivanjem u lokalnoj mreži, uz
administratora koji ga prima (0.0.25). Paket za priključenje je
administratorovo **unaprijed dano primanje**, za čvor koji nije u istoj
mreži ili da se uparivanje preskoči.

**Kako nastaje:** administrator na čvoru koji drži ključ mreže (danas
laptop) u *Postavke › Mreža › Novi čvor* upiše ime čvora i predloženu
pretplatu. Program:

1. napravi novi ključ čvora;
2. potpiše mu potvrdu članstva ključem mreže, istu kakvu daje uparivanje,
   pa je ostali čvorovi priznaju bez ikakve izmjene protokola;
3. sve zapakira i šifrira lozinkom koju administrator dojavi drugim putem
   (telefonom, ne istim e-mailom).

**Sadrži:** ime i javni ključ mreže, ključ i potvrdu članstva čvora, adrese
sastajališta (`https://cop-osijek.com`), predloženo ime čvora i pretplatu,
neobavezno poveznicu na biblioteku (bez ključa za čitanje).

**Zaštita:**

- šifrirano lozinkom (scrypt + AES-256-GCM, kao osobni ključ za potpis);
- jedan paket je jedan čvor. Drugi uvoz istog paketa ili dva čvora s istim
  ključem vide se na pločici kao sukob, a čvor se odbija;
- vrijedi 7 dana od nastanka: stariji paket se ne uvozi;
- administrator ga može opozvati (postojeći `RevokeMembership`) i prije
  i poslije uvoza;
- u dnevniku stoji tko je napravio paket i kada je uvezen.

Zato **nema nove vrste povjerenja**: potvrda članstva je ista kao danas, samo
je izdana unaprijed i putuje datotekom umjesto kroz port 4711.

### 3.3 Biblioteka izdanja (privatni repozitorij `.cop` paketa)

**Što je:** zaključan privatni GitHub repozitorij (npr.
`tkraljevic/gocop-biblioteka`), bez koda. U njegovim izdanjima (Releases)
stoje zadnja `.cop` izdanja i `katalog.json`. Paketi i dalje **ne ulaze u
goCOP repozitorij** (`pakete/` ostaje izvan Gita).

**Zašto:** prvi prijenos arhive je velik. Kroz Cloudflare tunel ga ne smijemo
gurati (uvjeti o velikim datotekama), a kroz razmjenu traje. GitHub ga
isporuči izravno i brzo, a dalje čvor nastavlja normalnom razmjenom.

**Pristup:** ključ samo za čitanje (fine-grained token na taj jedan
repozitorij, *Contents: read*, s rokom). Upisuje se pri instalaciji ili
kasnije u *Administracija › Biblioteka*, sprema se šifriran na čvoru, kao
lozinke vanjskih izvora, i ne putuje razmjenom. Isti ključ može koristiti
više čvorova jedne organizacije. Opoziv: obriše se na GitHubu.

**Čitanje:** `GET /repos/{vlasnik}/{repo}/releases/latest` → `katalog.json` →
korisnik bira letve i razdoblje → preuzimanje izabranih `.cop` → uvoz
postojećim putem (`/administracija/baza/uvoz`). Uvoz kao i danas provjerava
potpis paketa, otiske i red izdanja. **Paket se prihvaća samo ako ga je
potpisao član mreže**, pa se paket za priključenje (§3.2) uvozi prvi. Bez
njega biblioteka se može pregledati, ali ne i uvesti.

**Objava:** Unraid izdaje `.cop` pakete kao i danas. Novi alat ih uz
`katalog.json` stavlja u novo izdanje biblioteke, zasad ručno i ključem za
pisanje koji ostaje na Unraidu, kasnije gumbom *Objavi u biblioteku*.

**Što smije u biblioteku:** samo kanali bez osobnih podataka: arhiva
vodostaja, kiše i slični. Dnevnici, prijave, akti i djelatnici **ne**: oni
idu samo razmjenom među članovima. GitHub je treća strana izvan HV-a, pa i
privatni repozitorij treba gledati kao vanjsko spremište.

## 4. Što treba promijeniti u čvoru

To su upravo točke ugovora iz §3.1a:

- **`GET /zdravlje`**: samo s `127.0.0.1`/`::1`, bez prijave; vraća
  `{"izdanje":"0.0.28-alfa","radi":true}`. Postava po tome zna je li čvor
  živ i koje je izdanje.
- **Uredno gašenje:** `SIGTERM` na Windowsu ne postoji, a goEMM zato čvor
  na Windowsu jednostavno ubija. goCOP drži bazu i razmjenu, pa radije
  uredno: zastavica `-upravitelj` znači da se čvor gasi kad mu se zatvori
  standardni ulaz (cijev koju drži Postava). Radi i kad Postava padne: cijev
  se zatvori, čvor se uredno ugasi umjesto da ostane siroče. Isto na svim
  sustavima.
- **`-version`**: ispiše izdanje i izađe, za provjeru preuzete datoteke prije
  zamjene (goEMM `SanityCheck`).
- **Putanje:** već postoje `-db` i `-config`; Postava ih postavlja na
  `%LOCALAPPDATA%\goCOP\data\`.

## 5. Izvedba

**Postava** je `cmd/gocop-postava/` u istom repozitoriju (jedan SignPath
projekt, jedan CI), s vlastitim izdanjima i bez uvoza ičega iz goCOP-ovog
`internal/` osim malog zajedničkog paketa za provjeru potpisa izdanja. Ne
dijeli bazu, web ni razmjenu: s čvorom razgovara samo kroz ugovor (§3.1a).
Gradi se s `-H=windowsgui` (bez prozora konzole), pa joj ne treba poseban
pokretač kao goEMM-u: unos za pokretanje pri prijavi pokazuje izravno na nju,
a `gocop.exe` ostaje konzolni program kakav jest (`-ponisti-lozinku` i
ostale naredbe pišu u konzolu).

**Iz goEMM-a preuzimamo** ono što je tamo već riješeno i isprobano na sve tri
platforme (macOS svakodnevno, Windows 11 amd64 i ARM64, Ubuntu 24.04
GNOME/Wayland; vidi `goEMM/docs/TRAY.md` i `VERIFICATION.md`):

- **ikona u traci:** `github.com/gogpu/systray` (MIT), bez CGO-a: Windows kroz
  sistemske pozive, macOS kroz `goffi` (MIT), Linux kroz StatusNotifierItem na
  D-Busu (`godbus/dbus`). Te ovisnosti ulaze **samo u Postavu**, ne u
  `gocop.exe`;
- **dijalozi:** sistemski, bez vlastitog prozora: PowerShell `MessageBox` na
  Windowsu, `osascript` na macOS-u, `zenity`/`kdialog`/`notify-send` na
  Linuxu. Nadzorna ploča je u zadanom pregledniku;
- **pokretanje pri prijavi:** stvarno stanje je sam unos, nikad zapamćena
  zastavica: Windows `HKCU\…\Run`, macOS LaunchAgent, Linux
  `~/.config/autostart/gocop.desktop`. Postava provjerava pokazuje li unos
  na postojeću datoteku i to javlja (goEMM: unos je mjesecima pokazivao na
  izbrisan `/private/tmp`). Postavlja ga instalacijski program, a kvačica u
  traci ga mijenja. Redak „nema unosa u registry” u uputama mijenja se, jer
  uz instalacijski program ionako postoji unos za deinstalaciju; za ručno
  raspakiran `gocop.exe` ostaje kako je;
- **zamjena datoteke:** preimenovanje uz do pet pokušaja i prethodna binarka
  uz novu (§3.1);
- **PATH:** mapa `program\` (samo `gocop.exe`, bez ičega drugog) dodaje se
  u korisnički PATH (`HKCU\Environment`, bez administratora), uz obavijest
  sustavu (`WM_SETTINGCHANGE`) da je nove konzole vide. Tako `gocop
  -ponisti-lozinku`, `gocop -version` i ostale naredbe rade iz bilo koje
  mape. Postava pri svakom pokretanju provjeri je li unos još tu i pokazuje
  li na pravu mapu, a deinstalacija ga uklanja, i ništa drugo iz PATH-a.
  Na macOS-u i Linuxu poveznica `~/.local/bin/gocop` (Linux poslužitelj iz
  paketa: `/usr/bin/gocop`);
- **jedna Postava:** ako se pokrene dok već radi, javi to i izađe (goEMM #61:
  druga ikona nakon nadogradnje).

**Ostalo:**

- ikona `.ico` (16, 20, 24, 32, 48, 256 px) iz `hv-mark.svg` (četiri vala u
  plavo-zelenom prijelazu); resursi (ikona, podaci o izdanju, manifest za
  DPI i `asInvoker`) u `.syso` datoteci, koja se generira pri gradnji, za
  Postavu i za `gocop.exe`;
- **deinstalacija:** kroz *Aplikacije i značajke* (Inno Setup): briše Postavu,
  `gocop.exe`, prečace, unos za pokretanje i unos u PATH-u. Mapa `data` ostaje dok korisnik
  izričito ne potvrdi brisanje.

**CI:**

- `izdanje.yml` na oznaku `v*`: nakon `provjera.yml` gradi `gocop.exe` za
  `windows/amd64` i `SHA256SUMS`, pa stvara GitHub Release kao **nacrt**.
  Potpis `SHA256SUMS.sig` stavlja se ručno na laptopu (`tools/`, ključ
  izdanja ne odlazi na GitHub), i tek tada se nacrt objavljuje. To je ujedno
  „ručno odobrenje svakog izdanja” koje traži SignPath;
- `postava.yml` na oznaku `postava-v*`: gradi Postavu i instalacijski program
  (Inno Setup, Windows stroj u CI-ju). Pokreće se rijetko.

**Procjena:**

| dio | dani |
|---|---|
| Postava: traka, nadogradnja, nadzor čvora (rješenja iz goEMM-a) | 2,5–3 |
| izmjene čvora (`/zdravlje`, `-upravitelj`, `-version`) i test ugovora | 0,5–1 |
| CI, GitHub Release, potpis izdanja | 1 |
| ikona i resursi | 0,5 |
| instalacijski program (Inno Setup) | 1–1,5 |
| paket za priključenje (izrada, uvoz, zaštita, pločica) | 2–3 |
| biblioteka izdanja (čitanje, izbor u pregledniku, alat za objavu) | 2–3 |
| **ukupno** | **9,5–13** |

Može se isporučiti u dijelovima: najprije CI + Postava + instalacijski
program bez brzog namještanja, zatim paket za priključenje, pa biblioteka.

## 6. Besplatni certifikat (SignPath Foundation)

Uvjeti (signpath.org/terms, provjereno 3. 10. 2026.):

| uvjet | goCOP |
|---|---|
| licenca odobrena od OSI-ja, bez komercijalne dvojne licence | EUPL-1.2: ispunjeno |
| javni izvorni kod, bez vlasničkog koda i zlonamjernog softvera | repozitorij je javan; provjeriti licence u `web/static/vendor` |
| održavan projekt s postojećim izdanjima | **nema ni jednog GitHub Releasea**: prvo §5 CI |
| gradnja iz izvornog koda na provjerljiv način | GitHub Actions (§5) |
| podaci o datoteci (naziv proizvoda, izdanje) | resursi u `.syso` (§5) |
| ručno odobrenje svakog potpisa | uklapa se u nacrt izdanja |
| uloge autor/recenzent/odobravatelj, MFA na GitHubu i SignPathu | jedna osoba u svim ulogama; uključiti 2FA |
| stranica *Code signing policy* s ulogama, izjavom o privatnosti i rečenicom „Free code signing provided by SignPath.io, certificate by SignPath Foundation” | nova stranica u `docs/`, poveznica iz README-a |

**Izjava o privatnosti** mora pošteno reći što program šalje: razmjena među
čvorovima iste mreže, e-pošta kroz Exchange kad je uključena, javni izvori
vodostaja, kiše i karte, provjera nadogradnje na GitHubu. Ništa ne ide
autoru programa.

**Izdavač u potpisu bit će „SignPath Foundation”, ne goCOP.** SmartScreen
upozorenje nestaje kad certifikat stekne ugled, što traje od nekoliko dana do
nekoliko tjedana preuzimanja.

**Redoslijed:** 0.0.28 donosi CI s GitHub Releaseom i resursima. Zatim prijava
na SignPath (podnosi korisnik), pa uključivanje njihove GitHub akcije u
`izdanje.yml` i `postava.yml`. Postava radi i prije toga, uz vlastiti ed25519 potpis i
SmartScreen upozorenje pri prvoj instalaciji.

## 7. Linux i macOS (poslije Windowsa)

| | Linux | macOS |
|---|---|---|
| paket | `.deb` i `.rpm` (nfpm u CI-ju), potpisano GPG ključem; kasnije vlastiti apt repozitorij za nadogradnju | `.pkg` s aplikacijom `goCOP.app` |
| način rada | **poslužitelj** (uredski Ubuntu): systemd usluga, bez ikone, nadogradnja kroz apt; **radna stanica**: korisnička systemd usluga i ikona u traci | ikona u traci izbornika, LaunchAgent kao danas na laptopu |
| ikona u traci | ista `gogpu/systray` kao na Windowsu (StatusNotifierItem); u goEMM-u radi na Ubuntu GNOME/Wayland, a GNOME, KDE i XFCE imaju domaćina za ikonu | ista `gogpu/systray` kroz `goffi`, bez CGO-a; u goEMM-u svakodnevno u upotrebi |
| brzo namještanje | iste naredbe; na poslužitelju i bez preglednika (`-uvezi-prikljucak datoteka`) | kao Windows |
| potpis | GPG, besplatno | Developer ID i notarizacija: Apple Developer Program, 99 USD godišnje. Bez toga Gatekeeper blokira prvo otvaranje (desni klik → Otvori) |
| prioritet | drugi: treba uredskom Ubuntu čvoru, a ne košta ništa | treći: malo korisnika, uz godišnji trošak |

Postojeći Linux poslužitelj (uredski Ubuntu) ne treba čekati paket: systemd
usluga se može napisati ručno kao danas LaunchAgent na laptopu. Paket je za
kad bude više takvih računala.

## 8. Otvorena pitanja

1. Automatska nadogradnja noću: prekidač, zadano isključen?
2. Dijeli li više korisnika istog računala jedan čvor? Prijedlog: ne, čvor je
   po korisniku Windowsa.
3. Uredska računala HV-a: smije li program u `%LOCALAPPDATA%` i `HKCU\Run`
   (AppLocker, politike)? Pitati IT zajedno s §10 plana povezivanja.
4. Paket za priključenje sadrži privatni ključ čvora koji nastaje na
   administratorovom računalu. Alternativa bez toga je pozivnica: čvor sam
   napravi ključ, a potvrdu mu na sastajalištu izda nositelj ključa mreže kad
   je na vezi. To traži protokol iz 0.0.29 i čekanje na laptop, pa je
   prijedlog najprije paket, a pozivnica uz 0.0.29.
5. Biblioteka na GitHubu ili na čvoru? Kad bude copB.voda.hr, isti katalog
   može služiti i on, bez treće strane. Format i čitanje ostaju isti, mijenja
   se samo poveznica.
