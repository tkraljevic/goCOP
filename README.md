# goCOP — Centar obrane od poplava

Otvoreni, neprofitni program za obranu od poplava, namijenjen Hrvatskim vodama
i drugim vodoprivrednim organizacijama. Povezuje teren, vodostaje, prognoze,
dnevnike, službene dokumente, registre, ljude i sredstva. Računala rade s
lokalnim podacima i usklađuju se kada je mreža dostupna.

> **0.0.32-alfa — 4. 10. 2026.** Za razvoj i testiranje, ne za operativnu
> upotrebu. Prognoze su pomoć stručnoj procjeni, ne zamjena za službene
> prognoze i odluke odgovornih osoba. [Popis izmjena](CHANGELOG.md).

## Što administrator postavlja

- Samostalnu Go aplikaciju s ugrađenim web-sučeljem i lokalnim SQLite bazama.
  Repozitorij ne sadrži poslovne baze, imenik ni pristupne podatke.
- Stalni čvor može preuzimati vodostaje i izdavati prognoze; ostala računala,
  npr. laptopi, obično primaju gotova izdanja. Novi čvor nema nijednu ulogu
  dok mu se ne uključi u Administraciji → Čvor, mreža i sinkronizacija.
- Upareni čvorovi razmjenjuju zapise, prognoze i kišu. Kazalo hidrološke arhive
  putuje razmjenom, a potpisani `.cop` paketi dohvaćaju se prema pretplati.
- Osnovni lokalni rad moguć je bez interneta. Novi vanjski podaci, mrežne karte,
  e-pošta i razmjena zahtijevaju mrežnu vezu.

## Pokretanje

Na Windowsu goCOP instalira **goCOP Postava** (`goCOP-postava-….exe` s
GitHub stranice izdanja): bez administratorskih prava preuzme najnovije
potpisano izdanje, drži ikonu u traci za pokretanje, zaustavljanje i
nadogradnju te stavlja `gocop` u PATH. Ručno: od 0.0.28-alfa uz svako izdanje
stoje programi za Windows, Linux i macOS (ili se `gocop` prevodi iz izvora,
naredbe su pod „Dokumentacija i razvoj”), a postoji i spremnik. Pokrenuti
`gocop.exe` na Windowsu ili `./gocop` na Linuxu/macOS-u i otvoriti
`http://localhost` (ili port 8080 ako 80 nije dostupan). Pri prvoj prijavi
obvezno promijeniti zadanu lozinku. Novi čvor povezati s postojećom mrežom
čarobnjakom na prijavi; prvi čvor zahtijeva osnivanje mreže i punjenje registara.

Za Linux amd64 dostupan je spremnik `ghcr.io/tkraljevic/gocop:0.0.32-alfa`;
web u njemu sluša na 8080. Trajno montirati `/data` i `/arhiva`; SQLite mora
biti na lokalnom disku. Postavljanje, portovi, uparivanje, uloge i sigurnosne
kopije opisani su u [uputama administratoru](docs/INSTALACIJA.md).

## Mreža i sigurnost

Razmjena koristi TLS i ključeve uparenih čvorova: izravno na portu 4710 (u
lokalnoj mreži ili na adresi domenskog čvora) ili kroz HTTPS/WebSocket tunel na
`/razmjena/tunel`. Uparivanje na portu 4711 otvara se samo po potrebi;
pronalaženje na 4712/UDP ostaje lokalno.

Javni čvor treba HTTPS, ograničen pristup izvornom poslužitelju i isključen
cache aplikacijskih odgovora na posredniku. Od **0.0.25-alfa** ugrađeni su CSRF
zaštita, `Secure` kolačići iza HTTPS-a, podesivi pouzdani posrednici i ograničenja
HTTP zahtjeva i razmjene. Uparivanje odobrava administrator; zadana lozinka
ne vrijedi izvana. Od **0.0.32-alfa** čvor pri spajanju pokaže potvrdu
članstva, nositelj ključa mreže može članu dati ovlast za primanje, a računalo
se prima i na daljinu (zahtjev i potvrda vezani tajnim kodom pročitanim
telefonom); opozvana potvrda ne vrijedi ni kad je čvor pokaže sam. Kod
uparivanja dogovara se s obvezom unaprijed, a početna lozinka i svjež čvor
vrijede samo iz lokalne mreže. Od **0.0.26-alfa** prijava izvana može tražiti PIN poslan
na službenu e-poštu (prekidač zadano isključen). Popis posrednika treba suziti
na stvarne adrese posrednika. Otvoreni su potpisane ovlasti izdavatelja i opoziv
izgubljenog čvora uživo. Izvršna datoteka još nije potpisana; provedene zaštite
nisu potvrda spremnosti za operativnu upotrebu.
Prije nadogradnje izraditi sigurnosnu kopiju baza, sadržaja, postavki i
ključeva; kopiju identiteta ne koristiti kao novi čvor.

## Dokumentacija i razvoj

- **Pomoć u aplikaciji** — korisnički postupci, ovlasti, pojmovnik i „O programu”.
- [Administratorske upute](docs/INSTALACIJA.md) — instalacija i održavanje.
- [Povezivost](docs/plan-povezivost.md) i [arhiva](docs/plan-arhiva-i-zaborav.md)
  — izvedeno stanje i preostali razvojni planovi.
- [Katalog alata](docs/katalog-alata.md) — pomoćni lokalni alati izvan aplikacije.

Go verzija određena je u `go.mod`; aplikacija ne zahtijeva CGO.

```sh
go build -o bin/gocop ./cmd/gocop
go test ./...
go vet ./...
```

## Licenca i zasluge

[EUPL-1.2](LICENSE); posebne obavijesti i prava nad resursima su u [NOTICE](NOTICE).
Nositelj autorskih prava naveden u projektu: Hrvatske vode.
Program je osmislio i izgradio Tomislav Kraljević.
Licence ovisnosti i podataka opisane su u „O programu”, a doprinosi u
[ZAHVALE.md](ZAHVALE.md). Licenca programa ne prenosi prava nad poslovnim podacima.
