# Rekonstrukcija nizova iz susjedne postaje

Mnoge letve postoje kraće nego niz koji se uz njih vodi. Batina je
postavljena 2001., a njezin niz počinje 1901.: sve prije preračunato je iz
susjednih postaja. Tako produljen niz pokazuje kakve su bile male i velike
vode i prije letve. Preračunat podatak u programu uvijek nosi izvor i
točnost i nikad se ne prikazuje kao mjerenje.

Popunjavanje rupa u računu prognoze, od 0.0.36-alfa, zaseban je postupak.
Odavde uzima samo pravilo raspona: izvan raspona razina u kojem je
provjereno, prognoza se i dalje računa, ali uz izdanje stoji upozorenje da
točnost nije provjerena (Odluke (11. 10. 2026.)).

## Pravilo

**Odnos dviju letvi vrijedi samo u rasponu u kojem je izmjeren.**

Odnos Batina − Mohács stabilan je: medijan −135 cm na 127.574 uparena sata,
gotovo jednak od 0 do 400 cm. Ipak se ne smije primijeniti niže. Mohács u
tom razdoblju nije bio niži od 4 cm, pa ispod toga odnos nije izmjeren,
nego pretpostavljen.

## Kako se provjerava

Trećom postajom. Za Batinu 7. 1. 1909., uz odnose izmjerene na 8.126 dana
kad su sve letve mjerene istodobno:

| preko | udaljenost | Batina |
|---|---|---|
| Bezdan (postavljen 1856.) | 0,74 km uzvodno | −125 cm |
| Apatin (postavljen 1876.) | 23 km nizvodno | −159 cm |
| Mohács | 22 km uzvodno | −298 cm |

Bezdan i Apatin okružuju Batinu s obje strane i slažu se unutar 34 cm.
Mohács je 150 cm izvan njih. Evidencija COP-a za taj dan navodi oko
−127 cm.

Odnos Batina − Bezdan iznosi izmjerenih +19 cm i ne mijenja se od niske do
visoke vode. To je točno razlika kota nule (80,64 − 80,45 = 0,19 m), jer su
letve 740 m jedna od druge.

## Promjena kote nule Mohácsa 1943.

**1. 1. 1943. spuštene su kote nule dunavskih letvi od Budimpešte
nizvodno**, da se u nizovima izbjegnu negativni vodostaji, koji su se
javljali sve češće. Iznos je zapisan:

> „A nullpont süllyesztése a Budapest–Dunaföldvár szakaszon 100 cm, a lefelé
> következő Paks állomástól Mohácsig pedig 200 cm volt." (VITUKI 1976)

Dakle **Budimpešta–Dunaföldvár 100 cm, Paks–Mohács 200 cm**. Vrijednostima
Mohácsa prije 1943. dodaje se **+200 cm** prije preračuna.

Izvor: Kovács Péter, *A budapesti állami vízmérce két évszázados története*,
40. Vándorgyűlés, Magyar Hidrológiai Társaság; iznos preuzet iz VITUKI
(1976): *Hidrológiai alapok… 4.4. kötet: Adatgyűjtemény a vízmércékről*,
166 str.

Uz zapisane pomake između dviju epoha ostaje stepenica od −17 cm, jednaka za
Mohács i Baju u svim razredima vode. Zapisani pomak kote nule je ne
objašnjava, pa se ne ispravlja: ostaje u nizu kao razlika između epoha.

Nakon ispravka **nijedan dan više ne izlazi iz izmjerenog odnosa**: Mohács
ispravljen nikad nije ispod 4 cm. Niz Batine je cjelovit, svih 36.493 dana,
s točnošću ±14 cm. Za 7. 1. 1909. sve se procjene sada slažu:

| | Batina |
|---|---|
| preko Mohácsa, ispravljeno | **−108 cm** |
| preko Bezdana | −125 cm |
| evidencija COP-a | −127 cm |
| preko Apatina | −159 cm |

## Što kote nule govore

Visinski sustavi ne smetaju prijenosu: pomak se mjeri iz samih očitanja, pa
razliku sustava upija sam od sebe. Kad se kote ipak usporede, provjeravaju i
prijenos i same sebe. Sve kote ovdje su u jadranskom sustavu (m n.J.m.).
Mađarske kote su u evidenciji dane i kao baltičke, m n.B.m. + 0,675 (vidi
[baltičke kote](balticke-kote.md)).

| letva | rkm | kota nule | razlika prema Batini | izmjereni pomak | ostatak = pad vode |
|---|---|---|---|---|---|
| Mohács | 1446,90 | 79,88 | −57 cm | −135 cm | −78 cm / 22,1 km = **3,54 cm/km** |
| Bezdan | 1425,59 | 80,64 | +19 cm | +19 cm | **0 cm** / 0,7 km |
| Batina | 1424,85 | 80,45 | — | — | — |
| Apatin | 1401,90 | 78,84 | −161 cm | −48 cm | +113 cm / 22,9 km = **4,92 cm/km** |

Kod Bezdana je izmjereni pomak točno razlika kota, bez ostatka, kako i mora
biti na 740 m. To potvrđuje i kote i mjerenja. Pad Dunava u ovoj dionici je
**3,5 cm/km** uzvodno prema Mohácsu i **4,9 cm/km** nizvodno prema Apatinu.

Kote odaju i promjenu iz 1943. Mohács je 21,3 km uzvodno od Bezdana, a kota
nule mu je 76 cm niža (79,88 naspram 80,64). To je naopako. Uz 200 cm
spuštenih 1943. stara nula bila je 81,88 m, dakle iznad Bezdanove, kako
uzvodnoj letvi i priliči.

Kota postavlja i granicu. Mohács ispravljen za 1909. iznosi +35 cm.
Batinina nula viša je 57 cm, pa uz izmjereni pad od 3,5 cm/km preko 22 km
ispada oko −99 cm, a izmjereni pomak od −133 cm daje −108 cm. Evidencija
COP-a navodi −127 cm, dakle unutar dosega.

## Što program radi s time

- Rekonstrukcija se izvan izmjerenog raspona **ne izbacuje, nego
  označava**. Rupa u nizu hidrologu ne govori ništa, a znati da je bila
  ekstremna mala voda i otprilike kolika govori dosta. Dani na kojima
  susjedna letva izlazi iz raspona idu u zaseban izvor s nastavkom `-izvan`,
  s točnošću **±150 cm** umjesto ±14 cm. Uz svaku takvu vrijednost piše
  „izvan mjerenog odnosa”. Za Batinu takvih dana nakon ispravka iz 1943.
  nema; pravilo ostaje jer će trebati drugim letvama.
- Sažetak krajnosti pamti odakle je koja krajnost, a uz preračunatu piše
  znak `≈`, s objašnjenjem ispod tablice. Vrijednost se ne skriva, samo se ne
  predstavlja kao mjerenje.
- Ispadi telemetrije izbacuju se pri gradnji arhive, prema susjednim
  vrijednostima, a ne prema jednoj granici za sve.

## Batina 1960.–2001. iz Bezdana

Izvor `rhmz` su ovjereni dnevni vodostaji Bezdana, Apatina, Bogojeva i
Bačke Palanke za 1960.–2025. i protoci Bezdana, Bogojeva i Bačke Palanke,
koje daje Republički hidrometeorološki zavod Srbije. Za Bačku Palanku
protoci su za 2023.–2025.

Bezdan je 740 m uzvodno od Batine, na suprotnoj obali, pa je za Batinu
bolji susjed od Mohácsa na 22 km:

| | Bezdan − Batina | rasap |
|---|---|---|
| 2001.–2022., Batina izmjerena | −18,5 cm | **4,9 cm** |
| 1960.–2000., Batina preračunata iz Mohácsa | −24,5 cm | 13,2 cm |

Preračun iz Mohácsa griješio je u tom razdoblju oko 6 cm u prosjeku i 13 cm
od dana do dana. Odnos s Bezdanom blago ovisi o vodi (−20 cm pri maloj,
−15 cm pri velikoj), pa se preračunava pravcem izmjerenim na 7.968 dana:

    Batina = Bezdan + 20,23 − 0,0099 · Bezdan

| izvor | razdoblje | točnost |
|---|---|---|
| `preracun-bezdan` | 1. 1. 1960. – 8. 3. 2001. | ±5 cm |
| `preracun-mohacs` | 1901. – 1959. | ±14 cm |
| `preracun-mohacs-izvan` | dani izvan izmjerenog odnosa | ±150 cm |

Preračun iz Bezdana ima prednost pred preračunom iz Mohácsa. Petnaest dana
vrha 1965. (Bezdan do 776 cm) je do 21 cm iznad izmjerenog raspona. Odnos je
ondje gotovo ravan, pa je produljenje sigurno. Protok Batine za 1960.–2001.
uzima se Bezdanov: u 2001.–2022. dvije se postaje slažu na −0,5 % (rasap
4,7 %), a preračun krivuljom Batine davao je 15,6 % previše, jer je
krivulja današnja, a odnos vodostaja i protoka se otada pomaknuo.

Za 1901.–1959. dnevnog niza Bezdana zasad nema, pa taj dio stoji na
preračunu iz Mohácsa, najslabijem od tri prijenosa. Zabilježeni minimum
−127 cm (7. 1. 1909.) vodi se uz letvu kao rekonstrukcija iz Bezdana, a ne
kao vrijednost niza.

## Datum dnevnih vrijednosti s DanubeHIS-a

DanubeHIS srpske i češke dnevne vrijednosti označava s 23:00 UTC
prethodnog dana, što je ponoć po UTC+1, a mađarske s 00:00 UTC. Zato se
datum dnevnog vodostaja i protoka s DanubeHIS-a uzima iz oznake pomaknute
na UTC+1. Mađarske dane to ne mijenja, a srpski više ne ispadaju dan
raniji. Uz to pravilo svih 2.282 zajednička dana Bezdana i Bogojeva s
DanubeHIS-a i RHMZ-a slažu se do centimetra. Satnih nizova se to ne tiče.
Za austrijske, slovenske i češke dnevne vodostaje i protoke s DanubeHIS-a
pravilo još nije provjereno.

Kišomjeri s DanubeHIS-a imaju svoje pravilo. Mađarski i slovenski dnevni
zbroj nosi datum kraja razdoblja, a austrijski početka (provjereno prema
ERA5). Zato su mađarski i slovenski dani pomaknuti da stoje kao austrijski
(izvor `kisomjer-danubehis`).

## Promjene kote nule kao podatak

Premještanja nule vode se uz arhivu, u datoteci `promjene-kote.csv` u mapi
vodostaja: letva, datum, pomak, izvor i napomena. Pri gradnji arhive
program ih **primjenjuje**: vrijednostima prije tog datuma dodaje pomak, pa
cijeli niz stoji na današnjoj koti. Sirove datoteke ostaju netaknute.
Ispravlja se tumačenje, a ne podatak, kao i kod vremenskih zona.
**Historijat** letve ispisuje promjene iznad povijesti iz arhive, da se niz
ne čita kao da je oduvijek na istoj koti.

Dunavske letve, prema VITUKI (1976), na dan 1. 1. 1943.:

| letva | pomak |
|---|---|
| Budimpešta, Dunaföldvár | +100 cm |
| Paks, Baja, Dunaszekcső, Mohács | +200 cm |

Te su promjene ispravile 107.193 zapisa. Uz dunavske stoje
Orahovica 1. 5. 2002. (−20 cm, HIS-2000, historijat postaje) i osam austrijskih
letava iz matičnih podataka eHYD-a: Angern (1976. i 2001.), Mureck, Graz,
Rabland, St. Georgen (Mura), Zeltweg, Lienz-Peggetz i Oberdrauburg. Uz
austrijske u napomeni piše je li ih potvrdio skok dnevnog niza. Promjene iz
eHYD-a koje se u nizu ne vide nisu upisane.

## Snimke korita i njihova stacionaža

Snimke korita s godinama se iznova stacioniraju. Batinine tri:

| snimka | stacionaža | pomak na zajedničku mrežu |
|---|---|---|
| 22. 3. 2010. | 0 – 505,6 m | 0 |
| 12. 11. 2015. | 110 – 502,4 m | 0 |
| 18. 8. 2020. | 0 – 398,9 m | **+104,5 m** |

Snimka iz 2020. iznova je postavila stacionažu i izostavila prvih oko 104 m
lijeve terase. Pomak je izmjeren poklapanjem obala i pokosa. Bez njega
odstupanje između snimaka iznosi 6,33 m, a s njim **0,39 m**.

Poravnanja se vode u datoteci `poravnanje-profila.csv` u mapi vodostaja i
idu u arhivu. Zapisana su za dvanaest letava: Batinu, Dalj, Vukovar, Ilok,
Aljmaš, Osijek, Belišće, Donji Miholjac, Terezino Polje, Novo Virje,
Moslavinu i Vrbovku. Više snimaka ima 28 letava, a za ostalih šesnaest
poravnanje nije zapisano. Gotovo svaka snimka ima svoje polazište
stacionaže, od nekoliko metara (Dalj 2018., −5,4 m) do pola kilometra
(Ilok 2010., −544,5 m, jer snimka obuhvaća cijelu inundaciju). Uz
poravnanje odstupanje između snimaka pada s 2–8 m na 0,4–1,6 m.

Snimka koja se ni poravnanjem ne da složiti s ostalima dobiva u istoj
datoteci oznaku `crtaj` = `ne`. Ostaje u arhivi i u paketu, ali ne ulazi u
crtež. Dosad su dvije takve: Donji Miholjac 12. 7. 2001. (visine oko
3,15 m iznad svih ostalih snimaka iste letve, dakle druga visinska osnova)
i Dalj 11. 11. 2015. (točke desne obale upisane redom snimanja, a ne po
stacionaži).

### Spajanje snimaka

Presjek koji se prikazuje složen je od svih snimaka. **Novija snimka ima
prednost svugdje gdje seže, a starija se uzima samo ondje gdje novije
nema.** Dno tako dolazi iz najnovije snimke, a obale koje novija ne pokriva
iz starije, bez miješanja dna iz različitih godina. Za Batinu to daje
96 točaka na 0–505,6 m umjesto 82 na 0–398,9 m: dno 72,81 m iz 2020., a
lijevu obalu 90,11 m iz 2010.

Krilo starije snimke uzima se **samo ako se doista nadovezuje**. Razmak do
nacrtane linije ne smije biti veći od koraka same snimke, inače bi se
između povukla duga ravna crta kroz prostor koji nitko nije snimio. Korak
na spoju ne smije biti strmiji od terena koji spaja. Tako prolazi obala
koja se i inače diže, a zaustavlja se skok na ravnom. Snimka od koje ništa
nije ušlo u crtež ipak zauzima svoj raspon: ako se ona ne nadovezuje, neće
ni starija, a obala složena od komadića raznih godina nije presjek. Uz
crtež piše iz kojih je snimaka složen i koji je dio dala koja.
