# Plan razvoja

goCOP je u alfa fazi. Ova stranica kaže što dolazi, kojim redom i zašto. Kako
program radi danas, piše u dokumentu [Kako goCOP radi](kako-radi.md), a što
je već izašlo, u [popisu izmjena](../CHANGELOG.md).

Datumi nisu rokovi. Korak počne čim je gotov onaj o kojem ovisi, pa plan ne
navodi datume.

## Zašto ovim redom

- Prvo pouzdanost onoga što postoji, pa nove mogućnosti. Nova mogućnost na
  kodu koji se teško mijenja donosi greške i u starim dijelovima.
- Prvo ono što se naknadno teško popravlja. Oblik zapisa koji putuju
  mrežom poslije se mijenja samo uz prepravak svakog zapisa i svake
  razmjene, a način prijenosa može se zamijeniti kad god. Zato pravila
  razmjene idu prije novog puta preko interneta.
- Svaki korak mora biti upotrebljiv sam za sebe i ne smije zatvoriti put
  prema cilju.

## 1. Čitljiviji i pouzdaniji kod

Prvo se pišu testovi, a tek onda se složen kod dijeli na manje dijelove, bez
promjene ponašanja. Najvažniji dijelovi programa danas su gotovo potpuno
pokriveni testovima: razmjena, arhiva, prognoza, uvozi, prijava i ovlasti,
akti i stanje obrane. Najsloženiji dio, primjena podataka primljenih s
drugih računala, već je podijeljen. Slijede složeni dijelovi prognoze,
uvoza ugovora o održavanju i web stranica, a zatim veća pokrivenost
testovima cijelog koda, s ciljem od 85 %.

Zašto: kod mora biti čitljiv onome tko ga mijenja, inače svaka izmjena nosi
rizik. Automatska provjera ne pušta izmjenu koja pogoršava izmjereno stanje.
Cilj i sadašnje stanje su u dokumentu [Kvaliteta koda](CODE_QUALITY.md).

## 2. Pravila razmjene i WEB sync

Ovo je glavni posao sada, usporedo s prvim korakom.

Najprije pravila razmjene. Računala će na početku razgovora reći jedno
drugome što razumiju, pa novo izdanje može dodati novu vrstu poruke, a
starije je zanemari bez greške. Računalo koje ne drži stara očitanja više
ih neće dobivati svakom razmjenom iznova. Pločica razmjene već javlja
zapise koje program ne razumije. Nova poruka reći će jednostavnim riječima
koliko ih čeka nadogradnju, a uz drugo računalo koliko ih ono čuva. Program
će bilježiti poruke koje je odbio ili nije do kraja razumio, bez njihova
sadržaja. Dio je već
napravljen i izlazi sa sljedećim izdanjem: jedan neispravan zapis više ne
odbija cijeli primljeni paket.

Zatim WEB sync: izravna šifrirana veza dvaju računala preko interneta, i kad
su oba iza usmjerivača, kod kuće ili u uredu. Danas takva računala
razmjenjuju samo preko stalnog računala ili Tailscalea, pa podatak između
njih čeka krug na stalnom računalu. Uredsko računalo iza vatrozida
organizacije uz to danas ne može nitko nazvati izvana. Stalno računalo i
dalje će pomagati računalima da se nađu, ali kroz njega će ići samo kratke
poruke, ne podaci. Na usmjerivačima i vatrozidu organizacije ne otvara se
ništa, a na samom računalu možda treba dopustiti jedan UDP port. Pločica
razmjene reći će kojim je putem prošla zadnja razmjena i, kad put ne radi,
zašto. Kad stigne opoziv članstva, računalo će odmah zatvoriti veze s
opozvanim računalom.

WEB sync bit će zadano isključen. Zadano se uključuje tek zasebnom odlukom,
kad dva tjedna radi bez greške na svim probnim računalima i kad informatičari
organizacije dobiju njegov opis.

Zašto: razmjena je temelj svemu što radi na više računala. Pravila idu prva
jer WEB sync mijenja isti dio programa. WEB sync zatim daje put koji ne vodi
podatke kroz jedno računalo.

## 3. Proširenja

Kad je WEB sync gotov, dolaze nove mogućnosti:

- Prognoza po branjenim područjima i vodotocima. Model prognoze vodi se po
  branjenom području, a gleda se i po vodotoku. Administrator ga mijenja u
  postavkama, bez novog izdanja programa: od kojih se letvi računa koja
  letva, rezervne letve, računa li se letva u protoku ili u vodostaju,
  brzinu putovanja vala za nisku i za visoku vodu i ostalo što je danas
  upisano u program. Promjena se prije primjene isproba na arhivi.
- Ustave i crpne stanice. Ustava povezuje uzvodni i nizvodni vodostaj i
  bilježi je li zapornica otvorena ili zatvorena. Crpna stanica bilježi
  početak i kraj rada svakog agregata posebno. Strojar upisuje vodostaje
  na početku rada, tijekom rada i na kraju, a sve to ulazi u dnevnik
  strojara.
- Karte. Svaki objekt, od vodotoka do crpne stanice, ima svoje mjesto na
  karti. Modul Karte sve to povezuje u slojeve: vodotoci, nasipi i
  dionice, objekti, letve i obuhvati poplave iz karata opasnosti od
  poplava. Uz to karta bez interneta, iz vlastite zalihe pločica za
  područje, jer javni poslužitelj OpenStreetMapa ne dopušta skupno
  preuzimanje.
- Mobilna aplikacija za teren, kao pojednostavljen goCOP za mobitel: svima
  prognoza, stanje letvi i upis očitanja, vodočuvaru dnevni zadaci i dnevni
  list, rukovoditelju dionice dnevno izvješće. Dolazi tek kad je goCOP
  stabilan. Što će točno raditi i kako, još ne znamo; vjerojatno kao tanka
  aplikacija koja s čvorom razgovara preko sučelja (API).
- Akti: rješenje o privremenom imenovanju kao akt, čijom ovjerom privremena
  dužnost počinje, a stornom prestaje; QR kod na aktu za provjeru izvornika,
  kad program dobije javnu adresu; dopis županu uz proglašenje izvanrednog
  stanja.

## 4. Poliranje i unos podataka

Kad su proširenja gotova, slijedi poliranje onoga što postoji i unos
podataka.

## 5. Testna simulacija

Simulacija je odvojeni goCOP za probu i vježbu, s vlastitim bazama,
dokumentima i vlastitom mrežom. Arhivu vodostaja samo čita. Na svakoj
stranici, PDF-u i aktu piše „TESTNA SIMULACIJA”. Služi za probu novog
izdanja prije nadogradnje pravih računala i za vježbu osoblja na povijesnim
valovima: simulacija se vrati na početak nekog vala i krene s tadašnjim
vodostajima. Za to program mora znati raditi i s vremenom simulacije, a ne
samo sa stvarnim satom.

Zašto: da se može proći stvarna simulacija neke povijesne obrane od
poplava, uvježbati osoblje i isprobati sučelje, a da se pravi i probni
podaci ne miješaju, pa ni brojevi akata. Simulacija dolazi kad je sve
ispolirano. Ono što simulacija pokaže dorađuje se poslije nje.

## Prije bete

Ovi poslovi nisu vezani uz redoslijed koraka 1–5, ali svi moraju biti
gotovi prije nego program izađe iz alfa faze:

- Uredno gašenje. Program pri gašenju danas samo kratko pričeka. Treba
  pričekati da započeti zahtjevi i upisi završe.
- Testove i na Windowsu i na macOS-u. Danas se automatski testovi pokreću
  samo na Linuxu, a programi za Windows i macOS samo se grade, iako
  računala u mreži rade i na Windowsu.
- Pravila prijelaza baze između izdanja. Dok traje alfa, oblik baze se
  slobodno mijenja. Za betu treba pravilo što se smije promijeniti i kako se
  baza prevodi na novo izdanje.
- Probu obnove iz sigurnosne kopije na čistom Windows računalu.
- Potpis programa za Windows. Prijava je poslana i čeka odgovor; kako će
  potpis raditi, piše u [pravilima potpisivanja](CODE_SIGNING.md).

## Poslije: pouzdanost podataka u mreži

Ovo je smjer, bez redoslijeda unutar popisa:

- Stanje podatka uz vrijednost: odakle je, kad je izmjeren, koliko je star i
  je li sporan. „Vodostaj je 521 cm” navodi na odluku, a „posljednji poznati
  vodostaj je 521 cm, ali podatak je star 47 minuta” na provjeru. Stanje se
  računa pri prikazu, a ne sprema, jer spremljeno stanje tiho zastari. Prag
  starosti ovisi o nizu: 47 minuta uredno je za dnevno očitanje, a previše
  za satno za vrijeme vala.
- Sukobi: kad dva izvora daju različitu vrijednost za isto mjerenje, čuvaju
  se obje i sukob se označi. Tiho prepisan prag u obrani je operativni
  problem.
- Potpisani zapisi i poništavanje po računalu i vremenu. Kad se otkrije da
  je računalo bilo zaraženo ili preuzeto, mora se moći reći „sve što je ono
  upisalo od tog trenutka je sumnjivo” i to ukloniti bez ručnog čišćenja
  svake baze.
- Ovlast za izdavanje prognoze i arhive, potpisana kao članstvo, da drugo
  računalo zna da izdanje dolazi od računala koje ga smije izdati.
- Trenutačan opoziv. Danas opoziv vrijedi na drugom računalu tek kad mu
  stigne razmjenom.

## Poslije: službeni zapisi i arhiva

- Zajedničko kazalo svih službenih zapisa, s pravilima roka čuvanja.
- Prijenos velikih datoteka po dijelovima, s nastavkom nakon prekida, i
  dohvat dokumenta na zahtjev. Danas se dokument prenosi cijeli, a u jednoj
  razmjeni ide najviše 48 MB.
- Računalo ne otpušta svoje dokumente dok ih ne drži računalo određeno da
  ih čuva.
- Brisanje uloženih očitanja na svim računalima. Smije se tek kad svako
  računalo određeno da čuva arhivu potvrdi da drži paket arhive koji ta
  očitanja sadrži. Brisanje je zapis koji putuje, inače bi ih drugo računalo pri
  idućoj razmjeni tiho vratilo. Svako računalo prije brisanja samo provjeri
  svoju arhivu.
- Umirovljenje računala. Odluku donosi čovjek, a ne istek vremena, jer bi
  automatsko izbacivanje nakon duge tišine izbacilo i računalo djelatnika
  koji je bio na bolovanju. Prije umirovljenja program kaže što se gubi:
  kad je računalo zadnji put razmijenilo, što drži i ima li zapisa koje
  nitko drugi nema. Računalo koje se vrati prima se iznova, kao novo.
  Program na vrijeme upozori na računalo koje dugo ne potvrđuje paket
  arhive, jer bi zbog njega brisanje stalo.

## Kasnije, kad mreža naraste

- Uži opseg prije prvog vanjskog sudionika. Kad u mrežu uđu izvođači,
  općine i službe, svako računalo smije dobiti samo svoj dio podataka. Lakše
  je suziti prije nego oduzeti poslije.
- Ulazak novog računala u mrežu bez putovanja do njega, za udaljene
  lokacije. To traži zasebnu odluku.
- Razmjena s ograničenim brojem susjeda, kad u mreži budu deseci računala.
- SERVER sync i preko drugih poslužitelja organizacije.
- Lakša instalacija: brže namještanje novog računala, paketi za Linux i
  instalacijski paket za macOS. Pojedinosti su u opisu
  [Postave](postava.md).

## Kako radimo

- Svaka izmjena prolazi pregled i automatske provjere prije nego što uđe u
  program. Provjera ne pušta izmjenu koja pogoršava izmjereno stanje koda.
- Izdanje izlazi kad su sve provjere zelene. Uvjeti zapisani za izdanje
  0.0.35 traže i ovo: nakon nadogradnje sva stalna računala i računalo koje
  izdaje prognozu pokazuju isto stanje, isti broj zapisa, ista upozorenja uz
  istu prognozu i razmjenu bez grešaka, a nijedan podatak ne nestaje tiho.
- Nadograđuje se redom: najprije stalno računalo, odmah zatim ostala. Kad
  izdanje traži da sva računala budu na istoj inačici, sva se nadograđuju
  odmah.
- Odluke na koje se plan oslanja zapisane su po datumu u dokumentu
  [Odluke](odluke.md).
