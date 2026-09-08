# Karta položaja

Letva koja ima koordinate prikazuje se na karti, na svojoj kartici. Leaflet
stoji **lokalno** (`web/static/vendor/leaflet/`, BSD-2, 147 KB), pa se sam
program ne oslanja ni na jedan vanjski poslužitelj — s mreže dolaze samo
pločice podloge.

## Izvor pločica

Postavlja se u `gocop.toml`:

```toml
[karta]
plocice = 'https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png'
zasluge = '© OpenStreetMap, pločice Wikimedia'
najvise_z = 17
```

Prazan `plocice` isključuje kartu. To nije kvar nego izbor: čvor bez interneta
i bez preuzetih pločica nema što nacrtati, a prazan sivi okvir gori je od
nikakvog. Koordinate i poveznica na vanjsku kartu stoje i dalje.

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

Do tada karta radi ondje gdje ima interneta, a gdje ga nema, letva i dalje
pokazuje koordinate i poveznicu.
