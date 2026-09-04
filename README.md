# goCOP — Centar obrane od poplava

Operativni program za obranu od poplava Hrvatskih voda: registri štićenih
dionica, vodomjernih postaja i vodnih tijela, djelatnici i zaduženja,
teritorijalne jedinice. Radi i bez interneta; kopije na različitim
računalima se same usklađuju.

> **Status: alfa (0.0.x), za testiranje i daljnji razvoj.** Nije za
> operativnu upotrebu. Sve se još mijenja.
>
> Otvoreni kod, neprofitno. Za program je odgovoran Tomislav Kraljević.

Ovaj dokument je pisan za **administratore** koji program postavljaju na
prva računala. Kaže što program radi na računalu i mreži, što ne radi, i
što su poznate slabosti u ovoj fazi.

## Što već radi

- **Registri:** 465 štićenih dionica, 245 vodomjernih postaja, 442 vodna
  tijela, teritorijalne jedinice te djelatnici i njihova zaduženja. Registri
  imaju pretraživanje, detalje, unos i uređivanje, uz ovlasti prema području
  odgovornosti.
- **Povezani podaci:** dionice su povezane s mjerodavnim vodomjerima,
  vodotocima i pripadajućim županijama, gradovima, općinama i naseljima.
- **Rad bez interneta:** cijeli program, sučelje i podaci rade lokalno;
  sučelje je prilagođeno i radu na mobitelu.
- **Usklađivanje računala:** čvorovi se pronalaze u lokalnoj mreži, ručno
  uparuju i razmjenjuju podatke šifriranom i obostrano autentificiranom vezom.
- **Povijest:** svaka izmjena ostaje zabilježena kao nova verzija, a brisanje
  je arhiviranje. Administratori mogu provjeriti ovlasti pogledom kroz račun
  drugog djelatnika, bez mogućnosti izmjene u tom načinu rada.

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
  | `gocop.db` (+ `-wal`, `-shm`) | SQLite baza — svi podaci |
  | `gocop.toml` | postavke, s komentarima; program je zapiše pri prvom pokretanju |
  | `node-key` | privatni ključ ovog računala (Ed25519), prava 0600 |

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

- **Podaci su na računalu**, u jednoj SQLite datoteci. Ne šalju se nikamo
  osim na uparena računala.
- **Osobni podaci.** Registar djelatnika sadrži imena, funkcije, telefone i
  e-mail adrese djelatnika Hrvatskih voda i sudionika obrane od poplava —
  iz službenog imenika COP-ova. Tretirati mapu `data/` kao takvu.
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
- **Ništa se ne briše.** Svaka izmjena je nova verzija iznad prethodne;
  brisanje je arhiviranje. Povijest ostaje i može se vratiti.

## 4. Poznata ograničenja alfa faze — pročitati prije odobrenja

1. **Izvršni file još nije potpisan.** Windows SmartScreen će upozoriti, a
   neki antivirusi označe nepotpisane Go programe. Potpisivanje besplatnim
   certifikatom za otvoreni kod je u planu prije bete. Do tada: provjeriti
   SHA-256 izdanja i dopustiti ručno.
2. **Nema CSRF tokena** za obrasce i druge zahtjeve koji mijenjaju podatke.
3. **Pronalaženje preko interneta** (stalno izložena računala s domenom)
   još ne radi — samo lokalna mreža i ručni upis adrese.
4. **Podaci su testni.** Registri su iz službene dokumentacije, ali sve što
   se unese u alfi može se izgubiti pri promjeni sheme između verzija.

## 5. Preporuka za prva računala

- Dva do tri računala u istoj lokalnoj mreži, unutar mreže Hrvatskih voda,
  bez izlaganja na internet.
- Jedan administrator upravlja uparivanjem (⚙️ Postavke) i lozinkama.
- Provjeriti SHA-256 preuzetog izdanja prema `SHA256SUMS` uz izdanje.
- Program pokretati kao običan korisnik, iz vlastite mape.

## 6. Pokretanje

```
gocop.exe            (Windows)
./gocop              (Linux, macOS)
```

Prvo pokretanje napuni bazu iz ugrađene dokumentacije (nekoliko sekundi)
i zapiše `data/gocop.toml`. Otvoriti `http://localhost` (ili
`http://localhost:8080`). Postavke čvora, uparivanje i sinkronizacija su pod
**⚙️ Postavke** u korisničkom izborniku.

`gocop.toml` — adresa web sučelja, putanja baze, identifikator i naziv
računala, portovi, razmak automatske sinkronizacije, stalno izložene
domene. Zastavice na naredbenom retku (`-addr`, `-db`, `-node`, `-name`,
`-sync-port`, `-pair-port`, `-discovery-port`, `-auto-sync`, `-config`)
imaju prednost pred datotekom.

## 7. Verzije

| oznaka | značenje |
|---|---|
| `0.0.x` | **alfa** — razvoj, sve se mijenja |
| `0.x.0` | **beta** — oblik je stabilan, provjerava se na terenu |
| `x.0.0` | **stabilno** — u operativnoj upotrebi |

## 8. Za razvoj

Go, bez CGO-a; SQLite (modernc), sučelje `html/template` ugrađeno u binary.

```bash
go build -ldflags "-X main.version=0.0.1" -o gocop ./cmd/gocop
go test ./...
```

Sinkronizacijski transport (ključevi, uparivanje, TLS razmjena, LAN
pronalaženje) je zasebna biblioteka
[syncnet](https://github.com/tkraljevic/syncnet), zajednička s projektom
goEMM. Registri se pune iz Privitka 1 (Teritorijalne jedinice za izravnu
provedbu mjera obrane od poplava, HV 2018./2022.) i Odluke o popisu voda
I. reda (NN 79/2010). Kote nule vodomjera su u sustavu Trst; HVRS71 kote
se tek mjere.

Program razvija Tomislav Kraljević, uz pomoć kolega iz Hrvatskih voda.
Kao i uređivač koda i drugi razvojni alati, u radu se koriste i alati
umjetne inteligencije; sav kod prolazi ručni pregled i automatske testove
prije nego što uđe u program, a odgovornost za njega je isključivo ljudska.

## 9. Suradnici i doprinosi

Program nastaje uz pomoć kolega iz Hrvatskih voda. Popis raste kako se
posao širi.

- **Petar Završki, mag. ing. aedif.** — prvotno prikupio kontakte
  zaposlenika koji su bili dostupni u bazi; iz toga je nastao registar
  djelatnika.
- **Marko Stević, mag. ing. aedif.** — digitalizira dnevnike obrane od
  poplava Centra obrane od poplava Osijek od 2005. godine; ti dnevnici su
  povijesna građa za modul dnevnika i rješenja.

### Evidencija VGI Baranja (app.bp16.xyz)

Očitanja vodostaja te stanja crpnih stanica i ustava branjenog područja 16
(Baranja) od 2013. do 2026. uvezena su iz evidencije koju je Tomislav
Kraljević vodio na privatnom poslužitelju (app.bp16.xyz) i koju je VGI
Baranja punila svako jutro. Zahvaljujući tim ljudima goCOP od prvog dana
ima trinaest godina povijesti vodostaja:

- **unos u evidenciju:** Ivana Bukić, Krunoslav Ćosić, Maja Ivančić Bukić,
  Matej Krstić, Izabela Rukavina
- **očitanja na terenu:** Marko Blagus, Željko Brdar, Igor Čulin,
  Matej Drventić, Ivica Golubov, Vlatko Hostić, Igor Ivančić, Janoš Kištot,
  Mirko Lazar, Damir Lešković, Željko Marijanov, Adam Matijević,
  Marko Rajčević, Dražen Sabljak, Friedrich Seitz, Marko Šašlin, Ante Ursić

## Podaci koji nisu u repozitoriju

Uz program u repozitoriju stoje registri koji su javni: dionice, branjena
područja, vodomjerne postaje s pragovima obrane, vodotoci i teritorijalne
jedinice. Dvoje ovdje namjerno nema:

- **imenik djelatnika** — osobni podaci; čita se iz `data/imenik.json` uz
  bazu, samo pri prvom punjenju čvora;
- **očitanja vodostaja** — dio podataka dolazi od Državnog
  hidrometeorološkog zavoda, koji ih s Hrvatskim vodama dijeli po ugovoru o
  uzajamnom korištenju, pa se ne objavljuju. Povijest vodostaja stoji samo
  na čvorovima Hrvatskih voda; program je zna uvesti iz datoteke uz bazu i
  razmijeniti s drugim čvorovima mreže, ali je ne nosi u sebi.

Zbog toga svaki uvoz očitanja ide iz datoteke koja stoji uz bazu, nikad iz
`internal/db`, jer se sve odande ugrađuje u program. Test to i provjerava.

## Licenca

goCOP je otvoreni, neprofitni projekt namijenjen Hrvatskim vodama i drugim
vodoprivrednim organizacijama kojima je primjenjiv. Program je licenciran
pod European Union Public Licence, verzija 1.2 (EUPL-1.2). Tekst licence
je u datoteci `LICENSE`; sve jezične inačice EUPL-a, uključujući hrvatsku,
jednako su vjerodostojne.

Nositelj autorskih prava na program: Hrvatske vode.
Program je osmislio i izgradio Tomislav Kraljević; to navođenje je uvjet
korištenja i ostaje u svakoj izvedenici.

Podaci i grafički znakovi ugrađeni u program imaju vlastito podrijetlo i
prava, opisana u datoteci `NOTICE`.
