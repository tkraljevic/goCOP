# Odluke

Ovdje su odluke o tome kako goCOP radi, po datumu, od najstarije. Kod se na
njih poziva naslovom, a gdje su odluke numerirane, i brojem, na primjer
„Odluke (8. 10. 2026.), pitanje 13”. Zato naslovi i brojevi ostaju kakvi su
bili kad je odluka donesena.

Odluka kaže kako program treba raditi. Što je od toga već u programu, piše
u [popisu izmjena](../CHANGELOG.md), a što tek dolazi, u
[planu razvoja](plan-razvoja.md). Kad kasnija odluka mijenja raniju, to piše
uz raniju.

Računalo s goCOP-om u tekstu se zove i čvor.

## Odluke (2. 10. 2026.)

### Povezivanje računala preko interneta

- Računala se povezuju vlastitim protokolom, po uzoru na WebRTC. Nađu se
  preko sastajališta, računala koje je dostupno s interneta, a zatim se
  spoje izravno: oba u isto vrijeme otvore put kroz svoj usmjerivač.
- Svoju vanjsku adresu računalo doznaje od javnog STUN poslužitelja,
  Googleova ili Cloudflareova. Jedan se bira nasumično, a drugi je rezerva.
- Promet je šifriran ključevima čvorova.
- Promet se ne prerušava u promet Teamsa, Zooma ni drugih programa.
- Posrednik, računalo koje prenosi šifrirani promet kad izravna veza ne
  uspije, bio je predviđen kao zadnji izlaz. Odgođen je Odlukama
  (14. 10. 2026.), točka 3.

Od 14. 10. 2026. ovaj se put zove WEB sync.

### Kote nule mađarskih postaja

Pet kota nule iz tablice Dunava (Esztergom, Budapest, Dunaföldvár, Baja i
Mohács) jadranske su, s mađarskom oznakom mAf, i stoje kao kote u sustavu
Trst. Kao baltičke kote tih postaja upisane su kote iz DanubeHIS-a.
Pojedinosti su u [baltičkim kotama](balticke-kote.md).

## Odluke (4. 10. 2026.)

- Stadiji obrane proglašavaju se prema gore i ukidaju obrnutim redom, a akt
  stupa na snagu u vrijeme koje u njemu piše. Sva pravila stadija zapisana
  su na jednom mjestu, u [stadijima obrane](stadiji-obrane.md).
- Uvoz iz stare evidencije branjenog područja 16 (BP16) ostaje dok se
  evidencija ne pregleda, a predviđen je za brisanje. Ovo su zamijenile
  Odluke (13. 10. 2026.): uvoz je privremen, drži se izdvojen i briše se kad
  BP16 prestane.

## Odluke (5. 10. 2026.)

Dužnosti i privremeno imenovanje.

1. Sve dužnosti osim privremenih su stalne: vrijede dok ih uprava ne
   izmijeni ili opozove. Mandat nema rok, jer se popis ljudi po dionicama
   mijenja u pravilu jednom godišnje, a netko istu dionicu ima godinama.
   Imenovanja direktora vrijede dok ih administrator ne zamijeni.
2. Privremena je samo privremena ispomoć (privremeno imenovanje), kad za
   obranu od poplava nedostaje ljudi.
3. Privremeno se imenuje rukovoditelj ili zamjenik rukovoditelja dionice ili
   branjenog područja.
4. Imenovanje vrijedi dok na njegovim dionicama, odnosno u branjenom
   području, traje redovna ili izvanredna obrana ili izvanredno stanje. Može
   imati i zadan dan, zaključno s kojim vrijedi. Prestaje ono što dođe prvo.
   Kraj obrane određuje samo ovjeren akt o prestanku obrane, u trenutku koji
   u njemu piše.
5. Ovlasti koje privremeni rukovoditelj dodijeli istječu zajedno s njegovom
   dužnošću. Kad mu se dužnost opozove, istječu odmah. Istek koji je već
   prošao opoziv ne mijenja, jer trenutak opoziva nije zapisan, pa bi ga
   svaki čvor upisao drukčije.
6. Datum se piše zaključno. „Vrijedi do 15. 10.” čita se različito, pa se
   piše „zaključno s 14. 10.”. Dužnost tada prestaje 15. 10. u 0 h po
   zagrebačkom vremenu.

## Odluke (8. 10. 2026.)

### Pitanja 1–21

Odgovori na 21 pitanje iz pregleda koda od 6. 10. 2026. Brojevi su brojevi
tih pitanja. Sve su provedene u izdanju 0.0.35.

1. **Knjiga vodočuvara i prijave s terena.** Ovo vrijedi za dnevne listove
   vodočuvara i objavljene prijave, a ne za Dnevnik COP-a ni za dnevnike
   usluga A.02 i A.03.

   | Uloga (doseg) | čita dnevne listove | čita prijave | parafira | upisuje bilješku | zadaje zadatak |
   |---|---|---|---|---|---|
   | Dežurni operater u COP-u (svoj sektor) | da | da | ne | da | ne |
   | Voditelj usluga / Poslovođa | ne | ne | ne | ne | ne |
   | Rukovoditelj branjenog područja (svoje područje) | da | da | da | da | da, svojim vodočuvarima |
   | Rukovoditelj dionice na području vodočuvara (osobito u obrani, iznimno i izvan nje) | da | koje se tiču njegove dionice | ako se tiče njegove dionice | ako se tiče njegove dionice | samo u obrani i ako se tiče njegove dionice |
   | Ovlaštenik za praćenje ugovora (svoje područje) | da | da | da | da | da |

   Poslovođa u dnevnicima usluga A.02 i A.03 vidi samo dnevnik svoje firme,
   a ne dnevnike drugih firmi na drugim branjenim područjima. Time prestaje
   raniji privremeni pristup operatera i poslovođe. Zamjenik ima ista prava
   kao uloga koju zamjenjuje. Uprava sektora i razina 1, vlastita knjiga
   vodočuvara, ovjera lista i rješavanje prijave ostaju kao prije.
2. **Pregled tuđim očima isključenog računa.** Dopušten je, ali samo za
   čitanje, i onda kad su „Upisi tuđim očima” uključeni. Traka kaže da je
   račun isključen. Kad se račun isključi usred pregleda, pregled odmah
   prelazi na čitanje.
3. **Lozinka dežurnom operateru u COP-u.** Uprava sektora smije operateru
   svog sektora dati privremenu lozinku i kod za prijavu izvana. Operater
   ničim ne upravlja, pa se pri usporedbi razina ne broji. Lozinku drugoj
   upravi iste razine i dalje daje viša razina.
4. **Broj akta među čvorovima.** Broj je jedinstven na čvoru i dodjeljuje se
   u istom koraku s upisom akta. Ista oznaka s drugog čvora vidljivo se
   označi u popisu akata. Sukob rješava voditelj Centra obrane od poplava ili
   njegov zamjenik. Pravilo rada: akti se objavljuju na jednom čvoru.
5. **List za dan koji nije počeo.** Može biti samo nacrt, najviše 30 dana
   unaprijed. „Potpiši i predaj” moguće je tek od 00:00 tog dana. Isto
   vrijedi za upis rukovoditelja. Ne ovjerava se i ne zaključuje ono što još
   nije počelo. Dopunjeno Odlukama (11. 10. 2026.).
6. **Redni broj lista.** List dobiva broj kad počne njegov dan, a nacrti
   unaprijed nemaju broj. List za dan koji je prošao ne može se naknadno
   otvoriti: tko ga nije otvorio na svoj dan, taj dan nije radio. Već
   upisani listovi zadržavaju svoje brojeve. Ni program ne otvara list za
   prošli dan: ovjeren akt koji vrijedi od dana za koji vodočuvar nema lista
   upisuje se na njegov današnji list, uz napomenu od kada akt vrijedi.
   Dopunjeno Odlukama (11. 10. 2026.).
7. **Dva lista za isti dan.** Novi list prepoznaje se po osobi i danu, pa je
   na svim čvorovima to isti list. Predan list ima prednost pred nacrtom, a
   bilješke rukovoditelja se ne gube. Postojeći dvojnici se prijave, ne
   brišu.
8. **Tko upisuje očitanje.** Letva dobiva neobavezan podatak „sektori i
   područja koji je prate”, a može ih biti više. Očitanje upisuje svatko s
   valjanom dužnošću u sektoru letve, i dežurni operater u COP-u, ali ne gost
   ni preglednik. Svaka domaća letva je u nekom sektoru i branjenom
   području, a strane letve dobivaju sektor koji ih prati. Letva kojoj sektor
   još nije upisan, ni preko dionica, zadržava dosadašnje pravilo dok se
   sektor ne upiše, da noćni upis dežurnog ne zapne.
9. **Ispravak vlastitog očitanja.** Autor ispravlja svoje očitanje dok ima
   dužnost u sektoru letve, a nakon toga još 72 sata od upisa. Briše samo
   onaj tko odgovara za letvu.
10. **Nepoznato mjesto dežurstva.** Program prvo pokuša prepoznati mjesto iz
    opisa. Ako ne uspije, zapis je „za provjeru”: ne ulazi u zbroj sati i ne
    potvrđuje se dok uprava centra ne izabere opis. Uprava centra smije
    ispraviti opis postojećeg zapisa, a ispravak ostaje zabilježen. Obračun
    sati (obrazac IORS) smije se predati uz upozorenje.
11. **Prijenos u skladište drugog sektora.** Ostaje slobodan, uz obavijest
    sektoru koji prima, na zidu događanja. Potvrda primitka („na putu”)
    dolazi kasnije, ako zatreba.
12. **Predano izvješće sektora.** Izmjena ga vraća u nacrt i traži novu
    predaju, s novim vremenom. Pamte se prvo vrijeme predaje i tko je
    mijenjao. Isto vrijedi za dnevno izvješće dionice. Predano izvješće se ne
    briše, nego povlači.
13. **Testovi sa stvarnim podacima.** Testovi rade na izmišljenom skupu, sa
    sektorom P i izmišljenim osobama. Provjere stvarnih registara pokreću se
    lokalno prije izdanja.
14. **Ručno proglašenje obrane.** Stari kod ručnog proglašenja briše se sav.
    Hitnog ručnog proglašenja mimo akta nema. Ostaje prikaz na historijatu
    letve, „Valovi obrane u nizu”: kad bi prema vodostajima bio koji stadij.
    To je samo prikaz i nikad ne upisuje obranu.
15. **Dnevni model prognoze.** Neuspjelo učenje ponavlja se u idućem satnom
    krugu. Modeli se uče iznova nakon uvoza u arhivu i nakon „Pripremi
    model”. Dan se računa po zagrebačkom vremenu. U postavkama prognoze može
    se izvesti i ranija prognoza za odabrani dan i sat, isto kao aktualna.
16. **Rezervna inačica prognoze.** Kad letvu računa rezervna inačica
    prognoze, prognoza se ne ispravlja prema ranijim promašajima. Provjera
    unatrag radi po istom pravilu. Za sam sat u kojem je prognoza izdana
    ispravka nema ni u jednoj inačici.
17. **„Vrijedi od” u aktu.** Program odbija vrijeme prije otvaranja Dnevnika
    COP-a aktivne obrane u sektoru i vrijeme više od 7 dana unaprijed. Za
    više od 24 sata unatrag ili više od 3 dana unaprijed traži potvrdu, koja
    se bilježi uz akt. Godina oznake akta ostaje iz „Vrijedi od”.
18. **Tko sastavlja akt o obrani.** Akt se izdaje isključivo u Centru obrane
    od poplava. Nacrt sastavljaju voditelj Centra obrane od poplava, njegov
    zamjenik i dežurni operater u COP-u. Ovjerava ga (potpisuje)
    rukovoditelj branjenog područja ili sektora. Vodočuvari i rukovoditelji
    dionica ne sastavljaju akte, nego ih dobivaju na znanje kad su ovjereni.
19. **Primatelji akta.** Popis primatelja slaže se iz više registara. Kad se
    neki od njih ne da pročitati ili dionica nema upisan teritorij, nacrt se
    ipak sastavi, uz vidljivo upozorenje. Dok upozorenje stoji, ovjera traži
    potvrdu. Gumb „Ponovno složi primatelje” slaže popis iznova, a pri
    ovjeri se popis slaže ponovno i pokaže se razlika. Na popisu su firme i
    rukovoditelji svih područja čije su dionice u aktu. Sve dionice dobit će
    teritorij.
20. **Prijave s terena na zidu događanja.** Ondje su sve vrste objavljenih i
    riješenih prijava, vidljive samo onima koji ih smiju čitati. Prijave
    prenesene iz BP16 ne idu na zid.
21. **Vrijednosti izvan raspona.** Iste granice vrijede za sve putove upisa:
    ručni upis, preuzimanje i uvoz. Odbačene vrijednosti broje se i
    pokazuju na stranici letve. Tri akumulacije HEP-a dobivaju oznaku „kota
    u cm n. m.”, s rasponom od 5.000 do 40.000 cm. Označavanje po letvi
    dolazi kasnije.

### Plan izdanja 0.0.35

Ovdje je samo stavka na koju se poziva kod.

6. **Ključ čvora pri pokretanju.** Kad baza postoji, a datoteke s ključem
   čvora (`node-key`) uz nju nema, program se ne pokreće. Kaže koja datoteka
   nedostaje, gdje je očekuje i kako je vratiti iz sigurnosne kopije. Novi
   ključ daje čvoru novi identitet, a takav čvor uparena računala odbiju.
   Zato se novi identitet dobiva samo izričito, zastavicom
   `-novi-kljuc-cvora`. Novo računalo, bez baze, ključ i dalje napravi samo.
   Kad nema `gocop.toml`, ime čvora uzima se iz baze.

## Odluke (10. 10. 2026.)

### Izdanje 0.0.35

Izdanje 0.0.35 izlazi nakon drugog kruga odluka od 8. 10., a prije daljnjeg
pojednostavljenja koda. Ne mora donijeti gotovo pojednostavljenje, nego
mrežu u kojoj sva računala rade po istim pravilima. Nova pravila i polja
vrijede tek kad su sva računala na istoj inačici.

Redoslijed:

1. zatvoriti zaostatke i odluke od 8. 10.;
2. provjeriti, prema uvjetima niže;
3. izdati 0.0.35;
4. nadograditi sva aktivna računala, prvo stalna;
5. zatim mrežni protokol, a pojednostavljenje koda usporedo s njim, gdje se
   dotiču.

Uvjeti izdanja:

- `make quality` i sve provjere na GitHubu prolaze;
- sva aktivna računala nadograđuju se odmah, bez probe stare i nove inačice
  jedne uz drugu, pa mješovite inačice traju samo dok traje nadogradnja;
- nakon nadogradnje sva stalna računala i računalo koje izdaje prognozu
  vide isto stanje: isti broj zapisa po vrsti, ista upozorenja uz istu
  izdanu prognozu i pločicu razmjene bez grešaka;
- nema tihog gubitka podataka.

## Odluke (11. 10. 2026.)

### Vodočuvarski dnevnik

- **Listovi unaprijed ostaju.** Vodočuvar smije otvoriti nacrt lista za
  budući dan, najviše 30 dana unaprijed i bez broja (pitanje 5 od 8. 10.).
  Dopisuje ga dok ga ne potpiše i preda, i kad mu je dan prošao. Novi list
  za prošli dan i dalje se ne otvara (pitanje 6).
- **List otvoren prije ponoći.** Obrazac otvoren prije ponoći, a prvi put
  spremljen poslije nje, sprema se na dan na koji je otvoren. Do predaje
  nema broja, kao i nacrt. Upisani tekst se ne gubi.
- **Upozorenje na nepredane listove.** Vodočuvar dobiva upozorenje za svaki
  svoj list kojem je dan prošao, a nije ga potpisao i predao. Upozorenje
  stoji na početnoj stranici i u njegovu dnevniku, s poveznicom na list.
  Upozorava samo list na koji je sam nešto upisao: spremio obrazac, upisao
  tekst ili objavio prijavu. List koji je otvorio samo program, upisom
  ovjerenog akta ili „Upiši” rukovoditelja, ne upozorava, jer taj dan
  vodočuvar nije radio. Ne upozoravaju ni listovi iz BP16, dan na koji je
  neki list predan, ni arhivirana knjiga.

### Akti i obrana

- **Sken ovjere i slanje ovjerenog akta.** Smije onaj tko akt smije
  ovjeriti. Slati smije i dežurni operater u COP-u.
- **Stare otvorene obrane bez akta.** Obrane otvorene u ranijim izdanjima,
  bez akta, program pri nadogradnji zatvori, s napomenom da su zatvorene pri
  prijelazu na stanje iz akata. Ostaju u povijesti, a obrana se računa samo
  iz akata.

### Razmjena

- **Zapis koji program ne poznaje.** Čvor zapis vrste koju ne poznaje čuva i
  prosljeđuje dalje, ali ga ne primjenjuje dok se ne nadogradi. Nakon
  nadogradnje ga primijeni. Pločica razmjene kaže koliko zapisa čeka
  nadogradnju. Stare i nove inačice tako rade zajedno bez gubitka.

### Postavke čvora

- **Primjer postavki uz bazu.** Uz `gocop.toml` program pri svakom pokretanju
  zapiše `gocop.toml.primjer`, s kratkim opisima i komentiranim primjerima
  svih postavki svoje inačice. Postojeći `gocop.toml` ne dira, a primjer ne
  čita.

### Prognoza

- Modeli prognoze rade se po branjenim područjima, a ne po rijeci. Prikaz po
  vodotoku i sektoru izvodi se iz registra.
- Model prognoze mijenja administrator.
- Kasnije dolazi i Sava: njezina područja dobivaju svoje modele kao podatak,
  bez promjene programa.
- Ovo se provodi poslije kvalitete koda i mreže.
- **Rekonstrukcija izvan provjerenog raspona.** Izvan raspona razina na
  kojem je provjerena rekonstrukcija i dalje računa, ali uz izdanje stoji
  upozorenje da točnost nije provjerena. Dnevna prognoza ostaje izdana i
  označena je kao upitna. Pojedinosti su u dokumentu
  [Rekonstrukcija nizova vodostaja](rekonstrukcija-nizova.md).

### Kvaliteta koda

- Za nov i diran kod vrijede strože granice. Nova funkcija u dijelovima
  programa o kojima ovisi obrana (kritični dio) smije imati složenost i
  rizik izmjene (CRAP) najviše 7, a ostale nove funkcije najviše 10. Dirana
  funkcija ne smije postati gora. Rizik izmjene spaja složenost funkcije i
  pokrivenost testovima, a objašnjen je u dokumentu
  [Kvaliteta koda](CODE_QUALITY.md).
- Pravila pisanja koda iz dokumenta [Kvaliteta koda](CODE_QUALITY.md)
  vrijede za svaku izmjenu, tko god je piše.

### Redoslijed rada

- Prvo čitljivost i pojednostavljenje koda, zatim mreža, a tek onda
  proširenja. Odluke (14. 10. 2026.) mrežu su pomaknule naprijed.

## Odluke (12. 10. 2026.)

### Tailscale

Tailscale povezuje računala prijavljena u istu Tailscale mrežu. Imaju ga
samo neka računala.

- **Zahtjev preko Tailscalea.** Zahtjev koji stigne izravno na Tailscale
  adresu ovog računala vrijedi kao zahtjev iz lokalne mreže. Odlučuje
  adresa na koju je zahtjev stigao, a ne adresa pošiljatelja. Zahtjev na
  drugu adresu računala i svaki zahtjev kroz web tunel ili posrednički
  poslužitelj (npr. nginx) ostaju „izvana”.
  Postavka `[web] tailscale_lokalno = false` to isključuje.
- **Razmjena preko Tailscalea.** Tailscale adrese nikad ne ulaze u zapis
  čvora koji putuje mrežom, ni vlastite ni tuđe. Dva računala koja oba imaju
  Tailscale razmjenjuju njime izravno, a adrese pamte samo kod sebe.
  Postavka `[sync] tailscale = false` to isključuje.
- Računala bez Tailscalea rade kao prije i ne vide te adrese.

Odluke (14. 10. 2026.) ovo su dopunile: Tailscale je mogućnost uz LAN, WEB i
SERVER sync.

### Postavke čvora u Administraciji

- Postavke iz `gocop.toml` uređuju se u Administraciji. Nema zasebne
  stranice: svaka postavka stoji na stranici kojoj pripada (Čvor i mreža,
  Pretplate, Održavanje baze, Opcije).
- Mijenja ih samo globalni administrator, s tog računala ili iz lokalne
  mreže, nikad kroz web tunel ili posrednički poslužitelj. Program čuva
  kopiju prethodne datoteke.
  Promjena vrijedi nakon ponovnog pokretanja.
- **Ponovno pokretanje iz Administracije.** Program se ugasi i izađe, a
  podigne ga ono što ga je pokrenulo: Postava, usluga sustava ili Docker uz
  `--restart`. Čvor pokrenut ručno nitko ne podiže, pa za njega gumba nema,
  nego piše što treba napraviti.

## Odluke (13. 10. 2026.)

### Brisanje u razmjeni

- **Brisanje ima prednost.** Kad jedno računalo obriše zapis, a drugo u isto
  vrijeme upiše zapis koji o njemu ovisi, brisanje ima prednost. To vrijedi
  za sve veze, na primjer za dionicu i njezine obrane, za područje, sektor,
  općinu, županiju i korisnika. Zapis upisan uz obrisani ne pokazuje se
  nigdje, a vraća se ako se obrisani ponovno upiše. Obavijesti o tome mogu
  se dodati poslije, za radnje kojima trebaju.
- **Dionica se ne mijenja sama.** Brisanje letve, objekta ili naselja koje
  dionica navodi ne mijenja dionicu. Korisnik joj doda novu letvu ili
  izmijeni dionicu.
- **Zadnji upis računala ostaje na zidu događanja.** Sa zida se ne briše
  zadnje što je jedno računalo upisalo u jednu vrstu podataka za jedno
  područje i godinu, jer bi ga razmjena vratila. Može se obrisati kad s tog
  računala stigne noviji upis iste vrste. Ovo nije zasebna odluka: isto
  pravilo već vrijedi pri sažimanju knjige verzija, i odatle slijedi.

### Brisanje s računala

- Na stranici „Što ovo računalo prati” brisanje briše samo ono što je
  stiglo s drugih računala. Ono što je nastalo na tom računalu ostaje, s
  PDF-ovima i fotografijama, jer ga možda još nijedno drugo računalo nema.
  Primjer: prijenosno računalo bez veze napiše list s fotografijom, pa makne
  praćenje svog područja. List ostaje.
- Uz gumb „Obriši s računala” piše koliko se briše i koliko ostaje. Potvrda
  kaže iste brojeve, a poruka nakon brisanja kaže što je obrisano, a što je
  ostalo.
- Brisanje koje jedno računalo ponovi za drugim ne smije razmjenom obrisati
  ono što je drugo računalo u međuvremenu ponovno napisalo.

### Arhiva vodostaja

Program arhivu vodostaja gradi iz izvornih datoteka u mapi izvora (gradnja
arhive).

- **Vrijedi ono što piše u datoteci.** U arhivi letva ima točno one krivulje
  protoka koje su u njezinoj datoteci krivulja. Krivulju koje u datoteci više
  nema gradnja makne iz arhive i to napiše u izvještaju gradnje. Letvu bez
  datoteke krivulja gradnja ne dira.
- **Nestala datoteka ne briše se sama.** Kad nestane cijela datoteka niza,
  snimke korita ili krivulja, podaci ostaju u arhivi i pojave se na popisu
  bez datoteke, na stranici Unos u arhivu, s gumbom Makni. Miče ih čovjek.
  Datoteka snimke koja se ne da pročitati stoji na popisu kao zaseban redak,
  s razlogom i bez gumba Makni.
- **Primljeno paketom se ne dira.** Podatke koje je letva dobila paketom s
  drugog računala lokalna gradnja ne miče. Krivulja iz lokalne datoteke ne
  zamjenjuje krivulju koja je stigla paketom, a razliku pokaže izvještaj
  gradnje.
- **Gradnja ne piše natrag u mapu izvora.** Arhiva se gradi iz izvornih
  datoteka, a ne obrnuto. Datoteku koja nedostaje program ne stvara iz
  arhive, pa izvorne datoteke ostaju onakve kakve su stigle. U mapu izvora
  pišu samo uvoz izvornih datoteka i ulaganje očitanja u arhivu.

### Pretplate i paketi arhive

- Paket arhive vodostaja nosi cijelu povijest letve, pa za njega godine
  pravila ne vrijede. Donose ga pravila „Arhiva vodostaja” i „Sve vrste”
  za područje ili sektor. „Sve vrste” za sva područja donosi cijelu arhivu,
  sa stranim i meteorološkim letvama.
- Razina sadržaja za paket ne vrijedi. To su podaci vodostaja, a ne PDF ili
  slika uz zapis, pa paket stiže i uz „Samo pregledi” i „Samo kazalo”.
- Kad istekne rok držanja, primljeni paket briše se s računala, a podaci
  iz njega ostaju u arhivi.

### Uvoz iz BP16

- Uvoz iz stare evidencije BP16 je privremen. Još treba, ali se ne
  pojednostavljuje: drži se na jednom mjestu i briše se kad BP16 prestane.
  Ovo mijenja Odluke (4. 10. 2026.).
- Obavijest bez datuma nastanka dobiva datum zadnje izmjene. Kad ni njega
  nema, ne uvozi se.
- Ponovni uvoz dovršava prekinuti uvoz i donosi fotografije koje stara
  evidencija ranije nije dala.

### Prognoza i kiša

- **Ispuštanje hidroelektrana.** Prognoza računa sa stvarnim istjecanjem
  iz elektrane, a ne s dotokom u nju. Nakon nadogradnje na 0.0.38 globalni
  administrator na računalu koje izdaje prognozu pokrene Prognoze → Postavke
  → Pripremi model, da se ispravak prognoze izmjeri iznova. Pripremi model
  ispravak danas mjeri samo na prognozama izdanima u 0 i 12 h. Mjerenje na
  prognozi svakog sata dolazi kao zasebna izmjena.
- **Kiša po međuslivovima:** kad iz povijesti kiše za neki međusliv ne može
  izračunati uobičajenu kišu (niz je prazan ili nepotpun), program prag ne
  postavlja i u tablici stoji crtica, i za jedan i za tri dana. Prije je
  takav prag ispadao 0 mm, pa je program javljao pojačanu kišu i kad nije
  palo ništa.

### Inventura

- Zaključenu inventuru može otključati samo globalni administrator, gumbom
  „Otključaj inventuru”. Inventura se vraća u nacrt, a usklađenja koja je
  zaključenje upisalo brišu se. U napomenu se upiše tko je i kada otključao,
  a na zidu događanja stoji redak „Inventura vraćena u nacrt”. Otključava se
  samo zadnja zaključena inventura skladišta.
- Razlog: zaključena inventura ne vidi promet koji je poslije upisan s danom
  prije nje. Ponovno zaključenje razliku računa iznova.

### Tekst u programu

- Uz šifru dionice na historijatu letve stoji kratki opis: voda, obala i
  obuhvat, bez stacionaže i duljine.
- Rod osobe u tekstu ide prema spolu iz profila: „Izradio” ili „Izradila”.
  Polja za spol u profilu još nema, pa zasad piše „Izradio/la” i
  „Otključao/la”.

## Odluke (14. 10. 2026.)

### Putovi razmjene

Putovi se zovu LAN sync, WEB sync i SERVER sync i pokušavaju se tim redom:

- LAN sync: računala su u istoj lokalnoj mreži;
- WEB sync: izravna veza dvaju računala preko interneta, u izradi;
- SERVER sync: preko stalnog računala dostupnog s interneta.

Razmjena odmah krene putem koji radi, a WEB sync se pokušava u pozadini.
Tailscale je dodatni put kad ga imaju oba računala.

### Odgovori na šest pitanja o WEB syncu

1. Prije promjena u razmjeni postavlja se novo referentno mjerenje
   kvalitete koda, zasebnom malom izmjenom. Knjiga verzija pri mjerenju
   kvalitete ne ubraja se u dijelove o kojima ovisi obrana.
2. Adrese koje računalo samo dozna ostaju samo na njemu. U zapis računala
   koji putuje mrežom idu samo adrese koje upiše čovjek i adrese
   poslužitelja. Adresu koju računalo nađe u lokalnoj mreži pamti tek kad
   razmjena na njoj potvrdi ključ druge strane. Javnu adresu računalo nikad
   ne nauči samo, nego je upiše čovjek.
3. Posrednik se odgađa. Bez WEB synca dva računala razmjenjuju preko
   stalnog računala (SERVER sync), kao danas. Podaci tada čekaju na stalnom
   računalu do idućeg kruga razmjene, najviše desetak minuta. Ništa se ne
   gubi.
4. Ulazak u mrežu:
   - (a) Uparivanje preko Tailscalea, s istim šesteroznamenkastim kodom na
     oba ekrana, vrijedi kao ulazak u mrežu, isto kao uparivanje u lokalnoj
     mreži.
   - (b) Primanje na daljinu isključuje se dok se ne odluči kako u mrežu
     ulaze ispostave. Kod ostaje u programu. Za ispostave će trebati ulazak
     bez putovanja, a to traži posebnu odluku. Ovo su promijenile Odluke
     (19. 10. 2026.): primanje na daljinu ostaje uključeno, i u beti.
5. Natpisi na pločici razmjene:
   - (a) Tailscale ima svoj natpis „Tailscale”.
   - (b) Za zapise koji čekaju nadogradnju: „Stiglo je N zapisa koje ovaj
     program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.”
     Uz drugo računalo: „<ime računala> čuva N zapisa koje će primijeniti
     nakon nadogradnje.” Svuda piše „nadogradnja”.
6. WEB sync postaje zadano uključen tek posebnom odlukom, nakon dva tjedna
   bez grešaka na svim probnim računalima i kad informatičari organizacije
   dobiju njegov opis.

**Tailscale.** Tailscale ostaje mogućnost uz LAN, WEB i SERVER sync, i za
računala koja informatičari organizacije stave u svoju Tailscale mrežu.
goCOP razmjenjuje njime kad ga imaju oba računala, a
`[sync] tailscale = false` ga isključuje. Tailscale adrese nikad ne ulaze u
knjigu verzija.

**Datumi u planovima** su okvirni redoslijed, a ne rokovi. Korak kreće čim
je gotov onaj o kojem ovisi.

### Redoslijed rada

- Pravila razmjene i WEB sync idu usporedo s pojednostavljenjem koda.
- Testna simulacija, odvojena mreža za vježbu i probu nadogradnje, dolazi
  nakon WEB synca. Ovo su promijenile Odluke (17. 10. 2026.): testna
  simulacija dolazi kad je sve ispolirano.

### Slobodno mjesto na disku

- Program prati slobodno mjesto na diskovima na kojima čvor drži bazu,
  arhivu vodostaja, izvorne datoteke arhive i pakete. Provjerava svakih 10
  minuta.
- Upozorava kad na disku ostane manje od 10 GB, i na velikom disku. Upozorava
  i kad ostane manje od 10 % diska, makar to bilo više od 10 GB.
- Upozorenje stoji na pločici razmjene i u dnevniku programa. Razmjenu
  zasad ne zaustavlja.

### Zid događanja

- Zid događanja ne skriva poništeno ni obrisano. Redak ostaje precrtan, uz
  vrijeme, osobu i razlog, a brisanje traži razlog. To dolazi poslije.

## Odluke (15. 10. 2026.)

- Na GitHubu su u `docs/` samo četiri teme: o aplikaciji, instalacija, plan
  razvoja i kvaliteta koda. Radni planovi, bilješke i povijesne snimke vode
  se u zasebnom repozitoriju, izvan GitHuba.
- Upozorenje da na disku ostaje malo mjesta vide svi koji gledaju pločicu
  razmjene, uz savjet i rečenicu „Ako ne znate kako, javite
  administratoru.” Upozorenje vidi osoba za računalom, a ona javi
  administratoru kad ne zna što učiniti.
- E-poštu o tome dobiva svaka osoba koja je na tom računalu spremila
  lozinku svog sandučića, sa svog računa na svoju adresu. Ničija lozinka
  tako ne šalje poštu drugome. Isključeni računi i računi bez adrese
  preskaču se. Poruka ide najviše jednom dnevno dok je mjesta malo.

## Odluke (17. 10. 2026.)

- Redoslijed rada: najprije pravila razmjene i WEB sync, zatim proširenja,
  zatim poliranje postojećeg i unos podataka. Testna simulacija dolazi kad
  je sve ispolirano. Time se mijenja redoslijed iz Odluka (14. 10. 2026.),
  po kojem je simulacija dolazila odmah nakon WEB synca.
- Proširenja su ustave, crpne stanice i karte. Ustava povezuje uzvodni i
  nizvodni vodostaj i bilježi je li zapornica otvorena ili zatvorena. Crpna
  stanica bilježi početak i kraj rada svakog agregata posebno, a strojar
  upisuje vodostaje na početku, tijekom i na kraju rada, uz dnevnik
  strojara. Svaki objekt, od vodotoka do crpne stanice, ima mjesto na
  karti, a modul Karte sve povezuje u slojeve.
- Model prognoze dijeli se po branjenim područjima i vodotocima. Mijenja se
  u postavkama, bez vrijednosti upisanih u program: rezervna letva, brzina
  putovanja vala za nisku i za visoku vodu, račun u protoku ili u
  vodostaju i slično.
- Nakon simulacije slijedi još jedno poliranje, prema onome što simulacija
  pokaže.
- Kad je sve ispolirano, slijede prezentacija i priručnici za korištenje
  po vrstama korisnika: digitalni priručnik s videom, ugrađen u program.
  To je uvjet za betu. Za stabilnu inačicu dolazi digitalni pomoćnik.
- Uz prognozu po područjima dolazi ispravak prognoze koji ovisi o stanju
  vode, ispitan na valovima koje nije vidio. Uz priručnike za betu dolazi
  bolja tražilica Pomoći. Digitalni pomoćnik odgovara samo iz priručnika i
  ide u program tek kad prođe ispit.
- Pomoćnik osim priručnika čita i bazu, samo za čitanje i u granicama
  ovlasti osobe koja pita: objašnjava upozorenja programa, sažima stanje
  iz dnevnika i izvješća rukovoditelja i administratoru sažima dnevnik
  programa. Upozorenja daju pravila u programu, a sažetak ne zamjenjuje
  službeno izvješće.
- Izvora podataka bit će još, a postojeći se mijenjaju. Uz poliranje dolaze
  upozorenje kad izvor šuti i kratak opis svakog izvora: odakle, koliko
  često, licenca i kontakt.

## Odluke (18. 10. 2026.)

- Cijeli tehnički opis razmjene među računalima, od koraka razgovora do
  pravila za njezine izmjene, vodi se u zasebnom repozitoriju, izvan
  GitHuba. Na stranici [Kako goCOP radi](kako-radi.md) stoji samo kratak
  sažetak za korisnike i administratore, bez imena funkcija.

## Odluke (19. 10. 2026.)

- Primanje na daljinu ostaje uključeno, i u beti. Novo računalo pokaže kod
  za primanje, a osoba uz njega javi ga primatelju telefonom; to je
  dovoljno. Datoteka zahtjeva ide drugim putem, na primjer e-poštom. Kod
  nikad ne ide s datotekom.
- Time se mijenja točka 4b Odluka (14. 10. 2026.).
- Kako u mrežu ulaze ispostave, odlučuje se posebno.

## Odluke (21. 10. 2026.)

- Prava mreža (`gocop`) i testna simulacija (`gocop-sim`) moraju biti
  razdvojene. Zato baza pamti ime protokola svoje mreže, a čvor s drugim
  imenom ne kreće i ne razmjenjuje podatke. Simulacija sama i dalje dolazi
  kad je sve ispolirano.
- Pregled knjige verzija i čišćenje prostora na disku dolaze kasnije.
  Upravljanje prostorom osmislit će se kad dođe na red; zasad nije hitno.

## Odluke (22. 10. 2026.)

- Lanac prognoze ne ovisi ni o jednoj tuđoj prognozi: ni o austrijskoj
  (noel.gv.at) ni o mađarskoj (hydroinfo.hu: Komárom, Letenye). Tuđe
  prognoze stoje uz našu samo radi usporedbe.
- Model ispuštanja za veliku vodu za HE Dubrava i HE Varaždin sljedeći je
  veći posao na prognozi, prije sezone poplava.
- Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni
  model, pa od 2. dana slijede susjedne postaje koje ga imaju, razmjerno
  udaljenosti po rijeci. Pri visokoj vodi dnevni model na Dravi preuzima
  ranije: Botovo od 2., Donji Miholjac od 3. i Belišće od 4. dana.
- „Ono što mi prikazuje graf mora mi prikazivat i tablica.” Tablica,
  uzdužni profil, graf letve i graf u Excelu pokazuju isti niz, s jednim
  izborom modela u Postavkama prognoze umjesto tri. Između dana koje daju
  satni lanac i dnevni model crta postupno prelazi s jednoga na drugi.
- Dnevni model je osnova za sve dane, a satni lanac fino ugađanje koje s
  vremenom slabi. Koliko brzo slabi mjeri se za svaku letvu, posebno za
  nisku i visoku vodu. Kiša u satni lanac ulazi kasnije, kao dotok između
  letvi, iza modela ispuštanja za veliku vodu.
- Ugađanje je izmjereno točnije na Dunavu pri niskoj i srednjoj vodi, pa na
  Batini, Aljmašu, Vukovaru i Iloku tada do 96. sata stoje satni lanac i
  dnevni model zajedno. Pri visokoj vodi i na Dravi uživo je bilo lošije, pa
  ondje ostaje prijelaz; na Dravi dok se ne popravi model ispuštanja HE.
  Živu provjeru ponoviti za 3–4 tjedna. „Oznaka d za dnevni, oznaka s za satni, oznaka za
  kombinaciju je ds”; p ostaje za vrijednost iz susjeda.
- Izdanje 0.0.40 čeka da ovo bude gotovo.
