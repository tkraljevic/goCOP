# Baltičke kote — odvojeni izvorni podatak

Od 25. 9. 2026. postaja nosi zasebno `zero_datum_baltic`, oznaku sustava
`zero_datum_baltic_system` i izvor `zero_datum_baltic_source`. Polja se
uređuju na kartici, čuvaju u knjizi verzija, razmjenjuju među nadograđenim
čvorovima i prikazuju u izvozu kartice u Excel. Baltička kota nije zamjena
za Trst ni HVRS71 u izračunu apsolutnih kota vode.

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
Njihov Trst sada je prazan do potvrđenog preračuna. Promjena je provedena
kroz servis i knjigu verzija; ukupno je popunjeno 15 baltičkih kota.
Hrvatske postaje i slovačke Bratislava/Komárno nisu mijenjane.

Važno: devet tih postaja već je imalo Trst izveden dodavanjem 0,675 m,
prema ranije zabilježenoj metodi/tablici COP-a. To nije novopotvrđena BKG
transformacija. Te vrijednosti nisu ponovno korigirane niti proglašene
provjerenima. Letenye ima i raniji HVRS71 preračun iz tako dobivenog Trsta;
njegovo podrijetlo također ostaje zapisano i treba ga uzeti u obzir.

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
