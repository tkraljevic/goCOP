# Popis izmjena

Verzije prate [shemu iz administratorskih uputa](docs/INSTALACIJA.md#8-verzije): alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

## 0.0.38-alfa — u pripremi

- **Obrisana dionica ne ostavlja epizode obrane za sobom.** Brisanje
  dionice odnosilo je i njezine epizode obrane, ali to nije bilježilo, pa
  drugi čvorovi za to nisu znali. Novi čvor takvu je epizodu čekao pri
  svakoj razmjeni, a pri svakom pokretanju programa u dnevniku je stajala
  greška, i na računalu na kojem je dionica obrisana. Sad brisanje dionice
  kao obrisane bilježi i sve njezine epizode, a epizoda dionice obrisane
  ranije se preskače. Ne vraća se ni kad se dionica poslije ponovno upiše
  pod istom šifrom.
- **Dionica s obrisanom letvom ili objektom stiže na druge čvorove.** Kad se
  iz registra obriše letva ili objekt koji dionica još navodi, novi čvor tu
  dionicu nije dobio, ni njezine epizode obrane. Pri svakom pokretanju
  programa u dnevniku je stajala greška za dionicu, i na računalu na kojem
  je letva obrisana, a ondje se dionica više nije dala ni spremiti. Sad se
  obrisana letva ili objekt uz dionicu preskače. Letvu ili objekt koji na
  čvor još nisu stigli dionica i dalje čeka.
- **Obrisana bilješka arhive ne vraća se.** Kad su za isti sat postojale
  dvije bilješke, upisane na dva računala, brisanje je obrisalo samo onu
  koja se vidjela. Druga se vratila pri idućem pokretanju programa, a novi
  čvor ju je znao dobiti odmah. Sad brisanje bilješke briše sve bilješke tog
  sata. Kad se obriše bilješka koja se vidi, a druga za isti sat ostane,
  svi čvorovi odmah pokazuju tu drugu, ne tek nakon ponovnog pokretanja.
  Isto vrijedi za ispravke arhive.
- **Brisanje naselja ili općine koju navodi dionica stiže svuda.** Na
  računalima koja imaju takvu dionicu naselje je ostajalo, a razmjena je
  svaki put javljala grešku. Novi čvor dionicu nije dobio, a pri pokretanju
  programa u dnevniku je stajala greška za dionicu, i na računalu na kojem
  je naselje obrisano. Sad naselje nestane svuda, a dionica ostaje bez
  veze s njim.
- **Brisanje dok drugo računalo upisuje.** Jedno računalo obriše područje,
  sektor, općinu, županiju ili korisnika, a drugo u isto vrijeme tome upiše
  objekt, naselje ili dužnost. Računala su se tada trajno razišla: jedno je
  novi zapis čekalo zauvijek, a drugo nije moglo obrisati ono što je
  obrisano. Sad na svim računalima brisanje ima prednost, pa se takav zapis
  ne pokazuje. Kad se područje ili sektor ponovno upiše pod istim brojem,
  zapis se vraća s njim. Brisanje korisnika ili općine sad kao obrisane
  bilježi i dužnosti i naselja koja to računalo nije imalo na popisu.
- **Epizoda obrane dolazi s ponovno upisanom dionicom.** Kad je dionica
  obrisana pa ponovno upisana pod istom šifrom, epizoda koju je drugo
  računalo u međuvremenu otvorilo na njoj nije se pokazivala do ponovnog
  pokretanja programa. Sad dolazi odmah, s dionicom.
- **Riječ uz broj i nazivi ustroja pišu se pravilno.** Na stranicama, u
  porukama i izvozima pisalo je „21 očitanja”, „1 listova”, „2 ispravaka”
  ili „2 pisma premješteno”. Program je uz broj razlikovao samo jedan i
  sve ostalo, a ponegdje ni to. Sad se riječ uz broj, i glagol uz nju,
  slaže po hrvatskom pravilu: 1 list, 2 lista, 5 listova, 21 list;
  2 pisma su premještena. Na karticama dnevnika A.02 i A.03 pisalo je „Po
  sektoru i podrucju.”, a uz izbor uloge „uloga branjeno područjea”; te i
  slične rečenice s nazivima ustroja su prepravljene. Prvi prag letve na
  prognozi zove se „pripremno stanje”, kao i drugdje u programu.
- **Hrvatska slova u svojstvima PDF-a.** U svojstvima PDF-ova koje program
  izrađuje (naslov, autor, predmet) slova č, ć, đ, š i ž bila su upitnici,
  npr. „Rje?enje o uspostavi”. Sad se vide kako treba. Već spremljeni i
  potpisani PDF-ovi ostaju bajt za bajt isti.

## 0.0.37-alfa — 13. 10. 2026.

- **Očitanja koja su stigla, a nisu bila upisana.** Kad dva čvora šalju u
  isto vrijeme ili se računalo ugasi usred razmjene, primljena očitanja
  znala su ostati spremljena, ali se nisu pokazivala, a razmjena ih više
  nije slala. Jednom su čvoru tako nedostajali cijeli dani za Osijek,
  Aljmaš, Belišće, Donji Miholjac i Terezino Polje, oko 21 000 očitanja.
  Sad ih program vrati sam pri pokretanju i u dnevnik zapiše koliko ih je
  vratio. Isto vrijedi za izmjene koje su ostale samo spremljene, npr.
  ispravljen vodostaj ili potvrđeno dežurstvo, te za listove i upise
  dnevnika, dežurstva, bilješke i ispravke arhive. Razmjena koja se ne
  upiše do kraja sad se ponovi s idućom razmjenom.
- **Zapis o brisanju više se ne briše s oglasne ploče.** Kad bi uprava s
  ploče obrisala zapis o brisanju, npr. „Maknuto dežurstvo”, obrisani zapis
  vratio bi se pri idućem pokretanju programa, a dežurstvo i u plan i u
  obračun sati. Program sad takvo brisanje odbije, kao i brisanje zadnje
  verzije živog zapisa. Zapis obrisan s ploče još na 0.0.36-alfa ili
  starijem ovo ne popravlja: takvo dežurstvo 0.0.37-alfa vrati pri prvom
  pokretanju, a u dnevniku programa piše „Obnova površine: … dežurstva N”.
  Treba ga opet maknuti.
- **Opoziv čvora vrijedi i kad stigne zajedno s poništenjem.** Opoziv se ne
  poništava, ali kad je čvor primio opoziv i njegovo poništenje u istoj
  razmjeni, opoziv nije upisao, pa je opozvani čvor za njega ostao
  vjerodostojan.
- **Postavke iz gocop.toml i u Administraciji.** Dosad se `gocop.toml`
  mijenjao samo u uređivaču teksta, a na Unraidu i uz uslugu sustava za to
  treba pristup disku ili `sudo`. Sad dio postavki globalni administrator
  mijenja na stranici kojoj pripadaju. Na stranici Čvor, mreža i
  sinkronizacija, u kartici Ovaj čvor, su javna adresa te centar i telefon
  sa stranice prijave, a pod Mreža razmak sinkronizacije i razmjena preko
  Tailscalea. „Prati sve” i povijest tuđih očitanja su na stranici Što ovo
  računalo prati, do koje sad vodi i kartica u Administraciji, a izvor karte
  pod Opcije. Održavanje baze pokazuje gdje su baza, arhiva i datoteka
  postavki te njezine kopije. Radi samo s ovog računala ili iz lokalne
  mreže, ne kroz tunel. Program novu datoteku prije spremanja provjeri kao
  pri pokretanju, staru ostavi kao kopiju (zadnjih pet), a ručni komentari
  ostaju. Naziv čvora, portovi, putanje i posrednici samo se prikazuju.
- **Natpis o podrijetlu karte je običan tekst.** Karta ga sad uvijek
  pokazuje kao tekst, i kad je u `gocop.toml` upisan s HTML oznakama, a iz
  programa se znakovi `<` i `>` u natpisu ne mogu spremiti.
- **Ponovno pokretanje čvora iz programa.** Gumb u kartici Ovaj čvor uredno
  ugasi čvor, a pokrene ga Postava, usluga sustava ili Docker (samo uz
  `--restart`). Postava takvo gašenje zasad broji kao pad: kad se čvor ugasi
  treći put u deset minuta, ostane ugašen dok ga ne pokrenete u izborniku
  ikone u traci. Ručno pokrenut čvor gumba nema, nego piše što napraviti.
  Dok spremljena promjena čeka, stranice s postavkama to javljaju na vrhu.
  Usluga sustava i spremnik za to čvoru javljaju tko ga pokreće
  (`GOCOP_NADZOR`).
- **Prijava preko Tailscalea bez PIN-a.** Na računalu s Tailscaleom prijava
  na njegovu Tailscale adresu (npr. `http://100.x.y.z`) tražila je PIN kao
  izvana. PIN radi samo preko HTTPS-a, pa se tako nije moglo prijaviti. Sad
  zahtjev koji stigne izravno na Tailscale adresu računala vrijedi kao iz
  lokalne mreže, a kroz posrednika i dalje kao izvana. Na računalu bez
  Tailscalea ništa se ne mijenja. Na Macu se Tailscale prepozna samo kad uz
  adresu 100.x ima i svoju IPv6 adresu, jer adresu 100.x daju i drugi
  VPN-ovi, npr. Cloudflare WARP, NetBird i NordVPN Meshnet. Mac u Tailscale
  mreži s isključenim IPv6 zato radi kao računalo bez Tailscalea, i za
  prijavu i za razmjenu. Isključuje se u `gocop.toml` s
  `[web] tailscale_lokalno = false`; tada prijava preko Tailscalea traži
  PIN, i preko IPv6.
- **Razmjena preko Tailscalea.** Laptop kod kuće nije mogao izravno do
  uredskog čvora, iako ih Tailscale spaja; sve je išlo preko Unraida i
  tunela. Sad čvor s Tailscaleom zove i Tailscale adrese drugih vaših
  čvorova, iza adresa iz lokalne mreže, a ispred tunela. Prvu adresu čvor
  zapamti kad se dva čvora jednom upare preko Tailscale adrese, a ostale
  adrese jave jedan drugome u razgovoru preko Tailscalea. Te adrese svaki
  čvor pamti samo kod sebe. Nikad ne ulaze u popis adresa čvora, koji
  dobivaju svi čvorovi mreže, i ne idu u razgovor koji ne ide preko
  Tailscalea. Čvorovi bez Tailscalea ih zato ne vide i ne zovu, a razmjena
  im je ista kao prije. Tailscale adresa se više ne upisuje ručno među
  adrese čvora; program javi grešku. Tailscale adrese koje su već u popisu,
  upisane ručno ili zapamćene pri uparivanju u 0.0.36-alfa, program više ne
  pokazuje i ne zove. Zato i već uparene čvorove treba jednom upariti preko
  Tailscale adrese.
- **Postavka `[sync] tailscale`.** Zadano je uključena. Uz `false` razmjena
  radi kao da Tailscalea nema, npr. za probu razmjene preko interneta, a
  razmjenu koja stigne na Tailscale adresu tog računala odbije. Na prijavu
  ne utječe. Mijenja se i u programu, na stranici Čvor, mreža i
  sinkronizacija pod Mreža.
- **Zaboravljen čvor u knjizi nema bilješke o razmjeni.** Kad administrator
  zaboravi čvor, zapis u knjizi nosio je i zadnju poruku o grešci razmjene,
  a u njoj zna stajati adresa koju ostali čvorovi ne trebaju vidjeti. Taj
  zapis razmjena nosi svim čvorovima mreže. Sad ide bez bilješke.
- **Istodobne razmjene bez „database is locked”.** Uredski čvor je pri
  istodobnom primanju s Unraida javio tu grešku i verzija nije ušla u
  knjigu. Na Windowsu je istodobni rad vjerojatno ostavio dio očitanja samo
  u knjizi. Sad primljeno upisuje jedna razmjena u isto vrijeme. Razgovor
  koji nema ništa za upis ne čeka. Kad je upis zauzet dulje od pola minute,
  razgovor završi bez upisa i primljeno stigne sljedećom razmjenom; ploča
  razmjene to pokaže kao grešku s razlogom.
- **Nadogradnja.** 0.0.37-alfa radi uz 0.0.36-alfa i nadograđuje se redom
  (stalni čvor prvi). Čvorove s Tailscaleom treba nadograditi zajedno:
  stariji program Tailscale adresu koju vidi upiše u popis adresa čvora, a
  taj popis dobivaju svi čvorovi mreže.

## 0.0.36-alfa — 12. 10. 2026.

- **Novi čvor nakon prve razmjene ima sve dionice.** Neke dionice, i
  epizode obrane uz njih, nisu se pojavile na novom čvoru, a razmjena nije
  javila grešku. Događalo se kad je novija izmjena dionice stigla prije
  letve, objekta ili naselja o koje se oslanja, a starija tek poslije. Sad
  dionica dođe s najnovijom izmjenom čim stigne ono što joj treba. Epizode
  obrane program obnavlja i pri pokretanju, kao dionice.
- **Bilješka ili ispravak arhive upisan dvaput za isti sat.** Na novom
  čvoru druga bilješka za isti sat javljala je grešku pri svakoj razmjeni,
  a čvorovi su mogli pokazivati različitu bilješku, ovisno o tome koja je
  prva stigla. Sad svi pokazuju noviju od dviju. Isto vrijedi za ispravke
  arhive.
- **Obrisan dnevnik ne ostavlja upise za sobom.** Upisi dnevnika obrisanog
  ranije na novom su čvoru javljali grešku pri svakoj razmjeni. Sad se
  preskaču, kao i na čvoru na kojem je dnevnik obrisan. Brisanje dnevnika
  sad kao obrisan bilježi i upis koji to računalo nije imalo u dnevniku, pa
  ga drugi čvorovi više ne primaju.
- **Linux: ikona u traci uz uslugu sustava.** Uz izdanje sad stoji i
  Postava za Linux, a `instaliraj.sh` je postavlja s unosom goCOP u
  izborniku programa. Kad je čvor usluga sustava, Postava ne pokreće drugi
  čvor, nego pokazuje stanje usluge, zaustavlja je i pokreće, otvara njezin
  dnevnik i nadograđuje je na novo izdanje. Za to sustav traži lozinku
  administratora. Izlaz iz ikone uslugu ne zaustavlja. Upute su u
  docs/linux.md, poglavlje 12.
- **Bačka Palanka: najviši i najniži vodostaj s Iloka.** Letva nema
  vlastiti dugi niz, pa su MAX i MIN na kartici bili samo ono što smo
  zabilježili od 14. 9. 2026. Sad ih preuzima s Iloka, letve nasuprot s
  istom kotom nule, uz oznaku „s letve Ilok”. Vlastito očitanje koje ih
  nadmaši i dalje ima prednost.
- **Dionice: isto ugroženo područje uz više nasipa.** Općina ili naselje
  upisano uz jedan nasip nije se dalo upisati uz drugi nasip ni uz cijelu
  poddionicu: obrazac je drugi upis tiho preskakao, i pri ručnom dodavanju
  i pri prijepisu iz dokumentacije. Sad se odbija samo isto područje uz
  isti nasip.
- **Kartica dionice bez nasipa: ugroženo područje uz objekte.** Na
  poddionici bez nasipa ugroženo područje stoji u retku uz objekte, po
  županijama i općinama, kao i uz nasip na drugim dionicama, a ne više
  iznad tablice; tako ga piše i izvoz kartice. Ugroženo područje bez nasipa
  na poddionici koja nasipe ima ostaje iznad tablice.
- **Dnevnik: istodobni upisi se ne gube.** Kad dvoje u isto vrijeme upiše
  bilješku ili naredbu na isti list, ili jedan upisuje dok drugi parafira,
  ovjerava ili objavljuje prijavu, ili dok vodočuvar sprema ili predaje
  obrazac, list zadržava sve upisano. Dosad su oba spremanja javljala
  uspjeh, a na listu je ostajalo samo zadnje; u knjizi verzija bilo je
  zapisano oboje.
- **Dnevni list otvoren prije ponoći sprema se na svoj dan** (Odluke (11.
  10. 2026.)). Obrazac otvoren prije ponoći, a prvi put spremljen poslije
  nje, više se ne odbija jer je dan prošao: upisano se sprema na list dana
  za koji je obrazac otvoren. Obrazac to dokazuje oznakom otvaranja koju
  potpisuje program; nakon ponovnog pokretanja programa oznaka više ne
  vrijedi i obrazac se otvara iznova.
- **Vrh vala ispod nule** ispisivao se za centimetar previsoko (−15,7 cm kao
  −15 umjesto −16). Sad se zaokružuje na najbliži centimetar.
- **Ugradnja arhive s drugog čvora.** Dok se jedna letva namjerno vraća na
  starije izdanje, druga ugradnja u isto vrijeme mogla je preskočiti
  provjeru starosti paketa. Sad svaka ugradnja provjerava svoj paket.
- **Prognoza: rekonstrukcija iz susjeda.** Letvama bez letve na drugoj obali
  rupu dulju od šest sati u računu popunjavaju susjedi na istoj rijeci,
  pomaknuti za vrijeme putovanja vode: Szentborbás iz Barcsa (4 h uzvodno),
  Mohács iz Bezdana (5 h nizvodno), Dunaszekcsőa i Baje, a tako i
  Dunaszekcső, Siga, Aljmaš, Mohovo, Vukovar, Mursko Središće, Vízvár, Novo
  Virje, Vrbovka i Moslavina.
  - Rupa koja je završila može imati najviše 48 sati. Rupa koja traje
    popunjava se najviše 24 sata i nikad dalje od zadnjeg mjerenja susjeda.
  - Na arhivi je 95 % grešaka unutar ±1 do ±20 cm (Drava uz elektrane više,
    Dunav manje), a zadržano zadnje mjerenje griješi i do ±61 cm.
  - Upozorenje „nema mjerenja” postaje napomena odakle su sati i koliko su
    točni. Rekonstruirani sati ne pokazuju se kao mjerenje i ne upisuju se u
    očitanja.
  - Točnost vrijedi samo u rasponu razina na kojem je provjerena (arhiva od
    2015.). Izvan njega (Odluke 11. 10. 2026.) rekonstrukcija i dalje računa,
    pa dnevna prognoza ostaje izdana, ali uz izdanje stoji upozorenje da
    točnost nije provjerena; dnevna koja iz nje računa i satna niz rijeku
    označene su upitnima.
- **Očitanja iz prvog sata nove godine.** Takvo očitanje upisivalo se u
  novu godinu, a kad bi ga netko ispravio ili obrisao, prelazilo je u
  prethodnu. Čvor koji prati samo novu godinu tako nije vidio ni ispravak ni
  brisanje. Godina se sad uvijek računa po hrvatskom vremenu.
- **Karta na čvoru koji pokreće Postava.** Granice sektora, branjenih
  područja i naselja program je tražio u mapi gledano od mjesta iz kojeg je
  pokrenut. Postava čvor pokreće iz same mape podataka, pa karta na takvom
  čvoru nije imala granica. Sad se granice traže uz bazu.
- **Poveznica za poziv** sad ispravno pretvara broj upisan s prefiksom 00
  (npr. 00385 31 123 456).
- **Uvoz niza** sam popunjava sliv postojeće letve i kad je letvu upisao
  korisnik, a ne naziv datoteke.
- **Upute za Linux dopunjene iskustvom s prvim uredskim čvorom**
  (docs/linux.md): što kad port 80 drži drugi web poslužitelj (nginx),
  SSH na Ubuntu Desktopu i preko Tailscalea, uključivanje u postojeću
  mrežu, `sve = true` za uredski čvor i što se tada vidi u dnevniku, `sudo`
  i za čitanje `gocop.toml`, obavezan restart nakon izmjene te vatrozid
  koji je na Ubuntuu zadano isključen.

## 0.0.35-alfa — 11. 10. 2026.

**Odluke od 8. i 11. 10. 2026., upozorenja uz prognozu i sređen uzdužni profil.
Sve čvorove treba nadograditi: nova pravila vrijede tek na čvorovima s ovom
inačicom, a čvor na 0.0.34 zadržava staro ponašanje i ne ruši se na novim
poljima.**

- **Prognoza: upozorenja uz izdanje.** Kad nešto zakaže, prognoza to sama
  kaže. Okvir „Upozorenja uz ovo izdanje” javlja što ne radi i od kada (npr.
  „Mohács (vizugy.hu): nema mjerenja od 7. 10. u 11 h”) i koji su rezultati
  zbog toga upitni: koje letve i od kojeg najranijeg dosega, ili koji dani
  dnevne prognoze. Sitnice su u sklopljenim napomenama: rezervna inačica,
  krivulja izvan raspona, letva bez prognoze. Upozorenje se zapisuje uz
  izdanje i putuje razmjenom, pa ga svaki čvor pokazuje jednako. Ide i u
  Excel, na graf letve i na pločicu kiše. Kad je izdanje starije od 3 sata,
  stranica kaže da je prognoza zastarjela.
- **Satna uskače kad dnevne nema.** Dani bez dnevne prognoze pokazuju satnu
  vrijednost s oznakom „s”, a peti i šesti dan ostaju prazni, s objašnjenjem.
  Stranica čita dnevnu prognozu istog izdanja kao satnu, pa se starija dnevna
  više ne miješa s novijom satnom.
- **Ranija prognoza.** U postavkama prognoze bira se dan i sat, a dobiva se
  Excel kakav je tada izdan, s upozorenjima toga izdanja.
- **Dnevni model** (odluka 15): neuspjelo učenje ponavlja se već idući sat,
  modeli se uče iznova nakon uvoza u arhivu i nakon „Pripremi model”, a dan se
  računa po zagrebačkom vremenu. Popis letvi bez dnevne prognoze ne navodi
  letvu koju je pokrila rezerva.
- **Provjera unatrag** (odluka 16) mjeri i ispravlja promašaje samo na glavnom
  računu, kao živa prognoza; doseg 0 nigdje se ne ispravlja.
- **Uzdužni profil.** Nula se računa iz istih dana na cijelom toku, pa
  mađarske i naše letve stoje usporedivo. S profila ispada mjerilo koje danima
  stoji dok se susjedi miču (na rekordno niskoj vodi Siga, Petreš i Vrbovka) i
  letva koja nakon prekida mjerenja nastavi na drugoj razini. Ispod crteža piše
  koje su izostavljene i zašto.
- **Knjiga vodočuvara i prijave s terena** (odluka 1):
  - Dežurni operater u COP-u čita dnevne listove i prijave svog sektora i
    upisuje bilješku, ali ne parafira i ne zadaje zadatke.
  - Voditelj usluga / Poslovođa ne vidi ni knjigu vodočuvara ni prijave, a u
    dnevnicima usluga A.02 i A.03 samo dnevnik svoje firme.
  - Rukovoditelj dionice čita listove svog područja, a parafira, upisuje i
    (u obrani) zadaje samo ono što se tiče njegove dionice.
  - Ovlaštenik za praćenje ugovora ima sve na svom području.
- **Pregled tuđim očima** (odluka 2): isključen račun gleda se samo za čitanje,
  i kad su upisi tuđim očima uključeni.
- **Lozinka operateru** (odluka 3): uprava sektora daje Dežurnom operateru u
  COP-u svog sektora privremenu lozinku i kod za prijavu izvana.
- **Akti o obrani:**
  - Broj je jedinstven na čvoru; ista oznaka s drugog čvora označi se crveno,
    a sukob rješava voditelj Centra obrane od poplava ili zamjenik
    (odluka 4).
  - „Vrijedi od” se odbija prije otvaranja Dnevnika COP-a i više od 7 dana
    unaprijed; za više od 24 h unatrag ili 3 dana unaprijed traži se potvrda
    (odluka 17).
  - Popis primatelja: kad izvor zakaže ili dionica nema teritorij, nacrt
    nastaje uz upozorenje, a ovjera traži potvrdu. Popis se slaže iznova pri
    ovjeri i pokazuje razliku, a na njemu su firme i rukovoditelji svih
    područja akta (odluka 19).
  - Nacrt akta o stupnju obrane sastavljaju samo voditelj Centra obrane od
    poplava, njegov zamjenik i Dežurni operater u COP-u sektora; ovjera ostaje
    kao dosad. Gumb „Proglasi / prekini” i „Novi akt” vidi samo tko sastavlja
    (odluka 18).
- **Dnevni list vodočuvara:**
  - Za budući dan postoji samo nacrt, bez broja, najviše 30 dana unaprijed;
    predaja je moguća od 00:00 toga dana (odluka 5).
  - Isti vodočuvar i isti dan na svim čvorovima daju isti list, a predan list
    ima prednost; postojeći dvojnici se prijavljuju, ne brišu (odluka 7).
  - List za dan koji je prošao više se ne otvara: tko ga nije otvorio na svoj
    dan, taj dan nije radio. Akt ili prijava za takav dan ide na današnji
    list, uz napomenu. Brojevi idu redom dana; postojeći se ne mijenjaju
    (odluka 6).
  - Nacrt otvoren unaprijed vodočuvar dopisuje dok ga ne preda, i kad mu je
    dan prošao (Odluke 11. 10.).
  - Vodočuvar dobiva upozorenje na listove kojima je dan prošao, a nije ih
    potpisao i predao: na početnoj stranici i u svom dnevniku, s poveznicom
    na svaki list. Upozorava samo list na koji je sam nešto upisao; list koji
    je otvorio samo program (upis akta, „Upiši” rukovoditelja) ne upozorava
    (Odluke 11. 10.).
- **Dežurstva** (odluka 10): zapis bez poznatog mjesta rada program prvo
  pokuša prepoznati iz opisa, a inače je „za provjeru”, izvan zbroja, dok
  uprava centra ne izabere opis.
- **Izvješća** (odluka 12): izmjena predanog izvješća vraća ga u nacrt i traži
  novu predaju. Pamti se prvo vrijeme predaje i tko je mijenjao, a predano se
  povlači, ne briše.
- **Zid događanja:**
  - Objavljene i riješene prijave s terena, samo onima koji ih smiju čitati
    (odluka 20).
  - Prijenos sredstava u drugi sektor javlja se ciljnom sektoru (odluka 11).
- **Rasponi** (odluka 21): preuzimanje i uvoz ne upisuju vodostaj izvan
  raspona, isto kao ručni upis. Stranica letve pokazuje koliko je vrijednosti
  odbačeno i zadnju od njih, a tri akumulacije HEP-a imaju raspon kote.
- **Upis očitanja** (odluke 8 i 9): letva pamti sektore i područja koji je
  prate. Očitanje upisuje svatko s dužnošću u sektoru letve (ne Gost ni
  Preglednik); letva bez ikakvog sektora zadržava dosadašnje pravilo. Autor
  ispravlja svoje očitanje dok je u sektoru, a nakon toga još 72 sata; briše
  samo onaj tko odgovara za letvu.
- **Obrana** (odluka 14): uklonjen je stari kod ručnog proglašenja i računanja
  obrane iz vodostaja. „Valovi obrane u nizu” na historijatu letve pokazuju
  kada bi koji stadij bio dosegnut, samo kao prikaz.
- **Ključ čvora:** kad baza postoji, a datoteke `node-key` uz nju nema,
  čvor ne kreće i kaže što nedostaje i kako ga vratiti. Novi identitet čvora
  moguć je samo izričito, zastavicom `-novi-kljuc-cvora`. Kad nema
  `gocop.toml`, ime čvora uzima se iz baze.
- **Postavke čvora:** opisi u `gocop.toml` su kraći, uz komentirane primjere
  koji se uključuju micanjem znaka `#`, a uvod navodi najčešće izmjene. Uz
  bazu program pri svakom pokretanju zapiše `gocop.toml.primjer` s opisima
  tekuće inačice, pa ih vidi i čvor nadograđen sa starije inačice; postojeći
  `gocop.toml` ne dira, a primjer ne čita.
- **Testovi** (odluka 13): provjere djelatnika, uvoza i razmjene rade na
  izmišljenim osobama sektora P, a provjere registara posebno, prije izdanja.
- **Privremeno imenovanje i rokovi dužnosti** (odluke 5. 10.):
  - Dužnosti su stalne. Privremena ispomoć ističe s prestankom obrane prema
    ovjerenom aktu ili zaključno sa zadanim danom.
  - „Vrijedi zaključno s 14. 10.” prestaje 15. 10. u 0 h.
  - Profil kaže odakle je istek.
- **Pomoć:** u Zahvalama je opet stvarno ime (Mario Spajić); izmišljena imena
  služe samo za primjere i testove.

## 0.0.34-alfa — 4. 10. 2026.

**Stanje obrane iz ovjerenih akata, poništenje akta i ispravci nađeni
sređivanjem koda. Sve čvorove treba nadograditi.**

- **Stadiji obrane se slažu.** Pripremno stanje, redovna i izvanredna obrana i
  izvanredno stanje proglašavaju se prema gore, a ukidaju obrnutim redom: kad
  se ukine redovna obrana, i dalje vrijedi pripremno stanje, dok se i ono ne
  ukine. Veći stadij smije se proglasiti odmah; kad se ukine, a uvjeti za niži
  postoje, niži se proglašava novim aktom. Stanje dionice računa se iz
  ovjerenih akata, pa ga svaki čvor iz istih akata vidi isto. Dosad prekid
  redovne obrane nije mijenjao stanje, a akt koji vrijedi unaprijed nikad nije
  otvorio obranu.
- **Akt stupa na snagu kad u njemu piše.** Kartica dionice pokazuje stadij koji
  vrijedi, od kada i kojim aktom, niže stadije u pozadini i akte koji tek
  stupaju na snagu („stupa na snagu 04.11.2026 09:00”). Isto stanje čitaju
  dnevno izvješće, zid sektora i knjiga dionice.
- **Ovjera pušta samo moguć slijed.** Ne ovjerava se prekid stadija koji ne
  traje, prekid nižeg dok viši traje, ni proglašenje nižeg dok viši traje;
  poruka kaže što tada traje. Ovjera skenom potpisanog akta ima iste uvjete
  kao ovjera u programu, i aktivnu obranu u sektoru, a provjerava se prije
  nego što se sken spremi.
- **Poništenje (storno) akta.** Ovjeren akt koji uopće nije trebao biti izdan
  poništava onaj tko ga je pripremio ili tko ga smije ovjeriti, uz razlog
  (*Akti* → akt → *Poništi akt (storno)*). Poništava se najkasniji akt
  dionice. Poništen akt ostaje u popisu, označen, a ne ulazi u stanje obrane;
  poništenje se potpisuje ključem čvora i razmjenjuje. Ispravak ide novim
  aktom.
- **Povijest obrane** (razdoblja na kartici dionice i historijatu letve)
  izvodi se iz akata, pa poništen akt nestaje i iz nje.
- **Izravne rute obrane uklonjene.** Proglašenje, podizanje i prekid obrane
  mimo akta (adrese koje nijedna stranica nije koristila) više ne postoje.
- **Prava.**
  - Izvješće dionice ili sektora više se ne može prepisati poznatim ID-om iz
    druge dionice ili sektora.
  - Privremena uprava (privremeno imenovanje, zamjena) ne dijeli trajnu
    upravu: dužnosti uprave koje dodijeli, i na nižoj razini, istječu s
    njezinom ovlasti; terenske dužnosti ostaju kako su dodijeljene.
  - Zastavicu globalnog administratora skida samo stalna uprava organizacije.
  - Uloga koju program ne zna (npr. stigla s novijeg čvora) ne daje pravo
    pisanja i ne može se dodijeliti.
  - Isključenom računu briše se sesija i kolačić, pa ponovno uključenje ne
    vraća staru prijavu.
- **Dežurstva.** Ponovljena predaja nakon greške više ne upisuje drugi razmak
  istih sati, pa se isti sati ne plaćaju dvaput. Dežurstvo dulje od 24 sata
  bez stanke upisuje se cijelo (npr. 72 sata kod spašavanja), ali ne
  prešutno: ne potvrđuje se samo, ni kad ga upiše uprava, nego dobiva
  napomenu i čeka da ga uprava pogleda.
- **List vodočuvara.** Odbijena predaja (nema opisa, neobrazložen zadatak) više
  ne briše upisano: list se spremi kao nacrt, a poruka kaže zašto predaja
  nije prošla.
- **Prognoza.** Kreće od izmjerene vrijednosti: ispravak promašaja više se ne
  primjenjuje na sam sat izdavanja (dosad je prognoza kretala nekoliko
  centimetara pored mjerenja). *Generiraj* gazi staro izdanje istog sata, kao
  i čvorovi koji izdanje primaju razmjenom.
- **Ostali ispravci.** Stadij vodotoka u izvješću sektora je najviši stadij
  njegovih dionica, bez obzira na redoslijed izvješća; temperatura i protok
  NaN se ne primaju; kraj dana je ispravan i na dan pomaka sata (vodostaji na
  listu dnevnika COP-a i listu vodočuvara); ovjera akta s neispravnom letvom
  više ne ruši program.
- **Linux.** Upute i instalacijska skripta s provjerom potpisa izdanja
  (docs/linux.md); od ovog izdanja uz izdanje ide i jedinica systemd.
  SECURITY.md: prijava ranjivosti i model povjerenja.
- **Nadogradnja.** Sve čvorove nadograditi na 0.0.34: stariji program ne zna
  za poništen akt pa bi ga i dalje brojao u stanje obrane.
- **Razvoj: sređivanje koda** (docs/STABILIZACIJA.md). Pokrivenost testovima
  52,5 → oko 58 %, kritičnog koda 52,5 → oko 61 %, prosječni CRAP 42,5 → oko
  26; `main` rastavljen na korake pokretanja s testom životnog ciklusa čvora.
  Oko 66 sumnjivih ponašanja zapisano je i zaključano testovima; ispravci iz
  ovog izdanja među njima su. Vrata kvalitete znaju za premještaje (funkcija
  izdvojena iz postojeće nasljeđuje njezino stanje) i stabilniji su na
  testovima koji su ovisili o sreći.

## 0.0.33-alfa — 4. 10. 2026.

**Ovlašteni primatelj, primanje na daljinu i članstvo koje čvor sam pokaže.
Sve čvorove treba nadograditi.**

0.0.32-alfa je povučen: njegova Docker slika stigla je na ghcr.io prije nego
što su u članstvu nađene dvije slabosti (rok članstva koje izda primatelj i
nepotpisani opozivi, ispravljeno niže); izdanje za Windows nije izašlo.
Nadograditi izravno na 0.0.33.

- **Članstvo u certifikatu.** Čvor pri svakom spajanju pokaže svoju potvrdu
  članstva (i ovlast za primanje, ako je ima). Drugi član je provjeri ključem
  mreže, pa novo računalo ne treba uparivati sa svakim čvorom: dovoljno je da
  ga primi jedan, a s ostalima se sinkronizira odmah, i prije nego što im
  njegova potvrda stigne razmjenom. Stariji programi taj dio certifikata
  preskaču.
- **Ovlašteni primatelj.** Nositelj ključa mreže može članu dati ovlast da
  prima druge (Administracija → Čvor, mreža i sinkronizacija → *Daj ovlast za
  primanje*), npr. uredskom poslužitelju, a ključ mreže ostaje kod nositelja.
  Članstvo koje primatelj izda nosi njegovu ovlast, pa ga svaki član provjeri
  sam. Ovlast vrijedi dvije godine, a članstvo koje primatelj izda najviše
  dok vrijedi njegova ovlast; to provjerava svaki član, pa ni ključ primatelja
  nakon isteka ovlasti ne može izdati članstvo s datumom unatrag. Ovlast daje
  samo nositelj ključa mreže.
- **Potpisan opoziv, koji vrijedi i za potvrdu koju čvor pokaže sam.**
  Opoziv članstva ili ovlasti zapisuje se u knjigu i putuje svim čvorovima;
  opozvana potvrda ne vrijedi ni kad je čvor pokaže pri spajanju. Opoziv
  ovlasti potpisuje ključ mreže, a opoziv članstva ključ mreže ili primatelj
  koji je to članstvo izdao; nepotpisan opoziv ne vrijedi, a opoziv se ne
  poništava (ponovno primanje izdaje novo članstvo). Oduzeta ovlast poništava
  članstva svih računala koja je primatelj primio, a ekran ih nabroji da ih
  se po potrebi primi ponovno. Opoziv članstva ovlaštenog primatelja oduzima
  mu i ovlast, pa ga može napraviti samo nositelj ključa mreže.
- **Primanje na daljinu.** Kad novo računalo i ured nisu u istoj lokalnoj
  mreži: na novom računalu *Postavljanje → Postojeća mreža → Napravi
  zahtjev*. Ono pokaže **kod za primanje** (8 znakova, npr. `7KQ4-M2XD`);
  datoteka zahtjeva ide primatelju e-poštom, a kod mu čovjek pročita
  telefonom. Primatelj učita zahtjev pod *Primanje na daljinu*, upiše kod i
  preuzme potvrdu, a novo računalo je učita na *Postavljanju*. Zahtjev i
  potvrda nose dokaz ključem izvedenim iz koda (scrypt), pa se podmetnuta
  datoteka odbija na obje strane, a kod se iz presretnute datoteke ne može
  pogoditi. Potvrda nosi i čvorove s adresom, pa sinkronizacija kreće odmah,
  a kad stignu djelatnici, stranica sama vodi na prijavu.
- **Kod uparivanja s obvezom unaprijed.** Strana koja zove obveže se na
  nasumičan broj prije nego što vidi broj druge strane, a kod ovisi o oba
  broja i oba ključa. Dosad je napadač u sredini mogao za nekoliko sekundi
  naći ključ koji daje isti kod na oba ekrana; sada je kod za njega slučajan
  (1 : 1 000 000). Uparivanje sa starijim programom se odbija.
- **Početna lozinka i svjež čvor samo iz lokalne mreže.** Početna lozinka
  računa `admin`, uparivanje svježeg čvora bez prijave i stranica
  *Postavljanje* vrijede samo s ovog računala ili s privatne adrese, kao za
  PIN. Dosad ih je odbijao samo zahtjev kroz tunel, pa je svjež čvor
  izravno izložen internetu (npr. kroz proslijeđen port) mogao preuzeti
  svatko tko zna početnu lozinku.
- **Nadogradnja.** Sve čvorove nadograditi na 0.0.33: stariji program
  ne prepoznaje članstvo koje je izdao ovlašteni primatelj i ne uparuje se s
  novim.
- **Razvoj: provjera kvalitete koda** (`make quality`, docs/CODE_QUALITY.md).
  Mjeri testove, race, coverage, složenost, CRAP, duplikacije i lint, a novi kod
  uspoređuje s izmjerenim početnim stanjem: zatečeni dug ne ruši provjeru, ali
  pogoršanje da. Kod iz ovog izdanja je prošao, uz vremenski ograničenu iznimku
  za rast primjene sinkroniziranih zapisa (`applyOne`), koja je prva stavka
  sređivanja. Uz to su riješena tri statička nalaza iz 0.0.31, bez promjene
  ponašanja.

## 0.0.31-alfa — 4. 10. 2026.

**Karte s OpenStreetMapa stvarno rade.**

- OpenStreetMapov poslužitelj pločica svakom zahtjevu preglednika bez adrese
  stranice vraća pločicu „Access blocked” (osm.wiki/Blocked). Stranice
  goCOP-a imaju `Referrer-Policy: same-origin`, pa adresa nikad nije išla
  van. Karte sada pločicama šalju samo ishodište (npr.
  `https://cop-osijek.com`), bez putanje i upita; ostali zahtjevi stranice i
  dalje ne odaju ništa. Pločice koje poslužitelj slaže u PDF već su prolazile.
- Slika za Unraid je klasični Docker manifest: od ovog izdanja Unraid sam
  javlja nadogradnju (za prelazak na ovu još jednom *Force update*).

## 0.0.30-alfa — 4. 10. 2026.

**Karte ponovno rade; Unraid sam javlja nadogradnju.**

- **Pločice karte s OpenStreetMapa.** Wikimedia je svoje pločice zatvorila
  za sve osim vlastitih stranica (403), pa karte nisu radile ni na jednom
  čvoru. Zadani izvor je sada OpenStreetMap (`tile.openstreetmap.org`), s
  istim podacima i izgledom. Čvor koji u `gocop.toml` ima upisan stari
  Wikimedijin izvor sam prelazi na OpenStreetMap i to zapiše u dnevnik.
  Vidi docs/karta.md (pravila korištenja; za stotine računala vlastiti
  poslužitelj pločica ili podloge DGU-a).
- **Slika za Unraid bez potvrde podrijetla.** S njom je registar davao OCI
  indeks, a Unraidova provjera nadogradnje traži klasični Docker manifest,
  pa nikad nije javljala novo izdanje (trebao je *Force update*). Od ove
  slike Unraid sam pokaže nadogradnju; za prelazak na nju još jednom
  *Force update*.
- Slika nosi Unraidovu ikonu (valovi) i poveznicu *WebUI*.

## 0.0.29-alfa — 4. 10. 2026.

**Ime čvora, Postavljanje bez javne lozinke i Postava 1.1.0.** Postojeći
čvorovi ništa ne osjete: zadržavaju svoja imena i račune.

- **Jedinstveno ime čvora.** Dosad je svaki čvor kojem nitko ne upiše ime
  bio `gocop-cvor`, pa bi dva takva računala u mreži tiho prepisivala ključ
  jedno drugome i miješala zapise. Ime se sada zadaje prije prvog pokretanja
  (instalacijski program, `gocop.toml`, `-node`), a inače ga svjež čvor
  izabere sam (ime računala i četiri nasumična znaka) i upiše u `gocop.toml`.
  Postojeća baza bez upisanog imena zadržava `gocop-cvor`.
- **Uparivanje odbija dvojnike**: računalo koje nosi ime ovog čvora ili ime
  poznatog čvora s drugim ključem, uz poruku što učiniti; ključ poznatog
  čvora više se nigdje ne prepisuje tiho.
- **Postavljanje svježeg čvora** (`/postavljanje`): nova mreža s vlastitim
  računom globalnog administratora (početni `admin` se isključuje), uz
  izričitu potvrdu da je to prvo računalo nove mreže, ili put do uparivanja
  s postojećom. Radi samo s tog računala ili uz jednokratni kod iz dnevnika
  (poslužitelji, npr. Unraid); kroz tunel ne.
- **Početna lozinka pod Postavom samo s tog računala**: čvor sluša za cijelu
  lokalnu mrežu, pa u uredu javnu lozinku iz uputa više ne može upotrijebiti
  nitko osim vlasnika.
- **`gocop -pripremi`**: Postava prije prvog pokretanja upiše ime čvora iz
  instalacijskog programa; postojeće ime nikad ne mijenja. Dio ugovora s
  Postavom, uz test.
- **Postava 1.1.0** (zasebno izdanje `postava-v1.1.0`): u instalacijskom
  programu ime računala u mreži i važan izbor nove ili postojeće mreže (uz
  potvrdu za novu), pri prvom pokretanju otvara *Postavljanje*; *Ukloni goCOP*
  u izborniku Start i u izborniku ikone; ponovno pokrenut instalacijski
  program nudi *Popravi ili nadogradi* ili *Ukloni goCOP*.

## 0.0.28-alfa — 3. 10. 2026.

**goCOP Postava: instalacija, nadogradnja i ikona u traci.** Prvo izdanje s
gotovim programima na GitHubu; od njega Postava može instalirati i nadograditi
čvor.

- **Programi uz izdanje.** Uz svako izdanje na GitHubu stoje programi za
  Windows (amd64), Linux (amd64) i macOS (arm64, amd64), `SHA256SUMS` i
  potpis `SHA256SUMS.sig` ključem izdanja. Ključ ne odlazi na GitHub: izdanje
  nastaje kao nacrt i objavljuje se tek nakon potpisa.
- **goCOP Postava** (`gocop-postava.exe`, svoja izdanja `postava-v…`): mali
  program koji instalira goCOP za korisnika bez administratorskih prava,
  preuzme najnovije potpisano izdanje (ili ga uzme s USB-a), pali i gasi
  čvor, drži ikonu valova u traci, stavlja `gocop` u PATH i u pokretanje pri
  prijavi. Nadogradnja je na klik: provjera potpisa, kopija baze, zamjena,
  provjera da novo izdanje odgovara, a ako ne odgovori, vraćanje prethodnog.
  Postava nikad ne mijenja samu sebe, pa na Windowsu ne treba prepisivati
  program koji radi. Instalacijski program za Windows (`goCOP-postava-….exe`,
  Inno Setup) i upute u poglavlju 6 administratorskih uputa.
- **Za Postavu u čvoru:** `gocop -version` ispiše samo izdanje,
  `gocop -upravitelj` se uredno gasi kad mu se zatvori standardni ulaz
  (Windows nema SIGTERM), a `GET /zdravlje` javlja izdanje samo zahtjevu s
  istog računala (kroz tunel i iz mreže je 404). Taj ugovor čuva test.
- **Ikona programa** (valovi) i podaci o izdanju u programima za Windows.

## 0.0.27-alfa — 3. 10. 2026.

**Ovlasti, oporavak administratora i novi izgled.** Nadograditi najprije čvor
dostupan kroz tunel. Nakon nadogradnje dio djelatnika treba ponovno upisati
lozinku e-pošte (vidi Sandučić), a pravo pisanja suženo je na doseg dužnosti
(vidi Ovlasti).

- **Adresa koju potvrdi administrator.** PIN za prijavu izvana ide i na adresu
  izvan dopuštene domene (npr. djelatnika licencirane firme) kad je globalni
  administrator na tuđem računu, ne tuđim očima, u uređivanju profila označi
  *Adresa je provjerena*. Potvrda pamti tko ju je dao i kada, stiže na sve
  čvorove i vrijedi samo za tu adresu; vlastita promjena adrese je briše.
  Adresu koju ima još netko PIN ne dobiva ni potvrđenu.
- **Oporavak lozinke s konzole.** `gocop -ponisti-lozinku ime` na računalu na
  kojem čvor radi (na Unraidu kroz `docker exec`, vidi upute) postavlja
  privremenu lozinku i ispiše je jednom; gasi prijave, zapamćena računala,
  prijave na čekanju i privremene kodove, uklanja potpisni ključ i spremljenu
  lozinku e-pošte. Prava računa ne mijenja; isključen račun uključuje tek
  izričita zastavica `-aktiviraj`. Kroz web i razmjenu se ne može pokrenuti.
- **Lozinka koju postavi netko drugi** (poništenje, lozinka upisana u obrascu
  djelatnika, oporavak s konzole) uklanja osobni potpisni ključ, zaključan
  starom lozinkom: dosad je obavezna promjena lozinke na njemu zapinjala.
  Već potpisani dokumenti ostaju provjerljivi; novi ključ osoba napravi na
  profilu. Lozinka iz obrasca sada se mora zamijeniti pri prvoj prijavi.
- **Sandučić.** Spremljena lozinka e-pošte briše se uz poništenje i lozinku
  od administratora, a na svim čvorovima prestaje vrijediti čim se promijeni
  lozinka za goCOP (i vlastitom promjenom). Pri prvom pokretanju ovog
  izdanja brišu se i lozinke e-pošte za koje se ne može potvrditi da su
  spremljene uz sadašnju lozinku za goCOP; te treba upisati ponovno. Tko
  poništi tuđu lozinku više ne čita tuđu poštu, a tuđim očima sandučić se
  ne otvara.
- **Ovlasti.**
  - Pravo pisanja ide po dosegu dužnosti: dužnost sektora piše u sektoru,
    dužnost područja u svom području, dužnost na dionicama na tim dionicama,
    njihovim objektima i aktima te u dnevnicima svog područja i COP-a.
    Dosad je dužnost područja ili dionice pisala po cijelom sektoru.
  - Sektor dužnosti uvijek se uzima iz područja, a dionice moraju biti iz
    tog područja; uprava ne dodjeljuje dužnost sama sebi.
  - Račun bez aktivne dužnosti, isključen račun i globalnog administratora
    zadužuje samo globalni administrator; uprava koja ne smije uređivati
    cijeli račun dodaje samo ispomoć (bez primarnosti i vlastitog naziva).
  - Lozinku i privremeni kod uprava daje samo osobama niže razine kojima
    smije uređivati cijeli račun; osobama s dužnošću na njezinoj razini
    uprave ili višoj daje ih viša razina ili globalni administrator.
    Dužnost koja daje upravu na njezinoj razini privremena uprava dodjeljuje
    najdulje do isteka vlastite uprave nad tim sektorom ili područjem, a
    spremanje tuđe postojeće dužnosti joj ne skraćuje rok.
  - Zastavicu globalnog administratora postavlja samo stalna uprava
    organizacije, ne privremena.
  - Tuđim očima tuđa spremljena lozinka e-pošte ne koristi se nigdje (ni
    sandučić, ni adresar i njegova usporedba s imenikom, ni slanje akta).
  - Adresa e-pošte koju već ima drugi aktivni račun odbija se svima;
    korisničko ime koje postoji i drugim slovima se odbija.
  - Uklanjanje potpisnog ključa traži lozinku; tuđim očima ključ se ne
    pravi ni ne uklanja. Novi certifikati nose „Ime Prezime (korisničko)”.
- **Izgled u bojama Hrvatskih voda.** Jedan jezik ploča u cijeloj aplikaciji,
  sređena tamna tema i *Administracija › Tema*: glavna boja, naglasak i gumb
  za svijetlu i tamnu temu, s pregledom i provjerom čitljivosti (ispod 3:1
  tema se ne sprema). Tema putuje razmjenom. Karta u tamnoj temi, ikone koje
  su nedostajale i čitljiviji sitni tekst.
- **Uzdužni profil na mobitelu.** Crtež je visok za čitljive natpise i lista
  se vodoravno od nizvodnog kraja; preko cijelog zaslona popuni vidljivi dio
  zaslona (i na iPhoneu), a oblačić s vrijednostima radi i na dodir.

## 0.0.26-alfa — 3. 10. 2026.

**PIN za prijavu izvana.** Nadograditi najprije čvor dostupan kroz tunel.
Prekidač je zadano isključen: dok ga administrator ne uključi, PIN se ne
traži. Odmah vrijede pravila za vlastitu adresu e-pošte, brojanje prijava na
Exchange i gašenje prijava uz lozinku od administratora (stavke ispod).

- **Drugi korak prijave izvana.** Prijava kroz posrednika ili s javne adrese
  nakon točne lozinke traži šesteroznamenkasti PIN poslan na službenu
  e-poštu osobe (zadano samo `@voda.hr`), rezervni kod s profila (deset
  jednokratnih) ili privremeni kod administratora (24 h, jednom). Prijava iz
  lokalne mreže PIN nikad ne traži. PIN vrijedi 10 minuta i jednom; pet
  krivih upisa poništi prijavu na čekanju, deset u satu zaključa upis kodova
  na sat. Preglednik se može zapamtiti na 30 dana; promjena lozinke, na bilo
  kojem čvoru, to poništi. Radi samo preko HTTPS-a (posrednik mora slati
  `X-Forwarded-Proto: https`, što cloudflared radi sam); izvana preko
  nešifriranog http-a prijava se odbija jasnom porukom.
- **Pošiljatelj PIN-a.** Račun u domeni upisuje se na čvoru iza tunela
  (Administracija → E-pošta), provjeri prijavom na Exchange i čuva šifriran
  ključem čvora. PIN se šalje bez kopije u Poslanim stavkama i ne piše u
  dnevnik. Kad Exchange odbije lozinku, slanje staje dok je administrator ne
  upiše ponovno; kad poslužitelj nije dostupan, stane na pet minuta. Najviše
  tri PIN-a osobi u 15 minuta i 60 na sat po čvoru.
- **Prekidač** je zajednička postavka, a uključuje se samo izvana preko
  HTTPS-a, na čvoru koji ima pošiljatelja i nakon uspješnog probnog PIN-a;
  isključuje se odasvud. Čvor koji je u zadnjih sedam dana (otkad program
  radi) primio prijavu izvana, a PIN nema čime slati, diže uzbunu na
  stranicama administracije i u dnevniku; čvor samo u lokalnoj mreži je ne
  diže.
- **Prijave računa u domeni** (lozinka osobnog sandučića i pošiljatelj PIN-a)
  broje se po osobi i računu: najviše tri u pola sata, da goCOP ne zaključa
  račun u domeni. Prijava koju poslužitelj primi briše brojač tog računa i
  osobi ne troši upis; uz to vrijede najviše tri neprihvaćena upisa lozinke
  osobe u 15 minuta po obrascu, pa je pogađanje tuđih lozinki kroz goCOP
  sporo (vlastita točna lozinka više ne briše tu granicu).
- **Veza prema Exchangeu po imenu i lozinci.** Prijavljena veza dosad se
  dijelila po imenu računa, pa je druga lozinka istog imena (i druga osoba
  koja upiše tuđe ime) prolazila vezom vlasnika: kriva lozinka spremala se
  kao provjerena, a tuđi sandučić se mogao čitati. Sada svaka lozinka ima
  svoju vezu, a upis lozinke uvijek se provjerava novom prijavom.
- **Profil i Korisnici.** Osoba vidi svoja zapamćena računala i rezervne
  kodove; administrator izdaje privremeni kod. Vlastitu adresu e-pošte osoba
  mijenja samo iz lokalne mreže, uz trenutnu lozinku i tek nakon zamjene
  početne lozinke; adresa mora biti u
  dopuštenoj domeni (zadano `voda.hr`) i ne smije pripadati drugom aktivnom
  djelatniku, a promjena se javlja na staru adresu.
- **Lozinka koju administrator postavi** kroz obrazac korisnika gasi i
  otvorene prijave te osobe, kao poništenje lozinke.
- **Vremena iz razmjene** s pomakom koji mjesna zona nema (npr. čvor s
  `TZ=UTC`) spremaju se čitljivo u svim tablicama, ne samo u dnevnicima.

## 0.0.25-alfa — 2. 10. 2026.

**Sigurnosno učvršćivanje.** Nadograditi najprije čvor dostupan kroz tunel.

- **Uparivanje i primanje u mrežu samo administrator.** Dosad je svaki
  prijavljeni korisnik, i kroz tunel, mogao upariti svoje računalo i dobiti
  članstvo u mreži, a s njim sve podatke. Sada uparivanje pokreće i potvrđuje
  globalni administrator; svjež čvor bez računa nudi uparivanje samo iz
  lokalne mreže, nikad kroz tunel.
- **Zadana lozinka izvana ne vrijedi.** Javna je, pa prva prijava njome ide iz
  lokalne mreže; izvana s privremenom lozinkom od administratora.
- **Prijava.** Poruka ne otkriva postoji li račun ni je li deaktiviran (ni
  vremenom odgovora). Pokušaji se broje po imenu s adrese, po adresi i, za
  prijave izvana, po imenu, pa napadač više ne zaključava tuđi račun s pet
  pokušaja, a napad s interneta ne zaključava prijavu iz ureda. Ponovni upis
  lozinke (potpis, ovjera, e-pošta) ograničen je na tri pokušaja u 15 min.
  Promjena lozinke gasi ostale prijave te osobe na istom čvoru.
- **Sesija.** Kolačić je iza HTTPS-a (tunel) i `Secure`; identifikator sesije
  je nasumičan (UUIDv4, 122 bita) umjesto vremenskog.
- **Zaštita od tuđih stranica.** Izmjene (POST) koje pokrene tuđa web-stranica
  odbijaju se; usporedba imenika s Exchangeom, koja mijenja podatke, ide
  preko POST-a umjesto GET-a.
- **XSS.** Geometrija vodotoka, nazivi iz registra u obrascu dionice i nazivi
  koje javljaju računala s lokalne mreže (uparivanje) idu u stranicu samo kao
  tekst. Privici e-pošte i slike poslužuju se u pješčaniku; u pregledniku se
  otvaraju samo PNG, JPEG, GIF i WebP, a SVG i HTML se preuzimaju.
- **Zaglavlja.** `nosniff`, `Referrer-Policy: same-origin`, zabrana ugradnje u
  okvir, osnovni CSP i `Permissions-Policy`; iza HTTPS-a HSTS.
- **Pouzdani posrednici.** Adresa klijenta iz `CF-Connecting-IP` vrijedi samo
  od pouzdanog posrednika (zadano ovo računalo i privatne mreže; suzi se u
  `gocop.toml` `[web] pouzdani_posrednici`); zahtjev sa zaglavljem posrednika
  uvijek je vanjski. Krivo zadan posrednik zapisuje se u dnevnik.
- **Ograničenja.** Rokovi po ruti (obična stranica minuta, uvozi i izvozi 30
  min, tok događaja bez roka uz ping), tijelo zahtjeva zadano 2 MB, rute s
  datotekama svoje granice; prevelika datoteka odbija se umjesto da se reže.
  Poruka razmjene najviše 256 MiB, delta 32 MiB (ostatak odmah u nastavku),
  tunel 32 veze i 2 po klijentu uz rukovanje od 5 s, port razmjene 32
  rukovanja, a razmjena najviše 4 odjednom (višak dobije „čvor je zauzet”).
- **`/api/areas`** samo prijavljenima; sigurne povratne adrese.
- **GitHub provjera.** `go vet`, testovi i `govulncheck` na svako slanje i
  tjedno; slika za Unraid gradi se tek kad provjera prođe.
- **Vremena dnevnika iz razmjene** čitljiva su i na čvoru čija zona nije
  hrvatska.

## 0.0.24-alfa — 2. 10. 2026.

**Nadogradnja redom** — najprije stalni čvor, odmah zatim ostali. Dok svi ne
rade na ovom izdanju, ne mijenjati djelatnike ni zaduženja: 0.0.23-alfa i
starija izdanja pri pokretanju još prekodiraju korisnike i zaduženja.

**Tok obavijesti samo prijavljenima** — `/api/events` bio je otvoren bez
prijave i drugim web-stranicama, a pri izmjeni djelatnika slao je njegov cijeli
zapis, s telefonima i e-poštom; na čvoru dostupnom kroz tunel mogao ga je
čitati bilo tko. Sada ga dobivaju samo prijavljeni, s iste stranice, a
obavijest o djelatniku ili zaduženju nosi samo identifikator.

**Razmjena javlja izdanje programa** — čvorovi u razmjeni javljaju na kojem
izdanju rade. Pločica „Razmjena s čvorovima” i stranica Sinkronizacija
pokazuju izdanje svakog čvora. Za čvor na drugom izdanju stoji upozorenje da
treba ažurirati stariji od dvaju čvorova, a za čvor koji izdanje ne javlja da
njega treba ažurirati. Pločica tada ne javlja da je sve usklađeno.

**Izmjena čuva polja novijeg programa** — kad čvor izmijeni zapis, polja koja
njegov program ne poznaje (dodao ih je noviji program) prepisuju se iz
prethodne verzije. Dosad ih je čvor sa starijim programom svakom izmjenom
tiho brisao. Polje koje je korisnik ispraznio ne vraća se. Isto vrijedi za
popravke podataka pri pokretanju, koji sad polaze od zadnje verzije u knjizi.

**Zapis novije sheme se ne prepisuje** — svaka verzija nosi shemu svog
entiteta (zasad je svima 1). Zapis koji je zadnji izmijenio program s novijom
shemom ovaj ne prepisuje, nego javlja da prije uređivanja treba ažurirati
goCOP. Popravci podataka, postavke obračuna i katalog sredstava takav zapis
pri pokretanju preskoče i to zapišu, a čvor se normalno pokrene. Zapisi
novije sheme i entiteti koje program ne poznaje spremaju se i prenose dalje,
a zapisnik i pločica razmjene javljaju da treba ažurirati goCOP. Paket `.cop`
starije inačice i dalje se ugrađuje, a paket novijeg programa odbija se uz istu
poruku.

**Arhivirano nestaje i na drugim čvorovima** — obrisana bilješka uz arhivsku
vrijednost ostajala je na čvorovima koji su je primili; sada se i tamo
uklanja. Isto vrijedi za vezu dionice s obrisanom letvom te za epizode
obrane, ispravke arhive i nazive razina ustroja kad stignu arhivirani (nazivi
se tada vraćaju na zadane).

**Korisnici i zaduženja se više ne prekodiraju** — novi korisnik odmah dobije
stalni identifikator iz korisničkog imena, kao i korisnici iz početnih
podataka; kad je taj već zauzet (obrisan ili preimenovan račun), dobije
nasumični. Pokretanje zato više ne prekodira ni korisnike ni zaduženja.
Prekodiranje je mijenjalo već razmijenjene verzije na mjestu, pa je ista
verzija na dva čvora imala različit sadržaj, a brojanje zaduženja jednom je
spriječilo pokretanje čvora (0.0.17-alfa).

**Sažimanje knjige čuva granicu svakog čvora** — „Sažmi knjigu” (Administracija
→ Održavanje baze) više ne briše zadnju verziju koju je neki čvor upisao u
kanal, ni kad je zapis poslije izmijenio drugi čvor. Bez nje bi granica tog
čvora pala, a drugi čvorovi slali bi te verzije natrag svakom razmjenom.

## 0.0.23-alfa — 2. 10. 2026.

**Arhiva kiše raste i na čvoru koji ne preuzima** — čvor nije primao pakete
za letve koje je nekad sam sagradio iz svog stabla, pa je laptop, otkad
vodostaje i kišu preuzima Unraid, ostao bez novih zapisa 35 Pljuskovih i 6
DHMZ-ovih kišomjera. Čvor koji sam preuzima i dalje čuva svoje letve; čvor
koji ne preuzima ugrađuje paket kad on nosi sve što lokalna letva ima (do
istog ili kasnijeg dana i barem jednako zapisa). Paket koji seže kraće se ne
ugrađuje, pa se ništa ne gubi.

## 0.0.22-alfa — 2. 10. 2026.

**Ploča razmjene više ne broji poslano kao neposlano** — čvor je pamtio dokle
drugi čvor zna onako kako mu je ovaj javio na početku razmjene, pa je ono što
mu je u istoj razmjeni poslao do sljedeće razmjene brojao kao da još čeka
(Unraid je laptopu stalno „slao još 101 verziju”, a laptop je bio usklađen).
Zapamćena granica sada uključuje i poslane verzije.

## 0.0.21-alfa — 2. 10. 2026.

**Prva razmjena odmah nakon pokretanja** — pola minute nakon pokretanja, a ne
nakon punog razmaka od pet minuta: ažuriranje ili nekoliko pokretanja zaredom
više ne ostavljaju čvorove neusklađene četvrt sata.

**Kazalo arhive bez starih verzija** — pri pokretanju i svakih šest sati iz
knjige se brišu zamijenjene verzije kazala arhive (vrijedi samo zadnje izdanje
letve). Uklanja i oko 4.700 zapisa nakupljenih dok su se čvorovi nadglasavali.

## 0.0.20-alfa — 2. 10. 2026.

**Naziv čvora stiže do drugih čvorova** — naziv upisan u `gocop.toml` nakon
uparivanja čvor pri pokretanju sam objavi, pa ga drugi čvorovi vide umjesto
imena računala iz uparivanja. Mijenja se samo naziv; javne adrese ostaju.

## 0.0.19-alfa — 2. 10. 2026.

**Kazalo arhive bez nadglasavanja** — čvor objavljuje paket arhive samo kad je
njegovo izdanje novije od onoga što kazalo već ima. Kad je stalni čvor kao novi
izdavač izdao novija izdanja kišomjera, laptop je svake dvije minute ponovno
objavljivao svoja starija, a stalni čvor svoja: knjiga je rasla po 1.200
zapisa na sat, a pločica je stalno javljala „šalje se još 40 verzija”.

**„Javlja se sam” umjesto „ne odgovara”** — čvor s nedavnom uspješnom
razmjenom prikazuje se kao na mreži i kad ga ovaj čvor ne može nazvati (laptop
izvan kuće sam zove kroz tunel), uz tu napomenu umjesto greške mreže.

## 0.0.18-alfa — 2. 10. 2026.

**Izvoz prognoze uvijek svjež** — poveznice „Izvoz u Excel” i „Pričuvni
izračun” pri svakom učitavanju stranice dobivaju novu adresu, pa preglednik
ne može dati staru spremljenu datoteku (kopija spremljena dok je Cloudflare
slao „čuvaj 4 sata” inače bi i dalje stizala iz preglednika).

## 0.0.17-alfa — 2. 10. 2026.

**Čvor se pokreće bez obzira na redoslijed zaduženja** — pri pokretanju se
zaduženja prekodiraju na stalne identifikatore „korisnik + redni broj”, a broj
se uzimao iz redoslijeda redaka u lokalnoj bazi. Razmjena upiše zaduženja kako
stignu, pa je na drugom čvoru redoslijed bio obrnut: prekodiranje je zamijenilo
dva zaduženja, palo na jedinstvenosti i čvor se nije dao pokrenuti. Zaduženje
koje već nosi stalni identifikator svog korisnika sad ostaje kakvo jest, a
staro nasumično dobije prvi slobodan broj.

## 0.0.16-alfa — 2. 10. 2026.

**Ništa u priručnu memoriju posrednika** — svaki odgovor osim `/static/` nosi
`Cache-Control: private, no-store`. Cloudflare je izvoz prognoze (`.xlsx`)
prema nastavku čuvao do 4 sata i davao ga svakome, i bez prijave: drugi
korisnik dobivao je staru prognozu, a izvoz se dao preuzeti bez računa. Uz
ovo je na Cloudflareu dodano pravilo da se za cop-osijek.com ništa ne sprema.

## 0.0.15-alfa — 2. 10. 2026.

**Upozorenja DHMZ-a pregledno** — zeleno upozorenje (DHMZ-ovo „nema
upozorenja”) više se ne prikazuje kao upozorenje: umjesto sedam istih kartica
stoji jedan redak „županija: nema upozorenja” s razdobljem, a „drugdje u
Hrvatskoj” broji samo prava upozorenja. Redak unutar kartice nosio je klasu
crvenog upozorenja (ružičasta traka u svakoj kartici), a obojena upozorenja
dobila su tamnu temu, pa su čitljiva.

## 0.0.14-alfa — 2. 10. 2026.

**Letva razmjenom sa svim poljima** — primljena letva nije upisivala šest
polja koja zapis nosi: uvoz s Geolux HydroViewa (`telemetrija_site`,
`telemetrija_uvoz`), ograde niza, povijest, opis vodokaza i datum osnivanja.
Stalni čvor zato nije preuzimao 31 HydroView letvu, među njima dva vrha lanca
(Beničanci – Prkos, Kapelna), i prognoza je stala. Čvor pri pokretanju letve
obnovi iz knjige, pa se polja popune sama. Novi test prenosi letvu sa svim
poljima s čvora na čvor i pada čim letva dobije polje koje razmjena ne prenosi.

## 0.0.13-alfa — 2. 10. 2026.

**Razmjena kroz tunel bez rušenja** — WebSocket na strani poslužitelja nema
adresu druge strane, a bilješka o razmjeni ju je ispisivala: prva veza kroz
tunel srušila je čvor. Veza kroz tunel sad ima svoju adresu, a greška u jednoj
vezi razmjene više ne može srušiti cijeli čvor — veza se zatvori i zapiše.

## 0.0.12-alfa — 2. 10. 2026.

**Razmjena kroz web tunel** — čvor koji izvana smije samo na web (Cloudflare
tunel, bez otvorenog porta) prima razmjenu i na `https://<adresa>/razmjena/tunel`,
kroz WebSocket. Unutra teče isti TLS s ključevima čvorova kao na portu razmjene:
nepoznati ključ ne dobiva ni bajt, a tunel vidi samo šifrirane bajtove. Druga
strana se onamo spaja kad je adresa čvora `https://domena` (Postavke →
Domenski čvorovi); u istoj mreži prvo se pokušava port razmjene.

**Razmjena na naslovnoj** — pločica „Razmjena s čvorovima”: za svaki upareni
čvor zadnja razmjena, primljeno i poslano i koliko se još šalje ili prima;
napredak arhive (ugrađeno od koliko paketa, što se upravo ugrađuje, koliko se
još dohvaća); sadržaj koji čeka dohvat; tko izdaje prognozu i kad je stiglo
zadnje izdanje, s upozorenjem kad je starije od tri sata. Kad je sve usklađeno,
jedan redak. Osvježava se sama.

## 0.0.11-alfa — 2. 10. 2026.

**Kiša razmjenom** — uz svako izdanje prognoze idu mjerenja kišomjera zadnjih
48 sati i satna kiša Open-Meteo po točkama slivova s prognozom (oko 80 kB po
izdanju). Čvor koji ne izdaje prognozu kišu upiše u svoju bazu oborina, pa i
on ima svježa očitanja kišomjera, ne samo arhivu.

**Arhiva kiše raste sama** — nakon noćnog ulaganja kiše izdavač odmah izda
pakete promijenjenih kišomjera i kazalo pošalje razmjenom, bez ručnog
izdavanja arhive.

**Izdavanje nastavlja primljeni niz** — čvor koji je letvu primio paketom, a
sam je još nije izdavao, nastavlja broj izdanja onoga od koga ju je primio:
isti sadržaj zadrži broj, novi dobije sljedeći. Inače bi čvor koji preuzme
izdavanje krenuo od v1, a ostali bi ga odbili kao starije izdanje.

## 0.0.10-alfa — 2. 10. 2026.

**Kiša se ne ulaže bez stabla izvornih datoteka** — čvor koji izdaje prognozu
jednom dnevno ulaže izmjerenu kišu u arhivu i letvu iznova gradi iz stabla.
Čvor koji je arhivu dobio paketima stabla nema, pa bi gradnja meteorološkim
postajama povijest svela na zadnja četiri dana. Bez stabla se ulaganje sad
preskače i javlja u ispisu kruga.

**Telemetrija jasnije** — računi nose naziv sustava i adresu (Geolux HydroView,
hdv.voda.hr; mobilna stranica Hrvatskih voda, mletva.voda.hr), a stranica kaže
treba li ih ovaj čvor: računi trebaju samo čvoru koji preuzima vodostaje.

## 0.0.9-alfa — 2. 10. 2026.

Popravci koje je pokazao prvi stalni čvor (Unraid) pri prvoj razmjeni.

**Prva razmjena bez rupa na površini** — novi čvor registar prima u paketima
po 5000 verzija, pa je naselje znalo stići prije svoje općine i ostati samo u
knjizi (2205 naselja i jedna dionica). Neuspjeli zapisi sad se pokušavaju
iznova u istom prijenosu i sa svakom sljedećom razmjenom, dok ne prođu.

**Prva razmjena odjednom** — dok je razgovor pun (5000 verzija), razmjena
nastavlja odmah, do 200 razgovora zaredom, umjesto po 5000 svakih pet minuta.

**Brava arhive u spremniku** — program je u spremniku uvijek proces 1, pa je
brava prekinute ugradnje paketa nakon ponovnog pokretanja izgledala živom i
zaustavila bi ugradnju arhive. Brava sad razlikuje dva života istog procesa.

**Osnivanje mreže uz zatečeni ključ** — datoteka ključa mreže bez zapisa u
bazi više ne zaustavlja osnivanje: ključ se preuzme, ne pregazi.

## 0.0.8-alfa — 2. 10. 2026.

**Novi čvor s praznom bazom se pokreće** — jednokratni popravak registra
(gradovi Ivanec i Vrbovec) padao je na praznoj bazi jer županija još nema dok
registar ne stigne razmjenom, pa se novi čvor u spremniku nije dao pokrenuti.
Sad se preskače; gradovi stižu razmjenom s čvora na kojem je popravak izveden.

## 0.0.7-alfa — 2. 10. 2026.

**Uloge čvora** — u Postavkama čvora (Uloge ovog čvora) uključuje se preuzima
li čvor vodostaje s izvora i izdaje li prognozu. Vodostaje dovoljno je da
preuzima jedan čvor: očitanja putuju razmjenom. Čvor od prije zadržava što je
radio; novi ne radi ni jedno dok mu se uloga ne uključi.

**Prognoza razmjenom** — svako izdanje (satno i dnevno, izbor rezervi, kiša po
međuslivovima i tuđe prognoze zadnjih 48 sati) ide u knjigu verzija, a
namješteni model kad se promijeni. Ostali čvorovi izdanje upišu u svoju bazu
prognoza i prikazuju ga s oznakom čvora koji ga je izdao; kiša po slivovima na
naslovnoj kod njih dolazi iz izdanja. U razmjeni izdanja stoje sedam dana (oko
100 kB po izdanju), u bazi prognoza ostaju.

**Arhiva razmjenom** — kazalo .cop paketa (letva, izdanje, razdoblje, veličina)
drže svi čvorovi; sam paket dohvaća čvor kojemu ga pokriva pretplata (nova
vrsta „Arhiva vodostaja”, po području letve) ili koji prati sve, od bilo kojeg
čvora koji ga ima, i ugradi ga nakon provjere otiska. Starije izdanje se ne
ugrađuje, a letva sagrađena na samom čvoru se ne gazi.

**Slika za spremnik** — `ghcr.io/tkraljevic/gocop` gradi se za svako izdanje;
`/data` za bazu i postavke, `/arhiva` za arhivu, stablo, skenove i pakete.

**Izvoz prognoze** — redak „model” (satni/dnevni) skriven je u grupi i otkriva
se gumbom „+” uz rub. Gornja Radgona maknuta je s uzdužnog profila Mure jer za
nju nema prognoze.

## 0.0.6-alfa — 1. 10. 2026.

**Vukovar sa svojim protokom** — iz HIS-2000 uvezeni su protoci Vukovara
2001.–2025. (satni i, prvi put, dnevni; dotad satni samo do 2018.) i krivulja
2025.–2026. od −100 cm (dotad −80), pa sažetak više ne uzima protok Iloka;
godišnji protoci Vukovara imaju i 2019.–2025. Izdan je `vukovar_v6.cop`.

## 0.0.5-alfa — 1. 10. 2026.

**Čitljivost izvoza** — usporedba s mađarskom prognozom na sažetku dobiva
zaglavlje u dva retka (termin preko triju stupaca, ispod naša · HU · razlika),
a razlika je pravi broj s predznakom iz formata ćelije (bez Excelova upozorenja
„broj kao tekst”); provjera na poplavnim valovima na listu „O prognozi” ima
naslov skupine preko cijele širine (rijeka, model, broj valova) umjesto
odrezanog naziva u uskom stupcu; napomene uz godišnje vodostaje i protoke
dobivaju zalihu visine na uskim listovima.

## 0.0.4-alfa — 1. 10. 2026.

**Sažetak prognoze u Excelu** — novi prvi list „Sažetak” za čitatelje kojima je
puni izvoz previše, A4 položeno: rečenice „Ukratko” koje program sam slaže iz
brojki (rijeka pada, raste ili je stabilna, najveća promjena s protokom, mogući
novi najniži ili najviši zabilježeni vodostaj, pragovi obrane, kiša po
slivovima, razlike prema mađarskoj prognozi od 10 cm naviše), tablica naših
postaja s vodostajem i protokom za sada, sutra, za 3 dana i zadnji dan
prognoze, kretanjem, stanjem obrane i napomenom, te usporedba s mađarskom
prognozom. Protok Vukovara u sažetku je protok Iloka (mjerenih protoka
Vukovara nema od 2019.), označen kurzivom. Najniži i najviši zabilježeni
vodostaj čitaju se iz arhive prije sažetka, da odmah nakon pokretanja ne
stoje samo ovogodišnji.

**Prelamanje teksta i visina redaka u izvozu** — podnaslov svakog lista,
odlomci i tablice lista „O prognozi” te napomene uz godišnje vodostaje i
protoke dobivaju visinu prema duljini teksta (Excel spojenim ćelijama visinu
ne prilagodi sam), pa se tekst ne reže.

**Protok dnevnog modela** — ostaje i kad granica raspona ispadne iz krivulje
protoka (Vukovar 5. i 6. dan).

## 0.0.3-alfa — 1. 10. 2026.

**Ispravak brojki na naslovnoj (Podaci u sustavu)** — pravi kišomjeri broje se
kao različite postaje (109, prije 107: DHMZ-ove postaje sa samo satnim ili samo
dnevnim nizom brojale su se po većem nizu); popuna kratkih rupa pravih
kišomjera iz ERA5 više se ne broji kao oborina po slivovima nego stoji uz
kišomjere; oborina po slivovima navodi obje reanalize (ERA5 i CERRA); padeži
uz brojeve.

## 0.0.2-alfa — 1. 10. 2026.

**Kiša po slivovima na naslovnoj** — za svaki međusliv koji ulazi u prognozu
kiša pala u zadnja 24 i 72 sata i očekivana u sljedećih 48 sati, prema onome
što je za taj međusliv uobičajeno (ERA5 od 1990.): žuto kad toliko padne
prosječno tri puta godišnje, narančasto jednom godišnje, crveno jednom u pet
godina. Upozorenje kaže na kojim će letvama porasti voda, redom niz tok, s
najvećim porastom i danom iz zadnje dnevne prognoze; gleda i kišu koja tek
dolazi, pa se može pojaviti dan-dva prije kiše. Provjereno na kolovozu 2023. i
rujnu 2024. (crveno na Dravi i Muri dva dana prije vrha kiše).

## 0.0.1-alfa — 1. 10. 2026.

Prvo označeno izdanje. Program se koristi i provjerava u COP-u Osijek, ali
nije za operativnu upotrebu bez nadzora: sve se još mijenja.

**Operativa obrane** — teren i očitanja, pragovi i akti o stupnjevima obrane,
dnevnik COP-a, dežurstva i obračun IORS, vodočuvarska knjiga, prijave s
terena, dnevna izvješća dionica i sektora, dnevnici usluga A.02 i A.03.

**Registri** — ustroj organizacije, dionice, vodomjerne postaje, vodotoci,
objekti, teritorijalne jedinice s kartom sektora i branjenih područja,
djelatnici, izvođači, međuslivovi s izvedenim točkama i pravim kišomjerima
(DHMZ, pljusak.com, nacionalne službe s DanubeHIS-a), materijalno-tehnička
sredstva.

**Hidrološka prognoza** — satni lanac do 96 sati i dnevni model do 6 dana s
kišom po međuslivovima; model ispuštanja HE Dubrava, Čakovec i Varaždin (i iz
razine akumulacija); rezerve za svaki izvor (letve na suprotnoj obali,
DanubeHIS za mađarske letve, ponovni pokušaj kad upit istekne); uzdužni profil;
satni, dnevni ili kombinirani prikaz po izboru; pričuvni izračun u Excelu za
dane kad prognoza ne radi; izvoz u Excel; stranica O prognozi s metodom i
provjerom (doprinos kiše izmjeren s kišom poznatom u trenutku izdanja).

**Hidrološka arhiva** — dnevni i satni nizovi vodostaja, protoka i oborine iz
više izvora, spojeni po točnosti; arhiva se može držati na zasebnom disku
(postavka `arhiva`).

**Dokumenti i razmjena** — PDF i Excel obrasci, PAdES potpisi, žig, slanje
e-poštom, Exchange sandučić; knjiga verzija, sinkronizacija uparenih čvorova
(TLS s ključevima čvora), potpisana `.cop` izdanja.

**Poznato** — veliki dravski val od 3. dana prognoza podcjenjuje (dotok iz
Slovenije i Austrije, prognoza kiše u Alpama); Dunav iznad Komároma oslanja se
na mađarsku prognozu dok je svježa. Ostala ograničenja su u
[administratorskim uputama](docs/INSTALACIJA.md) (odjeljak 4) i na stranici
O prognozi.
