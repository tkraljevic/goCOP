# Popis izmjena

Verzije prate [shemu iz administratorskih uputa](docs/INSTALACIJA.md#8-verzije): alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

## 0.0.47-alfa — u pripremi

- **Održavanje baze: brisanje neispravnih zapisa iz knjige.** Kad u knjizi
  ima zapisa koje razmjena danas ne bi primila, npr. s vremenom iz
  budućnosti, stranica to kaže i nudi njihovo brisanje. Briše samo globalni
  administrator, s ovog računala ili iz lokalne mreže; dnevnik programa
  zapiše tko je to napravio i odakle, a zapisi se zatim obnove iz knjige.
- **Promjene prava s drugog računala čekaju potvrdu.** Promjena zastavice,
  lozinke ili uključenosti globalnog administratora i dužnosti uprave
  organizacije vrijedi na ovom računalu tek kad je ovdje potvrdi globalni
  administrator, na stranici Stanje razmjene (Odluke (30. 10. 2026.)).
- **Primanje verzija: oznaka u sadržaju mora biti oznaka zapisa.** Zapis
  primljen s drugog računala upisuje se samo kad oznaka u njegovu sadržaju
  odgovara oznaci zapisa. Verzija s drugom oznakom ostaje zapisana i dalje
  se prenosi, ali ne mijenja podatke na ovom računalu, a u dnevniku
  programa ostaje bilješka o tome. Primljeno brisanje područja, županije,
  općine ili naselja provodi se samo kad je oznaka zapisa običan broj, bez
  vodećih nula i predznaka.
- **Nadogradnja: samo novije izdanje.** Ikona u traci nudi i postavlja
  samo izdanje novije od postavljenog, na svim sustavima. Na Linuxu
  preuzeti program mora javiti baš to izdanje, a instalacijska skripta
  pokreće se iz kopije u mapi koju piše samo root, nakon još jedne provjere
  SHA-256. Nadogradnju s 0.0.46-alfa najbolje je napraviti ručno.
- **Početne lozinke: svaki račun iz imenika dobije svoju.** Zajedničke
  početne lozinke više nema. Pri prvom punjenju iz imenika svaki račun
  dobije svoju privremenu lozinku; popis se jednom preuzme na tom računalu
  (stranica Postavljanje › privremene lozinke) i preda ljudima, a lozinka se
  mijenja pri prvoj prijavi. Popis koji nitko nije preuzeo sljedeće
  pokretanje složi iznova. U bazi je samo sažetak (Odluke (30. 10. 2026.)).
- **Provjere prava: sredstva za obranu.** Skladište i nacrt inventure
  provjeravaju se prema onome što je već upisano, a s terena se vraća i
  troši samo ono što je onamo izdao sektor skladišta.
- **Provjere prava: fotografija prijave i ovjera vlastitog lista.** Uz svoj
  nacrt prijave vodočuvar miče samo fotografiju koja je na tom nacrtu. Tko je
  uz vodočuvara i rukovoditelj ili zamjenik, svoj list ne ovjerava i svoju
  prijavu ne označava riješenom; to radi netko drugi.
- **Provjere prava: dnevnik COP-a i obračun sati.** Izvođač više ne otvara
  dnevnik COP-a ni plan dežurstava i ne piše u njih; svoje izvješće o
  satima vidi na profilu. Obračun sati vidi uprava centra, a izvješće o
  satima još i osoba sama. Storno i stanje naloga mijenjaju samo upis iz
  dnevnika koji je otvoren. Ovjeren dnevnik COP-a više ne prima zapise,
  storno, ispravke ni promjenu naloga, a u dnevniku COP-a ne otvaraju se
  listovi.
- **Provjere prava: potpisi, akti i lozinke e-pošte.** Potpis e-pošte
  zadržava samo dopušteno oblikovanje, a sken potpisa ide samo u izvornik.
  Sken ovjere učitava samo onaj tko akt smije ovjeriti, tuđim očima se akt
  ne ovjerava, a nacrt drugog sektora se ne otvara.
  Lozinka e-pošte ide samo poslužitelju za koji je upisana.
- **Provjere prava: mreža izvana i uparivanje novog računala.** Izvana se
  računalo prima u mrežu, ovlast za primanje daje i oduzima, a članstvo
  opoziva samo uz lozinku računa. Dnevnik programa zapiše tko je to
  napravio i s koje adrese. Iz lokalne mreže lozinka ne treba. Uparivanje
  bez prijave iz lokalne mreže traži kod iz dnevnika, i bez Postave.
  Tuđim očima se mreža ne mijenja, a ovo računalo ne zaboravlja samo sebe.
- **Provjere prava: paketi, uvoz i postavke ovog računala.** Paket letve
  učitan s diska ugrađuje se samo potpisan, ključem ovog računala ili člana
  mreže, a pregled kaže tko ga je potpisao. Staru .db arhivu, izvore arhive
  i „Čuvaj povijest” globalni administrator mijenja samo s ovog računala ili
  iz lokalne mreže (Odluke (30. 10. 2026.)). Isto vrijedi za paket s diska
  dok računalo još nije u mreži, jer tada ne zna čiji je potpis. Gumb Makni prije micanja niza ili
  snimke korita još jednom provjeri da im datoteke i dalje nema, a s
  oglasne ploče briše se samo zapis koji se na njoj prikazuje.
- **Provjere prava: dnevnik usluge i plan dežurstava.** U dnevniku usluge
  nalog i ocjenu piše, nalog otkazuje i list za nadzor potvrđuje ovlaštenik
  za praćenje ugovora. Naslovnicu mijenjaju on i uprava, a dnevnik briše
  uprava sektora. Upis na listu koji su potvrdile obje strane više se ne
  stornira. Ovjera dnevnika COP-a traži potvrđen plan i zaključava ga;
  poslije nje plan ispravlja samo uprava centra, uz razlog. U plan ulazi
  osoba s dužnošću u sektoru centra, a iz drugog sektora uz privremenu
  dužnost u njemu (Odluke (30. 10. 2026.)). Poslovođa izvođača ne ulazi u
  plan, vrsta dnevnika usluge ne mijenja se nakon otvaranja, a svaka strana
  list potvrđuje jednom.
- **Provjere prava: izvješća dionica, dnevni listovi, ugovori i moduli.**
  Dnevno izvješće dionice pišu i predaju samo rukovoditelj dionice i
  zamjenik; uprava ga čita i briše. Dnevni list ovjerava rukovoditelj
  branjenog područja ili zamjenik, i onaj tko je zadao zadatak na listu;
  uprava sektora i glavni rukovoditelj ovjeravaju umjesto njih uz potvrdu.
  Svoj list ne ovjerava nitko. Novu vodu iz ugovora o održavanju u registar
  dodaje uprava branjenog područja, a ugovor drugog područja se ne upisuje.
  Akti, događanja i vodočuvarski dnevnik traže Dokumentaciju; vodočuvar svoj
  dnevnik vodi i bez nje. Izvođač na zidu ne vidi dnevnik COP-a ni
  dežurstva.
- **Opoziv člana oduzima i ovlast za primanje.** Kad računalo s ključem
  mreže opozove člana, uvijek mu opozove i ovlast za primanje novih
  računala, svaku izdanu do tog trenutka, i onu za koju to računalo još ne
  zna, i kad je članu članstvo već isteklo. Program opoziv provjerava i u
  knjizi primljenih zapisa, ne samo u popisu opoziva. U popis opoziva
  ulazi samo opoziv čija oznaka odgovara sadržaju i potpisu.
- **Razgovor razmjene: ograde onoga što stiže s drugog računala.** Svaka
  vrsta poruke ima svoju najveću veličinu, a verzija koja se ne bi dala
  poslati dalje ne prima se. Od onoga što drugo računalo javi pamti se
  najviše 32 oznake mogućnosti, kratak razlog odbijanja, kratko izdanje
  programa i kratko ime računala. Sadržaj se prima samo kad ga je ovo
  računalo tražilo.
- **Vrata razmjene i tunel: mjesta po adresi i rok rukovanja.** Na portu
  razmjene i kroz tunel veze se ograđuju po adresi, a veza koja ne započne
  rukovanje u 2 s (kroz tunel 3 s) zatvara se.
- **Provjere pri uvozu: paketi, vodomjerenja i godišnjaci.** Paket arhive i
  kazalo primaju samo letvu čiji naziv vrijedi i kao naziv mape, najviše
  64 bajta. Izdavanje piše pakete samo u mapu paketa i preskače letvu koja se
  od druge razlikuje samo po velikim i malim slovima. Paket kanala (.cop)
  prvo provjeri potpis i izdavača, svaki dio čita do njegove granice, zapise
  i popis sadržaja samo do broja iz manifesta, a sadržaj uzima samo kad ga
  navodi zapis iz paketa. Tablica vodomjerenja HIS-2000 s redom ili stupcem
  izvan radne tablice ili s previše ćelija javlja grešku. Čitanje godišnjaka
  iz PDF-a staje nakon dvije minute ili kad se pregled zatvori. Kućni server
  nadogradite prvi.
- **Javni izvori vodostaja čitaju se i otvaraju samo na poslužitelju izvora.**
  Odgovori vanjskih usluga čitaju se do granice, certifikat mletva.voda.hr
  provjerava se po otisku, a poruka o prijavi na HydroView navodi samo put.
- **Ugradnja .cop paketa: samo iz iste mreže.** Računalo ugrađuje paket
  podataka samo kad ga je izdalo računalo iste mreže. Računalo koje još
  nije u mreži ugrađuje samo vlastite pakete.
- **Administracija je složena u skupine, s Prvim koracima za novi čvor.**
  Dvadeset kartica stajalo je jedna do druge, bez reda, a nijedna nije
  govorila vrijedi li postavka samo za ovo računalo ili za cijelu mrežu.
  Sad su u šest skupina, s Testnim prekidačima na dnu, a uz karticu piše
  gdje vrijedi. Dok ustroj nije gotov, administrator iznad skupina vidi
  Prve korake s kvačicama. Stranica Čvor, mreža i sinkronizacija sad se
  zove „Ovo računalo i mreža” i ima svoj odjeljak za WEB sync, a
  Sinkronizacija se zove „Stanje razmjene”. Ručna razmjena, pretraga
  lokalne mreže i provjera veze su samo ondje (Odluke (30. 10. 2026.)).
- **Žig centra je na stranici sektora, a Djelatnici imaju svoju stavku u
  izborniku.** Uprava sektora nije mogla promijeniti svoj žig, jer je bio u
  Administraciji, koju zadano ne vidi. Sad je na stranici sektora, pod
  Registri › Administrativna organizacija; kartica u Administraciji i stare
  adrese vode onamo. Djelatnici su u glavnom izborniku, a ne više u
  padajućem izborniku Registri. Rok čuvanja fotografija s terena je na
  Održavanju baze, u dijelu „Pohrana” (Odluke (30. 10. 2026.)). Adresa za
  QR kod na dokumentima zasad je na stranici Elektronički potpisi, a adresa
  u Domenskim čvorovima zove se „adresa za razmjenu”.

- **Dnevna prognoza više ne izlazi bez kiše jednom dnevno.** U prvom izdanju
  novog dana po UTC-u, zimi u 0:45, a ljeti u 1:45, dnevni model računao je
  bez kiše za sve letve, uz upozorenje da oborine nisu preuzete za svih
  dana. Prognoza kiše preuzeta je još prethodnog dana, pa je zadnjem danu
  koji model gleda falio zadnji sat. Sad se s Open-Meteo preuzima dan više.
  (#202)
- **U dnevniku programa vidi se zašto sat nije izdan.** Kad privremena
  očitanja Hrvatskih voda za idući sat u :45 još nisu stigla, prognoza tog
  sata ne izađe, a u dnevniku programa o tome nije bilo ni retka. Sad piše
  za koji je sat zadnja prognoza izdana i da za sljedeći još nema novih
  očitanja. (#202)
- **Prava u administraciji prema Odlukama (30. 10. 2026.).** Tko smije
  uređivati letvu, sad joj i unosi podatke u arhivu, gumbom „Unos u arhivu”
  na kartici letve. Dosad je do te stranice vodila samo Administracija.
  Ulaganje, izdanja i brisanje nizova ostaju globalnom administratoru, a
  drugi ih više ne vide. Stranicu „Što ovo računalo prati” mijenja samo
  globalni administrator, s tog računala ili iz lokalne mreže, a ostali je
  samo gledaju. Prognozu rukom pokreću dežurni operater, voditelj centra,
  uprava sektora i globalni administrator.

  Lozinka svugdje mora imati najmanje 10 znakova. Dosad je na profilu bilo
  dovoljno 6, a za novi račun nije bilo granice. Uprava branjenog područja
  otvara račune u svom području, ali daje samo uloge niže od svoje, npr.
  vodočuvara ili rukovoditelja dionice. Dosad je mogla postaviti i novog
  rukovoditelja ili zamjenika područja; to sad rade uprava sektora i
  globalni administrator. Takvu dužnost uprava područja i dalje može
  izmijeniti, ali je ne može produljiti ni učiniti stalnom. To pravilo
  vrijedi kad se nadograde sva računala: računalo na 0.0.46 i dalje pušta
  upravu područja da postavi upravu područja, a razmjena tu dužnost prenese
  i na ostala. U obrascu novog djelatnika i novog zaduženja uloga više nije
  unaprijed odabrana (bio je dežurni operater). Naziv sektora na popisu
  organizacije vodi na stranicu sektora, a ne više na Dionice; do njih vodi
  gumb „Dionice sektora”. Isključivanje uloge računala traži potvrdu, a na
  Sinkronizaciji uz svako računalo piše koje uloge ima.

- **Karta bez interneta stiže razmjenom.** Kartu bez interneta trebalo je
  paketom `.cop` prenijeti na svako računalo. Sad ju računalo koje je nema
  dobije razmjenom, samo od sebe, dio po dio, i od računala koje je još
  preuzima. Kad se veza prekine, nastavlja od dijela na kojem je stala.
  Koliko je stiglo, piše u Administraciji › Karte. Dok je na računalu
  proglašen stadij obrane, karta stiže i šalje se samo s računalima u istoj
  lokalnoj mreži, ista kuća ili ured, a unutarnja mreža Hrvatskih voda među
  gradovima i VPN tada se računaju kao internet (Odluke (29. 10. 2026.) i
  (30. 10. 2026.)). Računalu s 0.0.46-alfa ili starijim kartu i dalje treba
  prenijeti paketom `.cop`. (#197)
- **Provjere prava: uvoz u arhivu piše samo u mapu letve.** Sliv mora biti
  obična mapa u stablu, a letva koja u stablu već stoji upisuje se u svoj
  sliv. Sliv, letva i izvor smiju imati samo slova, brojke, crticu i točku.
  Svi ZIP-ovi jednog uvoza zajedno smiju imati najviše 1000 datoteka i
  256 MB kad se raspakiraju.

- **Provjere prava: vodotok i šifra letve, novo očitanje.** Vodotok letve
  mijenja samo tko smije uređivati letvu, izmjena letve ne mijenja šifru, a
  novo očitanje identifikator dobije od programa.
- **Provjere datoteka: prevelika tablica ili slika odbija se porukom.**
  Excel izvan granica Excela, s previše ćelija ili s previše teksta više
  se ne čita. Isto vrijedi za fotografiju, žig, potpis i sken kojem bi za
  čitanje trebalo previše memorije, a vodočuvar vidi koja fotografija
  prijave nije priložena.
- **Provjere prava: uparivanje bez prijave, moduli i brojanje krivih
  prijava.** Na svježem računalu bez prijave računala se samo upare, a u
  mrežu drugo računalo prima tek prijavljeni globalni administrator.
  Računalo koje drži ključ mreže ili ovlast za primanje uparivanje bez
  prijave više ne nudi. Prognoze, izvješća, sredstva za obranu, popis
  firmi u CSV-u i ucrtavanje objekta sad traže svoj modul i kad se otvore
  izravnom adresom, a brojke podataka s početne stranice vidi samo
  prijavljeni. Zid događanja, njegov izvoz i prečaci na početnoj stranici
  pokazuju samo module koje račun vidi. Krive prijave s adresom koju javlja posrednik broje se
  odvojeno od izravnih, a prijava s javne adrese bez tunela broji se kao
  prijava kroz tunel.

- **Provjere prava: stalna uprava organizacije i registri.** Privremena
  uprava organizacije stalnoj više ne daje lozinku ni kod za prijavu
  izvana, ne uključuje ni ne isključuje joj račun i ne mijenja ni ne
  opoziva joj dužnost razine 1. To i dalje radi stalna uprava
  organizacije. Tuđim očima se lozinka, uključenost računa, zastavica
  globalnog administratora i dužnosti uprave ne mijenjaju, ni uz uključene
  upise tuđim očima. Objekt u registru uvijek dobiva
  sektor svog branjenog područja. Uprava sad može maknuti svoje mjesto rada
  s firme koja radi i drugdje. Obavijesti o djelatnicima više ne nose ime
  osobe, a granice županija i općina s kontaktima čuva samo preglednik.

- **Prijem verzija: oznaka verzije mora biti ispravnog oblika.** Razmjena,
  arhiva i COP paket primaju samo oznaku u obliku koji program sam daje, s
  vremenom najviše pet minuta ispred sata, a granica razmjene broji samo
  takve. Ostale se preskoče, a razmjena ide dalje. Stanje razmjene javi
  kad sat drugog računala odstupa.
- **Prijava i sesije.** Promijenjena ili poništena lozinka sad zatvara
  prijave te osobe i na drugim računalima, kad promjena stigne razmjenom,
  i ondje prijave koje čekaju PIN. Nakon nadogradnje svi se jednom ponovno
  prijave. Granica krivih lozinki i PIN-ova vrijedi i kad mnogo pokušaja
  stigne u istom trenutku, a krivi upisi lozinke u jednoj prijavi ne
  zaključavaju promjenu lozinke u drugoj. Otvorena stranica prestaje
  primati novosti kad prijava prestane. Prijava na ime koje ne vodi na ovo
  računalo vrijedi kao prijava izvana; na adresu, ime računala u mreži,
  localhost, upisanu javnu adresu i imena iz `[web] lokalna_imena` sve radi
  kao prije. Pod Postavom početna lozinka vrijedi samo na
  `http://localhost`.

## 0.0.46-alfa — 29. 10. 2026.

- **Karta radi i bez interneta.** Obična karta i satelitska snimka čitaju
  se s interneta, pa bez njega karte u programu ostaju prazne. Sad čvor može
  držati vektorsku kartu OpenStreetMapa na svom disku. Na karti se bira kao
  podloga „Karta bez interneta”, s nazivima na hrvatskom, svijetla ili tamna
  po temi. Globalni administrator je učitava u Administraciji › Karte:
  izvezenu `.pmtiles` na jednom računalu, a na ostalima njezin paket `.cop`,
  koji se preuzme na istoj stranici. Paket se prima samo od računala iste
  mreže. Razmjenom ide samo koja je karta zadana. Samu datoteku, za Sektor B
  oko 120 MB, treba paketom prenijeti na svako računalo. Karta se daje samo
  prijavljenima. (#172, #192)
- **Tok vodotoka crta se istim alatom kao objekt.** Karta vodotoka imala je
  stari uređivač, a tok koji još nije bio ucrtan nije se mogao nacrtati, pa
  se karta nije ni pokazivala. Sad ima isti alat kao karta objekta, s
  učitavanjem iz GeoJSON-a, a globalni administrator kartu dobije i kad toka
  još nema. Kad se tok crta na karti, spremaju se samo crte, a točke
  stacionaže, npr. rkm na Dunavu i Dravi, ostaju kakve su bile. Učitani
  GeoJSON i dalje zamijeni cijeli tok. Tok i dalje mijenja samo globalni
  administrator. Uz to naslov stranice objekta nosi ikonu svoje vrste iz
  Administracije › Karte, a ne isti znak građevine za sve vrste. (#180,
  #187)
- **Ugroženo područje cijele poddionice može vrijediti i uz nasip.** Na
  poddionici s više nasipa i istim ugroženim područjem naselja su se morala
  upisati uz svaki nasip posebno. Inače su stajala u retku „Bez nasipa”, a uz
  nasipe je pisalo 0. Sad uz svaki nasip u obrascu dionice stoji kvačica
  „Vrijedi i ugroženo područje cijele poddionice”. Nasip s kvačicom pokazuje
  svoja naselja i ona upisana za cijelu poddionicu, a nasip bez kvačice samo
  svoja. Naselja koja nisu uz nijedan nasip stajala su iznad tablice, a redak
  „Bez nasipa” pokazivao je nula ugroženih; sad su u tom retku. Izvoz dionice
  u Excel ide istim pravilom. Kvačice uključiti kad su sva računala na
  0.0.46: stariji program kvačicu briše kad sam spremi dionicu, a naselja
  ostaju. (#174, #188)
- **Objekt bez nasipa stoji uz nasipe svoje obale.** Kad je poddionica imala
  nasip uz lijevu i nasip uz desnu obalu s istim rasponom, objekti bez
  upisanog nasipa stajali su samo uz prvi nasip, a obala objekta se nije
  gledala. Sad objekt bez nasipa stoji uz nasip svoje obale čiji raspon
  pokriva njegovu stacionažu, a od više takvih uz onaj s najužim rasponom.
  Kad ih je više s jednakim rasponom, stoji uz sve. Objekt lijeve obale ide
  samo uz nasip lijeve obale, desne uz desni, a objekt bez obale ili s
  obalom LD uz najuži nasip svake obale, i kad rasponi nisu jednaki: most
  Petrova Slatina na B.15.2 stoji uz nasip l.o. i uz nasip d.o. Vuke. Obala nasipa čita se iz naziva („l.o.”, „d.o.”,
  „Lijevi…”, „Desni…”), a nasip bez obale u nazivu prima sve. Objekt čiju
  stacionažu ne pokriva nijedan nasip njegove obale ide u redak „Bez
  nasipa”. Objekt s upisanim nasipom ostaje na svom, a izvoz u Excel ide
  istim pravilom. Nasip uz kanal upisuje se kao dva nasipa, l.o. i d.o.,
  kad tako stoji u planu i GIS-u. Most, propust ili ušće, koji prelaze
  vodotok, upisuju se jednom, bez nasipa, s obalom LD (Odluke (29. 10.
  2026.)). (#190, #193)
- **Stranica nasipa i brane pokazuje mjerodavne vodomjere.** Većina nasipa
  nema svoju letvu, pa je u bloku „Vodomjer na objektu” pisalo „Objekt nema
  pridružen vodomjer”, iako svaka poddionica u planu obrane ima mjerodavni
  vodomjer. Sad nasip i brana imaju blok „Mjerodavni vodomjeri”, složen iz
  poddionica na kojima objekt stoji. Uz svaku letvu stoje voda, stacionaža,
  pragovi sa same letve, kao na stranici dionice, i oznake dionica s
  poveznicom. Ista letva na više poddionica pokaže se jednom. Nasip s
  vlastitom letvom zadržava i blok „Vodomjer na objektu”. (#189)
- **Naselju se može promijeniti broj stanovnika.** Polje je bilo samo u
  obrascu za novo naselje, pa se upisanom naselju broj nije dao ni upisati
  ni promijeniti. Sad je u retku naselja na stranici općine, uz naziv i
  poštanski broj. (#173)
- **Račun telemetrije mijenja samo globalni administrator.** Stranica
  Administracija › Telemetrija tražila je samo modul Administracija, pa je
  svatko s tim modulom mogao promijeniti ili obrisati račun kojim letve bez
  javne stranice čitaju vodostaje. Sad su stranica i spremanje samo za
  globalnog administratora, kao Izvori, Karte i Tema. (#194)
- **Testovi i sređivanje koda, bez promjene ponašanja.** Najsloženiji
  dijelovi programa, npr. potpis i pečat na PDF-u ili crtanje uzdužnog
  profila, dobili su testove i rastavljeni su na manje korake. Program radi
  kao i prije. (#175–#179, #181–#186)

## 0.0.45-alfa — 28. 10. 2026.

- **Nasipi i drugi objekti ucrtavaju se na kartu.** Objekt je dosad imao
  samo jednu točku, pa se nasip nije mogao ucrtati. Sad nosi crtu cijelog
  nasipa s točkama stacionaže, a nasip koji dijele dvije dionice ostaje
  jedan objekt. Crta se učitava iz GeoJSON-a (npr. iz QGIS-a) ili crta
  ravno na karti objekta: gumbom „Uredi na karti” dodaju se, pomiču i brišu
  točke, crte i poligoni, a nova točka na nasipu pita za km. (#164, #169)
- **Nasipi, crpne stanice, ustave i brane imaju svoje registre.** Svi su
  objekti bili na jednom popisu. Sad svaki registar ima svoju stavku u
  izborniku, svoju pretragu i adresu (npr. `/nasipi/…`); stare poveznice
  vode na novu adresu. Sifoni, pregrade i dodane vrste čep, propust i prag
  su u Ostalim objektima, pa se retci dionica imaju na što vezati. (#165,
  #167, #168)
- **Dionice idu redom brojeva.** B.34.2 je stajala iza B.34.19, jer su se
  šifre slagale kao tekst. Sad se uspoređuju po brojevima. (#166)
- **Na karti se može upaliti satelitska snimka.** Uz običnu kartu, na svim
  kartama u programu, gumbom sa slojevima gore desno. U tamnoj temi snimka
  ostaje u pravim bojama. Nasipi su narančasti, a od većeg povećanja naziv
  nasipa piše uz crtu. (#169)
- **Izgled karata bira se u Administraciji › Karte.** Za svaku vrstu
  objekta i za vodomjernu letvu bira se boja, debljina crte i ikona točke,
  a podloge se dodaju i uklanjaju, s adresom i ključem. Vrijedi na svim
  čvorovima. Točke su ikone bez kruga, a crpna stanica, sifon, čep, propust,
  prag i pregrada imaju nove ikone. Učitati se mogu i vlastite SVG ikone.
  Obična karta ovog čvora prešla je s Opcija na istu stranicu. (#170)
- **Prijava ima novi izgled.** Stranica prijave prva je prenesena iz novog
  sustava dizajna, za mobitel, tablet i računalo, u obje teme. (#163)

## 0.0.44-alfa — 26. 10. 2026.

- **WEB sync: računala koja se nikad nisu izravno razgovarala sad se
  spoje.** Uredsko i kućno računalo imali su uključen WEB sync, ali se nisu
  ni pokušali spojiti. WEB sync se pripremao samo prema računalu koje je u
  izravnoj razmjeni javilo da ga ima, a ta dva nikad nisu bila na istoj
  mreži. Sad se WEB sync pokuša i kad se ne zna ima li ga drugo računalo.
  Ako ga nema, sastajalište to odmah javi i nakon tri pokušaja par čeka 30
  minuta. Računalo koje je javilo da WEB sync nema i dalje se ne zove.
  (#158)
- **Uzdužni profil više ne nestaje do sutra.** Na kućnom serveru je nakon
  nadogradnje cijeli dan pisalo „Uzdužnog profila nema”, iako su letve imale
  i očitanja i arhivu. Profil vodostaje zadnjih 30 dana čita jednom na dan i
  pamti ih; kad to čitanje nije uspjelo, zapamtio je prazno do sutra. Sad se
  neuspjelo čitanje ne pamti, pa iduće otvaranje stranice čita iznova. Uz to
  se Excel šalje tek kad je cijeli složen, pa prekid veze usred preuzimanja
  više ne ostavlja grešku u dnevniku poslužitelja. (#157)
- **`gocop` iz instalacije Postave bez `-db` više ne pravi novi čvor.**
  Pokrenut ručno iz mape `program`, koja je u PATH-u, program je napravio
  novu bazu u `program\data`, s novim ključem i imenom, i u mreži bi se
  javio kao novo računalo. Sad bez `-db` ne kreće, nego kaže gdje su podaci
  čvora i kako ga ručno pokrenuti. Isto vrijedi za oporavak lozinke s
  konzole. (#159)
- **Kad srpski zavod nema podataka, dnevnik to i kaže.** Za Novi Sad je
  svaki sat pisalo „na stranici nema tablice očitanja — je li se stranica
  promijenila?”, a na stranici RHMZ-a stajalo je „Podaci nisu raspoloživi”.
  Sad poruka kaže da RHMZ trenutno nema podataka za postaju. (#160)

## 0.0.43-alfa — 25. 10. 2026.

- **WEB sync: dva računala iza usmjerivača razmjenjuju izravno.** Do sada
  su laptop kod kuće i računalo u uredu podatke izmjenjivali samo preko
  stalnog čvora, do 10 minuta kasnije. Sad se, kad je WEB sync uključen na
  oba, nađu preko sastajališta, probiju kućni i uredski usmjerivač i
  razmjenjuju izravno. Razmjena ide redom: najprije lokalna mreža, zatim
  živa WEB sync veza, a ako nje nema, zajedno Tailscale, upisane javne
  adrese i tunel do stalnog čvora; vrijedi prvi put koji se spoji, a
  spajanje traje najviše 45 s. WEB sync se priprema u pozadini i ništa ne
  zaustavlja; kad je spreman, razmjena kreće odmah. Prema računalu s javnim
  ulazom, kao što je kućni server, ne priprema se. Nakon tri neuspjela
  pokušaja par čeka 30 minuta, zatim do sat vremena, a pločica kaže zašto.
  Računalo s isključenim WEB syncom drugome odmah javi da ga nema. Jedan
  krug zove najviše 8 adresa istim putem, i *Provjeri vezu s računalom*
  isto. Pločica razmjene uz računalo piše „WEB sync” kad razmjena ide tim
  putem. WEB sync je i dalje zadano isključen; kako se uključuje, piše u
  uputama za nadogradnju. (#142, #144, #147, #148, #150, #154, #156)
- **Lažni skok s javne stranice više ne ulazi u bazu ni u prognozu.**
  Jedno krivo očitanje, stotine centimetara iznad prave razine, prošlo je
  kroz raspon letve i desecima centimetara dizalo dnevnu prognozu, sve dok
  ga netko nije ručno ispravio. Sad preuzimanje vrijednost koja od
  prethodne odskoči više od praga letve ne upisuje odmah, nego čeka iduće
  očitanje: kad se ono vrati na staru razinu, skok se odbacuje i broji na
  stranici letve kao „skok”, a kad ostane na novoj, upišu se obje.
  Očitanje nakon dužeg ispada uspoređuje se s iduća dva. Prag je po letvi
  iz arhive, najmanje 50 cm u satu, pa letve ispod elektrana dobiju veći.
  Prognoza isto pravilo primjenjuje na sva očitanja osim ručnih, uz
  napomenu uz izdanje. (#151)
- **Zaglavljen ili odskočen mjerni uređaj vidi se na letvi.** Kad
  automatsko očitanje stoji dulje nego što ta letva inače stoji (najmanje
  6 h), a susjedna letva iste rijeke se u to vrijeme pomaknula, u
  vrijednost se skočilo ili je ona daleko izvan uobičajenog raspona, na
  vrhu stranice letve je okvir „Mjerni uređaj je vjerojatno u kvaru”, a u
  popisu operativnih vodostaja znak uz letvu. Isto za uređaj koji odskoči
  i drži se. Kad se susjedi samo spuštaju, okvir kaže „Voda je vjerojatno
  ispod dosega mjernog uređaja”. Uređaj koji u tjedan dana barem tri puta
  lažno odskoči dobije okvir s popisom tih skokova. Ustave, crpne stanice,
  elektrane i akumulacije se ne provjeravaju. Očitanja se ne brišu. Kad
  kvar potvrđuje susjedna letva, prognoza ta očitanja ne uzima i računa
  kao da mjerenja nema; inače ih uzima, uz napomenu uz izdanje. (#155)
- **Dnevna prognoza Dunava vidi val izmjeren u Austriji.** Dnevni model
  Batine, Aljmaša, Vukovara i Iloka nije znao za val koji su Wildungsmauer i
  Achleiten već izmjerili, pa je 4.–6. dan podbacivao porast. Sad se
  prognoza pomiče prema promjeni na tim dvjema letvama u zadnja 24 i 48
  sati. Ne spušta je dok voda raste više od 5 cm na dan i ne diže je više od
  100 cm; kad je digne više od 50 cm ili kad neka od tih letvi ne javlja, to
  piše u napomeni uz izdanje. Isti ispravak dobivaju letve koje dnevni dio
  uzimaju od tih postaja. Satni lanac, Komárom i Budimpešta se ne mijenjaju.
  U provjeri na 2024.–2026. pogreška 4.–6. dana pala je s 17,8 na 16,0 cm.
  (#141)
- **Izmjena očitanja više ne mijenja njegov datum i vrijeme.** Kad se u
  postojećem očitanju ispravio samo vodostaj, obrazac je pri spremanju
  vrijeme postavljao na trenutak ispravka. Sad izmjena čuva izvorni
  trenutak, i sekunde i sat na dan pomaka sata; izmjena bez datuma i
  vremena se odbija. Novo očitanje i dalje dobiva trenutak spremanja. (#149)
- **Kraća i jednostavnija objašnjenja prognoze.** Natpis ispod uzdužnog
  profila i odjeljak na dnu stranice prognoze sad kažu samo kako čitati
  sliku i karticu, a pojedinosti su na stranici O prognozi. Kraće su i
  napomene u izvozu u Excel i nekoliko upozorenja uz izdanje. Napomena koja
  završava datumom više nema dvije točke. (#145, #146)
- **O prognozi: znanstveni radovi i literatura.** Pod Detaljima je novo
  poglavlje: radovi na kojima se temelji naš način prognoze, što koji
  potvrđuje i što iz njega primjenjujemo, te pristupi koje ne primjenjujemo
  i zašto. (#143)

## 0.0.42-alfa — 24. 10. 2026.

- **Srpska prognoza s krivim datumima više ne stoji pod krivim danom.**
  RHMZ Srbije je uz pomak sata 22.–24. 10. 2026. u tablici napisao jedan dan
  dvaput (subota 24., nedjelja 25., nedjelja 25., ponedjeljak 26. i utorak
  27. 10.), pa su njihove vrijednosti za 26.–28. 10. stajale dan prerano, a
  jedna je nestala. Takvu tablicu goCOP sad čita redom po danima, uz
  napomenu uz izdanje. Svaki drugi slijed datuma koji ne ide dan za danom, u
  srpskoj i mađarskoj tablici, sprema se samo do prvog neurednog datuma,
  ništa se ne premješta, a izdanje nosi upozorenje dok tablica ne bude
  ispravljena. Gdje RHMZ upiše „-”, granice obrane moravskih postaja više ne
  ulaze među dane prognoze. Pri prvom pokretanju novog izdanja program jednom
  ispravi stare srpske retke: dane uz pomak sata premjesti na pravi dan, a
  granice obrane spremljene kao prognoza obriše. (#139)
- **Pločica razmjene kaže kojim putem razmjena ide i zašto.** Uz svako
  računalo piše put zadnje razmjene, npr. „LAN sync, 3 ms” ili „SERVER sync,
  35 ms”, a kad razmjena ne ide boljim putem, rečenica kaže zašto. Dok je
  stalni čvor na mreži, uz računalo koje ne odgovara piše „SERVER sync,
  preko cop.primjer.hr: podaci stižu do 10 min kasnije.” Program pritom ne
  provjerava radi li to računalo, pa isti natpis stoji i uz ugašeno. Adrese i
  sirove greške razmjene više nisu na pločici. Vidi ih samo globalni
  administrator, na stranicama Sinkronizacija i Čvor, mreža i
  sinkronizacija; drugi s modulom Administracija više ih ne vide, a nemaju ni
  gumb *Pretraži lokalnu mrežu*. (#133, #136)
- **Članstvo u mreži i ovlast za primanje imaju rok koji se vidi i obnavlja
  samo.** Rok piše u Administraciji, na stranici Čvor, mreža i
  sinkronizacija, pod Mreža, a pločica upozorava 30 dana prije isteka.
  Članstvo vrijedi godinu dana, ovlast za primanje dvije. Računalo kojem rok
  ističe samo zatraži obnovu uz uspješnu razmjenu, a globalni administrator
  može je zatražiti i gumbom *Obnovi članstvo*. Novo članstvo izda računalo
  koje prima nova računala kad do njega stigne zahtjev, a ovlast samo
  računalo s ključem mreže. Isteklo članstvo vrijedi za razmjenu još 30 dana,
  ali u tom roku računalo ne prima druga računala; nakon toga računalo treba
  ponovno primiti u mrežu. (#130)
- **E-pošta o roku i šifrirana kopija ključa mreže.** Globalni
  administratori čija adresa smije primati PIN dobiju e-poštu kad članstvu
  ili ovlasti nekog računala ostane manje od 30 dana ili istekne, i za svaku
  spremljenu ili učitanu kopiju ključa mreže (tko, na kojem računalu, kada).
  Kopiju na računalu s ključem mreže sprema globalni administrator gumbom
  *Spremi šifriranu kopiju*, uz posebnu lozinku od najmanje 12 znakova;
  preglednik preuzme datoteku koju treba spremiti na hladni disk, a lozinku
  program ne pamti. Drugo računalo iste mreže kopiju učita gumbom *Učitaj
  kopiju ključa* i od tada i ono prima nova računala. Oboje samo s tog
  računala ili iz lokalne mreže. (#131)
- **Globalni administrator vidi što je odbijeno.** Na stranici
  Sinkronizacija vidi zadnjih 7 dana: veze koje vrata razmjene nisu pustila,
  razmjene koje je ovo računalo odbilo, odbijenice drugih računala i ono što
  ovaj program ne razumije, bez adresa i sadržaja. Svako računalo pokazuje
  samo svoj zapis. Kad poznato računalo u zadnja 24 sata zove ovo, a ovdje
  nije član, to piše na pločici, koju vide svi prijavljeni. (#129)
- **Pronalaženje u lokalnoj mreži je brže i otpornije.** Proba ide tri puta
  i završi čim se jave tražena računala, a jedno pronalaženje vrijedi za sve
  razmjene u krugu. Računalo na probe odgovara samo iz mreža na koje je
  spojeno, a kad odgovaranje stane, samo se ponovno pokrene (to piše samo u
  dnevniku). Mostovi Dockera, libvirta, Hyper-V-a i WSL-a, mostovi i VPN na
  macOS-u i Tailscale više se ne vide kao lokalna mreža; VirtualBox, VMware
  i neke VPN-ove program još ne prepoznaje. (#128)
- **Prvi dio WEB synca: provjera mreže i prijava kod sastajališta, zadano
  isključeni.** Uz `[povezivost] web_sync_prijava = true` računalo otvori UDP
  port 4713 i pita dva STUN poslužitelja (Googleov i Cloudflareov) kojom ga
  javnom adresom vide; na vatrozidu za to ne treba otvarati ništa. Stalni
  čvor uz `posluzi_sastajaliste = true` postaje sastajalište, a ostala
  računala uz `sastajaliste` i `sastajaliste_cvor` prijave se kod njega
  preko njegove web adrese i drže vezu, i nakon buđenja i promjene mreže.
  Prijava za WEB sync mora biti uključena na svima, i na sastajalištu.
  Sastajalište prima najviše 64 računala, a s iste javne adrese 8. Pločica
  kaže kod koga je računalo prijavljeno, a na stalnom čvoru koliko je
  računala prijavljeno. Kopiju istog goCOP-a na drugom računalu sastajalište
  odbija i javlja na oba. Podaci tim putem još ne idu; izravnu vezu donosi
  iduća faza. (#127, #132, #134, #136, #138)
- **Prekidač WEB synca pri padu.** Ako se program prekine dok WEB sync radi
  i idući put opet se prekine u prvih 10 minuta rada, sljedeće pokretanje
  kreće bez WEB synca, a pločica to kaže. Na stalnom čvoru tada ne radi ni
  sastajalište. Globalni administrator ga ponovno uključuje gumbom *Ponovno
  uključi WEB sync* u kartici *Ovaj čvor*, s tog računala ili iz lokalne
  mreže; WEB sync proradi kad se čvor ponovno pokrene. (#132)
- **Gumbi Provjeri vezu i Provjeri vezu s računalom.** *Provjeri vezu* u
  kartici *Ovaj čvor* izmjeri mrežu ovog računala, i kad je WEB sync
  isključen. Blok *Dohvatljivost ovog računala* na stranici Sinkronizacija
  pokazuje ishod zadnje provjere: javnu adresu, adrese koje bi WEB sync
  nudio, koje ne nudi i zašto. *Provjeri vezu s računalom* nazove svaku
  adresu koju ovo računalo zna za izabrano računalo i za svaku kaže kako je
  prošlo, bez razmjene podataka; radi samo za članove mreže. Gumbe vidi i
  pokreće samo globalni administrator, s tog računala ili iz lokalne mreže.
  (#135, #137)

## 0.0.41-alfa — 22. 10. 2026.

- **Kiša ulazi u satni lanac Dunava.** Lanac Dunava nije vidio kišu: vrh
  (Wildungsmauer) drži zadnje mjerenje, pa su Komárom, Budimpešta i Mohács
  pri porastu od kiše kasnili za Batinom, koja uz lanac ima i dnevni model.
  Sad Komárom i Budimpešta imaju dnevni model s kišom (gornji Dunav i
  Morava; Váh, Hron i Ipeľ), a lanac na njima miješa oba modela, pa kišu
  nose i sve letve nizvodno (oznaka ds). Na 2024.–2026. s pravim
  prognozama kiše Batina za 96 sati griješi 16,2 cm umjesto 18,9, a kad
  voda raste 23,3 umjesto 28,5. Kad kiša padne nakon suhih dana, prognoza
  zna biti previsoka, kao i dosad dnevni model Batine. Novi dnevni model
  program nauči sam pri prvom izračunu nakon nadogradnje (Odluke (22. 10.
  2026.)). (#125)
- **Letve između dunavskih postaja prate susjede.** Siga, Petreš,
  Bogojevo, Dalj, Sotin i Mohovo te mađarske letve od Esztergoma do
  Mohácsa nemaju dnevni model, pa na uzdužnom profilu nisu rasle sa
  susjedima. Sad dnevni dio posuđuju od susjeda, prema položaju na rijeci
  (oznaka p). Uz p raspon je procijenjen iz susjeda, a protok daje krivulja
  letve gdje je ima. (#124, #125)
- **Srpske letve s našom prognozom pokazuju isto što i letve s kojih se
  prenose.** Bezdan, Apatin i Bačka Palanka dobivaju ugađanje kao Batina i
  Ilok (oznaka ds). (#124)
- **Uzdužni profil Dunava kreće od Komároma.** Nagybajcs i Wildungsmauer
  imaju samo satni lanac, pa bi na profilu Komárom rastao sam; u tablici
  prognoza i dalje stoje. (#124, #125)
- **Stranica O prognozi i slika lanca** opisuju kišu u lancu i letve koje
  prate susjede. (#124, #125)

## 0.0.40-alfa — 22. 10. 2026.

- **Prognoza više ne ovisi ni o jednoj tuđoj prognozi.** Komárom je vodila
  mađarska prognoza, a Wildungsmauer austrijska. Austrijska je 21. 10.
  nestala preko noći, kad je Donja Austrija ugasila svoje stranice.
  Provjera je pokazala da mađarska prognoza našim letvama na Dunavu donosi
  najviše nekoliko centimetara na 96 sati, a u dnevnoj tablici ništa. Sad
  lanac računa sve sam, a mađarska, austrijska i srpska prognoza stoje uz
  našu samo radi usporedbe (Odluke (22. 10. 2026.)). Letenye budućnost
  dobiva samo iz našeg dnevnog modela, a bez njega drži zadnje mjerenje, uz
  upozorenje. (#115)
- **Na Dunavu satni lanac i dnevni model zajedno.** Na Batini, Aljmašu,
  Vukovaru i Iloku prognoza do 96. sata više ne prelazi s jednoga na drugi,
  nego ih uzima otprilike pola-pola, pri svakom vodostaju. Tako je
  izmjereno točnije, i na živim izdanjima i na valovima od 2012. U vrhu
  vala 3. dan Batina pokaže nekoliko centimetara više. Na Dravi ostaje
  prijelaz s jednoga na drugi (Odluke (22. 10. 2026.)). (#118, #119)
- **Uz svaku vrijednost prognoze stoji slovo** koje kaže iz čega je: s satni
  lanac, d dnevni model, ds oboje zajedno, p susjedi. Slovo s u drugoj boji
  znači da dnevnog modela u izdanju nema. U Excelu je slovo uz broj, a
  ćelija ostaje broj. (#118, #119)
- **Graf i tablica pokazuju isto.** Graf u Excelu držao je satni lanac do
  96. sata pa skočio na dnevni model, iako ga tablica uzima ranije, pa su se
  graf i tablica dan-dva razilazili. Sad tablica, uzdužni profil, graf letve
  i graf u Excelu čitaju isti niz: u 7 h svakog dana to je broj iz tablice,
  a između dana koje daju različiti modeli crta postupno prelazi s jednoga
  na drugi. Graf letve sad seže do kraja šestog dana. U Postavkama prognoze
  jedan je izbor modela umjesto tri. Šesti dan ima vrijednost i kad je
  izdanje bilo danju (Odluke (22. 10. 2026.)). (#117)
- **Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs prate
  susjede u danima unaprijed.** Te letve nemaju dnevni model s kišom, pa su
  im dani ostajali ravni dok su Botovo, Terezino Polje i Donji Miholjac
  rasli. Sad se njihovi dani od 2. do 6. računaju iz onoga što pokazuju
  susjedi, prema položaju na rijeci (oznaka p). Prate susjede i kad
  najavljeni porast ne dođe. U Postavkama prognoze to se može isključiti.
  (#116)
- **Na visokoj vodi dnevni model na Dravi preuzima ranije.** Botovo od 2.
  dana (prije 5.), Donji Miholjac od 3. (prije 4.), Belišće od 4. (prije
  5.). (#116)
- **HE Dubrava i HE Varaždin opet imaju model ispuštanja za veliku vodu.**
  Režim velike vode uči se samo kad u zadnje dvije godine ima barem sto sati
  velike vode za ocjenu. Te dvije elektrane imale su ih 98, pa je režim
  tiho otpao i vrh Drave je pri velikoj vodi držao zadnje istjecanje. Sad
  se ocjena tada uzima na zadnjih sto sati velike vode. Na valovima od
  2014. to lanac Botova popravlja na svim dosezima. Kad režima ipak nema,
  to piše u izvještaju „Pripremi model” i na kartici vrha (Odluke (22. 10.
  2026.)). (#122)
- **Stranica O prognozi je kraća.** Na vrhu piše kako čitati prognozu
  (vrijednost, raspon, oznake, boje faza), a odmah ispod je grafički
  prikaz lanca. Formule, provjere i povijest izmjena sklopljeni su pod
  „Detalji”, a zastarjeli odlomci o tuđim prognozama kao vrhu lanca su
  izbačeni. (#119, #121)
- **Raspon prognoze više ne pada na nulu gdje krivulja protoka prestaje.**
  Pri maloj vodi protok je skakao iz sata u sat između raspona bez širine i
  širokog raspona, a donji rub protoka znao je pasti ispod nule. (#110)
- **Vodostaj iz protoka više ne zapinje na rubu krivulje.** Na Ludbregu je
  prognoza vodostaja pri maloj vodi stajala na istom broju, a Tuhovec
  prognozu vodostaja nije imao. (#111)
- **Model prognoze uči se na istim satima na kojima radi.** Za letve koje
  se očitavaju češće od jednom na sat učenje je uzimalo zadnje očitanje u
  satu, a živa prognoza najbliže punom satu. Nakon nadogradnje treba
  jednom pokrenuti „Pripremi model”. (#112)
- **Ugašen tuđi izvor više ne izgleda kao promijenjen oblik datoteke.**
  Dnevnik sad kaže da je izvor vratio web-stranicu umjesto datoteke i
  navodi njezin naslov. (#109)
- **Vodostaji i prognoza osvježavaju se svaki sat u istoj minuti**, a ne
  od trenutka pokretanja programa. (#91)
- **Mađarska vodoprivreda kao izvor arhive.** Nizovi Mađarske
  vodoprivrede (DDVIZIG) imaju ime i stoje među ovjerenim izvorima. Uvoz
  krivulja protoka prima i datoteku krivulja u obliku arhive, s izvorom i
  napomenom iz datoteke. (#102)
- **Razmjena se priprema za vezu preko interneta (WEB sync, korak F0).** Za
  korisnika se ništa ne mijenja, a s računalima 0.0.37–0.0.39 razmjena ide
  kao i prije. Novo je ovo:
  - računala s različitim imenom protokola ne razmjenjuju podatke, pa se
    prava mreža (`gocop`) i testna simulacija (`gocop-sim`) ne mogu
    pomiješati; kopirana mapa podataka s drugim imenom ne kreće
    (Odluke (21. 10. 2026.));
  - adrese koje računalo nauči ostaju samo na njemu i ne stvaraju nove
    verzije zapisa računala;
  - tunel se spaja istim putem kao razmjena na portu, a računalo koje
    zove ne šalje svoj certifikat strani s drugim ključem.
  (#103, #104, #106, #108, #113)
- **Zahvale** sad navode Mađarsku vodoprivredu i Republički
  hidrometeorološki zavod Srbije. (#100)
- **Upute:** kratak opis razmjene među računalima; primanje na daljinu
  ostaje uključeno, a o početnoj lozinki piše točnije. Plan razvoja
  navodi pregled knjige verzija i manje mjesta na disku. (#92, #98, #114)
- Testovi su četiri puta brži, a nekoliko testova koji su padali sami od
  sebe sad prolazi stabilno. Provjera javnog teksta gleda i `docs/`.
  (#93, #94, #95, #96, #97, #101, #105, #107)

**Nakon nadogradnje** računala koje izdaje prognozu jednom pokrenuti
„Pripremi model” (#112, #122). **Prije povratka na 0.0.39-alfa** zaustaviti sve
instance `gocop-sim`, jer 0.0.39-alfa ne provjerava ime protokola.

## 0.0.39-alfa — 18. 10. 2026.

- **Član mreže više ne može podmetnuti ključ drugog računala.** Član mreže
  mogao je drugim računalima poslati zapis o trećem računalu sa svojim
  ključem i adresom. Razmjena je tada išla s njim kao da je on to treće
  računalo, i izravno i kroz tunel. Slično je bilo s članstvom: obični član
  mogao je krivotvorenim članstvom isključiti drugo računalo iz razmjene, a
  ovlašteni primatelj članstvom pod tuđim imenom preuzeti to ime. Za to je
  trebao izmijenjen program ili ručni upis u bazu. Sad program ključ
  računala uzima samo iz članstva koje vrijedi: s ispravnim potpisom, bez
  isteklog roka i bez opoziva. Kad pod istim imenom vrijede dva članstva s
  različitim ključem, vrijedi prvo izdano, a dnevnik programa javi da pod
  tim imenom valjano članstvo imaju dva računala. Ovlašteni primatelj s
  izmijenjenim programom i dalje može preuzeti ime računala koje je
  primljeno nakon što je on dobio ovlast; protiv toga pomaže samo
  oduzimanje ovlasti za primanje. Opoziv imena ukida sva članstva pod tim
  imenom. Podmetnut zapis računala još može promijeniti adrese i tako
  usporiti vezu s pravim računalom, ali razmjena više ne ide s krivim.
  Zaštićeno je samo računalo s 0.0.39-alfa.
- **Potvrđen upis ostaje i kad se računalo sruši.** Kad se računalo
  srušilo ili mu je nestalo struje, mogli su nestati zadnji upisi koje je
  program već potvrdio. Ovjeren akt koji je već poslan e-poštom ili
  ispisan vratio bi se u nacrt, a njegov broj dobio bi sljedeći ovjereni
  akt. Gradnja arhive vodostaja nije čekala disk, pa je nestanak struje
  usred gradnje mogao pokvariti cijelu arhivu. Popis izdanih paketa arhive
  mogao se vratiti na stariji, pa bi sljedeće izdanje pod istim brojem
  imalo drukčiji sadržaj, a računala koja taj broj već imaju ne bi ga
  uzela. Novi ključ računala ili mreže mogao je ostati prazna datoteka,
  iako su ga druga računala već upoznala. Sad program svaki upis u svoje
  baze potvrdi tek kad ga disk zapiše. Na Linuxu i Windowsu baza bi tako
  trebala podnijeti i nestanak struje, ali to nije ispitano. Na Macu baza
  podnosi samo pad sustava: nestanak struje ili prisilno gašenje i dalje
  mogu odnijeti zadnje upise, pa i pokvariti bazu, jer Mac pri zapisu baze
  ne prazni predmemoriju samog diska. Ključeve, pakete i popis izdanih
  paketa program sad spremi na disk prije nego ih upotrijebi. Na Windowsu
  to nije sasvim zajamčeno: nestanak struje odmah nakon izdavanja još može
  vratiti stari popis izdanih paketa.
- **Računalo koje ne čuva sva tuđa očitanja više ih ne dobiva iznova.**
  Računalo s upisanom postavkom „Povijest tuđih očitanja (mjeseci)”, npr.
  terenski laptop, starija tuđa očitanja ne čuva. Svakom razmjenom ipak ih
  je dobivalo iznova i odmah bacalo. Kad ih je od istog računala za isto
  područje i godinu bilo 5000 ili više zaredom, razmjena je ondje zapela, pa
  nije stizalo ni ono iza njih, a pločica razmjene stalno je pisala „prima
  se još”. Sad računalo pošiljatelju javi dokle je odbacilo ono što mu je
  poslao, pa mu pošiljatelj šalje samo novije. To se pamti samo dok program
  radi. Nakon ponovnog pokretanja, kad se promijeni što računalo čuva ili
  prati, ili kad pošiljatelj poslije dobije starije očitanje, npr. ugradnjom
  .cop paketa ili stare .db arhive, stara očitanja stignu još jednom. Isto
  se dogodi jednom na dan, kad zapamćeno istekne, i katkad na prijelazu
  mjeseca. „Šalje se još” na računalu pošiljatelja više ne broji ono što je
  drugo računalo javilo da je odbilo. „Prima se još” na svakom računalu
  gleda samo ono što ono prati. Od računala na 0.0.38-alfa stara očitanja i
  dalje stižu svakom razmjenom, dok se ono ne nadogradi.
- **Očitanje se više ne gubi kad putuje preko računala koje ne čuva sve.**
  Računalo s upisanom postavkom „Povijest tuđih očitanja (mjeseci)” drugima
  je predavalo niz s rupom: starija tuđa očitanja nije imalo, a novija jest.
  Računalo koje je od njega primilo takav niz mislilo je da ima sve, pa mu
  autor ono što nedostaje više nije slao, a rupa je išla dalje. Isto je bilo
  s nizom primljenim ugradnjom .cop paketa ili stare .db arhive. Sad svako
  računalo trajno pamti koja je tuđa očitanja izostavilo i što mu nedostaje,
  a uz poslane podatke javi za koji dio ne jamči. Isti zapis ide i u .cop
  paket kanala i u staru .db arhivu. Primatelj taj dio traži od računala
  koje ga ima cijelog. Kad računalo poslije zaprati letvu ili produlji
  razdoblje, zatraži i starija očitanja koja je dotad izostavljalo. Pločica
  razmjene upozorava u dva slučaja: kad je postavka upisana, a neko računalo
  u mreži ima stariji program ili s njim ovo računalo od nadogradnje još
  nije razmijenilo podatke, i kad od nekog računala dulje od dana možda
  nedostaju starija očitanja. Niz s rupom koji je neko računalo primilo
  prije nadogradnje ostaje takav.
- **Kota nule snimke korita više ne gubi decimale.** HIS-2000 kotu nule
  letve zna pisati decimalnim zarezom, npr. 85,21. Gradnja arhive čitala
  je takvu kotu kao 85 m, pa su na crtežu korita na stranici letve voda i
  crte stupnjeva obrane stajale prenisko, za decimalni dio kote, do gotovo
  jednog metra. Sad gradnja čita i decimalni zarez. Snimke koje su već u
  arhivi ispravljaju se tek kad se njihova letva ponovno izgradi, kako
  piše pod Nadogradnjom. Snimka u čijem se zaglavlju vodostaj ili kota
  nule ne da pročitati kao broj sad zaustavi gradnju, a poruka kaže koja je
  to datoteka. Dosad se tada upisivala nula, kao da je izmjerena.
- **Greška pri zapisu ili čitanju više ne prolazi tiho.** Na nekoliko mjesta
  program nije gledao je li zapis ili čitanje uspjelo, pa je radio dalje s
  nepotpunim ili krivim podacima. Uvoz hidrološkog godišnjaka RHMZ Srbije
  koji nije mogao zapisati cijeli niz, npr. jer je disk pun, javio je da je
  niz zapisan, a gradnja ga je poslije čitala kao cijeli. Sad uvoz javi
  grešku i nepotpun niz makne. Kad čitanje naziva iz registara uz dionicu
  nije uspjelo, dionica se prikazala bez njih, a brana bi u izvozu dionica
  izašla kao nasip. Sad program javi grešku. Kad se pri upisu novog niza u
  stablo starija datoteka istog niza nije dala maknuti, npr. jer je bila
  otvorena u Excelu, gradnja je obje čitala kao jedan niz. Sad program
  upozori koju datoteku treba maknuti ručno, a posao ide dalje. Dok je ne
  maknete, gradnja i dalje čita obje. Kod kišomjera, čija mjerenja program
  sam ulaže u arhivu, upozorenje je samo u dnevniku programa.
- **goCOP se skriva od tražilica.** Ništa nije priječilo tražilici da
  stranicu za prijavu uvrsti u rezultate čim negdje nađe adresu čvora. Sad
  svaki odgovor programa nosi oznaku da ga tražilica ne uvrsti, ne prati
  poveznice i ne čuva kopiju. `robots.txt` tražilici dopušta samo ulaz i
  prijavu, gdje tu oznaku vidi, a sve ostalo zabranjuje. Postavke za to
  nema, skriveno je uvijek. Prijavu to ne zamjenjuje. Razmjena kroz tunel
  radi kao i prije.
- **Upozorenje kad na disku ostane malo mjesta.** Program nije javljao kad
  se puni disk s bazom, arhivom, stablom izvornih datoteka ili paketima. Sad
  te diskove pogleda svakih deset minuta. Kad na nekom ostane manje od 10 GB
  ili manje od 10 % diska, pločica razmjene na naslovnoj kaže na kojem
  računalu, koliko je mjesta ostalo i što učiniti. Isto upozorenje stiže
  e-poštom, jednom na dan dok mjesta bude malo. Primaju ga osobe koje su na
  tom računalu spremile lozinku sandučića, svaka sa svog računa na svoju
  adresu. Kad slanje ne uspije, program ga ne ponavlja u sljedeća 24 sata,
  da poslužitelj e-pošte ne zaključa račun zbog krive lozinke. Razmjena zbog
  malo mjesta ne staje.
- **Stablo izvornih datoteka i paketi ne počinju iznova kad program radi iz
  druge mape.** Relativne putanje u postavkama `podaci` i `pakete` računaju
  se od mape iz koje je goCOP pokrenut. Pod Postavom je to mapa baze, pa
  računalo koje se prije pokretalo iz druge mape više nije vidjelo ni
  stablo ni izdane pakete. Uvoz bi tada pisao u novo, prazno stablo, a
  izdavanje bi krenulo od prvog broja. Jedini trag bila je sitna poruka na
  stranici Unos u arhivu. Sad dnevnik programa pri pokretanju upiše pune
  putanje stabla i paketa, a Unos u arhivu kaže koja se mapa traži, da je
  nema i što upisati u postavke. Upis u stablo, uvoz izvora i izdavanje
  odbiju se kad relativne mape nema, a mapa istog imena stoji jednu razinu
  iznad mape iz koje je goCOP pokrenut. U svim ostalim slučajevima mapa
  nastaje kao i prije.
- **Kriva očitanja s popisa kvarova ne vraćaju se novim uvozom.** Kriva
  očitanja telemetrije, npr. kad mjerilo zapne ili nasjedne pri maloj vodi,
  dosad su se ručno selila u zasebne datoteke s nastavkom `-kvar`. Svaki novi
  uvoz telemetrije napiše izvornu datoteku iznova, pa bi se kvar tiho vratio
  u spojeni niz i u prognozu. Sad gradnja arhive čita popis kvarova, datoteku
  `kvarovi.csv` u stablu izvornih datoteka: letva, izvor, veličina, od i do
  te po želji raspon vrijednosti. Takve retke gradnja sama odvoji u
  isključeni niz s nastavkom `-kvar`, a ispis gradnje kaže koliko ih je
  odvojeno. Kad se redak makne s popisa, očitanja se vraćaju u svoj niz.
  Redak popisa koji se ne da pročitati zaustavi gradnju, a poruka kaže koji
  je to redak.
- **Jedan neispravan zapis više ne odbija sve što je stiglo.** Kad je među
  primljenim zapisima bio jedan bez oznake, vrste ili identifikatora,
  računalo je odbilo sve što je u toj razmjeni stiglo, i tako svakom
  razmjenom. Ugradnja .cop paketa ili stare .db arhive s takvim zapisom
  pala je. Sad se takav zapis preskoči i upiše u dnevnik programa, a ostali
  se upišu. Računalo s 0.0.39-alfa takav zapis ni ne šalje.
- **Računala na početku razmjene javljaju što razumiju.** Zato nove poruke
  iz ovog izdanja idu samo između računala koja ih oba razumiju, a s
  0.0.38-alfa razmjena ide kao i prije.
- **Pločica razmjene kaže koliko zapisa čeka nadogradnju.** Kad stignu
  zapisi koje ovaj program još ne razumije, pločica razmjene pisala je imena
  tablica i broj verzija, a o drugim računalima ništa. Sad kaže koliko je
  zapisa stiglo, npr. „Stiglo je 120 zapisa koje ovaj program još ne
  razumije. Primijenit će ih nakon nadogradnje goCOP-a.” Kaže i koliko ih
  čuva svako računalo s kojim ovo izravno razmjenjuje podatke, npr. „Windows
  čuva 120 zapisa koje će primijeniti nakon nadogradnje.” Kad taj broj nije
  javljen danas, redak kaže kad je javljen. Računalo na 0.0.38-alfa broj ne
  javlja, pa uz njega toga retka nema. Između 0.0.38-alfa i 0.0.39-alfa
  takvih zapisa nema, pa se ti redci prvi put mogu pojaviti uz sljedeće
  izdanje. Upozorenja, poruke i Pomoć sad govore o nadogradnji, a ne o
  ažuriranju.
- **Napomena uz izvor RHMZ-a navodi i Bačku Palanku.** Napomena uz izvor
  `rhmz` govorila je o Bezdanu, Apatinu i Bogojevu do 2022. Sad navodi i
  Bačku Palanku i godine do 2025. Brojke u arhivi se ne mijenjaju. Računalo
  koje arhivu već ima zadržava staru napomenu. Novu može upisati globalni
  administrator u polje Napomena na stranici Izvori arhive.
- **Upute su sređene.** README je kraći. Upute u mapi `docs` imaju kazalo
  i četiri dijela: o aplikaciji, instalacija, plan razvoja i kvaliteta
  koda. Nove su upute za prvo pokretanje i opis uloga u programu.
- **Nadogradnja.** 0.0.39-alfa radi uz 0.0.38-alfa: nove poruke razmjene
  idu samo između računala koja ih oba razumiju, a 0.0.38-alfa nova polja
  zanemari, pa je razmjena s njom kakva je i bila. Ispravak ključa iz prve
  stavke štiti samo nadograđeno računalo, pa stalna računala nadogradite
  odmah, a ostala čim mogu. Dok u mreži ima računala na 0.0.38-alfa,
  nijedno računalo ne smije imati upisanu postavku „Povijest tuđih očitanja
  (mjeseci)” (`[vodostaji] povijest_mjeseci` u `gocop.toml`), ni novi
  terenski laptop, iako mu program uz polje nudi 12 mjeseci. Stari program
  ne zna koja su očitanja izostavljena, pa bi se mogla izgubiti. Postavku
  isključuje globalni administrator upisom 0 na stranici „Što ovo računalo
  prati”, s tog računala ili iz lokalne mreže, i zatim ponovno pokrene
  program; do tada postavka i dalje vrijedi. Računalo koje tu postavku već
  ima ne isključujte prije nadogradnje: nadogradite ga nakon stalnih
  računala, s još upisanom postavkom, i pokrenite. Zatim upišite 0 i
  ponovno ga pokrenite, pa od stalnih računala zatraži starija očitanja koja
  je dotad izostavljalo. Isključi li se postavka još na 0.0.38-alfa, tom
  računalu ta očitanja trajno nedostaju, a da to nigdje ne piše. Računalu
  na 0.0.38-alfa ne šaljite staru .db arhivu, nego .cop paket: arhivu
  uveze, ali ne vidi što u njoj nedostaje. Na računalu koje gradi arhivu
  ili izdaje pakete nakon nadogradnje pogledajte u dnevniku programa retke
  „Stablo arhive” i „Mapa paketa”. Snimke korita s kotom nule pisanom
  zarezom ostaju u arhivi krive dok se njihova letva ponovno ne izgradi.
  Na računalu koje gradi arhivu zato nakon nadogradnje ponovno izgradite
  takve letve: letvu izgradi svaki upis za nju pod Unos u arhivu, npr.
  ponovni uvoz njezinih snimki korita. Zatim im izdajte nove pakete, da
  ispravak stigne i na druga računala. Kako se takve snimke prepoznaju i
  kako se vratiti na 0.0.38-alfa, piše u administratorskim uputama, u
  odjeljku o nadogradnji.

## 0.0.38-alfa — 14. 10. 2026.

- **Brisanje s računala ne dira ono što je nastalo na njemu.** Na
  stranici „Što ovo računalo prati” gumbi „Obriši s računala” i „Makni i
  obriši” brisali su sve iz te vrste, područja i godine, i zapise, PDF-ove
  i fotografije nastale na tom računalu. Laptop bez veze tako je mogao
  izgubiti list koji još nije stigao u ured. Sad se briše samo ono što je
  stiglo s drugih računala. „Makni” uz pravilo samo makne pravilo, a
  „Makni i obriši” više nema. Uz „Obriši s računala” piše koliko će se
  zapisa i dokumenata obrisati i koliko ih ostaje jer su nastali na tom
  računalu. Iste brojeve kaže potvrda prije brisanja, a poruka nakon
  brisanja kaže što je obrisano i što je ostalo. Ako je ured poslije
  mijenjao zapis nastao na tom računalu, brisanje ga vrati na stanje kakvo
  je napisano na računalu. Zapis koji je ured obrisao ostaje obrisan i
  nakon ponovnog pokretanja. Ostaje i fotografija iz ureda uz list koji se
  mijenjao na tom računalu. Kad je u postavci „Povijest tuđih očitanja
  (mjeseci)” upisan broj mjeseci, potvrda kaže da se starija tuđa očitanja
  ne vraćaju, osim za letve koje računalo prati. U tablici „Što računalo
  drži” stoje i područje i godina u kojima su ostali samo dokumenti s
  drugih računala, pa se brisanje može ponoviti. Gumb „Obriši s računala”
  stoji samo ondje gdje ima što obrisati, a vrste pišu imenom, npr.
  Vodočuvarski dnevnik.
- **Pretplate: prijedlog iz zaduženja i stroži obrazac.** Prijedlog iz
  zaduženja dodavao je samo dnevnike radova i prijave, pa vodočuvaru nisu
  stizali njegovi dnevni listovi, zadaci iz ureda ni očitanja njegova
  područja. Sad dodaje i vodočuvarski dnevnik i očitanja, a poruka nabroji sva
  dodana pravila. Tko je stari prijedlog već prihvatio, dobije ponudu za ono
  što mu nedostaje. Obrazac je primao godinu i rok bez provjere: „2025.” s
  točkom tiho je postajalo „sve godine”, a „90 dana” u roku „trajno”. Sad
  obrazac na takav upis javi što treba upisati, a isto pravilo ne može se
  dodati dvaput. Zadana početna godina sad je prošla godina, jer vodočuvarski
  dnevnik i zadaci prelaze preko Nove godine. Natpisi su ispravljeni: nema
  više „PDF na zahtjev”, kojeg u programu nema, ni „ostaje u uredu”. Uz
  dnevnike piše da su to dnevnici radova po branjenom području, a na vrhu
  stranice piše ime računala.
- **Zaključena inventura može se otključati.** Kad se poslije zaključenja
  upiše promet s danom prije inventure, stanje skladišta ga pokaže, a
  zaključena inventura i njezin izvoz ne. Inventura se nije dala ponovno
  otvoriti. Sad na stranici zaključene inventure postoji gumb „Otključaj
  inventuru”: inventura se vraća u nacrt s knjižnim stanjem toga dana, a
  usklađenja koja je zaključenje upisalo brišu se. Ponovno zaključenje
  razliku računa iznova. Gumb vidi samo globalni administrator. Kad
  skladište iza inventure ima noviju zaključenu, najprije se otključava
  novija; noviji nacrt ne smeta. U napomenu inventure upiše se tko ju je i
  kada otključao, a na zidu događanja piše „Inventura vraćena u nacrt”.
- **Izvoz inventure više ne piše dvije točke na kraju napomene.** Napomena
  koja završava točkom u izvozu inventure dobila je još jednu. Sad se točka
  dodaje samo napomeni koja je nema.
- **Fotografije uz dnevne listove stižu i na druga računala.** Fotografije
  uz dnevni list vodočuvara, npr. uz obilaske prenesene iz stare
  evidencije, ostajale su samo na računalu na kojem su upisane. Drugo
  računalo dobilo je list, ali fotografiju nije ni tražilo, ni kad prati
  sve. Sad je traži prema praćenju vodočuvarskog dnevnika za to područje:
  uz „samo kazalo” ne, uz preglede ili sve da. Isto vrijedi za listove
  primljene ranije: program ih pri prvom pokretanju nove inačice pregleda
  i zatraži fotografije koje nedostaju. Fotografije sad pripadaju području
  i godini svog lista, pa na drugim računalima za njih vrijede razina i rok
  iz praćenja, kao za fotografije prijava.
- **Dohvat dokumenata i fotografija više ne zastaje.** Računalo u jednoj
  razmjeni traži najviše 200 dokumenata i fotografija koje mu nedostaju.
  Kad je na vrhu popisa stajalo 200 onih koje po praćenju ne dohvaća (npr.
  PDF-ovi uz „samo pregledi”), ostali iza njih nisu stizali nikad. Sad se
  takvi preskaču, pa se traže oni koje računalo zaista dohvaća.
- **Nakon uparivanja razmjena s tim računalom kreće odmah.** Kad računalo ne
  odgovara, automatska razmjena zove ga sve rjeđe, na kraju tek svaki osmi
  krug, što je uz zadani razmak od pet minuta jednom u 40 minuta. Laptop je
  tako od kuće dugo zvao uredsku adresu koja se izvana ne vidi. Kad se zatim s
  uredom upario preko Tailscalea, automatska razmjena ured i dalje nije zvala
  dok nije prošlo tih 40 minuta; radilo je samo „Sinkroniziraj sa svima”. Sad
  potvrda uparivanja briše raniji niz neuspjeha za to računalo, pa razmjena s
  njim kreće odmah. Pokreće je računalo koje je nazvalo, da isti podaci ne idu
  dvaput. Za ostala računala ništa se ne mijenja. Kad je automatska razmjena
  isključena, razmjena i dalje ide samo na ručni zahtjev.
- **Obrisana dionica ne ostavlja epizode obrane za sobom.** Brisanje dionice
  odnosilo je i njezine epizode obrane, ali to nije bilježilo, pa drugi
  čvorovi za to nisu znali. Novi čvor takvu je epizodu čekao pri svakoj
  razmjeni, a pri svakom pokretanju programa u dnevniku programa stajala je
  greška, i na računalu na kojem je dionica obrisana. Sad brisanje dionice kao
  obrisane bilježi i sve njezine epizode. Epizoda koja je zadnji put mijenjana
  prije brisanja dionice preskače se i ne vraća ni kad se dionica ponovno
  upiše pod istom šifrom. Epizoda koju je drugo računalo otvorilo ili
  mijenjalo poslije brisanja, dok za brisanje još nije znalo, vraća se s
  dionicom kad se ona ponovno upiše.
- **Dionica s obrisanom letvom ili objektom stiže na druge čvorove.** Kad se
  iz registra obriše letva ili objekt koji dionica još navodi, novi čvor tu
  dionicu nije dobio, ni njezine epizode obrane. Pri svakom pokretanju
  programa u dnevniku programa stajala je greška za dionicu, i na računalu
  na kojem je letva obrisana, a ondje se dionica više nije dala ni
  spremiti. Sad se
  obrisana letva ili objekt uz dionicu preskače. Letvu ili objekt koji na
  čvor još nisu stigli dionica i dalje čeka.
- **Obrisana bilješka arhive ne vraća se.** Kad su za isti sat postojale
  dvije bilješke, upisane na dva računala, brisanje je obrisalo samo onu
  koja se vidjela. Druga se vratila pri idućem pokretanju programa, a novi
  čvor ju je znao dobiti odmah. Sad brisanje bilješke briše sve bilješke tog
  sata koje to računalo ima. Kad je drugo računalo za isti sat upisalo
  bilješku za koju računalo koje briše još nije znalo, nakon brisanja sva
  računala odmah pokazuju nju, ne tek nakon ponovnog pokretanja.
- **Brisanje naselja ili općine koju navodi dionica stiže svuda.** Na
  računalima koja imaju takvu dionicu obrisano naselje ili općina ostajali
  su, a razmjena je svaki put javljala grešku. Novi čvor dionicu nije
  dobio, a pri pokretanju programa u dnevniku programa stajala je greška za
  dionicu, i na računalu na kojem je naselje ili općina obrisana. Sad
  obrisano naselje ili općina nestane svuda, a dionica ostaje bez veze s
  njim.
- **Brisanje dok drugo računalo upisuje.** Jedno računalo obriše područje,
  sektor, općinu, županiju ili korisnika, a drugo u isto vrijeme uz njih upiše
  objekt, naselje ili dužnost. Računala su se tada trajno razišla: jedno je
  novi zapis čekalo zauvijek, a drugo nije moglo provesti brisanje. Sad
  brisanje ima prednost, pa se takav zapis ne pokazuje. Kad se područje
  ponovno upiše pod istim brojem ili sektor pod istom oznakom, zapis se vraća
  s njim. Pravilo vrijedi na računalima s 0.0.38-alfa; što to znači dok se
  mreža nadograđuje, piše u stavci „Nadogradnja”. Brisanje korisnika ili
  općine sad kao obrisane bilježi i dužnosti i naselja koja to računalo nije
  imalo na popisu.
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
- **Ispuštanje elektrana pri običnoj vodi uračunava vodu s međusliva.** Pri
  običnoj vodi prognoza istjecanja HE Varaždin, Čakovec i Dubrava drži
  dnevni srednjak uz dotok uzvodne elektrane, pomnožen omjerom istjecanja i
  dotoka zadnjeg tjedna. Program je umjesto istjecanja uzimao sam dotok, pa
  je omjer bio uvijek 1 i voda s međusliva se gubila. Sad se uzima pravo
  istjecanje. Najviše se mijenja HE Varaždin. U provjeri za 2023.–2025., s
  ispravkom koji vidi dežurni, prosječna pogreška na Dravi mijenja se
  najviše 0,3 cm ili m³/s, a prognoza je u prosjeku do 1 cm ili m³/s viša.
  Na poplavnim valovima od 2013. najavljeni vrh se gotovo ne mijenja,
  najviše 7 cm na Novom Virju. Ispravak prognoze dosad je izmjeren uz ovu
  grešku, pa nakon nadogradnje na 0.0.38 globalni administrator na čvoru
  koji izdaje prognozu treba pokrenuti Prognoze → Postavke → *Pripremi
  model* (Odluke (13. 10. 2026.)).
- **Kiša po slivovima radi i s nepotpunim nizom.** Kad u nizu kiše nekog
  međusliva nema nijedna tri uzastopna dana, npr. jer fali svaki drugi dan,
  izračun uobičajene kiše se srušio, a s njim i kiša po slivovima na
  naslovnoj.
  Sad takav međusliv zadrži pragove jednog dana, a pragova triju dana
  nema. Prag od 0 mm, kakav ispadne u takvom ili gotovo suhom nizu, sad
  znači da praga nema; dosad je dizao razinu kiše i kad nije palo ništa. U
  tablici kiše po međuslivovima umjesto njega stoji crtica (Odluke (13. 10.
  2026.)). Rušenje je našao test; s nizovima iz arhive nije primijećeno ni
  jedno ni drugo.
- **Krivulje protoka opet se potvrđuju, a stranica pita za sustav snimke
  korita.** Na stranici Unos podataka u arhivu pregled krivulja protoka iz
  HIS-2000 prekidao se na pola, bez gumba „Upiši i izgradi”, pa se krivulje
  nisu dale upisati. Kad program ne prepozna kotu nule snimke korita,
  stranica nikad nije pitala u kojem je sustavu, pa se takva snimka uvijek
  preskakala.
  Uzrok je isti: pitanje o sustavu stajalo je u obrascu krivulja, a ne u
  obrascu snimki. Sad stoji uz snimke, a pregled krivulja iscrta se do
  kraja.
- **Uvoz krivulja protoka ne briše starije krivulje.** Uvoz krivulja iz
  HIS-2000 zamijenio je cijelu datoteku krivulja letve krivuljama iz
  izvoza. Izvoz s manje godina tako je brisao starije krivulje. Na Donjem
  Miholjcu izvoz postaje 5150 ima krivulje 1993.–2026., pa bi odnio
  krivulje 1926.–1992. koje je donio izvoz spojene postaje 5555. Sad se
  krivulje spajaju po razdoblju: razdoblje iz izvoza dodaje se ili
  zamjenjuje isto razdoblje, a razdoblja kojih u izvozu nema ostaju.
  Stariji izvoz uvezen poslije novijeg ne odnosi krivulje koje ima samo
  noviji, pa ne ostavlja godine bez krivulje. Kad oba izvoza imaju krivulju
  koja počinje istog dana, vrijedi ona iz izvoza uvezenog zadnjeg. Prije
  upisa pregled kaže
  koje se zatečene krivulje razlikuju od izvoza i koliko ih ostaje. Kad
  zatečena krivulja počinje usred krivulje iz izvoza, pregled kaže i to, jer
  od tog dana vrijedi zatečena krivulja. Izvornik novog izvoza sprema se uz
  stari, pod drugim imenom.
  Kad zatečena datoteka ima redak koji se ne da pročitati, krivulje se ne
  spajaju, a poruka kaže koji je to redak.
- **Uvoz niza bez letve kaže da letva nije zadana.** Administrator područja
  koji je potvrdio niz bez letve u nazivu datoteke i u obrascu dobio je
  poruku „Nemate pravo uvoziti podatke letve  — …”, bez imena letve. Sad
  dobije isto što i globalni administrator: da letva nije zadana.
- **Broj točaka snimke korita.** Poruka o snimci s premalo točaka pisala
  je „snimka ima samo 2 točaka” i „1 točaka”. Uvoz izvoza iz HIS-2000 uz
  dvije snimke istog dana pisao je „(3 točaka)”. Sad piše „2 točke”,
  „1 točku” i „3 točke”.
- **Snimka korita koja se ne da zapisati.** Kad se nijedna snimka nije dala
  zapisati, poruka je uvijek govorila da je odabir vjerojatno istekao i da
  datoteke treba poslati ponovno, i kad su odmah iza stajali pravi razlozi.
  Odabir tada nije istekao, a ponovno slanje ne pomaže. Sad poruka navodi
  samo razloge, a o isteku govori samo kad snimki koje čekaju više nema.
- **Vrijeme bez sekundi u datoteci niza čita se kao sat.** Gradnja arhive
  čitala je vrijeme bez sekundi, npr. „2024-03-01 07:00”, kao sam datum.
  Satne vrijednosti istog dana tako su se sažele u jednu, a ostala je
  zadnja. Datum kraći od deset znakova, npr. „2024-3-1”, srušio je gradnju.
  Sad se vrijeme sa satom i minutama čita kao sat, a krivo zapisan datum
  gradnja javi greškom s imenom datoteke i brojem retka. Datoteke koje
  piše program uvijek imaju sekunde, pa se to događalo samo s ručno
  složenim datotekama.
- **Dopuna niza više ne briše zatečene retke.** Kad se niz u arhivi
  dopunjuje, kroz vrata arhive, ulaganjem očitanja, s kišomjera ili s
  HydroViewa, program pročita što datoteka niza već ima i zapiše sve
  zajedno. Retke s trećim stupcem, polja u navodnicima i vrijeme bez
  sekundi pritom nije razumio, pa su tiho nestali iz datoteke, iako ih je
  gradnja čitala. Isto se događalo s retkom u kojem je datum ili vrijednost
  krivo upisana. Sad dopuna datoteku čita kao i gradnja. Kad neki redak ne
  razumije, ili iza vrijednosti nešto piše, upis odbije i kaže koji je to
  redak, a datoteka ostane kakva je bila. Pregled pod Unos u arhivu to kaže
  već prije upisa, u okviru „Niz već postoji”, i nudi zamjenu.
- **Katalog izdanja bez praznog retka.** Kad je čvor prvi put izdavao letvu
  koju je primio paketom, a paket s novim brojem nije se dao složiti, u
  katalog izdanja ušao je redak bez letve i bez datoteke. Taj je redak
  prelazio u svako sljedeće izdavanje, a objava arhive pokušavala ga je
  pročitati i u dnevnik programa pisala grešku. Sad se takva letva samo
  preskoči, kao i kad ne uspije prvo sastavljanje paketa. To se događalo
  rijetko, npr. kad se zadnji niz letve makne usred izdavanja. Objava arhive
  sad preskoči prazan redak koji je u katalog ušao prije 0.0.38, pa više ne
  puni dnevnik programa greškom. Sljedeće izdavanje arhive taj redak izbaci iz
  kataloga i to navede u ispisu.
- **Krivulje protoka prate svoju datoteku.** Krivulja protoka maknuta iz
  datoteke krivulja letve ostajala je u arhivi i nakon ponovne gradnje. Ista
  mapa tako je davala drukčiju arhivu, ovisno o tome gradi li se iznova ili
  preko stare. Sad gradnja letve koja u stablu ima datoteku krivulja u arhivu
  postavi točno krivulje iz te datoteke, a ispis gradnje kaže koja je krivulja
  maknuta i kad je vrijedila. Iznimka je letva koja je stigla paketom s drugog
  čvora: krivulje koje ta letva već ima u arhivi ostaju i kad ih u datoteci
  nema, a krivulja iz datoteke ne zamijeni krivulju koja u arhivi vrijedi od
  istog dana. Iz datoteke se upišu samo krivulje kojih u arhivi još nema.
  Ispis gradnje kaže koja krivulja nije maknuta ili nije upisana i zašto. Uvoz
  iz HIS-2000 krivulje spaja s datotekom, pa krivulja koje u novom izvozu nema
  ostaje i u datoteci i u arhivi. Krivulje letve koja u stablu nema datoteku
  krivulja gradnja ne dira (vidi Krivulje protoka bez datoteke čekaju odluku).
  Gradnja stane i ne makne nijednu krivulju letve kad datoteka krivulja nema
  zaglavlje kakvo treba, kad je prazna ili ispod zaglavlja ima samo prazne
  retke, ili kad neki njezin redak nema svih osam stupaca, ne počinje datumom,
  u stupcu za broj ima nešto što nije broj, npr. zbog zareza umjesto
  točke-zareza, ili u stupcu oblik nešto drugo nego polinom ili potencija.
  Poruka kaže koja je datoteka, a za neispravan redak i njegov broj. Uvoz
  izvoza HIS-2000 zaglavlje i retke zatečene datoteke provjerava istim
  pravilima i prazne retke, kao i gradnja, preskače; praznu zatečenu datoteku,
  ili onu koja ispod zaglavlja ima samo prazne retke, zamijeni izvozom.
- **Snimka korita bez datoteke čeka odluku, kao i niz.** Snimka korita kojoj
  je datoteka maknuta iz stabla ostajala je u arhivi, a nigdje se nije vidjelo
  da joj datoteke više nema. Ispravak dana snimke u datoteci tako je ostavljao
  dvije snimke istog korita. Sad gradnja takvu snimku ne briše, nego je pokaže
  pod Administracija → Unos u arhivu → Nizovi bez datoteke u stablu, s danom
  snimke i brojem točaka, a gumb Makni je miče iz arhive. Snimke letve koja u
  stablu nema svoju mapu na popis ne idu, npr. letve koju čvor samo prima
  paketom. Datoteke u stablu se ne diraju. Datoteka snimke koja se ne da
  pročitati stoji na popisu kao zaseban redak, s razlogom i bez gumba Makni.
  Ostatak popisa se i dalje vidi, osim snimki te letve kojima nema datoteke:
  one dođu na popis tek kad se nečitljiva datoteka popravi, jer se dotad ne
  zna koju snimku ona drži.
- **Arhiva koje nema ne nastaje prazna.** Na čvoru bez arhive vodostaja
  program je pri pokretanju i na stranicama arhive na njezinu mjestu
  ostavljao praznu datoteku vodostaji.db. Program je arhivu vodostaja,
  bazu očitanja i bazu prognoza i onda kad ih je samo čitao otvarao s
  pravom upisa. Sad ih tada otvara samo za čitanje, pa prazna datoteka više
  ne nastaje.
- **Krivulje protoka bez datoteke čekaju odluku.** Kad je iz stabla nestala
  cijela datoteka krivulja letve, krivulje su tiho ostajale u arhivi. Sad ih
  gradnja pokaže na istom popisu, pod Nizovi bez datoteke u stablu, s brojem
  krivulja i danom od kojeg vrijedi najstarija, a gumb Makni ih miče iz
  arhive. Krivulje letve koja u stablu nema svoju mapu, ili je stigla paketom
  s drugog čvora, na popis ne idu. Ako je letva otkad je stranica otvorena
  dobila datoteku krivulja ili stigla paketom, gumb Makni njezine krivulje ne
  miče, nego to javi.
- **Prognoza bira krivulju protoka po hrvatskom danu.** Prognoza je dan za
  izbor krivulje protoka određivala po svjetskom vremenu (UTC), a stranice po
  hrvatskom. Zato je u prvom satu dana kad počinje nova krivulja, ljeti u prva
  dva sata, prognoza računala protok iz prethodne krivulje. Na Donjem Miholjcu
  od 2017. to su bile tri satne vrijednosti. Sad prognoza i stranice biraju
  istu krivulju.
- **Historijat letve opet pokazuje proglašene obrane.** Odjeljak „Obrane
  vođene po ovoj letvi” nije se pokazivao ni na jednoj letvi, a obrana nije
  bilo ni u izvješću letve, iako su bile proglašene. Program je opis dionice
  tražio pod imenom kojeg u bazi nema, pa čitanje nije uspjelo, a stranica je
  grešku prešutjela. Sad se vide obrane svih dionica letve. Na stranici letve
  uz šifru dionice stoji kratki opis: voda, obala i obuhvat, npr. „rijeka
  Dunav, d.o.; Zeleni otok – Ludaš”. Stacionaža i duljina dionice ne pišu se,
  one su na stranici dionice. U izvješću letve, u Wordu i Excelu, stoji samo
  šifra dionice. Ako se obrane ne daju pročitati, stranica se i dalje otvori,
  a greška se zapiše u dnevnik programa.
- **Drugo brisanje iste letve.** Kad se letva obrisala dvaput, npr. drugim
  klikom ili iz druge otvorene kartice, program je i drugi put javio da je
  obrisana. Pritom je zabilježio brisanje prazne letve, bez imena i šifre,
  koje je razmjenom putovalo na druga računala. Sad javlja da letve više
  nema u registru, vraća na popis letvi i ništa ne bilježi. Brisanje letve
  koja postoji radi kao i prije.
- **Pretplata na sve vrste donosi i arhivu vodostaja.** Laptop s
  pretplatom npr. „sve vrste, BP 16, 2025.–2026.” nikad nije dobio pakete
  arhive svog područja. Paket nosi cijelu povijest letve i nema godinu, a
  pretplata je gledala godine. Paket nije stizao ni kad je sadržaj uz
  zapise bio „Samo pregledi” ili „Samo kazalo”, a vrsta „Arhiva vodostaja”
  s popisa na stranici pretplata nije se dala dodati. Sad pretplata na sve
  vrste i pretplata samo na arhivu donose pakete arhive bez obzira na
  godine i na sadržaj uz zapise, jer su u paketu podaci, a ne slike.
  Pretplata na sve vrste za sva područja tako donosi cijelu arhivu, i
  strane i meteorološke letve. Rok držanja i dalje otpušta primljeni paket
  s računala, a arhiva ostaje. Kad pretplata na sve vrste ima godinu
  početka, pakete računala razmjenjuju tek kad su oba na 0.0.38 ili
  novijem. Cijela arhiva je oko 300 paketa i oko 50 MB razmjene, a
  ugrađena na računalu zauzima oko 10 GB. Za sve ostale vrste godine i
  sadržaj vrijede kao i dosad.
- **Jedan neispravan zapis više ne ruši razmjenu.** Kad je u bazi računala
  bio zapis koji nije ispravan, pa se ne da poslati, pukla je svaka
  razmjena s računalom koje ga još nema, pa nisu stizali ni ostali podaci.
  Sad razmjena takav zapis preskoči, a ostalo pošalje. Zapis se jednom
  upiše u dnevnik programa i stoji kao upozorenje na stranici
  Sinkronizacija i na pločici razmjene na naslovnoj. Isto je s izdanjem
  .cop paketa u Održavanju baze: takav zapis rušio je cijelo izdanje, a sad
  se preskoči i ostali zapisi idu u paket.
- **Primanje računala koje ne uspije ne ostavlja članstvo.** Kad se novo
  računalo primalo u mrežu zahtjevom s kodom, a program je zapeo pri čitanju
  popisa računala koji ide uz potvrdu ili pri upisu ovlasti, računalo je
  ostalo upisano kao član iako potvrdu nije dobilo. Sad program popis
  pročita prije upisa, a članstvo i ovlast upiše zajedno. Računalo koje ne
  dobije potvrdu ne ostaje član, a isti se zahtjev može poslije ponovno
  primiti.
- **Uvoz iz stare evidencije BP16 staje kad ne može pročitati ono što
  već ima.** Prije upisa uvoz obilazaka, obavijesti s terena i evidencije
  radova provjerava što je već preneseno i koje nasipe i vode registar ima.
  Kad se to nije dalo pročitati, uvoz je išao dalje kao da nema ničega.
  Obilazak je tako mogao doći na list dana dvaput, prenesena prijava dobiti
  novi broj, a nasip ući u registar drugi put. Kad Directus nije dao popis
  voda, obavijesti su ostale bez vode i nakon ponovnog uvoza. Sad uvoz u
  takvom slučaju stane i kaže što nije mogao pročitati.
- **Fotografije obilaska stižu i nakon prekinutog uvoza.** Kad se
  fotografija obilaska nije dala spremiti, uvoz je javio grešku, ali
  zadatak je već bio upisan. Ponovni uvoz ga je zato preskočio, pa je na
  listu ostao opis fotografije bez slike. Sad uvoz prvo spremi fotografije,
  onda list, a zadatak tek na kraju. Ponovni uvoz zato obilazak dovrši, a
  list ne dobije ni opis fotografije koje nema ni isti zadatak dvaput.
- **Obavijest s terena bez vremena upisa uzima vrijeme izmjene.**
  Obavijest iz stare evidencije BP16 kojoj nije zapisano kad je upisana
  dobivala je dan uvoza. Objavljena bi tako dobila broj u tekućoj godini, a
  ne u godini kad je nastala. Sad uvoz uzima vrijeme zadnje izmjene.
  Obavijest kojoj nema ni njega ne uvozi se, nego se u izvješću uvoza broji
  pod nepoznato.
- **Obavijesti s terena dovršavaju se nakon prekinutog uvoza.** Kad se
  fotografija, sken ili izvornik obavijesti iz BP16 nisu dali spremiti,
  uvoz je javio grešku, ali prijava je već bila upisana. Ponovni uvoz ju je
  zato preskočio, pa je ostala bez onoga što nije spremljeno. Sad uvoz prvo
  spremi fotografije i sken, a prijavu zajedno s izvornikom tek na kraju.
  Ponovni uvoz zato prijavu prenese cijelu.
- **Fotografija koju Directus nije dao stiže ponovnim uvozom.** Kad
  Directus pri uvozu obilazaka ili obavijesti s terena iz BP16 nije dao
  fotografiju, uvoz je to samo zapisao, a obilazak ili prijavu upisao bez
  nje. Ponovni uvoz preneseno preskače, pa fotografija nije stigla nikad.
  Sad ponovni uvoz fotografiju koja nedostaje traži opet. Obilazak je dobije
  na list na kojem stoji, a zadatak ostaje kakav je bio. Kad vodočuvar za
  taj dan ima dva lista, fotografija ne ide na onaj na kojem obilaska nema.
  Prijava je dobije pod istim brojem, a izvornik se izradi iznova, s njom.
  Nacrt i urudžbirana prijava ne dopunjuju se, a u zapisniku uvoza piše
  zašto.
- **Brisanje nacrta prijave i dnevnog lista ostaje u svom području.**
  Nacrt prijave s terena i nacrt dnevnog lista vodočuvara idu samo na
  računala koja prate to područje i godinu. Brisanje nacrta išlo je ipak na
  sva računala, s cijelim tekstom nacrta, i ondje je ostajalo i kad
  računalo prestane pratiti područje. Sad brisanje ide istim putem kao i
  nacrt.
- **Paket izdanja pod tuđim imenom ne blokira prave pakete.** Član mreže
  mogao je svojim ključem potpisati paket izdanja (.cop) pod imenom drugog
  računala i s visokim brojem izdanja. Računalo koje ga je uvezlo zatim je
  prave pakete tog računala odbijalo kao starija izdanja. Sad računalo
  koje je u mreži ugrađuje paket samo kad ga je potpisalo računalo koje je
  u njemu navedeno kao izdavač. Računalo koje još nije u mreži to ne može
  provjeriti, pa paket ugradi kao i dosad, a u poruci nakon uvoza piše da
  članstvo izdavača nije provjereno.
- **Čekanje na uparivanje nakon prekida.** Kad se čekanje na uparivanje
  prekine i odmah pokrene ponovno, prvo čekanje je pri gašenju brisalo
  zapis o drugom. Stranica tada nije pokazivala da računalo čeka, a drugo
  čekanje nije se dalo prekinuti do isteka od deset minuta. Sad prekinuto
  čekanje ne dira novo. Kad se čekanje prekine i odmah pokrene ponovno,
  računalo koje je nazvalo baš u trenutku prekida dobije odmah poruku da
  je uparivanje odbijeno, pa može nazvati ponovno. Kad se čekanje samo
  prekine, takav se poziv kao i dosad pokaže na stranici uparivanja za
  potvrdu.
- **Brisanje s oglasne ploče ne briše ono što bi razmjena vratila.** Uprava
  je s ploče mogla obrisati objavu prijave poslanu s terenskog laptopa, a
  objava se vratila već sljedećom razmjenom. To se događalo kad s laptopa
  otad nije stigla nijedna nova prijava ni izmjena prijave za to područje i
  godinu. Sad program takvo brisanje odbije i javi zašto. Zapis se može
  obrisati kad to računalo upiše nešto novije iste vrste za isto područje i
  godinu. Kod zajedničkih podataka, npr. ustroja ili registara, dovoljan je
  bilo koji noviji upis tog računala u te podatke.
- **Nadogradnja.** 0.0.38-alfa radi uz 0.0.37-alfa i nadograđuje se redom, po
  mogućnosti istoga dana: najprije stalni čvor koji izdaje prognozu, zatim
  ostala računala. Odmah nakon nadogradnje tog čvora globalni administrator na
  njemu, izvan poplavnog vala, pokrene Prognoze → Postavke → *Pripremi model*.
  Pri prvom pokretanju program zatraži fotografije dnevnih listova koje
  nedostaju; ima ih samo računalo na kojem su upisane, pa neka bude uključeno
  dok ih stalni čvor ne preuzme, oko tri sata po gigabajtu fotografija. Dok
  sva računala nisu na 0.0.38-alfa, ne brišite područja, sektore, općine,
  naselja, županije, korisnike ni dionice, ni letve i objekte koje navodi neka
  dionica. Na računalu koje je još na 0.0.37-alfa ne koristite ni „Obriši s
  računala” ni „Makni i obriši”, nego pravilo maknite samo gumbom „Makni”.
  Pravilo „Sve vrste” za sva područja s godinom početka, ili sa sadržajem
  „Samo pregledi” ili „Samo kazalo”, nakon nadogradnje počne dohvaćati i
  cijelu arhivu vodostaja, oko 10 GB na disku. Na Windowsu nakon nadogradnje
  provjerite vide li se historijat letve i graf prognoze. Pojedinosti su u
  administratorskim uputama, u odjeljku o nadogradnji.

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
  mijenjao samo u uređivaču teksta, a na NAS-u i uz uslugu sustava za to
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
  uredskog čvora, iako ih Tailscale spaja; sve je išlo preko kućnog servera i
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
  istodobnom primanju s kućnog servera javio tu grešku i verzija nije ušla u
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
- **Razvoj: sređivanje koda** (radni plan, izvan GitHuba). Pokrivenost testovima
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
  `https://cop.primjer.hr`), bez putanje i upita; ostali zahtjevi stranice i
  dalje ne odaju ništa. Pločice koje poslužitelj slaže u PDF već su prolazile.
- Slika za spremnik je klasični Docker manifest: od ovog izdanja NAS sam
  javlja nadogradnju (za prelazak na ovu još jednom *Force update*).

## 0.0.30-alfa — 4. 10. 2026.

**Karte ponovno rade; NAS sam javlja nadogradnju.**

- **Pločice karte s OpenStreetMapa.** Wikimedia je svoje pločice zatvorila
  za sve osim vlastitih stranica (403), pa karte nisu radile ni na jednom
  čvoru. Zadani izvor je sada OpenStreetMap (`tile.openstreetmap.org`), s
  istim podacima i izgledom. Čvor koji u `gocop.toml` ima upisan stari
  Wikimedijin izvor sam prelazi na OpenStreetMap i to zapiše u dnevnik.
  Vidi docs/karta.md (pravila korištenja; za stotine računala vlastiti
  poslužitelj pločica ili podloge DGU-a).
- **Slika za spremnik bez potvrde podrijetla.** S njom je registar davao OCI
  indeks, a provjera nadogradnje na NAS-u traži klasični Docker manifest,
  pa nikad nije javljala novo izdanje (trebao je *Force update*). Od ove
  slike NAS sam pokaže nadogradnju; za prelazak na nju još jednom
  *Force update*.
- Slika nosi ikonu (valovi) i poveznicu *WebUI* za popis spremnika na NAS-u.

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
  (poslužitelji, npr. NAS s Dockerom); kroz tunel ne.
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
  kojem čvor radi (u Dockeru kroz `docker exec`, vidi upute) postavlja
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
  tjedno; slika za spremnik gradi se tek kad provjera prođe.
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
vodostaje i kišu preuzima kućni server, ostao bez novih zapisa 35 Pljuskovih i 6
DHMZ-ovih kišomjera. Čvor koji sam preuzima i dalje čuva svoje letve; čvor
koji ne preuzima ugrađuje paket kad on nosi sve što lokalna letva ima (do
istog ili kasnijeg dana i barem jednako zapisa). Paket koji seže kraće se ne
ugrađuje, pa se ništa ne gubi.

## 0.0.22-alfa — 2. 10. 2026.

**Ploča razmjene više ne broji poslano kao neposlano** — čvor je pamtio dokle
drugi čvor zna onako kako mu je ovaj javio na početku razmjene, pa je ono što
mu je u istoj razmjeni poslao do sljedeće razmjene brojao kao da još čeka
(kućni server je laptopu stalno „slao još 101 verziju”, a laptop je bio usklađen).
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
ovo je na Cloudflareu dodano pravilo da se za domenu poslužitelja ništa ne sprema.

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

Popravci koje je pokazao prvi stalni čvor (kućni server) pri prvoj razmjeni.

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

Prvo označeno izdanje. Program se koristi i provjerava, ali
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
