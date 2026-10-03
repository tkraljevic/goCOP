# Protokol povezivanja čvorova (goCOP veza/1)

Prijedlog dizajna, 2. 10. 2026. Ništa od ovoga još nije ugrađeno.
Nastavlja [plan-povezivost.md](plan-povezivost.md), korake 2, 6 i 8 te §9
(politika releja). Kad se usvoji, korak 8 tablice plana („vlastiti
transport — možda nikad") treba ažurirati.

Temelj je najmanja izvedba (sastajalište kroz postojeći tunel, UDP
probijanje, posrednik). Na nju je od početka ucijepljen sloj sesije s
kanalima, da se roj kasnije uključi bez nove verzije protokola.

Oznake izvora: **[V]** provjereno u kodu ili primarnom izvoru, **[T]**
isprobano u istraživanju, **[S]** samo sekundarni izvori, **[P]** prijedlog ili zaključak.

---

## 1. Svrha i ograničenja

**Problem.** Dva čvora iza NAT-a (laptop izvan kuće, uredski čvor, laptop
na mobilnoj mreži) danas se ne mogu izravno spojiti. Sve ide kroz jedini
dohvatljiv čvor, Unraid, preko Cloudflare tunela. Unraid je zato usko grlo i
jedina točka kvara za sve izvan LAN-a.

**Cilj ovog koraka.** Svaka dva člana mreže dobiju izravan, šifriran UDP put
kad god je to fizički moguće. Kad nije, dobiju posrednika koji vidi samo
šifrat. Roj (više izvora za sadržaj i pakete) dolazi poslije, nad istim
sesijama.

**Ograničenja koja se ne mijenjaju:**

| ograničenje | posljedica |
|---|---|
| kod kuće nema otvorenih portova | Unraid je za UDP i sam iza NAT-a; i do njega se dolazi probijanjem |
| Cloudflare Tunnel nosi samo HTTP/HTTPS/WebSocket, ne UDP | signalizacija i posrednik idu WebSocketom; sastajalište ne može izmjeriti UDP |
| uvjeti Cloudflarea: zabrana „VPN or other similar proxy services" (§2.2.1(j)) i pravilo o velikim datotekama [V] | kroz posrednika samo knjiga i mali sadržaj, uz tvrdi dnevni proračun |
| uredski vatrozid (Hrvatske vode) nepoznat | uredski čvorovi se ionako sinkroniziraju u LAN-u; put ured ↔ vani možda samo preko posrednika |
| nema javnih trackera ni DHT-a, nema općeg VPN-a | „tko što ima" nikad ne izlazi iz autentificirane sesije |
| stariji čvorovi moraju raditi | samo dodana, neobavezna polja; nove vrste poruka samo na novim kanalima; novo ponašanje se dogovara |
| Go 1.27.1, bez CGO-a, jedna statička binarka | jedna nova ovisnost: `github.com/quic-go/quic-go` (MIT, bez CGO-a, prikvačena v0.63.0) |

**Odluke korisnika:** vlastiti protokol po uzoru na WebRTC; signalizacija
kroz postojeći tunel, Unraid kao sastajalište; javni STUN Google
(`stun.l.google.com:19302`) i Cloudflare (`stun.cloudflare.com:3478`),
nasumično izabran, drugi kao rezerva; istodobno probijanje; šifriranje
ključevima čvorova; WebSocket posrednik preko Unraida kao zadnji izlaz.
copB.voda.hr ili VPS kasnije ulaze kao još jedno sastajalište bez promjene
protokola.

**Što se ne uvodi:** pion/ice, DTLS, SCTP, TURN, pion/stun. STUN Binding
zahtjev je oko 200 redaka stdlib koda; probni program iz istraživanja to već
radi [T]. Binarka raste za oko +1,6–1,8 MiB [T].

---

## 2. Pregled

```
                     Cloudflare (vidi samo vanjski TLS i veličine)
                                     │
      ┌─────────── wss /razmjena/signal ───────────┐
      │      (unutarnji TLS, ključ čvora, ALPN      │
      │       gocop-signal/1, potpisani pozivi)     │
      ▼                                             ▼
 ┌─────────┐   poziv / odziv / sinkro      ┌──────────────────┐
 │ čvor A  │◄─────────────────────────────►│  SASTAJALIŠTE R  │
 │ laptop  │                               │  (Unraid danas;  │
 └────┬────┘                               │   copB/VPS sutra)│
      │  UDP 4713: STUN ─► Google / Cloudflare (nasumično, drugi rezerva)
      │                                    └────────┬─────────┘
      │  udarci (HMAC) ◄────────────────────────►   │  wss /razmjena/spoj
      │  QUIC sesija A↔B (TLS 1.3, ključevi čvorova)│  (posrednik, zadnji izlaz:
      │   tok 0x00 kontrola                         │   spaja dvije noge, unutra
      │   tok 0x01 razmjena (današnji razgovor)     │   TLS A↔B kao u tunelu)
      │   tok 0x02 sadržaj (rezervirano za roj)     │
      ▼                                             ▼
 ┌─────────┐                                ┌─────────┐
 │ čvor B  │◄──── LAN: TCP 4710 / QUIC ────►│ čvor C  │   (ured: uvijek LAN)
 └─────────┘                                └─────────┘
```

**Redoslijed puteva** (ocjena, manje je bolje; po uzoru na Syncthing [V]):

| put | ocjena | `Put` |
|---|---|---|
| LAN QUIC (beacon nosi `quic_port`) | 10 | `lan` |
| LAN TCP 4710 (današnji) | 15 | `lan` |
| zapamćena izravna TCP adresa | 20 | `izravno` |
| postojeća živa QUIC sesija | njezina ocjena | — |
| probijeni UDP (QUIC) | 30 | `probijeno` |
| stari tunel `https://`, kad je cilj sam sastajalište | 40 | `tunel` |
| posrednik | 50 | `posrednik` |

LAN pretraga (`FindDevice`, 700 ms) i signalizacija kreću usporedno. LAN
pobjeđuje čim odgovori. Ured: dva čvora u istoj mreži uvijek idu LAN-om i
probijanje se ne pokušava.

---

## 3. Sudionici i uloge

**Čvor (sudionik).** Svaki čvor s `[povezivost] probijanje = true`:
- jedna UDP utičnica, zadano **4713** (obitelj 4710 TCP razmjena, 4711
  uparivanje, 4712 UDP otkrivanje); ako je zauzeta, nasumični port i
  upozorenje na pločici;
- utičnica se odmah predaje u `quic.Transport`; STUN i udarci idu kroz
  `Transport.WriteTo`, a primaju se kroz `ReadNonQUICPacket` [V];
- **jedna gorutina čita `ReadNonQUICPacket` od pokretanja do gašenja**.
  Paketi prije prvog čitanja se gube, a red drži samo 32 paketa [V];
- razvrstavanje ne-QUIC paketa: bajtovi 4–7 = `0x2112A442` → STUN; prvi
  bajt `0x2A` → udarac; ostalo se odbacuje i broji. Provjera STUN-a ide
  prva;
- prijavljen je kod najviše 3 sastajališta.

**Sastajalište (R).** Čvor s javnom HTTPS adresom (`[povezivost]
sastajaliste = true`): danas samo Unraid. Poslužuje `/razmjena/signal`.
Registar prijavljenih, prosljeđivanje potpisanih poruka, izdavanje žetona za
posrednika. Ne vidi otiske, kanale, pretplate ni sadržaj. Sastajalište je i
običan čvor: poziv kojem je cilj on sam obradi lokalno, pa laptop izvan kuće
dobije izravan UDP put do Unraida.

**Posrednik.** Obično isti čvor kao sastajalište (`posrednik = true`).
Poslužuje `/razmjena/spoj`: spaja dvije WebSocket noge bajt za bajtom. Kroz
njih teče isti unutarnji TLS A↔B kao u današnjem tunelu. Posrednik nikad ne
šalje promet nikome osim dvjema prijavljenim nogama iste sesije.

**STUN poslužitelji.** `stun.cloudflare.com:3478` i `stun.l.google.com:19302`.
`stun1–4.l.google.com` su ista IP adresa (74.125.250.129) i nisu rezerva
[T]. Oba su samo UDP, bez RFC 5780 OTHER-ADDRESS, bez SLA [T/V]. Zahtjev nema
atributa `SOFTWARE`. Kasnije copB/VPS može služiti vlastiti STUN s dvije
adrese (tada i test filtriranja).

**Otkrivanje uloga.** Na vrh zapisa čvora (`EntityPeers`) dodaju se polja
(od 0.0.24 se nepoznata polja na vrhu čuvaju [V]):

```json
"mogucnosti": ["veza/1", "sastajaliste/1", "posrednik/1"],
"dohvatljivost": "tunel" | "javna" | "nat" | "nepoznato",
"posluzujeSadrzaj": true,
"prioritetSastajalista": 50
```

Javne IP adrese, NAT preslikavanja i „tko s kim" **ne ulaze** u knjigu.
Čvor bez `veza/1` ide starim `SyncWith` putem. Ako polje još nije stiglo,
404 na `/razmjena/signal` znači „nije sastajalište", nalaz se pamti 6 h.
Redoslijed sastajališta za poziv: `prioritetSastajalista` (copB npr. 10,
kućni Unraid 50), zatim RTT kontrolne veze.

**Adresar.** Lokalna tablica `veza_adresar` (nije u knjizi): čvor, vrsta
(lan, javni, probijeni, tcp, https), adresa, izvor, zadnji uspjeh/neuspjeh,
neuspjeha zaredom, RTT. Probijena `ip:port` vrijedi najviše 2 min nakon
zadnjeg prometa.

**Adresa je ključ čvora.** U signalizaciji se cilj imenuje javnim ključem.
ID čvora sastajalište izvodi iz ključa dokazanog u TLS-u i redka u
`memberships`, nikad iz onoga što klijent javi.

---

## 4. Tijek uspostave veze

### 4.1 Mjerenje NAT-a (netcheck)

- **Kada:** pri pokretanju, svakih 10 min, pri promjeni adresa sučelja
  (provjera svakih 30 s) i na gumb „Provjeri vezu".
- **Puno mjerenje:** s **iste utičnice** pitaju se **oba** poslužitelja.
  - ista `ip:port` od oba → `eim = da` (preslikavanje neovisno o odredištu);
  - različit port ili IP → `eim = ne` (simetrično);
  - odgovorio samo jedan → `eim = nepoznato`;
  - nijedan u 2,5 s → `udpBlokiran = true`;
  - mapirani port = lokalni → `cuvaPort = true`.

  Tako netcheck radi i Tailscale (`MappingVariesByDestIP`) [V]. Usporedba ne
  proturječi odluci „nasumično, drugi kao rezerva": rezerva vrijedi za
  dohvat adrese, a usporedba je jedini način da se vidi vrsta NAT-a.
- **Osvježavanje prije poziva** (ako je nalaz stariji od 20 s): jedan
  nasumično izabran poslužitelj, ponavljanja nakon 0,5 / 1 / 2 s; bez
  odgovora u 500 ms pita se i drugi.
- **Provjera odgovora:** IP:port poslužitelja kojem je poslano, transakcijski
  ID (96 bita nasumice), Binding Success.
- **`CF-Connecting-IP` se samo prikazuje.** Razlika prema STUN adresi ne
  znači `eim = ne`: klijent često do Cloudflarea ide IPv6-om, a STUN IPv4-om,
  ili kroz korporativni proxy s drugim izlazom. Uspoređuje se samo ista
  obitelj adresa, i samo za natpis „više izlaza" na pločici.
- **Izmjereno [T]:** laptop na HT-u ima EIM i čuva port (najbolji slučaj).
  Ured, mobilna mreža i Unraid u Dockeru nisu izmjereni.

### 4.2 Signalna veza prema sastajalištu

```
ISKLJUCENO ─(probijanje=true, postoji sastajalište)─► SPAJANJE
SPAJANJE ─(WS + TLS + ALPN gocop-signal/1 + ključ = očekivani)─► PRIJAVA
SPAJANJE ─(404 / prazan ALPN / drugi ključ)─► NIJE_SASTAJALISTE (6 h)
SPAJANJE ─(mreža / 503 / rok)─► CEKANJE
PRIJAVA ─(prijavljen ≤ 10 s)─► PRIJAVLJEN
PRIJAVLJEN ─(75 s bez prometa / zatvaranje / Cloudflare restart)─► CEKANJE
CEKANJE ─(2 s · 2^n, najviše 5 min, ±20 %)─► SPAJANJE
```

Puls svakih 25 s u oba smjera (Cloudflare ne objavljuje rok neaktivnosti
WebSocketa; zajednica navodi oko 100 s [S]). Cloudflare ionako povremeno
prekida duge veze pri svojim deployima [V], pa je ponovno spajanje redovni
put, ne iznimka.

### 4.3 Izravno probijanjem (A zove B)

1. **Odluka.** `SyncWith(B)`: nema LAN-a ni izravne adrese, B ima `veza/1`,
   B nije u negativnoj pričuvi. Ako je moj `udpBlokiran`, odmah korak 7.
2. **Poziv.** A osvježi STUN (≤ 2 s) i šalje potpisani `poziv` kroz R.
   Rok za odziv 5 s; `nema` ili rok → sljedeće sastajalište.
3. **Odziv.** B provjeri poziv (§5.3), osvježi STUN i vrati potpisani
   `odziv` s `odluka`:
   - `probaj`;
   - `posrednik` (B zna da neće uspjeti: UDP blokiran);
   - `odbij` (zauzet, previše sesija, sat).

   Ako su oba `eim = ne`, A ide ravno na korak 7.
4. **Sinkro (po DCUtR-u [V]).** A izmjeri RTT od slanja poziva do odziva i
   oduzme `obradaMs` koju B javi u odzivu (B-ovo STUN osvježavanje ne smije
   napuhati RTT). A šalje `sinkro{rttMs}`. B počinje udarati odmah po
   primitku, A nakon RTT/2.
5. **Udarci.** Obje strane šalju udarce na kandidate iz **potpisane** poruke
   druge strane, u nasumičnim razmacima 10–200 ms, najviše 3 s po pokušaju.
   Na valjan udarac s adrese X strana pamti X (peer-reflexive, kao ICE) i
   vraća odgovor jednake veličine na X.
   - Pokušaja: 3 ako su obje strane `eim = da`, inače 1. Između pokušaja
     1 s, svaki s novim `sinkro`.
6. **QUIC.** Prvi valjan udarac ili odgovor otvara rukovanje. **Zove strana
   s leksikografski manjim javnim ključem**, druga prima na istom
   Transportu. Ako ipak nastanu dvije sesije, ostaje ona koju je otvorio
   manji ključ.
   - TLS: `tlsConfig(priv).Clone()`, ALPN `gocop-veza/1`, `VerifyConnection`
     → `trusted(ključ)`; pozivatelj traži i `ključ == B`; primatelj traži
     da ključ pripada aktivnoj sesiji.
   - **`Dial` bez greške nije prijem.** U TLS 1.3 klijent završi prije nego
     poslužitelj provjeri njegov certifikat; nečlan dobije grešku tek na
     prvom čitanju [T]. Sesija je primljena tek kad stigne `pozdrav` druge
     strane na toku 0x00.
   - Na toku 0x01 teče **današnji razgovor razmjene, bajt za bajtom**.
7. **Posrednik** (§4.4) ako: moj UDP je blokiran, B je odgovorio
   `posrednik`, obje strane `eim = ne`, ili svi pokušaji probijanja i QUIC
   nisu uspjeli. Relej se **ne otvara usporedno** s probijanjem.
8. **Stari put.** Ako ni posrednik ne uspije: stari tunel kad je B
   tunelski čvor, inače neuspjeh do sljedećeg kruga.

**Rokovi (usklađeni):** STUN ≤ 2 s + signalizacija ≤ 5 s + 3 × (3 s + 1 s)
+ QUIC 5 s ≈ 24 s; posrednik ≤ 10 s. Ukupni rok novog puta u `SyncWith`
je **45 s**. Kad je cilj sam sastajalište (Unraid), probijanje ima rok 8 s,
a onda ide stari tunel.

**Negativna pričuva:** nakon 3 neuspjeha zaredom prema B, 30 min ravno na
posrednika ili stari put (udvostručuje se do 6 h). Briše se kad se promijeni
moja javna adresa ili vrsta NAT-a.

**Sesija živi** dok ima tokova i još 120 s nakon zadnjeg. `KeepAlivePeriod`
15 s drži NAT preslikavanje otvorenim (kućni usmjernici: istek 30–691 s,
medijan 90 s [V]; RFC 8445 Tr ≥ 15 s [V]). Sljedeći razgovor unutar 120 s
ne probija ponovno.

**Promjena mreže** (laptop ode od kuće, buđenje): novi netcheck, nova
prijava, probijene sesije se zatvaraju, negativna pričuva se briše.

### 4.4 Posrednik

1. A pošalje `spoj{sesija, za: B}` kroz signalizaciju.
2. R provjeri: oba prijavljena i `trusted()`, ograde, dnevni proračun. Izda
   dva jednokratna žetona (32 B, rok 30 s), vezana uz (A, B, sesija, uloga).
3. Obje strane otvore WebSocket na `https://<R>/razmjena/spoj`. Prvi binarni
   okvir je `{"zeton":"…"}` (≤ 256 B, rok 5 s). Žeton ide u okvir, ne u URL,
   da ne završi u dnevnicima zahtjeva.
4. Kad stignu obje noge, R ih spoji `io.Copy` u oba smjera i samo broji
   bajtove. Svaka noga ima vlastiti identitet u registru (današnji
   `wsVeza.RemoteAddr` vraća konstantu za sve veze [V, tunel.go:131], pa se
   za to ne smije koristiti).
5. Unutra: A je TLS klijent s `expect = B`, B je TLS poslužitelj s
   `trusted()` i provjerom da je ključ A iz sesije. Zatim
   `razmjena.Conn{Put: "posrednik"}` i nepromijenjen `exchange()` (isti kod
   kao `/razmjena/tunel`).
6. Ping na svaku nogu svakih 25 s (`websocket.Conn.PayloadType =
   PingFrame` u `x/net/websocket`; potvrditi u F4). Bez prometa 120 s →
   prekid.

**Pravilo sadržaja preko posrednika** (plan §9, uvjeti Cloudflarea): obje
strane znaju `Put == "posrednik"`. Knjiga verzija (delta ≤ 32 MiB) ide
normalno. Sadržaj: samo stavke ≤ 1 MiB, ukupno ≤ 8 MiB po razgovoru;
`zeljeniOtisci` i `sadrzajZa` poštuju istu granicu. Paketi arhive i veći
prilozi čekaju LAN, izravni ili probijeni put.

**Iskreno ograničenje:** za knjigu verzija posrednik daje malo više od
današnje razmjene s Unraidom, koji je ionako punopravni član. Vrijedi kad A
treba sadržaj koji Unraid po pretplati ne drži, i za svježinu (bez dva kruga
od 5 min). Zato je zadnja faza i može se odgoditi.

---

## 5. Poruke i polja

### 5.1 Signalizacija (`/razmjena/signal`)

- Vanjski sloj: WebSocket, binarni okviri (`x/net/websocket`, kao tunel).
- Unutarnji sloj: TLS 1.3 s `tlsConfig` + `NextProtos = ["gocop-signal/1"]`.
  Poslužitelj provjeri `trusted(ključ)` prije prvog čitanja; prazan ALPN →
  zatvori.
- Poruke: postojeća `razmjena.Envelope{Kind, Reason, DeviceID, Payload}`,
  jedna JSON vrijednost po poruci, **najviše 64 KiB**, rok čitanja 75 s.
  Nepoznata vrsta se zanemaruje i jednom zapiše (novi kanal, pa to pravilo
  vrijedi od prvog dana).

| vrsta | smjer | sadržaj |
|---|---|---|
| `prijava` | čvor → R | `{"ver":1,"program":"0.0.30-alfa","mogucnosti":["veza/1"],"udp":true,"nat":{"eim":"da","cuvaPort":true,"udpBlokiran":false,"stun":"cloudflare","rttMs":11}}` (nepotpisano; identitet daje TLS) |
| `prijavljen` | R → čvor | `{"ver":1,"sastajaliste":"<ključ R>","mogucnosti":["sastajaliste/1","posrednik/1"],"vidjenaIP":"93.142.240.240","vrijeme":1790990000,"pulsSek":25}` |
| `puls` | oba | prazno; svakih 25 s |
| `poziv` | A → R → B | potpisano tijelo (§5.2) |
| `odziv` | B → R → A | potpisano tijelo (§5.2) |
| `sinkro` | A → R → B | potpisano tijelo `{ver, sesija, od, za, sastajaliste, izdano, rttMs}` |
| `nema` | R → A | `{"sesija":"…","razlog":"nije prijavljen"│"nije član"│"previše poziva"│"posrednik isključen"}` |
| `spoj` | A → R | `{"sesija":"…","za":"<B>"}` |
| `spoj_zeton` | R → A, R → B | `{"sesija":"…","zeton":"<32 B b64>","uloga":"zove"│"prima","rok":30}` |
| `odbijeno` | bilo koji | postojeća `VrstaOdbijeno` s `Reason` |

`vidjenaIP` je `CF-Connecting-IP`, uzet samo od pouzdanog posrednika
(`klijentIz`). Služi za prikaz, ne za odluke. `vrijeme` služi za procjenu
pomaka sata (pločica upozorava iznad 2 min).

### 5.2 Potpisane poruke (poziv, odziv, sinkro)

Oblik poruke koja ide drugom čvoru:

```json
{"kind":"poziv","payload":{
  "tijelo":"<base64(JSON)>",
  "potpis":"<base64(ed25519(ključ pošiljatelja, \"gocop-poziv-v1|\" || tijelo))>"}}
```

Potpisuju se **bajtovi tijela**, ne preoblikovani JSON, pa nema kanonskog
oblika ni rizika da ga preoblikovanje slomi. R čita tijelo (treba `za` i
`sesija`), ali ga prosljeđuje netaknuto. Kontekstni prefiksi:
`gocop-poziv-v1|`, `gocop-odziv-v1|`, `gocop-sinkro-v1|`.

Tijelo poziva:

```json
{"ver":1,
 "sesija":"<32 hex, 16 B nasumično>",
 "od":"<ključ A, base64>", "za":"<ključ B>", "sastajaliste":"<ključ R>",
 "izdano":1790990000,
 "tajna":"<32 B nasumično, base64>",
 "kandidati":[{"vrsta":"javna","adresa":"93.142.240.240:4713"},
              {"vrsta":"lokalna","adresa":"192.168.1.23:4713"}],
 "nat":{"eim":"da","udpBlokiran":false}}
```

Tijelo odziva: isto (`od` = B, `za` = A), uz `"odluka":"probaj"│"posrednik"│
"odbij"`, `"razlog"` i `"obradaMs"`.

**Kandidati:**
- `javna`: STUN XOR-MAPPED-ADDRESS;
- `lokalna`: adrese sučelja (RFC 1918), bez loopbacka i link-locala, bez
  sučelja `docker*`, `br-*`, `veth*`, `utun*`, `tailscale*`; isključivo
  postavkom `lan_kandidati = false`;
- Unraid u Dockeru objavljuje `lan_adresa` (npr. `192.168.1.2`) umjesto
  `172.17.x`;
- najviše 8; odbacuju se multicast, broadcast, nespecificirane i loopback
  adrese te port 0.
- Kandidata koji bi sastajalište dopisalo iz `CF-Connecting-IP` **nema**:
  bio bi nepotpisan i bez poznatog porta.

**Provjera kod primatelja:**
- `od` je `trusted()` (članstvo valjano sada) i potpis vrijedi;
- `za` je moj ključ, `sastajaliste` je R preko kojeg je stiglo (ne može se
  ponoviti kod drugog sastajališta);
- `|sada − izdano| ≤ 10 min` (uredska i terenska računala znaju odstupati);
- `sesija` nije viđena. Pričuva viđenih sesija drži zapis dok ne prođe
  `izdano + 10 min`, dakle pokriva cijeli prozor; najviše 8192 zapisa, a
  kad je puna, novi pozivi se odbijaju („previše"), ne izbacuju se stari.

**R ne prepisuje ništa.** Ako `od` u tijelu nije ključ dokazan u TLS-u, R
poruku odbacuje i bilježi kao sigurnosni događaj.

### 5.3 Udarac (paket probijanja), 60 B

```
[0]      0x2A        oznaka (gornja 2 bita 00 → quic-go ga ne uzima kao QUIC)
[1]      0x01        verzija
[2]      tip         1 = udarac, 2 = odgovor
[3]      0x00        rezerva
[4:20]   sesija      16 B
[20:28]  brojač      uint64, raste po pošiljatelju
[28:60]  HMAC-SHA256(K, bajtovi 0..27)
K = HKDF-SHA256(tajnaA || tajnaB, info = "gocop-udarac-v1" || sesija)
```

Prihvaća se ako je sesija aktivna, HMAC ispravan i brojač veći od zadnjeg.
Odgovor je iste veličine, pa nema pojačanja. Najviše 20 paketa/s i 60 po
pokušaju.

### 5.4 QUIC sesija i kanali

**`quic.Config`:** `HandshakeIdleTimeout` 5 s, `MaxIdleTimeout` 60 s,
`KeepAlivePeriod` 15 s, `MaxIncomingStreams` 8, `MaxIncomingUniStreams` 0,
`InitialPacketSize` 1200, `Allow0RTT` false, `EnableDatagrams` false.
`StatelessResetKey` = HKDF(ključ čvora, "gocop-quic-reset-v1").

**Kanali.** Prvi bajt svakog toka je broj kanala:

| bajt | kanal | sadržaj |
|---|---|---|
| `0x00` | kontrola | otvara ga pozivatelj; poruke `Envelope` s prefiksom duljine od 4 B, ≤ 64 KiB: `pozdrav`, `ping`, `zatvaram` |
| `0x01` | razmjena | današnji razgovor (frontier, delta, zelje, sadrzaj, done) preko `razmjena.Conn`; jedan razgovor po toku; troši mjesto iz **iste ograde od 4 razmjene** kao port i tunel |
| `0x02` | sadrzaj | rezervirano za roj |
| `0x03` | web | rezervirano; zahtjev bi se uvijek označio kao vanjski, nikad po IP-u |
| ostalo | — | reset kodom `0x0101` |

`pozdrav` (oba smjera, prva poruka na 0x00):

```json
{"kind":"pozdrav","payload":{"verzija":1,"program":"0.0.30-alfa",
 "mogucnosti":["veza/1","razmjena/1"],"kanali":[0,1],"put":"probijeno"}}
```

Kanal koji druga strana nije navela u `kanali` se ne otvara. Tako se
`sadrzaj/1` kasnije uvodi bez nove vrste poruke usred razgovora.

**Kodovi grešaka aplikacije:** `0x0100` opće, `0x0101` nepoznat kanal,
`0x0102` zauzet, `0x0103` odbijeno, `0x0104` nije član / opozvan, `0x0105`
proračun posrednika.

**Prilagodba `razmjena.Conn`:** `raw *tls.Conn` postaje sučelje `prijenos`
(Read, Write, Close, dva rokova, RemoteAddr). Za QUIC tok `Close` = `Close` +
`CancelRead`, jer quic-go `Close` zatvara samo slanje [V]. `newConnKljuc(p,
ključ)`: ključ dolazi iz veze, ne iz toka. Nova polja `Put` i `RTT`.
`serveTLS` se razdvaja na zajednički `primiVezu(conn)`.

**`noteSync`** (peers.go:1007 [V]) danas upisuje host iz `c.RemoteAddr()` u
`Peer.Addresses`, koji putuje knjigom. Mora **izričito preskočiti svaku vezu
čiji `Put` nije `lan` ili `izravno`**. Pseudo-adresa nije zaštita: `p2p:<id>`
`net.SplitHostPort` prihvaća bez greške (host `p2p`). Probijene i
posredničke adrese idu samo u lokalni adresar.

---

## 6. Sigurnost

### 6.1 Tko smije što

| radnja | uvjet |
|---|---|
| otvoriti signalnu vezu | unutarnji TLS, `trusted(ključ)`, ALPN `gocop-signal/1` |
| poslati `poziv` / `spoj` | prijavljen, `trusted`, ograde brzine |
| primiti proslijeđen `poziv` | R provjerio oba; B ponovno provjeri `trusted(od)`, potpis, `za`, `sastajaliste`, vrijeme, sesiju |
| započeti QUIC rukovanje prema čvoru | izvorna adresa provjerena Retryjem i u zadnjih 30 s poslala valjan udarac, ili je kandidat aktivne sesije, ili LAN adresa iz beacona |
| sesija primljena | TLS ključ `trusted()` (+ `== B` na strani pozivatelja), stigao `pozdrav` |
| otvoriti nogu posrednika | jednokratni žeton iz signalizacije |
| razmjena na bilo kojem putu | isti `trusted()` + ograda od 4 razmjene kao danas |

**Vrata za nepozvane na UDP 4713.** `quic.Transport.VerifySourceAddress`
vraća `true` za svaku novu vezu (Retry, jedan RTT više). Tek tada
`Transport.ConnContext` (postoji u v0.63.0, zove se na Initial paketu prije
rukovanja [V]) vidi `ClientInfo.AddrVerified = true` i odbija adresu koja
nije u skupu iznad. Bez Retryja je `RemoteAddr` laživ. Redoslijed poziva
`VerifySourceAddress` → `ConnContext` treba potvrditi u izvorniku prije F3.
Tako stranac s interneta ne može ni započeti TLS rukovanje.

**Duge sesije i opoziv.** Svaki čvor svakih 5 min i odmah pri primitku
opoziva u knjizi ponovno provjerava `trusted()` za sve žive sesije.
Opozvani dobiva `0x0104`. R ponovno provjerava pri svakom pulsu, pa opozvani
ispadne iz registra najkasnije za 25 s.

**Zamke:**
- QUIC i posrednik nisu web zahtjevi: ne prolaze kroz `klijentIz`, ne
  dobivaju „lokalna" prava, ne ulaze u `ZadaniPosrednici`.
- Privatna izvorna adresa nije povjerenje; samo ključ jest.
- `Dial` bez greške nije prijem (§4.3, korak 6).

### 6.2 Ograničenja (DoS)

| površina | granica |
|---|---|
| web `/razmjena/signal` | 64 WS ukupno, **8 po IP-u** (cijeli ured je jedna IP); zasebno od tunela (32 / 2) |
| unutarnji TLS signalizacije | rukovanje 5 s, 16 istodobnih |
| signalna poruka | 64 KiB; najviše 20 poruka/s po vezi, inače prekid |
| registar | 256 prijava; jedna veza po ključu (nova zatvara staru) |
| pozivi | 6/min po A; 2 istodobne sesije prema istom B; 8 nedovršenih prema B; sesija u registru 30 s |
| B: dolazne sesije u uspostavi | 4 istodobne |
| udarci | ≤ 20 pkt/s, ≤ 60 po pokušaju, samo na potpisane kandidate, odgovor iste veličine |
| QUIC | Retry za sve; 8 istodobnih rukovanja; ≤ 32 sesije ukupno, 1 po čvoru; 8 tokova po sesiji |
| web `/razmjena/spoj` | 16 WS ukupno, 4 po IP-u |
| posrednik | 8 sesija; 64 MiB i 15 min po sesiji; 120 s mirovanja; ukupno 4 MB/s; **2 GiB dnevno** (postavka) |
| sadržaj kroz posrednika | stavka ≤ 1 MiB, ≤ 8 MiB po razgovoru |
| razmjene | i dalje 4 istodobne, zajedničke svim putovima; 256 MiB po poruci, 32 MiB po delti |

### 6.3 Ponavljanje i krivotvorenje

- **Poziv, odziv, sinkro:** potpis ključem čvora nad bajtovima tijela,
  `sastajaliste` u tijelu, `izdano` ± 10 min, pričuva sesija koja pokriva
  cijeli prozor.
- **R ne može podmetnuti kandidate** ni natjerati A da udara pakete na treću
  adresu. Može samo odbaciti ili zakasniti, što bi mogao i bez toga.
- **Udarac:** HMAC vezan uz sesiju i tajne obiju strana, brojač raste,
  sesija ≤ 30 s. R zna tajne (prolaze kroz njega) i mogao bi krivotvoriti
  udarac. Najviše pokrene QUIC prema adresi koju je potpisao sam B, i taj
  QUIC padne na provjeri ključa. Prihvaćeno, jer je R član.
- **Žeton posrednika:** jednokratan, 30 s, vezan uz ulogu. Cloudflare ga
  vidi (vanjski TLS završava kod njega); posljedica je najviše prekid jedne
  sesije, jer unutarnji TLS traži ključ čvora.
- **QUIC:** bez 0-RTT, pa nema ponavljanja ranih podataka.

### 6.4 Metapodaci

| tko | vidi | ne vidi |
|---|---|---|
| Cloudflare | postojanje signalnih veza i nogu posrednika, veličine, ritam, žeton posrednika | tko koga zove, kandidate, sadržaj |
| sastajalište (član mreže) | tko je na vezi, tko koga zove, javne i lokalne adrese kandidata, količinu posredničkog prometa | otiske, kanale, pretplate, sadržaj |
| STUN (Google, Cloudflare) | javnu IP adresu čvora i ritam upita | ID čvora, ništa drugo |
| knjiga verzija | samo `mogucnosti`, `dohvatljivost`, `posluzujeSadrzaj`, `prioritetSastajalista` | IP adrese, NAT, tko s kim |

Javne IP adrese drugih čvorova (kućne adrese zaposlenika) **ne idu u
dnevnik** na razini info; vide se samo administratoru na pločici. Ako se
ikad uvede VPS kod treće strane kao sastajalište, to treba navesti u
dokumentaciji o zaštiti podataka.

---

## 7. Kompatibilnost i uvođenje

**Što se ne mijenja:** `/razmjena/tunel`, TCP 4710, uparivanje (4711), LAN
otkrivanje (UDP 4712), potvrde članstva (`signedBytes` v1) i razgovor
razmjene, bajt za bajtom. TLS na TCP-u ne dobiva `NextProtos`.

| A \ B | 0.0.27 | novi |
|---|---|---|
| **0.0.27** | kao danas | kao danas (novi B poslužuje stare putove nepromijenjeno) |
| **novi** | kao danas: B nema `veza/1` u zapisu ili nije prijavljen → stari put | probijanje / posrednik |

**Dogovor sposobnosti:** `mogucnosti` u zapisu čvora (ima li smisla
pokušati); 404 na novom putu; ALPN `gocop-signal/1` i `gocop-veza/1` (prazno
→ stari put; `gocop-veza/2` kasnije ide u isti popis, bira se najviša
zajednička); `pozdrav.kanali`.

**Beacon:** `Meta["quic_port"]` (Meta je već `map[string]string`,
discovery.go:31 [V]). Stari čvorovi polje zanemaruju.

**Izdanja (okvirno; pravilo plana: stalni čvor prvi, odmah zatim ostali; 0.0.26 i 0.0.27 su izdanja s PIN-om i ovlastima, pa protokol počinje od 0.0.28):**

| izdanje | sadržaj | zadano |
|---|---|---|
| **0.0.28** | F0 + F1: prerada `Conn`, `Put`, ispravak `noteSync`; UDP utičnica, STUN, netcheck, blok „Dohvatljivost", gumb „Provjeri vezu". Bez promjene ponašanja razmjene. | utičnica uključena, periodični netcheck uključen samo na Unraidu i laptopu; ostali samo na gumb |
| **0.0.29** | F2: `/razmjena/signal`, prijava, puls, potpisani poziv/odziv, `mogucnosti` u zapisu čvora | `sastajaliste = true` samo na Unraidu; `probijanje = false` |
| **0.0.30** | F3: QUIC sesija, kanali, udarci, sinkro, vrata, novi redoslijed u `SyncWith`, LAN QUIC | `probijanje = false`; ručno uključiti na Unraidu, zatim laptopu |
| **0.0.31** | F4: posrednik, žetoni, proračuni, pravilo sadržaja | `posrednik = true` samo na Unraidu |
| **0.0.32** | F5: terenska ugađanja | `probijanje = true` zadano, kad pločica dva tjedna nema grešaka |

Uredski čvorovi dolaze zadnji i tek nakon dogovora s IT-om Hrvatskih voda
(§10).

**Unraid (Docker):** host mreža ili `-p 4713:4713/udp`; `lan_adresa =
"192.168.1.2"`; `QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING=1` ili veći
`net.core.rmem_max`/`wmem_max`. Na kućnom usmjerivaču se **ništa ne
otvara**.

**Povratak:** `probijanje = false` vraća čvor na ponašanje 0.0.27 bez
tragova u knjizi (polja `mogucnosti` ostaju, ali bez `veza/1`).

**Postavke (`gocop.toml`):**

```toml
[povezivost]
probijanje    = false     # sudionik: UDP utičnica, signalizacija, QUIC
udp_port      = 4713      # 0 = isključeno; zauzet → nasumični port
stun          = ["stun.cloudflare.com:3478", "stun.l.google.com:19302"]  # nasumično prvi, drugi rezerva
lan_kandidati = true
lan_adresa    = ""        # Docker: adresa domaćina u LAN-u
sucelja_iskljuci = ["docker*", "br-*", "veth*", "utun*", "tailscale*"]
sastajalista  = []        # ručno, uz ona iz knjige, npr. "https://copB.voda.hr"
sastajaliste  = false     # poslužuj /razmjena/signal
prioritet_sastajalista = 50
posrednik     = false     # poslužuj /razmjena/spoj
posrednik_mib_dan     = 2048
posrednik_sesija_mib  = 64
posrednik_sadrzaj_mib = 8
```

---

## 8. Što se vidi na pločici

**Novi blok „Dohvatljivost ovog čvora"** (dodana polja u `Status`,
`omitempty`):
- UDP: radi na :4713 / isključen / blokiran;
- NAT: „neovisan o odredištu (dobar za probijanje)" / „ovisan o odredištu
  (ide preko posrednika)" / „nepoznat"; uz to „čuva port";
- STUN: koji je odgovorio i RTT („Cloudflare 11 ms, Google 33 ms");
- javna adresa: samo administratoru; ostalima „poznata";
- „više izlaza" kad se STUN IPv4 i `vidjenaIP` IPv4 razlikuju (samo
  natpis);
- pomak sata prema sastajalištu, upozorenje iznad 2 min;
- sastajalište: „prijavljen kod cop-osijek.com od 12:01" / „nije dostupno
  (razlog)" / „nijedno";
- na sastajalištu: broj prijavljenih, poziva u zadnjem satu, sesija
  posrednika, MiB danas prema proračunu.

**Po čvoru** (`PeerStatus`, dodano): `put`, `rtt_ms`, `put_od`,
`put_razlog` („probijanje nije uspjelo: oba NAT-a ovisna o odredištu",
„posrednik — veliki sadržaj čeka"), `mogucnosti`. `SamoDolazi` se računa
samo za stari put.

**Upozorenja:** „UDP blokiran — veze idu preko posrednika", „sastajalište
nedostupno 10 min", „dnevni proračun posrednika potrošen", „čvor X nema
veza/1 (stariji program)".

**Dnevnik:** prefiks `povezivost:`, hrvatski, jedan redak po prijelazu,
s ograničenjem učestalosti. Primjer: `povezivost: probijanje s ured-osijek
uspjelo u 2. pokušaju (318 ms), put probijeno`. Bez žetona, tajni, potpisa
i tuđih javnih IP adresa.

**Administracija:** gumb „Provjeri vezu" (netcheck odmah) i „Provjeri vezu
s čvorom" (pokušaj uspostave s ispisom koraka: STUN, poziv, odziv, udarci po
kandidatu, QUIC, put, RTT). Brojači: pokušaji i uspjesi po vrsti puta,
medijan vremena do veze, MiB kroz posrednika dnevno.

---

## 9. Priprema za roj

**Što protokol već ispunjava** (zahtjevi iz istraživanja roja):

| zahtjev | kako |
|---|---|
| svaki put završava u istoj autentificiranoj sesiji s ključem | QUIC sesija (LAN, probijeno) i TLS (posrednik, tunel) daju `razmjena.Conn` s istim ključem i `trusted()` |
| gornji sloj zna vrstu puta i RTT | `Put`, `RTT` na vezi |
| sesije su simetrične | tko god je zvao, obje strane smiju tražiti i služiti; laptop iza NAT-a služi sadržaj |
| svrha se dogovara prije prvog bajta | ALPN + `pozdrav.kanali`; prazan ALPN = stari razgovor |
| neovisni usporedni tokovi | QUIC tokovi s vlastitom kontrolom toka; 48 MB prijenos ne blokira knjigu |
| sadržaj ima svoj proračun | kanal 0x02 dobit će vlastitu ogradu, odvojenu od 4 razmjene |
| pravilo posrednika | `Put == "posrednik"` → samo knjiga i mali sadržaj |
| sastajalište vidi samo „A želi B" | nema otisaka, kanala ni pretplata u signalizaciji |
| bilo koji javni čvor može biti sastajalište | `sastajaliste/1` + `prioritetSastajalista`; copB/VPS bez promjene koda |
| grube činjenice u zapisu čvora | `mogucnosti`, `dohvatljivost`, `posluzujeSadrzaj` |
| podizanje puta | nova sesija na boljem putu preuzima nove tokove; stara se zatvara kad ostane prazna (quic-go ne seli vezu: `AddPath` mijenja samo lokalnu stranu [V]) |

**Što ostaje za F7 (zaseban zapis):**
- kanal `sadrzaj/1`: binarni okviri ≤ 1 MiB, zahtjev `{id, otisak, pomak,
  duljina}`, više zahtjeva u letu uz ogradu bajtova, `otkazi`; odgovori
  `podaci`, `nemam`, `odbijeno`, `zauzet{ponoviZa}`. Pada granica od 64 MiB
  po stavci i base64;
- ID sadržaja ostaje SHA-256 cijele datoteke; neobavezni opisnik dijelova
  (`velicinaDijela` 1 MiB = `sadrzaj.VelicinaDijela`), `otisakDijelova` u
  novim referencama;
- tablica provjerenih dijelova i čitanje raspona; stanje po `(otisak, dio)`
  preživljava promjenu puta;
- „tko što ima" samo u sesiji: Bloom sažetak na početku (oko 1,5 KB za 1225
  stavki), poruke `ima`, kratka negativna pričuva za `nemam`;
- ovlast iz ključa sesije i članstva, ne iz samoprijavljenih `Wants`;
- izbor najmanje zauzetog izvora; Unraid se javlja kao zadnji izbor; LAN bez
  ograde slanja;
- otisak koji dohvaća `sadrzaj/1` ne ide u stari `zelje`.
- Ako roj zatraži kanale i preko posrednika: QUIC paketi kao WebSocket
  okviri na zasebnoj nozi `/razmjena/spoj` (dogovor u prvom okviru), nikad
  na signalnoj vezi, s odbacivanjem kad je međuspremnik pun.

---

## 10. Otvorena pitanja

0. **QUIC ili WebRTC (DTLS) nad probijenim putem.** Korisnik je tražio
   standardni WebRTC model, da promet izgleda kao videopozivi koji u uredu
   rade (Teams, Zoom, Skype) i da se IT-u otvoreno kaže što je. Dio za
   probijanje (STUN, istodobni udarci, kandidati) jednak je WebRTC-ovom ICE-u.
   Za prijenos nakon probijanja suci su izabrali QUIC, ne DTLS/SCTP: koristi
   naš postojeći TLS 1.3 s ključevima čvorova, daje tokove, i dodaje jednu
   ovisnost umjesto pion paketa. QUIC je standard (HTTP/3), ali neki
   korporativni vatrozidi ga namjerno blokiraju da bi pregledavali promet na
   TCP-u. Provjera u ponedjeljak: u uredu otvoriti google.com u Chromeu,
   DevTools → Network → stupac Protocol; „h3" znači da QUIC prolazi. Ako ne
   prolazi, faza F3 umjesto QUIC-a koristi DTLS (pion/dtls) nad istom
   utičnicom; ostale faze se ne mijenjaju. Prerušavanje u promet Teamsa ili
   Zooma ne dolazi u obzir (odluka korisnika).
1. **Ured (ponedjeljak).** U uredu bez problema rade Tailscale, Teams, Zoom
   i Skype, pa je odlazni UDP gotovo sigurno otvoren; ostaje pitanje vrste
   NAT-a. `tailscale netcheck` (vrsta preslikavanja, UDP)
   i `tailscale ping` / `tailscale status` (izravno ili DERP) iz ureda, uz
   naš netcheck iz 0.0.28. Odluka:
   - izravno, EIM → probijanje ured ↔ vani vrijedi, redoslijed faza ostaje;
   - DERP ili simetrično → ured ↔ vani ide preko posrednika; F4 dobiva
     važnost, a veliki paketi za ured idu samo LAN-om dok ne bude copB.
     Probijanje i dalje vrijedi za laptop ↔ Unraid i laptop ↔ laptop.
   - UDP potpuno blokiran → isto, a `probijanje` u uredu ostaje isključeno.
2. **HTTP proxy u uredu.** `x/net/websocket` dialer ne poštuje
   `HTTPS_PROXY` [V, tunel.go:157–161]. Ako tunel iz ureda danas radi,
   radit će i signalizacija. Ako ured traži proxy, treba proxy-svjestan
   dialer (`http.ProxyFromEnvironment` + CONNECT), i za tunel i za
   signalizaciju.
3. **IT politika Hrvatskih voda.** Aplikacijski UDP prema javnim adresama sa
   službenih računala može izgledati kao zaobilaženje vatrozida (NIS2).
   Prije `probijanje = true` na službenim laptopima treba dogovor; do tada
   samo posrednik i LAN.
4. **Unraid u Dockeru.** Host mreža ili mapiranje 4713/udp; čuva li
   Dockerov MASQUERADE i kućni usmjerivač EIM — mjeri 0.0.28.
5. **quic-go detalji prije F3:** redoslijed `VerifySourceAddress` →
   `ConnContext` i `AddrVerified` nakon Retryja; ponašanje
   `ReadNonQUICPacket` pod opterećenjem.
6. **WebSocket ping** preko `PayloadType = PingFrame` u `x/net/websocket` —
   potvrditi testom u F4; inače ostaje rok od 120 s.
7. **Stvarni rok neaktivnosti Cloudflarea** — izmjeriti 24 h signalizacije
   s brojanjem prekida.
8. **Treba li sastajalište znati tajne udaraca.** Može se izbjeći
   šifriranjem tajne javnim ključem druge strane (X25519 iz ed25519). Za v1
   prihvaćeno, jer je R član i udarac ne otvara ništa bez ključa.
9. **IPv6** (zasebna `udp6` utičnica i kandidati) i **predviđanje porta**
   za tvrdi NAT s jedne strane (1024 probe ≈ 98 % [V]) — poslije, ako
   mjerenja pokažu potrebu.
10. **copB.voda.hr / VPS** — kad postoji: `sastajaliste = true`, viši
    prioritet, vlastiti STUN s dvije adrese (RFC 5780, test filtriranja),
    posrednik na UDP-u bez Cloudflareovih uvjeta.

---

## 11. Procjena rada

| faza | sadržaj | dani | samostalno isporučivo |
|---|---|---|---|
| **F0 prerada** | sučelje `prijenos` u `Conn`, `newConnKljuc`, `primiVezu`, zajednički rep `DialExchange`/`DialTunel`, `Put`/`RTT`, ispravak `noteSync` s testom | 1–1,5 | da, bez promjene ponašanja |
| **F1 STUN i netcheck** | utičnica s `quic.Transport` (još bez QUIC slušalice), stalni čitač, vlastiti STUN, netcheck, blok „Dohvatljivost", gumb | 2–3 | da: stvarni podaci iz ureda, s mobilne mreže i Unraida |
| **F2 signalizacija** | `/razmjena/signal`, ograde, registar, prijava/puls/backoff, potpisani poziv/odziv/sinkro, pričuva sesija, `mogucnosti` u zapisu čvora | 3–4 | da: pločica pokazuje tko je „na vezi" |
| **F3 sesija i probijanje** | quic-go, sesija s kanalima 0x00/0x01, pozdrav, udarci, sinkro, vrata (Retry + ConnContext), strojevi stanja, adresar, `SyncWith` redoslijed, LAN QUIC, ponovna provjera `trusted()`, NAT simulator | 6–7 | da: glavni dobitak |
| **F4 posrednik** | `/razmjena/spoj`, žetoni, spajanje, proračuni, pravilo sadržaja, ping | 2–3 | da; može se odgoditi |
| **F5 teren i dokumentacija** | mjerenja, ugađanje rokova, `plan-povezivost.md` §3 i korak 8, govulncheck s quic-go | 2–3 | — |
| **ukupno** | | **16–21,5 radnih dana** | |
| F7 (poslije) | roj: `sadrzaj/1`, dijelovi, nastavak, `ima`/Bloom, izbor izvora | 2–3 tjedna | zaseban zapis |

Oko 2.300 redaka proizvodnog koda i oko 1.000 redaka testova.

---

## 12. Ispitivanje

**Jedinični testovi:**
- STUN: testni vektori RFC 5769; XOR-MAPPED-ADDRESS IPv4/IPv6; odbijanje
  krivog izvora i transakcijskog ID-a; ponavljanja i rokovi;
- netcheck: EIM, `nepoznato`, `udpBlokiran` iz lažnih odgovora; razlika
  `vidjenaIP` ne mijenja `eim`;
- udarac: ispravan i krivi HMAC, stari brojač, nepoznata sesija; prvi bajt
  nikad ne izgleda kao QUIC ili STUN;
- potpis: tijelo izmijenjeno na R, krivotvoren `od`, krivo `sastajaliste`,
  `izdano` izvan prozora, ponovljena sesija, puna pričuva;
- kandidati: loopback, multicast, port 0, više od 8, filtar sučelja;
- `newConnKljuc` nad QUIC tokom (Close + CancelRead); nepoznat kanal →
  `0x0101`;
- **`noteSync` ne upisuje ništa za `Put` probijeno, posrednik, tunel**
  (uključujući `p2p:`-oblik adrese).

**Fuzz:** dekoder signalnih poruka (64 KiB), udarac, STUN parser, okvir
kontrolnog toka.

**NAT simulator** (`internal/razmjena/natsim_test.go`, bez root-a i mreže):
`net.PacketConn` u memoriji (quic-go ga prima), usmjernik-gorutina, NAT s
podesivim preslikavanjem (EIM / ovisno o adresi / ovisno o adresi i portu),
filtriranjem, istekom (lažni sat ili `testing/synctest`), čuvanjem porta,
hairpinom, blokiranim UDP-om, gubitkom i kašnjenjem. Dva lažna STUN
poslužitelja, pravo sastajalište nad `httptest` WebSocketom, pravi QUIC.
Alternativa: `pion/transport/v5` vnet (`NATType` s
`EndpointIndependent` / `EndpointAddrDependent` / `EndpointAddrPortDependent`)
samo u `_test` datotekama — prije toga provjeriti što povlači u `go.mod`.

| A \ B | EIM, neovisno filtriranje | EIM, filtriranje po adresi i portu | simetrično | UDP blokiran |
|---|---|---|---|---|
| EIM, neovisno | probijeno (1. pokušaj) | probijeno | probijeno (peer-reflexive) | posrednik |
| EIM, po adresi i portu | probijeno | probijeno (istodobno slanje) | posrednik | posrednik |
| simetrično | probijeno (peer-reflexive) | posrednik | posrednik, bez 3 uzaludna pokušaja | posrednik |

Dodatno: 20 % gubitka → uspjeh unutar 3 pokušaja; istek preslikavanja 30 s
i razmjena 2 min → keepalive drži; dva čvora iza istog NAT-a bez hairpina →
lokalni kandidat; cilj je sam sastajalište → probijeno ili stari tunel za 8 s.

**Sigurnosni scenariji:**
- stranac šalje QUIC Initial na 4713 → Retry, zatim odbijen u `ConnContext`;
  lažirana izvorna adresa ne prolazi Retry;
- nečlan s valjanim TLS-om → `Dial` prođe, `pozdrav` ne stigne, prvo čitanje
  padne, `Accept` ga ne preda;
- zlonamjerno sastajalište mijenja kandidate ili `od` → odbačeno;
- opozvan član usred sesije → `0x0104` najkasnije za 5 min ili odmah pri
  primitku opoziva; iz registra R za 25 s;
- ponovljeni udarci, sinkro i žetoni → odbačeni;
- posrednik ne šalje ništa izvan dviju nogu iste sesije; svaka noga ima
  vlastiti identitet;
- poplava poziva i rukovanja → ograde iz §6.2 drže, ostali čvorovi rade;
- goleak / broj gorutina nakon zatvaranja sesija i signalnih veza.

**Usklađenost:**
- postojeći `razmjena_test.go`, `tunel_test.go` i testovi `peers` prolaze
  nepromijenjeni;
- binarka 0.0.27 (iz oznake) protiv nove: razmjena tunelom i TCP-om, i
  očuvanje novih polja zapisa čvora;
- novi klijent protiv sastajališta bez `/razmjena/signal` (404) → stari put;
- ALPN `gocop-signal/1` prema čvoru bez `NextProtos` → stari put.

**Teren:**
1. laptop doma ↔ Unraid: mora ostati LAN;
2. laptop na mobilnoj mreži (CGNAT) ↔ Unraid;
3. laptop na mobilnoj mreži ↔ laptop doma;
4. ured ↔ Unraid i ured ↔ laptop izvan ureda (uz `tailscale netcheck`);
5. Unraid u bridge i host načinu;
6. 24 h signalizacije kroz Cloudflare uz brojanje prekida;
7. redovita provjera iz plana: isključi Unraid — dva laptopa u istoj
   prostoriji moraju raditi LAN-om, bez ičega u sredini.

Mjeri se: vrijeme do prve razmjene, udio probijenih veza po vrsti NAT-a,
MiB kroz posrednika dnevno.
