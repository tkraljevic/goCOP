# Postava

Postava je mali instalacijski program uz goCOP. Instalira goCOP, pali ga i
gasi, drži ikonu u traci uz sat i nadograđuje ga. Kako se njome instalira i
nadograđuje, piše u [uputama administratoru](INSTALACIJA.md#6-pokretanje).
Ovdje piše zašto su to dva programa, na što se Postava oslanja i što još
dolazi.

Postava danas radi na Windowsu 10 i 11 (64-bitni Intel i AMD) s
instalacijskim programom, a na Linuxu i macOS-u bez instalacijskog paketa.
Na Linuxu je postavlja instalacijska skripta, a kad je goCOP usluga
sustava, Postava upravlja tom uslugom ([linux.md](linux.md), poglavlje 12).

## Zašto dva programa

| | goCOP Postava | goCOP |
|---|---|---|
| što radi | instalira, pali i gasi čvor, nadograđuje ga, drži ikonu u traci i pokretanje pri prijavi | čvor: baza, razmjena, web |
| koliko se mijenja | rijetko | svako izdanje |
| izdanja | vlastita (oznake `postava-v…`) | `v0.0.x-alfa` |

Razlog je Windows: program koji radi ne može prepisati sam sebe. Zato
Postava nikad ne mijenja sebe, nego samo goCOP, koji pokreće kao svoj
podređeni program: zaustavi ga, zamijeni datoteku i ponovno ga pokrene.
Instalacijski program ne nosi goCOP u sebi. Pri instalaciji preuzme
najnovije izdanje, pa ga ne treba graditi ni potpisivati za svako izdanje
goCOP-a.

Postava od goCOP-a uzima samo male zajedničke dijelove: provjeru potpisa
izdanja, pravila za ime računala i ikonu. Ne dijeli s njim bazu, web ni
razmjenu. Zato novo izdanje goCOP-a ne može pokvariti Postavu.

Iz iskustva s ovakvim programima Postava pazi na tri stvari. Antivirus zna
kratko zadržati tek preuzetu datoteku, pa Postava zamjenu pokuša do pet
puta, a prethodni program ostavi uz novi. Unos za pokretanje pri prijavi
može ostati usmjeren na mapu koje više nema, pa Postava pri svakom
pokretanju provjeri kamo pokazuje. Druga Postava pokrenuta uz prvu samo
javi da već radi i izađe, da se u traci ne pojave dvije ikone.

## Provjera izdanja

Uz svako izdanje na GitHubu stoje programi za sve sustave, datoteka
`SHA256SUMS` s otiskom svakog programa i njezin potpis `SHA256SUMS.sig`.
Potpis nastaje ključem izdanja, koji nikad ne odlazi na GitHub. Postava u
sebi nosi javni dio tog ključa i ne prihvaća ništa što njime nije
potpisano. Zato ni provala u GitHub račun ne može Postavi podmetnuti drugi
program.

Postava pri pokretanju i svakih šest sati pita GitHub za popis izdanja. Ne
šalje ništa o računalu, korisniku ni podacima. Nadograđuje se samo na klik.

## Ugovor između Postave i goCOP-a

Postava se ne mijenja s goCOP-om, pa ono na što se oslanja mora ostati
isto. Test u repozitoriju prevodi pravi program i razgovara s njim kao
Postava, pa nijedno izdanje ne može tiho prekršiti ugovor. Ugovor čine:

- imena programa u izdanju (`gocop-windows-amd64.exe`, `gocop-linux-amd64`,
  `gocop-darwin-arm64`…) te oblik datoteka `SHA256SUMS` i `SHA256SUMS.sig`;
- `gocop -version` ispiše točno jedan redak, npr. `goCOP 0.0.38-alfa`, i
  izađe;
- `gocop -pripremi -db … -node IME` prije prvog pokretanja upiše ime
  računala u `gocop.toml`, a bazu ne otvara;
- `gocop -upravitelj -db … -config …` radi kao čvor i ugasi se sam kad mu
  Postava zatvori standardni ulaz. Isto se dogodi i kad Postava padne, pa
  čvor ne ostane raditi bez nadzora;
- `GET /zdravlje`, samo s istog računala, odgovara
  `{"izdanje":"…","radi":true}`. Po tome Postava zna radi li čvor i koje je
  izdanje.

Od 0.0.35-alfa čvor koji ne smije krenuti pod Postavom ne izlazi. To je
čvor s postojećom bazom bez ključa čvora
([poglavlje 3](INSTALACIJA.md#3-podaci-i-sigurnost)). Dok ga Postava ne
zaustavi, odgovara na `/zdravlje` uz polje `odbijeno` s razlogom, a
stranica u pregledniku kaže zašto ne radi. Inače bi ga Postava pri nadogradnji proglasila
neuspjelim i vratila prethodno izdanje. Prethodno izdanje bi uz takvu bazu
napravilo novi ključ, a time i novi identitet računala u mreži. Postava
polje `odbijeno` zasad ne čita, pa takav čvor u traci izgleda kao da radi.

Od 0.0.44-alfa `gocop` iz mape `program` instalacije, pokrenut bez `-db` i
`-config`, ne kreće, nego javi gdje su podaci čvora
([poglavlje 6 uputa](INSTALACIJA.md#windows-instalacijski-program-gocop-postava)).
Inače bi u `data` ispod trenutne mape napravio novu bazu i u mreži se javio
kao novo računalo. Postava ga uvijek pokreće s `-db`, pa nju to ne dira.

Sve ostalo goCOP smije mijenjati bez obzira na Postavu.

## Potpis za Windows

Gotovi programi stoje na GitHubu od 0.0.28-alfa. Zasad nose samo potpis
izdanja, pa Windows pri prvoj instalaciji pokaže upozorenje SmartScreena.
Prijava za besplatni certifikat SignPath Foundationa poslana je
12. 10. 2026. i čeka odgovor. Što se potpisuje i što program šalje izvan
računala, piše u [pravilima potpisivanja](CODE_SIGNING.md), na engleskom.

Kad certifikat stigne, izdavač u potpisu bit će SignPath Foundation, a ne
goCOP. Upozorenje SmartScreena nestaje kad certifikat stekne ugled, što
traje od nekoliko dana do nekoliko tjedana preuzimanja. Postava će tada uz
potpis izdanja provjeravati i potpis Windowsa.

## Što još dolazi

- **Biblioteka izdanja.** Prvi prijenos arhive vodostaja je velik i kroz
  razmjenu traje. Zadnji paketi arhive stajali bi u zasebnom spremištu
  izdanja, npr. u privatnom repozitoriju na GitHubu, uz ključ samo za
  čitanje. Novo računalo preuzme ih izravno, a dalje nastavi običnom
  razmjenom. Paket se prihvaća samo ako ga je potpisao član mreže. U
  biblioteku idu samo podaci bez osobnih podataka, npr. vodostaji i kiše.
  Dnevnici, prijave, akti i djelatnici idu samo razmjenom među članovima
  mreže.
- **Brže namještanje novog računala.** Instalacijski program ponudio bi
  uvoz iz biblioteke, a koje letve i koje godine, bira se u pregledniku.
  Naredbe za to bit će dio ugovora s Postavom.
- **MSI paket** za raspodjelu na uredska računala, kad informatička služba
  kaže što joj treba.
- **Paketi za Linux** (`.deb` i `.rpm`, potpisani, a kasnije i vlastiti
  repozitorij za nadogradnju). Do tada Linux poslužitelj koristi
  instalacijsku skriptu i uslugu sustava iz izdanja ([linux.md](linux.md)).
- **macOS:** paket `.pkg` s aplikacijom goCOP. Potpis i ovjera kod Applea
  traže njegov program za razvijatelje, koji se plaća godišnje. Bez toga
  macOS pri prvom otvaranju blokira program. Zato je macOS na redu
  posljednji: malo je korisnika, a trošak je stalan.

## Otvorena pitanja

1. Treba li automatska nadogradnja noću? Ako treba, bila bi prekidač koji
   je zadano isključen.
2. Dijeli li više korisnika istog računala jedan čvor? Prijedlog: ne, svaki
   korisnik Windowsa ima svoj.
3. Smije li program na uredskim računalima raditi iz korisničke mape
   (`%LOCALAPPDATA%`) i pokretati se pri prijavi (`HKCU\…\Run`), uz pravila
   kao što je AppLocker? Pitanje ide informatičkoj službi organizacije.
4. Treba li biblioteka stajati na GitHubu ili na poslužitelju organizacije?
   Oblik i čitanje ostaju isti, mijenja se samo adresa.
