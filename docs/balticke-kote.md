# Baltičke kote

Kota nule letve vodi se u hrvatskim sustavima, Trstu i HVRS71. Strane
postaje, osobito mađarske, kotu često daju u baltičkom sustavu. Zato je
baltička kota zaseban izvorni podatak postaje i ne preračunava se sama u
Trst.

## Sadašnje stanje

Od 25. 9. 2026. postaja uz Trst i HVRS71 ima i **baltičku kotu nule**, s
oznakom sustava i izvorom. Sva tri podatka uređuju se na kartici postaje,
čuvaju s poviješću izmjena, šalju ostalim računalima i pokazuju u izvozu
kartice u Excel. Baltička kota ne upisuje se u Trst ni u HVRS71 i ne
mijenja ih.

Program baltičku kotu koristi samo na **uzdužnom profilu** (od
27. 9. 2026.). Letva koja nema kotu u Trstu, a ima baltičku kotu sa
sustavom `mBf`, crta se u Trstu uz dodatak 0,675 m. Iznos je provjeren na
paru Letenye–Goričan, gdje razlika iznosi 0,66 m. To je samo prikaz pada
vodnog lica: nije potvrđen preračun i ne upisuje se na karticu.

**Pet mađarskih kota s Dunava** iz tablice od 20. 9. 2026. su u jadranskom
sustavu (mađarska oznaka mAf). Vode se u Trstu, a baltička kota im je ona iz
DanubeHIS-a (ICPDR, podatak mađarske vodne službe OVF), u sustavu EOMA 1900
(Odluke (2. 10. 2026.)):

| Postaja | Trst (tablica, mAf) | Baltička (DanubeHIS) | Razlika |
|---|---:|---:|---:|
| Esztergom | 101,640 | 100,920 | 0,720 m |
| Budapest | 95,650 | 94,970 | 0,680 m |
| Dunaföldvár | 89,580 | 88,860 | 0,720 m |
| Baja | 81,720 | 80,990 | 0,730 m |
| Mohács | 79,880 | 79,195 | 0,685 m |

Razlika od 0,68 do 0,73 m je razlika jadranskog i baltičkog sustava u
Mađarskoj. Tek s tim kotama slaže se i pad vodnog lica od Mohácsa do Batine
(3,5 cm/km umjesto 6,6 cm/km). Je li mađarski jadranski sustav jednak
hrvatskom Trstu, nije posebno provjereno.
[Rekonstrukcija nizova](rekonstrukcija-nizova.md) računa s tim kotama kao
jadranskima, što je ispravno.

**Deset postaja ima baltičku kotu iz izvornog opisa metode**, s mađarskom
oznakom mBf: Paks, Dunaszekcső, Nagybajcs, Drávaszabolcs, Barcs,
Szentborbás, Őrtilos, Vízvár-Heresznye, Letenye i Komárom. Točna
realizacija sustava nije pretpostavljena. Za ostale strane postaje sustav
se ne zaključuje po državi.

**Upozorenje.** Devet od tih postaja već je prije imalo Trst dobiven samo
dodavanjem 0,675 m, prema ranijoj metodi iz evidencije COP-a. To nije
provjeren preračun. Te vrijednosti nisu ispravljene ni proglašene
provjerenima. Letenye ima i HVRS71 preračunat iz tako dobivenog Trsta, pa
treba imati na umu i njegovo podrijetlo.

## Zašto nema automatskog preračuna

[BKG/EUREF](https://evrs.bkg.bund.de/results-and-products/national-reference-frames-and-geoid-models)
vodi nacionalne visinske sustave i njihove veze s europskim EVRF-om.
[Mađarski katalog](https://www.crs-geo.eu/crs/eu-countrysel.php?country=HU)
navodi HU_KRON/NH (EOMA 1980), a
[hrvatski katalog](https://www.crs-geo.eu/crs/eu-countrysel.php?country=HR)
odvojeno HR_TRIE/NOH i HR_HRVD71/NOH. Ta evidencija sama ne potvrđuje
preračun koji bi vrijedio za svaku našu postaju.

Postupak od baltičke kote do hrvatskog Trsta ili HVRS71 na mjestima stranih
postaja zasad nije potvrđen. Zato program ništa ne preračunava i ne upisuje
sam. Prije toga treba utvrditi točan ulazni sustav, gdje vrijedi svaki
korak, koju realizaciju EVRF-a, vrstu visina i očekivanu točnost. Prosječni
državni pomak nije preračun točan na centimetar.

## Kratka povijest

Polja za baltičku kotu dodana su 25. 9. 2026. Istog je dana, nakon potvrde
da su mađarske kote u tablici Dunava baltičke, pet kota iz tablice
premješteno iz Trsta u baltičke, a Trst im je ostao prazan. Ta se potvrda
pokazala pogrešnom. Kote DanubeHIS-a i pad vodnog lica pokazali su da su
kote iz tablice jadranske. Zato su 27. 9. u baltičke upisane kote
DanubeHIS-a, a 2. 10. vrijednosti iz tablice vraćene su u Trst. Upisi su
išli kroz povijest izmjena, bez preračuna, pa se ranije vrijednosti i dalje
vide.
