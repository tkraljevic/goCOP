# Povezivost čvorova — zamisao za v2/v3

Zapis zamisli o tome kako se čvorovi pronalaze i razmjenjuju podatke kad
prerastu lokalnu mrežu. **Ovo nije odluka nego kandidat**: v1 ostaje na onome
što već radi. Nastalo iz razgovora u rujnu 2026.

Arhiva i izdanja imaju svoj zapis u [plan-arhiva-i-zaborav.md](plan-arhiva-i-zaborav.md);
ovdje se ne ponavlja, samo se na njega naslanja.

## Zamisao

Jedan javni **tracker** koji ne čuva korisničke podatke nego samo pomaže
čvorovima da se pronađu:

```
                 Tracker / Rendezvous
                        |
          pronalaženje + potvrda + prisutnost
                        |
        -------------------------------------
        |                 |                 |
      Čvor A            Čvor B            Čvor C
        \                 |                /
         \______ šifrirana P2P mreža _____/
```

Svaka mreža ima `network_id` i ključ mreže; tracker nikad ne dobiva sam ključ
nego samo iz njega izveden identifikator. Svaki čvor ima svoj UUID i par
ključeva.

Ljestvica prijenosa, od najjeftinijeg prema najskupljem:

```
LAN izravno
   ↓
izravno preko interneta (probijanje NAT-a)
   ↓
relay preko drugog čvora
   ↓
središnji relay kao zadnji izlaz
```

Isti prijenosni sloj nosi tri protokola: **sinkronizaciju baze** (UUID,
revizije, upis/izmjena/brisanje), **prijenos blobova** (arhive, slike, PDF —
sve nepromjenjivo) i **upravljanje** (pronalaženje, stanje, sposobnosti).

Za velike arhive zamišljena je podjela na dijelove kao kod torrenta: tracker
kaže tko ima koji `resource hash`, čvorovi međusobno razmjenjuju popise
dijelova, a tko dobije dio odmah ga nudi dalje. Središnji relay ostaje za male
podatke; za velike arhive se ograničava ili zabranjuje da poslužitelj ne
postane usko grlo.

Podjela slojeva:

```
Tracker    = pronalazi čvorove
Transport  = sigurna veza među njima
Sync       = sinkronizira bazu
Blob       = raznosi velike nepromjenjive datoteke
Relay      = zadnji izlaz
```

## Što od toga već postoji

Više nego što se na prvi pogled čini:

| dio | stanje |
|---|---|
| ključ mreže, potpisana članstva | radi (`internal/peers/network.go`) |
| UUID i par ključeva po čvoru | radi |
| šifrirana veza među čvorovima | radi (TLS, `syncnet`) |
| pronalaženje na LAN-u | radi (UDP broadcast) |
| sinkronizacija po revizijama | radi (knjiga verzija, `internal/ledger`) |
| tracker, probijanje NAT-a, relay | nema |
| prijenos blobova | nema |
| podjela na dijelove, swarm | nema |

Dakle nedostaje **dohvatljivost preko interneta** i **prijenos velikih
datoteka**. Sinkronizacija i sigurnost su riješene.

## Ocjena

Podjela slojeva je točna i otprilike odgovara Syncthingovu ustroju, koji
vrijedi pogledati kao presedan. Da tracker nikad ne vidi ključ mreže nego samo
izvedeni identifikator je ispravno, kao i ljestvica prijenosa.

Tri prigovora, po važnosti.

### 1. Transport je već odlučen — Tailscale

Za spoj kućnog i uredskog čvora ionako je predviđen Tailscale. On **jest** ta
četiri okvira odjednom: koordinacija koja ne vidi sadržaj, probijanje NAT-a,
E2E WireGuard i DERP relay kao zadnji izlaz.

Napisati vlastiti tracker znači napisati probijanje NAT-a — STUN, ICE, hole
punching, simetrični NAT-ovi, CGNAT kod operatera. To je područje u kojem sve
radi na stolu, a otkaže na terenu, i to obično noću za vrijeme velike vode.

Uz to otpada i briga oko javnog poslužitelja: raspoloživost, certifikat,
zloporaba, i podaci o prisutnosti — tko je kad na mreži — koji su sami po sebi
osjetljivi za ustanovu.

**Preporuka:** Tailscale ili čisti WireGuard kao prijenosni sloj, protokol
goCOP-a iznad njega. Granica ostaje čista, pa se sloj ispod može zamijeniti
ako ta ovisnost jednom zasmeta.

### 2. Swarm je prevelik stroj za ovaj broj čvorova

Podjela na dijelove isplati se kad ima mnogo čvorova. Ovdje ih je u dogledno
vrijeme dva do desetak — vlastiti, uredski, možda po jedan po sektoru.

Prijenos s nastavkom, provjeren otiskom, s bilo kojeg čvora koji datoteku ima,
uz „ako je prvi spor, pitaj drugoga", daje gotovo svu korist za mali dio
posla. Razmjena popisa dijelova dodaje se iznad istog blob sloja kad stvarno
bude dvadesetak čvorova; ništa se ne baca.

### 3. Šifrirani `.cop` paketi ne pristaju uz ovaj program

Ovo je najkonkretniji prigovor i vrijedi ga zapamtiti.

**Analize čitaju cijeli niz.** Povratni vodostaji, valovi obrane, Mann-Kendall
— sve ide preko razdoblja 1902.–2026. Ako je stara polovica u neprozirnim
šifriranim paketima, svako otvaranje historijata mora dešifrirati i posložiti
niz. Već je jednom trebalo popravljati odziv jer se pri svakom prikazu čitalo
260.000 točaka.

**Pure Go SQLite ne poznaje SQLCipher.** Program se gradi s `CGO_ENABLED=0`
na `modernc.org/sqlite`. Šifrirana baza se ondje ne otvara — ostaje
dešifriranje u privremenu datoteku, čime otvoreni podaci ionako završe na
disku i svrha otpada.

**Umjesto toga:** arhiva je obična SQLite datoteka po razdoblju, prikačena
samo za čitanje (`ATTACH`), nepromjenjiva i adresirana svojim otiskom. Time se
dobiva izdvajanje iz aktivne baze, jeftina distribucija i provjera
cjelovitosti, a niz ostaje upitljiv bez raspakiravanja. To je i ono što
[plan-arhiva-i-zaborav.md](plan-arhiva-i-zaborav.md) već predviđa izdanjima s
otiskom.

**I prije nego se išta šifrira, treba odgovoriti od koga.** Mjerenja Hrvatskih
voda i DHMZ-a putuju između vlastitih ureda po privatnoj mreži. Šifriranje na
disku donosi upravljanje ključevima — gdje ključ stoji na Docker čvoru koji se
diže bez čovjeka? Ako je odgovor „u datoteci pokraj baze", nije dobiveno
ništa, a dobiven je jedan novi način da sustav ne krene.

## Što u zamisli nedostaje, a važnije je od swarma

**Rješavanje sukoba.** Prijenos je najlakši dio. Teško je ovo: ured i kućni
čvor oboje promijene prag na istoj letvi dok su razdvojeni. Tko pobjeđuje?

Kod obrane od poplava tiho kriv prag nije estetski nego operativni problem.
Tu treba uložiti pažnju koja u skici ide na dijelove datoteka — uzročnost po
zapisu, ili barem posljednji-piše uz **izričitu oznaku sukoba koja ispliva u
sučelju**, kako već radi `NeedsReview`.

## Generički sloj za više projekata

Zamisao je da ovo bude općenit privatni P2P sloj upotrebljiv u više projekata.
Vrijedi odgoditi: sloj pisan kao općenit prije nego postoji drugi korisnik
gotovo uvijek ispadne kao prvi korisnik s više parametara.

Neka ostane `internal/` paket s čistim sučeljem. Kad se drugi projekt stvarno
pojavi, izdvajanje u vlastiti modul je posao od pola dana; pogađanje unaprijed
je skupo i obično promašeno.

## Redoslijed ako se ovo bude radilo

1. **Tailscale za dohvatljivost** — bez koda, rješava tracker, NAT i relay
   odjednom
2. **Podjela arhive** na prikačene SQLite datoteke po razdoblju, s izdanjem i
   otiskom — rješava 471 MB neovisno o svemu ostalom
3. **Prijenos blobova**: s nastavkom, provjeren otiskom, izravno s čvora — bez
   swarma
4. **Sukobi u knjizi verzija** — vidljivi, ne tihi
5. **Tracker i relay** tek ako se pojavi čvor koji ne može biti na Tailscaleu
6. **Izdvajanje generičkog sloja** kad se pojavi drugi projekt

Ukratko: podjela slojeva je dobra, ali korist za program leži u koraku 2, a
koraci 1 i 5 su posao koji je netko drugi već obavio bolje nego što se isplati
raditi sam.
