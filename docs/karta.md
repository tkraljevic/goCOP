# Karte

Za administratora ukratko: podlogu karte bira postavka `[karta] plocice` u
`gocop.toml` ili Administracija → Karte, gdje se biraju i druge podloge
(npr. satelit), boje i ikone objekata za cijelu mrežu. Tu stranicu mijenja
samo globalni administrator. Zadan je OpenStreetMap, koji je
dovoljan za probnu i laganu upotrebu. Za širu upotrebu (stotine računala)
bolja je službena podloga Državne geodetske uprave ili vlastiti poslužitelj
pločica u mreži organizacije, upisan u `plocice`.

Letva koja ima koordinate prikazuje se na karti, na svojoj kartici. Ista
podloga stoji i u registrima **Vodomjerne postaje** i **Teritorijalne
jedinice** (prikaz **Karta**), na stranici vodotoka, u **Slivovi i
meteorološke postaje**, pri odabiru mjesta na prijavi s terena i na obrascu
kišomjera. Biblioteka za kartu (Leaflet 1.9.4, licenca BSD-2) ugrađena je
u program, pa preglednik s mreže uzima samo pločice podloge. Kartu bez
interneta (niže) dobiva od samog čvora.

Pločice skida i sam poslužitelj kad kartu slaže u PDF: položaj na
prijavi s terena i ucrtani obuhvat zadatka na dnevnom listu vodočuvara.
Uzima isti predložak iz `[karta]`, na svaku pločicu čeka najviše 5 s i
prima samo PNG. Ne stigne li i jedna pločica, PDF se sastavlja bez te
karte. Karta bez interneta u PDF ne ide.

## Karta bez interneta

Obična karta i satelit čitaju se s interneta. Kad interneta nema, na
primjer u obrani, ostaju prazni. Zato čvor može držati i vektorsku kartu
na svom disku, u mapi `karte/` uz bazu, i poslužiti je pregledniku.

Karta je u obliku PMTiles i napravljena je iz OpenStreetMapa. Podaci
OpenStreetMapa su pod licencom ODbL, pa karta nosi navod „© OpenStreetMap
suradnici”. Karta za svoje područje izreže se iz Protomapsove karte
svijeta alatom `pmtiles`. Kartu Sektora B daje npr.
`pmtiles extract https://build.protomaps.com/20261027.pmtiles sektor-b.pmtiles
--bbox=17.0,45.05,19.65,46.30 --maxzoom=15` (oko 120 MB). Kartu u
pregledniku crta biblioteka protomaps-leaflet 5.1.0 (licenca BSD-3),
ugrađena u program, svijetlo ili tamno prema temi stranice, s nazivima na
hrvatskom. Ona ništa ne traži s interneta.

Karta se učitava u Administraciji → Karte, pod Karta bez interneta. Tu
kartu učitava i miče samo globalni administrator.

1. Na jednom računalu učita se izvezena `.pmtiles` karta, uz naziv
   (npr. Sektor B) i izvor. Ona tada postaje zadana karta bez interneta
   na svim čvorovima.
2. Ostala računala u mreži dobiju je sama, razmjenom (od 0.0.47-alfa).
   Ništa se ne prenosi ručno.

Razmjenom najprije stigne opis karte: koja je karta zadana, njezino
područje, veličina i otisak cijele karte i svakog dijela od 4 MB. Računalo
koje kartu nema zatim traži dijelove koji mu nedostaju, uz PDF-ove i druge
priloge, koji imaju prednost. Dio može dati i računalo koje kartu još samo
preuzima. Svaki dio provjerava se otiskom pri slanju i pri primanju, a
složena karta otiskom i veličinom iz opisa. Kad se veza prekine ili se
računalo ugasi, preuzimanje nastavlja od dijela na kojem je stalo. Stara
karta briše se tek kad je nova cijela.

Koliko je karte stiglo, piše na istoj stranici, u retku „Na ovom
računalu”. Kad pola sata ne stigne nijedan dio, stranica kaže da karta
čeka računalo koje je ima. Kad dijelovi stignu ispravni, a složena karta ne
odgovara opisu, stranica to kaže, a kartu treba ponovno učitati na
računalu koje je ima.

Dok je na računalu proglašen stadij obrane, karta stiže i šalje se samo s
računalima u istoj lokalnoj mreži, ista kuća ili ured. Unutarnja mreža
Hrvatskih voda među gradovima, VPN, Tailscale i tunel tada se računaju kao
internet. Svako računalo gleda svoju obranu. Čvor u Dockeru uz most
(bridge) dok traje obrana kartu ne prima ni ne daje; uz mrežu računala
(host) radi kao i drugi.

Paketom `.cop` karta se i dalje prenosi računalu s 0.0.46-alfa ili
starijim programom, koje dijelove karte razmjenom ne prima:

1. Na računalu koje kartu ima preuzme se njezin potpisani paket, gumbom
   *Preuzmi .cop za druga računala*.
2. Paket se prenese na drugo računalo, npr. USB ključem, i ondje se učita
   na istu stranicu. Paket karte ne učitava se u Održavanju baze.

Program paket karte prima samo kad ga je izdalo ovo računalo ili član iste
mreže, i provjerava da karta ima otisak i veličinu iz potpisanog paketa.
Izmijenjenu kartu ili paket s krivim potpisom odbije. Računalo koje još
nije u mreži prima samo vlastiti paket; kartu dobije razmjenom kad uđe u
mrežu. Učitavanje karte od stotinjak MB traje i do minute, a karta smije
imati najviše 2 GB.

Kartu čvor daje samo prijavljenim korisnicima. Na kartama u programu
prvo stoji obična karta, a „Karta bez interneta” bira se gumbom sa
slojevima, dolje desno, kao i satelit. Na karti teritorijalnih jedinica
isti gumb otvara i slojeve županija, gradova, općina i naselja. Karta
pokazuje samo izvezeno područje. Iznad povećanja do kojeg je izvezena karta ne dobiva nove
pojedinosti. Uz prazan `plocice` ostale karte nema, pa ni karte bez
interneta, jer se podloge biraju uz običnu kartu (niže). Karta
teritorijalnih jedinica tada uzima zadani OpenStreetMap.

Gumb *Makni kartu* miče kartu kao podlogu na svim čvorovima, a datoteku
briše s ovog računala. Na ostalima datoteka ostaje dok ne stigne nova
karta, a nedovršeno preuzimanje se briše. *Vrati zadano* na istoj stranici
kartu bez interneta ne dira.

## Slojevi vodotoka i područja

Vodotoci su na Karti u slojevima po redu vode (Odluke (4. 11. 2026.)):
međudržavni (kategorija „MEĐUDRŽAVNE VODE” s popisa voda I. reda), I. reda
(ostale kategorije s popisa ili skupina „kanal I. reda”), II. reda te III.
i IV. reda (po upisanoj skupini). Vodotok bez kategorije i bez skupine s
redom ide u sloj „Vodotoci bez upisanog reda”, koji se pokazuje samo kad
takvih ima.

Jezera, akumulacije i retencije su vode s vrstom „jezero”, „akumulacija”
ili „retencija” u registru voda (Odluke (4. 11. 2026.)). Na Karti su u
sloju „Retencije, akumulacije i jezera” kao površina, a ne kao crta toka.
Crtež takve vode prima Polygon i MultiPolygon (`PripremiPovrsinuVode`), a
crta smije ostati uz površinu. Na Karti se crta alatom „Novi poligon” i
sprema cijeli crtež; stacionaže ni prekida crte tu nema. Polja za
zapreminu, površinu iz dokumentacije i branu još nema; zasad su u
napomeni.

Skupina Područja ima županije, gradove i općine te branjena područja.
Karta ih dohvaća s `/karta/podrucja/{sloj}.geojson`: granice su
pojednostavljene (županije oko 80 m, općine i branjena područja oko 30 m)
i nose samo naziv i točku natpisa, bez imena čelnika i kontakata. Složene
su jednom po pokretanju programa. Županije, gradovi i općine dolaze iz
programa, a branjena područja iz mape `geometrija` uz bazu čvora. Gradovi i
općine zadano su isključeni i učitavaju se tek kad se sloj uključi.

## Crtanje na Karti

Objekti, tokovi vodotoka i letve crtaju se i ispravljaju samo na Karti
(Odluke (4. 11. 2026.)), alatom Leaflet-Geoman Free 2.20.2 (licenca MIT,
ugrađen u program). Crtanje se otvara gumbom *Uredi na karti* u
prozorčiću, gumbom *Ucrtaj* ili *Uredi* u pogledu *opširnije* uz sloj, ili
gumbom *Uredi na Karti* na stranici objekta, odnosno *Uredi tok na Karti*
na stranici vodotoka. Stranice objekta i vodotoka kartu samo prikazuju i
učitavaju KML ili GeoJSON, koji odmah zamijeni cijeli zapis.

Na Karti se crta nova crta, pomiču se vrhovi (desni klik briše vrh, a na
tabletu alat *Briši vrhove* dodirom), pomiče se ili briše cijeli dio, a
*Prekini crtu* makne komad crte između dva klika. Nova crta koja počne ili
završi na kraju postojeće nastavlja je. Prianjanje (magnet) se pali i gasi,
a dok se drži Alt, isključeno je samo za taj potez. *Poništi* i *Ponovi* (Ctrl+Z,
Ctrl+Shift+Z) pamte do 50 koraka do spremanja. Kad se nasip dulje od 50 m
poklapa s drugim nasipom (bliže od 10 m), ploča upozori. Bez crte, samo s
točkama km, nasip se ne sprema. Novi i povučeni vrh lijepe se na crte vodotoka, nasipa i brana te na
točke objekata i letvi. Ploča desno pokazuje nacrtanu duljinu prema
duljini nasipa po dionicama. *Generiraj stacionažu* postavi točke km na
svaki puni km i na krajeve dijelova: dijelovi se slože u niz, praznina
između njih zadano ulazi u km, a nasip kroz više dionica nacrtan u isto
toliko dijelova može dobiti km svake dionice. Datoteka KML ili GeoJSON na
Karti se doda crtežu (alat *Učitaj*), pa se pregleda prije spremanja, a
alat *Izvezi* spremi crtež u GeoJSON ili KML za Google Earth. Na kraju
*Spremi* ili *Odustani*.

## Ravnalo i ispis

Ravnalo je na svim kartama, gumb gore desno. Mjeri udaljenost, površinu i
duljinu po nasipu ili vodi; za km stacionaže dohvati punu crtu s
`/karta/objekt/{id}` ili `/karta/vodotok/{kod}`. Ništa ne sprema, a dok se
crta na Karti, ne radi. Računica (udaljenost na kugli polumjera 6371 km,
površina, km) je u `js/mjera.js`, zajednička s mjerom pri crtanju.

Ispis je samo na Karti: format A4 do A0, uspravno ili položeno, i mjerilo
za okvir koji se povlači po karti (Odluke (4. 11. 2026.)). Mjerilo je
obavezno: „Kao na zaslonu” ispisuje okvir uz izračunato mjerilo,
zaokruženo na stotice, a okruglo mjerilo (1 : 5 000 do 1 : 100 000)
namjesti okvir točno na sebe. Za ispis se karta postavi na veličinu
korisnog dijela papira (rub 10 mm) i zum za točno mjerilo, pa ide kroz
preglednik (ispis ili PDF). Na listu su legenda uključenih slojeva i
naslov, mjerilo i sjever, te datum, podloga i tko je ispisao. Format i
položaj papira pamte se na računalu, mjerilo se bira svaki put.

Objekt crta onaj tko smije uređivati objekt, a letvu onaj tko smije
mijenjati letvu. Za objekt se crtaju i točke i poligoni, a nova točka na
nasipu ili brani pita za km stacionaže.

Tok vodotoka mijenja samo globalni administrator. Prazan tok se ne sprema.
Bez nove stacionaže spremaju se samo crte, a točke rkm ostaju iz
spremljenog zapisa. Karta Dunava i Drave pokazuje stacionažu preračunatu
prema [ENC kartama](stacionaza-enc.md), pa se za njih ne generira.
Prethodni crtež ostaje u povijesti zapisa.

## Izvor pločica

Postavlja se u `gocop.toml`:

```toml
[karta]
plocice = 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'
zasluge = '© OpenStreetMap suradnici'
najvise_z = 17
```

Do 0.0.29-alfa zadane su bile Wikimedijine pločice
(`maps.wikimedia.org/osm-intl`). Od listopada 2026. Wikimedia ih daje samo
svojim stranicama i svima ostalima odgovara s 403, pa karte nisu radile.
Program od 0.0.30 taj upis sam zamjenjuje OpenStreetMapovim i to javi u
dnevniku; postavke ga mogu zadržati, ali ga je bolje ispraviti.

OpenStreetMapov poslužitelj pločica ima pravila korištenja: zasluga na
karti, predstavljanje programa (poslužitelj pri slaganju PDF-a šalje svoj
User-Agent), adresa stranice uz zahtjev preglednika i bez masovnog
preuzimanja. Zahtjev bez adrese stranice dobije pločicu „Access blocked”
(osm.wiki/Blocked). Stranice goCOP-a imaju `Referrer-Policy: same-origin`,
pa karte pločicama izričito daju `strict-origin-when-cross-origin`:
poslužitelj pločica vidi samo ishodište, npr. `https://cop.primjer.hr`, bez
putanje i upita.

Prazan `plocice` isključuje kartu. To nije kvar nego izbor: čvor bez interneta
i bez preuzetih pločica nema što nacrtati, a prazan sivi okvir gori je od
nikakvog. Koordinate i poveznica na vanjsku kartu stoje i dalje.

Više karata od toga odstupa. Karta teritorijalnih jedinica s praznim
`plocice` podlogu ne isključuje, nego je traži s `tile.openstreetmap.org`.
Prikaz **Karta** u registru **Vodomjerne postaje** i
karta na stranici prijave s terena tada ostaju prazan okvir. Pri odabiru
mjesta na prijavi karta ostaje bez podloge, ali se točka i dalje označava
klikom ili upisuje ručno.

Karta teritorijalnih jedinica crta granice županija, gradova i općina
ugrađene u program, pa ih pokazuje i kad pločice ne stignu. Ispod nje stoji navod izvora granica (vidi `NOTICE`).

Kad pločice ne stignu, jer mreže nema ili ih poslužitelj odbije, karta to
napiše, umjesto da čovjek gleda sive kvadrate.

## Preuzimanje pločica po području (nije napravljeno)

Na zaslonu karta bez interneta (gore) već radi bez mreže. Dokumenti s
kartom i dalje trebaju obične pločice s interneta. Za njih ostaje
zamisao: svaki čvor preuzme pločice **samo za svoje područje** (sektor ili
što već pokriva) i spremi ih uz bazu. Tada se u postavkama upiše
lokalna putanja i karta radi bez interneta, kao i sve ostalo.

Što treba razriješiti prije nego se to napravi:

1. **Opseg.** Područje se izvodi iz onoga što čvor ionako zna: dionice koje
   prati, letve koje su na njima, njihove koordinate. Omeđen pravokutnik plus
   rub od nekoliko kilometara.
2. **Koliko je to.** Broj pločica raste četverostruko po razini
   približavanja. Za grubu procjenu: cijela Baranja do z=15 je reda veličine
   nekoliko tisuća pločica, do z=17 nekoliko desetaka tisuća. Prije preuzimanja
   program mora reći koliko će toga biti i pitati.
3. **Odakle.** Ni OpenStreetMapov poslužitelj pločica ne dopušta masovno
   preuzimanje (pravila su gore). Za zalihu treba izvor koji to dopušta:
   vlastiti poslužitelj pločica ili podloga koju Hrvatske vode ionako smiju
   koristiti, npr. podloga Državne geodetske uprave.
4. **Kako putuju.** Zaliha pločica je velika i ne ide kroz razmjenu verzija,
   kao ni arhiva vodostaja. Preuzima se zasebno, po istom obrascu.
5. **Osvježavanje.** Podloga stari sporo; jednom godišnje je dovoljno. Ali
   mora se znati koliko je stara.
6. **Dokumenti.** Kartu za PDF slaže poslužitelj i pločice traži punom
   adresom. Lokalna putanja poput `/karta/{z}/{x}/{y}.png`, kakvu predlaže
   komentar u `gocop.toml`, njemu ne radi: zaliha mora biti dohvatljiva i
   poslužitelju, inače PDF ostaje bez karte.

Do tada obična karta i dokumenti s kartom rade ondje gdje ima interneta.
Gdje ga nema, na zaslonu ostaje karta bez interneta, a letva i dalje
pokazuje koordinate i poveznicu.
