# Kako goCOP radi

Ova stranica je za čitatelja izvana: informatičara, kolegu iz druge službe
ili nekoga tko procjenjuje program. Kaže kako je goCOP zamišljen i zašto.
Kako se program koristi, piše u samom programu, na stranici **Pomoć**. Kako
se postavlja, piše u [uputama administratoru](INSTALACIJA.md).

Program je u alfa fazi. Ovdje je opisano ono što radi danas, u izdanju
0.0.39-alfa. Što još dolazi, piše u [planu razvoja](plan-razvoja.md).

## Što je goCOP

goCOP je program za obranu od poplava. Na jednom mjestu drži teren i
očitanja letvi, arhivu vodostaja i prognozu, dnevnike, akte o stupnjevima
obrane, registre, ljude i sredstva.

To je jedan program s ugrađenim web sučeljem. Radi na računalu, a otvara se
u pregledniku, na tom računalu ili s drugog u istoj mreži. Uz njega ne treba
poseban poslužitelj baze ni oblak.

## Svako računalo ima svoje podatke

Svako računalo s goCOP-om (u programu se zove čvor) drži svoju kopiju
podataka, u vlastitim bazama na svom disku. Zato radi i bez interneta:
unos, pregled, dnevnici, akti i dokumenti rade lokalno. Mreža treba samo za
ono što dolazi izvana: nova očitanja s javnih izvora, vrijeme, karte,
e-poštu i razmjenu s drugim računalima.

Izmjena ne prepisuje stari zapis. Svaka izmjena ostaje kao nova verzija
zapisa, s računalom na kojem je nastala i vremenom, a starije verzije
ostaju u povijesti zapisa, dok ih administrator ne sažme na stranici
Održavanje baze. U programu se ta povijest zove knjiga verzija.
Kad se dva računala spoje, svako drugome pošalje verzije koje ono još nema.
Ponovljena razmjena ništa ne kvari: verzija koju računalo već ima samo se
preskoči.

Kad dva računala bez veze izmijene isti zapis, nakon razmjene oba prikazuju
noviju izmjenu, a starija ostaje u povijesti. Program takav sukob zasad ne
označava. To je u planu razvoja.

Računala ne moraju imati isto izdanje programa. Zapis koji starije izdanje
ne razumije, ono čuva i šalje dalje, a primijeni ga nakon nadogradnje. Kad
neko izdanje traži da sva računala budu na istoj inačici, to piše uz izdanje
u [popisu izmjena](../CHANGELOG.md).

## Kako se računala usklađuju

Računala razmjenjuju podatke samo s članovima iste mreže. Veza je šifrirana
ključevima samih računala, pa sadržaj ne vidi ni web tunel ni mreža kojom
podaci putuju. Postoje četiri puta:

| put | kako ide | stanje |
|---|---|---|
| LAN sync | računala su u istoj lokalnoj mreži, u uredu ili kod kuće, i nađu se sama | radi |
| SERVER sync | preko stalnog računala koje je dostupno s interneta, na njegovoj javnoj adresi ili kroz web tunel (HTTPS); ono je član mreže, prima podatke i šalje ih dalje | radi |
| Tailscale | oba računala su u istoj Tailscale mreži | radi od 0.0.37-alfa; u postavkama se može isključiti |
| WEB sync | izravna veza dvaju računala preko interneta, i kad su oba iza usmjerivača | u izradi; bit će zadano isključen |

Program uvijek prvo pokuša lokalnu mrežu. Razmjena krene sama nakon
pokretanja i ponavlja se svakih pet minuta. Na naslovnoj stranici pločica
„Razmjena s čvorovima” pokazuje zadnju razmjenu i napredak arhive.

Javnu adresu stalnog računala administrator upiše jednom, na bilo kojem
računalu mreže, a ostala je dobiju razmjenom. Dva računala koja se ne vide
izravno razmjenjuju preko njega, pa podatak između njih čeka do idućeg kruga
razmjene, najviše desetak minuta. WEB sync to skraćuje: stalno računalo
pomogne dvama računalima da se nađu, kratkim porukama, a podaci zatim idu
izravno. Razmjena nikad neće čekati WEB sync. Dok se veza priprema, krug ide
putem koji radi.

Kad mreže nema dulje vrijeme, odabrani podaci prenose se i bez nje. Jedno
računalo izda potpisan paket (Administracija → Održavanje baze → Izdaj
.cop), a drugo ga ugradi, na primjer s USB ključa. Paket prolazi ista
pravila kao razmjena.

### Što koje računalo dobiva

Ustroj, registri i djelatnici zajednički su i dolaze na svako računalo.
Ostalo računalo prati po pravilima na stranici „Što ovo računalo prati”:
očitanja, dnevnike, prijave s terena, vodočuvarski dnevnik i arhivu
vodostaja, po sektoru ili branjenom području i po godinama. Uz zapise bira
i koliko sadržaja dohvaća: sve, samo slike bez PDF-ova ili samo popis
dokumenata. Bira i koliko dana sadržaj ostaje na računalu. Uredsko računalo
obično prati sve, a prijenosno samo svoje područje i zadnje godine.

Granica je u razmjeni, ne u prikazu. Računalo ne dobiva ono što ne prati,
pa izgubljeno prijenosno računalo nosi samo zajedničke registre i svoj dio
ostalih podataka.

### Kako računala razgovaraju

Kad se dva računala spoje, najprije dokažu tko su. Svako pokaže svoj ključ
i potpisano članstvo u mreži, a s računalom koje nije član razgovor ni ne
počne. Zatim jedno drugome kažu dokle imaju podatke od svakog računala koje
ih je upisalo, a očitanja, dnevnike i ostalo što se prati po izboru još i
po području i godini. Uz to kažu što prate, koje izdanje programa imaju i
što od razmjene znaju.

Iz toga svako računalo samo vidi što drugome nedostaje i pošalje samo to:
novije verzije od onih koje drugo ima, i to samo ono što drugo prati. Šalje
i ono što je dobilo od trećih, pa podatak stigne i preko računala koje ga
nije upisalo. Računalo koje ne čuva stara tuđa očitanja javi dokle ih je
odbacilo, pa mu ih drugo više ne šalje svaki put iznova. Kad računalu
nešto možda nedostaje, jer je podatke dobilo preko računala koje nije
čuvalo sve, javi i to, pa mu taj dio pošalje računalo koje ga ima.
Računalo zatim upiše ono što je primilo i zatraži dokumente i slike uz
zapise koje prati, a na kraju oba potvrde koliko su upisala. Jedan razgovor
nosi ograničenu količinu. Kad ima više, odmah ide sljedeći.

Kad računala nemaju isto izdanje programa, novi dio razgovora ide samo ako
ga znaju oba. Novi dio poruke koji starije izdanje ne poznaje ono zanemari,
pa s njim razmjena ide kao i prije, a novo počne raditi kad se i ono
nadogradi. Zapise koje ne razumije ne baca, nego ih čuva i šalje dalje.
Kad računalo odbije razgovor, na primjer jer je zauzeto, javi razlog, a
razmjena se ponovi malo kasnije. Računalo koje redom odbija ili ne odgovara
zove se sve rjeđe. Pločica „Razmjena s čvorovima” pokazuje koliko je
stiglo zapisa koje ovaj program još ne razumije. Za svako drugo računalo
koje to javi pokazuje i koliko zapisa kod njega čeka nadogradnju, ako ih
ima.

## Zašto nijedan poslužitelj nije nužan

Obrana od poplava ne smije stati zato što je nešto palo: internet,
poslužitelj, domena organizacije ili struja u jednoj zgradi. Zato vrijedi
osnovno načelo:

> Nijedan pojedinačni servis, poslužitelj ni mrežna lokacija ne smije biti
> nužan za nastavak rada i za pristup posljednjim pouzdanim podacima.

Mreža računala nije tu da podaci putuju brže, nego da rad ne stane kad dio
nestane. Manje prijenosa i arhiva na više mjesta usputna su korist.

Stalno računalo je poželjan put, ali nikad nužan. Kad bi neka radnja tražila
da je ono dostupno, na primjer kad bi se korisnik morao prijaviti preko
uredskog računala, opet bi nastao središnji poslužitelj, samo zaobilaznim
putem. Provjera je jednostavna i treba je ponavljati: ugasite stalno
računalo i pogledajte rade li dva prijenosna računala u istoj prostoriji
jedno s drugim, bez ičega u sredini.

Otpornost dolazi od raznolikosti, a ne od broja računala. Deset računala na
istoj domeni, s istim ažuriranjima i istim pristupnim podacima, u praksi su
jedno: kad padne domena, padnu sva. Zato vrijedi imati računala koja
otkazuju iz različitih razloga, na drugoj lokaciji, s drugim pristupom
internetu ili izvan domene organizacije.

Mreža štiti od ispada, ali ne i od napada. Ako napadač uđe u jedno
računalo, mreža će njegove izmjene savjesno raznijeti na sva ostala. Zato
je ulaz u mrežu zatvoren uparivanjem i potpisanim članstvom, a uz mrežu
treba i sigurnosna kopija u koju mreža ne može pisati. Mreža daje kopije u
prostoru, a sigurnosna kopija u vremenu. Od pokvarenih zapisa pomaže samo
kopija od prije napada. Kako se takva kopija radi, piše u
[uputama administratoru](INSTALACIJA.md#kopija-u-koju-mreža-ne-piše).
Potpisani zapisi i poništavanje svega što je jedno računalo upisalo od
nekog trenutka u planu su razvoja.

## Uloga računala

U mreži nema glavnog računala. Uloga računala proizlazi iz onoga što ono
jest i radi, a ne iz postavke. Računalo u uredu koje je stalno upaljeno i
dostupno s interneta postaje put za druga. Prijenosno računalo na terenu
prati samo svoje područje i razmjenjuje kad ima veze.

Dvije uloge danas se ipak uključuju ručno, na stranici Administracija →
Čvor, mreža i sinkronizacija, pod Uloge ovog čvora: „Preuzima vodostaje s
izvora” i „Izdaje prognozu”. Svaka se uključuje na samo jednom računalu.
Tako izvore ne pita svako računalo, a za isti sat ne nastanu dvije
različite prognoze. Kad to
računalo ne radi, vodostaji s izvora i prognoza ne obnavljaju se dok
globalni administrator ne uključi ulogu na drugom. Samostalno prebacivanje
uloge na drugo računalo još ne postoji.

## Tko smije u mrežu

Mrežu osniva prvo računalo i drži njezin ključ. Novo računalo ulazi u mrežu
uparivanjem s računalom koje smije primati članove: s onim koje drži ključ
mreže ili s onim kojem je ono dalo ovlast za primanje. Uparivanje traži
čovjeka na oba ekrana. Oba računala pokažu isti šesteroznamenkasti kod i
oba ga potvrde.

Računalo koje nije u istoj lokalnoj mreži s računalom koje ga prima ulazi
primanjem na daljinu. Novo računalo pokaže kod za primanje i napravi
datoteku zahtjeva. Datoteka ide administratoru koji ga prima, na primjer
e-poštom, a kod mu čovjek javi telefonom. Administrator vrati datoteku
potvrde, a novo računalo je učita i time ulazi u mrežu. Primanje na daljinu ostaje i u beti (Odluke (19. 10. 2026.)).

Primljeno računalo dobije potpisano članstvo, koje vrijedi godinu dana. Pri
svakoj vezi računalo dokazuje svoj ključ, a računalo bez važećeg članstva
odbija se. Zato novo računalo ne treba upariti sa svakim članom posebno.
Opoziv članstva je potpisan i putuje razmjenom, pa na drugom računalu vrijedi
kad ondje stigne. Pojedinosti i granice zaštite su u
[SECURITY.md](../SECURITY.md).

Što koja osoba smije u programu, određuju njezine dužnosti. Uloge su
opisane u dokumentu [Tko što radi u programu](uloge.md).

## Arhiva vodostaja

Arhiva vodostaja odvojena je od operative. Operativna baza uz svaki zapis
nosi i njegovu povijest, jer se razmjenjuje. U mjerenju iz rujna 2026. jedno
očitanje ondje je zauzimalo oko 1.360 bajtova, a u arhivi oko 20. Sama Batina,
s 45.547 očitanja, zauzimala je 61 % glavne baze. Cijela povijest Dunava i
Drave bila bi u tom obliku oko šest gigabajta, koje bi svako računalo
moralo prenijeti i držati.

U arhivi su povijesni nizovi: službeni podaci, telemetrija, nizovi
susjednih država i nizovi produljeni iz susjednih postaja, uz protok i
temperaturu vode gdje ih ima. Arhiva ne putuje razmjenom zapis po zapis.
Računalo koje drži izvorne nizove izda potpisan paket za svaku letvu, a
razmjenom putuje samo kazalo: letva, inačica paketa, razdoblje i veličina.
Paket dohvaća računalo koje arhivu prati, od bilo kojeg računala koje ga
ima, i ugradi ga tek kad ga provjeri. Novija inačica paketa smije
promijeniti i stare godine, jer službeni podaci stižu ovjereni i godinu do
dvije kasnije. Nenamjerni povratak na stariju inačicu primatelj odbija.

Operativna očitanja zatvorene godine administrator može uložiti u arhivu.
Iz operativne baze smije ih ukloniti tek kad program provjeri da je svaka
vrijednost u arhivi spremljena točno.

## Prognoza

Prognozu računa jedno računalo, ono s ulogom „Izdaje prognozu”, svaki sat.
Vrijednost na letvi za sat koji dolazi računa se iz uzvodnih letvi, a
njihova iz letvi još uzvodnije. Koliko daleko unaprijed prognoza seže,
ovisi o tome koliko je taj lanac letvi dug. Dnevna prognoza uzima u obzir i
kišu na slivu. Uz svaku vrijednost ide raspon: program prognozu računa i za
prošle vode iz arhive i mjeri koliko je promašila. Prognoza stiže ostalim
računalima razmjenom, pa je ne računa svako samo. Prognoza je pomoć stručnoj procjeni. Ne zamjenjuje
službene prognoze ni odluke odgovornih osoba.

## Od nacrta do službenog zapisa

Dokument u goCOP-u prolazi ovaj put:

`nacrt → objava ili ovjera → službeni zapis → trajno čuvanje ili izlučivanje nakon roka`

Nacrt se smije mijenjati i brisati. Objava ili ovjera zaključa sadržaj i
priloge, i od tog trenutka zapis je službeni. Službeni zapis se više ne
otvara za uređivanje. Ispravak nastaje kao novi povezani zapis, a izvorni
ostaje čitljiv i označen kao zamijenjen ili poništen. Arhivska građa čuva se
trajno, a drugi službeni zapisi mogu imati propisani rok čuvanja. Tada se ne
brišu obično, nego zapisanom odlukom.

Tako danas rade akti, ovjereni dnevni listovi, dnevnici COP-a i objavljene
prijave s terena. Pri ovjeri ili objavi nastaje izvornik, konačan PDF ili
sken potpisanog akta. Sprema se jednom, a istu datoteku program prepoznaje
po otisku sadržaja (SHA-256). Razmjenom putuje taj otisak, a sam izvornik
dohvaća računalo koje ga prati. Zajedničko kazalo svih službenih zapisa i pravila roka čuvanja još
nisu napravljeni.

Stanje obrane dionice nitko ne upisuje ručno. Program ga računa iz
ovjerenih akata o stupnjevima obrane, prema vremenu njihova stupanja na
snagu. Poništen akt ostaje u evidenciji, s razlogom, ali više ne određuje
stanje. Pravila su opisana u dokumentu [Stadiji obrane](stadiji-obrane.md).

## Što još nije gotovo

Još nisu napravljeni WEB sync, oznaka sukoba i starosti podatka,
trenutačan opoziv, zajedničko kazalo službenih zapisa, dogovoreno brisanje
starih podataka na svim računalima i umirovljenje računala. Kojim redom
dolaze i zašto, piše u [planu razvoja](plan-razvoja.md).
