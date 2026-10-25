# goCOP — Centar obrane od poplava

goCOP je program za obranu od poplava. Namijenjen je Hrvatskim vodama i
drugim vodoprivrednim organizacijama. Na jednom mjestu drži teren,
vodostaje, prognozu, dnevnike, akte, registre, ljude i sredstva. Kod je
otvoren, a program je neprofitan.

Svako računalo s goCOP-om radi sa svojim podacima, pa radi i bez interneta.
Kad ima veze, računala iste mreže međusobno se usklađuju.

> **Alfa: zadnje izdanje je 0.0.43-alfa, od 25. 10. 2026.** Program je za
> razvoj i testiranje, ne za operativni rad. Prognoza pomaže stručnoj
> procjeni. Ne zamjenjuje službene prognoze ni odluke odgovornih osoba. Što
> je u kojem izdanju, piše u [popisu izmjena](CHANGELOG.md).

## Što goCOP radi

- Obrana: akti o stupnjevima obrane, rješenja i obavijesti, dnevnik COP-a,
  dežurstva, dnevna izvješća i događanja. Stanje obrane dionice program
  računa iz ovjerenih akata.
- Teren: vodočuvarski dnevnik, prijave s terena s fotografijama i očitanja
  letvi.
- Vodostaji i prognoza: arhiva vodostaja, satna i dnevna prognoza s
  rasponom i uzdužni profil vala.
- Dokumenti: PDF i Excel obrasci, ovjera, osobni potpis i žig, slanje
  e-poštom.
- Registri i sredstva: ustroj, dionice, letve, objekti, vodotoci,
  djelatnici, održavanje s dnevnicima usluga A.02 i A.03, skladišta i
  inventura.

Kako se što radi, piše u samom programu, na stranici **Pomoć**.

## Kako početi

Program se preuzima sa [stranice izdanja](https://github.com/tkraljevic/goCOP/releases).

- Windows: preuzmite `goCOP-postava-….exe` i pokrenite ga. Administratorska
  prava ne trebaju. Postava preuzme zadnje potpisano izdanje, drži ikonu u
  traci i nudi nadogradnju.
- Linux: skripta iz izdanja postavi goCOP kao uslugu sustava, a uz nju i
  Postavu. Upute su u dokumentu [goCOP na Linuxu](docs/linux.md).
- macOS i ručno pokretanje: uz svako izdanje stoji program za Windows,
  Linux i macOS. Pokrenite ga i otvorite `http://localhost`. Kad port 80
  nije slobodan, program radi na `http://localhost:8080`.
- Spremnik za Linux (Docker): `ghcr.io/tkraljevic/gocop`, s oznakom izdanja
  ili `latest`. Mape `/data` i `/arhiva` montirajte trajno, na lokalni disk.

Kako se postavlja prvo računalo i kako mu se pridružuju ostala, piše u
uputi [Prvo pokretanje](docs/prvo-pokretanje.md). Portovi, vatrozid, uloge
računala, sigurnosna kopija i nadogradnja opisani su u
[uputama administratoru](docs/INSTALACIJA.md).

Prije nadogradnje napravite kopiju podataka. Kad je redoslijed nadogradnje
računala važan, piše u popisu izmjena, uz izdanje na koje se nadograđuje.

## Kako se računala usklađuju

Računala dijele podatke samo s uparenim članovima iste mreže, i to
šifrirano. Danas to ide u istoj lokalnoj mreži (LAN sync), preko stalnog
računala s javnom adresom (SERVER sync) ili preko Tailscalea, kad ga imaju
oba računala. Izravna veza dvaju računala preko interneta (WEB sync) je u
izradi. Kad bude gotova, ostat će isključena dok se ne isproba.

Kako to radi i zašto nijedan poslužitelj nije nužan, piše u dokumentu
[Kako goCOP radi](docs/kako-radi.md).

## Plan razvoja i kvaliteta koda

Što dolazi, kojim redom i zašto, piše u [planu razvoja](docs/plan-razvoja.md).
Koji je cilj kvalitete koda i gdje smo danas, piše u dokumentu
[Kvaliteta koda](docs/CODE_QUALITY.md).

Program se prevodi iz izvora. Verzija Go-a zadana je u `go.mod`, a C
prevoditelj ne treba:

```sh
go build -o bin/gocop ./cmd/gocop
go test ./...
go vet ./...
```

## Sigurnost

Ranjivost prijavite privatno, kako piše u [SECURITY.md](SECURITY.md). Ondje
piše i što program štiti, a što ne. Izdanja su potpisana ključem izdanja, a
Postava taj potpis provjerava. Potpisa za Windows još nema. Kako će raditi,
piše u [pravilima potpisivanja](docs/CODE_SIGNING.md), na engleskom.

## Dokumentacija

Svi dokumenti, složeni po temama, navedeni su u
[kazalu dokumentacije](docs/README.md).

## Licenca i zahvale

Program je pod licencom EUPL-1.2 ([LICENSE](LICENSE); hrvatski tekst je u
[LICENSE_hr.txt](LICENSE_hr.txt)). Nositelj prava su Hrvatske vode. Program
je osmislio i izgradio Tomislav Kraljević. Prava nad podacima i znakovima
te iznimke od licence navedene su u [NOTICE](NOTICE). Licenca programa ne
daje prava nad poslovnim podacima. Licence korištenog otvorenog koda i
podataka navedene su u Pomoći, u poglavlju „O programu”. Tko je pomogao,
piše u [zahvalama](ZAHVALE.md).

Dijelovi programa razvijeni su uz pomoć alata umjetne inteligencije. Sav
kod prije ulaska u program prolazi ručni pregled i automatske testove, a
odgovornost za njega je isključivo ljudska.
