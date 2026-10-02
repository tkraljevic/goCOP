# Popis izmjena

Verzije prate shemu iz README-a: alfa `0.0.x` (oznaka `v0.0.1-alfa`), beta
`0.y.x` od `0.1.0` (`v0.1.0-beta`), stabilno `z.y.x` od `1.0.0` (`v1.0.0`).
Alfa traje dok se ne zaokruže funkcionalnosti koje program treba imati.

## 0.0.19-alfa — 2. 10. 2026.

**Kazalo arhive bez nadglasavanja** — čvor objavljuje paket arhive samo kad je
njegovo izdanje novije od onoga što kazalo već ima. Kad je stalni čvor kao novi
izdavač izdao novija izdanja kišomjera, laptop je svake dvije minute ponovno
objavljivao svoja starija, a stalni čvor svoja: knjiga je rasla po 1.200
zapisa na sat, a pločica je stalno javljala „šalje se još 40 verzija”.

**„Javlja se sam” umjesto „ne odgovara”** — čvor s nedavnom uspješnom
razmjenom prikazuje se kao na mreži i kad ga ovaj čvor ne može nazvati (laptop
izvan kuće sam zove kroz tunel), uz tu napomenu umjesto greške mreže.

## 0.0.18-alfa — 2. 10. 2026.

**Izvoz prognoze uvijek svjež** — poveznice „Izvoz u Excel” i „Pričuvni
izračun” pri svakom učitavanju stranice dobivaju novu adresu, pa preglednik
ne može dati staru spremljenu datoteku (kopija spremljena dok je Cloudflare
slao „čuvaj 4 sata” inače bi i dalje stizala iz preglednika).

## 0.0.17-alfa — 2. 10. 2026.

**Čvor se pokreće bez obzira na redoslijed zaduženja** — pri pokretanju se
zaduženja prekodiraju na stalne identifikatore „korisnik + redni broj”, a broj
se uzimao iz redoslijeda redaka u lokalnoj bazi. Razmjena upiše zaduženja kako
stignu, pa je na drugom čvoru redoslijed bio obrnut: prekodiranje je zamijenilo
dva zaduženja, palo na jedinstvenosti i čvor se nije dao pokrenuti. Zaduženje
koje već nosi stalni identifikator svog korisnika sad ostaje kakvo jest, a
staro nasumično dobije prvi slobodan broj.

## 0.0.16-alfa — 2. 10. 2026.

**Ništa u priručnu memoriju posrednika** — svaki odgovor osim `/static/` nosi
`Cache-Control: private, no-store`. Cloudflare je izvoz prognoze (`.xlsx`)
prema nastavku čuvao do 4 sata i davao ga svakome, i bez prijave: drugi
korisnik dobivao je staru prognozu, a izvoz se dao preuzeti bez računa. Uz
ovo je na Cloudflareu dodano pravilo da se za cop-osijek.com ništa ne sprema.

## 0.0.15-alfa — 2. 10. 2026.

**Upozorenja DHMZ-a pregledno** — zeleno upozorenje (DHMZ-ovo „nema
upozorenja”) više se ne prikazuje kao upozorenje: umjesto sedam istih kartica
stoji jedan redak „županija: nema upozorenja” s razdobljem, a „drugdje u
Hrvatskoj” broji samo prava upozorenja. Redak unutar kartice nosio je klasu
crvenog upozorenja (ružičasta traka u svakoj kartici), a obojena upozorenja
dobila su tamnu temu, pa su čitljiva.

## 0.0.14-alfa — 2. 10. 2026.

**Letva razmjenom sa svim poljima** — primljena letva nije upisivala šest
polja koja zapis nosi: uvoz s Geolux HydroViewa (`telemetrija_site`,
`telemetrija_uvoz`), ograde niza, povijest, opis vodokaza i datum osnivanja.
Stalni čvor zato nije preuzimao 31 HydroView letvu, među njima dva vrha lanca
(Beničanci – Prkos, Kapelna), i prognoza je stala. Čvor pri pokretanju letve
obnovi iz knjige, pa se polja popune sama. Novi test prenosi letvu sa svim
poljima s čvora na čvor i pada čim letva dobije polje koje razmjena ne prenosi.

## 0.0.13-alfa — 2. 10. 2026.

**Razmjena kroz tunel bez rušenja** — WebSocket na strani poslužitelja nema
adresu druge strane, a bilješka o razmjeni ju je ispisivala: prva veza kroz
tunel srušila je čvor. Veza kroz tunel sad ima svoju adresu, a greška u jednoj
vezi razmjene više ne može srušiti cijeli čvor — veza se zatvori i zapiše.

## 0.0.12-alfa — 2. 10. 2026.

**Razmjena kroz web tunel** — čvor koji izvana smije samo na web (Cloudflare
tunel, bez otvorenog porta) prima razmjenu i na `https://<adresa>/razmjena/tunel`,
kroz WebSocket. Unutra teče isti TLS s ključevima čvorova kao na portu razmjene:
nepoznati ključ ne dobiva ni bajt, a tunel vidi samo šifrirane bajtove. Druga
strana se onamo spaja kad je adresa čvora `https://domena` (Postavke →
Domenski čvorovi); u istoj mreži prvo se pokušava port razmjene.

**Razmjena na naslovnoj** — pločica „Razmjena s čvorovima”: za svaki upareni
čvor zadnja razmjena, primljeno i poslano i koliko se još šalje ili prima;
napredak arhive (ugrađeno od koliko paketa, što se upravo ugrađuje, koliko se
još dohvaća); sadržaj koji čeka dohvat; tko izdaje prognozu i kad je stiglo
zadnje izdanje, s upozorenjem kad je starije od tri sata. Kad je sve usklađeno,
jedan redak. Osvježava se sama.

## 0.0.11-alfa — 2. 10. 2026.

**Kiša razmjenom** — uz svako izdanje prognoze idu mjerenja kišomjera zadnjih
48 sati i satna kiša Open-Meteo po točkama slivova s prognozom (oko 80 kB po
izdanju). Čvor koji ne izdaje prognozu kišu upiše u svoju bazu oborina, pa i
on ima svježa očitanja kišomjera, ne samo arhivu.

**Arhiva kiše raste sama** — nakon noćnog ulaganja kiše izdavač odmah izda
pakete promijenjenih kišomjera i kazalo pošalje razmjenom, bez ručnog
izdavanja arhive.

**Izdavanje nastavlja primljeni niz** — čvor koji je letvu primio paketom, a
sam je još nije izdavao, nastavlja broj izdanja onoga od koga ju je primio:
isti sadržaj zadrži broj, novi dobije sljedeći. Inače bi čvor koji preuzme
izdavanje krenuo od v1, a ostali bi ga odbili kao starije izdanje.

## 0.0.10-alfa — 2. 10. 2026.

**Kiša se ne ulaže bez stabla izvornih datoteka** — čvor koji izdaje prognozu
jednom dnevno ulaže izmjerenu kišu u arhivu i letvu iznova gradi iz stabla.
Čvor koji je arhivu dobio paketima stabla nema, pa bi gradnja meteorološkim
postajama povijest svela na zadnja četiri dana. Bez stabla se ulaganje sad
preskače i javlja u ispisu kruga.

**Telemetrija jasnije** — računi nose naziv sustava i adresu (Geolux HydroView,
hdv.voda.hr; mobilna stranica Hrvatskih voda, mletva.voda.hr), a stranica kaže
treba li ih ovaj čvor: računi trebaju samo čvoru koji preuzima vodostaje.

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
