# Kalibracija stacionaže po OSM liniji

Stanje 25. 9. 2026. Geometrija toka nije pomaknuta ni pojednostavljena.
Između susjednih ENC sidara rkm se linearno interpolira **po duljini OSM
polilinije**, ne zračnom udaljenošću. Svaki segment ima svoj omjer.
Položaji uređaja i upisane stacionaže postaja ostaju nepromijenjeni.

## Pokrivenost i podrijetlo

| Rijeka | Raspon rkm | Sidra | Izdanje ENC-a |
|---|---:|---:|---|
| Dunav | 1295,600–1432,900 | 139 | 11. 9. 2018. |
| Drava | 0,100–23,200 | 25 | 29. 11. 2018. |

Izvor: [službene ENC karte](https://www.vodniputovi.hr/ris/enc-karte/),
[Dunav ZIP](https://www.vodniputovi.hr/enc/dunav/dunav.zip) i
[Drava ZIP](https://www.vodniputovi.hr/enc/drava/drava.zip).
Izvadak sloja `dismar`, kategorije `catdis=1`, polje `wtwdis`, WGS84.
Uzimaju se cijeli kilometri i krajnje dostupne oznake; obalne oznake druge
kategorije nisu pomiješane s njima. Svako sidro ima izvornu koordinatu i ENC
ćeliju; JSON nosi URL, datum izdanja i SHA-256 preuzetog ZIP-a.

Ovo je **izračunata kartografska stacionaža prema službenom izvoru**, ne novi
službeni geodetski podatak i nije za navigaciju. Karte su iz 2018., a OSM
središnjica može slijediti drugi dio korita od plovnog puta. Najveći poprečni
odmak korištenog sidra od ugrađene OSM linije je oko 79 m na Dravi i 200 m na
Dunavu; to nije procjena točnosti stacionaže.

Nema ekstrapolacije izvan pokrivenosti. Stare oznake ondje ostaju vidljive
kao **orijentacijske**, ali ih izračun ušća na uzdužnom profilu ne koristi.
Za preostalu Dravu i međunarodne dionice Dunava trebaju dodatna provjerena
sidra s koordinatama. Ne koristiti postojeće koordinate postaja kao zamjenu
za neovisna sidra dok nisu provjerene.

## Kontrole

- Projekcija sidara mora imati strogo monoton rkm; duplikati i preokreti
  odbijaju se. Sidro dalje od 1 km od linije zaustavlja kalibraciju prikaza.
- Nepokrivena ili nekompatibilna ručno uređena linija ostaje prikazana uz
  upozorenje, bez tvrdnje da je kalibrirana.
- Iz ENC-a Dunava odbačena su dva neusklađena zapisa, bez automatskog
  ispravljanja: `wtwdis=13981` / ID koji završava `013981`, te `1429.0` /
  ID koji završava `014291`. Ispravno zasebno sidro 1429,0 ostaje.
- Prikaz se izvodi iz geometrije baze (prvenstveno), diska ili ugrađene
  datoteke. Ne upisuje se u bazu i ne stvara zapise sinkronizacije. Cache
  se osvježava kada se promijeni sadržaj geometrije.
- Testovi provjeravaju očuvanje linije, povratni izračun svih sidara,
  nejednake omjere segmenata, granice, anomalije i ponovnu primjenu.

## Ponovljiva priprema i usporedba

Čitač `pyogrio` potreban je samo razvojnoj skripti, ne aplikaciji:

```sh
.venv/bin/python scripts/extract_enc_rkm.py drava /tmp/gocop-drava-enc.zip internal/geometrija/sidra/rijeka-drava.json
.venv/bin/python scripts/extract_enc_rkm.py dunav /tmp/gocop-dunav-enc.zip internal/geometrija/sidra/rijeka-dunav.json
go run ./cmd/usporedi-rkm -vodotok rijeka-drava -enc
go run ./cmd/usporedi-rkm -vodotok rijeka-dunav -enc
```

Alat bazu otvara samo za čitanje i daje prednost njezinoj geometriji. Izmjena
položaja postaja zaseban je sljedeći korak. U sadašnjoj usporedbi Osijek daje
19,113 prema upisanih 19,100; Dalj 1354,023 prema 1355,100, a Batina
1424,497 prema 1424,850. Razlike nisu dovoljne za prepisivanje stacionaža:
treba provjeriti položaj uređaja i izvor njegovog operativnog rkm.
