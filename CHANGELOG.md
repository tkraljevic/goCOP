# Popis izmjena

Verzije prate shemu iz README-a: alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

## 0.0.9-alfa — 2. 10. 2026.

Popravci koje je pokazao prvi stalni čvor (Unraid) pri prvoj razmjeni.

**Prva razmjena bez rupa na površini** — novi čvor registar prima u paketima
po 5000 verzija, pa je naselje znalo stići prije svoje općine i ostati samo u
knjizi (2205 naselja i jedna dionica). Neuspjeli zapisi sad se pokušavaju
iznova u istom prijenosu i sa svakom sljedećom razmjenom, dok ne prođu.

**Prva razmjena odjednom** — dok je razgovor pun (5000 verzija), razmjena
nastavlja odmah, do 200 razgovora zaredom, umjesto po 5000 svakih pet minuta.

**Brava arhive u spremniku** — program je u spremniku uvijek proces 1, pa je
brava prekinute ugradnje paketa nakon ponovnog pokretanja izgledala živom i
zaustavila bi ugradnju arhive. Brava sad razlikuje dva života istog procesa.

**Osnivanje mreže uz zatečeni ključ** — datoteka ključa mreže bez zapisa u
bazi više ne zaustavlja osnivanje: ključ se preuzme, ne pregazi.

## 0.0.8-alfa — 2. 10. 2026.

**Novi čvor s praznom bazom se pokreće** — jednokratni popravak registra
(gradovi Ivanec i Vrbovec) padao je na praznoj bazi jer županija još nema dok
registar ne stigne razmjenom, pa se novi čvor u spremniku nije dao pokrenuti.
Sad se preskače; gradovi stižu razmjenom s čvora na kojem je popravak izveden.

## 0.0.7-alfa — 2. 10. 2026.

**Uloge čvora** — u Postavkama čvora (Uloge ovog čvora) uključuje se preuzima
li čvor vodostaje s izvora i izdaje li prognozu. Vodostaje dovoljno je da
preuzima jedan čvor: očitanja putuju razmjenom. Čvor od prije zadržava što je
radio; novi ne radi ni jedno dok mu se uloga ne uključi.

**Prognoza razmjenom** — svako izdanje (satno i dnevno, izbor rezervi, kiša po
međuslivovima i tuđe prognoze zadnjih 48 sati) ide u knjigu verzija, a
namješteni model kad se promijeni. Ostali čvorovi izdanje upišu u svoju bazu
prognoza i prikazuju ga s oznakom čvora koji ga je izdao; kiša po slivovima na
naslovnoj kod njih dolazi iz izdanja. U razmjeni izdanja stoje sedam dana (oko
100 kB po izdanju), u bazi prognoza ostaju.

**Arhiva razmjenom** — kazalo .cop paketa (letva, izdanje, razdoblje, veličina)
drže svi čvorovi; sam paket dohvaća čvor kojemu ga pokriva pretplata (nova
vrsta „Arhiva vodostaja”, po području letve) ili koji prati sve, od bilo kojeg
čvora koji ga ima, i ugradi ga nakon provjere otiska. Starije izdanje se ne
ugrađuje, a letva sagrađena na samom čvoru se ne gazi.

**Slika za spremnik** — `ghcr.io/tkraljevic/gocop` gradi se za svako izdanje;
`/data` za bazu i postavke, `/arhiva` za arhivu, stablo, skenove i pakete.

**Izvoz prognoze** — redak „model” (satni/dnevni) skriven je u grupi i otkriva
se gumbom „+” uz rub. Gornja Radgona maknuta je s uzdužnog profila Mure jer za
nju nema prognoze.

## 0.0.6-alfa — 1. 10. 2026.

**Vukovar sa svojim protokom** — iz HIS-2000 uvezeni su protoci Vukovara
2001.–2025. (satni i, prvi put, dnevni; dotad satni samo do 2018.) i krivulja
2025.–2026. od −100 cm (dotad −80), pa sažetak više ne uzima protok Iloka;
godišnji protoci Vukovara imaju i 2019.–2025. Izdan je `vukovar_v6.cop`.

## 0.0.5-alfa — 1. 10. 2026.

**Čitljivost izvoza** — usporedba s mađarskom prognozom na sažetku dobiva
zaglavlje u dva retka (termin preko triju stupaca, ispod naša · HU · razlika),
a razlika je pravi broj s predznakom iz formata ćelije (bez Excelova upozorenja
„broj kao tekst”); provjera na poplavnim valovima na listu „O prognozi” ima
naslov skupine preko cijele širine (rijeka, model, broj valova) umjesto
odrezanog naziva u uskom stupcu; napomene uz godišnje vodostaje i protoke
dobivaju zalihu visine na uskim listovima.

## 0.0.4-alfa — 1. 10. 2026.

**Sažetak prognoze u Excelu** — novi prvi list „Sažetak” za čitatelje kojima je
puni izvoz previše, A4 položeno: rečenice „Ukratko” koje program sam slaže iz
brojki (rijeka pada, raste ili je stabilna, najveća promjena s protokom, mogući
novi najniži ili najviši zabilježeni vodostaj, pragovi obrane, kiša po
slivovima, razlike prema mađarskoj prognozi od 10 cm naviše), tablica naših
postaja s vodostajem i protokom za sada, sutra, za 3 dana i zadnji dan
prognoze, kretanjem, stanjem obrane i napomenom, te usporedba s mađarskom
prognozom. Protok Vukovara u sažetku je protok Iloka (mjerenih protoka
Vukovara nema od 2019.), označen kurzivom. Najniži i najviši zabilježeni
vodostaj čitaju se iz arhive prije sažetka, da odmah nakon pokretanja ne
stoje samo ovogodišnji.

**Prelamanje teksta i visina redaka u izvozu** — podnaslov svakog lista,
odlomci i tablice lista „O prognozi” te napomene uz godišnje vodostaje i
protoke dobivaju visinu prema duljini teksta (Excel spojenim ćelijama visinu
ne prilagodi sam), pa se tekst ne reže.

**Protok dnevnog modela** — ostaje i kad granica raspona ispadne iz krivulje
protoka (Vukovar 5. i 6. dan).

## 0.0.3-alfa — 1. 10. 2026.

**Ispravak brojki na naslovnoj (Podaci u sustavu)** — pravi kišomjeri broje se
kao različite postaje (109, prije 107: DHMZ-ove postaje sa samo satnim ili samo
dnevnim nizom brojale su se po većem nizu); popuna kratkih rupa pravih
kišomjera iz ERA5 više se ne broji kao oborina po slivovima nego stoji uz
kišomjere; oborina po slivovima navodi obje reanalize (ERA5 i CERRA); padeži
uz brojeve.

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
