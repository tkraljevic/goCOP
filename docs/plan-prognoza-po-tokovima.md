# Prognoza po tokovima, sektorima i branjenim područjima

Plan od 11. 10. 2026., prema 0.0.35-alfa i grani rekonstrukcije (PR #32).
Nastavlja Odluke (4. 10. 2026.) o prognozi po rijekama i plan Postavki
prognoze (30. 9. 2026.). Ništa od ovoga još nije ugrađeno.

## Odluke (11. 10. 2026.)

1. **Jedinica modela je branjeno područje.** Modeli se rade po branjenim
   područjima, ne po rijeci. Prikaz po vodotoku i sektoru izvodi se iz
   registra, kao i prije.
2. **Model prognoze mijenja administrator.**
3. **Kasnije dolazi i Sava.** Područja na Savi dobivaju svoje modele kao
   podatak, bez promjene programa.
4. **Plan se provodi kasnije.** Sada ostaju kvaliteta koda (faza 3) i mrežni
   protokol.

## Cilj

1. Prognoza se gleda i izvozi po vodotoku, sektoru i branjenom području, a
   ne samo kao jedna stranica za sve rijeke.
2. Što se iz čega računa i s kojim parametrima više nije zapisano u kodu.
   To je podatak koji se uređuje u Postavkama prognoze, za svaki tok
   posebno, bez nove inačice programa.
3. Promjena modela je zapisana: tko, kada i zašto. Prije primjene se proba
   na arhivi, a razmjenom stiže na sve čvorove.
4. Kasnije svaki tok može imati svoj kanal i svoj čvor koji ga izdaje, pa
   drugi sektor ili vodoprivreda dodaje svoju rijeku.

Načela ostaju:
- **Neovisna prognoza:** tuđe prognoze su mjerilo ili privremeni vrh lanca,
  a ne vrh lanca po zadanom.
- **Procjena nikad kao mjerenje:** zamjena s druge obale i rekonstrukcija
  ostaju samo u računu.
- **Uvjet promjene:** promjena koja mijenja brojeve koje dežurni čita ide tek
  uz usporedbu prije i poslije na istoj arhivi.

## Stanje u 0.0.35

**Već je podatak.**
- „Pripremi model” iz arhive namješta brojeve: pomake, širine i nagibe po
  pojasima, rasap, promašaje i modele ispuštanja elektrana.
- Brojevi stoje u bazi prognoza. Razmjenom putuju kao jedan zapis
  `prognoza_model/lanac` (`internal/prognoza/razmjena.go`).
- Izdanje je jedno za sve rijeke, a izdaje ga jedan čvor.

**Zakucano u kodu.** Struktura lanca i sve oko nje, u tablicama paketa:

| Što | Gdje danas | Kamo ide |
|---|---|---|
| od čega se letva računa (`Tokovi`, skupine Drava i Dunav) | `prognoza/lanac.go` | tok |
| rezerve, u čemu se letva vodi, letve vezane samo na višoj vodi, rijetke letve | `lanac.go` (`Rezerve`, `Velicine`, `SamoPovezane`, `RijetkeLetve`) | tok |
| letve s druge obale | `zamjenske.go` (`ParoviObala`) | tok |
| rekonstrukcija iz susjeda i raspon provjere | `rekonstrukcija.go` (PR #32) | tok |
| tuđa prognoza kao vrh ili ispred računa | `osvjezavanje.go` (`VrhoviSTudomPrognozom`, `TudaIspredRacuna`) | tok |
| pregledne i prenesene (srpske) letve | `osvjezavanje.go`, `prenesene.go` | tok |
| dnevni model: ciljevi, ulazi, slivovi kiše, rezerve, od kojeg dana, visoka voda | `dnevna.go` (`DnevniCiljevi`, `DnevneRezerve`, `DnevnaOdDana`, …) | tok |
| modeli ispuštanja elektrana i kote uspora | `operater.go` | tok |
| pričuvni Excel (putovi i ulazi) | `pricuvno.go` | tok |
| šifre letvi kod drugih službi (RHMZ, hydroinfo, NOEL, DanubeHIS) | `hidmet.go`, `hydroinfo.go`, `noel.go`, `javnivodostaji/danubehis.go` | registar postaja |
| pragovi računa: 14 dana unatrag, kašnjenje karike i vrha, premoštenje rupe, slaganje, poluvijek ispravka | konstante u paketu | opće postavke, tok ih smije promijeniti |
| način namještanja (pojasi, širine) i dosezi provjere i pregleda | `namjestanje.go`, `provjera.go`, `web/handlers_prognoze.go` | opće postavke |
| prikaz: skupine stranice (Dunav, inundacija, Drava, pritoke), letve izvan profila | `web/handlers_prognoze.go` | izvodi se iz toka i registra |

Svaka promjena u tablici traži novu inačicu programa i nadogradnju svih
čvorova. Tablice su globalne varijable, pa ih testovi podmeću. To je velik
dio složenosti paketa `prognoza` (faza 3 u STABILIZACIJA).

Tokovi već sada ovise jedan o drugome:
- Osijek se računa iz Belišća i Aljmaša.
- Aljmaš iz Batine i Belišća.
- Dnevni model Osijeka uzima i Mohács, Budimpeštu i Komárom.

## Tok kao podatak

Prema Odlukama (11. 10. 2026.) jedinica modela je branjeno područje: zapis
nosi letve tog područja i ono iz čega se one računaju. Ulazi iz susjednih
područja ulaze kao i svaka letva. Primjer su međudržavne rijeke: područje 33
(Mura i Drava) hrani područje 34 (Drava i Dunav) preko Botova i Terezinog
Polja, a u području 34 Osijek se računa iz Aljmaša. Riječ „tok” u ostatku
plana znači model jednog branjenog područja.

Kod danas računa dvije skupine, Dravu (s Murom, Bednjom, Vučicom i
Karašicom) i Dunav. Pri prvom punjenju one se raspodijele po područjima
prema registru: dionice letve i područja koja je prate.

Sektor i branjeno područje nisu tok: oni se za prikaz izvode iz registra
(vodotok letve, sektori i područja koji je prate, dionice).

Model područja je zapis u knjizi verzija (novi entitet, jedan zapis po
branjenom području). Primjer oblika, samo kao ilustracija; ulazi i brojevi
su iz današnjih tablica:

```json
{
  "podrucje": 34,
  "naziv": "Međudržavne rijeke Drava i Dunav",
  "letve": [
    {"letva": "botovo", "velicina": "protok",
     "ulazi": ["he-dubrava", "gorican"],
     "rezerve": [["he-dubrava", "letenye"], ["donja-dubrava", "gorican"]]},
    {"letva": "szentborbas", "velicina": "vodostaj",
     "ulazi": ["terezino-polje"], "rezerve": [["barcs"]],
     "rekonstrukcija": {"susjedi": [{"letva": "barcs", "zastoj": 4, "prosla": 8, "traje": 10}],
                        "raspon": [-107, 569]}},
    {"letva": "osijek", "velicina": "vodostaj",
     "ulazi": ["belisce", "aljmas"]}
  ],
  "druga_obala": [["terezino-polje", "barcs"], ["donji-miholjac", "dravaszabolcs"]],
  "dnevni": [{"cilj": "osijek",
              "ulazi": ["belisce", "donji-miholjac", "botovo", "aljmas", "mohacs", "budapest", "komarom"],
              "slivovi": ["A", "B", "C", "D", "E", "F", "G", "H", "I", "J"]}],
  "postavke": {"zaostatak_karike_h": 6}
}
```

Pravila:
- **Prvo punjenje.** Današnje tablice postaju početni tokovi, kao početni
  registar. Program ih upiše kad tokova u knjizi nema, a poslije vrijedi
  ono što je u knjizi.
- **Brojevi modela uz tok.** Paket modela razdvaja se po toku (ključ toka
  umjesto jednog `lanac`). „Pripremi model” namješta samo tok koji se
  mijenjao.
- **Kako račun čita tok.** Prognoza dobiva tokove kao parametar osvježivača,
  a ne iz globalnih varijabli. Tako i testovi daju svoj tok, umjesto da
  podmeću tablice.
- **Ulaz iz drugog toka.** Letva iz drugog toka ulazi kao i svaka letva:
  mjerenje iz očitanja. Za budućnost uzima zadnje izdanje tog toka (korak 4).

## Koraci

Svaki korak je zaseban PR s testovima. Brojevi koje dežurni čita smiju se
promijeniti samo ondje gdje se to namjerava.

### 1. Prikaz po vodotoku, sektoru i branjenom području

- Na stranici prognoze bira se vodotok, sektor ili branjeno područje. Zadano
  je ono što korisnik ima na dužnosti, a „sve” ostaje.
- Izvoz u Excel, sažetak i uzdužni profil slijede odabir.
- Skupine stranice izvode se iz registra (vodotok, prati sektor, prati
  područje), a ne iz popisa u kodu.
- Račun se ne dira.

Provjera: testovi web sloja za svaki odabir; isti brojevi na odabiru „sve”
kao danas.
Opseg: 2–4 dana.

### 2. Struktura lanca kao podatak

- Novi entitet `prognoza_tok` i prvo punjenje iz današnjih tablica.
- Paket `prognoza` čita tokove iz podatka. Globalne tablice nestaju ili
  ostaju samo kao početno punjenje.
- Paket modela po toku; razmjena prihvaća i stari oblik (`lanac`) dok se
  čvorovi ne nadograde.
- Šifre drugih službi prelaze u registar postaja.

Provjera:
- isto izdanje prije i poslije na istoj bazi i arhivi, bajt po bajt u
  vrijednostima;
- testovi čitanja, zapisa i razmjene toka.

Opseg: 1–2 tjedna. Ide zajedno s koracima 3 i 4 faze 3 za prognozu
(provjera unatrag, osvježavanje), jer dira iste funkcije. To je i najveći
dobitak čitljivosti tog paketa.

### 3. Uređivanje u Postavkama prognoze

- **Što se uređuje.** Za svaki tok: letve, ulazi, rezerve, u čemu se vodi,
  druga obala, rekonstrukcija, dnevni model i parametri.
- **Prekidači** iz plana od 30. 9.:
  - letva u kvaru (isključi mjerenja, rezerva računa);
  - tuđa prognoza kao vrh;
  - model ispuštanja elektrane;
  - dnevni model bez kiše;
  - rezerve DanubeHIS i druga obala.
- **Tijek promjene.** Promjena je nacrt, pa „Probaj”: namještanje na arhivi
  i točnost prema držanju zadnjeg mjerenja, po dosezima. Danas to radi
  `ProbajRacun` za jedan račun. Tek onda „Primijeni”, uz obavezan razlog.
- **Provjera unosa** odbija:
  - nepoznatu letvu;
  - kružni lanac;
  - ulaz bez podataka u arhivi;
  - rekonstrukciju bez raspona provjere;
  - susjeda iz budućnosti za rupu koja traje.
- **Zapis.** Knjiga pamti tko je mijenjao i kada, a uz zapis ide razlog.
  Kartica letve kaže kad račun ide prema promijenjenom modelu.

Provjera:
- testovi provjere unosa i ovlasti;
- „Probaj” na kopiji stvarne arhive;
- promjena na jednom čvoru stiže na drugi.

Opseg: 1–2 tjedna.

### 4. Kanali po toku i više izdavača

- Svaki tok ima svoj kanal izdanja i čvor s ulogom „izdaje prognozu” za
  taj tok. Čvorovi se pretplaćuju na tokove koje trebaju.
- Ulaz iz drugog toka za budućnost uzima zadnje izdanje tog toka, kao danas
  mađarsku prognozu, uz upozorenje kad kasni.
- Dok drugi sektor sam ne poželi izdavati, jedan čvor izdaje sve tokove.

Ovisi o mrežnom protokolu (`applyOne` kao granica protokola, rok 15. 11.
2026.), pa ide poslije njega.

## Otvorena pitanja

Odlučeno 11. 10. 2026.: jedinica modela je branjeno područje, model
mijenja administrator, Sava dolazi kasnije, a plan se provodi kasnije.
Ostaje:

1. **Kad promjena vrijedi.** Prijedlog: odmah nakon „Probaj” i „Primijeni”,
   za sljedeće izdanje; razmjenom na sve čvorove.
2. **Stari oblik modela.** Koliko dugo razmjena prihvaća paket `lanac` uz
   pakete po toku. Prijedlog: jedno izdanje.
3. **Redoslijed kad dođe na red.** Prijedlog: korak 1 prvi; koraci 2 i 3 uz
   fazu 3 za prognozu; korak 4 nakon protokola.

## Što se ne radi

- Ne mijenja se metoda računa (pojasi, ispravak prema mjerenju, dnevni
  model). Mijenja se samo gdje stoji ono što joj kaže što računati.
- Tuđe prognoze ne postaju vrh lanca po zadanom.
- Uređivanje ne smije zaobići „Probaj”: model bez provjere na arhivi ne ide
  u izdanje.
