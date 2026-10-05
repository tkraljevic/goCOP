# Privremeno imenovanje — pravila i nacrt rješenja

Odluke su od 5. 10. 2026. Dužnosti u programu provedene su za 0.0.35;
rješenje o privremenom imenovanju kao akt u programu dolazi poslije. Polazi
od [pravila uprave](INSTALACIJA.md#3-podaci-i-sigurnost) i
[stanja obrane iz akata](NACRT-STADIJI-OBRANE.md).

## Odluke (5. 10. 2026.)

1. Sve dužnosti osim privremenih su **stalne**: vrijede dok ih uprava ne
   izmijeni ili opozove. Mandat se ne ograničava trajanjem: popis ljudi po
   dionicama mijenja se u pravilu jednom godišnje, a netko dionicu ima
   godinama; imenovanja direktora vrijede dok ih administrator ne zamijeni.
2. Privremena je samo **privremena ispomoć (privremeno imenovanje)**, kad za
   obranu od poplava nedostaje ljudi.
3. Privremeno se imenuje **rukovoditelj ili rukovoditeljica**, odnosno
   **zamjenik ili zamjenica**, dionice ili branjenog područja.
4. Imenovanje vrijedi **dok na njegovim dionicama, odnosno u branjenom
   području, traje redovna ili izvanredna obrana** (i izvanredno stanje).
   Može imati i zadan datum; prestaje ono što dođe prvo.
5. **Ovlasti koje privremeni rukovoditelj dodijeli** (dužnosti uprave)
   istječu zajedno s njegovom dužnošću. To piše i u rješenju.

## Kako radi u programu

- **Obrazac zaduženja:** kvačica *Privremena ispomoć (privremeno
  imenovanje)*, ispod nje *Ističe prestankom redovne i izvanredne obrane* (za
  novo imenovanje uključeno), razlog i neobavezni *Vrijedi do*. Profil
  djelatnika pokazuje *Privremeno* i dokle vrijedi: datum, „dok traje redovna
  ili izvanredna obrana” ili „do opoziva”.
- **Zapis dužnosti:** `Rok` je zadani datum, `IsticeSObranom` veže imenovanje
  za obranu, a `OvisiO` je dužnost privremene uprave iz koje je dužnost
  dodijeljena. Stvarni istek ostaje u `ExpiresAt`, najraniji od zadanog
  datuma, kraja obrane i isteka dužnosti iz koje je dodijeljena. Ostatak
  programa i čvorovi starijeg izdanja gledaju samo `ExpiresAt`. Stalna
  dužnost nema ni istek ni ta polja, i kad ih obrazac pošalje.
- **Kraj obrane** (`models.KrajRedovneObrane`) čita se iz ovjerenih,
  neponištenih akata na dionicama dosega: upisanim dionicama, inače svim
  dionicama područja, odnosno sektora. Razdoblja u kojima je najviši stadij
  barem redovna obrana spajaju se preko dionica; imenovanje prestaje na kraju
  prvog razdoblja koje završava nakon dodjele. Pripremno stanje ga ne
  produljuje. Dok obrana traje, i dok još nije ni počela (imenovanje dano
  kad obrana dolazi), kraja nema.
- **Preračun isteka:** pri dodjeli i izmjeni, pri ovjeri i poništenju akta,
  pri opozivu dužnosti i u krugu svakih deset minuta, jer akti stižu i
  razmjenom. Promijenjen istek bilježi se u knjigu. Poništen akt o prestanku
  obrane vraća imenovanje.
- **Ovisnost:** dužnost uprave koju dodijeli privremena uprava ističe s njom.
  Opoziv izvora (ili brisanje računa) prekida je odmah. Već prošli istek se
  pritom ne prepisuje: trenutak opoziva nije zapisan, pa bi svaki čvor upisao
  svoj „sad” i prepisivali bi ga jedan drugome.
- **Izmjena** postojeće dužnosti iste uloge i dosega ne skraćuje ni ne
  produljuje njezin vijek: ovisnost i istek s obranom ostaju, a zadani datum
  ide najviše do kasnijeg od kraja uprave i dosadašnjeg datuma.
- **Terenske dužnosti** (bez uprave) ne vežu se za upravu koja ih dodijeli.
- Zatečene privremene dužnosti s datumom u isteku zadržavaju taj istek;
  obrazac izmjene nudi ga kao zadani datum.

## Rješenje o privremenom imenovanju (poslije, kao akt)

Prema rješenjima Sektora B (BP 34, lipanj 2024.):

- zaglavlje centra obrane od poplava sektora, mjesto i datum;
- osnova: „U skladu s odredbom članka XXIX. stavka 5. Državnog plana obrane
  od poplava (NN 84/10), a na osnovu ukazane potrebe, donosim sljedeće
  RJEŠENJE”;
- **Članak 1.** Za privremenog zamjenika rukovoditelja (zamjenicu,
  rukovoditelja, rukovoditeljicu) obrane od poplava na dionici broj …,
  odnosno u branjenom području …, imenujem [ime i zvanje];
- **Članak 2.** Imenovani preuzima poslove trenutkom uručenja rješenja;
- **Članak 3.** Imenovani obavlja sve poslove predviđene Državnim planom
  obrane od poplava i planom obrane dionice;
- **Članak 4.** Rješenje prestaje važiti prestankom mjera izvanredne i
  redovne obrane na dionici (u području), a najkasnije [datum], ako je zadan;
- **novi članak:** ovlasti koje imenovani dodijeli u programu goCOP u okviru
  uprave dionice, odnosno branjenog područja, prestaju zajedno s ovim
  imenovanjem (tekst još treba uskladiti);
- „O tome obavijest”, potpis rukovoditelja obrane od poplava sektora i žig.

U programu: ovjera rješenja stvara privremenu dužnost (uloga, doseg, datum,
istek s obranom), storno je opoziva, a broj akta stoji uz dužnost.

## Otvoreno

1. *Vrijedi do* sprema se kao ponoć UTC na početku tog dana, pa dužnost s
   datumom 15. 10. istječe 15. 10. u 2 h. Tako je bilo i dosad za sve dužnosti
   s rokom. Treba li „do 15. 10.” uključivati cijeli taj dan?
2. Čvor starijeg izdanja ne zna za zadani datum ni istek s obranom. Ako on
   izmijeni takvu dužnost, noviji čvor istek preračuna iz zapisanih polja.
   Privremena imenovanja uređivati na 0.0.35 ili novijem.
