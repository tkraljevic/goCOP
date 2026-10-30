# Sigurnost goCOP-a

> **goCOP je u alfa fazi i nije za operativnu upotrebu.** Ovaj dokument
> opisuje kako sigurnost radi u kodu zadnjeg izdanja i što nije zaštićeno.
> Provedene zaštite nisu potvrda spremnosti za operativni rad.

Upute za postavljanje su u [docs/INSTALACIJA.md](docs/INSTALACIJA.md), za
Linux u [docs/linux.md](docs/linux.md).

## Prijava ranjivosti

Ranjivost se prijavljuje privatno, **GitHubovim privatnim prijavljivanjem
ranjivosti**:

- na stranici repozitorija otvorite karticu **Security** i odaberite
  **Report a vulnerability**;
- ili otvorite izravno
  <https://github.com/tkraljevic/goCOP/security/advisories/new>.

Prijavu vide samo prijavitelj i održavatelji repozitorija, a za nju treba
račun na GitHubu. Ne prijavljujte ranjivost javnim zahtjevom (issue),
raspravom ni pull requestom, jer bi tako bila vidljiva svima prije ispravka.

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
Iznimka su promjene prava globalnog administratora i uprave organizacije:
one s drugog računala čekaju potvrdu ključem mreže (vidi dolje).
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
- Zamjena ključa ne postoji. Računalo s novim ključem ponovno se prima u
  mrežu; staro ime treba najprije zaboraviti i opozvati mu članstvo.

### Mreža, ključ mreže i potvrda članstva

- Mrežu osniva prvi čvor (na stranici `/postavljanje`). On napravi Ed25519
  ključ mreže i spremi ga u datoteku `network-key` uz bazu (`0600`); u bazu
  ide samo javni dio i ne putuje razmjenom.
- **Potvrda članstva** je Ed25519 potpis nad imenom mreže, imenom i javnim
  ključem čvora, oznakom izdavača te vremenom izdavanja i isteka. Vrijedi
  365 dana (osnivaču deset puta dulje). Izdaje je čvor koji ima
  `network-key` ili ovlašteni primatelj, kad osoba primi drugi čvor u
  mrežu uparivanjem ili na daljinu.
- **Ovlašteni primatelj.** Nositelj ključa mreže može članu dati ovlast za
  primanje: potpis ključem mreže nad ključem i imenom tog člana, koji
  vrijedi dvije godine. Primatelj članstvo potpisuje svojim ključem čvora,
  a u članstvo ugradi svoju ovlast, pa ga svaki član provjeri ključem
  mreže. Svaki član odbija članstvo koje primatelj izda ako traje dulje od
  njegove ovlasti ili je izdano u budućnosti (dopuštena je razlika satova
  od sat vremena). Ni ukraden ključ primatelja zato nakon isteka ovlasti
  ne izdaje valjana članstva. Ovlast daje i oduzima samo nositelj ključa
  mreže, a primatelj je ne može dati dalje.
- Čvor pri svakom spajanju pokaže svoju potvrdu u certifikatu TLS-a. Strana
  koja prima vezu prihvaća člana čiju valjanu potvrdu ima u bazi, ili
  onoga koji je pokazao valjanu potvrdu: potpis i rok vrijede (uz pričuvu od
  30 dana nakon isteka), potvrda nije opozvana, a ime ne pripada drugom
  ključu. Zato novo računalo ne treba
  uparivati sa svakim čvorom. Strana koja zove provjerava očekivani ključ i
  potvrdu iz svoje baze.
- Čvor bez valjane potvrde je **poznat, ali mu se razmjena odbija**.
- Povjerenje vrijedi za cijelu mrežu: svaki valjani član smije razmjenjivati
  sa svakim čvorom te mreže, ne samo s onim s kojim se uparivao.
- **Opoziv** (Administracija → Ovo računalo i mreža, pod Mreža) zapisuje
  se u knjigu i razmjenom stiže svim čvorovima. Vrijedi samo potpisan:
  - opoziv ovlasti potpisuje ključ mreže;
  - opoziv članstva potpisuje ključ mreže ili primatelj koji je to članstvo
    izdao;
  - opoziv članstva ovlaštenog primatelja oduzima mu i ovlast, pa ga može
    napraviti samo nositelj ključa mreže.

  Opoziv ključem mreže vrijedi za svako članstvo tog imena izdano do tada,
  i ono izdano na drugom računalu, i kad je članstvo već isteklo. Uvijek
  oduzme i svaku ovlast za primanje izdanu tom imenu do tada, i onu za
  koju nositelj ključa još ne zna. Kad primatelj opozove člana čija ovlast
  do njega još nije stigla, ovlast ostaje, a pločica računala s ključem
  mreže upozorava dok je ono ne oduzme.

  Nepotpisan ili tuđe potpisan opoziv stoji u bazi, ali ništa ne opoziva.
  U popis opoziva ulazi samo opoziv čija oznaka odgovara sadržaju i
  potpisu, a čvor opoziv traži i u svim primljenim verzijama u knjizi, ne
  samo u popisu. Zato se opoziv ne poništava: arhivirana verzija opoziva
  ne briše ga ni na jednom čvoru. Opozvana potvrda ne vrijedi ni kad je
  čvor pokaže sam, a potvrda izdana nakon opoziva vrijedi odmah (ponovno
  primanje izdaje novo članstvo). Oduzeta ovlast poništava sva članstva
  koja je primatelj izdao.
- **Obnova potvrde.** Mjesec dana prije isteka čvor pri uspješnoj razmjeni
  sam upiše zahtjev za obnovu, potpisan svojim ključem čvora; isto radi
  gumb Obnovi članstvo, u svako doba. Zahtjev putuje knjigom do čvora koji
  smije primati, a on izda novu potvrdu s istim imenom i ključem, ako
  potvrda još vrijedi ili je u pričuvi i nije opozvana. Zahtjev pod tuđim
  imenom ili s tuđim ključem ništa ne obnavlja, a nova potvrda vrijedi samo
  za ključ koji je već bio u potvrdi. Ovlast za primanje obnavlja samo
  nositelj ključa mreže, isto na zahtjev, a nositelj ključa sam sebi
  obnavlja potvrdu. Čvor koji se ne koristi ne traži obnovu i istekne.
- **Pričuva.** Istekla potvrda vrijedi za razmjenu još 30 dana, uz
  upozorenje na pločici. U tom roku čvor ne prima i ne obnavlja druge:
  primatelj treba i ovlast i vlastitu potvrdu koja nije istekla. Nakon
  pričuve čvor treba ponovno primiti u mrežu.
- **Kopija ključa mreže.** Nositelj ključa mreže sprema kopiju za hladni
  disk: sjeme ključa šifrirano je AES-256-GCM-om s ključem izvedenim iz
  lozinke kopije (scrypt, najmanje 12 znakova), a oznaka formata, naziv
  mreže, javni ključ i cijena izvoda vezani su uz šifru, pa izmijenjena
  datoteka ne prolazi. Lozinka se ne pamti nigdje. Kopiju učita samo čvor
  iste mreže, i samo kad iz nje izađe upravo ključ mreže; tada i on drži
  `network-key`. Tko ima datoteku i lozinku, ima ključ mreže, pa se lozinka
  čuva odvojeno od diska. Ključ mreže se ne da opozvati, pa kopiju sprema
  samo globalni administrator svojim očima, s tog računala ili iz lokalne
  mreže (ne izvana ni kroz tunel), uz trenutnu lozinku svog računa, s istim
  ograničenjem krivih upisa kao pri promjeni lozinke. Učitavanje traži isto,
  osim lozinke računa. U dnevnik i u knjigu ide tko je i s koje adrese
  spremio ili učitao kopiju, a globalni administratori s adresom za PIN o
  tome dobiju e-poštu: odmah s tog čvora, ili, kad on nema račun za
  slanje, sat ili više kasnije s drugog čvora kojem događaj stigne
  razmjenom. Bez ijednog računa za slanje u mreži e-pošte nema.

### Uparivanje kodom od 6 znamenki

- Uparivanje ide preko TCP porta 4711, kroz TLS 1.3 s ključevima obaju
  čvorova. Port je otvoren samo dok uparivanje čeka (najviše 10 minuta),
  a odjednom može čekati samo jedno uparivanje.
- Obje strane prikazuju isti kod od 6 znamenki. Kod ovisi o javnim
  ključevima dokazanim u TLS-u i o dva nasumična broja (32 bajta), po
  jedan sa svake strane. Strana koja zove obveže se na svoj broj (SHA-256)
  prije nego što vidi broj druge strane. Napadač u sredini zato ne može
  unaprijed tražiti ključ koji daje isti kod; za njega je kod slučajan
  (1 : 1 000 000). Uparivanje sa starijim programom, bez obveze, odbija se.
- Osobe na obje strane uspoređuju kod i potvrđuju; uparivanje uspijeva samo
  ako obje potvrde (najviše 2 minute). Prije toga se ništa ne sprema.
- Sve ostalo što drugi čvor javi (ime, izdanje, port razmjene) samo je
  njegova tvrdnja.
- Uparivati smije **globalni administrator** s promijenjenom lozinkom (ne
  dok gleda tuđim očima). Bez prijave smije samo dok je čvor **svjež**
  (ima najviše jedan korisnički račun), i to samo osoba za tim računalom:
  s ovog računala bez koda, a s privatne adrese uz jednokratni kod iz
  dnevnika, isti kao za `/postavljanje`. To vrijedi i kad čvor ne radi pod
  Postavom. Kroz posrednika nikad. Bez prijave se računala samo upare: u
  mrežu drugo računalo prima samo prijavljeni globalni administrator, a
  čvor koji drži ključ mreže ili ovlast za primanje uparivanje bez prijave
  ne nudi.
- Primanje računala u mrežu (uparivanjem ili na daljinu), davanje i
  oduzimanje ovlasti za primanje i opoziv članstva smiju i izvana, kroz
  tunel ili s javne adrese, ali uz lozinku računa upisanu u isti obrazac.
  Isto vrijedi za osnivanje mreže, zaborav čvora, obnovu članstva, adrese
  i oznaku „stalno izložen”, svaku potvrdu uparivanja, privremeni kod za
  prijavu izvana, potvrdu adrese na koju ide PIN i sve što dira pravo
  globalnog administratora: novi račun s tim pravom, davanje i oduzimanje
  oznake ili dužnosti uprave organizacije (razina 1, i privremene), te
  poništenje ili upis lozinke, uključivanje, isključivanje i brisanje
  računa koji to pravo ima. Isto traži i odluka o promjeni prava koja
  čeka potvrdu (vidi niže).
  Krivi upisi broje se kao pri promjeni lozinke. S ovog računala i iz
  lokalne mreže lozinka ne treba. Tko je to napravio i s koje adrese, piše
  u dnevniku programa, a tamo piše i zahtjev izvana odbijen zbog lozinke.
  Tuđim očima se mreža ne mijenja.

### Primanje na daljinu

- Za računalo koje nije u istoj lokalnoj mreži kao primatelj. Novo
  računalo napravi zahtjev i pokaže **kod za primanje** od 8 znakova (npr.
  `7KQ4-M2XD`). Datoteka zahtjeva ide primatelju e-poštom, a kod mu čovjek
  javi drugim putem, npr. telefonom.
- Zahtjev nosi ime i javni ključ novog čvora, potpis tim ključem i dokaz
  (HMAC-SHA256) ključem izvedenim iz koda (scrypt, N=2¹⁶, r=8, p=1, sol od
  16 bajtova). Primatelj (globalni administrator na čvoru s ključem mreže
  ili s valjanom ovlasti) upiše kod. Članstvo izda tek ako dokaz odgovara.
- Potvrda koju primatelj vrati nosi ime mreže, javni ključ mreže,
  članstvo, popis čvorova s adresama i dokaz istim ključem. Novo računalo
  je prihvaća samo ako dokaz odgovara njegovu kodu, a članstvo vrijedi pod
  ključem mreže iz potvrde. Podmetnuta datoteka odbija se na obje strane.
- Zahtjev nema rok: vrijedi dok ga ne zamijeni novi ili dok se ne učita
  potvrda. Broj pokušaja upisa koda nije ograničen; kod štiti veličina
  prostora kodova (31⁸) i cijena scrypta.

### Promjene prava s drugog računala

- Primljena verzija koja daje ili oduzima zastavicu globalnog
  administratora ili dužnost uprave organizacije (razina 1, i privremenu),
  mijenja lozinku ili uključenost računa globalnog administratora ili briše
  takav račun ne mijenja podatke dok o njoj ne odluči globalni
  administrator (Odluke (30. 10. 2026.)). Isto vrijedi za razmjenu, `.cop`
  paket i arhivu. Verzija ostaje u knjizi i šalje se dalje, a ostale
  izmjene istog računa vrijede odmah. Dok promjena čeka, taj se zapis na
  ovom računalu ne mijenja.
- U mreži se odlučuje jednom, na čvoru koji drži `network-key`, na
  stranici Stanje razmjene. Potvrda ili odbijanje potpisuje se ključem
  mreže i putuje razmjenom kao i svaki zapis; svaki čvor je provjeri
  javnim ključem mreže iz svoje baze, pa je svejedno stigne li prije ili
  poslije same promjene. Ostali čvorovi popis samo pokazuju.
- Izmjena prava napravljena na čvoru s ključem mreže odmah dobiva potvrdu
  potpisanu tim ključem i ne čeka ni na jednom čvoru.
- Čvor koji još nije u mreži odlučuje samo za sebe, jer nema ključa mreže
  kojim bi provjerio odluku.
- Odlučuje globalni administrator svojim očima; izvana uz lozinku svog
  računa.
- Čvor s ključem mreže nadograđuje se prvi. Dok radi starijim programom,
  promjene prava s drugih čvorova čekaju i nitko ih ne može potvrditi.

### Samo s ovog računala ili iz lokalne mreže

Ove radnje mijenjaju samo ovo računalo ili unose podatke bez provjerljivog
potpisa. Radi ih samo globalni administrator, svojim očima, izravno s
ovog računala ili iz lokalne mreže, a izvana i kroz tunel nikad, ni uz
lozinku:

- postavke ovog čvora (`gocop.toml`), ponovno pokretanje i uloge ovog
  čvora;
- stranica „Što ovo računalo prati”;
- izvori arhive i „Čuvaj povijest” uz letvu;
- uvoz stare `.db` arhive, koja nema potpis, i paketa s diska dok
  računalo još nije u mreži; računalo izvan mreže staru `.db` arhivu ne
  uvozi uopće;
- spremanje i učitavanje kopije ključa mreže;
- oba gumba za provjeru veze na stranici Stanje razmjene (STUN i veza s
  drugim računalom);
- dopuštenje da Postava postavi starije izdanje;
- brisanje neispravnih zapisa iz knjige na stranici Održavanje baze. To su
  zapisi koje razmjena danas ne bi primila, npr. s vremenom iz budućnosti.
  Dnevnik programa zapiše tko ih je obrisao i s koje adrese.

Lokalno je ovo računalo i privatna adresa, uz izravnu vezu bez
posrednika, i Tailscale adresa ovog računala dok je `[web]
tailscale_lokalno` uključen.

### Razmjena

- Razmjena ide preko TCP porta 4710 (TLS 1.3) ili kroz web: WebSocket na
  `/razmjena/tunel` u kojem je ista TLS veza. Tunel nema prijavu
  korisnika; tko je s druge strane, dokazuje se ključem unutar TLS-a, a
  vanjski HTTPS provjerava se certifikatima sustava.
- Certifikati čvorova su samopotpisani; ne provjerava ih tijelo za
  certifikate, nego se uspoređuje javni ključ (očekivani ključ pri pozivu,
  valjana potvrda članstva pri primanju).
- Ograde vrata: najviše 32 rukovanja i 4 razmjene odjednom. Dok druga
  strana ne dokaže ključ, veze se ograđuju po adresi: s jedne adrese
  najviše 4 rukovanja, a s jedne javne mreže 8; adresa s koje je član
  nedavno dokazao ključ ima nešto više mjesta. Kroz tunel s jednog
  klijenta idu najviše 2 veze prije dokaza ključa, a s jednog ključa 2.
  Veza koja u 2 s (kroz tunel 3 s) ne pošalje prvu poruku rukovanja
  zatvara se, a cijelo rukovanje ima rok od 15 s (kroz tunel 5 s).
- Ograde razgovora: svaka vrsta poruke ima svoju najveću veličinu, a
  nijedna ne prelazi 256 MiB. U jednom razgovoru ide najviše 5000 verzija i
  32 MiB. Verzija koja se ne bi dala poslati dalje ne prima se. Od onoga
  što druga strana javi pamti se najviše 32 oznake mogućnosti, kratak
  razlog odbijanja, kratko izdanje programa i kratko ime. Sadržaj
  (privitak, PDF, dio karte) prima se samo kad ga je ovaj čvor tražio.
- Primljena verzija mijenja podatke samo kad joj je oznaka u obliku koji
  program sam daje, s vremenom najviše pet minuta ispred sata, i kad
  oznaka u njezinu sadržaju odgovara oznaci zapisa. Ostale se preskoče ili
  samo čuvaju i šalju dalje, a razmjena ide dalje. Isto vrijedi za `.cop`
  paket i arhivu.
- Pronalaženje (UDP 4712, IPv4 broadcast) je samo imenik: odgovor nosi ime,
  oznaku čvora i portove. Lažan odgovor može potrošiti pokušaj veze, ali
  ne donosi povjerenje.
- Adrese drugih računala i poruke o greškama razmjene, koje ih nose, vidi
  samo globalni administrator, na stranici Stanje razmjene. Drugi korisnik s
  modulom Administracija dobiva stanje razmjene bez njih, a pločica razmjene
  na naslovnoj, koju vidi svaki prijavljeni korisnik, pokazuje samo naziv
  puta (LAN sync, SERVER sync, Tailscale, WEB sync) i razlog rečenicom.
- Provjera mreže za WEB sync (UDP 4713, uz `[povezivost]
  web_sync_prijava = true` i na gumb Provjeri vezu, koji vidi samo
  administrator) pita dva STUN poslužitelja kojom javnom adresom vide
  računalo. Vlastitu javnu adresu i adrese za WEB sync pokazuje samo blok
  Dohvatljivost na stranici Stanje razmjene, administratoru. STUN odgovor nije potpisan: tko je na putu, može podmetnuti
  lažnu adresu, a tko nije, morao bi pogoditi 96-bitni ID upita. Zato se
  popis poslužitelja mijenja samo u `gocop.toml`. Lažna adresa mijenja samo
  ishod provjere, a ne povjerenje među čvorovima.
- Provjera veze s računalom (gumb na stranici Stanje razmjene, samo
  administrator, lokalno) zove poznate adrese drugog računala i stane čim
  ono dokaže ključ, prije nego ovo pošalje svoj certifikat i potvrdu
  članstva. Na adresi gdje se javi netko drugi zato ne ostaje ništa o ovom
  računalu, kao ni kod razmjene (provjera ključa usred rukovanja).
- Sastajalište za WEB sync (`/razmjena/signal`, WebSocket) odgovara samo
  na čvoru s `[povezivost] posluzi_sastajaliste = true` i
  `web_sync_prijava = true`, a inače vraća 404. Unutar WebSocketa je TLS
  1.3 s ključevima čvorova i vlastitim ALPN-om (`<protokol>-signal/1`).
  Sastajalište šalje certifikat bez potvrde članstva, pa je strani klijent
  ne vidi; članstvo klijenta provjeri odmah nakon rukovanja, istim
  pravilom kao razmjena, a nečlanu odgovori samo odbijenicom. Članstvo
  gleda ponovno kad računalo ulazi u registar, čim primijeni članstva,
  ovlasti ili opozive i svakih sat vremena, pa opozvano računalo odmah
  ispada, a ono kojem je članstvo isteklo najkasnije za sat. Računalo adresu i ime čvora
  sastajališta uzima samo iz vlastitog `gocop.toml`, a ključ iz članstva,
  i stane prije svog certifikata kad se na adresi javi drugi ključ.
- Ograde sastajališta: najviše 64 veze, s jednog klijenta 8, i po 2
  rukovanja s rokom od 2 s; 64 prijavljena računala i jedna veza po
  ključu (kopija istog ključa na drugom računalu se odbija i javi);
  poruka najviše 64 KiB, više od 20 poruka u sekundi prekida vezu; 6
  poziva u minuti po pozivatelju, 2 u tijeku prema istom računalu, 8 prema
  istom računalu ukupno.
- Poziv, odziv i sinkro potpisuje pošiljatelj svojim ključem, nad
  bajtovima tijela i uz ime protokola i vrstu poruke. Sastajalište ih
  prosljeđuje netaknute i odbacuje poruku čiji pošiljatelj nije računalo
  na vezi. Primatelj provjeri člana, potpis, da je poruka za njega i
  stigla preko njegova sastajališta, vrijeme (±10 min) i da je ne vidi
  drugi put. Sastajalište vidi tko je na vezi i tko koga zove, ali ne
  vidi podatke razmjene.
- quic-go nema `recover` u svojim gorutinama. Ako se program dvaput zaredom
  prekine dok WEB sync radi, iduće pokretanje kreće bez njega (oznaka
  `povezivost-u-radu.txt` uz bazu), da se ne vrti u krugu padova.
- Veliki sadržaji (privici, PDF-ovi) adresirani su SHA-256 otiskom i
  provjeravaju se pri primitku. `.cop` paketi arhive potpisani su ključem
  čvora koji ih izdaje i provjeravaju se pri čitanju.
- Paket kanala (`.cop`) prvo provjeri potpis i izdavača, pa tek onda
  raspakira ostalo. Čvor ga ugrađuje samo kad ga je izdao član iste mreže;
  čvor koji još nije u mreži ugrađuje samo vlastite pakete. Svaki dio
  paketa čita se do svoje granice, a sadržaj se uzima samo kad ga navodi
  zapis iz paketa.

### Izdanja i potpis

- Uz svako izdanje na GitHubu stoje `SHA256SUMS` (u obliku koji ispisuje
  `sha256sum`) i `SHA256SUMS.sig`: Ed25519 potpis nad tekstom
  `goCOP izdanje v1` (s prelaskom u novi red) i sadržajem `SHA256SUMS`,
  jedan redak base64.
- `SHA256SUMS` nosi i skriptu `instaliraj.sh` i jedinicu `gocop.service`
  za Linux, pa ih pokriva isti potpis kao i program. Skripta prije
  postavljanja provjeri potpis te SHA-256 programa i jedinice.
- Potpis se stavlja ručno, ključem izdanja koji nije na GitHubu; GitHub
  Actions prevodi program i objavljuje izdanje kao nacrt, bez potpisa.
- Javni ključ izdanja ugrađen je u kod (`internal/izdanje/izdanje.go`):
  `qFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=`. Provjera na Linuxu
  OpenSSL-om opisana je u [docs/linux.md](docs/linux.md).
- Potpis automatski provjerava Postava pri nadogradnji. Sam `gocop` se ne
  nadograđuje i ništa ne preuzima.
- Postava nudi i postavlja samo izdanje novije od postavljenog. Starije
  izdanje, najstarije 0.0.37-alfa, postavlja samo kad ga globalni
  administrator dopusti na stranici Ovo računalo i mreža, svojim očima, s
  tog računala ili iz lokalne mreže (Odluke (30. 10. 2026.)). Dopuštenje
  je samo u memoriji čvora, ne putuje razmjenom i vrijedi 30 minuta, a
  izdanje se provjerava kao i pri nadogradnji.
- Na Linuxu preuzeti program mora javiti baš izdanje koje se postavlja, a
  instalacijska skripta pokreće se kao root iz kopije u mapi koju piše
  samo root, nakon još jedne provjere potpisa i SHA-256.

### Prijava

- Lozinke su spremljene kao bcrypt sažetak (zadana cijena). Lozinka svakog
  računa ima barem 10 znakova, na svakom mjestu gdje se postavlja.
  Nepostojeće korisničko ime provjerava se jednako dugo kao postojeće.
- Sesija traje 24 sata od prijave. Kolačić `gocop_session` je `HttpOnly`,
  `SameSite=Lax`, a `Secure` kad je zahtjev stigao preko HTTPS-a. Sesija
  pamti otisak lozinke s kojom je otvorena. Promijenjena ili poništena
  lozinka zato gasi ostale sesije te osobe, i prijave koje čekaju PIN, i
  na drugim čvorovima, čim im promjena stigne razmjenom. Otvorena
  stranica prestaje primati novosti kad sesija prestane.
- Zaključavanje nakon krivih lozinki: isto ime s iste adrese 5 puta u
  15 minuta, ista adresa 20 puta u 15 minuta, a izvana isto ime 30 puta na
  sat. Izvana se IPv6 adrese broje po mreži /64, osim na Tailscaleu.
  Javna IPv6 adresa vrijedi kao izvana i kad je računalo u istoj lokalnoj
  mreži, pa se takva mreža u uredu broji kao jedna adresa. Uz isključen
  `[web] tailscale_lokalno` zahtjev preko Tailscalea je izvana i broji se
  i ime. Svaki zahtjev sa zaglavljem posrednika broji se odvojeno od iste
  adrese kad zahtjev stigne izravno, a isto i zahtjev na ime koje ne vodi
  na ovo računalo. Brojači su u memoriji i nestaju s
  ponovnim pokretanjem.
- **Početne lozinke.** Nijedan novi račun nema zajedničku ni zadanu
  lozinku. Svaki račun iz imenika dobije pri prvom punjenju svoju nasumičnu
  privremenu lozinku. Početni račun `admin` bez imenika dobije lozinku koju
  nitko ne zna; uz imenik je i on na popisu.
  U bazi je samo sažetak. Popis lozinki program drži u memoriji, dok ga
  netko jednom ne preuzme na `/postavljanje/lozinke`: s tog računala na
  adresi `localhost`, `127.0.0.1` ili `[::1]`, ili inače uz kod iz dnevnika,
  a nikad kroz posrednika ni s javne adrese. U dnevnik lozinke ne idu. Nakon
  preuzimanja popisa više nema. Ako se čvor ponovno pokrene prije
  preuzimanja, računi koje u međuvremenu nitko nije mijenjao dobiju nove
  privremene lozinke i novi popis. Popis vrijedi 24 sata od prvog punjenja
  (Odluke (30. 10. 2026.)); tada se briše iz memorije i baze, uz redak u
  dnevniku, a lozinke se daju poništenjem. Privremena lozinka mijenja se
  pri prvoj prijavi. Popis vrijedi za čvor koji ga je složio.
- Računi iz izdanja do 0.0.46-alfa koji još imaju staru zajedničku početnu
  lozinku ne prijavljuju se ni s jedne adrese, ni iz lokalne mreže, dok im
  administrator ne poništi lozinku (Odluke (30. 10. 2026.)). Ta lozinka ne
  potvrđuje ni radnje uz ponovni upis lozinke (promjena lozinke, potpisni
  ključ), a otvorena prijava takvog računa prestaje pri prvom zahtjevu.
  Čvor ih nakon pokretanja pronađe u pozadini i pokaže ih globalnom
  administratoru na stranici Djelatnici. Pravilo vrijedi na čvorovima s
  izdanjem 0.0.47-alfa ili novijim; na čvoru sa starijim izdanjem račun se
  iz lokalne mreže još prijavljuje i sam mijenja lozinku, a nova lozinka
  razmjenom stiže i na novije čvorove. Zato lozinke s popisa treba poništiti
  odmah.
- **Stranica `/postavljanje`** postoji samo dok je čvor svjež i radi samo
  iz lokalne mreže, nikad kroz posrednika. Na njoj se postavlja vlastiti
  administrator (račun `admin` se tada isključuje) i osniva mreža, ili se
  čvor pridružuje postojećoj uparivanjem ili na daljinu. S tog računala
  radi bez koda; s privatne adrese traži jednokratni kod od 8 znakova koji
  čvor ispiše u dnevnik pri pokretanju. Nakon 10 krivih kodova, ukupno,
  kod ne radi do ponovnog pokretanja.
- **PIN za prijavu izvana** (zadano isključen; uključuje ga globalni
  administrator, izvana i preko HTTPS-a, tek nakon uspješnog probnog
  PIN-a): kad je uključen, prijava izvana nakon točne lozinke traži PIN od
  6 znamenki poslan e-poštom na službenu adresu. PIN vrijedi 10 minuta i
  jednom; dopušteno je 5 krivih unosa po prijavi i 10 na sat po osobi.
  „Izvana” znači: zahtjev sa zaglavljem posrednika ili s adrese koja nije
  privatna, ovo računalo ni lokalna veza. Na računalu s Tailscaleom izravan
  zahtjev na Tailscale adresu tog računala nije izvana (`[web]
  tailscale_lokalno`, zadano uključeno). Odlučuje adresa na koju je veza
  stigla, ne adresa pošiljatelja. Izvana je i zahtjev upućen imenu za
  koje se ne zna da vodi na ovo računalo; adresa, ime računala u mreži,
  `localhost`, upisana javna adresa i imena iz `[web] lokalna_imena` rade
  kao prije. Postoje i zapamćena računala
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
- **Tražilice:** svaki odgovor nosi `X-Robots-Tag: noindex, nofollow,
  noarchive, nosnippet, noimageindex`, a stranice i `<meta name="robots">`.
  `robots.txt` tražilici dopušta samo ulaz i prijavu, da na njima vidi
  oznaku „ne indeksiraj”, a sve ostalo zabranjuje. goCOP nije namijenjen
  javnosti, pa se ne nudi tražilicama. To ne zamjenjuje prijavu.
- **Ograničenja veličine i rokova** za svaku vrstu zahtjeva (zadano 2 MiB),
  ograničena zaglavlja i rokovi veze.

### Tajne na disku

- `node-key` i `network-key` su nešifrirani, s pravima `0600`. Dok zahtjev
  za primanje na daljinu čeka potvrdu, kod za primanje stoji u
  `zahtjev-na-cekanju.json` uz bazu, također nešifriran i s pravima `0600`.
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
  popis čvorova i izdavatelje potpisnih certifikata. Iznimka su promjene
  prava globalnog administratora i uprave organizacije, koje čekaju
  potvrdu ključem mreže. Članstva, ovlasti i
  opozivi provjeravaju se potpisom pri uporabi, pa ih član ne može
  krivotvoriti, a opoziv ne može poništiti. Svaki član
  smije objaviti prognozu i arhivu. Potpisani zapisi i uloge izdavanja su
  u [planu razvoja](docs/plan-razvoja.md).
- **Opoziv nije trenutan.** Čvor prihvaća opozvanu potvrdu dok mu opoziv ne
  stigne razmjenom; drugog puta za opoziv nema.
- **Ključ ovlaštenog primatelja** vrijedi koliko i ključ mreže za primanje:
  tko ga ima, može primati čvorove u mrežu dok ovlast nije opozvana. Čuvajte
  računalo primatelja kao i računalo s ključem mreže, a ovlast oduzmite
  čim više ne treba.
- **Baze nisu šifrirane.** Tko ima mapu podataka (ili njezinu kopiju) ima
  sve: osobne podatke, sažetke lozinaka, oznake sesija i ključ čvora, a s
  njim i ključeve kojima su šifrirane lokalne tajne.
- **Web sučelje je obični HTTP.** HTTPS postoji samo kroz tunel ili
  posrednika ispred čvora. U lokalnoj mreži bez HTTPS-a lozinke i kolačići
  putuju nešifrirano. PIN izvana radi samo preko HTTPS-a.
- **Svjež čvor u lokalnoj mreži.** Dok se na `/postavljanje` ne postavi
  vlastiti administrator, uparivanje bez prijave može pokrenuti svatko tko
  sjedi za tim računalom ili iz lokalne mreže ima kod iz dnevnika
  programa. Svjež čvor postavite odmah nakon prvog pokretanja.
- **Tailscale mreža je lokalna mreža.** Na računalu s Tailscaleom svaki
  uređaj iz iste Tailscale mreže prolazi kao iz ureda: prijava bez PIN-a,
  radnje nad mrežom i pravima bez lozinke računa, radnje koje se smiju
  samo iz lokalne mreže, popis privremenih lozinki s kodom i postavljanje
  svježeg čvora. Tko je u toj mreži, određuju
  njezin račun i pravila pristupa. Isključuje se s `[web] tailscale_lokalno =
  false` u `gocop.toml`; tada takav zahtjev traži PIN kao izvana, i preko
  IPv6. Na macOS-u se Tailscale prepozna samo kad njegova kartica ima i IPv6
  adresu iz `fd7a:115c:a1e0::/48`: karticu `utun` i adresu 100.x imaju i
  drugi VPN-ovi (npr. Cloudflare WARP), a oni nisu lokalna mreža.
- **Sesije:** oznaka sesije je u bazi spremljena kao čisti tekst; sesija
  ne istječe zbog neaktivnosti; na drugim čvorovima promjena lozinke gasi
  sesije tek kad ondje stigne razmjenom.
- **Skripte u stranici:** CSP ne ograničava skripte (`script-src`), pa
  zaštita od ubačenog koda ovisi o ispravnom kodiranju ispisa u predlošcima.
- **E-pošta:** slike iz primljenih poruka učitavaju se s udaljenih adresa,
  pa pošiljatelj može saznati da je poruka otvorena. HTML u porukama i
  potpisima čisti se regularnim izrazima, a ne pravim raščlanjivačem.
- **Pronalaženje čvorova** odgovara svakome tko dohvati UDP 4712 i otkriva
  ime čvora i portove.
- **Provjera mreže za WEB sync:** STUN poslužitelji (zadano Google i
  Cloudflare) vide javnu IP adresu i port računala.
- **Potpisi programa:** izvršna datoteka nema Authenticode ni drugi potpis
  sustava; Docker slika i instalacijski program Postave nemaju potpis
  izdanja (Postava ima samo SHA-256).
- **Paketi arhive:** paket letve učitan s diska ugrađuje se samo potpisan,
  ključem ovog računala ili člana mreže, a dok računalo nije u mreži samo
  vlastiti. Paket koji stigne razmjenom ugrađuje se kad mu otisak
  odgovara kazalu koje je poslao član mreže. Potpis mu nije nužan, pa mu
  se vjeruje koliko i članu koji je poslao kazalo.
- **Izgubljeno ili ukradeno računalo** i administrator s pristupom
  datotekama izvan su ovog modela.
- **Uskraćivanje usluge:** ograde ograničavaju broj veza i veličinu
  zahtjeva, ali čvor nije zaštićen od preopterećenja s mreže.
