# Prvo pokretanje

Ova stranica vodi od preuzimanja programa do prve prijave. Sve ostalo, od
mreže i sigurnosti do nadogradnje, opisano je u
[uputama administratoru](INSTALACIJA.md).

> goCOP je u alfi: služi za razvoj i testiranje, ne za operativni rad.

Postupak ima dva koraka. Najprije se program instalira i pokrene. Zatim se
na stranici **Postavljanje** odluči je li to računalo prvo u novoj mreži ili
se priključuje postojećoj.

## 1. Instalacija

Izdanja su na GitHubu, na
[stranici izdanja (Releases)](https://github.com/tkraljevic/goCOP/releases). Uz svako izdanje
stoje programi za Windows, Linux i macOS te datoteke `SHA256SUMS` i
`SHA256SUMS.sig`, kojima se provjerava da je program izvoran.

### Windows: Postava

Postava je mali program koji instalira goCOP, pokreće ga, drži ikonu u traci
uz sat i nudi nadogradnju. Ima svoja izdanja, s oznakom `postava-v…`.

1. Preuzmite `goCOP-postava-<izdanje>.exe` iz zadnjeg izdanja Postave i
   pokrenite ga. Administratorska prava ne trebaju.
2. Čarobnjak pita za mapu (zadano `%LOCALAPPDATA%\goCOP`), za ime ovog
   računala u mreži (npr. `pperic-laptop`) i za mrežu: nova mreža samo za
   prvo računalo, postojeća za sva ostala. Ime se poslije ne mijenja.
3. Postava preuzme zadnje izdanje goCOP-a, provjeri mu potpis i pokrene ga.
   Kad goCOP odgovori, sama otvori stranicu Postavljanje u pregledniku.
4. Kad Windows vatrozid pita smije li goCOP na mrežu, dopustite samo
   privatne mreže. Uredsku ili kućnu mrežu u Windowsima označite kao
   privatnu, inače se računala u njoj ne nalaze.

Računalo bez interneta: uz instalacijski program stavite
`gocop-windows-amd64.exe`, `SHA256SUMS` i `SHA256SUMS.sig` iz istog izdanja
goCOP-a, npr. s USB-a. Postava ih uzme odande i provjeri jednako.

Windows može upozoriti da program nije potpisan. Pokrenite ga samo ako ste ga
preuzeli sa stranice izdanja na GitHubu.

### Linux: skripta

Za stalno računalo na Linuxu (x86_64, sa systemd-om) uz izdanje stoji
skripta koja goCOP postavi kao uslugu sustava.

1. Preuzmite datoteke izdanja u praznu mapu i provjerite potpis, kako piše
   u [uputama za Linux, poglavlje 2](linux.md#2-preuzimanje-i-provjera-izdanja).
2. Pokrenite skriptu i zadajte ime računala:

   ```sh
   sudo bash instaliraj.sh -ime pperic-posluzitelj ./gocop-linux-amd64
   ```

3. Skripta na kraju ispiše adresu i jednokratni kod za Postavljanje. Kod je
   i u dnevniku usluge: `journalctl -u gocop | grep Postavljanje`.

Kad je u istoj mapi i `gocop-postava-linux-amd64`, skripta postavi i
Postavu, s ikonom u traci.

Ostalo, od vatrozida do nadogradnje, opisano je u [uputama za Linux](linux.md).

### macOS i ručno pokretanje

Program je jedna datoteka i ne treba instalaciju. Za macOS uzmite
`gocop-darwin-arm64` (Apple silicon) ili `gocop-darwin-amd64` (Intel), a za
Windows i Linux `gocop-windows-amd64.exe` ili `gocop-linux-amd64`.

1. Stavite program u mapu u kojoj želite podatke i pokrenite ga iz nje.
   Program napravi mapu `data` u mapi iz koje je pokrenut.
2. Pokrenite ga i zadajte ime računala, npr.
   `./gocop-darwin-arm64 -node pperic-laptop`. Na macOS-u i Linuxu programu
   prije toga dajte pravo izvođenja (`chmod +x`).
3. Otvorite adresu koju program ispiše: `http://localhost` ili, kad port 80
   nije slobodan, `http://localhost:8080`. Na stranici prijave kliknite
   **Postavite ga**.

macOS može odbiti prvo pokretanje programa preuzetog preglednikom, jer
program nema Appleov potpis. Tada ga dopustite u Postavkama sustava, pod
Privatnost i sigurnost.

### Docker

Slika je `ghcr.io/tkraljevic/gocop`, za Linux amd64. Oznaka je broj izdanja
bez početnog `v` ili `latest`. Za stalno računalo birajte broj izdanja.

```sh
sudo mkdir -p /srv/gocop/data /srv/gocop/arhiva
sudo chown 99:100 /srv/gocop/data /srv/gocop/arhiva
docker run -d --name gocop --restart unless-stopped \
  -p 8080:8080 -p 4710:4710 -p 4711:4711 -p 4712:4712/udp -p 4713:4713/udp \
  -v /srv/gocop/data:/data -v /srv/gocop/arhiva:/arhiva \
  ghcr.io/tkraljevic/gocop:<izdanje> -node pperic-posluzitelj
```

Obje mape moraju biti na lokalnom disku računala, ne na mrežnom disku.
Spremnik zahtjev s vašeg računala obično ne vidi kao lokalni, pa
Postavljanje traži kod iz dnevnika spremnika:
`docker logs gocop 2>&1 | grep Postavljanje`. Zatim otvorite
`http://<adresa računala>:8080/postavljanje?kod=<kod>`.

Mape, sigurnosna kopija i nadogradnja spremnika opisani su u
[uputama administratoru, poglavlje 7](INSTALACIJA.md#7-stalni-čvor-spremnik-i-sigurnosna-kopija).

## 2. Postavljanje

Stranica **Postavljanje** postoji samo dok u računalu još nema djelatnika.
Radi s računala na kojem je goCOP pokrenut, a iz lokalne mreže samo uz
jednokratni kod iz dnevnika. Preko interneta je nema. Nakon deset krivih
kodova kod ne vrijedi do ponovnog pokretanja.

### Prvo računalo nove mreže

Nova mreža osniva se jednom, na prvom računalu.

1. Otvorite *Ovo je prvi čvor nove mreže*.
2. Upišite naziv mreže, svoje korisničko ime, ime i prezime, po želji
   e-poštu i lozinku od najmanje 10 znakova.
3. Potvrdite da je ovo prvo računalo nove mreže i kliknite **Osnuj mrežu i
   prijavi me**.

Tako nastaje vaš račun globalnog administratora, a računalo osnuje mrežu i
čuva njezin ključ. Početni račun `admin` se isključuje. Nova mreža je
zaseban svijet: ne vidi podatke drugih mreža i kasnije se ne može spojiti s
njima. Ako niste sigurni, izaberite postojeću mrežu.

### Svako sljedeće računalo

Sljedeće računalo se upari s računalom koje prima računala u mrežu. To je
prvo računalo mreže ili računalo kojemu je ono dalo ovlast za primanje. Za
uparivanje treba osoba i na drugoj strani.

1. Na novom računalu otvorite *Priključit ću se postojećoj mreži* i kliknite
   **Poveži ga s uredom**.
2. Na drugom računalu globalni administrator otvori Administracija → Ovo računalo
   i mreža → Uparivanje.
3. Jedno računalo čeka, a drugo ga nađe u lokalnoj mreži ili ga nazove po
   adresi.
4. Oba ekrana pokažu isti šesteroznamenkasti kod. Ako je isti, oboje kliknu
   **Kod se podudara — upari**. Ako nije isti, odbijte.
5. Razmjena zatim krene sama, a gumb **Razmijeni sa svima** na stranici
   Stanje razmjene pokreće je odmah.
   Djelatnici, registri i podaci stižu s drugog računala. Kad stignu
   djelatnici, prijavljujete se svojim računom.

### Početni račun admin

Svježe računalo ima i početni račun `admin` s javnom lozinkom. On služi samo
za prijelaz: vrijedi samo iz lokalne mreže, a pod Postavom samo s tog
računala, i pri prvoj prijavi traži novu lozinku. Kad računalo postavite na
stranici Postavljanje, taj vam račun ne treba.

## 3. Što dalje

Na prvom računalu mreže globalni administrator nastavlja ovim redom.

1. **Ustroj.** U Administracija → Administrativna organizacija upišite
   sektore, a zatim branjena područja. Registre i imenik možete i uvesti iz
   datoteka. Oblik datoteka opisan je u [predlošcima uvoza](predlosci/README.md).
2. **Nazivi.** Ako vaša organizacija razine ustroja ili uloge zove drukčije,
   promijenite ih u Administracija → Nazivi i Administracija → Sudionici
   obrane.
3. **Djelatnici i dužnosti.** Koje uloge postoje i što koja smije, piše u
   dokumentu [Tko što radi u programu](uloge.md). Lozinku koju upišete
   djelatniku on pri prvoj prijavi zamijeni svojom.
4. **Drugi globalni administrator.** Neka mreža ima barem dva. Kad jedan
   zaboravi lozinku, drugi mu je poništi.
5. **Uloge računala.** U Administracija → Ovo računalo i mreža →
   Uloge ovog čvora izaberite koje računalo preuzima vodostaje i koje izdaje
   prognozu. Obično je to stalno računalo, a prijenosna računala samo
   primaju. Novo
   računalo nema nijednu ulogu dok mu je ne uključite.
6. **Ostala računala.** Instalirajte ih i uparite, kako piše gore. Na svakom
   računalu osoba na svom profilu, pod Što ovo računalo prati, izabere sektor
   ili područje i godine koje joj trebaju.
7. **Sigurnosna kopija.** Kopija mora obuhvatiti cijelu mapu `data`, s
   ključem računala (`node-key`), a na računalu koje drži ključ mreže i s
   ključem mreže (`network-key`). Za ključ mreže spremite i šifriranu kopiju
   na hladni disk (Ovo računalo i mreža, pod Mreža, Kopija ključa mreže). Kad je mapa `vodostaji` izvan nje, i nju; u spremniku
   obje montirane mape. Povrat probajte prije operativnog rada. Pojedinosti
   su u [uputama administratoru, poglavlje 7](INSTALACIJA.md#7-stalni-čvor-spremnik-i-sigurnosna-kopija).
8. **Nadogradnja.** Na Windowsu nadogradnju nudi ikona Postave, uvijek na
   klik. Prije nadogradnje napravite kopiju podataka. Kad je redoslijed
   nadogradnje računala važan, piše u [popisu izmjena](../CHANGELOG.md), uz
   izdanje na koje se nadograđuje.

Portovi i vatrozid opisani su u [uputama administratoru, poglavlje 2](INSTALACIJA.md#2-mreža).
Kako se koji posao radi u programu, piše u samom programu, na stranici
**Pomoć**.
