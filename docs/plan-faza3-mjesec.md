# Faza 3 u mjesec dana

Plan od 11. 10. 2026., za mjesec rada s većim limitom (od 12. 10. do 13. 11.
2026.). Slijedi redoslijed iz Odluka (11. 10. 2026.): prvo čitljivost i
pojednostavljenje (faza 3 iz [STABILIZACIJA.md](STABILIZACIJA.md)), zatim
mreža, a tek onda proširenja. Odluke (14. 10. 2026.) mrežu pomiču naprijed:
pravila razmjene na granici i WEB sync ([plan-web-sync.md](plan-web-sync.md))
idu usporedo s koracima 1–5 faze 3, a TESTNA SIMULACIJA tek nakon WEB synca.
Datumi u oba plana su okvirni redoslijed: korak kreće čim je spojen onaj o
kojem ovisi, pa može i ranije. Polazište je mjerenje od 11. 10. 2026.:

| | 4. 10. | 11. 10. |
|---|---:|---:|
| pokrivenost | 51,8 % | 64,9 % |
| kritična pokrivenost | 50,4 % | 69,3 % |
| prosječni CRAP | 43,4 | 18,0 |
| funkcije iznad cilja (kritične / ostale) | 476 / 530 | 373 / 466 |

## Pravila za cijeli mjesec

- Svaki korak je zaseban PR s testovima. Prvo se pišu testovi koji zaključaju
  današnje ponašanje, a tek onda se mijenja kod.
- Preuređuje se samo kad to poboljšava čitljivost. Gdje ne pomaže, zapiše se
  iznimka s razlogom.
- Korak koji mijenja brojeve koje dežurni čita ide uz usporedbu prije i
  poslije na istoj bazi ili arhivi.
- Posao koji se dijeli (testovi po paketima, lagana pojednostavljenja) ide
  usporedo, u zasebnim radnim stablima. Rizični koraci idu jedan za drugim.
- Granice kvalitete za nove i dirane funkcije su strože od prvog dana
  (odluka 11. 10.).

## Po danima

### 1. tjedan (12.–16. 10.): pravila, dorade, testovi

| dan | posao | ishod |
|---|---|---|
| 1 | Strože granice: nove kritične funkcije CRAP ≤ 7, ostale ≤ 10, dirana ne smije postati gora; pravila pisanja koda u `CODE_QUALITY.md`. Popis 42 commita drugog kruga dorada i što je od njih već ispravljeno drugim putem. | PR granica; popis dorada |
| 2 | Dorade bez sukoba: `odluke/ocitanja-raspon`, `odluke/testovi-izmisljeni`, `odluke/zid`. | PR |
| 3 | Dorade sa sukobom: `odluke/akti`, `odluke/knjiga-ovlasti`, `odluke/uprava`; pregled. | PR |
| 4–5 | Testovi za 205 kritičnih funkcija kojima trebaju samo testovi, usporedno po paketima (prognoza, peers, arhiva, uvoz, repository, prijava). | PR po paketu |

### 2. tjedan (19.–23. 10.): `applyOne` i protokol razmjene

| dan | posao | ishod |
|---|---|---|
| 6 | Testovi koji zaključaju primjenu po entitetu: novija i starija verzija, spomenik, površina, sukobi. | testovi |
| 7–8 | Razdvajanje `applyOne` i `removeFromSurface` po entitetu, bez promjene ponašanja. Usporedba iste baze nakon primjene istih verzija prije i poslije, na kopiji stvarne baze. G3 iz [plan-web-sync.md](plan-web-sync.md) u isto vrijeme mijenja `ledger.Apply` na istom putu primjene; spajaju se jedan za drugim. | PR |
| 9–10 | Protokol na toj granici: inačica entiteta, nepoznati entitet (prema odluci), zapis odbijenih i djelomično podržanih poruka, pločica razmjene to pokazuje. Dokumentacija protokola. To su koraci G1–G7 iz [plan-web-sync.md](plan-web-sync.md) (§3); ondje traju 6,5–8,5 dana i idu usporedo s ostalim koracima. | PR po koraku |

Rok za `applyOne` je 15. 11. 2026.; plan ga stavlja ranije, da ostane vremena.

### 3. tjedan (26.–30. 10.): koraci 1–3 faze 3 i greške

| dan | posao | ishod |
|---|---|---|
| 11 | Korak 1: odluka po dionici pri ovjeri akta kao čista funkcija, provjerena cijelom tablicom (radnja × stupanj × stanje). | PR |
| 12 | Korak 2: uvoz ugovora: provjera područja prije upisa, uparivanje odvojeno od upisa. | PR |
| 13–14 | Korak 3: provjera unatrag: mjerenje odvojeno od ispisa i spremanja. Usporedba tablice promašaja prije i poslije na istoj arhivi. | PR |
| 15 | Pregled 496 errcheck nalaza: stvarne greške se popravljaju, a namjerno zanemarene se označe. Nalazi u `internal/peers`, `internal/razmjena` i `internal/ledger` čekaju izdanje nakon F3 iz [plan-web-sync.md](plan-web-sync.md). Nalazi u `cmd/gocop`, `internal/db` i `internal/config` čekaju izdanje 1, a u datotekama `internal/web` koje dira WEB sync spoj F2a (isti plan, §5). | PR |

### 4. tjedan (2.–6. 11.): osvježavanje, lagana pojednostavljenja, ovlasti

| dan | posao | ishod |
|---|---|---|
| 16–17 | Korak 4: osvježavanje: budućnost vrhova i primjena promašaja na jednom mjestu. Testna arhiva (`spoj`, `hq_krivulje`); usporedba izdanja prije i poslije. | PR |
| 18 | 86 laganih pojednostavljenja (CC 11–15) u kritičnim paketima, usporedno po paketima. `internal/peers`, `internal/razmjena` i `internal/ledger` čekaju izdanje nakon F3 iz [plan-web-sync.md](plan-web-sync.md). | PR po paketu |
| 19–20 | Korak 5: ovlasti, prvi komadi: metode s imenom namjere, preseljenje izravnih čitanja datoteku po datoteku. Dio koji dira `internal/web` kreće tek kad je spojen F2a iz [plan-web-sync.md](plan-web-sync.md). | PR po komadu |

### Rezerva (9.–13. 11.): ovlasti, mjerenje, izdanje

- Ovlasti, nastavak.
- Srednja pojednostavljenja (CC 16–30) u kritičnim paketima, osim `internal/peers`,
  `internal/razmjena` i `internal/ledger` (čekaju izdanje nakon F3 WEB synca).
- Novo mjerenje kvalitete i zapisane iznimke.
- **Izdanje:** izdanja su zajednička. Svako nosi sve što je do tada spojeno, pa koraci faze 3 izlaze s izdanjima 0, 1 ili 2 iz [plan-web-sync.md](plan-web-sync.md) (§5). Pravila razmjene na granici izlaze s izdanjem 0. Rekonstrukcija iz susjeda izašla je već u 0.0.36-alfa (12. 10. 2026.). Izdanja nemaju unaprijed dodijeljene brojeve. Unraid se nadograđuje prvi, zatim ostala računala, i Windows.

## Poslije mjeseca

- **Na manjem limitu:** srednja pojednostavljenja u webu, pokrivenost cijelog
  koda do 85 %, stare teške funkcije (`RunJournals`, `opisi`).
- **TESTNA SIMULACIJA**, tek nakon WEB synca (Odluke 14. 10. 2026.). WEB sync
  više nije ovdje: ide usporedo, po [plan-web-sync.md](plan-web-sync.md).
- **Tek onda proširenja:**
  - prognoza po branjenim područjima ([plan-prognoza-po-tokovima.md](plan-prognoza-po-tokovima.md));
  - karte: nasipi, objekti i obuhvati poplave, iz podloga karata opasnosti od
    poplava;
  - registar crpnih stanica i ustava: ustave s uzvodnim i nizvodnim
    vodomjerom; crpne stanice s radom agregata i vodostajima prije, tijekom i
    nakon rada.

## Odluke potrebne za plan

Već riješeno:
- **Doseg 0 i glačanje u provjeri unatrag:** u 0.0.34 i odlukom 16.
- **Prekid koji nije pripremni:** Odluke (4. 10. 2026.) o stadijima obrane;
  ukida se samo najviši stadij koji traje.

Odluke (11. 10. 2026.):
1. **Nepoznati entitet:** čvor zapis entiteta koji ne poznaje čuva u knjizi i
   prosljeđuje dalje, ali ga ne primjenjuje dok se ne nadogradi; tada ga
   primijeni. Pločica razmjene kaže koliko zapisa čeka nadogradnju. Stare i
   nove inačice tako rade zajedno bez gubitka. Ide u 2. tjedan.
2. **Dnevni list otvoren prije ponoći, spremljen poslije nje:** sprema se na
   svoj dan, dan na koji je obrazac otvoren, bez broja dok se ne preda, kao i
   nacrt. Tekst se ne gubi. Ide uz dorade u 1. tjednu.
3. **Sken ovjere i slanje ovjerenog akta:** smije onaj tko akt smije
   ovjeriti; slati smije i Dežurni operater u COP-u. Ide u 3. tjedan.
4. **Stare otvorene epizode obrane bez akta:** pri prelasku na 0.0.36
   program ih zatvori, s napomenom da su zatvorene pri prijelazu na stanje
   iz akata. Ostaju u povijesti, a obrana se računa samo iz akata. Ide u
   3. tjedan.

Otvoreno, do 4. tjedna:
- **Ovlasti:** smije li dužnost uprave biti bez sektora ili s područjem 0?
  Smije li privremena uprava skinuti zastavicu globalnog administratora?
