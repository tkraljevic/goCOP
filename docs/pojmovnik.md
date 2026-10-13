# Pojmovnik

Nacrt za pregled. Brojke su stanje programa na `cbb7eeac` (13. 10. 2026.).

Isti pojam treba se u cijelom programu pisati istom riječju: u pomoći, na
gumbima, u porukama, u PDF-ovima i u e-pošti. Ovaj popis kaže koja je to
riječ, što znači i koje se druge riječi danas za isti pojam javljaju u kodu.
Služi za ispravke hrvatskog teksta i kao podloga za kasniji prijevod.

## Odluke (13. 10. 2026.)

1. Izrađuju se pojmovnik i tablica svih tekstova u programu, za pregled prije
   ispravaka i prijevoda.
2. Ispravljaju se greške u tekstu koje se danas vide i uvodi se pravilo za
   novi tekst.

Dok se ovaj nacrt ne pregleda, po njemu se u programu ništa ne mijenja.

## Kako se koristi

- **Uz tablicu tekstova.** Svaki redak tablice u kojem se javlja pojam iz
  pojmovnika usporedi se sa stupcem „Pravi oblik”. Redak s nepoželjnom
  inačicom označi se za ispravak.
- **Novi tekst** piše se odmah pravim oblikom.
- **„Za odluku”** znači da je pravi oblik samo prijedlog. Ta su pitanja
  skupljena na kraju, svako s prijedlogom.
- **Stupac „Engleski”** ostaje „?” dok ne krene prijevod. Jedan pojam dobit će
  jedan engleski izraz.
- Pojmovnik u pomoći programa (Pomoć → Pojmovnik) objašnjava riječi
  korisniku. Ovaj je za one koji tekst pišu i prevode. Kad se ovaj pregleda,
  onaj se uskladi s njim.

## Što se ovdje ne prevodi i ne mijenja

- **Nazivi organizacije**: sektor, branjeno područje, COP, VGO, VGI,
  podcentar, Direkcija, GCOP i vlastiti nazivi uloga. Upisuje ih organizacija
  u Administraciji → Nazivi (`OrgTerms`, `internal/models/terms.go`) i putuju
  razmjenom, pa se ne prevode ovdje. U pojmovniku stoje samo zadani nazivi
  Hrvatskih voda. U tekstu predložaka „sektor” piše izravno najmanje 172 puta,
  a iz postavke dolazi 91 put; kratica „COP” piše izravno 53 puta, a iz
  postavke 2 puta. Na tim mjestima promjena naziva u postavci ne djeluje. To
  treba pregledati posebno.
- **Tekst akata**: zadana špranča i sve što ulazi u sažetak akta koji se
  potpisuje. Izmjena bi srušila provjeru potpisa starih akata.
- **Nazivi uloga, vrsta dnevnika i službi**: spremaju se u bazu kao naziv
  dužnosti, uspoređuju se i idu u certifikat potpisa.
- **Zaglavlja CSV datoteka**, riječi koje program čita pri povratnom uvozu i
  šifre u bazi.

## Kako su brojke dobivene

Brojano je u mapama `cmd`, `internal` i `web`, bez testova, mape `testdata`,
CSS-a i vanjskih biblioteka (`web/static/vendor`). Broje se cijele riječi u
svim padežima, bez obzira na veliko slovo. Pogoci u imenima iz koda
(npr. `stadijKratica`) ne broje se, ali ime napisano kao obična riječ
(npr. varijabla `letva`) ne da se odvojiti. U broj ulaze i komentari u kodu,
pa je brojka putokaz, a ne popis mjesta; za česte riječi (letva, sektor, akt)
veća je od stvarnog teksta.

Zapis „47 (17)” znači: 47 puta u programu, od toga 17 u mapi `web`
(predlošci stranica i JavaScript), što korisnik najčešće izravno vidi.

```sh
git grep -o -i -w -E 'stup(anj|nj)[a-zčćžšđ]* obrane' -- cmd internal web \
  ':!*_test.go' ':!**/testdata/**' ':!web/static/vendor' ':!*.css' | wc -l
```

## Pojmovi

### Program i mreža

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **čvor** | Jedno računalo s goCOP-om i vlastitom bazom, koje razmjenjuje zapise s drugim čvorovima. | 2.106 (398) | računalo 418 (159), u pomoći često „ovo računalo”; za odluku | Pojmovnik u pomoći („Čvor”) | ? |
| **mreža** | Upareni čvorovi koji dijele isti ključ mreže. | 484 (133) | — | kako ga koristi program | ? |
| **razmjena** | Prijenos zapisa, prognoza i kiše među uparenim čvorovima. | 655 (83) | sinkronizacija 145 (44), i u nazivu stranice „Čvor, mreža i sinkronizacija”; usklađivanje 3 (1) | kako ga koristi program | ? |
| **globalni administrator** | Uloga s pravom na cijeli program: sve račune, sve sektore i postavke čvora. | 138 (36) | glavni administrator 5 (0), i zadani naziv dužnosti prvog računa, koji se sprema u bazu; administrator sustava 2 (1) | naziv uloge (`internal/models/roles.go`) | ? |
| **uloga** | Određuje što osoba u programu vidi i smije mijenjati. | 301 (83) | — | pomoć, „Djelatnici” | ? |
| **dužnost** | Zaduženje osobe: uloga s dosegom (sektor, područje ili dionice), stalno ili kao privremena ispomoć. | 275 (49) | funkcija 155 (39), npr. stupac „Funkcija” na stranici osobe i „primarna funkcija”; zaduženje 208 (39); za odluku | pomoć, „Djelatnici” | ? |

### Vode i mjerenja

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **letva** | Vodomjer na kojem se očitava vodostaj, ručno ili automatski. | 4.065 (490) | vodomjerna postaja 78 (37), npr. naslov poglavlja pomoći „Vodomjerne postaje”; vodomjer izvan izraza „mjerodavni vodomjer” 183 (48); vodokaz 6 (4); za odluku | Pojmovnik u pomoći („Letva”) | ? |
| **mjerodavni vodomjer** | Letva po čijem se vodostaju proglašava obrana na dionici. | 38 (13) | mjerodavna letva 8 (6) | tekst akata (`internal/models/akt.go`) | ? |
| **vodostaj** | Visina vode na letvi u centimetrima, od nule letve. | 1.142 (253) | razina vode 2 (1) | kako ga koristi program | ? |
| **protok** | Količina vode koja proteče u sekundi (m³/s); najčešće se računa iz vodostaja po krivulji protoka. | 549 (88) | ženski rod „protoka”, npr. „Vodostaji (protoke)”, 7 (4), na dnevnom i sektorskom izvješću; možda je preuzeto s obrasca, nije provjereno s izvornikom | kako ga koristi program | ? |
| **očitanje** | Vodostaj, a po potrebi i protok ili temperatura, upisan za letvu u jednom trenutku. | 839 (217) | — | kako ga koristi program | ? |
| **nula letve** | Točka na kojoj letva pokazuje 0 cm. | 27 (2) | nula vodomjera 10 (2) | kako ga koristi program | ? |
| **kota nule** | Visina nule letve nad morem, u metrima; vodostaj + kota nule = kota vode. | 116 (38) | Kota „0” 9 (9), kao naslov stupca i polja; kota nula 1 (0) | Pojmovnik u pomoći („Kota nule”) | ? |
| **kota** | Visina točke nad morem u metrima, uvijek s oznakom visinskog sustava (npr. TRST ili HVRS71). | 511 (134) | oznaka „m n.m.” 11 (3) prema „m n. m.” 40 (12); apsolutna visina 7 (2) prema apsolutna kota 21 (2); sustav „Trst” 18 (12) prema „TRST” 23 (7), za odluku | Pojmovnik u pomoći („TRST i HVRS71”) | ? |
| **krivulja protoka** | Veza vodostaja i protoka za jednu letvu, po odsječcima vodostaja; postavlja je DHMZ. | 42 (9) | HQ krivulja 15 (8), i u Pojmovniku pomoći; za odluku | Pojmovnik u pomoći („HQ krivulja”) | ? |

### Ustroj

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **sektor** | Područje vodnogospodarskog odjela s centrom obrane od poplava (razina 2). | 2.283 (545) | sektor obrane 8 (2) | naziv organizacije (`internal/models/terms.go`); ne prevodi se ovdje | — |
| **branjeno područje** | Dio sektora, u pravilu s vodnogospodarskom ispostavom i podcentrom obrane (razina 3). | 300 (66) | — (kratica BP dolazi iz naziva organizacije) | naziv organizacije (`internal/models/terms.go`); ne prevodi se ovdje | — |
| **dionica** | Dio vodotoka koji se brani kao cjelina; šifra je sektor.područje.broj, npr. B.34.1. | 1.492 (342) | — (puni naziv „branjena dionica” 3 (3), samo u pomoći) | pomoć, „Dionice” | ? |

### Stupnjevi obrane i akti

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **stupanj obrane** | Jedan od četiriju stupnjeva koje proglašava akt: pripremno stanje, redovna obrana, izvanredna obrana i izvanredno stanje. | 47 (17) | stadij obrane 10 (2), a riječ „stadij” sama 142 (38); faza obrane 29 (4); za odluku | Pojmovnik u pomoći („Stupnjevi obrane”); model akta (`internal/models/akt.go`); [NACRT-STADIJI-OBRANE.md](NACRT-STADIJI-OBRANE.md) piše „stadij” | ? |
| **pripremno stanje** | Najniži stupanj obrane; proglašava se obaviješću. | 45 (16) | pripremna obrana 3 (1): naslov praga na kartici prognoze, stranica o metodi prognoze i jedan komentar | Državni plan obrane od poplava (NN 84/10), čl. XXII., kako ga navodi stranica Špranča (`web/templates/spranca.html`) | ? |
| **redovna obrana** | Drugi stupanj obrane; proglašava se rješenjem. | 25 (9) | redovita obrana 4 (2), samo gdje se navodi propis; za odluku | Državni plan obrane od poplava (NN 84/10), čl. XXIII., ondje „redovita obrana”, kako ga navodi stranica Špranča | ? |
| **izvanredna obrana** | Treći stupanj obrane; proglašava se rješenjem. | 41 (9) | — | Državni plan obrane od poplava (NN 84/10), čl. XXIV., kako ga navodi stranica Špranča | ? |
| **izvanredno stanje** | Najviši stupanj obrane; proglašava se rješenjem. | 42 (13) | — (u propisu puni naziv „izvanredno stanje na zaštitnim vodnim građevinama”) | Državni plan obrane od poplava (NN 84/10), čl. XXV., kako ga navodi stranica Špranča | ? |
| **stanje obrane** | Najviši stupanj koji na dionici u nekom trenutku vrijedi, izračunat iz ovjerenih, neponištenih akata. | 45 (11) | — | [NACRT-STADIJI-OBRANE.md](NACRT-STADIJI-OBRANE.md) | ? |
| **aktivna obrana** | Vrijeme dok je otvoren dnevnik COP-a; bez otvorenog dnevnika sektor je u preventivnoj obrani. | 27 (11) | — (suprotno je „preventivna obrana”, 5 (4)) | Pojmovnik u pomoći („Preventivna i aktivna obrana”); „preventivna” i u Zakonu o vodama, čl. 130., kako ga navodi stranica Špranča | ? |
| **akt** | Službeni dokument o uspostavi ili prekidu stupnja obrane: rješenje ili obavijest. | 1.383 (289) | — | model akta (`internal/models/akt.go`) | ? |
| **rješenje** | Akt za redovnu obranu, izvanrednu obranu i izvanredno stanje. | 82 (34) | — (riječ ima više značenja, vidi niže) | Državni plan obrane od poplava (NN 84/10), kako ga navodi stranica Špranča | ? |
| **obavijest** | Akt za pripremno stanje. | 114 (44) | — (riječ ima više značenja, vidi niže) | kako ga koristi program, prema dosadašnjim aktima (`internal/models/akt.go`) | ? |
| **uspostava** / **prekid** | Uspostava proglašava stupanj obrane, a prekid ga ukida i stavlja izvan snage akt o uspostavi. | 52 (14) / 197 (57) | proglašenje i proglasiti 124 (53); ukidanje i ukinuti 29 (9); za odluku izvan teksta akata | tekst akata (`internal/models/akt.go`) | ? |
| **ovjera** | Potvrda ovlaštene osobe kojom akt, dnevni list ili dnevnik COP-a postaje službeni zapis. | 784 (196) | — | kako ga koristi program | ? |
| **poništenje (storno)** | Akt ili zapis u dnevniku COP-a koji nije trebao nastati ostaje vidljiv uz razlog, ali više ne vrijedi. | 168 (63) | storno i stornirati 111 (34): akt se „poništava”, a zapis u dnevniku COP-a „stornira”; za odluku | [NACRT-STADIJI-OBRANE.md](NACRT-STADIJI-OBRANE.md), pravilo 6; pomoć, „Dnevnik COP-a” | ? |

Broj uz „poništenje” uključuje i poništavanje lozinke i gumb „Poništi” na
karti, a broj uz „prekid” i prekid mjerenja ili veze.

### Dnevnici i teren

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **dnevnik COP-a** | Kronološki zapisnik jednog centra obrane dok traje obrana. | 144 (42) | dnevnik centra 5 (0), i naslov PDF-a „Dnevnik centra obrane od poplava”; dnevnik obrane 4 (0), i u jednoj poruci o grešci; dnevnik dežurstva 1 (0); operativni dnevnik 1 (0) | pomoć, „Dnevnik COP-a” | ? |
| **dežurstvo** | Razdoblje rada jedne osobe u obrani, od–do, s opisom i mjestom; iz njega se računa obračun sati. | 192 (57) | — | kako ga koristi program | ? |
| **vodočuvar** | Djelatnik koji obilazi dionice i očitava letve. | 318 (53) | — | naziv uloge (`internal/models/roles.go`) | ? |
| **vodočuvarski dnevnik** | Godišnja knjiga jednog vodočuvara s jednim dnevnim listom po danu. | 39 (16) | dnevnik vodočuvara 6 (0); knjiga vodočuvara 3 (1); vodočuvarska knjiga 2 (2) | pomoć, „Vodočuvarski dnevnik” | ? |
| **dnevni list** | Stranica vodočuvarskog dnevnika za jedan dan; kratko se piše i „list”. | 95 (32) | — („list vodočuvara”, 36 (8), dopušten je kratki oblik) | pomoć, „Vodočuvarski dnevnik” | ? |
| **prijava s terena** | Dokument vodočuvara s opisom, mjestom na karti i fotografijama; vrsta je obavijest, prijava, izvješće ili zahtjev. | 56 (24) | obavijest s terena 12 (2), i naslov stranice „Prijave i obavijesti s terena”; za odluku | pomoć, „Prijave i obavijesti s terena” | ? |

### Prognoza i arhiva

| Pravi oblik | Što znači | Broj | Nepoželjne inačice (broj) | Izvor | Engleski |
|---|---|---|---|---|---|
| **prognoza** | Procjena vodostaja i protoka za +6 i +12 sati i za idućih šest dana; pomoć stručnoj procjeni, ne službena prognoza. | 1.676 (280) | — | pomoć, „Prognoze” | ? |
| **uzdužni profil** | Crtež svih letvi jedne rijeke redom niz vodotok, na kojem se vidi kako val putuje. | 31 (8) | — | pomoć, „Uzdužni profil” | ? |
| **hidrološka arhiva** (kratko: arhiva) | Zasebna baza starijih mjerenja (vodostaj, protok, temperatura vode, nanos), donesena izvana; ne uređuje se. | 13 (3); arhiva 1.010 (182) | arhiva vodostaja 23 (12), i naslov poglavlja pomoći; za odluku | kako ga koristi program (README, pomoć „Arhiva vodostaja”) | ? |
| **ispravak arhive** | Nova vrijednost s obaveznim razlogom, koja stoji uz arhivsku vrijednost i ne briše je; može se povući. | 5 (1) | — | pomoć, „Izvoz i ispravci arhive” | ? |
| **bilješka arhive** | Tekst uz arhivsku vrijednost koji ne mijenja broj nego kaže što on jest, npr. „očitan maksimum”. | bilješka 143 (20) | — („napomena” je slobodan tekst uz očitanje ili zapis; jedna vrsta bilješke zove se „obična napomena”) | model bilješke (`internal/models/biljeska.go`) | ? |

## Riječi s više značenja

Za prijevod su to najčešće različite riječi, pa treba znati koje je značenje
na kojem mjestu.

| Riječ | Značenja u programu |
|---|---|
| prijava | prijava u program (lozinka, PIN); otvorena prijava, tj. sesija („nova lozinka gasi otvorene prijave”); prijava s terena; jedna od četiriju vrsta prijave s terena |
| obavijest | vrsta akta (pripremno stanje); vrsta zapisa u dnevniku COP-a, ono što je stiglo odozgo; vrsta prijave s terena; poruka e-poštom |
| rješenje | vrsta akta; rješenje prijave s terena, koje zatvara predmet („Riješen zahtjev s terena”) |
| izvješće | dnevno izvješće rukovoditelja dionice; sektorsko izvješće; vrsta prijave s terena |
| dojava | vrsta zapisa u dnevniku COP-a, ono što je netko javio; vrijednost s automatske postaje („dojave automatike”) |
| uloga | uloga osobe; uloga čvora („Preuzima vodostaje s izvora”, „Izdaje prognozu”); izraz „uloga čvora” 4 (0) |
| stanje | pripremno i izvanredno stanje su stupnjevi obrane; stanje obrane je najviši stupanj koji vrijedi |
| arhiva | hidrološka arhiva; arhivska građa, tj. službeni zapisi za dugo ili trajno čuvanje |
| mreža | mreža čvorova; mrežna veza („kad je mreža dostupna”) |
| poništiti | poništenje akta; poništavanje lozinke; gumb „Poništi” na karti, koji vraća točke na spremljene položaje |

## Za odluku

Uz svako pitanje je prijedlog. Tekst akata ostaje kakav jest, kako god se
odluči.

1. **Stupanj ili stadij obrane.** Prijedlog: *stupanj*. Češći je (47 prema 10),
   tako ga zovu model akta, obrazac akta i Pojmovnik u pomoći. „Faza” ostaje
   samo u kodu.
2. **Redovna ili redovita obrana.** Prijedlog: *redovna*, kako program danas
   piše naziv stupnja; „redovita” samo gdje se doslovno navodi propis.
3. **Poništenje ili storno.** Prijedlog: *poništiti* za akt i za zapis u
   dnevniku COP-a; „storno” samo u zagradi, kao danas uz akt („Poništi akt
   (storno)”).
4. **Uspostava i prekid ili proglašenje i ukidanje.** Prijedlog: u tekstu
   akata i na popisu akata *uspostava* i *prekid*; u pomoći i porukama smije
   „proglasiti” i „ukinuti”, ali ne u istoj rečenici s „prekidom”.
5. **Dužnost ili funkcija.** Prijedlog: *dužnost*; „funkcija” samo za podatak
   iz vanjskog imenika (Exchange).
6. **Čvor ili računalo.** Prijedlog: *čvor* u postavkama i razmjeni;
   *računalo* smije u pomoći kad se govori o samom uređaju („na ovom
   računalu”).
7. **Letva ili vodomjer.** Prijedlog: *letva*; „vodomjer” samo u izrazu
   *mjerodavni vodomjer* i u tekstu akata; „vodomjerna postaja” zamijeniti
   s „letva”.
8. **Krivulja protoka ili HQ krivulja.** Prijedlog: *krivulja protoka*;
   „HQ krivulja” jednom u Pojmovniku pomoći, kao drugi naziv.
9. **Hidrološka arhiva ili arhiva vodostaja.** Prijedlog: *hidrološka
   arhiva*, kratko *arhiva*, jer u njoj nisu samo vodostaji.
10. **Prijava ili obavijest s terena.** Prijedlog: zajednički naziv *prijava s
    terena*, a vrsta (obavijest, prijava, izvješće, zahtjev) uz nju.
11. **Razmjena ili sinkronizacija.** Prijedlog: *razmjena*, i u nazivu
    stranice („Čvor, mreža i razmjena”).
12. **TRST ili Trst** za visinski sustav. Prijedlog: *TRST*, kao u Pojmovniku
    pomoći i uz HVRS71.
