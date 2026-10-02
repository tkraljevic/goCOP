# Baltičke kote — odvojeni izvorni podatak

Od 25. 9. 2026. postaja nosi zasebno `zero_datum_baltic`, oznaku sustava
`zero_datum_baltic_system` i izvor `zero_datum_baltic_source`. Polja se
uređuju na kartici, čuvaju u knjizi verzija, razmjenjuju među nadograđenim
čvorovima i prikazuju u izvozu kartice u Excel. Baltička kota ne upisuje se
u Trst ni HVRS71 i ne mijenja ih. U računu je koristi samo uzdužni profil (od
27. 9. 2026.): letva bez kote u Trstu, a s baltičkom kotom čiji sustav nosi
oznaku `mBf`, crta se u Trstu uz dodatak 0,675 m (`BaltikUTrst` u
`internal/web/handlers_prognoze.go`, provjereno na paru Letenye–Goričan,
0,66 m). To je prikaz pada vodnog lica, a ne potvrđena transformacija niti
upis na karticu.

U živoj bazi prenesene su samo eksplicitno navedene izvorne vrijednosti iz
postojećeg opisa metode/napomene: Paks, Dunaszekcső, Nagybajcs, Drávaszabolcs,
Barcs, Szentborbás, Őrtilos, Vízvár-Heresznye, Letenye i Komárom. Izvorna
oznaka je mađarska mBf; točna realizacija nije dodatno pretpostavljena.
Stare napomene ostaju sačuvane. Postojeće vrijednosti Trst i HVRS71 nisu
izmijenjene. Kod ostalih stranih postaja sustav se ne zaključuje po državi.

Naknadno je korisnik istog dana izričito potvrdio da su mađarske kote u
priloženoj tablici Dunava baltičke, a hrvatske Trst. Zato je još pet kota
premješteno bez promjene brojke iz Trsta u Baltička: Esztergom 101,640,
Budapest 95,650, Dunaföldvár 89,580, Baja 81,720 i Mohács 79,880 m.
Njihov Trst tada je ostao prazan (vidi ispravak niže). Promjena je provedena
kroz servis i knjigu verzija; ukupno je popunjeno 15 baltičkih kota.
Hrvatske postaje i slovačke Bratislava/Komárno nisu mijenjane.

Važno: devet tih postaja već je imalo Trst izveden dodavanjem 0,675 m,
prema ranije zabilježenoj metodi/tablici COP-a. To nije novopotvrđena BKG
transformacija. Te vrijednosti nisu ponovno korigirane niti proglašene
provjerenima. Letenye ima i raniji HVRS71 preračun iz tako dobivenog Trsta;
njegovo podrijetlo također ostaje zapisano i treba ga uzeti u obzir.

**Ispravak 27. 9. i 2. 10. 2026.** Pet gornjih kota iz tablice od 20. 9. ipak
je jadranskih (mađarska oznaka mAf), a ne baltičkih. DanubeHIS (ICPDR, podatak
OVF-a) za iste postaje navodi kotu nule u sustavu EOMA 1900 za 0,68–0,73 m
nižu, što je razlika Jadran–Baltik u Mađarskoj; pad vodnog lica od Mohácsa
do Batine tek se s tim kotama slaže (6,6 → 3,5 cm/km). Zato su 27. 9. u
Baltička upisane kote DanubeHIS-a, a 2. 10., uz potvrdu korisnika, vrijednosti
iz tablice vraćene su u Trst:

| Postaja | Trst (tablica, mAf) | Baltička (DanubeHIS) | Razlika |
|---|---|---|---|
| Esztergom | 101,640 | 100,920 | 0,720 m |
| Budapest | 95,650 | 94,970 | 0,680 m |
| Dunaföldvár | 89,580 | 88,860 | 0,720 m |
| Baja | 81,720 | 80,990 | 0,730 m |
| Mohács | 79,880 | 79,195 | 0,685 m |

Upis je proveden kroz knjigu verzija (alat `tools/admin/trst-madjarske-kote`,
izvan repozitorija), bez preračuna i bez promjene povijesti kote. Usklađenost
mađarske jadranske realizacije s hrvatskim Trstom nije posebno provjerena.
[Rekonstrukcija nizova](rekonstrukcija-nizova.md) računa s tim kotama kao
jadranskima, što je ispravno.

## Transformacija nije automatska

[BKG/EUREF](https://evrs.bkg.bund.de/results-and-products/national-reference-frames-and-geoid-models)
vodi nacionalne sustave i veze s europskim EVRF-om.
[Mađarski katalog](https://www.crs-geo.eu/crs/eu-countrysel.php?country=HU)
navodi HU_KRON/NH (EOMA 1980), a
[hrvatski katalog](https://www.crs-geo.eu/crs/eu-countrysel.php?country=HR)
odvojeno HR_TRIE/NOH i HR_HRVD71/NOH. Sama ta evidencija ne potvrđuje
primjenjiv lanac transformacije za svaku našu postaju.

Zasad nije potvrđen cijeli postupak baltička → hrvatski Trst ili HVRS71
koji bi vrijedio na lokacijama obuhvaćenih stranih postaja. Zato nema novog
automatskog upisa. Prije njega treba utvrditi točan ulazni sustav, područje
valjanosti svih koraka, realizaciju EVRF-a, vrstu visina i očekivanu točnost.
Prosječne državne pomake ne koristiti kao centimetarski precizan preračun.

CLI za pojedinačni unos (postojeće ostale rubrike ne mijenja):

```sh
go run ./tools/migrations/upis-letve -letva SIFRA -kota-balticka VRIJEDNOST \
  -kota-balticka-sustav OZNAKA -kota-balticka-izvor IZVOR -probno
```

Prije stvarnog upisa pregledati probni ispis; za upis ukloniti `-probno`.
CLI pri pokretanju dopunjava shemu baze i u probnom načinu.
