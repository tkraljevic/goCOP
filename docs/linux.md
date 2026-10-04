# goCOP čvor na Linuxu

Administratorske upute za stalni čvor na Linux poslužitelju, kao usluga
sustava (systemd). Opće upute (mreža, podaci, ograničenja alfa faze) su u
[INSTALACIJA.md](INSTALACIJA.md), a model sigurnosti u
[SECURITY.md](../SECURITY.md).

> **Status: alfa, za testiranje i daljnji razvoj.** Nije za operativnu
> upotrebu.

## Što treba

- **Linux na procesoru x86_64 (amd64).** Izdanja za Linux postoje samo za
  amd64 (`gocop-linux-amd64`); za ARM (npr. Raspberry Pi) program se prevodi
  iz izvora.
- **systemd** i uobičajeni alati sustava (`useradd`, `runuser`,
  `sha256sum`, `curl` ili `wget`).
- **OpenSSL 3 ili noviji** za provjeru potpisa izdanja (Ubuntu 22.04+,
  Debian 12+ i noviji sustavi ga imaju): `openssl version`.

Program je jedna izvršna datoteka bez drugih ovisnosti; bazu (SQLite) nosi
u sebi.

## 1. Što se postavlja

| Što | Gdje | Napomena |
|---|---|---|
| program | `/usr/local/bin/gocop` | vlasnik root, `0755` |
| korisnik sustava | `gocop` | bez prijave i bez lozinke |
| mapa podataka | `/var/lib/gocop` | vlasnik `gocop`, `0750` |
| usluga | `/etc/systemd/system/gocop.service` | iz `build/linux/gocop.service` |

U mapi podataka su:

| Datoteka ili mapa | Što je |
|---|---|
| `gocop.db` (uz `-wal` i `-shm`) | glavna baza |
| `sadrzaj.db`, `prognoze.db`, `oborine.db` | spremište privitaka, prognoze, kiša |
| `vodostaji.db` | hidrološka arhiva (može narasti na desetak GB) |
| `node-key` | ključ čvora (`0600`) — identitet ovog računala na mreži |
| `network-key` | ključ mreže (`0600`), samo na čvoru koji je osnovao mrežu |
| `gocop.toml` | postavke čvora |
| `vodostaji/`, `pakete/`, `skenovi/` | izvorne datoteke arhive, `.cop` paketi, skenovi prijava |
| `kopije/` | kopije baze koje skripta napravi prije nadogradnje |

Program sve putanje računa od mape baze, pa je dovoljno da usluga dobije
`-db /var/lib/gocop/gocop.db` i da joj je ta mapa radna mapa.

## 2. Preuzimanje i provjera izdanja

Uz svako izdanje na GitHubu stoje program, `SHA256SUMS` i `SHA256SUMS.sig`
(potpis datoteke `SHA256SUMS` ključem izdanja). Sve tri datoteke se
preuzimaju zasebno, u praznu mapu (oznaku izdanja zamijenite onom koju
postavljate):

```sh
IZDANJE=v0.0.31-alfa
mkdir -p ~/gocop-$IZDANJE && cd ~/gocop-$IZDANJE
for f in gocop-linux-amd64 SHA256SUMS SHA256SUMS.sig; do
  curl -fLO "https://github.com/tkraljevic/goCOP/releases/download/$IZDANJE/$f"
done
```

Skripta iz poglavlja 3 provjerava potpis i SHA-256 sama. Ručna provjera:

```sh
cat > kljuc-izdanja.pem <<'KLJUC'
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAqFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=
-----END PUBLIC KEY-----
KLJUC
{ printf 'goCOP izdanje v1\n'; cat SHA256SUMS; } > poruka
base64 -d SHA256SUMS.sig > potpis
openssl pkeyutl -verify -pubin -inkey kljuc-izdanja.pem -rawin -in poruka -sigfile potpis
sha256sum -c --ignore-missing SHA256SUMS
```

Ispravno izdanje ispiše `Signature Verified Successfully` i
`gocop-linux-amd64: OK`. **Ako bilo koja provjera ne prođe, program ne
postavljajte.**

Potpisuje se tekst `goCOP izdanje v1` (s prelaskom u novi red) i odmah iza
njega sadržaj `SHA256SUMS`. Javni ključ izdanja je u kodu, u
`internal/izdanje/izdanje.go` (`JavniKljucevi`, base64). PEM iznad je isti
ključ u obliku za OpenSSL; tko ga ne želi prepisati iz uputa, složi ga sam
iz ključa u kodu:

```sh
{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'
  printf '%s' 'qFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=' | base64 -d
} | openssl pkey -pubin -inform DER
```

## 3. Instalacija skriptom

Skripta `build/linux/instaliraj.sh` i jedinica `build/linux/gocop.service`
su u repozitoriju, ne uz izdanje. Preuzmite ih iz repozitorija za istu
oznaku izdanja, pregledajte (kratke su) i stavite u istu mapu:

```sh
for f in instaliraj.sh gocop.service; do
  curl -fLO "https://raw.githubusercontent.com/tkraljevic/goCOP/$IZDANJE/build/linux/$f"
done
chmod +x instaliraj.sh
sudo ./instaliraj.sh -ime pperic-posluzitelj ./gocop-linux-amd64
```

Skripta se ne pokreće kroz cjevovod (`curl … | sh`): preuzima se, čita i tek
onda pokreće.

Što skripta radi:

1. provjeri potpis `SHA256SUMS` ključem izdanja i SHA-256 programa (bez
   valjanog potpisa ne radi ništa);
2. stvori korisnika sustava `gocop`, ako ga nema;
3. postavi program u `/usr/local/bin/gocop`; ako je ondje drugi program,
   najprije zaustavi uslugu i kopira `gocop.db` (i `gocop.db-wal`) u
   `/var/lib/gocop/kopije` (zadržava zadnje tri kopije);
4. upiše ime čvora u `/var/lib/gocop/gocop.toml` naredbom
   `gocop -pripremi` — ime koje je već upisano ne mijenja;
5. postavi jedinicu `gocop.service`, uključi je i pokrene;
6. pričeka da čvor odgovori na `/zdravlje` i ispiše adresu.

**Ime čvora** (`-ime`) je jedinstveno ime u mreži: mala slova, brojke i
crtica, 3–40 znakova (npr. `pperic-posluzitelj`). Ne mijenja se nakon prvog
pokretanja. Bez `-ime` program uzme ime računala s četiri nasumična znaka.

Skripta se smije pokretati više puta: s istim programom ne mijenja ništa,
s novim je nadogradnja (poglavlje 9).

### Ručno, bez skripte

Isto što radi skripta (nakon provjere iz poglavlja 2):

```sh
sudo useradd --system --user-group --home-dir /var/lib/gocop --no-create-home \
  --shell /usr/sbin/nologin gocop
sudo install -d -o gocop -g gocop -m 0750 /var/lib/gocop
sudo install -o root -g root -m 0755 gocop-linux-amd64 /usr/local/bin/gocop
cd /var/lib/gocop && sudo runuser -u gocop -- /usr/local/bin/gocop \
  -pripremi -db /var/lib/gocop/gocop.db -node pperic-posluzitelj
sudo install -m 0644 gocop.service /etc/systemd/system/gocop.service
sudo systemctl daemon-reload
sudo systemctl enable --now gocop
```

`gocop -pripremi` samo zapiše `gocop.toml` s imenom čvora i ne otvara bazu;
pokreće se iz mape podataka jer program `gocop.toml` traži i u radnoj
mapi.

## 4. Usluga

Jedinica `gocop.service`:

- pokreće `/usr/local/bin/gocop -db /var/lib/gocop/gocop.db` kao korisnik
  `gocop`, s radnom mapom `/var/lib/gocop`;
- postavlja `TZ=Europe/Zagreb`, kao i spremnik: prikaz je uvijek u zoni
  Zagreba, ali neka vremena (unos epizode, vrijeme snimanja fotografije)
  čitaju se u mjesnoj zoni računala;
- ponovno pokreće program ako padne (`Restart=on-failure`);
- daje programu samo pravo vezanja na port 80
  (`CAP_NET_BIND_SERVICE`); bez njega bi program prešao na 8080;
- ostatak sustava drži samo za čitanje (`ProtectSystem=strict`,
  `ProtectHome`, `PrivateTmp`, `NoNewPrivileges` i druga ograničenja);
  pisati može samo u `/var/lib/gocop` i privremenu mapu.

Zastavica `-upravitelj` se pod systemd-om ne koristi: s njom se čvor gasi
čim mu se zatvori standardni ulaz, a pod systemd-om je ulaz prazan.

Izmjene jedinice ne upisuju se u `/etc/systemd/system/gocop.service`
(skripta je pri nadogradnji vraća), nego u dopunu:

```sh
sudo systemctl edit gocop
```

Svakodnevno:

```sh
systemctl status gocop
journalctl -u gocop -f          # dnevnik čvora
sudo systemctl restart gocop    # nakon izmjene gocop.toml
```

Program piše dnevnik samo na standardni izlaz, pa je cijeli dnevnik u
`journalctl`.

## 5. Prvo postavljanje

Nov čvor nema administratora ni mrežu. Dok je takav, svako pokretanje u
dnevnik upiše redak `Postavljanje: svjež čvor …` s jednokratnim kodom
(skripta ga ispiše na kraju):

```sh
journalctl -u gocop | grep Postavljanje
```

Na stranici `/postavljanje` postavlja se vlastiti administrator (početni
račun `admin` se tada isključuje) i osniva nova mreža, ili se čvor uparuje
s postojećom mrežom.

- **S tog računala** stranica radi bez koda. Na poslužitelju bez preglednika
  isto se postiže SSH tunelom s vlastitog računala:

  ```sh
  ssh -L 8080:127.0.0.1:80 pperic@posluzitelj
  ```

  pa u pregledniku `http://localhost:8080/postavljanje`.
- **Iz lokalne mreže:** `http://<adresa poslužitelja>/postavljanje?kod=<kod iz dnevnika>`.
  Nakon 10 krivih kodova kod ne vrijedi do ponovnog pokretanja.

**Dok čvor nije postavljen, ne smije biti dostupan s interneta.** Početni
račun `admin` s lozinkom iz uputa postoji od prvog pokretanja, a čvor tu
lozinku odbija samo za zahtjeve koji dolaze kroz posrednika (vidi
[SECURITY.md](../SECURITY.md)).

## 6. Postavke i mapa podataka

Postavke su u `/var/lib/gocop/gocop.toml`. Pri prvom pokretanju program
zapiše primjer sa zadanim vrijednostima i objašnjenjem svakog ključa.
Nakon izmjene: `sudo systemctl restart gocop`. Zastavice u jedinici imaju
prednost pred datotekom; jedinica zadaje samo `-db`.

Česte izmjene:

- **drugi port weba:** `addr = ':8080'`;
- **čvor iza posrednika** (Cloudflare tunel, nginx): `[web]
  pouzdani_posrednici` i `zaglavlje_klijenta` (opisani u INSTALACIJA.md);
- **portovi razmjene:** `[sync] exchange_port`, `pair_port`,
  `discovery_port` (0 isključuje).

### Arhiva na drugom disku

Hidrološka arhiva može biti velika. Ako je na drugom disku, usluga u tu
mapu mora smjeti pisati. U dopuni jedinice (`sudo systemctl edit gocop`):

```ini
[Service]
ReadWritePaths=/srv/gocop-arhiva
```

a u `gocop.toml`:

```toml
arhiva = '/srv/gocop-arhiva/vodostaji.db'
podaci = '/srv/gocop-arhiva/vodostaji'
pakete = '/srv/gocop-arhiva/pakete'
skenovi = '/srv/gocop-arhiva/skenovi'
```

Mapa mora pripadati korisniku `gocop` (`sudo chown -R gocop:gocop
/srv/gocop-arhiva`). SQLite baze ne smiju biti na mrežnoj ni FUSE mapi.

### Sigurnosna kopija

- Zaustavite uslugu (`sudo systemctl stop gocop`) i kopirajte cijelu mapu
  `/var/lib/gocop`; datoteka `.db` nikad se ne kopira bez pripadne `-wal`.
- Ključ čvora (`node-key`) i ključ mreže (`network-key`) čuvajte posebno i
  zaštićeno: tko ih ima, predstavlja se kao ovaj čvor, odnosno prima
  članove u mrežu.
- Dva čvora nikad ne smiju raditi s istim ključem čvora.

## 7. Portovi i vatrozid

| Port | Protokol | Namjena | Otvoriti |
|---|---|---|---|
| 80 (ili 8080) | TCP | web sučelje i tunel razmjene (`/razmjena/tunel`) | korisnicima u lokalnoj mreži |
| 4710 | TCP | izravna razmjena s čvorovima (TLS 1.3) | samo čvorovima mreže |
| 4711 | TCP | uparivanje (TLS 1.3) | samo dok traje uparivanje |
| 4712 | UDP | pronalaženje čvorova u lokalnoj mreži (broadcast) | samo lokalnoj mreži |

Web sučelje je obični HTTP. Pristup s interneta ide kroz tunel ili
posrednika s HTTPS-om ispred čvora, nikad izravno na port 80. Čvorovi koji
razmjenjuju kroz web tunel ne trebaju otvoren 4710.

Primjer s **ufw**, za lokalnu mrežu `192.168.1.0/24`:

```sh
sudo ufw allow from 192.168.1.0/24 to any port 80 proto tcp
sudo ufw allow from 192.168.1.0/24 to any port 4710 proto tcp
sudo ufw allow from 192.168.1.0/24 to any port 4712 proto udp
# samo dok traje uparivanje:
sudo ufw allow from 192.168.1.0/24 to any port 4711 proto tcp
sudo ufw delete allow from 192.168.1.0/24 to any port 4711 proto tcp
```

Primjer s **firewalld**:

```sh
sudo firewall-cmd --permanent --zone=internal --add-source=192.168.1.0/24
sudo firewall-cmd --permanent --zone=internal --add-port=80/tcp --add-port=4710/tcp --add-port=4712/udp
sudo firewall-cmd --reload
# samo dok traje uparivanje (bez --permanent, nestaje s ponovnim učitavanjem):
sudo firewall-cmd --zone=internal --add-port=4711/tcp
```

## 8. Provjera rada

```sh
gocop -version
curl -s http://127.0.0.1/zdravlje
```

`gocop -version` ispiše npr. `goCOP 0.0.31-alfa`. `/zdravlje` vrati
`{"izdanje":"0.0.31-alfa","radi":true}`, ali **samo za zahtjev s istog
računala** (127.0.0.1 ili ::1) koji ne dolazi kroz posrednika; za sve
ostale vrati 404. To je namjerno: služi provjeri na samom računalu, ne
nadzoru izvana. Ako je program prešao na 8080, adresa je
`http://127.0.0.1:8080/zdravlje`.

## 9. Ažuriranje

1. Preuzmite novo izdanje u novu mapu (poglavlje 2), zajedno sa skriptom i
   jedinicom za istu oznaku.
2. Pokrenite skriptu s novim programom:

   ```sh
   sudo ./instaliraj.sh ./gocop-linux-amd64
   ```

   Skripta provjeri potpis, zaustavi uslugu, kopira `gocop.db` u
   `/var/lib/gocop/kopije`, zamijeni program, pokrene uslugu i pričeka
   `/zdravlje`.

Napomene:

- Stalni čvor ažurira se prvi, pa ostali (redoslijed je u INSTALACIJA.md).
- Program pri pokretanju sam prilagodi bazu novom izdanju. Povratak na
  starije izdanje zato nije podržan: tko ga ipak treba, zaustavi uslugu,
  ručno vrati stari program i kopiju baze iz `kopije/` (sve promjene nakon
  kopije se gube).
- Skripta kopira samo `gocop.db`; za ostale baze vrijedi sigurnosna
  kopija iz poglavlja 6.
- `gocop` se ne nadograđuje sam i ništa ne preuzima; nadogradnja je uvijek
  ovaj postupak.

## 10. Deinstalacija

```sh
sudo ./instaliraj.sh -ukloni
```

Zaustavi i isključi uslugu, ukloni jedinicu i program. **Podatke i
korisnika `gocop` ne briše.** Ako ih više ne trebate (nakon kopije):

```sh
sudo rm -rf /var/lib/gocop
sudo userdel gocop
```

Prije brisanja mape podataka imajte na umu da je u njoj ključ čvora: ako
čvor ostaje upisan u mreži, na drugom čvoru ga zaboravite i opozovite mu
članstvo.

## 11. Napomene

- Pri prvom pokretanju zapisani `gocop.toml` nosi zadane vrijednosti, ne
  zastavice s kojima je program pokrenut (osim imena čvora).
- Postava (program za instalaciju i nadogradnju) zasad se izdaje samo za
  Windows; na Linuxu je ovaj postupak njezina zamjena.
- Program preveden iz izvora skripta ne postavlja jer nema potpisa
  izdanja; za njega vrijedi ručni postupak iz poglavlja 3 (prijevod s
  `CGO_ENABLED=0 go build -trimpath ./cmd/gocop`).
