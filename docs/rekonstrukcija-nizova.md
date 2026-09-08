# Rekonstrukcija nizova iz susjedne postaje

Letva često postoji kraće nego što niz koji joj pripisujemo seže unatrag.
Batina je utemeljena 2001., a njezin niz počinje 1901.: sve prije preračunato
je iz Mohácsa. Ovdje stoji što se iz toga naučilo, jer se ista zamka ponavlja.

## Pravilo

**Odnos dviju letvi vrijedi samo u rasponu u kojem je izmjeren.**

Odnos Batina − Mohács stabilan je i uvjerljiv: medijan −135 cm na 127.574
uparena sata, gotovo ravan od 0 do 400 cm. Iskušenje je primijeniti ga i
niže. Ne smije se — Mohács u tom razdoblju nije bio niži od 4 cm, pa ispod
toga odnos nije izmjeren nego pretpostavljen.

Ista greška napravljena je istog dana i s krivuljom protoka, gdje je potencija
prilagođena mjerenjima oko srednje vode davala 41 % previše na maloj vodi.
Obrazac je isti: dobra prilagodba unutar podataka, bijeg izvan njih.

## Kako se provjerava

Trećom postajom. Za Batinu 7.1.1909., uz odnose izmjerene na 8.126 dana kad
su sve letve mjerene istovremeno:

| preko | udaljenost | Batina |
|---|---|---|
| Bezdan (utemeljen 1856.) | 0,74 km uzvodno | −125 cm |
| Apatin (utemeljen 1876.) | 23 km nizvodno | −159 cm |
| Mohács | 22 km uzvodno | −298 cm |

Bezdan i Apatin zaokružuju Batinu s obje strane i slažu se unutar 34 cm.
Mohács je 150 cm izvan njih. Evidencija COP-a za taj dan navodi ≈−127 cm.

Odnos Batina − Bezdan iznosi mjerenih +19 cm i ne mijenja se od niske do
visoke vode — točno razlika kota nule (80,64 − 80,45 = 0,19 m), jer su letve
740 m jedna od druge.

Uzrok razlike prema Mohácsu **nije utvrđen**. Pretpostavka o promjeni kote
nule nije se potvrdila: vituki i COP daju danas istovjetne vrijednosti
(razlika 0,0 cm na 5.208 dana), a u godišnjim srednjacima 1901.–2026. nema
pomaka od 173 cm. Za razrješenje treba povijest kote nule Mohácsa ili dugi
niz Bezdana.

## Što program radi s time

- Rekonstrukcija se **ne proteže** izvan raspona u kojem je odnos izmjeren.
  Za Batinu je izostavljen 161 dan na kojima je Mohács bio ispod 4 cm — 0,4 %
  niza, sve u prvoj trećini stoljeća. Isti dani izostavljeni su i iz
  preračunatog protoka, da nizovi ostanu složni.
- Sažetak krajnosti pamti **odakle je** koja krajnost i uz preračunatu piše
  znak `≈`, s objašnjenjem ispod tablice. Vrijednost se ne skriva, samo se ne
  predstavlja kao mjerenje.
- Ispadi telemetrije izbacuju se pri gradnji arhive, po susjedima a ne po
  apsolutnoj granici (`bezSiljaka` u `internal/arhiva/gradnja.go`).

## Zašto Batina nije rekonstruirana iz Bezdana

Trebala bi biti — Bezdan je 740 m uzvodno, prijenos mu je najuži (raspršenost
10 cm naspram 32 kod Apatina i 18 kod Mohácsa), i utemeljen je 1856.

Ne može se: **Bezdana i Apatina imamo tek od 2004.** (jutarnja očitanja COP-a).
Jedini niz koji seže u 1901. je mađarski Mohács. Rekonstrukcija 1901.-2001.
zato stoji na najslabijem od tri prijenosa.

Da bi se to popravilo, treba dnevni niz Bezdana 1901.-2001. — Republički
hidrometeorološki zavod Srbije. S njim bi se cijelo razdoblje preračunalo
iznova, i to prijenosom koji je na istom presjeku.

Dotad: dani na kojima Mohács izlazi iz mjerenog raspona izostavljeni su, a
zabilježeni minimum -127 cm (7.1.1909.) vodi se uz letvu kao rekonstrukcija
iz Bezdana, ne kao vrijednost niza.

## Što treba za sljedeću letvu

Prije nego se niz produlji unatrag iz susjedne postaje:

1. Izmjeriti odnos i **zapisati raspon** u kojem je mjeren.
2. Ne primjenjivati ga izvan tog raspona.
3. Provjeriti trećom postajom ondje gdje se može.
4. Označiti preračunato kao preračunato, svugdje gdje se prikazuje.
