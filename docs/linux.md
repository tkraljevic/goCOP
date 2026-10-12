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
u sebi. Postupak je isproban na Ubuntu 24.04 LTS (uredski čvor u Osijeku).

### Prije instalacije: slobodni portovi

Čvor treba port 80 (web) i portove 4710–4712 (razmjena). Provjerite da ih
ne drži neki drugi program:

```sh
sudo ss -ltnup | grep -E ':(80|8080|4710|4711|4712)\b'
```

Ako naredba ništa ne ispiše, portovi su slobodni. Na računalu koje je prije
služilo kao poslužitelj port 80 često drži drugi web poslužitelj (nginx,
Apache). Ako vam ne treba, spremite mu postavke i uklonite ga, npr. za nginx
na Ubuntuu:

```sh
sudo tar czf ~/nginx-kopija.tgz /etc/nginx /var/www
sudo systemctl disable --now nginx
sudo apt purge -y nginx nginx-common nginx-core
sudo apt autoremove -y
```

Ako vam treba, čvor će sam raditi na 8080 (u dnevniku piše `Port 80 nije
dostupan … prelazim na :8080`), ili mu port zadajte u `gocop.toml`
(poglavlje 6).

## 1. Što se postavlja

| Što | Gdje | Napomena |
|---|---|---|
| program | `/usr/local/bin/gocop` | vlasnik root, `0755` |
| korisnik sustava | `gocop` | bez prijave i bez lozinke |
| mapa podataka | `/var/lib/gocop` | vlasnik `gocop`, `0750` |
| usluga | `/etc/systemd/system/gocop.service` | `gocop.service` iz izdanja |
| ikona u traci (Postava) | `/usr/local/bin/gocop-postava` | samo ako je uz program (poglavlje 12) |

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

Uz svako izdanje na GitHubu stoje program, instalacijska skripta
`instaliraj.sh`, jedinica `gocop.service`, `SHA256SUMS` i `SHA256SUMS.sig`
(potpis datoteke `SHA256SUMS` ključem izdanja). `SHA256SUMS` nosi i skriptu i
jedinicu, pa ih pokriva isti potpis kao i program. Datoteke se preuzimaju
zasebno, u praznu mapu (oznaku izdanja zamijenite onom koju postavljate):

```sh
IZDANJE=v0.0.36-alfa
mkdir -p ~/gocop-$IZDANJE && cd ~/gocop-$IZDANJE
for f in gocop-linux-amd64 gocop-postava-linux-amd64 instaliraj.sh gocop.service SHA256SUMS SHA256SUMS.sig; do
  curl -fLO "https://github.com/tkraljevic/goCOP/releases/download/$IZDANJE/$f"
done
```

Skripta iz poglavlja 3 sama provjerava potpis te SHA-256 programa i
jedinice. Samu skriptu provjerite prije pokretanja, ovom ručnom provjerom:

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

Ispravno izdanje ispiše `Signature Verified Successfully` te
`gocop-linux-amd64: OK`, `gocop.service: OK` i `instaliraj.sh: OK` (uz
Postavu i `gocop-postava-linux-amd64: OK`). **Ako bilo koja provjera ne
prođe, ništa od izdanja ne postavljajte.**

Skripta i jedinica stoje uz izdanja od prvog izdanja nakon 0.0.33-alfa, a
Postava za Linux (`gocop-postava-linux-amd64`, poglavlje 12) od 0.0.36-alfa.
Starija izdanja ih nemaju; za njih vrijedi ručni postupak na kraju
poglavlja 3, a jedinica se uzima iz repozitorija za istu oznaku
(`build/linux/gocop.service`), bez potpisa.

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

Skripta i jedinica stižu uz izdanje (poglavlje 2) i u repozitoriju su u
`build/linux/`. Kratke su, pa ih vrijedi pregledati prije pokretanja. Iz mape
s preuzetim izdanjem:

```sh
sudo bash instaliraj.sh -ime pperic-posluzitelj ./gocop-linux-amd64
```

Skripta se ne pokreće kroz cjevovod (`curl … | sh`): preuzima se, čita i tek
onda pokreće.

Što skripta radi:

1. provjeri potpis `SHA256SUMS` ključem izdanja te SHA-256 programa i
   jedinice (bez valjanog potpisa ne radi ništa). Provjerava i postavlja
   kopije u privremenoj mapi, pa se datoteka ne može zamijeniti između
   provjere i postavljanja;
2. stvori korisnika sustava `gocop`, ako ga nema;
3. postavi program u `/usr/local/bin/gocop`; ako je ondje drugi program,
   najprije zaustavi uslugu i kopira `gocop.db` (i `gocop.db-wal`) u
   `/var/lib/gocop/kopije` (zadržava zadnje tri kopije);
4. upiše ime čvora u `/var/lib/gocop/gocop.toml` naredbom
   `gocop -pripremi` — ime koje je već upisano ne mijenja, a uz postojeću
   bazu bez `gocop.toml` ne zapisuje ništa, nego javlja da datoteke nema;
5. postavi jedinicu `gocop.service` iz izdanja, uključi je i pokrene;
6. pričeka da čvor odgovori na `/zdravlje` i ispiše adresu.

Kad je uz program i `gocop-postava-linux-amd64`, skripta prije čvora
postavi i ikonu u traci (poglavlje 12). Bez te datoteke Postavu ne dira.

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

`gocop -pripremi` samo zapiše `gocop.toml` s imenom čvora, a bazu najviše
pročita; pokreće se iz mape podataka jer program `gocop.toml` traži i u
radnoj mapi. Kad baza već postoji, a `gocop.toml` nema (premješten ili
izgubljen), od 0.0.35-alfa ne zapisuje novi primjer, nego ispiše gdje se
datoteka očekuje i pod kojim imenom čvor bez nje kreće (ime pod kojim baza
zna ključ čvora); izvornu datoteku treba vratiti.

## 4. Usluga

Jedinica `gocop.service`:

- pokreće `/usr/local/bin/gocop -db /var/lib/gocop/gocop.db` kao korisnik
  `gocop`, s radnom mapom `/var/lib/gocop`;
- postavlja `TZ=Europe/Zagreb`, kao i spremnik: prikaz je uvijek u zoni
  Zagreba, ali neka vremena (unos epizode, vrijeme snimanja fotografije)
  čitaju se u mjesnoj zoni računala;
- ponovno pokreće program ako padne (`Restart=on-failure`) i kad ga
  administrator ponovno pokrene iz programa (izlazni kod 75);
- javlja programu da je usluga sustava (`GOCOP_NADZOR=systemd`), pa
  Administracija nudi gumb *Ponovno pokreni čvor*;
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

### Pristup na daljinu (SSH)

Ubuntu Desktop nema SSH poslužitelj. Ako se na računalo spajate s drugog
računala:

```sh
sudo apt install -y openssh-server
sudo systemctl enable --now ssh
```

Ako je na računalu Tailscale, umjesto toga se može uključiti Tailscale SSH
(`sudo tailscale set --ssh`). Tada se prijavljujete Tailscale računom, bez
lozinke i bez ključeva, preko adrese iz Tailscalea (`ssh korisnik@100.x.y.z`).
Ovisno o pravilima vaše Tailscale mreže, povremeno se traži potvrda u
pregledniku. Taj pristup radi samo preko Tailscalea, ne preko adrese u
lokalnoj mreži.

### Uključivanje u postojeću mrežu

Na stranici `/postavljanje` novog čvora (npr. s laptopa u istoj mreži,
`http://<adresa novog čvora>/postavljanje?kod=<kod>`) odaberite *Priključit
ću se postojećoj mreži* → *Poveži ga s uredom* i uparite ga s računalom koje
je već u mreži. Uparivanje ide preko porta 4711, koji poslije više ne treba
(poglavlje 7). Nakon uparivanja čvor sam počne razmjenu sa svim čvorovima
koje zna.

### Uredski čvor: svi podaci

Nov čvor prima samo ono što se u programu označi za praćenje. To odgovara
laptopu, a uredski poslužitelj treba imati sve: očitanja i dnevnike svih
područja i godina. Za to u `gocop.toml` u odjeljku `[sync]` stavite
`sve = true` i ponovno pokrenite uslugu:

```sh
sudo sed -i 's/^sve = false$/sve = true/' /var/lib/gocop/gocop.toml
sudo grep -n -E '^(sve|bootstrap)' /var/lib/gocop/gocop.toml
sudo systemctl restart gocop
```

`grep` treba ispisati `sve = true`. Pod `bootstrap` su javni čvorovi preko
kojih čvor nalazi ostale i izvan lokalne mreže (zadano `cop-osijek.com`).

Što se vidi u dnevniku (`journalctl -u gocop -f`) dok se čvor puni:

- `sinkronizacija s <čvor>: primljeno N, poslano M`. Prva razmjena ide u
  paketima od po 5000 zapisa, pa se ovakvih redaka nakupi puno;
- `arhiva: zatraženo N paketa od drugih čvorova`, a zatim po jedan redak
  `… → spojeni niz …` za svaki niz hidrološke arhive. Arhiva ima desetak GB,
  pa ovo traje dulje od ostalog;
- `database is locked` ili `EOF` dok se čvor puni: dva čvora su istodobno slala
  podatke. Ništa se ne gubi, razmjena se ponovi u sljedećem krugu (za 5
  minuta);
- `čvor … nije dostupan ni na jednoj adresi`: čvor na drugoj mreži (npr.
  kućno računalo). To je očekivano.

Ako uredski čvor treba primati nova računala u mrežu i kad računalo s
ključem mreže nije u uredu, na tom računalu u *Administracija → Čvor, mreža
i sinkronizacija* uz uredski čvor kliknite *Daj ovlast za primanje*.

**Dok čvor nije postavljen, ne smije biti dostupan s interneta.** Početni
račun `admin` s lozinkom iz uputa postoji od prvog pokretanja, a čvor tu
lozinku odbija samo za zahtjeve koji dolaze kroz posrednika (vidi
[SECURITY.md](../SECURITY.md)).

## 6. Postavke i mapa podataka

Postavke su u `/var/lib/gocop/gocop.toml`. Pri prvom pokretanju program
zapiše primjer sa zadanim vrijednostima i objašnjenjem svakog ključa.
Mapu podataka smiju čitati samo `gocop` i `root`, pa i za čitanje datoteke
treba `sudo` (`sudo grep …`, `sudo nano /var/lib/gocop/gocop.toml`).

Program postavke čita samo pri pokretanju. **Nakon izmjene obavezno:**
`sudo systemctl restart gocop`. Zastavice u jedinici imaju prednost pred
datotekom; jedinica zadaje samo `-db`.

Dio postavki mijenja se i u programu, bez `sudo` i uređivača: javna adresa
i centar sa stranice prijave te razmak sinkronizacije (*Čvor, mreža i
sinkronizacija*), `sve` i `povijest_mjeseci` (*Što ovo računalo prati*) i
karta (*Opcije*); do svih se dolazi iz Administracije (opis u
[INSTALACIJA.md](INSTALACIJA.md#6-pokretanje), *Postavke u programu*). Radi samo s ovog računala ili iz lokalne mreže, a ne kroz
tunel. Program prije spremanja provjeri novu datoteku, staru ostavi kao
`/var/lib/gocop/gocop.toml.<datum-vrijeme>` (zadnjih pet) i ne dira ručne
komentare. Naziv čvora, port weba i portove razmjene, putanje i posrednike
mijenja samo datoteka. Nakon spremanja gumb *Ponovno pokreni čvor* uredno
ugasi čvor, a systemd ga za pet sekundi pokrene, isto kao
`sudo systemctl restart gocop`. Vlastita jedinica bez `Restart=` ostavila bi
ga ugašenog.

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
- Bez ključa čvora uz bazu koja već ima zapise čvor ne kreće (od
  0.0.35-alfa) i u dnevniku (`journalctl -u gocop`) javlja koja datoteka
  nedostaje. Vratite izvornik iz kopije. Novi identitet samo izričito,
  jednim pokretanjem sa zastavicom `-novi-kljuc-cvora`, kao korisnik
  `gocop` i uz istu bazu (naredba pokrenuta kao `root` ostavila bi ključ
  koji usluga ne može pročitati):

  ```sh
  sudo systemctl stop gocop
  cd /var/lib/gocop && sudo runuser -u gocop -- /usr/local/bin/gocop \
    -novi-kljuc-cvora -db /var/lib/gocop/gocop.db
  # kad javi da je poslužitelj spreman (bez prava na port 80, na :8080): Ctrl+C
  sudo systemctl start gocop
  ```

  Zatim ga čvor koji ga je primio u mrežu zaboravi, opozove mu članstvo i
  ponovno ga primi (vidi [INSTALACIJA.md](INSTALACIJA.md), poglavlje 3).

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

Najprije provjerite je li vatrozid uopće uključen:

```sh
sudo ufw status
```

Ako piše `Status: inactive` (zadano na Ubuntu Desktopu), portovi su već
otvoreni prema mreži na kojoj je računalo, a pravila ispod nisu potrebna.
Zato tada `ufw delete` javlja `Could not delete non-existent rule`. Ako
vatrozid uključujete, **prvo dopustite pristup kojim ste spojeni**, inače
će vas `ufw enable` izbaciti: `sudo ufw allow OpenSSH`, a za Tailscale
`sudo ufw allow in on tailscale0`.

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

`gocop -version` ispiše npr. `goCOP 0.0.33-alfa`. `/zdravlje` vrati
`{"izdanje":"0.0.33-alfa","radi":true}`, ali **samo za zahtjev s istog
računala** (127.0.0.1 ili ::1) koji ne dolazi kroz posrednika; za sve
ostale vrati 404. To je namjerno: služi provjeri na samom računalu, ne
nadzoru izvana. Ako je program prešao na 8080, adresa je
`http://127.0.0.1:8080/zdravlje`.

## 9. Ažuriranje

1. Preuzmite novo izdanje u novu mapu i provjerite ga (poglavlje 2).
2. Pokrenite skriptu tog izdanja s novim programom:

   ```sh
   sudo bash instaliraj.sh ./gocop-linux-amd64
   ```

   Skripta provjeri potpis, zaustavi uslugu, kopira `gocop.db` u
   `/var/lib/gocop/kopije`, zamijeni program, pokrene uslugu i pričeka
   `/zdravlje`. Ako je novo izdanje promijenilo jedinicu, postavi i nju.

Uz ikonu u traci isto se radi iz njezina izbornika, *Nadogradi na …*
(poglavlje 12).

Napomene:

- Stalni čvor ažurira se prvi, pa ostali (redoslijed je u INSTALACIJA.md).
- Program pri pokretanju sam prilagodi bazu novom izdanju. Povratak na
  starije izdanje zato nije podržan: tko ga ipak treba, zaustavi uslugu,
  ručno vrati stari program i kopiju baze iz `kopije/` (sve promjene nakon
  kopije se gube).
- Skripta kopira samo `gocop.db`; za ostale baze vrijedi sigurnosna
  kopija iz poglavlja 6.
- `gocop` se ne nadograđuje sam i ništa ne preuzima; nadogradnja je uvijek
  ovaj postupak, ručno ili iz ikone u traci.

## 10. Deinstalacija

```sh
sudo bash instaliraj.sh -ukloni
```

Zaustavi i isključi uslugu, ukloni jedinicu i program, a uz njih i
Postavu, unos u izborniku i ikonu. **Podatke i korisnika `gocop` ne
briše.** Ako ih više ne trebate (nakon kopije):

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
- Postava za Linux dolazi uz izdanje goCOP-a i postavlja je skripta
  (poglavlje 12). Zasebna izdanja Postave (`postava-v…`), s instalacijskim
  programom, su samo za Windows.
- Program preveden iz izvora skripta ne postavlja jer nema potpisa
  izdanja; za njega vrijedi ručni postupak iz poglavlja 3 (prijevod s
  `CGO_ENABLED=0 go build -trimpath ./cmd/gocop`).

## 12. Ikona u traci (Postava)

Postava je mali program s ikonom goCOP-a u traci, isti kao na Windowsu i
macOS-u. Uz izdanje stoji kao `gocop-postava-linux-amd64` (od
0.0.36-alfa). Kad je u mapi uz program, `instaliraj.sh` je provjeri istim
potpisom kao i program i postavi:

| Što | Gdje |
|---|---|
| Postava | `/usr/local/bin/gocop-postava` |
| unos u izborniku programa (goCOP) | `/usr/share/applications/gocop-postava.desktop` |
| ikona | `/usr/share/icons/hicolor/256x256/apps/gocop.png` |

U izborniku programa zove se **goCOP**. Pokreće se kao obični korisnik, ne
kao root. Kad nađe uslugu `gocop`, ne pokreće drugi čvor, nego radi s
uslugom. Pri prvom pokretanju sama uključi *Pokreni pri prijavi* (unos u
`~/.config/autostart`), samo jednom: ako ga korisnik poslije isključi, ostaje
isključeno. Svoj dnevnik piše u `~/.local/share/gocop/data/postava.log`.

Izbornik ikone:

- **Otvori goCOP** otvara goCOP u pregledniku (isto radi dvoklik na ikonu).
- **Zaustavi** zaustavlja uslugu, nakon pitanja: dok usluga stoji, nema
  razmjene s drugim čvorovima i nitko u uredu ne može otvoriti goCOP.
  **Pokreni** je pokreće bez dodatnog pitanja.
- **Nadogradi na …** preuzme novo izdanje s GitHuba u
  `~/.local/share/gocop/data`, provjeri potpis i SHA-256 svake datoteke i
  pokrene `instaliraj.sh` tog izdanja (poglavlje 9). Ispis skripte ide u
  dnevnik Postave. **Provjeri nadogradnje** odmah pita GitHub; inače Postava
  pita sama, svakih šest sati.
- **Dnevnik čvora** otvara zadnjih 2000 redaka iz `journalctl -u gocop`.
  Dnevnik sustava smiju čitati članovi grupe `adm` ili `systemd-journal`
  (prvi korisnik na Ubuntuu je u grupi `adm`); za ostale je prazan.
- **Pokreni pri prijavi** uključuje ili isključuje ikonu pri prijavi.
- **O programu** pokazuje izdanje Postave i čvora.
- **Izlaz** zatvara samo ikonu. Usluga radi dalje.

*Otvori mapu s podacima* uz uslugu se ne nudi, jer `/var/lib/gocop` smije
čitati samo korisnik `gocop`.

**Lozinka administratora.** Zaustavi, Pokreni i Nadogradi mijenjaju uslugu
sustava, pa sustav za njih traži lozinku administratora u svom prozoru
(polkit). Nadogradnja pita jednom, za cijelu skriptu (`pkexec`). Pri
prijavi Postava ne pita ništa i uslugu ne pali: to radi systemd pri
pokretanju računala, a uslugu koju je netko zaustavio pali samo *Pokreni*.
Ako sustav nema `pkexec`-a ili
se lozinka ne upiše, goCOP se nadograđuje ručno (poglavlje 9). Postavu koju
je donijela nadogradnja ikona koristi od sljedećeg pokretanja, npr. nakon
sljedeće prijave.

**GNOME (Ubuntu).** Ubuntu ikonu pokazuje preko ugrađenog proširenja
AppIndicator (*Ubuntu AppIndicators*), koje je uključeno samo od sebe. Na
GNOME-u bez njega (npr. Fedora) treba uključiti proširenje *AppIndicator and
KStatusNotifierItem Support*. KDE i većina drugih radnih okruženja ikonu
pokazuju bez dodataka.

Bez usluge (na računalu bez `gocop.service`) Postava na Linuxu radi kao na
macOS-u: pokreće vlastiti čvor u `~/.local/share/gocop`.
