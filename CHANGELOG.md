# Popis izmjena

Verzije prate shemu iz README-a: alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

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

Prvo označeno izdanje. Program se koristi i provjerava u COP-u Osijek, ali
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
na mađarsku prognozu dok je svježa. Ostala ograničenja su u README-u i na
stranici O prognozi.
