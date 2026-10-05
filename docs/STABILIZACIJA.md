# Plan stabilizacije: najrizičnije funkcije

Ovaj dokument bira funkcije koje je najopasnije mijenjati i za svaku kaže:
- što radi i koja poslovna pravila nosi;
- gdje je ista logika napisana još jednom;
- što promjena dira;
- koji testovi moraju postojati prije promjene;
- koliko je preuređenje razumno.

Ne predlaže veliko prepisivanje. Svaka promjena ide tek kad je ponašanje zaključano testom.

Polazišna analiza je snimka mastera 0.0.33-alfa (`713d9df`), ne trenutačan
popis otvorenih grešaka. Testovi koji se spominju kao „iz grane
`stabilizacija-plan`” su datoteke `*_zakljucano_test.go`, dodane uz ovaj dokument.

**Stanje 5. 10. 2026., prema izdanju 0.0.34-alfa (`3194e39`):** uvedeno je
[stanje obrane iz akata i storno](NACRT-STADIJI-OBRANE.md), izravne rute obrane
su uklonjene, a prognoza u satu izdavanja zadržava mjerenje. Ispravljeni su
i upisi izvješća izvan dosega, delegiranje privremene uprave na niže razine,
prava nepoznatih uloga, odjava isključenog računa, ponovljena predaja
dežurstva i gubitak nacrta nakon odbijene predaje lista. `main` je rastavljen
na korake pokretanja. Detalji su u [popisu izmjena](../CHANGELOG.md#0034-alfa--4-10-2026).
Brojke i nalazi niže ostaju povijesni dokaz polazišta; novo mjerenje daje
`make quality` na konkretnom commitu.

**Stanje 6. 10. 2026.:** zaostaci iz faza 1 i 2 (sumnjiva ponašanja iz
PR-ova #4–#11 i nalazi iz ovog plana koji su ostali nakon 0.0.34) ispravljeni
su ili čekaju odluku. Popis je u odjeljku
[Zaostaci faza 1 i 2](#zaostaci-faza-1-i-2-6-10-2026).

## Kako je izabrano

Polazište je `.quality/code-health.json` mjerenja mastera na Linuxu (vidi „Brojke” niže), ali brojka nije presudila. Uz složenost (CC), pokrivenost i CRAP gledano je:

- **Kritičnost.** Je li funkcija na putu:
  - stanja obrane i akata o obrani;
  - ovlasti i prijave;
  - prognoze koju dežurni čita;
  - ili uvoza podataka koji razmjenom odlaze na druge čvorove.
- **Churn.** Broj commitova koji su dirali tijelo funkcije (`git log -L`). Povijest repozitorija počinje 26. 9. 2026. i ima 67 commitova, većinom cijelih izdanja, pa churn ovdje malo razlikuje. Jasno se ističu samo `main` (19), `server.go` (23 commita na datoteci) i osvježavanje prognoze (3 do 4).
- **Pokrivenost po paketu.** Alat ne koristi `-coverpkg` (`dev/quality/tests_run.go:48`). Funkcija servisa koju vježbaju samo testovi iz `internal/web` zato ima 0 %, iako se izvodi. Uz svaku funkciju piše koji je testovi stvarno izvode.

Namjerno su izostavljeni:
- `repository.applyOne` i `removeFromSurface` (`internal/repository/apply.go`): applyOne je sljedeća stavka održavatelja i ima iznimku do 15. 11. 2026.;
- `internal/peers` i `internal/razmjena`: paralelan rad.

Brojevi redaka odnose se na `713d9df`.

## Brojke

„Prije” je mjerenje mastera `713d9df` (0.0.33-alfa) alatom `dev/quality` na Linuxu (CI izdanja 0.0.33): ukupni coverage 52,5 %, kritični 52,5 %. „Nakon faze 1 i 2” je lokalno mjerenje (darwin/arm64) na isti način, coverage po paketu, nakon spajanja faze 2 i testova ove grane. CC je isti, jer se kod ne mijenja.

| # | Funkcija | CC | Coverage prije | CRAP prije | Coverage nakon faze 1 i 2 | CRAP nakon |
|---:|---|---:|---:|---:|---:|---:|
| 1 | `(*Osvjezivac).Osvjezi` (`internal/prognoza/osvjezavanje.go`) | 63 | 73,2 % | 139,6 | 76,2 % | 116,5 |
| 2 | `(*Osvjezivac).dnevno` (`internal/prognoza/osvjezavanje.go`) | 40 | 4,4 % | 1436,0 | bez promjene | bez promjene |
| 2 | `(*Osvjezivac).vrhoviIzDnevnog` (`internal/prognoza/osvjezavanje.go`) | 26 | 4,9 % | 607,1 | bez promjene | bez promjene |
| 3 | `ProvjeriUnatrag` (`internal/prognoza/provjera.go`) | 49 | 0,0 % | 2450,0 | 76,0 % | 82,2 |
| 4 | `(*AktService).Ovjeri` (`internal/service/akt_service.go`) | 8 | 0,0 % | 72,0 | 71,4 % | 9,5 |
| 4 | `(*AktService).zakljuciOvjeru` (`internal/service/akt_service.go`) | 19 | 0,0 % | 380,0 | 77,1 % | 23,3 |
| 5 | `(*AktService).Pripremi` (`internal/service/akt_service.go`) | 38 | 0,0 % | 1482,0 | 84,6 % | 43,3 |
| 6 | `(*AktService).primatelji` (`internal/service/akt_service.go`) | 52 | 0,0 % | 2756,0 | 49,6 % | 398,2 |
| 7 | `(*EpisodeService).Declare` (`internal/service/episode_service.go`) | 9 | 0,0 % | 90,0 | 95,2 % | 9,0 |
| 7 | `(*EpisodeService).Raise` (`internal/service/episode_service.go`) | 7 | 0,0 % | 56,0 | 92,3 % | 7,0 |
| 7 | `(*EpisodeService).End` (`internal/service/episode_service.go`) | 8 | 0,0 % | 72,0 | 93,8 % | 8,0 |
| 7 | `izracunaj` (`internal/service/episode_service.go`) | 8 | 100,0 % | 8,0 | bez promjene | bez promjene |
| 8 | `(*ReadingService).validate` (`internal/service/reading_service.go`) | 25 | 0,0 % | 650,0 | 100,0 % | 25,0 |
| 9 | `NewUserPermissions` (`internal/models/user.go`) | 27 | 94,9 % | 27,1 | 100,0 % | 27,0 |
| 10 | `(*Server).authMiddleware` (`internal/web/server.go`) | 18 | 30,0 % | 129,1 | 90,0 % | 18,3 |
| 11 | `Run` (`internal/importer/csvlevels/csvlevels.go`) | 47 | 0,0 % | 2256,0 | 94,3 % | 47,4 |
| 12 | `Run` (`internal/importer/ugovor/ugovor.go`) | 31 | 0,0 % | 992,0 | 89,7 % | 32,1 |
| 12 | `(*Contract).parseTroskovnik` (`internal/importer/ugovor/ugovor.go`) | 26 | 0,0 % | 702,0 | 97,9 % | 26,0 |
| 13 | `RunJournals` (`internal/importer/bp16/journals.go`) | 82 | 0,0 % | 6806,0 | 22,1 % | 3260,6 |
| 13 | `Run` (`internal/importer/bp16/bp16.go`) | 55 | 0,0 % | 3080,0 | bez promjene | bez promjene |
| 15 | `(*opisivac).opisi` (`internal/service/zid_service.go`) | 104 | 27,0 % | 4314,4 | bez promjene | bez promjene |
| 14 | `main` (`cmd/gocop/main.go`) | 217 | 0,0 % | 47306,0 | vidi ispod | vidi ispod |

Stavka 14 riješena je zasebno (PR #12): `main` je rastavljen na `run` i korake pokretanja (`main` CC 1, `run` CC 65, uz test životnog ciklusa čvora), a premješteni dijelovi bez testova vode se kao premještaji u vratima kvalitete (`docs/CODE_QUALITY.md`).

## 1. `prognoza.(*Osvjezivac).Osvjezi` — `internal/prognoza/osvjezavanje.go:156`

**Odgovornost.** Jedno satno izdanje prognoze. Funkcija:
- učita namješteni lanac (sve inačice), zapisane promašaje i modele elektrana;
- pročita 14 dana očitanja;
- odabere inačicu računa po letvi i odredi sat izdanja;
- vrhu lanca da budućnost;
- složi sve što pregled prikazuje u `Ishod`.

Sama ništa ne piše: zapis radi `Zapisi` (`:719`).

**Pravila.**
- Prazna baza prognoza je greška (`:161-163`).
- Inačica po letvi je prva čiji su ulazi svježi, najviše 3 sata iza (`ZaostatakVrha`, `:865`). Ako ni jedna nije, a letva je svježa, letva postaje vrh lanca (`OdaberiInacice`, `:773-827`).
- Tuđa prognoza ispred računa (Komárom, `:53-55`) skida vlastiti račun dok je svježa (`:206-220`).
- Sat izdanja je zadnji sat koji imaju svi vrhovi (`ZadnjiZajednicki`, `:870-887`). Vrhovi koji kasne više od 3 sata idu u `Ceka` (`:244-260`).
- Isti sat se bez `Iznova` ne računa ponovno (`:261-264`).
- Budućnost vrha bira se redom: naš dnevni model, pa tuđa prognoza, pa model elektrane (`:270-315`).
- Zapisani promašaji primjenjuju se samo na glavnoj inačici: oduzme se pomak, a raspon postaje izmjereni rasap (`:334-345`).
- Bez zadanog dosega računa se 96 sati (`:316-319`).

**Duplikati** (provjereni čitanjem; detektor ne javlja par):
- Blok koji primjenjuje promašaje ponovljen je u `ProvjeriUnatrag` (`provjera.go:133-138`). Aritmetika je ista, ali tamo nema uvjeta glavne inačice, a komentar tvrdi „isto što radi živa prognoza”.
- Spajanje inačica i popis potrebnih nizova (`:171-186`) ponovljeni su u `provjera.go:60-68, 84-87`. `trebaniProvjere` (`provjera.go:343-354`) doslovno je prva polovica `TrebaniIzvori` (`:842-858`).
- Redoslijed izvora budućnosti vrha ponovljen je u `internal/web/slika_lanca.go:66-94`, i to drugim redom: elektrana, dnevni, tuđa. Danas se skupovi letvi ne preklapaju, pa razlike nema, ali je skrivena.

**Što promjena dira.**
- Satni posao: `cmd/gocop/main.go:1219` pokreće uvoznik javnih vodostaja svakih sat (`javnivodostaji.go:562-585`), a nakon preuzimanja ide `NakonPreuzimanja` (`main.go:976-1126`).
- `POST /prognoze/generiraj` (`server.go:993`), koji smije svaki prijavljeni korisnik.
- `POST /prognoze/pripremi-model` (`server.go:1002`), samo administrator.
- Razmjena: izdanje ide u knjigu kao `prognoza_izdanje` (`cmd/gocop/prognoza_razmjena.go:27-65`, `internal/prognoza/razmjena.go:122-152`). Promjena oblika `izdane`, `dnevne` ili `izbor` dira primatelje na drugim čvorovima, i starije čvorove.

**Testovi prije promjene.**
- Postoje u `osvjezavanje_test.go`:
  - izdavanje, isti sat, novo očitanje, bez očitanja;
  - zaostatak vrha, `Iznova`;
  - prelazak na rezervnu inačicu;
  - tuđa prognoza ispred računa.
- Iz grane `stabilizacija-plan`: `TestOsvjezavanjeRubniSlucajevi` i `TestProvjeraUnatragGlacanjeDiraSatIzdavanja` (primjena zapisanih promašaja, i u satu izdavanja).
- Nedostaju:
  - promašaji na rezervnoj inačici (ne primjenjuju se);
  - promašaji s rasapom većim od nule;
  - `Ceka` i `KoCeka`;
  - redoslijed izvora budućnosti vrha i preskakanje zauzetih;
  - budućnost iz modela elektrane;
  - tuđa prognoza starija od 48 sati;
  - pregledne letve;
  - sve što traži arhivu: druga veličina po krivulji, sidra druge veličine, zamjena s druge obale.
- Na razini pozivatelja: „Generiraj” računa s `Iznova`, a zapisuje bez njega (`main.go:1093-1120`), vidi „Sumnjivo ponašanje” u PR-u.
- Postojeći testovi mijenjaju globalni `PoluvijekIspravka` i ne vraćaju ga. Novi testovi moraju ga vraćati, kao `bezIspravka` u grani.

**Prijedlog.**
- Jedna funkcija za budućnost vrhova lanca (`:206-220` i `:270-315`), s izričitim satom izdanja. Redoslijed izvora bio bi na jednom mjestu, nestala bi neusklađenost sata na zidu i sata izdanja (`:208` prema `:292`), a provjera unatrag mogla bi koristiti dio za elektrane.
- `primijeniPromasaje` za oba mjesta, s uvjetom glavne inačice kao izričitim pravilom. Treba li to pravilo i provjeri unatrag, poslovna je odluka.
- Sat kao ovisnost (`time.Now` na `:187` i `:208`).
- **Kozmetički** bi bilo izdvojiti rep (`:352-398`: sidra, prenesene prognoze, pregledne letve). Treba iste varijable, pa složenost pada, a povezanost ostaje.

## 2. `prognoza.(*Osvjezivac).dnevno` i `vrhoviIzDnevnog` — `internal/prognoza/osvjezavanje.go:573` i `:480`

**Odgovornost.**
- `dnevno` izdaje prognozu za 1. do 6. dan za letve iz `DnevniCiljevi` (`dnevna.go:160-174`). Pada na rezervne modele i na modele bez kiše, preračuna je u protok po krivulji i zabilježi kojim je putem išla.
- `vrhoviIzDnevnog` od iste dnevne prognoze gradi satnu budućnost za vrhove lanca Letenye i Goričan (`dnevna.go:191`).

**Pravila.**
- Bez arhive nema ničega (`:576-578`, `:482-484`).
- Dnevni modeli uče se jednom na dan. Spremnik je ključan samo po datumu (`:411-462`): oborine koje se pojave tijekom dana ne ulaze do sutra, a greška učenja ostaje u `BezDnevne` i kad rezervni model uspije.
- Prvi uspješan model, pa `SastaviBezKise`: prva dva dana bez kiše za Goričan i Letenye (`dnevna.go:225-251`).
- Ako granica raspona izađe iz krivulje, obje granice postaju sam protok (`:661-683`).
- U `vrhoviIzDnevnog` dnevne vrijednosti stoje na `Ciljni−12`, a dan 0 se preskače (`:542-558`).
- Kad pretvorba ostavi manje od dvije točke, `vrhoviIzDnevnog` ne pokušava sljedeći model, jer `break` stoji izvan uvjeta (`:560-563`).

**Duplikati** (provjereni):
- Model i kiša (`:494-499` prema `:579-589`), učitavanje satnih nizova (`:505-515` prema `:616-627`), petlja prvog uspješnog modela i pretvorba po krivulji (`:538-557` prema `:663-683`) napisani su dvaput.
- Letenye i Goričan su u oba popisa, pa svako osvježavanje računa njihovu dnevnu prognozu dvaput i dvaput traži oborine.
- Dnevno u satno na `Ciljni−12` postoji i u webu (`handlers_prognoze.go:1442-1456`), s drugačijim granicama.

**Što promjena dira.** Pozivatelj je samo `Osvjezi` (`:270`, `:359`), pa i sve što dira njega. Dnevne prognoze idu u knjigu s izdanjem. Sami modeli se ne spremaju ni ne razmjenjuju: svaki čvor koji izdaje uči ih iz svoje arhive.

**Testovi prije promjene.**
- Izravnog testa nema; svi testovi `Osvjezi` rade bez arhive. Postoje samo testovi dijelova u `dnevna_test.go` i `kisa_slivova_test.go`.
- Nedostaju:
  - rezervni model i opis izbora;
  - tri poruke o oborini;
  - preračun u protok i granice izvan krivulje;
  - sidra ulaza;
  - spremnik modela (isti dan, oborine dodane tijekom dana);
  - za `vrhoviIzDnevnog`: dan 0, `Ciljni−12`, sidro zadnjeg mjerenja, letva bez krivulje i manje od dvije točke.
- Treba arhiva s `spoj` i `hq_krivulje`, ili ručno umetnuti `dnevniModeli`.

**Prijedlog.**
- Dnevne prognoze računati jednom po satu izdanja i dati ih objema funkcijama. Nestaje udvostručeni račun i dvostruki kod, a `vrhoviIzDnevnog` postaje čista pretvorba dnevnih vrijednosti u satni niz, provjerljiva bez baze.
- Testovi prvo, jer ispravak izostalog prelaska na sljedeći model mijenja ponašanje.
- **Kozmetički** bi bilo izdvojiti poruke o oborini (`:686-697`) ili sidra (`:700-714`).

## 3. `prognoza.ProvjeriUnatrag` — `internal/prognoza/provjera.go:45`

**Odgovornost.** Pusti prognozu po arhivi, svakih `Korak` sati od `Od` do `Do`. Izmjeri promašaj po letvi i dosegu prema mjerenju i prema postojanosti (zadnjoj izmjerenoj vrijednosti). Ispiše tablicu, a sa `Zapisi` upiše promašaje koje živa prognoza poslije koristi za ispravak i raspon.

**Pravila.**
- Korak mora biti barem 1 sat, a provjerava se prije čitanja baza (`:50-52`).
- Inačica se bira po satu izdanja (`:115`). Vrhu se daje samo budućnost elektrana (`:117-122`).
- Promašaji se primjenjuju samo uz `Ispravi`, i to na svim inačicama (`:133-138`).
- Zapisuju se samo dosezi s barem 100 slučaja (`:233`). Rasap je 70. postotak odstupanja, brojanjem (`UdioURasponu`, `baza.go:988`).
- Glačanje (zadano 6 h, `:40`) prosječi i doseg 0 (`:258-287`).
- Spremanje samo dopisuje i mijenja iste dosege (`baza.go:1007-1027`).

**Duplikati.** Vidi pod 1: blok promašaja, spajanje inačica, `trebaniProvjere` i postava računala za jedan sat razilaze se od `Osvjezi`. U webu nema ponovljenog mjerenja.

**Što promjena dira.**
- Jedini pozivatelj je priprema modela (`main.go:1150`), preko `POST /prognoze/pripremi-model` (samo administrator, posao do 3 sata).
- Piše samo tablicu `promasaji` u `prognoze.db`, ali ona putuje razmjenom kao dio modela (`razmjena.go:69`). Primatelji je zamijene cijelu (`razmjena.go:267-286`).
- Promjena mjerenja tako mijenja raspone i ispravke prognoze na svim čvorovima.
- Vanjski alat `provjeri-prognozu` (izvan repozitorija, `docs/katalog-alata.md:49`) zove istu funkciju.

**Testovi prije promjene.**
- Na masteru nema nijednog.
- Iz grane `stabilizacija-plan` (`provjera_zakljucano_test.go`):
  - korak;
  - prazna baza;
  - stalna voda (13 zapisanih dosega, pomak −5, postojanost, CSV);
  - glačanje dosega 0;
  - premalo slučaja i stari zapis koji ostaje;
  - čitanje bez veličine.
- Nedostaju:
  - `Ispravi` (i na rezervnoj inačici);
  - miješanje glavne i rezervne inačice u istoj statistici;
  - budućnost elektrana;
  - tablica udjela i oznake „bolje” i „LOŠIJE”;
  - glačanje s rupama u dosezima;
  - greške čitanja.

**Prijedlog.**
- Odvojiti mjerenje (petlja `:111-174`, vraća zbrojeve) od ispisa (`:176-224`) i spremanja (`:226-250`). Statistika se onda provjerava bez čitanja teksta.
- `trebaniProvjere` zamijeniti s `TrebaniIzvori`. To je čisti duplikat.
- Odluke, ne preuređenje:
  - smije li se doseg 0 gladiti i zapisivati (`racun.go:569-571` kaže da u satu izdavanja promašaj mora biti nula);
  - treba li uvjet glavne inačice i ovdje;
  - smije li spremanje brisati dosege koji više nisu izmjereni. *Riješeno
    6. 10. 2026. (F1-24): nova provjera zamjenjuje sve promašaje letve i
    veličine, i kad ne zapiše ništa.*
- **Kozmetički** bi bilo izdvojiti samo ispis.

## 4. `service.(*AktService).Ovjeri` i `zakljuciOvjeru` — `internal/service/akt_service.go:638` i `:666`

**Odgovornost.** Ovjera akta o obrani: broj akta, potpis, i promjena stanja obrane na dionicama akta (proglašenje, podizanje, prestanak). Uz to i objava u dnevnicima COP-a, održavanja i vodočuvara.

**Pravila.**
- Ovjerava globalni administrator.
- Na razini sektora ovjeravaju rukovoditelj, zamjenik ili glavni zamjenik sektora, a zamjenik za područje samo za svoje područje.
- Rukovoditelj i zamjenik područja ovjeravaju sve osim izvanredne obrane (`SmijeOvjeriti`, `:336-392`).
- Bez otvorenog dnevnika COP-a u sektoru nema ovjere (`trebaAktivnu`, `:655-657`). Ovjera skenom (`UcitajSkenirani`, `:825-866`) to ne traži.
- Broj je `MAX(broj)+1` po sektoru i godini (`akti_repo.go:137-144`), izvan transakcije i bez jedinstvenog ključa.
- Ovjera se spremi prije promjene obrane (`:687`). Greške obrane i objave postaju upozorenja.
- Obrana se mijenja ovlastima izvedenima iz dionica akta, a ne ovlastima onoga tko ovjerava (`:697-700`):

| akt | otvorena obrana na dionici | radnja |
|---|---|---|
| uspostava | nema | proglašenje (`:709-711`) |
| uspostava | niži stupanj | podizanje (`:712-713`) |
| uspostava | isti ili viši | ništa, bez upozorenja |
| prekid pripremnog stanja | bilo koja | prestanak (`:715-716`) |
| prekid drugog stupnja | postoji | `Raise` s istim stupnjem, uvijek samo upozorenje (`:717-718`) |

**Duplikati.**
- Dva puta mijenjaju stanje obrane: ovjera akta i izravne rute na dionici (`handlers_episodes.go`). Razlikuju se u:
  - ovlastima: dionice akta, odnosno pisanje na dionici;
  - aktivnoj obrani: tražena, odnosno ne;
  - vremenskoj zoni: Zagreb (`handlers_akti.go:266`), odnosno `time.Local` (`handlers_episodes.go:35-41`);
  - mjerodavnoj letvi;
  - pravilu „sat unaprijed”: upozorenje, odnosno odbijanje.
- Izravne rute nema nijedan predložak ni skripta. Žive su, ali ih sučelje ne koristi (samo `server.go:1086-1088`).
- Pravila potpisnika stoje na četiri mjesta: `:318-323`, `:336-392`, `:676-678` i `models/akt.go:336-352`.
- `Ovjeri` i `UcitajSkenirani` ponavljaju iste preduvjete i razlikuju se u aktivnoj obrani.

**Što promjena dira.**
- `POST /akti/{id}/ovjeri` (`server.go:1303`) i `POST /akti/{id}/sken` (`server.go:1305`).
- Sve što piše ide kroz knjigu i razmjenu: `akti`, `akti_izvornici`, `defense_episodes`, listovi i zapisi dnevnika, vodočuvarski listovi.
- Na drugim čvorovima primjena je obični upis po identitetu, pa dvostruki brojevi akata i dvije otvorene obrane na istoj dionici ostaju neusklađeni.

**Testovi prije promjene.**
- Postoje u `internal/web`:
  - `TestAktOdVodomjeraDoOvjereKrozRute`: brojevi 1 i 2, objava, potpis, „u zamjenu”, izvanredna na dvije dionice, prekid pripremnog;
  - `TestTkoSmijeOvjeritiAkt`;
  - `TestRucniPotpisISkenKrozRute`, `TestSlanjeNaZnanjeKrozRute`.
- Iz grane `stabilizacija-plan`: `TestOvjeraAktaMijenjaStanjeObrane` (cijela tablica gore, akt sat unaprijed) i `TestOvjeraBezOvlasti`.
- Nedostaju:
  - ovjera u preventivnoj obrani, i skenom;
  - izvanredno stanje koje ovjerava rukovoditelj područja (prepisani potpisnik);
  - broj na prijelazu godine (Zagreb prema UTC) i dvije ovjere u isto vrijeme;
  - letva koje nema i prazna letva (`uuid.MustParse`, `:701`);
  - djelomičan neuspjeh po dionicama;
  - prognoza kao osnova epizode;
  - čvor bez ključa (bez potpisa).

**Prijedlog.**
- Odluku po dionici (`:691-724`) izdvojiti u čistu funkciju `(radnja, stupanj, otvorena) → radnja` i provjeriti cijelu tablicu: 2 radnje × 4 stupnja × nema, niži, isti, viši. Grana prekida koji nije pripremni onda se popravi ili izričito zadrži. To nije kozmetika.
- Preduvjete ovjere staviti na jedno mjesto za `Ovjeri` i `UcitajSkenirani`, a broj akta dodijeliti u istoj transakciji kao spremanje.
- Jedinstvenost broja među čvorovima traži odluku o dizajnu, ne preuređenje.
- Odlučiti o izravnim rutama obrane: maknuti ih ili ih voditi istim pravilima.

## 5. `service.(*AktService).Pripremi` — `internal/service/akt_service.go:132`

**Odgovornost.** Od letve složi nacrt akta: dionice, sektor i područje, provjeru ovlasti, vodostaj i tendenciju (ili prognozu), vezu prekida na akt koji prekida, potpisnika, primatelje i tekst iz predloška. Ništa ne piše.

**Pravila.**
- Radnja je uspostava ili prekid. Stupanj mora biti na snazi (`:136-141`).
- Dionice su sve dionice letve, po želji presječene sa zadanima; tuđe tiho otpadnu (`:164-183`).
- Sektor je sektor prve dionice (`:193-195`). Područje je ono s najviše dionica (`:197`, `:782-798`).
- Pripremiti smije tko piše na sektoru, području ili bilo kojoj dionici akta, ili tko smije ovjeriti (`:198-200`, `:757-776`). Vodočuvar jedne dionice tako priprema akt za sve dionice letve.
- `Vrijedi` bez vrijednosti postaje sada. Gornje ni donje granice nema; ona je samo u proglašenju obrane (`episode_service.go:55`).
- Prekid se veže na zadani ovjereni akt uspostave, ili na najnoviji ovjereni akt iste letve i stupnja (`:907-923`), bez provjere je li već prekinut.

**Duplikati.**
- Dionice letve (`:164-167`, `:186-192`) ponavljaju `DioniceLetve` (`:941-952`).
- Sučelje nudi letve po labavijem pravilu (`smijeLetvu`, `handlers_akti.go:228-240`), pa obrazac može ponuditi letvu koju servis odbije.

**Što promjena dira.** `POST /akti/novi` (`server.go:1294`), pa `Spremi`, koji akt upiše u knjigu (`akti_repo.go:130`). Nacrti se razmjenjuju kao i ovjereni akti.

**Testovi prije promjene.**
- Postoje u `internal/web`: `TestAktOdVodomjeraDoOvjereKrozRute` (dionice, područje, vodostaj, tendencija, izabrano očitanje, potpisnik, veza prekida).
- Postoje u servisu: `TestPodrucjeAktaJeOdredeno`, `TestAktPiseUpravaPodrucjaSvakeDionice`.
- Iz grane `stabilizacija-plan`: odbijanje neispravnog zahtjeva u `TestOvjeraBezOvlasti`.
- Nedostaju:
  - korisnik bez dosega;
  - vodočuvar koji priprema akt za više dionica;
  - prognoza umjesto očitanja;
  - nepoznato očitanje;
  - letva čije su dionice u dva sektora;
  - dionica koje nema u registru;
  - prekid zadan na akt druge letve, na nacrt ili na već prekinut akt.

**Prijedlog.**
- Izdvojiti doseg akta (`:163-197`, uz `DioniceLetve`) i osnovu vodostaja (`:202-235`) kao čiste funkcije nad očitanjima, provjerljive bez baze.
- **Kozmetički** bi bilo dijeliti ostatak (potpisnik, primatelji, predložak).
- Odlučiti pripada li ovdje i granica za `Vrijedi`.

## 6. `service.(*AktService).primatelji` — `internal/service/akt_service.go:395`

**Odgovornost.** Složi popis primatelja akta, po skupinama, bez dvojnika i poredan. Nikad ne vraća grešku: svaka greška izvora se preskoči, pa popis može tiho ostati nepotpun (`:419-421`, `:439-441`, `:468`, `:483`, `:487`, `:499`, `:508`, `:535-537`).

**Pravila.**
- Ugroženi teritorij dolazi iz dionica akta (`:411-433`).
- Županijske službe:
  - službe vezane za općinu idu samo ako je općina ugrožena;
  - vatrogasci i Crveni križ nikad;
  - stožer civilne zaštite samo u izvanrednom stanju (`:442-462`, `sluzba.go:92-100`).
- Registar sektora: aktivni, nearhivirani, iz područja akta, od zadanog stupnja (`akt.go:529-540`).
- Izvođači područja, ili naziv ugovorne pravne osobe kad izvođača nema (`:483-493`).
- U izvanrednom stanju i župani te gradovi i općine (`:494-522`).
- Ljudi s dužnošću na području i dionici, svaki jednom, sa svim funkcijama (`:523-568`). Upit ne gleda istek dužnosti (`section_repo.go:349-356`).
- Pismohrana uvijek, na kraju (`:569`).

**Duplikati.** Nema. `AdresatiAkta` (`akt_posta.go:279-305`) čisti adrese za slanje, što je druga svrha.

**Što promjena dira.** Popis se zamrzne u aktu i u potpisanom sažetku (`akt.go:404-406`) i čita pri slanju (`akt_posta.go:279-305`). Promjena ne dira već ovjerene akte, ali dira svaki novi.

**Testovi prije promjene.**
- Postoji samo `TestAktOdVodomjeraDoOvjereKrozRute`: registar, Župan samo u izvanrednom stanju, redoslijed CZ, 112, policija, pismohrana zadnja, isključeni vatrogasci i stožer.
- Nedostaju:
  - cijela grana izvanrednog stanja;
  - izvođači iz registra prema zamjenskom nazivu;
  - skupina ljudi (razina, spojene funkcije, istekla dužnost);
  - dvojnici među skupinama;
  - 112 bez civilne zaštite;
  - filtri registra;
  - više županija;
  - greška bilo kojeg izvora.

**Prijedlog.**
- Izvori kao zasebne funkcije i jedan sakupljač, ali samo ako su spremišta iza malih sučelja. Inače je to **kozmetika**: niža složenost, isto ponašanje.
- Odlučiti trebaju li greške izvora postati upozorenja na aktu.

## 7. `service.(*EpisodeService).Declare`, `Raise`, `End` i `izracunaj` — `internal/service/episode_service.go:44`, `:85`, `:109`, `:213`

**Odgovornost.** Proglašenje, podizanje i prestanak obrane na dionici (operaterske epizode), i izračun epizoda iz niza očitanja.

**Pravila.**
- Sve tri traže pisanje na dionici: `HasWriteAccess("",0,code)`, dakle globalni administrator ili dionica u `AllowedSections` (`:46`, `:87`, `:111`). Rukovoditelj sektora ili područja zato ne smije.
- **Proglašenje:**
  - stupanj na snazi;
  - najviše sat unaprijed, bez donje granice (`:52-57`);
  - nema druge otvorene na lokalnoj bazi;
  - osnova se ne provjerava;
  - prag se traži u očitanjima 14 dana unatrag (`dopuniPrag`, `:138-162`) i ne dopunjava se poslije.
- **Podizanje:** samo na strogo viši stupanj (`:97-99`).
- **Prestanak:** ne prije početka. Budućnost je dopuštena, i epizoda je odmah zatvorena (`:121-126`).
- **Izračun:** epizoda počinje prvim očitanjem iznad praga, a kraj je zadnje očitanje iznad praga. I zadnja epizoda niza dobije kraj (`:231-232`).

**Duplikati.**
- Mjerodavna letva dionice definirana je na četiri mjesta:
  - `handlers_episodes.go:18-30`: prva letva po imenu, iako komentar kaže „prva upisana na pododsjecima”;
  - `handlers_sections_pages.go:180-187`: prva letva prvog pododsjeka; izvoz je naziva „mjerodavnom”;
  - na putu akta svaka letva dionice (`akt_service.go:164-167`);
  - `models.Section.AllStationIDs` (`spatial.go:274-287`).

  Za dionicu s više letava dva puta upisuju različite letve.
- Pretvorba očitanja u `dopuniPrag` i `Rebuild` (`:147-153`, `:187-193`).
- „Sat unaprijed” stoji i u `reading_service.go:150`.

**Što promjena dira.**
- Put akta (pod 4) i tri izravne rute (`server.go:1086-1088`), koje sučelje ne koristi.
- `defense_episodes` ide kroz knjigu (`episode_repo.go:194`), a jedinstvenosti otvorene epizode nema: dva čvora mogu obojica proglasiti.
- `izracunaj` zove samo `Rebuild`, a `Rebuild` nema pozivatelja. Njegov `DeleteEpisodesFrom` (`episode_repo.go:202-210`) briše bez knjige, pa bi spajanje rasporedilo čvorove.

**Testovi prije promjene.**
- Na masteru postoji samo izračun (`episode_service_test.go`). Ime `TestEpizodaNaKrajuNizaOstajeOtvorena` tvrdi da zadnja epizoda ostaje otvorena, a test to ne provjerava.
- `Raise` nema testa na masteru.
- Iz grane `stabilizacija-plan` (`obrana_zakljucano_test.go`):
  - tko smije;
  - proglašenje (stupanj, 50 i 61 min, osnova, jedna verzija u knjizi, dvostruko, nepoznata dionica, prazna letva);
  - prag u 14 dana;
  - podizanje;
  - prestanak u budućnosti;
  - otvorene obrane sektora bez identiteta;
  - izračun na rubovima.
- Nedostaju:
  - izravni rukovatelji (obrazac, `time.Local`, mjerodavna letva, modul);
  - dvije otvorene obrane na dionici nakon razmjene;
  - prag iz ranijeg vala;
  - izračun s rupama u nizu.

**Prijedlog.**
- Odlučiti o `Rebuild` i `izracunaj`: obrisati ih, ili ih spojiti uz brisanje kroz knjigu.
- Jedna funkcija `MjerodavnaLetva(dionica)` za stranicu dionice, izvoz i izravne rute.
- Vremenska pravila nazvati konstantama: sat unaprijed, prestanak u budućnosti.
- **Kozmetički** bi bilo dalje dijeliti `Declare`, `Raise` i `End` (CC 7 do 9).

## 8. `service.(*ReadingService).validate` — `internal/service/reading_service.go:143`

**Odgovornost.** Provjera očitanja prije upisa ili izmjene. Usput očitanje i mijenja: zadan izvor, obrezana napomena i očitao.

**Pravila.**
- Točno jedno od letve i objekta (`:144-146`).
- Vrijeme: ne nula, ne više od sat unaprijed (poruka kaže „budućnost”) i ne prije 1900. (`:147-155`).
- Barem jedna vrijednost, napomena, stanje objekta ili zapornica (`:156-158`).
- Vodostaj između −500 i 3000 cm, temperatura između −5 i 45 °C, protok između 0 i 100 000 m³/s (`:159-169`).
- Izvor je ručno, automatski ili uvoz; prazno znači ručno (`:170-176`).
- Nema provjere uvjerljivosti prema pragovima ili prethodnom očitanju.

**Duplikati.** Granice postoje samo ovdje. Dio provjere je u parseru obrasca (`handlers_readings.go:856-930`), a „sat unaprijed” dijeli s proglašenjem obrane.

**Što promjena dira.**
- `Create` (`:190`, provjera prije ovlasti), `Update` (`:244`) i `UveziZalijepljena` (`:486`), preko `POST /readings/create`, `POST /readings/update`, potvrde CSV-a i lijepljenja (`server.go:1154-1167`).
- Mimo nje idu:
  - `ImportBatch` (javni vodostaji, `csvlevels`, `bp16`);
  - primjena razmjene (`apply.go:691`).
- Jača provjera ovdje ne štiti podatke koji dolaze tim putem.

**Testovi prije promjene.**
- Postoji samo `TestTemperaturaIProtokKrozRute`, a i njegova greška dolazi iz parsera obrasca.
- Nedostaju:
  - svaka granica (uključive), drugi vodostaj;
  - oba ili nijedno od letve i objekta;
  - samo napomena;
  - stanje i zapornica (poznato i nepoznato);
  - zadani i nepoznati izvor;
  - 1900. i sat unaprijed;
  - obrezivanje;
  - put izmjene.

**Prijedlog.**
- Uvjerljivost kao čista provjera nad `models.Reading`, s tabličnim testovima. Neka je koriste i `ImportBatch` i uvoznici.
- **Kozmetički** bi bilo cijepati `validate` u pomoćne funkcije.

## 9. Ovlasti: `models.NewUserPermissions` i pravila u `internal/service/user_rules.go` — `internal/models/user.go:382`

**Odgovornost.** `NewUserPermissions` od dužnosti složi plosnate mape:
- uprave: globalni, sektori, područja;
- pisanja: sektori, područja, dionice;
- prikaza.

`user_rules.go` odlučuje tko smije dodijeliti, opozvati i upravljati dužnostima i računima. `UserService` i `password_reset.go` to ponovno provjeravaju pri svakoj izmjeni.

**Pravila.**
- Neaktivne i istekle dužnosti se preskaču (`user.go:397-402`). Spremište ih ionako ne učita (`user_repo.go:721`).
- Razina uprave dolazi iz uloge (`roles.go:67-78`) i upisuje se bez provjere praznog sektora i područja 0 (`user.go:405-416`).
- Doseg pisanja ide po `Duty.Doseg()`. Dionični doseg bez šifri znači cijelo područje (`user.go:456-460`).
- `HasWriteAccess` je „ili” po argumentima, a `CanAdminister` traži sektor. Nijedna ne podnosi `nil` (`:477-505`).
- `actorRank` računa samo najvišu razinu (`user_rules.go:22-34`).
- `mayAssign` uspoređuje rang uloge iz kataloga s razinom uprave (`:163`). `normalizeScope` za doseg „sve” ne provjerava dionice (`:243-299`).
- Privremena uprava daje upravne dužnosti najdulje do svog isteka (`:43-112`).
- Uloga iz obrasca se ne provjerava prema katalogu (`handlers_users.go:358`). Nepoznata uloga ima rang 5 i piše.

**Duplikati** (provjereni):
- Tri ljestvice razina (`RazinaUprave`, `Rank`, `RazinaZaUpravu`, `roles.go:67-117`). `actorRank` razinu ponovno izvodi iz mapa.
- „Aktivna i neistekla” na pet mjesta, i neusklađeno: `IsFieldUser`, `VidiVodocuvarskiDnevnik`, `PrimaryDuty`, `NositeljFunkcije` i upit za primatelje istek ne gledaju.
- „Dionični doseg bez šifri je područje” na dva mjesta (`user_rules.go:287-296`, `user.go:456-460`).
- Provjere dosega mimo pomoćnih metoda: `dutyInScope`, `canManageTarget`, `MaintenanceService.CanEdit`, `ZidService.vidi`, `smijeLetvu`, `sektorPodrucja`. Ukupno je 34 izravna čitanja mapa izvan `user_rules.go` i `password_reset.go`.

**Što promjena dira.**
- Svaki zahtjev: `AuthenticateSessionView` iz `authMiddleware` računa ovlasti.
- Oko 100 čitanja ovlasti iz konteksta u 44 datoteke weba i oko 195 funkcija servisa s `*models.UserPermissions`.
- Rute korisnika i dužnosti (`server.go:1045-1053`, `:1276`).
- Korisnici i dužnosti idu kroz knjigu, pa se na drugom čvoru ovlasti računaju iz razmijenjenih dužnosti. Sesije i pregled tuđim očima su lokalni.

**Testovi prije promjene.**
- Postoje na masteru:
  - `user_doseg_test.go`;
  - `user_rules_test.go`;
  - `ovlasti_doseg_test.go`;
  - `doseg_duznosti_test.go`;
  - `ovlasti_zaduzenja_test.go`;
  - `ista_razina_test.go`;
  - `password_reset_test.go`.

  `user_service_test.go` se bez `data/imenik.json` preskače, i u CI-ju.
- Iz grane `stabilizacija-plan`:
  - `ovlasti_zakljucano_test.go`: tablica uloga, mape uprave, istek, rubni dosezi, prazni sektor, `nil`, dnevnik, primarna dužnost;
  - `pravila_uprave_zakljucano_test.go`: mješovita uprava, dodjela, upravljanje, poništenje lozinke operateru, doseg iz uloge, rok privremene uprave.
- Nedostaju:
  - `RevokeDuty` nadređenog koji nije globalni (vlastita, izvan dosega, već opozvana — nova verzija u knjizi);
  - `UpdateUser` kad privremena uprava skida zastavicu globalnog administratora;
  - `DeleteUser` uprave sektora i područja;
  - uloga izvan kataloga kroz rukovatelja;
  - ovlasti na drugom čvoru nakon opoziva dužnosti razmjenom;
  - 34 izravna čitanja prema istim slučajevima kao `HasWriteAccess` i `CanAdminister`, da se vidi gdje se razilaze.

**Prijedlog.**
- Metode s imenom namjere, otporne na `nil`, i postupno preseljenje izravnih čitanja mapa iza njih, uz testove. To nije kozmetika, jer se danas razilaze.
- Provjeriti ulogu prema katalogu, odbiti prazan sektor i područje 0, i odlučiti o skidanju zastavice (`user_service.go:464`).
- **Kozmetički** bi bilo cijepati `user_rules.go`: 299 redaka, jedna cjelina.

## 10. `web.(*Server).authMiddleware` — `internal/web/server.go:1533`

**Odgovornost.** Provjeri kolačić sesije i propusti zahtjev kroz zajednička pravila. U kontekst stavi korisnika, stvarnog korisnika, ovlasti, oznaku pregleda tuđim očima i sesiju.

**Pravila.**
- Bez kolačića, s kolačićem koji nije UUID, ili uz bilo koju grešku provjere (i grešku baze) slijedi preusmjerenje na prijavu (`:1535-1551`). Sesija isključenog računa se ne briše.
- Obvezna promjena lozinke stvarnog korisnika propušta samo profil, odjavu i statiku (`:1556-1559`, `ratelimit.go:368-374`).
- U pregledu tuđim očima propušta se samo čitanje i `/view-as*`, osim uz `UpisTudjimOcima` (`:1563-1570`, `:1658-1664`). Tuđi sandučić je uvijek zabranjen (`:1573-1576`).
- Moduli se računaju za gledanog korisnika, a vlastiti karton prolazi bez modula (`:1584-1592`).

**Duplikati.** Kolačić, pa UUID, pa provjera sesije ponavlja se u `PairHandler.sessionView` (`handlers_pairing.go:85-99`) i `ShowLogin` (`handlers_auth.go:139-145`).

**Što promjena dira.** 409 od 434 rute (`setupRoutes`, `server.go:639`): 381 izravno i 28 kroz `samoAdmin`. Stranica uparivanja ima svoju kopiju. Ništa se ne razmjenjuje; mijenja se samo lokalni stupac `viewing_as`.

**Testovi prije promjene.**
- Postoje:
  - `viewas_test.go`;
  - `ratelimit_test.go`;
  - `vlastiti_karton_test.go`;
  - `ovlasti_rute_test.go`;
  - `sigurnost_web_test.go`;
  - `prijava_sigurnost_test.go`.

  `service/view_as_test.go` se u CI-ju preskače.
- Iz grane `stabilizacija-plan` (`autentikacija_zakljucano_test.go`):
  - bez kolačića, kriv i nepoznat kolačić, istekla sesija;
  - isključen račun;
  - obvezna promjena lozinke;
  - kontekst;
  - pregled tuđim očima kroz pravu provjeru.
- Nedostaju:
  - upis tuđim očima kad je dopušten;
  - zabrana skrivenog modula kroz pravi rukovatelj;
  - greška vidljivosti modula (500);
  - obvezna promjena lozinke gledanog korisnika;
  - `samoAdmin` sa stvarnom sesijom;
  - greška baze koja se ne razlikuje od odjave.

**Prijedlog.**
- Jedna pomoćna funkcija „kolačić u pogled”, zajednička s uparivanjem, i testovi za nepokrivene grane.
- **Kozmetički** bi bilo dalje cijepati middleware (CC 18): pravila su već male funkcije.

## 11. `importer/csvlevels.Run` — `internal/importer/csvlevels/csvlevels.go:104`

**Odgovornost.** Uvoz tablice dnevnih vodostaja centra: prvi stupac je datum, ostali su letve, ćelije su jutarnja očitanja. Stupce preslika na letve iz registra i javi poklapanja, dvosmislenosti i razlike. Bez probnog prolaza upiše samo nova očitanja i ništa ne prepisuje.

**Pravila.**
- Sat 0:00 znači 7:00 (`:106`).
- Kodiranje je UTF-8 ili Windows-1250. Razdjelnik se pogađa (`:500-559`).
- Stupac se traži redom: po nazivu, dijelu ispred zagrade ili iza crte; pa po zadanom ili ugrađenom aliasu. Objekt je ispred svojeg vodomjera (`:141-173`, `:408-474`).
- Identitet očitanja je letva i trenutak (`:208`), pa dva stupca na istu letvu daju isti identitet.
- Vrijednost: zarez je decimalni, točka s tri znamenke su tisućice; `NaN` i `1e3` prolaze (`:593-621`).
- Usporedba sa zatečenim je po danu i letvi (`:233-275`).
- Upis ide u serijama od 2000, bez transakcije preko serija (`:281-292`).

**Duplikati** (provjereni):
- Windows-1250 je ručno napisan tri puta (`:500-526`, `uvoz/his2000/his.go:198-213`, `prognoza/hydroinfo.go:244-255`), a `golang.org/x/text` je već u `go.mod`.
- Pogađanje razdjelnika postoji i u `handlers_organization.go:250-262`, bez tabulatora.
- Čitanje broja namjerno se razlikuje od `web.parseBroj`, koji „1.234” čita kao 1,234. Ne spajati naslijepo.

**Što promjena dira.** Samo naredbeni redak (`-tablica` i srodne zastavice, `main.go:449-481`), ne sučelje. Očitanja idu kroz `ImportBatch` u knjigu i razmjenom na druge čvorove.

**Testovi prije promjene.**
- Na masteru `TestTablicaDnevnihVodostaja` i `TestPreklapanjePoDanuBezObziraNaSat` traže `data/` i u CI-ju se preskaču. Rade samo testovi ćelije, datuma i kodiranja.
- Iz grane `stabilizacija-plan` (`uvoz_zakljucano_test.go`):
  - rane greške;
  - preslikavanje stupaca;
  - dva stupca na istu letvu;
  - razlike i nečitljive vrijednosti;
  - sat očitanja;
  - trag zapisa.
- Nedostaju:
  - Windows-1250, tabulator i zarez kroz `Run`;
  - Excelov redni broj datuma kroz `Run`;
  - kvaliteta i podrijetlo;
  - stupac objekta;
  - više od 2000 očitanja i greška usred serije;
  - granica dana oko ponoći i ljetnog vremena.

**Prijedlog.**
- Mali zajednički paket za čitanje CSV-a (BOM, Windows-1250 iz `x/text`, razdjelnik). Čitanje brojeva ostaje odvojeno.
- `Run` razdvojiti u čiste korake: čitanje, stupci, serija, razlike, upis. Svaki je onda provjerljiv bez baze.

## 12. `importer/ugovor.Run` i `parseTroskovnik` — `internal/importer/ugovor/ugovor.go:540` i `:108`

**Odgovornost.** Čita ugovor A.02 (dodatak Hrvatskih voda za Excel): lokacije, stavke radova i ugovorne stavke. Lokacije upari s registrom voda i nasipa područja. Bez probnog prolaza upiše popis lokacija, stvori vode i nasipe kojih nema i doda nove stavke radova, bez cijena.

**Pravila.**
- Područje iz `PPI_POSTAVKE` ili iz prve pozicije plana. Pozicija drugog područja je greška (`:74-96`, `:121-132`).
- Stavka je oznaka, opis i jedinica, a ponavljanja se broje (`:153-170`).
- Nasip se prepoznaje po nazivu ili po objektu „Nasip” u troškovniku (`:266-277`).
- Uparivanje (`resolve`, `:406-471`):
  - ključ s vrstom, pa bez vrste;
  - među više kandidata pobjeđuje pojašnjenje koje odgovara području, pa voda iz Odluke (`pick`, `:373-391`);
  - inače dvoznačno ili prijedlog.
- Zadana veza na nepostojeću šifru prekida cijeli uvoz (`:575-580`).
- Upis lokacije ide za svaku lokaciju, i prepiše ručnu vezu (`:640-651`). Nema transakcije oko cijelog uvoza.

**Duplikati** (provjereni):
- Stvaranje nove lokacije ponovljeno je u `bp16.RunJournals` (`journals.go:346-382`): isti kod nasipa `bp%d-…`, isti nastavak `-bp%d` pri sudaru, ista podjela vrste s početka naziva. Razlikuju se u kodu vode (`WatercourseCode` prema `Slug`) i u podrijetlu.
- `MatchKey` dijeli s `RunJournals`. Identitet lokacije je ipak sirovi naziv (`MaintainedWaterID`, `maintenance_repo.go:212-214`).
- `MaintenanceService.LinkWater` i `AddWater` detektor javlja kao točan par.

**Što promjena dira.**
- Naredbeni redak (`-ugovor`, `main.go:485-522`).
- `POST /odrzavanje/uvoz` i `POST /odrzavanje/uvoz/upisi` (`server.go:847-848`), preko `MaintenanceService.ImportContract`. Ta provjerava pripada li ugovor odabranom području tek nakon upisa (`maintenance_service.go:145-147`); to je već zasebno prijavljeno.
- Sve što piše ide kroz knjigu: vode, objekti, lokacije, stavke.

**Testovi prije promjene.**
- Na masteru `TestUvozUgovora` traži `data/` i u CI-ju se preskače. Radi samo `TestKljucevi`.
- Iz grane `stabilizacija-plan` (`uvoz_zakljucano_test.go`, na izmišljenom ugovoru i registru):
  - greške radne knjige i područje;
  - stavke i ponudbeni troškovnik;
  - razvrstavanje i nasipi;
  - uparivanje: postoji, novo, prijedlog, dvoznačno;
  - ponovni uvoz koji briše ručnu vezu;
  - zadana veza i nepoznata šifra;
  - nepoznato područje;
  - pad `pick` bez naziva područja.
- Nedostaju:
  - sudar šifre nove vode;
  - prijedlozi po „ – ” i „ i ”;
  - dvoznačni nasipi;
  - dva nasipa istog imena u jednom ugovoru;
  - djelomičan upis pri grešci;
  - `ImportContract` s drugim područjem i bez prava.

**Prijedlog.**
- Provjeru područja premjestiti u `ugovor.Run`, prije ikakvog upisa, i razmotriti jednu transakciju.
- Odvojiti uparivanje od upisa i popraviti pad `pick`.
- **Kozmetički** bi bilo cijepati `parseTroskovnik` (CC 26, ravna sklopka po oznakama): zaključati testom i ostaviti.

## 13. `importer/bp16.RunJournals` i `bp16.Run` — `internal/importer/bp16/journals.go:242` i `bp16.go:368`

**Odluka (4. 10. 2026.):** stara evidencija još nije pregledana do kraja, pa uvoz ostaje, ali je **predviđen za brisanje** (paket `internal/importer/bp16` i zastavice `-import-bp16*`). Ne ulaže se u preuređenje ni u nove testove osim onih koji sprečavaju pad.

**Odgovornost.** Preuzimanje iz stare evidencije VGI Baranja (Directus, BP 16):
- `Run` uvozi očitanja crpnih stanica, ustava i letava;
- `RunJournals` iz evidencije radova A.02 i A.03 slaže rekonstruirane građevinske dnevnike, jedan po programu i godini.

**Pravila.**
- Samo područje 16 i sektor B, upisano u kod (`bp16.go:375`, `:666-701`; `main.go:556`, `:660`).
- Ponovni `Run` preskače već uvezeno (`:499-523`).
- U dnevnicima: list po danu i po šest zapisa izvođača; nadzor ne zauzima mjesto (`journals.go:525-542`).
- Zapis bez datuma ruši uvoz (`:430`).

**Duplikati.**
- `RunObilasci` i `RunPrijave` detektor javlja kao točan par: isti uvod i karta korisnika, a treći put u `RunJournals`.
- HTTP s tokenom je napisan tri puta.
- Stvaranje lokacija kao u ugovoru (pod 12).

**Što promjena dira.** Samo naredbeni redak (`-import-bp16`, `-import-bp16-dnevnici` i srodne, `main.go:525-689`), ne sučelje. Upisano ide kroz knjigu.

**Testovi prije promjene.**
- `TestUvozBP16` traži `data/`. `RunJournals` nema nijedan test.
- Iz grane `stabilizacija-plan` zaključane su samo pomoćne funkcije i pad na zapisu bez datuma (`pomocne_zakljucano_test.go`).

**Prijedlog.** Brojke su ovdje najgore (CC 82 i 55, 0 %), a rizik malen: to je alat za prelazak sa starog sustava, vezan uz jedno područje.
- Ako je prelazak gotov, alat maknuti iz glavnog programa (zaseban `cmd` ili oznaka gradnje).
- Ako nije, zaključati ga testovima nad `DirSource` i ne preuređivati, osim pada na datumu.
- **Kozmetički** bi bilo izdvajati zajedničke pomoćne funkcije.

## 14. `main` — `cmd/gocop/main.go:99`

**Odgovornost.** Oko 1160 redaka:
- zastavice i `gocop.toml`;
- otvaranje baze, knjige, razmjene i svih spremišta i servisa;
- jednokratni načini: verzija, priprema, poništenje lozinke, tablica, ugovor, bp16;
- pokretanje poslužitelja i pozadinskih poslova.

Najveća složenost i najveći churn u repozitoriju (19 commitova).

**Što nije samo spajanje.**
- Cijeli lanac prognoze: uloga izdavača, `NakonPreuzimanja` i priprema modela (`:873-1173`), uključujući zapis „Generiraj” bez `Iznova` (`:1120`).
- Karta korisnika i područja za bp16 (`:544-549`, `:594-617`).
- Tri gotovo ista dohvata tuđih prognoza (`:987-1028`).
- Tri zatvaranja za otključavanje pošte (`:951-962`, `:1180-1197`, `:1202-1213`).
- Zamjenski port 8080 (`:1239-1247`).

**Testovi prije promjene.**
- `cmd/gocop` ima testove pripreme, poništenja lozinke, razmjene izdanja i jedan koji gradi program (verzija, priprema, `/zdravlje`, gašenje).
- Sam `main` u procesu ima 0 %.
- Nedostaju:
  - prednost zastavice pred `gocop.toml`;
  - svaki jednokratni način završava prije poslužitelja;
  - `-upisi` kao suprotnost probnom prolazu za svaki uvoznik;
  - grane prognoze: čvor koji ne izdaje, isti sat, vrh koji kasni.

**Prijedlog.**
- Lanac prognoze premjestiti u tip s metodama, da se grane provjere testom.
- Jednokratne načine premjestiti u zasebnu datoteku.
- Ukloniti tri ponovljena bloka.
- **Kozmetički** bi bilo premještati stvaranje spremišta, servisa i `Set*` poziva (`:399-445`, `:711-824`): to je spajanje sustava i manja brojka ne smanjuje rizik.

## 15. `service.(*opisivac).opisi` — `internal/service/zid_service.go:363`

**Odgovornost.** Jednu verziju iz knjige pretvara u događaj na zidu aktivnosti ili odluči da se ne prikazuje. Čita samo knjigu, pa prikazuje i promjene s drugih čvorova.

**Zašto je na popisu.** CC 104 i 27 % pokrivenosti, na naslovnoj stranici svakog korisnika (`GET /`, `/dashboard`, `/dogadjanja`, `/dogadjanja.xlsx`).

**Što je upitno.**
- Grana za prijave je mrtva: `entitetiModula` (`:116-124`) nema `EntityPrijave`, a `Recent` za prazan popis vraća ništa (`ledger.go:547-549`).
- Za svako očitanje iznad praga čitaju se sve dionice, bez spremnika (`:576-608`). Izvoz s 2000 događaja to radi do 2000 puta.
- Doseg prikaza (`vidi`, `:214-245`) ponovno piše provjere dosega mimo ovlasti.

**Testovi prije promjene.**
- Postoje `TestZidIzKnjige` i `TestNaslovnaIDogadjanjaKrozRute`.
- Nedostaju (oko 73 % naredbi):
  - stornirani i izmijenjeni zapisi;
  - dežurstva;
  - izvješća;
  - epizode obrane;
  - popisi i potrebe;
  - očitanja iznad praga;
  - nove dionice, letve i objekti;
  - grane `vidi`.

**Prijedlog.**
- Sklopku od 14 slučajeva zamijeniti kartom „entitet → opisivač”. Ukupna složenost ostaje ista, pa je to dijelom **kozmetika**, ali svaki se opisivač može testirati sam.
- Odlučiti o grani prijava.
- Dionice učitati jednom po `opisivac`.

## Što je ostalo izvan izbora

- `repository.applyOne` i `removeFromSurface`, `peers.exchange` i `razmjena`: izostavljeni namjerno (gore).
- `ReadingService.FieldOverview` (CC 34, bez ijednog testa): pregled za teren, samo čitanje. Neprovjeren `?area=` i „moje letve” po imenu rizik su prikaza, ne podataka.
- `web.crtajUzduzni` (CC 107, 96 %): pokriven crtež. Brojka je signal za održavanje, ne za rizik.
- `PrognozeHandler.listSazetka`, `SeedInitialData`, `pdfw.Dodaj`, `posta.PokreniProbniEWS`: velik CRAP, ali prikaz, prvo punjenje ili probni alat, bez utjecaja na stanje obrane, ovlasti ili razmjenu.

## Odluke (4. 10. 2026.)

- **Stadiji obrane se slažu.** Pripremno stanje, redovna i izvanredna obrana te izvanredno stanje proglašavaju se prema gore i ukidaju obrnutim redom: kad vrijedi pripremno stanje pa se proglasi redovna obrana i kasnije ukine, pripremno i dalje vrijedi dok se i ono ne ukine. Veći stadij smije se proglasiti odmah, bez prethodnih, kad se zna da dolazi velika opasnost. Stanje obrane dionice je najviši stadij koji je proglašen, a nije ukinut. (Stavke 11 i 13 sumnjivog ponašanja, riješene u 0.0.34 izračunom iz akata.)
- **Akt stupa na snagu prema vremenu koje u njemu piše**, i kad je ovjeren ranije (stavka 12). Do tada stanje obrane ostaje kakvo jest.
- **Uvoz iz stare evidencije (BP16)** ostaje dok se ne pregleda, predviđen za brisanje (pod 13).
- **Riješeno u 0.0.34:** izravne rute obrane (`POST /sections/{code}/obrana/*`) uklonjene su; doseg 0 više se ne ispravlja promašajima niti ulazi u glačanje susjednih dosega.

## Nestabilno mjerenje

Coverage nekih funkcija mijenja se od pokretanja do pokretanja bez promjene koda i testova, pa `make quality` može javiti lažnu regresiju. Takav test treba učiniti determinističkim (grana se pogađa namjerno, a ne slučajno); baseline se zbog toga ne prihvaća.

- `internal/service/drugi_korak.go`: `rezervirajUnos` (88,9 % ↔ 77,8 %), `ProvjeriKod` (74–82 %), `JaviPromjenuAdrese` i `posalji`. Uzrok još nije nađen.
- `internal/razmjena`: `Dial` i `DialExchange` — grana greške spajanja pogađala se samo kad test nazove prije slušalice. Riješeno testovima nazivanja bez slušalice.

## Zaostaci faza 1 i 2 (6. 10. 2026.)

Stavke su označene prema izvoru: F1-n je stavka n „Sumnjivog ponašanja” iz
PR-a #4 (faza 1), F2-p.n stavka n iz PR-a #p (faza 2). E, S, R, T i U su
nalazi iz ovog plana, iz pregleda popravaka i iz njihovih pregleda. Svaka
ispravka ima test koji bez nje pada, a zaključani testovi
(`*_zakljucano_test.go`) okrenuti su na ispravno ponašanje.

**Već ispravljeno u 0.0.34:** F1-3, F1-8, F1-10 do F1-13, F1-14, F1-19, F1-20,
F1-22, F1-25, F2-5.1, F2-5.3, F2-6.1, F2-8.1, F2-8.2, F2-8.6, F2-9.1, F2-9.2,
F2-9.4, F2-10.1 i F2-10.2.

**Ispravljeno sada:**

| Područje | Stavke | Što je ispravljeno |
|---|---|---|
| Ovlasti | F1-1, F1-2, F1-5, F1-6, F1-7, F2-10.3 | doseg uprave je unija svih upravnih dužnosti; uprava praznog sektora ili područja 0 ne broji se; primarna dužnost je prva aktivna i neistekla; `nil` ovlasti ne padaju; rok privremene uprave i bez zadanog cilja; zabranjena promjena vlastitog računa i zastavice odbija se porukom, a ne tiho |
| Vodočuvarski dnevnik | F1-4, R-1, R-2, R-3 | dnevnik ne vide gost, preglednik, nepoznata uloga ni istekla dužnost; parafa, upis i zadatak samo uz pravo čitanja, po dužnosti (i istek kod ovjere); operater i poslovođa zadržani do odluke; vlastito korisničko ime samo za čitanje |
| Vodočuvarski list | F2-5.2, F2-5.4, F2-5.6, F2-5.7, F2-5.8, R-7, T-3, T-5, U-1, U-2, U-5 | predaja je jedna cjelina; zadatak zaključen drugim listom bilježi se kako stoji u evidenciji; nepoznato stanje zadatka odbija se; greška čitanja zadataka se javlja; radno vrijeme HH:MM; istodobne predaje ne zaključuju zadatak ni list dvaput; nacrt, upis rukovoditelja i brisanje ne diraju list predan u međuvremenu (brisanje lista i izvornika u jednoj transakciji); isključeni račun ne prima zadatke ni mimo popisa |
| Obrana i akti | F1-15, F1-16, F1-17, F1-21, E-1, R-4, R-5, S-6, S-8, S-10, T-1, T-2, U-3 | epizoda bez letve se odbija; računata epizoda na kraju niza ostaje otvorena; otvorene obrane sektora s identitetom; istekle dužnosti nisu među primateljima akta; povijest obrane za akt koji stupa na snagu kasnije izvodi krug čvora (pod bravom s ovjerom i stornom, s ponavljanjem greške najviše dan); prekid se veže samo na neponišten, neprekinut akt uspostave, i pri ovjeri nacrta; zadana dionica izvan letve ili registra je greška; obrazac nudi samo letve koje priprema prihvaća |
| Prognoza | F1-23, F1-24, F1-26, S-1, S-5 | promašaji po veličini; nova provjera zamjenjuje sve promašaje letve; svježina tuđe prognoze po satu izdanja; rezervni dnevni model kad glavni ne da dvije točke; testovi vraćaju globalne postavke |
| Uvozi | F1-27 do F1-33, R-6, S-21, T-4, T-7, U-4 | dva stupca na istu letvu su dvosmislena; ponoć se može zadati; samo konačni brojevi bez eksponenta i u rasponu −500..3000 cm (izvan raspona broji se zasebno); pad `pick` i gubitak ručne veze lokacije; zapis BP16 bez datuma se preskače; retci teksta ostaju; poništena serija (i pali upis kišomjera) ne broji se kao upisana |
| Očitanja | F2-6.2, F2-6.4, F2-6.5, F2-6.6, F2-6.8, F2-6.9 | stanje objekta i zapornica samo uz objekt; terenski pregled samo u dopuštenom području; navike po računu, ne po imenu; uobičajeno vrijeme kružnom sredinom; prvo prava, pa unos; popis područja čita prazan podcentar |
| Dionice, dežurstva, MTS | F2-7.1, F2-7.2, F2-7.3, F2-8.3, F2-8.4, F2-11.2 | šifra dionice mora odgovarati području i sektoru; premještanje izmjenom se odbija; greška čitanja pri prijedlogu šifre se javlja; predaja dežurstva u punim minutama; rad u sektoru po dionici iz registra; objekt MTS-a mora biti u registru |
| Provjera prijave | S-17 | greška baze nije odjava: zapis u dnevniku i 500 |
| Dokumenti | F1-P8 | SA4009 više nije blokada u `CODE_QUALITY_BASELINE.md` |

F1-15 i F1-16 ispravljeni su u kodu koji od 0.0.34 nema proizvodnog
pozivatelja (`EpisodeService.Declare`, `Raise`, `End`, `Rebuild`); vidi
otvoreno pitanje 14.

**Otvorena pitanja.** Kod nije mijenjan. Uz svako je preporuka.

1. *Operater i poslovođa* (F1-4): smiju li dežurni operater i poslovođa
   izvođača vidjeti vodočuvarski dnevnik i prijave s terena? Do odluke vide
   oboje kao prije. Preporuka: operater vidi prijave s terena (prati ih u
   obrani), dnevnik ne mora; poslovođa ni jedno, osim ako izvođač treba
   prijave svog područja.
2. *Pregled tuđim očima isključenog računa* (F1-9). Preporuka: dopušten, ali
   samo za čitanje i kad je upis tuđim očima uključen.
3. *Poništenje lozinke operateru* (F1-P5): danas samo razina 1. Preporuka:
   uprava sektora smije operateru svog sektora (za istu razinu broje se samo
   dužnosti koje upravljaju).
4. *Broj akta među čvorovima* (F1-18). Preporuka: dodjela broja u istoj
   transakciji s upisom i jedinstven ključ (sektor, godina, broj) na čvoru;
   dvostruki broj s drugog čvora otkriti pri razmjeni i pokazati u popisu.
5. *List za budući dan* (F2-5.5). Preporuka: nacrt najviše 30 dana unaprijed,
   predaja tek kad dan počne; isto za upis rukovoditelja.
6. *Redni broj lista* (F2-5.Q2). Preporuka: ostaje redoslijed upisa; knjiga i
   ispis slažu po datumu i označe naknadno upisane listove.
7. *Dva lista za isti dan* (F2-5.Q3). Preporuka: identitet novog lista iz
   (osoba, dan), poslije jedinstven indeks; postojeće dvojnike prijaviti, ne
   brisati.
8. *Letva bez dionica* (F2-6.3). Preporuka: letva dobiva neobavezan „sektor
   koji je prati”; do tada upis samo uprave.
9. *Izmjena očitanja nakon gubitka prava* (F2-6.7). Preporuka: autor mijenja
   još 72 h, ne briše.
10. *Nepoznato mjesto dežurstva* (F2-8.5). Preporuka: u obračunu zasebno („za
    provjeru”), ne potvrđuje se dok uprava ne izabere opis.
11. *Prijenos u skladište drugog sektora* (F2-11.1). Preporuka: ostaje
    slobodan; kasnije potvrda primitka.
12. *Predano izvješće sektora* (F2-9.3). Preporuka: izmjena ga vraća u nacrt i
    traži novu predaju.
13. *Testovi koji traže `data/` i stvarna imena u testovima* (F1-P7): 54
    testa CI preskače, a neki testovi nose imena stvarnih djelatnika.
    Preporuka: service, importeri i peers na izmišljeni testni skup; provjere
    stvarnih registara u `db` ostaju na `data/`.
14. *Mrtav kod epizoda*: `Declare`, `Raise`, `End` i `Rebuild` (s
    `DeleteEpisodesFrom`, koji briše mimo knjige) nemaju pozivatelja.
    Preporuka: ukloniti.
15. *Dnevni modeli*: uče se jednom na dan, a neuspjelo učenje ostaje do
    ponoći. Preporuka: neuspjeh ponoviti nakon sat vremena, ključ po danu u
    Zagrebu.
16. *Promašaji na rezervnoj inačici* u provjeri unatrag primjenjuju se, a u
    živoj prognozi ne. Preporuka: uskladiti s živom prognozom.
17. *Vrijeme akta bez granica* (moguće 1900. ili 2099.). Preporuka: granica,
    npr. najviše godinu unatrag i 30 dana unaprijed.
18. *Vodočuvar jedne dionice priprema akt za sve dionice letve* (namjerno,
    primjer Vukovara). Preporuka: potvrditi kao pravilo.
19. *Greške izvora primatelja akta* tiho se preskaču. Preporuka: upozorenje
    pri pripremi, bez zaustavljanja.
20. *Prijave s terena na zidu*: grana opisa postoji, ali se nikad ne
    pokazuju. Preporuka: odlučiti trebaju li na zid.
21. *Granice vrijednosti za ostale uvoze* (tablica centra, BP16, javni
    izvori). Preporuka: vrijednosti izvan raspona javnih izvora voditi kao
    kvar izvora, ne odbacivati tiho.

**Poznato, za kasnije:** uvoz ugovora upisuje prije provjere područja (korak 2
faze 3); storno koji stigne razmjenom primjenjuje se izvan brave povijesti;
provjera sesije na stranici prijave i uparivanja guta grešku baze; ponovni
uvoz ugovora šalje novi trenutak nastanka vode u knjigu; MTS ne provjerava je
li objekt u zadanom području ni je li ciljno skladište aktivno; sat uvoza
tablice nema provjeru raspona; dvije istodobne parafe ili parafa i ovjera
mogu jedna drugoj prepisati upis; `IsFieldUser` ne gleda istek; nestabilno mjerenje pokrivenosti u
`drugi_korak.go`; dvije zatečeno neformatirane testne datoteke.

## Faza 3

Kandidati za fazu 3 (CRAP > 30 i coverage < 80 %, mjerenje mastera `713d9df`) su u [`STABILIZACIJA-faza3.md`](STABILIZACIJA-faza3.md). Prvo testovi; preuređenje se predlaže tek ako CC/CRAP i dalje ostane visok, u zasebnom commitu bez promjene ponašanja, a ne radi se kad samo spušta brojku i otežava čitanje.

## Redoslijed za prvih pet

Redoslijed ide od najmanjeg rizika prema najvećem dosegu. Svaki korak je zaseban PR, s testovima iz „Testovi prije promjene” dopisanima prije same promjene. Odluke označene kao poslovne treba donijeti prije koraka, ne usput.

| # | Korak | Rizik | Što može poći krivo | Kako se vidi |
|---|---|---|---|---|
| 1 | Odluka po dionici pri ovjeri akta kao čista funkcija s cijelom tablicom (pod 4) | nizak | promijeni se koja se radnja izvodi za neki par akta i otvorene obrane | tablični test 2 × 4 × 4; postojeći web testovi ovjere |
| 2 | Uvoz ugovora: provjera područja prije upisa, uparivanje odvojeno od upisa, pad `pick` (pod 12) | nizak | pogrešna lokacija veže se na drugu vodu; djelomičan upis | testovi ugovora iz grane; ponovni uvoz na kopiji baze s pravim ugovorom |
| 3 | Provjera unatrag: mjerenje odvojeno od ispisa i spremanja, `trebaniProvjere` napolje (pod 3) | srednji | drukčiji zapisani promašaji, pa drukčiji rasponi na svim čvorovima nakon razmjene modela | testovi stalne vode iz grane; usporedba tablice `promasaji` prije i poslije na istoj arhivi |
| 4 | Osvježavanje: jedna funkcija za budućnost vrhova i zajednička primjena promašaja (pod 1 i 2) | srednji | prognoza drugih brojeva ili vrh bez budućnosti; razlika između izdavača i primatelja | novi testovi redoslijeda izvora i `Ceka`; usporedba dvaju izdanja na istoj bazi |
| 5 | Ovlasti: metode s imenom namjere i preseljenje izravnih čitanja mapa, datoteku po datoteku (pod 9) | srednji do visok | netko dobije ili izgubi pravo; doseg je svaki zahtjev | testovi ovlasti iz grane; za svako preseljeno mjesto test slučajeva u kojima se danas razilazi od `HasWriteAccess` |

**Zašto tim redom.**
- **Koraci 1 i 2** su mali i imaju testove. Mijenjaju po jednu funkciju, a pravila koja diraju (stanje obrane, lokacije održavanja) vide se odmah u izvješću testa.
  - Korak 1 otvara pitanje prekida koji nije pripremni (popis „Sumnjivo ponašanje” u PR-u). Odluku treba donijeti prije koraka.
  - Korak 2 usput zatvara već prijavljenu grešku s područjem ugovora A.02.
- **Koraci 3 i 4** mijenjaju brojeve koje dežurni čita i koji razmjenom idu na sve čvorove. Zato idu tek s usporedbom prije i poslije na istoj bazi.
  - Prije koraka 3 treba odlučiti o dosegu 0 i glačanju. Inače preuređenje ili zadrži grešku, ili je popravi usput i bez traga.
  - Korak 4 traži testnu arhivu (`spoj`, `hq_krivulje`), koje danas nema.
- **Korak 5** ima najveći doseg: oko 100 čitanja ovlasti u webu, oko 195 funkcija servisa, svaki zahtjev. Zato ide zadnji i u malim komadima, nakon što je odlučeno o nepoznatoj ulozi, praznom sektoru i skidanju zastavice globalnog administratora.

`main`, `bp16` i `opisi` nisu među prvih pet. Brojke su im najgore, ali preuređenje ne smanjuje rizik za stanje obrane, ovlasti ili razmjenu. Za `bp16` prvo treba odluka je li prelazak sa stare evidencije gotov.
