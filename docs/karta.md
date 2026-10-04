# Karta položaja

Letva koja ima koordinate prikazuje se na karti, na svojoj kartici. Ista
podloga stoji i u registrima **Vodomjerne postaje** i **Teritorijalne
jedinice** (prikaz **Karta**), na stranici vodotoka, u **Slivovi i
meteorološke postaje**, pri odabiru mjesta na prijavi s terena i na obrascu
kišomjera. Leaflet stoji **lokalno** (`web/static/vendor/leaflet/`, Leaflet
1.9.4, BSD-2, 147 KB), pa preglednik s mreže uzima samo pločice podloge.

Pločice skida i sam poslužitelj kad kartu slaže u PDF: položaj na prijavi s
terena i ucrtani obuhvat zadatka na dnevnom listu vodočuvara
(`internal/web/karta_slika.go`, `karta_geo.go`). Uzima isti predložak iz
`[karta]`, na svaku pločicu čeka najviše 5 s i prima samo PNG. Ne stigne li i
jedna pločica, PDF se sastavlja bez te karte.

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
User-Agent) i bez masovnog preuzimanja. Za pilot i laganu upotrebu je
dovoljan. Za širu upotrebu (stotine računala) bolji su službene podloge
Državne geodetske uprave ili vlastiti poslužitelj pločica u mreži Hrvatskih
voda, upisan ovdje.

Prazan `plocice` isključuje kartu. To nije kvar nego izbor: čvor bez interneta
i bez preuzetih pločica nema što nacrtati, a prazan sivi okvir gori je od
nikakvog. Koordinate i poveznica na vanjsku kartu stoje i dalje.

Više karata od toga odstupa. Karta teritorijalnih jedinica s praznim
`plocice` podlogu ne isključuje, nego je traži s `tile.openstreetmap.org`
(`web/static/js/app.js`). Prikaz **Karta** u registru **Vodomjerne postaje** i
karta na stranici prijave s terena tada ostaju prazan okvir. Pri odabiru
mjesta na prijavi karta ostaje bez podloge, ali se točka i dalje označava
klikom ili upisuje ručno.

Karta teritorijalnih jedinica crta granice županija, gradova i općina
ugrađene u program (`internal/geometrija/`), pa ih pokazuje i kad pločice ne
stignu. Ispod nje stoji navod izvora granica (vidi `NOTICE`).

Ako pločice ne stignu — mreže nema, poslužitelj odbije — karta to i napiše
umjesto da pusti čovjeka da gleda sive kvadrate.

## Preuzimanje pločica po području (nije napravljeno)

Zamisao: svaki čvor preuzme pločice **samo za svoje područje** — sektor,
Slavoniju, što već pokriva — i spremi ih uz bazu. Tada se u postavkama upiše
lokalna putanja i karta radi bez interneta, kao i sve ostalo.

Što treba razriješiti prije nego se to napravi:

1. **Opseg.** Područje se izvodi iz onoga što čvor ionako zna: dionice koje
   prati, letve koje su na njima, njihove koordinate. Omeđen pravokutnik plus
   rub od nekoliko kilometara.
2. **Koliko je to.** Broj pločica raste s četverostruko po razini
   približavanja. Za grubu procjenu: cijela Baranja do z=15 je reda veličine
   nekoliko tisuća pločica, do z=17 nekoliko desetaka tisuća. Prije preuzimanja
   program mora reći koliko će toga biti i pitati.
3. **Odakle.** Wikimedijine pločice nisu namijenjene masovnom preuzimanju.
   Za zalihu treba izvor koji to dopušta — vlastiti poslužitelj pločica,
   ili podloga koju Hrvatske vode ionako imaju pravo koristiti.
4. **Kako putuju.** Zaliha pločica je velika i ne ide kroz razmjenu verzija,
   kao ni arhiva vodostaja. Preuzima se zasebno, po istom obrascu.
5. **Osvježavanje.** Podloga stari sporo; jednom godišnje je dovoljno. Ali
   mora se znati koliko je stara.
6. **Dokumenti.** Kartu za PDF slaže poslužitelj i pločice traži punom
   adresom. Lokalna putanja poput `/karta/{z}/{x}/{y}.png`, kakvu predlaže
   komentar u `gocop.toml`, njemu ne radi: zaliha mora biti dohvatljiva i
   poslužitelju, inače PDF ostaje bez karte.

Do tada karta radi ondje gdje ima interneta, a gdje ga nema, letva i dalje
pokazuje koordinate i poveznicu.
