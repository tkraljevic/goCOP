# Plan testiranja mreže čvorova (kućni ispitni poligon)

Prijedlog, 3. 10. 2026. Služi kao stalni ispit za razmjenu, pretplate, a
kasnije i za protokol povezivanja (0.0.29–0.0.33) i Postavu.

## 1. Postava

| čvor | gdje | mreža | pretplata | uloga u probi |
|---|---|---|---|---|
| **U** Unraid (`cop-osijek-unraid`) | kuća, Docker | LAN + tunel `cop-osijek.com` | sve (`[sync] sve = true`) | sastajalište, jedini dohvatljiv izvana |
| **L** laptop (macOS) | kuća ili hotspot mobitela | LAN / mobilna mreža | kako je sada | putujući čvor |
| **W** Windows PC (Intel, 64-bit) | kuća | LAN | **uska**, `[sync] sve = false` | ograničeni čvor, kasnije Postava |

**Početna pretplata na W:** očitanja jednog područja za 2026., razina
„pregled” (samo slike), sadržaj 30 dana. Ništa drugo osim zajedničkog
kanala (ustroj, registri, djelatnici), koji drži svaki čvor.

**Prije početka:**

- W je osobno računalo, ne zajedničko, po mogućnosti s BitLockerom. Na njega
  dolaze djelatnici, s osobnim podacima i zapisima lozinki.
- U Windowsima kućna mreža mora biti označena kao *privatna*. Na pitanje
  vatrozida pri prvom pokretanju dopustiti samo privatne mreže.
- W se uparuje iz kućne mreže. Primanje u mrežu odobrava administrator.
- Prvi prijenos na W ide kroz LAN, ne kroz hotspot.

## 2. Mjere uspjeha

Za svaku probu se bilježi:

- vrijeme od upisa na jednom čvoru do pojave na drugom;
- put: LAN, tunel ili preko U (pločica čvora);
- broj zapisa i veličina sadržaja na W prije i poslije;
- greške u dnevniku sva tri čvora.

Rezultati idu u tablicu u §5.

## 3. Probe (0.0.27, bez novog koda)

**P1 Uparivanje i prvi prijenos (sve doma)**

- W uparen, primljen u mrežu, na pločicama U i L vidi se izdanje.
- W ima samo zajednički kanal i pretplaćeni kanal. *Baza* na W ne smije
  pokazati druga područja ni godine.
- Prilozi na W su samo slike; PDF-ova nema.

**P2 Otkrivanje u LAN-u**

- L i W se nađu sami (UDP 4712) i sinkroniziraju izravno, ne preko U.
- Zapis na L u pretplaćenom području stigne na W unutar razmaka automatske
  sinkronizacije (zadano 5 min).

**P3 Zapis izvan pretplate**

- Na L upisati očitanje u drugom području: na U stiže, na W ne.
- Na W se kanal ne pojavljuje ni kao prazan.

**P4 Laptop na hotspotu mobitela**

- L spojen na hotspot, izvan kućne mreže: razmjena ide kroz tunel prema U.
- Zapis na L stiže na W preko U (L → U → W) i obrnuto. Bilježi se kašnjenje.
- Pritom na L pokrenuti `tailscale netcheck` (UDP, `MappingVariesByDestIP`,
  PortMapping) i zapisati rezultat: NAT mobilne mreže. Kućna strana izmjerena
  je 3. 10.: lak NAT, bez UPnP-a.
- Pratiti potrošnju podataka na mobitelu za jedan sat rada.

**P5 Sužavanje pretplate i brisanje**

- Na W maknuti pretplatu; *Baza* pokazuje kanal kao „nije pod pretplatom”.
- Brisanje nepretplaćenog: kanal nestaje, prilozi nestaju sa diska.
- Ponovno dodana pretplata: sljedeća razmjena vraća kanal.

**P6 Istek priloga**

- Pretplata s „sadržaj 1 dan”: nakon isteka slike nestaju s W, zapisi ostaju.
  Vlastiti prilozi W se ne diraju.

**P7 Različita izdanja**

- W ostaje izdanje iza (npr. 0.0.28 dok su U i L na 0.0.29).
- Zapis na novijem čvoru s novim poljem: stariji ga ne kvari pri izmjeni
  (čuvanje nepoznatih polja). Pločica pokazuje različita izdanja.

**P8 Prekid i oporavak**

- W ugašen dva dana, pa upaljen: nadoknadi sve što je propustio.
- U ponovno pokrenut usred razmjene s L preko tunela: nema dvostrukih zapisa.

**P9 Sukob izmjena**

- Isti zapis zajedničkog kanala (npr. telefon djelatnika) izmijenjen na L
  (hotspot) i na W dok ne vide jedan drugoga. Nakon spajanja sva tri čvora
  imaju istu vrijednost i povijest obje izmjene.

## 4. Kasnije probe, uz nova izdanja

| izdanje | proba |
|---|---|
| 0.0.29 | „Provjeri vezu” na U, L (kuća i hotspot) i W; usporedba s `tailscale netcheck`; isto u uredu |
| 0.0.30 | L na hotspotu prijavljen na sastajalištu U; pločica pokazuje „na vezi” |
| 0.0.31 | L na hotspotu ↔ W doma **izravno** probijanjem NAT-a, bez prolaza kroz U; P4 ponovljen i uspoređen |
| 0.0.32 | probijanje namjerno onemogućeno: L ↔ W preko posrednika; veliki prilog ne smije proći |
| instalacijski program | W bez brzog namještanja: prazan čvor, uparivanje u LAN-u kao danas. W s paketom za priključenje dok je L na hotspotu: član mreže bez uparivanja; isti paket drugi put odbijen; paket stariji od 7 dana odbijen; opozvan paket ne daje članstvo. Biblioteka: uvoz izabranih letvi, paket koji nije potpisao član odbijen, pogrešan ključ za čitanje jasno javljen |
| Postava | instalacija na W bez administratora, instalira najnovije izdanje (i s USB-a bez interneta), ikona u traci, start/stop, nadogradnja 0.0.x → 0.0.x+1, namjerno pokvaren paket (pogrešan potpis) mora biti odbijen, vraćanje staroga kad novi ne odgovori |

## 5. Rezultati

| datum | proba | izdanja U/L/W | ishod | kašnjenje | napomena |
|---|---|---|---|---|---|
| | | | | | |
