# goCOP — Centar obrane od poplava

Otvoreni, neprofitni program za obranu od poplava, namijenjen Hrvatskim vodama
i drugim vodoprivrednim organizacijama. Povezuje teren, vodostaje, prognoze,
dnevnike, službene dokumente, registre, ljude i sredstva. Računala rade s
lokalnim podacima i usklađuju se kada je mreža dostupna.

> **0.0.34-alfa — 4. 10. 2026.** Za razvoj i testiranje, ne za operativnu
> upotrebu. Prognoze su pomoć stručnoj procjeni, ne zamjena za službene
> prognoze i odluke odgovornih osoba. [Popis izmjena](CHANGELOG.md).

## Što administrator postavlja

- Samostalnu Go aplikaciju s ugrađenim web-sučeljem i lokalnim SQLite bazama.
  Repozitorij ne sadrži poslovne baze, imenik ni pristupne podatke.
- Stanje obrane dionica izvodi se iz ovjerenih akata, prema vremenu stupanja
  na snagu; poništeni akti ostaju u evidenciji, ali ne određuju stanje.
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

Za Linux amd64 dostupan je spremnik `ghcr.io/tkraljevic/gocop:0.0.34-alfa`;
web u njemu sluša na 8080. Trajno montirati `/data` i `/arhiva`; SQLite mora
biti na lokalnom disku. Postavljanje, portovi, uparivanje, uloge i sigurnosne
kopije opisani su u [uputama administratoru](docs/INSTALACIJA.md).

## Mreža i sigurnost

Razmjena koristi TLS i ključeve uparenih čvorova: izravno na portu 4710 (u
lokalnoj mreži ili na adresi domenskog čvora) ili kroz HTTPS/WebSocket tunel na
`/razmjena/tunel`. Uparivanje na portu 4711 otvara se samo po potrebi;
pronalaženje na 4712/UDP ostaje lokalno.

Javni čvor treba HTTPS, ograničen pristup izvornom poslužitelju i isključen
cache aplikacijskih odgovora. Ugrađeni su CSRF zaštita, sigurni sesijski
kolačići iza HTTPS-a i ograničenja zahtjeva. Pouzdane posrednike treba suziti
na njihove stvarne adrese; prijava izvana može tražiti PIN e-poštom (zadano
isključeno). Početno postavljanje i zadana lozinka dostupni su samo lokalno.

Članove prima nositelj ključa mreže ili ovlašteni primatelj, uparivanjem ili
zahtjevom i potvrdom na daljinu. Članstva, ovlasti za primanje i opozivi su
potpisani; opoziv vrijedi na drugom čvoru kad mu stigne. Potpisane ovlasti za
izdavanje prognoza/arhiva i trenutačni opoziv ostaju razvojni zadaci.
Izvršne datoteke još nemaju potpis operacijskog sustava; potpis izdanja
provjerava se zasebno. Granice zaštite opisane su u [SECURITY.md](SECURITY.md).

Prije nadogradnje izraditi kopiju baza, sadržaja, postavki i ključeva;
kopiju identiteta ne koristiti kao novi čvor. Za stanje obrane i storno svi
čvorovi trebaju **0.0.34-alfa ili novije**; stariji poništen akt još broje.

## Dokumentacija i razvoj

- **Pomoć u aplikaciji** — korisnički postupci, ovlasti, pojmovnik i „O programu”.
- [Administratorske upute](docs/INSTALACIJA.md) — instalacija i održavanje.
- [Linux poslužitelj](docs/linux.md) — izdanje s provjerom potpisa, usluga
  systemd, vatrozid, ažuriranje i deinstalacija.
- [Sigurnost](SECURITY.md) — prijava ranjivosti, podržane verzije, model
  povjerenja i što nije zaštićeno.
- [Predlošci uvoza](docs/predlosci/README.md) — oblik datoteka za registre,
  prvo pokretanje, očitanja, ugovor A.02 i tok vodotoka.
- [Povezivost](docs/plan-povezivost.md) i [arhiva](docs/plan-arhiva-i-zaborav.md)
  — izvedeno stanje i preostali razvojni planovi.
- [Katalog alata](docs/katalog-alata.md) — pomoćni lokalni alati izvan aplikacije.
- [Kvaliteta koda](docs/CODE_QUALITY.md) — `make quality`, referentno mjerenje
  i provjera regresija; [stabilizacija](docs/STABILIZACIJA.md) prati popravke.

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
