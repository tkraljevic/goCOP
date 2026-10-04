# PREDAJA — privremena datoteka, ukloniti prije otvaranja PR-a

Rad je prekinut. Ova datoteka nije dio promjene: služi agentu koji preuzima granu. Pravila i postupak mjerenja su u `PREDAJA.md` na grani `stabilizacija-plan`.

## Stanje grane `stabilizacija-vodocuvar`
- Dva commita: testovi (9cea530) i popravak dana lista kad se sat pomiče, s testom (f21a6b2). Oba su poslana.
- `go test` za `internal/service`, `internal/repository` i `internal/web` prolazi, a lint ne daje novih nalaza.
- **Nije napravljeno:** `make quality` za granu prema baselineu. Nacrt PR-a nije otvoren.

## Što preostaje
1. Pokrenuti `make quality` i provjeriti da nema regresija.
2. Ukloniti ovu datoteku i otvoriti NACRT PR-a bez AI potpisa, s opisom ispod (gotov je).

## Nacrt opisa PR-a

## Što je u ovom PR-u

Testovi za `VodocuvarService.Spremi`, odnosno spremanje i predaju lista vodočuvarskog dnevnika, zajedno s onim što Spremi poziva: `Pripremi`, `zadaciNaListu`, `ocitanja` i `terenskaDuznost`. Svi su u `internal/service/vodocuvar_spremanje_test.go` i rade na pravoj SQLite bazi s izmišljenim podacima: sektor P, područja 1 i 2, letva Primjerovo i vodočuvar Pero Perić (pperic).

U zasebnom commitu je jedan mali popravak (vidi točku 1 pod „Sumnjivo ponašanje”).

## Testovi

| Test | Što tvrdi |
|---|---|
| `TestDnevnikVodiSamoVodocuvar` | Bez osobe Spremi vraća ErrUnauthorized. Rukovoditelj, strojar, bivši vodočuvar (neaktivno zaduženje) i osoba bez zaduženja dobivaju ErrNijeVodocuvar. Odbijeni upisi ne ostavljaju list. |
| `TestListNosiPodrucjeZaduzenjaVodocuvara` | List nosi sektor i područje glavnog zaduženja vodočuvara, a bez njega prvog aktivnog. Ime, osoba i čvor upisuju se na list. |
| `TestRadnoVrijemeNaListu` | Prazno, „7”, „15h”, „24:00”, „07:60” i „07:00:00” odbijaju se, a odbijeni upis ne sprema list. Razmaci se brišu iz svih polja. „7:00” prolazi i sprema se kako je upisano. Noćni rad (22:00–06:00) daje 8 sati. |
| `TestPripremaListaRadnoVrijeme` | Zadano radno vrijeme je 07:30–15:30, a postavka organizacije vrijedi samo kad su upisana oba kraja. Spremi ga ne nasljeđuje (prazno se odbija). Pripremljen list nije spremljen. |
| `TestRedniBrojLista` | Broj se daje pri prvom spremanju i izmjena ga ne mijenja. Raniji dan upisan kasnije dobiva sljedeći broj. Svaki vodočuvar ima svoju knjigu, a svaka godina počinje od 1. |
| `TestZakljucenListSeNeMijenja` | Predan, prenesen (Rekonstrukcija) i list iz prošle godine ne mijenjaju se, a sadržaj u bazi ostaje isti. Granica: 31. 12. prošle godine u 23:59 je arhiviran, 1. 1. nije. |
| `TestPredajaTraziOpis` | Predaja bez opisa (i s opisom od samih razmaka) se odbija. Odbijena predaja ne sprema ni nov list ni izmjene nacrta. Nacrt bez opisa smije se spremiti. |
| `TestZadaciNaListuIPredaja` | Na listu su samo otvoreni zadaci tog vodočuvara zadani do tog dana. Obavljen ili odbačen zadatak traži upis što je napravljeno. Nepoznato stanje znači otvoren. Unos za zadatak kojeg nema na listu se zanemaruje. Nacrt ne zaključuje zadatke u evidenciji, a predaja ih zaključuje (stanje, tekst, list, vrijeme). Otvoren zadatak prelazi na sljedeći list, a planirani stoji na svom danu. |
| `TestPredajaTraziObrazlozenjeNeobavljenogZadatka` | Otvoren zadatak bez obrazloženja (i s obrazloženjem od samih razmaka) zaustavlja predaju i ništa se ne sprema. S obrazloženjem list se predaje, a zadatak ostaje otvoren. |
| `TestZadatakZakljucenNaDvaLista` | Isti zadatak na dva lista: drugi list ga predajom zaključi, a predaja prvog ne mijenja evidenciju, pa prvi list i evidencija pokazuju različito stanje. Bilježi zatečeno ponašanje (točka 4). |
| `TestOcitanjaNaListuPriPredaji` | Pri predaji na list ulaze vodostaji koje je vodočuvar upisao tog dana (sa predznakom i satom), bez tuđih, bez onih bez vodostaja i bez susjednih dana. Nacrt ih ne osvježava. |
| `TestNeuspjeliUpisLista` | Kad upis u bazu ne uspije, Spremi vraća grešku i list ne postoji. |
| `TestPredajaBezZakljucenogZadatka` | Kad zaključivanje zadatka ne uspije, list ostaje predan, a zadatak otvoren. Bilježi zatečeno ponašanje (točka 2). |
| `TestDanListaKadSeSatPomice` | Dan lista je kalendarski dan i kad se sat pomiče. U listopadu (25 sati) vodostaj i zadatak iz 23:30 su na listu tog dana. U ožujku (23 sata) vodostaj iz 00:30 sljedećeg dana nije na listu prethodnog. Prije popravka padao je na sve tri provjere. |

## Prije i poslije

CC i „prije” su iz mjerenja mastera 713d9df alatom `dev/quality`. „Poslije” je coverage paketa `service` s grane, a CRAP je izračunat istom formulom (CC² · (1 − cov)³ + CC). Navedene su samo promijenjene funkcije, bez `drugi_korak.go`, čiji coverage varira od pokretanja do pokretanja. Prije PR-a treba zamijeniti tablicom iz `make quality`.

| Funkcija | CC | Coverage prije | Coverage poslije | CRAP prije | CRAP poslije | Kritična |
|---|---:|---:|---:|---:|---:|:---:|
| `internal/service/vodocuvar_service.go:36` · `(*VodocuvarService).SetRadnoVrijeme` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/vodocuvar_service.go:40` · `NewVodocuvarService` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/vodocuvar_service.go:45` · `(*VodocuvarService).SetWeather` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/vodocuvar_service.go:52` · `terenskaDuznost` | 6 | 0.0 % | 100.0 % | 42.0 | 6.0 |  |
| `internal/service/vodocuvar_service.go:70` · `VodiDnevnik` | 2 | 0.0 % | 100.0 % | 6.0 | 2.0 |  |
| `internal/service/vodocuvar_service.go:141` · `(*VodocuvarService).Pripremi` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |
| `internal/service/vodocuvar_service.go:145` · `(*VodocuvarService).pripremiZa` | 10 | 0.0 % | 100.0 % | 110.0 | 10.0 |  |
| `internal/service/vodocuvar_service.go:176` · `(*VodocuvarService).zadaciNaListu` | 8 | 0.0 % | 94.4 % | 72.0 | 8.0 |  |
| `internal/service/vodocuvar_service.go:206` · `(*VodocuvarService).prilike` | 11 | 0.0 % | 12.5 % | 132.0 | 92.1 |  |
| `internal/service/vodocuvar_service.go:234` · `(*VodocuvarService).ocitanja` | 5 | 0.0 % | 88.9 % | 30.0 | 5.0 |  |
| `internal/service/vodocuvar_service.go:262` · `(*VodocuvarService).Spremi` | 26 | 0.0 % | 98.2 % | 702.0 | 26.0 |  |
| `internal/service/vodocuvar_service.go:502` · `(*VodocuvarService).Moji` | 1 | 0.0 % | 100.0 % | 2.0 | 1.0 |  |

## Sumnjivo ponašanje

1. **`internal/repository/vodocuvar_repo.go:258` i `:394`: kraj dana računao se kao početak + 24 sata.** Na dan pomicanja sata to nije ponoć. U listopadu vodostaj i zadatak iz 23:30 nisu ulazili na list tog dana. U ožujku je vodostaj iz 00:30 sljedećeg dana ulazio na list prethodnog. *Treba:* kraj dana je sljedeća ponoć u hrvatskom vremenu. **Popravljeno** u zasebnom commitu (`AddDate(0, 0, 1)`) s testom `TestDanListaKadSeSatPomice`. Isti obrazac ima i `internal/service/journal_service.go:216` (vodostaji na listu dnevnika COP-a). To je izvan ovog PR-a i nije dirano.
2. **`internal/service/vodocuvar_service.go:322–341`: predaja nije jedna cjelina.** List se sprema kao predan prije nego što se zadaci zaključe u evidenciji. Ako zaključivanje ne uspije, Spremi vraća grešku, a list ostaje predan i zadatak otvoren. Ponovni pokušaj odbija se jer je list predan, a zadatak se i dalje pojavljuje na sljedećim listovima. *Treba:* list i zadatke spremiti u istoj transakciji, ili zadatke zaključiti prije nego što se list označi predanim. Isto se vidi i u webu: `handlers_vodocuvar.go:380` javlja „List je predan, ali izvornik nije potpisan”.
3. **`internal/web/handlers_vodocuvar.go:373–376` uz `vodocuvar_service.go:308–316`: odbijena predaja briše upisano.** Kad predaja padne na provjeri (nema opisa, neobrazložen zadatak), ništa se ne sprema, a obrazac se preusmjerava na spremljeno stanje. Sve upisano od zadnjeg spremanja tako nestaje. *Treba:* spremiti nacrt pa odbiti predaju, ili vratiti obrazac s upisanim.
4. **`internal/service/vodocuvar_service.go:196–199` i `:332–334`: zadatak zaključen na dva lista.** Dok nijedan list nije predan, isti otvoren zadatak stoji na listovima više dana i svaki ga može označiti drukčije. Evidenciju zaključuje prvi predani list. Kasnija predaja drugog lista evidenciju preskače bez poruke, pa list i evidencija pokazuju različito stanje (npr. na listu „obavljen”, u evidenciji „odbačen”). *Treba:* pri predaji javiti da je zadatak već zaključen drugim listom, ili ga na listu prikazati kako stoji u evidenciji.
5. **`internal/service/vodocuvar_service.go:273`: nema provjere budućeg dana.** List se smije spremiti, pa i predati (potpisati), za dan koji još nije došao, pa i za iduću godinu. *Treba:* barem predaju odbiti dok dan nije počeo (vidi otvoreno pitanje).
6. **`internal/service/vodocuvar_service.go:291–299`: nepoznato stanje zadatka tiho postaje „otvoren”.** S obrasca može doći bilo koja vrijednost, pa i „BEZ_ODGOVORA”. *Treba:* odbiti nepoznato stanje.
7. **`internal/service/vodocuvar_service.go:177–180`: greška čitanja otvorenih zadataka se guta.** List se tada spremi samo s ranije upisanim zadacima, bez novih, i bez poruke. Manja stvar.
8. **`internal/service/vodocuvar_service.go:276–281`: radno vrijeme se ne svodi na isti oblik.** „7:00” prolazi i sprema se tako, a ostali listovi imaju „07:00”. Sati se ipak računaju dobro. Manja stvar.

## Otvorena pitanja

- Smije li se list unaprijed ispuniti kao nacrt za dan koji još nije došao (npr. plan obilaska)? Ako smije, treba li barem predaju vezati uz dan koji je počeo?
- Redni broj prati redoslijed prvog spremanja, ne datum („kao stranica u knjizi”). Je li to namjera i kad se list za raniji dan upiše naknadno, pa stranice u knjizi ne idu po datumu?
- Tablica `vodocuvarski_listovi` nema jedinstvenost (user_id, datum). Dva istodobna prva spremanja istog dana (dva preglednika ili dva čvora) mogu napraviti dva lista za isti dan, a `ZaDan` tada vraća bilo koji od njih. Ovim testovima to nije pokriveno. Treba li to spriječiti u shemi ili pri sinkronizaciji?
