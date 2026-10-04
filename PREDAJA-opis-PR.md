Testovi koji zaključavaju sadašnje ponašanje i plan stabilizacije (`docs/STABILIZACIJA.md`). Produkcijski kod nije mijenjan, nema novog izdanja ni novog baselinea, a `quality/`, `dev/quality`, `.golangci.yml`, `internal/repository/apply.go`, `internal/peers` i `internal/razmjena` nisu dirani.

Testovi bilježe ponašanje kakvo jest, i ondje gdje izgleda pogrešno. Takva mjesta su u testu označena komentarom i popisana ispod, pod „Sumnjivo ponašanje”; nijedno nije popravljeno. Jedina osoba u testovima je Pero Perić (`pperic`), a sektor, područje, dionice, letve i vode su izmišljeni (sektor P, područje 1, „Primjerica”, „Probno”…).

## Novi testovi po području

### Ovlasti (RBAC)
- `internal/models/ovlasti_zakljucano_test.go`:
  - razine uloga (`RazinaUprave`, `Rank`, `NaturalScope`, `Writes`, `RazinaZaUpravu`) za sve uloge i za nepoznatu ulogu;
  - neaktivne i istekle dužnosti;
  - pisanje po dosegu i rubni slučajevi (prazni sektor, područje 0, nil ovlasti);
  - zastavica globalnog administratora;
  - skladište, vodočuvarski dnevnik, primarna dužnost, doseg dužnosti.
- `internal/service/pravila_uprave_zakljucano_test.go`:
  - razina uprave kad netko ima više upravnih dužnosti;
  - dodjela dužnosti i upravljanje tuđim računom;
  - poništenje lozinke operateru;
  - doseg iz uloge;
  - rok privremene uprave.
- `internal/web/autentikacija_zakljucano_test.go`: `authMiddleware` kroz pravu provjeru sesije:
  - bez kolačića, s kolačićem koji nije UUID, s nepoznatom i s isteklom sesijom;
  - isključen račun;
  - obvezna promjena lozinke i putanje koje su od nje izuzete;
  - kontekst koji rukovatelj dobiva;
  - pregled tuđim očima: zabrana upisa, izlaz iz pregleda, isključen gledani račun, gubitak uprave.

### Obrana
- `internal/service/obrana_zakljucano_test.go`:
  - tko smije proglasiti obranu na dionici;
  - proglašenje (stupanj, najviše sat unaprijed, jedna verzija u knjizi, dvostruko proglašenje);
  - prag prijeđen po očitanjima (14 dana unatrag);
  - podizanje stupnja, prekid obrane;
  - otvorene obrane sektora;
  - izračunate epizode na rubovima niza.
- `internal/service/akt_obrana_zakljucano_test.go`: ovjera akta o obrani:
  - otvara i podiže obranu na svim dionicama akta, i kad onaj tko ovjerava ne piše izravno na dionici;
  - niži ili isti stupanj ne mijenja ništa;
  - prekid redovne samo upozorava, a prekid pripremnog završava obranu;
  - akt za dva sata ovjeri se bez obrane;
  - ovjera bez ovlasti, dvostruka ovjera i neispravan zahtjev.
- `internal/models/obrana_zakljucano_test.go`:
  - stupanj obrane na pragu 0, na negativnim, jednakim i krivo poredanim pragovima, i kad je zadan samo rekord ili samo tekst;
  - nepoznati stupnjevi;
  - trajanje epizode u danima.

### Prognoza (bez mreže, na ugrađenoj arhivi)
- `internal/prognoza/provjera_zakljucano_test.go`, na stalnoj vodi gdje je promašaj računa poznat (5 cm):
  - `ProvjeriUnatrag` (`korak`, prazna baza, zapisani promašaji po dosegu, postojanost, CSV, premalo slučaja, glačanje);
  - `Osvjezi` (prazna baza, zadanih 96 sati, vrh lanca, primjena zapisanih promašaja);
  - čitanje promašaja kad letva ima dvije veličine.

### Uvozi (bez `data/`, na bazi koju test sam složi)
- `internal/importer/csvlevels/uvoz_zakljucano_test.go`: `csvlevels.Run`:
  - rane greške;
  - preslikavanje stupaca: voda i naziv, zadani i nepoznati alias, dvosmisleni naziv, objekt ispred vodomjera, preskočeni stupci;
  - dva stupca na istu letvu;
  - probni prolaz prema upisu;
  - razlike prema zatečenom očitanju;
  - nečitljivi datumi i vrijednosti;
  - sat očitanja.
- `internal/importer/ugovor/uvoz_zakljucano_test.go`: ugovor A.02, na izmišljenom ugovoru:
  - greške u radnoj knjizi i područje;
  - stavke i ponudbeni troškovnik;
  - razvrstavanje lokacija i nasipi;
  - uparivanje (postoji, novo, prijedlog, dvoznačno, zadana veza);
  - ponovni uvoz;
  - nepoznato područje.
- `internal/importer/bp16/pomocne_zakljucano_test.go`: pomoćne funkcije evidencije Baranje:
  - `LoadEnv`;
  - stanje i zapornica;
  - vrijeme očitanja, napomena i izvor;
  - brojevi i tekst, prilike, razvrstavanje vode;
  - pad uvoza dnevnika na zapisu bez datuma.

Postojeći testovi `csvlevels`, `ugovor` i `bp16` traže `data/` i u CI-ju se preskaču; novi rade bez nje.

## make quality: prije i poslije

__TABLICA__

## Sumnjivo ponašanje

Nijedno nije popravljeno. Svako je zaključano testom (naveden uz stavku), osim onih označenih „iz čitanja koda”. Redci su s mastera (0.0.33-alfa, 713d9df).

### Ovlasti
1. **`internal/service/user_rules.go:22-34`** (`actorRank`):
   - **Što se događa:** računa se samo najviša upravna razina. Uprava sektora D koja je i uprava područja 16 (sektor B) upravlja kao razina 2, pa područje 16 ispada iz njezina dosega.
   - **Što bi trebalo:** doseg uprave je unija svih upravnih dužnosti.
   - **Test:** `TestRazinaUpraveActora`.
2. **`internal/models/user.go:405-416`** (`NewUserPermissions`):
   - **Što se događa:** uprava sektora s praznim sektorom upiše `AdminSectors[""]`, a uprava područja s područjem 0 upiše `AdminAreas[0]`.
   - **Što bi trebalo:** takva se dužnost odbija pri upisu ili se ne broji.
   - **Test:** `TestUpravaBezProvjerePraznihVrijednosti`.
3. **`internal/models/roles.go:92`, `:145`, `:163`**:
   - **Što se događa:** nepoznata uloga (npr. stigla razmjenom sa starijeg čvora) ima rang 5 i doseg dionice, a `Writes()` je `true`. Svaka uprava je smije dodijeliti, a nositelj piše.
   - **Što bi trebalo:** nepoznata uloga ne daje nikakva prava.
   - **Testovi:** `TestRazineUloga`, `TestDodjelaRubniSlucajevi`.
4. **`internal/models/user.go:242`** (`VidiVodocuvarskiDnevnik`):
   - **Što se događa:** komentar kaže „vodočuvar u svoj, rukovoditelji, ovlaštenici i uprava u tuđe”, ali dnevnik vide i gost, promatrač, operater i nepoznata uloga. Istek dužnosti se ne gleda.
   - **Što bi trebalo:** kao u komentaru, samo s aktivnim i neisteklim dužnostima.
   - **Test:** `TestTerenIVodocuvarskiDnevnik`.
5. **`internal/models/user.go:203`** (`PrimaryDuty`):
   - **Što se događa:** kad nema aktivne primarne dužnosti, a prva dužnost nije aktivna, vraća `nil`, iako osoba ima drugu aktivnu dužnost.
   - **Što bi trebalo:** prva aktivna dužnost.
   - **Test:** `TestPrimarnaDuznostIUloga`.
6. **`internal/models/user.go:477`, `:494`**:
   - **Što se događa:** `HasWriteAccess` i `CanAdminister` padnu na `nil` ovlastima.
   - **Što bi trebalo:** `false`, kao u `actorRank`.
   - **Test:** `TestNilOvlasti`.
7. **`internal/service/user_rules.go:43-80`** (`rokUprave`):
   - **Što se događa:** uprava područja bez zadanog ciljnog područja ne nađe svoju dužnost i vrati `nil` („stalna”), iako je jedina uprava privremena.
   - **Što bi trebalo:** rok privremene uprave i kad cilj nije zadan.
   - **Test:** `TestRokPrivremeneUprave`.
8. **`internal/web/server.go:1547-1551`** (`authMiddleware`):
   - **Što se događa:** isključen račun dobije preusmjerenje na prijavu, ali sesija ostaje u bazi, a kolačić se ne briše.
   - **Što bi trebalo:** sesija se briše i kolačić poništava, kao pri odjavi.
   - **Test:** `TestProvjeraPrijavePrijeRukovatelja`.
9. **`internal/service/auth_service.go:245-260`** (`StartViewingAs`):
   - **Što se događa:** cilj pregleda tuđim očima smije biti isključen račun.
   - **Što bi trebalo:** vidi otvorena pitanja; možda je namjerno.
   - **Test:** `TestTudjimOcimaKrozProvjeru`.

10. **`internal/service/user_service.go:464-467`** (`UpdateUser`, iz čitanja koda):
    - **Što se događa:** zastavicu globalnog administratora smije postaviti samo stalna uprava organizacije, ali skinuti je smije i privremena (npr. zamjenik s dužnošću razine 1 koja ističe). Privremena uprava tako može oduzeti zastavicu stalnoj.
    - **Što bi trebalo:** isto pravilo za postavljanje i skidanje, ili izričita odluka da je skidanje slobodno. Testa nema.

### Obrana
11. **`internal/service/akt_service.go:717-718`**:
    - **Što se događa:** ovjeren akt o prekidu stupnja koji nije pripremni (npr. prekid redovne obrane) zove `Raise` s istim stupnjem. To uvijek javi „obrana je već na stupnju”, pa za svaku dionicu ostane samo upozorenje, a stanje se ne mijenja.
    - **Što bi trebalo:** spustiti obranu na stupanj koji Plan propisuje nakon prekida, ili akt odbiti.
    - **Test:** `TestOvjeraAktaMijenjaStanjeObrane`.
12. **`internal/service/episode_service.go:55`, s ovjerom u `akt_service.go:666`**:
    - **Što se događa:** akt koji vrijedi više od sat unaprijed se ovjeri, ali obrana se ne otvori (samo upozorenje „unaprijed”), i ništa je poslije ne otvara.
    - **Što bi trebalo:** ili odbiti ovjeru, ili otvoriti obranu kad akt stupi na snagu.
    - **Test:** `TestOvjeraAktaMijenjaStanjeObrane`.
13. **`internal/service/episode_service.go:46`, `:87`, `:111`**:
    - **Što se događa:** izravno proglašenje, podizanje i prestanak obrane traže pisanje na dionici. Rukovoditelj sektora ili područja to ne može, a ovjerom akta iste promjene napravi.
    - **Što bi trebalo:** isto pravilo na oba puta.
    - **Testovi:** `TestObranuNaDioniciProglasavaSamoOnajTkoPiseNaDionici`, `TestOvjeraAktaMijenjaStanjeObrane`.
14. **`internal/service/episode_service.go:109-133`** (`End`):
    - **Što se događa:** prestanak obrane prima vrijeme u budućnosti.
    - **Što bi trebalo:** isto ograničenje kao proglašenje (najviše sat unaprijed).
    - **Test:** `TestPrekidObrane`.
15. **`internal/service/episode_service.go:68`** (`Declare`):
    - **Što se događa:** s praznom letvom upiše se epizoda s letvom `00000000-0000-0000-0000-000000000000`.
    - **Što bi trebalo:** letva bez identiteta se odbija ili ostaje prazna.
    - **Test:** `TestProglasenjeObrane`.
16. **`internal/service/episode_service.go:231-232`** (`izracunaj`):
    - **Što se događa:** epizoda koja traje do kraja niza dobije kraj na zadnjem očitanju iznad praga, pa izgleda zatvorena. Postojeći `TestEpizodaNaKrajuNizaOstajeOtvorena` po imenu tvrdi suprotno, a kraj ne provjerava.
    - **Što bi trebalo:** zadnja epizoda bez pada ispod praga ostaje otvorena.
    - **Test:** `TestRacunataEpizodaNaRubovima`.
17. **`internal/repository/episode_repo.go:229-235`** (`OpenEpisodesInSector`):
    - **Što se događa:** vraća epizode bez identiteta (`uuid.Nil`).
    - **Što bi trebalo:** s identitetom, kao `Open`.
    - **Test:** `TestOtvoreneObraneSektoraBezIdentiteta`.
18. **`internal/repository/akti_repo.go:137-144`** (`SljedeciBroj`, iz čitanja koda):
    - **Što se događa:** broj akta je `MAX(broj)+1` bez jedinstvenog ključa. Dvije ovjere u isto vrijeme, ili dva čvora prije razmjene, dobiju isti broj.
    - **Što bi trebalo:** jedinstvenost broja po sektoru i godini, ili broj koji uključuje čvor.
19. **`internal/service/akt_service.go:701`** (iz čitanja koda):
    - **Što se događa:** `uuid.MustParse(a.StationID)` padne ako akt nosi neispravnu letvu, npr. pristigao razmjenom.
    - **Što bi trebalo:** greška umjesto pada.

20. **`internal/service/akt_service.go:825-866`** (`UcitajSkenirani`, iz čitanja koda):
    - **Što se događa:** ovjera skenom potpisanog akta ne traži aktivnu obranu u sektoru, a `Ovjeri` je traži (`:655-657`). Ovjera skenom tako prolazi u preventivnoj obrani.
    - **Što bi trebalo:** isti preduvjeti na oba puta ovjere.
21. **`internal/repository/section_repo.go:349-356`** (`GetSectionPersonnel`, za primatelje akta u `akt_service.go:523-568`; iz čitanja koda):
    - **Što se događa:** traži samo `is_active = 1`, a ne i `expires_at`. Osoba čija je dužnost istekla ostaje među primateljima akta, iako je ovlasti (`user_repo.go:721`) više ne vide.
    - **Što bi trebalo:** isti uvjet isteka kao pri učitavanju dužnosti.

### Prognoza
22. **`internal/prognoza/provjera.go:243`, `:258-287`, s primjenom u `internal/prognoza/osvjezavanje.go:338-345`**:
    - **Što se događa:** glačanje promašaja (zadano 6 h) prosječi i doseg 0, sat izdavanja, s okolnim dosezima. Doseg 0 tako dobije pomak različit od nule, a živa prognoza ga oduzme od izmjerene vrijednosti u satu izdavanja. U testu prognoza kreće od 105 + 30/7 cm, a izmjereno je 105.
    - **Što bi trebalo:** sat izdavanja ostaje mjerenje; doseg 0 se ne gladi i ne ispravlja.
    - **Test:** `TestProvjeraUnatragGlacanjeDiraSatIzdavanja`.
23. **`internal/prognoza/baza.go:1017` i `:1047`**:
    - **Što se događa:** tablica `promasaji` razlikuje veličinu, a `Promasaji` čita u `letva → doseg`. Za letvu s vodostajem i protokom jedan od dva zapisa se izgubi, a koji, nije određeno.
    - **Što bi trebalo:** ključ s veličinom i pri čitanju.
    - **Test:** `TestPromasajiPoLetviBezVelicine`.
24. **`internal/prognoza/baza.go:1007`** (`SpremiPromasaje`):
    - **Što se događa:** komentar kaže „zamjenjujući zatečene”, ali se samo dopisuje i mijenjaju isti dosezi. Doseg koji u novoj provjeri nema 100 slučaja zadrži stari pomak.
    - **Što bi trebalo:** nova provjera zamjenjuje sve promašaje letve.
    - **Test:** `TestProvjeraUnatragPremaloSlucaja`.

25. **`cmd/gocop/main.go:1093-1120`** (iz čitanja koda):
    - **Što se događa:** „Generiraj” (`POST /prognoze/generiraj`) računa kopijom s `Iznova=true`, ali zapisuje izvornim `osvjezivac.Zapisi`, koji ima `Iznova=false`. Zato se staro izdanje istog sata ne briše (`ObrisiIzdanje`, `internal/prognoza/osvjezavanje.go:723-729`), a letva koja se više ne računa zadrži staru prognozu. Čvorovi koji izdanje primaju razmjenom brišu ga uvijek (`internal/prognoza/razmjena.go:225`), pa izdavač i primatelji mogu imati različite podatke za isti sat.
    - **Što bi trebalo:** zapisati istim `racun` kojim je računato, kao što to radi priprema modela (`main.go:1163`).
26. **`internal/prognoza/osvjezavanje.go:208-215` prema `:292`** (iz čitanja koda):
    - **Što se događa:** svježina tuđe prognoze ispred računa provjerava se po satu na zidu, a budućnost se uzima po satu izdavanja. Kad izdanje kasni više od šest sati, letvi se skine račun, a tuđa budućnost ne postavi.
    - **Što bi trebalo:** oba po satu izdavanja.

### Uvozi
27. **`internal/importer/csvlevels/csvlevels.go:208`**:
    - **Što se događa:** dva stupca na istu letvu daju isti identifikator očitanja. Probni prolaz najavi oba, upis upiše jedno, a koja vrijednost ostane ovisi o redoslijedu mape, dakle nije određeno.
    - **Što bi trebalo:** drugi stupac iste letve javiti kao dvosmislen.
    - **Test:** `TestUvozTabliceDvaStupcaNaIstuLetvu`.
28. **`internal/importer/csvlevels/csvlevels.go:106`**:
    - **Što se događa:** sat 0:00 znači „zadano” i postaje 7:00, pa ponoć nije moguće zadati.
    - **Što bi trebalo:** zasebna oznaka za nezadan sat.
    - **Test:** `TestUvozTabliceSatOcitanja`.
29. **`internal/importer/csvlevels/csvlevels.go:616-620`** (`parseLevel`):
    - **Što se događa:** prihvaća `NaN`, `Inf` i `1e3`. `NaN` pretvoren u cijeli broj daje vrijednost koja ovisi o platformi.
    - **Što bi trebalo:** samo konačni brojevi bez eksponenta.
    - **Test:** `TestUvozTabliceRazlikePremaZatecenom`.
30. **`internal/importer/ugovor/ugovor.go:383`** (`pick`):
    - **Što se događa:** padne kad područje nema ni naziv ni naziv ispostave, a neki kandidat ima pojašnjenje u zagradi (indeks −1).
    - **Što bi trebalo:** usporedba s praznim nazivom ne pogađa ništa.
    - **Test:** `TestOdabirKandidataBezNazivaPodrucja`.
31. **`internal/importer/ugovor/ugovor.go:640-651`, s `internal/repository/maintenance_repo.go:172-175`**:
    - **Što se događa:** ponovni uvoz istog ugovora upiše lokaciju iznova, pa ručno vezana dvoznačna lokacija izgubi vezu.
    - **Što bi trebalo:** postojeća veza ostaje kad uvoz nema svoju.
    - **Test:** `TestUvozUgovoraUparivanje`.
32. **`internal/importer/bp16/journals.go:430`**:
    - **Što se događa:** `r.Datum[:4]` padne na zapisu bez datuma.
    - **Što bi trebalo:** takav zapis preskočiti i prebrojati.
    - **Test:** `TestUvozDnevnikaBezDatumaPada`.
33. **`internal/importer/bp16/obilasci.go:304`** (`sredi`):
    - **Što se događa:** tekst izgubi nove retke, iako ih funkcija pokušava sačuvati (`strings.Fields` dijeli i po `\n`).
    - **Što bi trebalo:** sačuvati retke.
    - **Test:** `TestBrojeviITekstIzEvidencije`.

## Otvorena pitanja

1. **Prekid stupnja koji nije pripremni** (stavka 11): što ovjeren akt o prekidu redovne ili izvanredne obrane treba učiniti sa stanjem obrane? Spustiti obranu na pripremno stanje, završiti je, ili samo upisati bilješku? Danas ne radi ništa osim upozorenja.
2. **Akt unaprijed** (stavka 12): akt koji vrijedi više od sat unaprijed odbiti pri ovjeri, ili obranu otvoriti kad akt stupi na snagu?
3. **Izravne rute obrane** (`POST /sections/{code}/obrana/*`): nijedna stranica ih ne koristi. Maknuti ih, ili ih voditi istim pravilima kao ovjeru akta (ovlasti, aktivna obrana, mjerodavna letva, vremenska zona)?
4. **Promašaji prognoze** (stavka 22): smije li se doseg 0 uopće gladiti i ispravljati? Treba li provjera unatrag primjenjivati zapisane promašaje i na rezervnim inačicama, kad ih živa prognoza ne primjenjuje?
5. **Pregled tuđim očima isključenog računa** (stavka 9) i **poništenje lozinke operateru** (`TestPonistenjeLozinkeOperatoru`): uprava sektora operatera uređuje, ali mu ne smije poništiti lozinku. Je li oboje namjerno?
6. **Uvoz iz stare evidencije Baranje** (`internal/importer/bp16`): je li prelazak gotov? O tome ovisi treba li ga testirati ili ukloniti iz glavnog programa (`docs/STABILIZACIJA.md`, pod 13).
7. **Testovi koji traže `data/`**: `user_service_test.go`, `view_as_test.go` i stari testovi `csvlevels`, `ugovor` i `bp16` u CI-ju se preskaču. Novi testovi pokazuju da se baza za njih može složiti iz izmišljenih podataka. Želite li da i stari dobiju takve podatke?
8. **`docs/CODE_QUALITY_BASELINE.md`** još navodi SA4009 u `akt_service.go` kao blokadu, iako ju je popravio 4382ef4. Dokument nisam mijenjala, jer ovaj PR ima samo testove i jedan dokument.
