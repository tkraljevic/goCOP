# WEB sync (bivše veza/1): plan izvedbe

Ovo je `docs/plan-web-sync.md`. Plan je od 14. 10. 2026., napisan prema 0.0.38-alfa (`c0162ca9`). Dopunjen je idejama iz osam otvorenih projekata (Syncthing, Tailscale, iroh, pion/ice, go-libp2p, Nebula, Magic Wormhole, LocalSend) i ispravcima iz pregleda. Na šest pitanja iz prve inačice odgovoreno je istog dana; odgovori su u odjeljku 8, Odluke (14. 10. 2026.).

Plan nadopunjuje `docs/plan-protokol-povezivanja.md` i zamjenjuje ove njegove dijelove: tablicu faza u §7, otvorena pitanja (§10), procjenu rada (§11) i ispitivanje (§12). NAT tablica iz §12 prepisana je u F3. Odjeljci §2–§6 i §8 starog plana i dalje opisuju protokol, s ispravcima iz odjeljka 2.2 niže. Uz iste ispravke vrijede i §1 i dio §7 izvan tablice faza. §9 ostaje kao smjer za roj, ali bez redaka o novim poljima u zapisu čvora.

**Datumi nisu rokovi.** Svi datumi u planu su okvirni redoslijed. Faza kreće čim je spojena faza o kojoj ovisi, pa posao može završiti i puno prije. Kraj studenog je okvirna gornja granica za F0–F3 i probu na terenu. Što ne stane, ide u prosinac, uz G8 i dva tjedna bez grešaka (Odluke 14. 10. 2026.).

Imena putova i njihov redoslijed određeni su u Odlukama (14. 10. 2026.): LAN sync, WEB sync i SERVER sync, redom LAN → WEB → SERVER. Tailscale ostaje četvrti put, s vlastitim natpisom „Tailscale” (točka 5). Tehnička imena su ALPN `<protokol>-web/1` i `<protokol>-signal/1` (danas `gocop-web/1` i `gocop-signal/1`) te oznaka mogućnosti `web/1`. Kad plan piše „točka N”, misli na točku N iz Odluka (14. 10. 2026.) u odjeljku 8.

Oznake izvora: **[V]** provjereno u kodu ili izvoru, **[T]** isprobano, **[S]** sekundarni izvor, **[P]** prijedlog ili procjena. Što je uzeto iz kojeg projekta piše u tablici 2.3. Izvori s datotekama i retcima zasad su u datoteci `uzori-sync.md` uz skill razmjena-cvorova. Ta datoteka je lokalna, izvan gita; u F5 popis izvora prelazi u `docs/protokol-razmjene.md`. Od uzora se uzima samo ideja. Kod se ne prepisuje i ne uvodi se nova ovisnost, osim quic-go-a, za koji je već odlučeno.

**Pojmovi:**

| pojam | značenje |
|---|---|
| sastajalište | računalo kod kojeg se ostala prijave da bi se našla preko interneta: Unraid, kroz cop-osijek.com. Kao sastajalište nosi samo kratke poruke, ne podatke. |
| provjera mreže | računalo pita dva STUN poslužitelja kako ga vide izvana i iz toga zaključi kakav mu je NAT. U kodu je to `netcheck`. |
| lak NAT (EIM) | usmjerivač daje računalu istu javnu adresu i port prema svima, pa probijanje gotovo uvijek uspije. Suprotno je NAT ovisan o odredištu (simetrični). |
| kandidat | adresa na kojoj se računalo može javiti: lokalna, u kući ili uredu, ili javna, kako je vidi STUN. |
| poziv, odziv, sinkro | kratke potpisane poruke preko sastajališta. Njima se dva računala dogovore o kandidatima i o trenutku probijanja. |
| udarac | mali UDP paket (60 B) koji otvara NAT prema drugom računalu. Nosi HMAC, pa se zna da ga je poslala prava strana. |
| probijanje | obje strane u isto vrijeme šalju udarce jedna drugoj, pa NAT pusti QUIC vezu. Probijanje u pozadini, dok razmjena ide drugim putem, zove se priprema. |
| sesija | otvorena WEB sync veza između dva računala. Živi 6 min nakon zadnje razmjene. |
| puls | kratka poruka kojom se provjerava je li veza živa. |
| Tailscale | mreža koja povezuje računala prijavljena u istu Tailscale mrežu. Kad su u njoj oba računala, goCOP može razmjenjivati njome. |

U testovima se spominju još: Retry (QUIC traži da pošiljatelj dokaže svoju adresu), hairpin (NAT vrati paket u istu mrežu), conntrack (Linuxova tablica veza) i fuzz (test nasumičnim ulazom).

---

## 1. Svrha

Danas računala koja nisu u istoj lokalnoj mreži razmjenjuju podatke samo preko Unraida, kroz tunel cop-osijek.com, ili preko Tailscalea, kad ga imaju oba računala. WEB sync daje svakom paru članova mreže izravnu šifriranu vezu preko interneta, kad to dopuštaju mreže na obje strane. Kad WEB sync ne uspije, razmjena ide kao danas.

WEB sync ne zamjenjuje Unraid. Unraid i dalje upoznaje računala kratkim porukama kroz tunel (sastajalište) i ostaje SERVER sync: punopravni član mreže, koji prima i prosljeđuje podatke svakih 5 min. WEB sync zamjenjuje Unraid samo kao put podataka između dva računala, i to kad probijanje uspije.

**Što se mijenja za korisnika:**
- Kad WEB sync uspije, podaci između laptopa, ThinkCentrea i Windowsa ne idu kroz Unraid. Unraid ih upozna kratkom porukom kroz tunel, a podaci idu izravno.
- Laptop izvan ureda i ThinkCentre u uredu razmjenjuju izravno; podaci ne idu ni kroz Tailscale ni kroz Unraid. Danas taj par ide preko Tailscalea [V, `peer_sync` laptopa].
- Unraid prvi put može sam započeti razmjenu s ThinkCentreom. Danas ThinkCentre zove Unraid, a obrnuto ne ide, jer vatrozid Hrvatskih voda ne pušta poziv izvana.
- Razmjena nikad ne čeka probijanje. Dok se WEB sync priprema, taj krug ide putem koji radi, a WEB sync se koristi čim je spreman (Odluke 14. 10. 2026.).
- Pločica razmjene uz svako računalo kaže kojim je putem prošla zadnja razmjena i, kad put ne radi, zašto. Tailscale ondje ima svoj natpis, „Tailscale”. To stiže već u F1, prije samog WEB synca.
- Primanje na daljinu zasad je isključeno. Računalo se uparuje u lokalnoj mreži ili preko Tailscalea, oba puta sa šesteroznamenkastim kodom na oba ekrana (točka 4).
- Na usmjerivačima i na vatrozidu Hrvatskih voda ne otvara se ništa. Na samom računalu možda treba dopustiti UDP 4713: ufw na ThinkCentreu, vatrozid na Windowsu, predložak za Unraid. goCOP sam ne mijenja pravila vatrozida.

**Što ostaje kako jest:**
- Računalo smije razmjenjivati preko interneta tek kad je upareno u mreži, a svaki put preko interneta prima samo člana mreže (Odluke 14. 10. 2026.).
- Unraid je jedino sastajalište. Dok je ugašen, nova WEB sync veza ne može se dogovoriti. LAN sync i Tailscale tada rade kao danas.
- Tailscale ostaje mogućnost, uz LAN, WEB i SERVER sync. Rijetko tko će ga koristiti. Sad ga koristi testno okruženje, a informatičari Hrvatskih voda možda ga žele za neka računala, sa svojim računom. Tailscale adrese ni tada ne ulaze u knjigu.
- Opoziv članstva putuje razmjenom, kao danas. Novo je samo to da računalo, kad opoziv stigne, odmah zatvori WEB sync veze s opozvanim računalom.
- WEB sync je zadano isključen, a na testnim računalima uključuje se ručno. Zadano se uključuje tek zasebnom odlukom, kad vrijede oba uvjeta: dva tjedna bez grešaka na svim testnim računalima, i IT Hrvatskih voda je dobio opis na predstavljanju bete (točka 6).

---

## 2. Što se promijenilo od 2. 10.

### 2.1 Riješeno

| pitanje | odgovor | izvor |
|---|---|---|
| §10.0 QUIC ili DTLS | QUIC. h3 prolazi u uredu. Grana s DTLS-om se briše. | mjerenje 12. 10. |
| §10.1 NAT u uredu | Lak NAT (EIM), port očuvan, UDP prolazi. Laptop je mjeren na kabelu u LAN-u HV-a (192.168.24.92) i na Wi-Fi-ju iza vlastitog usmjerivača (192.168.10.75). ThinkCentre sam nije mjeren, ali je u istom LAN-u, a Tailscale do njega ide izravno, s očuvanim portom 41641. | mjerenje 12. 10. |
| §10.2 proxy | U uredu ga nema. Spajanje bez proxyja ostaje, a za druge mreže ostaje samo bilješka. | mjerenje 12. 10. |
| §10.3 IT Hrvatskih voda | Proba u uredu ide bez IT-a, samo na testnim računalima. IT dobiva opis protokola kad se predstavlja beta. Zadano uključivanje čeka to predstavljanje (točka 6). | Odluke (14. 10. 2026.) |
| §10.5, prva polovica | Redoslijed u quic-go-u je `VerifySourceAddress` → Retry → `ConnContext` s `AddrVerified = true`, sve prije TLS-a. | [V] izvor v0.63.0, [T] proba 14. 10. |
| §10.6 WebSocket ping | Ne koristi se, jer se promjena vrste okvira utrkuje s pisačem. Puls je poruka aplikacije. | [V] x/net v0.59.0 |
| §10.8 tajne udaraca | Prihvaćeno je da sastajalište zna tajne udaraca, jer je Unraid član mreže. Zato sastajalište smije biti samo računalo upisano u lokalne postavke (F2b). Prije nego copB ili VPS postane sastajalište, to se mijenja (odjeljak 4, „Što ne radimo sada”). | [P] |
| §10.9 IPv6 | Na izmjerenim mjestima (kuća, ured) IPv6-a nema, pa v1 radi samo s IPv4. Hotspot se mjeri u fazi M, samo kao podatak. Ni predviđanje porta ne treba: svi izmjereni NAT-ovi su laki, preslikavanje ne ovisi o odredištu. | mjerenja 12. 10. |
| mobilna mreža | Na hotspotu iPhonea ↔ Unraid obje strane imaju lak NAT. Veza je uspjela samo uz istodobno slanje. | proba 4. 10. |
| NAT simulator | quic-go ima mrežu u memoriji (`testutils/simnet`) koja radi s `testing/synctest`. U probi 14. 10. prošli su naši ključevi, QUIC i probijanje kroz izmišljeni NAT, s istim ishodom u 300 ponavljanja. Ne treba nova ovisnost ni spavanje u testovima. Cijela razmjena s bazom unutar synctesta još nije isprobana; to je proba na kraju F0. | [T] |
| redoslijed rada | WEB sync ima prednost pred TESTNOM SIMULACIJOM. Kreće nakon pravila na granici i ide usporedo s koracima 1–5 faze 3. | Odluke (14. 10. 2026.) |
| redoslijed putova | LAN → WEB → SERVER, a program uvijek prvo proba LAN. Razmjena odmah krene putem koji radi; WEB sync se pokušava u pozadini i koristi čim je spreman. | Odluke (14. 10. 2026.) |
| sigurnost | Računalo se prvo upari u mreži, a tek onda smije razmjenjivati preko interneta. Svaki put preko interneta (tunel, sastajalište, probijanje, posrednik) prima samo člana mreže. | Odluke (14. 10. 2026.) |
| uzori | Plan je uspoređen s osam otvorenih projekata. Sastajalište provjerava članstvo obiju strana, a kandidati i udarci su potpisani; to ostaje. Ideje koje se uzimaju popisane su u 2.3. | proučavanje 14. 10. |

**Kod od 0.0.34 do 0.0.38**, koliko je važno za plan:
- Tailscale je četvrti put (#59). Redoslijed je: LAN pronalaženje, adrese iz knjige, Tailscale, tunel. Tailscale adrese ne ulaze u knjigu.
- `DialExchangeOn` već postoji, pa je pola zajedničkog dijela spajanja iz F0 gotovo. `Odbij` je izvezen.
- Postoji brava upisa: jedna primjena odjednom, čekanje do 30 s.
- Nakon uparivanja odmah razmjenjuje samo računalo koje je zvalo (#63).
- Postavke iz `gocop.toml` same se pojavljuju u Administraciji (#57). Svaki novi ključ ipak treba redak u rasporedu postavki (`internal/web/postavke_cvora.go`), gdje piše smije li se mijenjati. To je mali posao u F1 i F2.
- Ime protokola (`Protocol = "gocop"`) danas ide u CN certifikata (`gocop-sync`), u kod za uparivanje i u pronalaženje. `gocop` je uz to zakucan u oznaci vjerodajnica u certifikatu (`gocop:vjerodajnice:`) i u datotekama primanja na daljinu (`gocop-zahtjev/1`, `gocop-potvrda/1` i sol `gocop-primanje/1|`). Provjera članstva ime ne gleda [V, `keys.go` `selfSignedCert` i `prefiksVjerodajnica`, `daljina.go`, `ovlast.go` `trusted`].
- LAN sync je sporiji nego treba. `FindDevice` probu šalje jednom i uvijek čeka punih 700 ms, a `SyncWith` zove adrese jednu po jednu, svaku do 20 s, uz TCP rok od 15 s. Ukupnog roka nema [V, `discovery.go`, `peers.go` `SyncWith`, `nazovi`, `otvori`].
- Glasnik na 4712 nakon greške čitanja prestane odgovarati, sve do ponovnog pokretanja programa. `main.go` grešku samo zapiše [V, `discovery.go` `Announce`].
- `Discover` upisuje adresu iz odgovora na pronalaženje u zapis čvora prije ikakve provjere ključa. Nova verzija zapisa nastaje i kad se promijeni samo redoslijed adresa [V, `peers.go` `Discover`, `SavePeer`].
- Primljeni zapis čvora prepisuje javni ključ i adrese poznatog računala bez provjere, a `SyncWith` očekivani ključ uzima iz tog zapisa. Član mreže tako može drugom računalu podmetnuti ključ nekog drugog člana [V, `repository/apply_mreza.go` `primijeniCvor`, `peers.go` `partnerZaRazmjenu`]. WEB sync se na taj zapis ne oslanja (F2b, F3b). Za TCP i tunel ispravak je zaseban mali PR prije F2b (F2, „Dva mala PR-a prije F2b”).
- Vrata kvalitete uspoređuju s referentnom točkom `0da9ebb6` od 12. 10. i za postojeću funkciju dopuštaju CC do brojke iz te točke. Ondje `run` ima CC 66, `SyncWith` 13 i `exchange` 44, a danas 59, 7 i 40, pa vrata u njima propuštaju nove grane [V, `quality/baseline.json`, `dev/quality/gate.go`]. Zato G0 (točka 1).

**Novo iz mjerenja, a stari plan to ne zna:**
- Kod kuće je laptop iza drugog usmjerivača: 192.168.0.17 iza 192.168.0.1, a taj usmjerivač izlazi kao 192.168.1.175. Unraid (192.168.1.2) i Windows (192.168.1.117) su na vanjskom LAN-u.
- Laptop zato dolazi do Unraida, ali Unraid do laptopa ne. UPnP i NAT-PMP kod kuće daje unutarnji usmjerivač, pa za internet ne vrijedi.
- U uredu je slično: Wi-Fi 192.168.10.x ima vlastiti usmjerivač unutar LAN-a 192.168.24.x.
- Ured izlazi s barem dvije javne adrese: laptop kao …212, ThinkCentre kao …211.
- Pronalaženje na UDP 4712 ne prelazi iz jedne podmreže u drugu. Iz odgovora na pronalaženje zato se ne smije zaključivati da su računala „u istoj mreži”.

### 2.2 Zastarjelo u starom planu

| gdje | što piše | kako je sad |
|---|---|---|
| zaglavlje, §7 | pregledan prema 0.0.34; ured dolazi zadnji, tek uz IT | pregledan prema 0.0.38; ThinkCentre ulazi u probe od faze M |
| §1 | uredski vatrozid nepoznat; ured ↔ vani možda samo preko posrednika | izmjereno: lak NAT, probijanje bi trebalo raditi |
| §1, §4.4 | posrednik preko Unraida je zadnji izlaz | odgođen je (točka 3); bez WEB synca par razmjenjuje SERVER syncom, kao danas |
| §2 | tablica putova bez Tailscalea | dodati `tailscale`, sa zasebnim natpisom „Tailscale” (točka 5) |
| §2 | redoslijed „po uzoru na Syncthing [V]”, s QUIC-om u LAN-u ispred TCP-a | oznaka je kriva: Syncthing stavlja TCP ispred QUIC-a i u LAN-u i izvan njega. Redoslijed je naša odluka [P], a QUIC u LAN-u je odgođen |
| §2, §4.3 | tunel samo kad je cilj sastajalište | tunel je SERVER sync za sva računala. Kreće odmah kad LAN ne odgovori, usporedo s Tailscaleom (F3b) |
| §3 | do tri sastajališta, prioriteti, 6 h pamćenja odgovora 404 | jedno sastajalište, samo iz lokalne postavke, nikad iz knjige; x/net/websocket ne razlikuje 404 od 503 [V] |
| §3 | sastajalište čita identitet samo iz retka `memberships` | mora primiti i provjereno članstvo iz certifikata, kao `trusted()`. Inače odbije računalo koje je upravo upareno, dok mu članstvo još nije stiglo do Unraida |
| §3, F2 | `mogucnosti`, `dohvatljivost` i ostala polja u zapisu čvora | ništa u zapisu čvora: taj zapis pišu i druga računala, pa se polje može vratiti na staru vrijednost, a `identityChanged` ga ne bi ni poslao [V] |
| §4.1, §7 | provjera mreže svakih 10 min | pri pokretanju, na gumb, pri ponovnom spajanju na sastajalište i prije poziva. Adrese sučelja provjeravaju se svakih 10 s (F2b), jer mrtva TCP veza na laptopu zna visjeti minutama |
| §4.1 | vrsta NAT-a i „UDP blokiran” iz jednog mjerenja | mijenjaju se tek nakon dva ista mjerenja zaredom, a „nepoznato” ne briše poznatu vrijednost (F1) |
| §4.1 | osvježavanje STUN-a nakon 0,5, 1 i 2 s, a §4.3 traži ≤ 2 s | oba poslužitelja usporedno, ponavljanje nakon 250 i 750 ms, rok 2 s (F1) |
| §4.2 | ponovno spajanje 2 s · 2ⁿ, najviše 5 min | najviše 60 s, a nakon buđenja ili promjene mreže odgoda kreće ispočetka (F2b) |
| §4.3, k. 1 i 7, rokovi | `SyncWith` čeka probijanje oko 24 s, uz ukupni rok od 45 s | `SyncWith` ne čeka: kad LAN ne odgovori, krug ide putem koji radi, a WEB sync se priprema u pozadini i ne drži mjesto za razmjenu (Odluke 14. 10. 2026., F3b). Ukupni rok od 45 s uvodi se novo, jer ga danas nema |
| §4.3, k. 5 | 3 pokušaja samo kad su obje strane EIM, 1 s između pokušaja | 3 pokušaja i kad je NAT nepoznat, 1 samo kad je kod obiju strana potvrđen NAT ovisan o odredištu; u 2. pokušaju B kreće 1 s kasnije, u 3. A (F3a) |
| §4.3 | negativna pričuva od 30 min do 6 h, briše se samo kad se promijeni naša strana | pauza po paru, prema stanju obiju strana: 30 min, najviše 1 h; vrijedi samo za pripremu u pozadini (F3b) |
| §4.3 | sesija živi 120 s nakon zadnjeg toka | 6 min, dulje od kruga razmjene od 5 min, pa idući krug ne probija ponovno (F3a) |
| §4.3 | promjena mreže: nova provjera mreže, nova prijava, zatvaranje sesija | uz to: buđenje po razmaku zidnog sata, čekanje rute do 5 s prije provjere mreže i nova UDP utičnica (F2b) |
| §4.3, §5.1 | `tlsConfig(priv)` i `trusted(ključ)` | `tlsConfigS(priv, protokol, vj)` i `trusted(pub, vj)` [V] |
| §5.2 | sinkro bez broja pokušaja | sinkro nosi `pokusaj` (1–3), inače se ponovljeni sinkro ne da razlikovati od idućeg pokušaja (polje u F2b, provjera u F3a) |
| §5.2 | `sucelja_iskljuci` kao postavka | stalan popis u kodu (`sucelja.go`, LAN1): `docker*`, `br-*`, `veth*`, `virbr*`, `utun*`, `tailscale*`, `vEthernet*`, `bridge*` |
| §5.3 | jedan ključ udarca za oba smjera | po jedan ključ za svaki smjer; inače se tuđi udarac može odbiti natrag pošiljatelju. Uz to: vlastite adrese se ne udaraju, a odgovor se prima samo s adrese na koju je poslan udarac (F3a) |
| §5.4 | `MaxIncomingUniStreams: 0` | `-1`, jer nula znači 100 tokova [V] |
| §5.4 | `trusted()` svakih 5 min | na početku svakog toka 0x01 i odmah nakon primjene članstava, ovlasti ili opoziva (F3a) |
| §5.4 | `pozdrav` bez sesije | nosi `sesija` iz potpisanog poziva; QUIC veza iz starijeg pokušaja se zatvara (F3a) |
| §5.4 | rezervirani kanal 0x03 zove se „web” | zove se „stranice”, da se ne miješa s WEB syncom i `web/1` |
| §5.4 | `noteSync` na peers.go:1007 | „blok adresa u `noteSync`”, bez broja retka |
| §6.2 | 4 razmjene zajedničke svim putovima | vrijedi za dolazne; odlazne imaju svoja 4 mjesta |
| §6.2 | registar: nova veza zatvara staru; 16 rukovanja zajedničkih svima | isti proces zamijeni svoju staru vezu. Drugi proces s istim ključem: sastajalište staroj vezi pošalje puls s rokom od 5 s; ako stara ne odgovori, nova je zamijeni, a ako odgovori, nova se odbije i čeka 10 min [P, F2b]. Registar drži najviše 64 računala, koliko i veza. Po 2 rukovanja po adresi (F2a). Retci za posrednika i `/razmjena/spoj` otpadaju |
| §6.2 | glasnik na 4712 bez ograde | odgovara samo izvorima iz vlastite podmreže, najviše 20 odgovora u sekundi i 2 po izvoru [P, LAN1] |
| §6.4 | knjiga ne vidi IP adrese | danas ih vidi: `noteSync` i `Discover` upisuju naučene adrese u `peers.addresses`. Od F0 naučene adrese ostaju samo na računalu koje ih je naučilo (točka 2) |
| §7 | QUIC u LAN-u, `Meta["quic_port"]`; Unraid na mreži računala ili u mostu | QUIC u LAN-u je odgođen; QUIC na lokalni kandidat ionako se prikazuje kao LAN sync. Unraid ostaje u mostu dok mjerenje ne pokaže drukčije |
| §8 | javna adresa samo administratoru | isto, i šire: na pločici nikad adrese ni sirovi tekst greške, nego gotova rečenica (F1) |
| §8 | `put_razlog` kao slobodan tekst | kod iz stalnog popisa (F1); prolazno stanje dolazi na pločicu tek kad traje dulje od 10 s (F2a) |
| imena i postavke | `veza/1`, `gocop-veza/1`, `[povezivost] probijanje`, `lan_kandidati`, `sucelja_iskljuci`, `sastajalista = []`, `sastajaliste = false` („poslužuj signal”), `prioritet_sastajalista`, `posrednik*`; `Put` `izravno` i `posrednik` | `web/1`, `<protokol>-web/1`, `web_sync_prijava` i `web_sync`. `sastajaliste` više nije prekidač, nego adresa sastajališta (na primjer `https://cop-osijek.com`), uz `sastajaliste_cvor`; posluživanje se uključuje s `posluzi_sastajaliste` (F2). `Put` `izravno` sad je `adresa`, a `posrednik` otpada. Ostale stare postavke otpadaju |
| §9 | polja u zapisu čvora za roj | §9 ostaje kao smjer za roj, bez tih polja |
| §11 | 16–21,5 dana, s posrednikom i QUIC-om u LAN-u | 22–26,5 dana bez njih (odjeljak 4) |
| §12 | pion vnet kao zamjena; proba na terenu s dva laptopa | `simnet` i `synctest`; proba na četiri testna računala, pet s H; testovi su u fazama, NAT tablica u F3 |
| `plan-povezivost.md` §3 i korak 8 | „Tailscale nije ovisnost”, „vlastiti transport, možda nikad” | ažurira se u F5 |
| `plan-testiranja-mreze.md` | izdanja 0.0.29–0.0.33; samo U, L, W | faze umjesto brojeva izdanja; četiri testna računala, pet s H |
| `plan-faza3-mjesec.md` | veza/1 „poslije mjeseca”; izdanje 0.0.36 | WEB sync ide usporedo s koracima 1–5 faze 3, prije TESTNE SIMULACIJE; F0 kreće čim su spojeni G1 i G3. Plan faze 3 ispravlja se u istom PR-u |

**Još otvoreno:**
- NAT Dockera na Unraidu (§10.4): mjeri se u fazi M.
- Koje adrese Unraid ima za ThinkCentre (vjerojatno samo nedostupnu 192.168.24.70, ili nijednu): gleda se u fazi M, a test u F3b pokriva oba slučaja.
- `ReadNonQUICPacket` pod opterećenjem: provjerava se testom u F1.
- Cijela razmjena preko QUIC-a unutar synctesta: proba na kraju F0.
- Koliko dugo Cloudflare drži tihu WebSocket vezu (§10.7): mjeri se nakon F2.
- Radi li UDP utičnica na macOS-u nakon promjene mreže. Po Nebuli, Darwin veže utičnicu uz sučelje na kojem je nastala. Mjeri se na laptopu nakon izdanja 1.
- Stižu li odgovori na probu pronalaženja kroz ufw na ThinkCentreu: mjeri se u fazi M.
- Ima li hotspot IPv6: samo podatak, mjeri se u fazi M.

### 2.3 Što uzimamo iz uzora

„Slično kao” znači da uzor radi nešto blisko, ali ne isto. Gdje uzora nema, piše [P].

| ideja | uzor | gdje |
|---|---|---|
| razmjena ne čeka probijanje; WEB sync se priprema u pozadini | Tailscale, iroh, Nebula | F3b |
| pauza po paru, prema stanju obiju strana, najviše 1 h | slično kao libp2p (odgoda po adresi, najviše 5 min) | F3b |
| adrese se zovu usporedno, svaka iduća 250 ms kasnije | libp2p (po RFC 8305) | F3b |
| sesija živi 6 min, dulje od kruga | slično kao Nebula (tunel bez prometa ne gasi) | F3a |
| 3 pokušaja i za nepoznat NAT | libp2p | F3a |
| u 2. pokušaju B kreće 1 s kasnije | Nebula (utrka u Linux conntracku) | F3a |
| smjer u ključu udarca; vlastiti udarac se odbija | Tailscale (paket nosi ključ pošiljatelja) | F3a |
| bez udaraca na vlastite adrese | iroh | F3a |
| odgovor samo s adrese na koju je udareno; udarac s nove adrese je nova adresa druge strane | pion | F3a |
| lokalni kandidat dobiva 500 ms prednosti | pion | F3a |
| `pozdrav` nosi sesiju | Magic Wormhole | F3a |
| održavanje veze svakih 15 s | Syncthing, libp2p | F3a |
| sinkro po RTT-u | libp2p | F3a |
| buđenje po razmaku zidnog sata | Tailscale | F2b |
| čekanje rute prije provjere mreže; nova utičnica nakon promjene mreže | iroh; za macOS Nebula | F2b |
| ponovno spajanje na sastajalište najviše za 60 s | Magic Wormhole (iroh čeka najviše 16 s, Tailscale 5 s) | F2b |
| drugi proces s istim ključem ne izbacuje staru vezu naslijepo | iroh; puls staroj vezi je naša odluka [P] | F2b |
| upozorenje s odgodom; posljedice se skrivaju dok traje uzrok | Tailscale | F2a |
| trajna greška odvojena od prolazne | iroh | F1, F2a |
| histereza provjere mreže | libp2p | F1 |
| provjera mreže prema dva poslužitelja s iste utičnice | Tailscale; iroh slično, prema relejima | F1 |
| jedna utičnica za STUN i QUIC | Syncthing; iroh slično, provjeru mreže radi QUIC-om | F1 |
| STUN oba poslužitelja usporedno, ponavljanje nakon 250 i 750 ms | pion | F1 |
| kratak stalan popis ishoda | Magic Wormhole, libp2p | F1 |
| greška po adresi | Syncthing | F0, F1 |
| adresa iz pronalaženja vrijedi tek nakon TLS-a | LocalSend | F0 |
| više proba pronalaženja | slično kao LocalSend (tri javljanja, nakon 100, 500 i 2000 ms) | LAN1 |
| glasnik se sam ponovno digne | slično kao LocalSend (nova utičnica kad sve padnu) | LAN1 |
| testovi: conntrack u Docker mostu | Nebula | F3a |
| istek NAT-a nakon 10, 30 i 60 s | Tailscale | faza M |
| program ne dira vatrozid | Nebula (sama) i LocalSend (na gumb) na Windowsu ga mijenjaju; goCOP ne | F1, F5 |

Naše odluke bez uzora [P]: najviše 4 naučene adrese, spremljene samo na računalu koje ih je naučilo (F0); rani izlaz iz pronalaženja i jedno pronalaženje po krugu (LAN1); glasnik odgovara samo svojoj podmreži, uz granicu odgovora (LAN1; LocalSend otkrivanje drži u podmreži drukčije, s TTL 1, i ne odgovara UDP-om); puls staroj vezi kod istog ključa (F2b); razmaci udaraca od 50 do 200 ms (F3a; Nebula ima 100 do 1000 ms); laptop koji dođe kući iduću razmjenu radi LAN syncom (F3b); test s istom privatnom adresom u dvije kuće (F3a).

Što se ne uzima, i zašto, piše u „Što ne radimo sada” i u odjeljku 9.

---

## 3. Preduvjet: pravila razmjene na granici

WEB sync mijenja istu datoteku i isti razgovor kao pravila na granici. Zato pravila idu prva.

**Što je već dobro.** Računalo čuva i prosljeđuje zapise koje ne poznaje, a primijeni ih nakon nadogradnje (Odluke 11. 10. 2026., točka 1). Test pazi da je svaka vrsta zapisa na popisu za obnovu. Nedostaju broj zapisa na pločici i test od kraja do kraja; oba idu u G5.

**Što ne valja:**
1. Računalo koje ne drži stara očitanja dobiva ih svakom razmjenom iznova i baca ih. To je provjereno testom 14. 10. Iz koda slijedi da se kod 5000 ili više takvih verzija isti paket može ponoviti do 200 puta u jednom krugu; to nije pokretano.
2. Na laptopu i Windowsu, koji ne prate sve, pločica vjerojatno stalno piše „prima se još”. Nije provjereno na računalu.
3. Jedna neispravna verzija u primljenom paketu odbije cijeli paket, i tako svakom razmjenom.
4. Računala ne kažu jedno drugome što razumiju. Zna se samo izdanje programa, i to samo za prikaz.
5. `exchange()` ima CC 40, pa se u nju ne može dodati novo pravilo dok se ne podijeli.

| korak | što | gdje | dani |
|---|---|---|---|
| G0 (prije G1; točka 1) | Nova referentna točka kvalitete na čistom masteru (`make quality-baseline ACCEPT_BASELINE=yes`), u zasebnom PR-u s opisom, kako traži `docs/CODE_QUALITY.md`. Tada vrata pamte današnje brojke i za `run`, `SyncWith` i `exchange`. `internal/ledger` ostaje izvan kritičnih paketa. | `quality/baseline.json` | sat-dva, ne broji se |
| G1 | Razgovor razmjene podijeliti na korake, bez promjene ponašanja. Na žici idu isti bajtovi; test bilježi tijek razgovora prije i poslije, za obje uloge. Izdvojene funkcije ulaze u `premjestaji` u `quality/config.json`, s datumom pregleda, ili svaka sama zadovoljava granice kritičnog koda. | `internal/peers/peers.go` (`exchange`), novi `razgovor.go`, `quality/config.json` | 1–1,5 |
| G2 | Na početku razgovora računala kažu što znaju: popis mogućnosti, vrste zapisa i sheme koje razumiju, i broj zapisa koji kod njih čekaju nadogradnju. Druga strana to pamti lokalno. | novi `internal/razmjena/mogucnosti.go`; `frontierMsg`; novi `ledger.Katalog`, nad postojećim `PoznatiEntiteti` i `ShemaEntiteta`; `internal/peers/status.go`; `internal/db/schema.go` (`peer_sync`) | 1–1,5 |
| G3 | Odbijenica dobiva kod razloga (`Envelope.Kod`), usklađen s kodovima koje će koristiti QUIC (0x0100 i dalje). Neispravna verzija u primljenom paketu se preskoči, a ostale prođu. Provjera ide zasebnim prolazom prije petlje u `Apply`, bez nove grane u njoj. | `internal/razmjena/exchange.go` (`Odbij`), `internal/peers/upis.go`, `tailscale.go`, `ledger.Apply` | 0,5 |
| G4 | Računalo pamti dokle je odbacilo stara očitanja i to javi drugoj strani, pa ih ona više ne šalje. Kad se promijeni što računalo drži, to se zaboravi i stara očitanja opet stignu. Isti korak popravlja „prima se još”. Poruka ide samo kad obje strane imaju oznaku za nju. `applyReadingPolicy` je zatvorenica unutar `run` (`cmd/gocop/main.go`), pa logika ide u novu funkciju, a u zatvorenici ostaje samo poziv. | novi `internal/peers/granica_odbijenih.go`, pretplate, brisanje kanala, `applyReadingPolicy`, `stanjeRazmjene` | 1,5–2 |
| G5 | Pločica kaže koliko zapisa čeka nadogradnju: broj zapisa, ne broj verzija, i bez imena tablica. Pokazuje i koliko čeka na drugim računalima. Natpis (točka 5): „Stiglo je 120 zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.”, a uz drugo računalo „Windows čuva 120 zapisa koje će primijeniti nakon nadogradnje.” Svuda piše „nadogradnja”, pa postojeći tekst gubi „ažurirajte”. Test od kraja do kraja: staro računalo primi nepoznat zapis, proslijedi ga trećem, primijeni ga nakon nadogradnje, a pločica pokaže broj. | `internal/ledger/shema.go` (`Novost`, `UcitajNovosti` kroz novu funkciju), `cmd/gocop/arhiva_razmjena.go` | 1 |
| G6 | Opis razmjene: koraci razgovora, pravila mogućnosti, kodovi, granice, kako se dodaje polje ili poruka. Dopuniti skill razmjena-cvorova. | novi `docs/protokol-razmjene.md` | 0,5–1 |
| G7 (nakon F0) | Zapis odbijenih i djelomično podržanih poruka: tablica `razmjena_odbijeno` bez sadržaja zapisa. Bilježi odbijene poruke i poruke koje je računalo primilo, a dio nije razumjelo (nepoznata vrsta zapisa, nepoznata oznaka mogućnosti, novija shema). Broji odbijanja na vratima po putu, bez adresa. Detalj ide na stranicu Sinkronizacija, a na pločicu samo kvarovi. Ide nakon F0, jer se vrata broje u `primiVezu`. | novi `internal/peers/odbijeno.go`, `internal/db/schema.go` | 1 |
| G8 (prosinac) | Nakon nadogradnje jednom primijeniti i zapise koji imaju samo jednu verziju. Danas se oni ne osvježe kad dobiju novu shemu ili stupac. | novi `internal/repository/apply_nadogradnja.go` | 0,5–1 |

**Pravilo mogućnosti (G2), zapisano jednom:**
- Oznake su oblika `ime/N`.
- Nepoznata oznaka se zanemaruje. Kad popisa nema, vrijedi `razmjena/1`.
- Za isto ime vrijedi najviši zajednički N.
- Novo neobavezno polje ne treba oznaku. Nova vrsta poruke ili promijenjeno značenje dopušteni su samo kad obje strane imaju oznaku.
- Nositelj je `frontierMsg`, pa oznake idu svakim putem: TCP, tunelom i kasnije QUIC kanalom 0x01.
- `pozdrav` WEB synca i signalna `prijava` koriste isti tip.
- Današnji sadržaj se ne zove `sadrzaj/1`, jer je to ime rezervirano za roj.
- Oznake nikad ne sadrže „gocop”. ALPN je ime protokola plus oznaka.

**Kompatibilnost s 0.0.38**, uz test za svaki korak:
- G2: 0.0.38 ne šalje popis mogućnosti, pa za nju vrijedi `razmjena/1` i razmjena ide kao danas. Test provjerava da 0.0.38 nova polja zanemari (`TestCvor0038ZanemarujeZnanje`), a test tijeka razgovora da se uz sugovornika bez popisa mijenja samo vlastita granica. `cekaNadogradnju` je od prvog dana broj zapisa, ne verzija: zapisi entiteta koje program ne poznaje i zapisi s verzijom novije sheme, uključujući obrisane (`ledger.(*Recorder).CekaNadogradnju`). G5 ga samo prikaže.
- G3: 0.0.38 ne poznaje kod razloga; test provjerava da ga zanemari. Odbijenice 0.0.38 prepoznaju se po tekstu, pa su tekstovi zamrznuti u testu (`TestOdbijeniceCvora0038`) i ne mijenjaju se dok u mreži ima čvorova 0.0.38. Novi čvor ne šalje verziju bez oznake, entiteta ili identifikatora zapisa, jer je primatelj preskoči bez pomaka granice. Čvor 0.0.38 takvu verziju i dalje šalje. Kad ih u jednom ključu ima 5000 ili više zaredom, njegov krug razmjena s novim čvorom ide do 200 razgovora i ispravne iza njih ne stignu, sve dok se ne nadogradi. goCOP takve retke sam ne stvara, pa to traži pokvarenu bazu.
- G4: računalo s 0.0.38 nema oznaku, pa ne dobiva poruku o granici i i dalje prima stara očitanja kao danas. Napravljeno 17. 10.: oznaka `granica-odbijenih/1` i poruka `granica-odbijenih` odmah iza granica, samo kad obje strane imaju oznaku (`internal/peers/granica_odbijenih.go`). Računalo pamti što je odbilo posebno za svako računalo koje mu šalje, samo u memoriji, pa migracije nema. Zaboravi to kad se promijeni pravilo čuvanja, pretplata, „prati sve”, kad se obriše kanal ili zaboravi računalo, kad sat ode unatrag, nakon 24 sata po zidnom satu i pri ponovnom pokretanju. Kad se nešto od toga dogodi usred razgovora, razgovor ne zapamti ništa. Ako je računalo usred razgovora počelo držati više, ne upiše ni novije verzije onih ključeva koje je druga strana zbog granice preskočila, pa sljedeći razgovor donese sve. Pločica „prima se još” broji samo kanale koje računalo prati, bez onoga što je odbilo (`peers.PrimaJos`). Od pošiljatelja 0.0.38 stara očitanja i dalje stižu svaki put, a ključ s 5000 ili više odbijenih zaredom zapne kao prije, dok se pošiljatelj ne nadogradi. Nakon uvoza arhive ili COP paketa, i nakon ponovnog pokretanja, pošiljatelj granicu odbijenih zanemari dok svakom računalu jednom ne pošalje sve, jer je uvozom mogao dobiti očitanje ispod nje (`TestUvozIspodGraniceOdbijenihStigne`). Testovi: `TestTijekBezOznakeNemaGraniceOdbijenih`, `TestMjesovitaMrezaBezOznake`, `TestIzaOdbijenihKljucNeZapne`, `TestOgradaPopustiUsredRazgovora`.
- Nakon izdanja 0 Windows ostaje na 0.0.38 do izdanja 1, da se mješovita mreža isproba na pravim računalima.

**Ukupno G1–G6: 5,5–7,5 dana, okvirno od 16. do 27. 10.** Stari plan dao je ovome 2 dana. Razlika dolazi od potvrđene greške pod 1 i od podjele `exchange()`. Za početak F0 nužni su G1 i G3. G2 mora biti gotov prije F2. G4 nije nužan za WEB sync, ali popravlja potvrđenu grešku, pa ide odmah.

---

## 4. Faze

**Pravila za sve faze:**
- **Granice kvalitete.** U kritičnom kodu koji plan dira (`internal/peers`, `internal/razmjena` i datoteke `internal/repository/apply*`, koje diraju ispravak zapisa čvora i G8) nova funkcija smije imati CC ≤ 7 i CRAP ≤ 7, uz pokrivenost ≥ 95 %. Kod CC 7 to znači 100 % pokrivenosti, pa ciljamo CC ≤ 6. Strojeve stanja pišemo kao tablice prijelaza. `internal/ledger` nije na popisu kritičnih paketa u `quality/config.json`, i ovaj ga plan ne dodaje; ondje vrijede obične granice (CC ≤ 10, CRAP ≤ 10, 85 %). `internal/natsim` i pomoćnik s vezama u memoriji obični su paketi i trebaju vlastite testove, barem do 85 %.
- **Bez novih grana u velikim funkcijama.** To vrijedi za `run` u `cmd/gocop/main.go` (CC 59), `setupRoutes` (42), `exchange` (40), `(*Service).alerts` (22), `(*Service).Status` (20), `(*Recorder).Apply` (12), `UcitajNovosti` (12) i `SyncWith` (7). Za `Status`, `alerts`, `Apply`, `UcitajNovosti` i `setupRoutes` pravilo provode vrata kvalitete, jer su brojke iste kao u referentnoj točki. Za `run`, `SyncWith` i `exchange` vrata ga provode tek nakon G0; do tada je to pravilo rada i provjerava se u pregledu PR-a. Novo ponašanje ide u novu funkciju koja se poziva bez uvjeta i sama odlučuje ima li posla. Primjeri: jedan redak `pokreniPovezivost(ctx, cfg, peersService, server)` u `run`, `alertsPovezivosti(st)` na kraju `Status`, zasebna provjera prije petlje u `Apply`.
- **Testovi:**
  - nikad ne spavaju;
  - rabe `testing/synctest` i `testing/cryptotest.SetGlobalRandom`;
  - gubitak paketa odlučuje se po sadržaju paketa, ne po brojaču;
  - provjeravaju ishod, put i vremensku granicu, nikad broj paketa;
  - ne koriste `net.Pipe`, jer TLS 1.3 na njemu zastane do isteka roka [T]; umjesto njega služi zajednički pomoćnik s međuspremnikom;
  - poslužitelj s WebSocketom, i lažni Cloudflare ispred njega, unutar synctesta je `http.Server` na slušalici u memoriji. `httptest` služi samo za jedan ili dva testa u stvarnom vremenu, bez provjere rokova, jer synctest ne pomiče lažno vrijeme dok gorutina čeka na pravoj mreži.
- **quic-go:** pozivi quic-go-a su samo u dvije datoteke u `internal/razmjena`: `quic.go` (prijenos, UDP utičnica i prilagodnik toka; proba i F1) i `quic_slusalica.go` (slušalica i pozivanje; F3a). One daju mala sučelja: veza, tok i slušalica. `sesija.go`, `probijanje.go` i `nova_uticnica.go` rade samo s tim sučeljima. Iznimka su testovi. `http3` se ne uvozi.
- **Postavke, ne konstante.** Ime protokola (`[sync] protokol`, zadano `gocop`), UDP port, STUN poslužitelji i sastajalište su postavke. ALPN, prefiksi potpisa novih poruka i oznake za HKDF izvode se samo iz imena protokola i nemaju vlastite postavke, da ih simulacija i prava mreža ne podijele greškom. Stare oznake formata (`gocop:vjerodajnice:`, `gocop-zahtjev/1`, `gocop-potvrda/1` i sol primanja na daljinu) ostaju stalne (F0). Baza pamti ime protokola svoje mreže (F0), pa kopirana mapa podataka pokrenuta s drugim imenom ne krene. TESTNA SIMULACIJA tada radi kao zasebna instanca: novi ključ računala i nova mreža napravljena od početka, nikad kopirana mapa podataka, uz `sim.cop-osijek.com`. Rokovi (odgoda signala, pauza, trajanje sesije) su stalne vrijednosti u kodu, ne postavke; ugađaju se u F5 prema terenu.
- **Dva prekidača** u `[povezivost]`:
  - `web_sync_prijava`: UDP utičnica, provjera mreže i prijava kod sastajališta (F1, F2). Podaci još idu kao prije;
  - `web_sync`: sam WEB sync, dakle QUIC slušalica, probijanje i razmjena tim putem (F3). Vrijedi samo uz `web_sync_prijava = true`.

  Oba su zadano `false`. Uz `web_sync_prijava = false` računalo ne drži UDP utičnicu i nema ni quic-go prijenosa, ni slušalice, ni prijave kod sastajališta. I `posluzi_sastajaliste` (F2) vrijedi samo uz `web_sync_prijava = true`.
- **Isporuka:** svaka faza je jedan ili dva PR-a, a izdanje je zaseban korak. Unraid se nadograđuje prvi.

**Putovi i natpisi na pločici:**

| `Put` u kodu | natpis na pločici |
|---|---|
| `lan` (pronalaženje) | LAN sync |
| `adresa`, privatna adresa | LAN sync |
| `adresa`, javna adresa ili domena koju je upisao čovjek (poslužitelj) | SERVER sync |
| `probijeno`, privatna adresa (lokalni kandidat) | LAN sync |
| `probijeno`, javna adresa | WEB sync |
| `tailscale` | Tailscale |
| `tunel` | SERVER sync |
| računalo do kojeg ovo računalo nema puta; podaci idu preko poslužitelja u dva kruga | „SERVER sync, preko cop-osijek.com: podaci stižu do 10 min kasnije.” |

Natpis LAN sync ovisi o tome je li adresa privatna, a ne o tome je li u istoj podmreži. Laptop kod kuće (192.168.0.x) i Unraid (192.168.1.x) nisu u istoj podmreži, a ipak su u istoj kući.

Tailscale ima svoj natpis, jer je zaseban put: njime razmjenjuju samo računala koja ga imaju (točka 5).

U tekstu programa put se uvijek zove svojim imenom: LAN sync, WEB sync, SERVER sync ili Tailscale. Riječi „izravno” i „izravna veza” se ne pojavljuju. Ne pojavljuje se ni „Unraid”, jer će jednom poslužitelj biti drugi: umjesto toga piše ime poslužitelja iz adrese tunela ili postavke `sastajaliste` (na primjer cop-osijek.com), a kad ga nema, „poslužitelj”.

### F0 Priprema prijenosa (3–3,5 dana, okvirno 21.–23. 10.)

**Cilj.** Razmjena zna kojim je putem prošla. Javne adrese i adrese s puta koji nije LAN više ne ulaze u knjigu. Baza je vezana uz ime protokola. Ponašanje se inače ne mijenja.

| gdje | promjena |
|---|---|
| `internal/razmjena/exchange.go` | `Conn.raw` postaje sučelje `prijenos` (čitanje, pisanje, zatvaranje, rokovi, adrese); `*tls.Conn` i `wsVeza` ga već zadovoljavaju. `newConn` se dijeli na čitanje ključa iz TLS-a i `newConnKljuc`. Dodaju se `Conn.Put` i `Conn.RTT`. Dio `serveTLS` nakon rukovanja prelazi u `primiVezu`, pa CC pada s 11 na ≤ 7. `primiVezu` i `DialExchangeOn` odbijaju certifikat čiji CN nije `<protokol>-sync`. Sva današnja računala šalju `gocop-sync`, pa 0.0.37 i 0.0.38 rade kao prije. |
| `internal/razmjena/keys.go`, `internal/peers/daljina.go` | `gocop:vjerodajnice:`, `gocop-zahtjev/1`, `gocop-potvrda/1` i sol `gocop-primanje/1|` ostaju stalne oznake formata i ne izvode se iz imena protokola. Promjena bi slomila razmjenu s 0.0.38 i već izdane datoteke primanja. Simulaciju od prave mreže ionako odvajaju ključ mreže i provjera CN-a. Test pazi da se te oznake ne mijenjaju. |
| `internal/razmjena/tunel.go` | `DialTunel` završava preko `DialExchangeOn` (CC s 8 na ≤ 7). Spajanje ide kroz zajedničku funkciju koja vraća `net.Conn`, a zatim `websocket.NewClient`. Ista funkcija poslije nosi i signalnu vezu. Vanjski TLS je po primjerku, ne globalni `vanjskiTLS`; dva testa tunela (`tunel_test.go`, `rubovi_veze_test.go`) prelaze na to. |
| `internal/peers/peers.go` | `adreseZaSpajanje` vraća `[]kandidat{adresa, put}`, istim redom kao danas. `nazovi` upisuje `Put` i mjeri RTT; za adresu bez porta uzima `s.exchangePortOf(*peer)`, kao i ostatak koda, umjesto stalnog 4710. `nazovi` pamti zadnju grešku i vrijeme za svaku adresu, samo u memoriji; danas `SyncWith` pamti samo grešku zadnje adrese. `Ograda` razmjena seli iz `Serve` u `Service`, da je dijele i budući putovi. `Protocol` postaje postavka `[sync] protokol`, zadano `gocop`. |
| `internal/peers/peers.go` (vrata za učenje adresa) | `noteSync` i `Discover` idu kroz ista nova vrata. Adresa se nauči samo kad je put `lan` ili `adresa`, kad je privatna (RFC 1918 ili ULA) i kad veza ne ide preko Tailscalea. Javna adresa se nikad ne nauči sama, samo je čovjek upiše. Naučena adresa ostaje samo na računalu koje ju je naučilo: sprema se u novi stupac `adrese` u `peer_sync`, koji se ne razmjenjuje, i ostaje nakon ponovnog pokretanja. U knjigu idu samo adrese koje upiše čovjek i adrese poslužitelja (točka 2). `adreseZaSpajanje` uzima i jedne i druge. |
| `internal/peers/peers.go` (`Discover`, `SavePeer`) | Adresa iz odgovora na pronalaženje je samo tvrdnja. Ostaje u memoriji dok razmjena na njoj ne prođe TLS s očekivanim ključem, i tek se tada nauči. Naučenih adresa po računalu je najviše 4, poredane po zadnjem uspjehu. Naučene adrese više ne diraju zapis čvora, pa ni njihov redoslijed ne stvara novu verziju. To je dio pola dana za točku 2. |
| `internal/peers` (mreža), `internal/db/schema.go` | Tablica mreže dobiva stupac `protokol`. Upisuju ga `CreateNetwork`, `joinNetwork` i `UveziPotvrdu`; postojeće mreže dobivaju `gocop`. Ista migracija dodaje stupac `adrese` u `peer_sync`. Kad se postavka razlikuje od upisanog, program ne krene i javi: „goCOP nije pokrenut: ova mapa podataka pripada mreži „gocop”, a u gocop.toml piše [sync] protokol = „gocop-sim”. Ispravi postavku ili pokreni goCOP s drugom mapom podataka.” |
| `internal/peers/tailscale.go` | Poslužitelj dobiva `Put`: običan TCP je `adresa`, TCP preko Tailscalea je `tailscale`, tunel je `tunel`. Za `Put` i za vrata učenja nova provjera gleda i lokalnu adresu veze: kad je ona vlastita Tailscale adresa ovog računala, put je `tailscale`, i kad je druga strana na 192.168.x (ruta podmreže ili izlazni čvor). `prekoTailscalea` danas prepoznaje samo drugu stranu na 100.64/10; za razmjenu Tailscale adresa ostaje kakva jest. |
| pomoćnik za testove | veza i slušalica u memoriji s međuspremnikom, za tunel i kasnije za sastajalište. To je običan paket s vlastitim testovima, jer ga koriste i `internal/razmjena` i `internal/peers`. |

`exchange()` se u F0 ne dira. To je posao G1.

**Testovi:**
- svi postojeći testovi prolaze; interni testovi koji zovu `serveTLS` (`ograde_test.go`, `rubovi_veze_test.go`) mijenjaju se samo zbog potpisa;
- `noteSync` ne stvara novu verziju zapisa čvora za `tunel`, `tailscale` i `probijeno`, ni za adresu oblika `p2p:x`;
- dolazni TCP s javne adrese (93.x.x.x) i s adrese 100.64/10 ne stvara verziju zapisa čvora;
- TCP preko Tailscale rute podmreže (lokalna adresa veze je vlastita Tailscale adresa, druga strana 192.168.x) dobiva `Put` `tailscale`, a adresa se ne nauči;
- adresa naučena pronalaženjem ili preko `noteSync` ne stvara novu verziju zapisa čvora, nego ostaje samo na ovom računalu, i nakon ponovnog pokretanja (točka 2);
- adresa iz pronalaženja na kojoj se javi krivi ključ ne nauči se;
- naučenih adresa ostaje najviše 4;
- `Put` se provjerava u `TestRedoslijedAdresaZaSpajanje` (`tailscale_test.go`), `prekidac_test.go` i `bez_tailscalea_test.go`;
- `TestPortRazmjeneZaAdresuBezPorta` pokriva i `nazovi`;
- greška se pamti za svaku adresu posebno;
- poslužitelj dobiva ispravan `Put` za TCP, tunel i prerušenu Tailscale vezu (`prerusenaVeza`);
- certifikat s krivim CN-om odbija se na TCP-u i u tunelu;
- stare oznake formata ne mijenjaju se kad se promijeni `[sync] protokol`;
- kopirana mapa podataka pokrenuta s protokolom `gocop-sim` ne krene;
- razmjena kroz tunel ide preko veze u memoriji, unutar synctesta.

**Vidi se.** Na pločici ništa. U mjerenju kvalitete `serveTLS`, `DialTunel` i `noteSync` imaju niže brojke nego danas.

**Prekidač.** Nema ga.

**Kompatibilnost.** Na žici se ništa ne mijenja, pa 0.0.37 i 0.0.38 ne primjećuju razliku. Novi stupci `protokol` i `peer_sync.adrese` su lokalni; povratak na 0.0.38 ostavlja ih nekorištene, što treba ručno provjeriti na kopiji baze laptopa [P]. Nakon povratka 0.0.38 adrese uči kao danas.

### Proba: cijela razmjena preko QUIC-a u synctestu (0,5–1 dan, okvirno 26. 10.)

Odmah nakon F0, prije F1 i prije jezgre F3. Na tome stoje svi testovi F1–F3.

- U `go.mod` ulazi `github.com/quic-go/quic-go v0.63.0` (MIT, bez C-a, traži Go 1.26+). Uz nju stiže samo `golang.org/x/sync` kao neizravna ovisnost; ostalo projekt već ima u novijim inačicama [V]. Syncthing koristi istu inačicu.
- U `internal/razmjena/quic.go` ide prilagodnik koji QUIC tok pretvara u `prijenos`: zatvaranje radi CancelRead pa Close, a adrese se mogu pročitati i nakon zatvaranja.
- Test: dva `simnet` računala, `exchange()` u obje uloge, baza otvorena unutar synctesta, `cryptotest.SetGlobalRandom`, 300 ponavljanja s istim ishodom. `testutils/simnet` uvozi se iz istog modula quic-go; ništa se ne kopira.
- Ako ishod nije uvijek isti, to se rješava prije F1. Danas synctest postoji samo u `uparivanje_cekanje_test.go`, s bazom otvorenom izvan njega, a `cryptotest` se još nigdje ne koristi.
- Grana s probom polazište je za jezgru F3 (vidi F3).

### LAN1 Pronalaženje i glasnik (0,5–1 dan, okvirno 27.–28. 10.)

**Cilj.** LAN sync, koji program uvijek proba prvi, nađe računalo brže i ne prestane raditi nakon jedne greške. Ovo je zaseban mali PR, jer ne ovisi o WEB syncu. Ide prije kandidata iz F1, jer donosi filtar sučelja koji F1 koristi.

| gdje | promjena |
|---|---|
| `internal/razmjena/discovery.go` | Proba ide više puta i završi čim se javi traženo računalo. Glasnik dobiva ogradu. |
| novi `internal/razmjena/sucelja.go` | Filtar sučelja, isti za probu pronalaženja i za kandidate u F1. |
| `internal/peers` (`Announce`, `uLokalnojMrezi`) | Glasnik se sam ponovno pokrene, a nalaz pronalaženja dijele sve razmjene u krugu. `run` se ne mijenja. |
| `docs/linux.md` | Pravilo za ufw, ako ga mjerenje u fazi M traži. |

Pravila:
- Proba ide tri puta, u 0, 150 i 400 ms, jer se jedan UDP paket lako izgubi. `FindDevice` završi čim se javi traženo računalo, a ne čeka punih 700 ms.
- Proba ne ide na sučelja `docker*`, `br-*`, `veth*`, `virbr*`, `utun*`, `tailscale*`, `vEthernet*` (Windows) i `bridge*` (macOS). Na Linuxu s Dockerom `docker0` (172.17.0.1) je adresa iz RFC 1918 i bez filtra bi postao cilj probe i lokalni kandidat.
- Glasnik odgovara samo izvorima iz vlastite podmreže, s petlje i iz link-locala, najviše 20 odgovora u sekundi ukupno i 2 po izvoru.
- Glasnik se nakon greške čitanja ili slanja sam ponovno pokrene: nakon 1 s, pa sve dulje, najviše za 1 min. Svaki pad zapiše se u dnevnik.
- U jednom krugu razmjene pronalaženje ide jednom, a nalaz vrijedi 60 s za sva računala. Danas ga svaka razmjena ponavlja.
- Ako mjerenje u fazi M pokaže da ufw baca odgovore na probu s prolaznog porta, `docs/linux.md` dobiva pravilo koje pušta UDP s izvornog porta 4712 iz lokalne mreže. Ako ni to ne pomogne, proba ide s utičnice glasnika.

**Testovi:**
- prva proba se izgubi, a računalo se ipak nađe;
- `FindDevice` vrati nalaz prije isteka 700 ms;
- četiri razmjene u istom krugu dijele jedno pronalaženje;
- glasnik se digne nakon greške čitanja i nakon greške slanja;
- glasnik ne odgovara izvoru izvan svoje podmreže, a 21. odgovor u sekundi ne šalje;
- proba ne ide na izbačena sučelja, ni na `docker0` (lažni popis sučelja).

**Vidi se.** Laptop u uredu na kabelu nađe ThinkCentre za manje od pola sekunde. Glasnik radi i nakon greške, bez ponovnog pokretanja programa.

**Prekidač.** Nema ga.

**Kompatibilnost.** Staro računalo odgovori na svaku od tri probe, a novo uzme prvi odgovor. Odgovor i dalje ide na izvorni port probe.

### F1 UDP, STUN i put na pločici (5–5,5 dana, okvirno 27. 10.–3. 11.)

**Cilj.** Računalo s uključenom prijavom za WEB sync zna kakva mu je mreža i to pokaže. Pločica uz svako računalo kaže kojim je putem prošla zadnja razmjena. Razmjena radi kao danas.

| gdje | promjena |
|---|---|
| `go.mod` | quic-go je već tu od probe. Veličina binarke mjeri se prije i poslije F1, za sva četiri sustava. |
| `internal/razmjena/quic.go` | `quic.Transport` nad UDP utičnicom (zadano 4713), još bez QUIC slušalice; otvara se samo uz `web_sync_prijava = true`. |
| `internal/razmjena/stun.go` | STUN Binding bez nove ovisnosti, oko 200 redaka. Odgovor se prepoznaje po prvom bajtu 0x00/0x01 i po kolačiću. |
| `internal/razmjena/netcheck.go` | Provjera mreže: usporedba odgovora Googlea i Cloudflarea s iste utičnice. |
| `internal/razmjena/kandidati.go` | Vlastiti kandidati, s filtrom sučelja iz LAN1 i provjerom stvarnog puta. |
| novi `internal/peers/pad_povezivosti.go` | Prekidač pri padu, od prvog otvaranja `quic.Transport`. |
| `internal/config`, `internal/web/postavke_cvora.go` | Odjeljak `[povezivost]`: `web_sync_prijava = false`, `web_sync = false`, `udp_port = 4713`, `stun = [...]`, `lan_adresa = ""`. U Administraciji se smiju mijenjati `web_sync_prijava` i `web_sync`. `udp_port`, `stun` i `lan_adresa` samo se čitaju, uz razlog: tuđi STUN poslužitelj saznao bi javne adrese računala i mogao vratiti lažne, a krivi port odsiječe računalo. |
| `cmd/gocop/main.go` | Jedan redak u `run`: `pokreniPovezivost(...)`. Utičnica i provjera mreže kreću u pozadini, tek kad web već sluša, jer Postava nakon 90 s vraća stari program. Ne ovise o `exchange_port > 0`. |
| `internal/db/schema.go`, `internal/peers/status.go` | `peer_sync` dobiva put, RTT, razlog i brojače (pojedinosti niže). `recordSyncState` ih upisuje, a `PeerStatus` nosi; novo ide u nove funkcije, bez nove grane u `Status`. |
| pločica: `cmd/gocop/arhiva_razmjena.go`, `internal/web/razmjena_plocica.go`, predlošci | Uz svako računalo put zadnje razmjene i, kad put ne radi, razlog kao gotova rečenica. |
| Sinkronizacija i Administracija: `internal/web/handlers_sync.go` (`ShowDashboard`), `server.go` | Blok „Dohvatljivost ovog računala” na stranici Sinkronizacija, koju vidi samo administrator. U Administraciji gumb „Provjeri vezu” i, kad je WEB sync isključen nakon padova, gumb „Ponovno uključi WEB sync”. |
| `Dockerfile` | `EXPOSE 4713/udp`, `QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING=1` |
| `docs/INSTALACIJA.md`, `docs/linux.md`, `docs/CODE_SIGNING.md` | 4713/udp: UDP u predlošku za Unraid, ufw, Windows. Ista varijabla okoline u systemd jedinici. STUN poslužitelji ulaze u pravila privatnosti, uz napomenu da ih `stun = []` isključuje. goCOP sam ne mijenja pravila vatrozida; dopuštenje daje čovjek. Nebula na Windowsu sama dodaje pravilo ispod Defendera, a to na službenim računalima ne smijemo. |

Pravila za UDP utičnicu (`quic.go`):
- Jedan čitač `ReadNonQUICPacket` kreće prije prvog STUN upita. Razvrstava STUN, udarce i ostalo, a odbačene pakete broji.
- Kad se port ne da otvoriti, iz bilo kojeg razloga, uzima se nasumičan port i javlja upozorenje.

Pravila provjere mreže (`netcheck.go`):
- Ista adresa i port od Googlea i Cloudflarea znače lak NAT. To su različiti operateri; stun i stun1 se nikad ne uspoređuju.
- Bilježi se čuva li NAT port, ali to je samo podatak, ne uvjet. Bez odgovora znači da je UDP blokiran.
- Vrsta NAT-a i „UDP blokiran” mijenjaju se tek nakon dva ista mjerenja zaredom, a „nepoznato” ne briše poznatu vrijednost. Jedan izgubljen odgovor tako ne mijenja ni broj pokušaja ni natpis.
- Provjera radi pri pokretanju i na gumb, a od F2 i pri ponovnom spajanju na sastajalište i prije poziva, kad je zadnji rezultat stariji od 20 s.
- Osvježavanje prije poziva pita oba poslužitelja usporedno, ponavlja nakon 250 i 750 ms i ima rok 2 s.
- Uz `web_sync_prijava = false` gumb otvori privremenu utičnicu i zatvori je nakon mjerenja; stranica tada kaže da rezultat vrijedi samo za tu provjeru.

Pravila za kandidate (`kandidati.go`):
- Lokalne adrese su samo iz RFC 1918. Tailscale se prepoznaje provjerom iz `internal/tailscale`, ne po imenu kartice.
- Za svaki vlastiti kandidat i svaki STUN poslužitelj otvori se povezana UDP utičnica, bez slanja, da se vidi kroz koje sučelje promet ide.
- Izbacuju se sučelja s popisa iz LAN1 (`sucelja.go`).
- Najviše 8 kandidata; irohov test pokazuje da probijanje s 32 lokalne adrese pada, a s 8 prolazi.
- `lan_adresa` služi za Docker. Lokalni kandidat ne mora biti u istoj podmreži kao druga strana.

Prekidač pri padu (`pad_povezivosti.go`, poziva ga `pokreniPovezivost`):
- Kad se otvore UDP utičnica i `quic.Transport`, uz bazu se upiše oznaka `povezivost-u-radu`, a pri urednom gašenju se briše. Oznaka pamti na koliko je pokretanja zaredom zatečena.
- Kad je oznaka zatečena na dva pokretanja zaredom, program krene bez WEB synca, kao uz `web_sync_prijava = false`, i to kaže na pločici i u dnevniku. `gocop.toml` ostaje kakav jest.
- Administrator u Administraciji gumbom „Ponovno uključi WEB sync” briše oznaku. To vrijedi od idućeg pokretanja, kao i promjena postavki u Administraciji.
- quic-go nema `recover()`, a naš `recover` u `serveTLS` ne pokriva njegove gorutine, pa bi greška inače vrtjela program u krug padova. Prijenos radi od izdanja 2 na L, T i W, pa zaštita mora stići prije, s izdanjem 1.

Novo u `peer_sync`:
- `put`, `rtt_ms`, `put_od` i `put_razlog` (kod, ne tekst);
- brojači od `put_od`: pokušaji, uspjesi, neuspjesi, primljeni i poslani bajtovi, i tko je zadnji zvao (`ovaj` ili `drugi`);
- brojači priprema WEB synca (pokušaji, uspjesi, neuspjesi), koje puni tek F3b; tako F3b ne treba novu migraciju.

Kodovi razloga su stalan popis: nije se javio, UDP ne prolazi, oba NAT-a ovisna o odredištu, UDP do drugih računala ne prolazi, vatrozid na računalu možda ne pušta UDP, nije član ili je opozvan, stariji program, WEB sync se priprema, pauza nakon neuspjeha, isti ključ na još jednom računalu. Razlozi koji se sami ne poprave (nije član, stariji program, isti ključ) označeni su posebno.

Pločica i Sinkronizacija:
- Uz svako računalo piše „zadnja razmjena: LAN sync, 3 ms”.
- Kad put ne radi, razlog je gotova rečenica iz koda, bez adresa, na primjer „Računalo se ne javlja u lokalnoj mreži; podaci idu preko poslužitelja (SERVER sync).” Razlog koji traži čovjeka piše se drukčije od prolaznog.
- Pločicu vidi svaki prijavljeni korisnik, pa sirovi tekst greške i adrese, za sve putove, odlaze na stranicu Sinkronizacija.
- Preslikavanje `Put` → natpis je na jednom mjestu i već zna buduće `probijeno`. `SamoDolazi` („javlja se sam, kroz tunel”) vrijedi samo kad je zadnji put bio tunel.
- U Dockeru (`GOCOP_NADZOR=docker`) pločica upozori kad je `lan_adresa` prazna.
- Na Sinkronizaciji za računalo koje ne odgovara stoji svaka adresa sa zadnjom greškom i vremenom (iz F0).
- Gumb „Provjeri vezu” je nova POST ruta s obrađivačem, registrirana kroz novu funkciju za rute povezivosti.

Simulator NAT-a (`natsim`) ne ide u F1, nego u jezgru F3, gdje se stvarno koristi. Za provjeru mreže dovoljna su dva lažna STUN poslužitelja.

**Testovi:**
- vektori iz RFC 5769 i fuzz STUN parsera;
- provjera mreže s dva lažna STUN poslužitelja: lak NAT, NAT ovisan o odredištu, odgovorio samo jedan, blokiran UDP;
- jedno izgubljeno mjerenje ne mijenja vrstu NAT-a; „nepoznato” ne briše poznatu vrijednost;
- osvježavanje STUN-a pita oba poslužitelja, ponavlja nakon 250 i 750 ms i završi u 2 s;
- filtar kandidata s lažnim popisom sučelja i lažnim usmjeravanjem (Tailscale, izlazni čvor, WARP, `docker0`);
- 33 i više paketa naglo, prije čitanja i za vrijeme čitanja;
- zauzet port;
- uz `web_sync_prijava = false` nitko ne drži port: `net.ListenPacket` na :4713 uspije;
- prekidač pri padu s lažnim prijenosom koji padne: nakon dva pada program krene bez WEB synca, a pločica to kaže; gumb briše oznaku, pa iduće pokretanje opet otvara utičnicu;
- dva do četiri testa na pravoj utičnici 127.0.0.1 (Linux u CI-ju, macOS lokalno);
- pločica za svaki `Put` i svaki razlog: u HTML-u nema ni IPv4 ni IPv6 adrese ni riječi „Unraid”;
- Sinkronizacija pokazuje grešku po adresi, a pločica ni tada nema adresa;
- testovi za `stanjeRazmjene` stižu već s PR-om o disku.

**Vidi se:**
- na pločici uz svako računalo stvarni put kojim danas ide: LAN sync, Tailscale ili SERVER sync;
- na stranici Sinkronizacija, na primjer: „UDP radi na :4713. NAT je lak i čuva port. STUN: Cloudflare 8 ms.”, i za računalo koje ne odgovara svaka adresa sa zadnjom greškom;
- „Provjeri vezu” na T, W i L pokazuje isto što i alat za mjerenje na istom mjestu. Na Unraidu se uspoređuje s probnim spremnikom u mostu iz faze M, jer alat radi na samom Unraidu, a gocop u spremniku;
- u dnevniku po jedan redak `povezivost:` za svaku promjenu, bez tuđih javnih adresa.

**Prekidač.** `web_sync_prijava = false` je zadano. Tada nema UDP utičnice ni STUN upita osim na gumb. Na testnim računalima `web_sync_prijava = true` pokreće provjeru mreže pri pokretanju.

**Kompatibilnost.** Na žici nema ničeg novog. Nove kolone u `peer_sync` su lokalne. Povratak na 0.0.38 trebao bi ih ostaviti nekorištene, kao kolonu `tailscale` iz 0.0.37, ali to treba ručno provjeriti na kopiji baze laptopa [P].

### F2 Sastajalište (4,5–5,5 dana, okvirno 3.–10. 11.)

**Cilj.** Testna računala i sam Unraid prijave se za WEB sync i drže vezu, i nakon buđenja i promjene mreže. Unraid zna tko je na vezi i prosljeđuje potpisane pozive, a pozive kojima je sam cilj obradi. Razmjena još ide starim putem.

F2 ima dva PR-a, jer je `internal/web` zajednički s korakom 5 faze 3.

**F2a, dio u `internal/web` (nakon spoja F1, okvirno 3.–5. 11.; spaja se prije nego korak 5 faze 3 počne mijenjati `internal/web`):**

| gdje | promjena |
|---|---|
| `internal/web/server.go`, `rokovi.go` | Ruta `GET /razmjena/signal` registrira se samo uz `posluzi_sastajaliste = true`; inače vraća 404. U `pravilaRuta` dobiva oznaku `netaknuto`, jer je naš poslužitelj inače prekida nakon 60 s. Test provjerava da je svaka WebSocket ruta `netaknuto`. |
| novi `internal/web/ograda_signala.go` | 64 veze ukupno, 8 po adresi (cijeli ured je jedna ili dvije adrese), po 2 rukovanja po adresi s rokom od 2 s za unutarnji TLS. Tako dvije adrese ne mogu zauzeti sva mjesta za rukovanje. |
| pločica, Sinkronizacija, Administracija | Polja za stanje signala iza sučelja `SignalStanje`, s privremenom zamjenom dok ne stigne F2b. Svi natpisi i razlozi za F2 i F3 pišu se sada (popis niže), da F3 ne mora dirati `internal/web`. Na Unraidu: broj prijavljenih računala i poziva u zadnjem satu. Broj prekida u 24 h. |
| pločica (odgoda) | Prolazno stanje dolazi na pločicu tek kad traje dulje od 10 s, a dok traje uzrok, posljedice se ne pokazuju. Dok sastajalište nije dostupno, pločica ne piše još i da WEB sync ne uspijeva. Greška koja se sama ne popravi (ruta vraća 404, krivi ključ sastajališta, nije član, isti ključ) dolazi odmah. |
| Administracija | Gumb „Provjeri vezu s računalom” (samo administrator): ispis koraka (STUN, poziv, odziv, udarci po kandidatu, QUIC, put, RTT) i razlog za svaku adresu. Od F3 svaki pritisak otvara novu WEB sync vezu, mimo pričuve sesija, pa služi i za mjerenje na terenu. To je jedino mjesto gdje se čeka na probijanje. |
| `internal/web/postavke_cvora.go` | Retci za `sastajaliste`, `sastajaliste_cvor` i `posluzi_sastajaliste`; sve tri samo se čitaju, uz razlog. |

Natpisi za F2 i F3. Svaki kaže što se nije dogodilo, zašto i što učiniti. Ime poslužitelja je iz postavki, a ime puta je put kojim je razmjena stvarno išla (SERVER sync ili Tailscale):
- „WEB sync: prijavljen kod cop-osijek.com od 12:01. Podaci još idu kao prije.” (druga rečenica stoji dok je `web_sync = false`)
- „Računalo Windows ima stariji goCOP i ne zna za WEB sync. Nadogradi ga.”
- „WEB sync je isključen: goCOP je dvaput zaredom pao dok je WEB sync radio. Podaci idu kao prije. Uključi ga u Administraciji nakon nadogradnje.”
- „WEB sync se priprema; ova razmjena ide SERVER syncom.”
- „WEB sync s ovim računalom ne uspijeva. Podaci idu SERVER syncom, a WEB sync pokušava ponovno u 14:30.”
- „WEB sync iz ove mreže ne uspijeva; podaci idu preko poslužitelja (SERVER sync).” Pojedinosti o UDP-u i NAT-u idu na Sinkronizaciju.
- „Vatrozid na ovom računalu možda ne pušta WEB sync. Dopusti UDP 4713 u vatrozidu; uputa je u pomoći.”
- „Na još jednom računalu radi kopija ovog goCOP-a. Ugasi kopiju; WEB sync ovdje pokušava ponovno za 10 minuta.” (na odbijenom računalu; na drugom samo prva rečenica i „Ugasi kopiju.”)
- „Sat ovog računala odstupa 3 min od poslužitelja. Uključi automatsko podešavanje sata.” (kad je odstupanje veće od 2 min)

**Dva mala PR-a prije F2b** (nisu u danima F2):
- **Primanje na daljinu se isključuje** (0,5 dana; točka 4). Ostaje isključeno dok se ne odluči kako ulaze ispostave. PR skriva korak „Postojeća mreža na daljinu” u Postavljanju (`handlers_postavljanje.go`) i isključuje uvoz potvrde. Kod ostaje. Uvoz potvrde javlja: „Primanje na daljinu zasad je isključeno. Računalo se uparuje u lokalnoj mreži ili preko Tailscalea.” Test: korak se ne vidi, a uvoz potvrde se odbija.
- **Ispravak zapisa čvora** (0,5–1 dan [P]). Danas primljeni zapis čvora prepisuje javni ključ poznatog računala (`primijeniCvor`), a `SyncWith` očekivani ključ uzima iz tog zapisa (`partnerZaRazmjenu`). Nakon ispravka očekivani ključ za TCP i tunel dolazi iz `memberships` (potpisano ključem mreže), po oznaci računala. Istu funkciju F2b i F3b koriste za WEB sync. Test: član pošalje zapis čvora s ključem drugog člana, a razmjena i dalje traži pravi ključ.

**F2b, ostatak (okvirno 5.–10. 11.):**

| gdje | promjena |
|---|---|
| `internal/razmjena/okvir.go` | Jedan oblik okvira za signal i kasnije za kontrolni kanal: 4 bajta duljine, najviše točno 64 KiB. |
| `internal/razmjena/keys.go` | Inačica `tlsConfigS` s `NextProtos` (`<protokol>-signal/1`, kasnije `<protokol>-web/1`) i s `VerifyConnection`, koji poziva `trusted(pub, vj)`. Kao poslužitelj računalo šalje certifikat samo s ključem; članstvo i ovlast idu samo u certifikatu klijenta. Strani klijent tako ne vidi članstvo. Na svakom putu kojim računalo zove, `VerifyPeerCertificate` traži da ključ bude onaj očekivani, pa računalo stane prije nego pošalje svoj certifikat. 0.0.37 i 0.0.38 ne čitaju vjerodajnice poslužitelja, pa to ništa ne kvari. TCP i tunel ostaju bez ALPN-a. |
| `internal/razmjena/signal.go` | Klijent: prijava kod sastajališta, puls i ponovno spajanje (pravila niže). |
| `internal/razmjena/sastajaliste.go` | Poslužitelj, po uzoru na `Tunel.Handler`: registar prijavljenih i prosljeđivanje poruka (pravila niže). |
| `internal/razmjena/poziv.go` | Potpis ide nad dekodiranim bajtovima tijela. Prefiksi dolaze iz imena protokola. Vrijeme smije odstupati ±10 min. Pričuva sesija drži 8192 sesije, a kad je puna, odbija nove. `sinkro` nosi `pokusaj` (1–3). Polje `sastajaliste` u tijelu nosi ključ sastajališta. Računalo prima poziv samo kad je to ključ njegovog sastajališta iz postavki; na samom sastajalištu to je njegov vlastiti ključ. |
| novi `internal/razmjena/nova_uticnica.go` | Kad se promijeni skup lokalnih adresa (nakon filtra sučelja), ili kad slanje s utičnice 4713 javi „no route to host”, zatvaraju se UDP utičnica i `quic.Transport` i otvaraju novi na istom portu, s novim čitačem. Zove samo funkcije iz `quic.go`. To vrijedi na svim sustavima. Na macOS-u je nužno: po Nebuli, Darwin veže UDP utičnicu uz sučelje na kojem je nastala, pa nakon selidbe šalje u prazno (mjeri se nakon izdanja 1). Sesije preko stare utičnice ionako su mrtve, a F3b ih vraća u pozadini. |
| `internal/peers` (sastajalište i ključevi) | `Service` drži signalnog klijenta. Sastajalište se uzima samo iz lokalnih postavki `sastajaliste` (na primjer `https://cop-osijek.com`) i `sastajaliste_cvor` (oznaka računala, ključ iz `memberships`). Zapis čvora u knjizi (`IsBootstrap`) se za to ne koristi, jer ga može promijeniti bilo koji član. Ključ cilja za poziv uzima se iz `memberships` (potpisano ključem mreže), po oznaci računala, nikad iz zapisa čvora (`peers.public_key`). To je ista funkcija kao u ispravku zapisa čvora. Mogućnosti drugih računala dolaze iz `peer_sync` (G2) i iz registra; u zapis čvora ništa ne ide. |
| `internal/peers` (buđenje i promjena mreže) | Klijent prati spavanje računala i promjenu adresa (pravila niže). |
| `cmd/gocop/main.go` | Ništa novo u `run`; sve kreće iz `pokreniPovezivost`. `posluzi_sastajaliste = true` postavlja se ručno, samo na Unraidu, uz `web_sync_prijava = true`. |

Pravila klijenta (`signal.go`):
- WebSocket na `/razmjena/signal` ide kroz zajedničku funkciju za spajanje iz F0, pa unutarnji TLS s očekivanim ključem sastajališta, zatim `prijava` i `prijavljen`.
- `prijava` nosi nasumičan broj procesa. `prijavljen` mora doći od člana mreže i nositi oznaku `sastajaliste/1`.
- Puls ide svakih 25 s, rok čitanja je 75 s.
- Ponovno spajanje čeka 2 s · 2ⁿ, najviše 60 s, ±20 %. Nakon buđenja ili promjene lokalnih adresa odgoda kreće ispočetka.
- Kad WebSocket ne uspije, klijent čeka i pokuša ponovno; to nikad ne znači da Unraid nije sastajalište.

Pravila sastajališta (`sastajaliste.go`):
- Registar je samo u memoriji: najviše 64 računala, koliko je i veza, i jedna veza po ključu.
- Kad se isti proces ponovno spoji, nova veza zamijeni staru, jer Cloudflare zna ostaviti poluotvorenu vezu.
- Kad se javi drugi proces s istim ključem, sastajalište staroj vezi pošalje puls s rokom od 5 s. Ako stara ne odgovori, nova je zamijeni: to je program koji je ponovno pokrenut, na primjer nakon nadogradnje. Ako stara odgovori, nova se odbije s razlogom „isti ključ spojen drugdje” i javi se tek za 10 min, a dnevnik i pločica na Unraidu i na odbijenom računalu kažu da radi kopija. To je naša odluka [P]. iroh u tom slučaju novu vezu čini aktivnom, a staru drži u pričuvi; od njega je uzeto samo to da nova veza ne izbacuje staru naslijepo, jer bi se dva računala s istim ključem izbacivala u krug.
- Člana prepoznaje iz retka `memberships` ili iz provjerenog članstva u certifikatu, istom provjerom kao `trusted()`.
- `poziv`, `odziv` i `sinkro` prosljeđuje bajt po bajt; za nepoznat cilj vraća `nema`. Tijelo čiji `od` nije ključ iz TLS-a odbacuje.
- Granice iz §6.2: 6 poziva u minuti po pozivatelju, 2 istodobne sesije prema istom cilju, 8 nedovršenih prema istom cilju, najviše 20 poruka u sekundi po vezi, inače prekid.
- Nakon primjene novih članstava, ovlasti ili opoziva odmah ponovno provjeri sve prijave.

Sastajalište kao cilj:
- Uz `posluzi_sastajaliste = true` računalo je samo sebi sastajalište. U svoj registar upisuje se sam, bez WebSocketa, a postavke `sastajaliste` i `sastajaliste_cvor` ostaju prazne.
- Poziv kojem je cilj sastajalište stiže istom signalnom vezom kao i ostali. Sastajalište ga ne prosljeđuje, nego ga obradi kao cilj i odziv pošalje natrag istom vezom. Tako laptop na hotspotu može pripremiti WEB sync s Unraidom.
- Kad Unraid zove ThinkCentre, poziv iz vlastitog registra ide ravno na signalnu vezu ThinkCentrea.

Pravila buđenja i promjene mreže (`internal/peers`):
- Klijent dobiva sat izvana, pa se može testirati lažnim satom.
- Svakih 15 s gleda koliko je prošlo po zidnom satu; razmak veći od 22,5 s znači da je računalo spavalo.
- Nakon kratkog spavanja računalo pošalje puls sastajalištu s rokom od 5 s i pogleda adrese sučelja. Veza se ne otvara iznova, jer se macOS i u snu kratko budi.
- Nakon spavanja duljeg od 10 min, ili kad se promijene lokalne adrese, računalo se ponovno spoji: odgoda kreće ispočetka, otvara se nova UDP utičnica i ponovi se provjera mreže.
- Adrese sučelja provjeravaju se svakih 10 s.
- Prije provjere mreže računalo čeka zadanu rutu: povezana UDP utičnica prema STUN poslužitelju, ponovno nakon 100 ms, pa dvostruko dulje do 1 s, najviše 5 s. Bez toga provjera odmah nakon promjene mreže javi lažni „UDP blokiran”.

**Ručno, korisnik:** jedno pravilo ograničenja brzine u Cloudflareu samo za `/razmjena/signal`, na primjer više od 30 zahtjeva u minuti s iste adrese → blokiraj. Prag nije 10: kod kuće do četiri računala izlaze s iste javne adrese, a nakon pada Unraida svako u prvoj minuti pokuša do pet puta. Tunel `/razmjena/tunel` pravilo ne dobiva: računalo koje sustiže zaostatak otvara novu vezu za svaki od do 200 krugova, a za ThinkCentre je tunel jedini put. Treba provjeriti da preskakanje WAF-a ne zaobilazi pravilo i da ulaz cloudflared za cop-osijek.com nema pravilo puta koje bi za `/razmjena/signal` vratilo 404. Spremnik cloudflared od BP16 se ne dira.

**Testovi:**
- synctest s pravim `http.Server` na slušalici u memoriji i pravom rutom;
- lažni Cloudflare unutar synctesta: `httputil.ReverseProxy` čiji `Transport` zove slušalicu u memoriji, dodaje `CF-Connecting-IP` i prekida vezu na naredbu;
- klijenti: A, B, stranac, opozvani član, član druge mreže, drugo ime protokola;
- puls i rok od 75 s; prosljeđivanje bajt po bajt; krivotvoren `od`; `nema`;
- opozvani član nestaje iz registra čim se opoziv primijeni; granica od 64 KiB;
- tek upareno računalo, čije članstvo Unraid još nema, prijavi se s članstvom iz certifikata;
- kad poslužitelj zatvori vezu, klijent se ponovno spoji; odgoda raste do 60 s i kreće ispočetka nakon buđenja ili promjene adresa;
- isti proces se ponovno spoji dok je stara veza poluotvorena: ostaje nova;
- drugi proces s istim ključem dok stara veza ne odgovara na puls: ostaje nova; dok stara odgovara: nova je odbijena, javi se tek za 10 min, a upozorenje je na oba računala;
- sedmi poziv u minuti se odbija; 21 poruka u sekundi prekida vezu; poplava od 64 veze, 8 po adresi i rukovanja po adresi; 65. računalo ne ulazi u registar;
- sastajalište prima poziv kojem je samo cilj, ne prosljeđuje ga i odgovori istom vezom; uz `posluzi_sastajaliste = true` računalo ne otvara WebSocket prema sebi;
- poziv čije polje `sastajaliste` nosi ključ drugog računala se odbija;
- ključ cilja dolazi iz `memberships`: zapis čvora s podmetnutim ključem ne mijenja cilj poziva;
- strani klijent koji otvori rukovanje prema tunelu ili signalu ne vidi članstvo u certifikatu;
- član koji sam sebi postavi `is_bootstrap` ne postaje ničije sastajalište;
- lažni sat: razmak od 30 s pošalje samo puls, razmak od 11 min ponovno spoji i otvori novu utičnicu;
- promjena adresa sučelja: nova utičnica i novi čitač, a broj gorutina ostaje isti; greška slanja „no route to host” isto;
- provjera mreže nakon promjene mreže čeka rutu i ne javi „UDP blokiran” dok rute nema najviše 5 s;
- prolazno stanje kraće od 10 s ne dođe na pločicu, a trajna greška dođe odmah;
- broj gorutina nakon zatvaranja signalne veze;
- jedan ili dva testa u stvarnom vremenu kroz `httptest`, bez provjere rokova;
- fuzz dekodera; provjera potpisa.

**Vidi se.** Laptop (na hotspotu), ThinkCentre i Windows pokazuju „WEB sync: prijavljen kod cop-osijek.com. Podaci još idu kao prije.” Unraid pokazuje „prijavljena 3 računala”. Prekidi se broje 24–72 h. Laptop nakon spavanja duljeg od 10 min opet je prijavljen za manje od minute. Razmjena i dalje ide starim putem.

**Prekidač.** `posluzi_sastajaliste = false` je zadano; na Unraidu se uključuje ručno, uz `web_sync_prijava = true`. Ostala računala s `web_sync_prijava = true` se prijavljuju.

**Kompatibilnost.** Stara računala tu rutu ne zovu. Novo računalo prema Unraidu bez rute ne uspije otvoriti WebSocket, pa čeka i radi starim putem. U knjigu ne ide ništa.

### F3 WEB sync: QUIC i probijanje (7,5–9 dana)

F3 ima dva dijela. Jezgra (F3a) je samo u novim datotekama u `internal/razmjena` i u `internal/natsim`. Ide u zasebnom radnom stablu čim je proba QUIC-a gotova, od grane s probom, protiv zamjene za signal u memoriji. Nakon spoja F1 presloži se na glavnu granu. Redoslijed i spoj (F3b) su u `internal/peers` i kreću kad su spojeni F2b i F3a.

**Cilj.** Dva testna računala iza NAT-a razmjenjuju izravno preko interneta. Kad to ne uspije, razmjena ide kao danas. Nijedna razmjena ne čeka probijanje.

**F3a, jezgra (4,5–5,5 dana, okvirno 27. 10.–9. 11.; spaja se nakon spoja F1):**

| gdje | promjena |
|---|---|
| `internal/natsim` (novo; nije kritično, samo za testove) | NAT usmjernik na `simnet`, uz dva lažna STUN poslužitelja i test da ga `cmd/gocop` ne uvozi. Vlastiti testovi do barem 85 %. |
| novi `internal/razmjena/quic_slusalica.go` | QUIC slušalica na istoj utičnici, samo uz `web_sync = true`, i pozivanje druge strane. |
| novi `internal/razmjena/sesija.go` | Sesija i kanali nad QUIC vezom. |
| novi `internal/razmjena/udarac.go` | Udarci: paket od 60 B s HMAC-om. |
| novi `internal/razmjena/probijanje.go` | Stroj stanja probijanja kao tablica prijelaza (čista funkcija). Poruke `poziv`, `odziv` i `sinkro` samo čita; njihov oblik je u `poziv.go` (F2b). |
| novi `internal/razmjena/kandidati_druge.go` | Provjera kandidata druge strane prije probijanja. `kandidati.go` iz F1 se ne dira. |

Simulator NAT-a (`natsim`) zna:
- preslikavanje (EIM, po adresi, po adresi i portu), filtriranje, istek po lažnom satu, čuvanje porta i hairpin;
- blokiran UDP, dvostruki NAT i više javnih adresa;
- dva ili tri računala s istim unutarnjim portom iza istog NAT-a;
- vatrozid na računalu koji baca nenaručene pakete;
- conntrack u Docker mostu koji kod istodobnog slanja napravi krivo preslikavanje;
- kad je red pun, usmjernik baca paket, kao `simnet.NewSimConn` (a ne kao `NewBlockingSimConn`, koji čeka).

Postavke QUIC-a (`quic_slusalica.go`):
- rukovanje 5 s, mirovanje 60 s, održavanje veze svakih 15 s, 8 dolaznih tokova, `MaxIncomingUniStreams: -1`, bez 0-RTT-a i bez datagrama, `StatelessResetKey` iz ključa čvora;
- `VerifySourceAddress` uvijek vraća true, pa svatko dobiva Retry;
- `ConnContext` pušta samo adresu koja je u zadnjih 30 s poslala valjan udarac ili odgovor druge strane;
- zove računalo s manjim ključem; ALPN je `<protokol>-web/1`.

Pravila sesije (`sesija.go`):
- Kanal 0x00 je kontrolni (`pozdrav`, `ping`, `zatvaram`). Kanal 0x01 nosi današnji razgovor bajt po bajt i zauzima jedno od četiri zajednička mjesta za dolazne razmjene. Za svaki drugi kanal vraća se 0x0101. Kanali 0x02 i 0x03 („stranice”) samo su rezervirani.
- Sesija vrijedi tek kad stigne `pozdrav`. `pozdrav` nosi sesiju iz potpisanog poziva; QUIC veza iz starijeg pokušaja se zatvara.
- Sesija živi 6 min nakon zadnjeg toka, dakle dulje od kruga razmjene od 5 min, pa idući krug ne probija ponovno. Održavanje veze svakih 15 s drži NAT otvorenim.
- Ključ iz TLS-a mora biti ključ druge strane te sesije; sesija sa samim sobom (isti ključ) se odbacuje.
- `trusted()` se provjerava na početku svakog toka 0x01 i odmah nakon primjene novih članstava, ovlasti ili opoziva; tada sesija dobiva 0x0104 i zatvara se.

Pravila udaraca (`udarac.go`):
- Dva ključa, po jedan za svaki smjer: HKDF bez soli, 32 B, oznaka `<protokol>-udarac-v1`, sesija i smjer (A je računalo s manjim ključem). Brojač je big-endian, zaseban za svaki smjer.
- Udarac vlastitog smjera se odbija, pa se ne može odbiti natrag pošiljatelju.
- Kandidati druge strane jednaki vlastitim adresama ne udaraju se.
- Udarci idu samo potpisanim kandidatima druge strane. Granica je zajednička za sve kandidate jednog pokušaja: najviše 20 udaraca u sekundi i 60 po pokušaju. Kandidati dobivaju udarce naizmjence. Razmak između dva udarca istom kandidatu raste od 50 do 200 ms, ali ukupna granica vrijedi i tada: s 8 kandidata svaki dobije udarac otprilike svakih 400 ms.
- Na valjan udarac druge strane računalo odgovara na adresu s koje je udarac stigao. To je jedina iznimka od pravila „samo potpisanim kandidatima”: odgovor ide samo na adresu s koje je upravo stigao valjan udarac, iste je veličine kao udarac i ulazi u istu granicu. Ta adresa pamti se kao nova adresa druge strane i ulazi u vrata za QUIC (`ConnContext`). Tako radi par u kojem drugu stranu vidi tek NAT, kao kod simetričnog NAT-a ili dvostrukog NAT-a kod kuće.
- Odgovor druge strane prima se samo s adrese na koju je poslan udarac.

Pravila probijanja (`probijanje.go`):
- Rade se 3 pokušaja, osim kad je kod obiju strana potvrđen NAT ovisan o odredištu; tada 1.
- Pokušaj traje do 5 s: udarci 3 s, a QUIC kreće na prvi valjan udarac.
- U 1. pokušaju vrijedi sinkro po RTT-u, u 2. B kreće 1 s kasnije, a u 3. A, zbog utrke u Linux conntracku.
- Kad prvi valjan udarac stigne s javnog kandidata, čeka se do 500 ms udarac s lokalnog; ako stigne, QUIC ide lokalnim putem.
- Računalo `sinkro` prima samo za vlastitu aktivnu sesiju koja čeka sinkro, i samo kad je `pokusaj` veći od zadnjeg.

Kandidati druge strane (`kandidati_druge.go`):
- Prije probijanja otvara se povezana UDP utičnica, bez slanja, i prema svakom kandidatu druge strane.
- Kandidat se izbacuje kad promet do njega ide preko Tailscale adrese ili sučelja (rute podmreža, izlazni čvor).
- QUIC put čija je lokalna adresa Tailscale vodi se kao `tailscale`.

**F3b, redoslijed i spoj (3–3,5 dana, okvirno 11.–17. 11.; nakon spoja F2b i F3a):**

| gdje | promjena |
|---|---|
| `internal/peers/peers.go` (redoslijed) | Novi redoslijed putova po Odlukama (14. 10. 2026.), pravila niže. `SyncWith` ostaje na CC ≤ 7; novo ide u pomoćne funkcije. Kandidati druge strane iz poziva i odziva drže se samo u memoriji. |
| `internal/peers` (priprema u pozadini) | WEB sync se priprema u pozadini, a `SyncWith` ga ne čeka. |
| `internal/peers` (pauza) | Pauza po paru nakon neuspjelih priprema. |
| `internal/peers` (ključ cilja) | Ključ cilja za poziv i `expect` za QUIC uzimaju se iz `memberships`, po oznaci računala, kao u F2b. Zapis čvora (`peers.public_key`) za WEB sync se ne koristi. Za TCP i tunel isto radi ispravak zapisa čvora prije F2b. |
| `internal/peers/tailscale.go` | `[sync] tailscale = false` provjerava se po putu, ne po `LocalAddr`, jer QUIC sluša na 0.0.0.0. QUIC put preko Tailscale adrese tada se odbija. Udarci s Tailscale adresa se odbacuju. |
| signal i jezgra | Signalni klijent iz F2 i jezgra iz F3a spajaju se u `Service`. Kad F2b otvori novu UDP utičnicu, sve WEB sync sesije ovog računala se zatvaraju, a nove se pripremaju u pozadini. Natpisi i razlozi su već u F2a, pa F3b ne dira `internal/web`. |

Redoslijed u `SyncWith`:
1. **LAN sync**: pronalaženje (iz LAN1) i spremljene privatne adrese, TCP, usporedno, svaka iduća 250 ms kasnije, s rokom od 2–3 s. Pobjeđuje prva veza koja prođe TLS s očekivanim ključem; ostale se zatvore prije razgovora. LAN ide prvi i kad postoji živa WEB sync sesija, jer program uvijek prvo proba LAN; tako laptop koji dođe kući iduću razmjenu radi LAN syncom.
2. **WEB sync**, kad s tim računalom postoji živa sesija.
3. Inače kreću odmah, zajedno: Tailscale, javne adrese i domene koje je upisao čovjek (TCP, natpis SERVER sync) i tunel (SERVER sync). Vrijedi onaj koji se prvi spoji, a ostali se zatvore prije razgovora. Usporedo s tim, kad treba, u pozadini kreće priprema WEB synca.
4. Uvodi se ukupni rok od 45 s; danas ga nema. Tunel nikad ne čeka WEB sync.

Tako redoslijed LAN → WEB → SERVER vrijedi kad je WEB sync spreman. Dok se priprema, taj krug ide putem koji radi (Odluke 14. 10. 2026.).

Mjesto Tailscalea u redoslijedu je naš prijedlog [P]: iza žive WEB sync sesije, usporedo sa SERVER syncom. Odluke (14. 10. 2026.) o tome ne govore. Ako informatičari jednom zatraže Tailscale ispred WEB synca, to je mala promjena redoslijeda u F3b.

Priprema u pozadini:
- Kreće kad LAN ne odgovori i nema žive sesije, a drugo računalo ima `web/1` i nije u pauzi: provjera mreže, poziv, odziv, udarci, QUIC.
- `SyncWith` je ne čeka i ne drži za nju jedno od četiri mjesta za razmjenu.
- Najviše 4 pripreme odjednom, jedna po računalu.
- Kad priprema uspije, sesija ostaje živa (F3a), a razmjena s tim računalom kreće odmah, ne za 5 min.
- Računalo do kojeg nema puta (ni adrese koja odgovara, ni Tailscalea, ni tunela) dobije samo pripremu, a `SyncWith` vrati „WEB sync se priprema”. To je čest slučaj kad Unraid zove ThinkCentre: Unraid vjerojatno ima samo nedostupnu adresu 192.168.24.70, ili nijednu. Danas `SyncWith` tada stane s greškom „nije poznata nijedna adresa” ili „nije dostupan ni na jednoj adresi”. To se ne broji kao neuspjeh u `due()`.
- Kad je cilj sam Unraid, krug ide tunelom odmah nakon LAN-a, a WEB sync se priprema za idući krug.
- Ishod pripreme ide u brojače iz F1.

Pauza:
- Neuspjesi pripreme broje se po paru, uz stanje obiju strana: javna adresa i vrsta NAT-a ovog računala, i druge strane iz zadnjeg poziva ili odziva.
- Nakon 3 neuspjeha zaredom pauza je 30 min, zatim najviše 1 h.
- Pauza se briše kad se promijeni stanje bilo koje strane, nakon uspjeha i kad F2b otvori novu UDP utičnicu.
- Kandidat na kojem se javio krivi ključ nije neuspjeh probijanja.
- Kad STUN radi, a pokušaji ne uspijevaju, razlog na pločici je „WEB sync iz ove mreže ne uspijeva”. Kad udarci druge strane ne stižu nijednom, a ova strana šalje, razlog je da vatrozid na računalu možda ne pušta WEB sync.
- Dnevnik: `povezivost: … pozvao=ovaj` ili `pozvao=drugi`.

**Testovi:**
- **`natsim`, tablica** (prepisana iz §12; posrednik zamijenjen SERVER syncom; slaže se s irohovim i pionovim testovima):

  | A \ B | EIM, neovisno filtriranje | EIM, filtriranje po adresi i portu | simetrično | UDP blokiran |
  |---|---|---|---|---|
  | EIM, neovisno | WEB sync (1. pokušaj) | WEB sync | WEB sync (adresa viđena tek kod druge strane) | SERVER sync |
  | EIM, po adresi i portu | WEB sync | WEB sync (istodobno slanje) | SERVER sync | SERVER sync |
  | simetrično | WEB sync (adresa viđena tek kod druge strane) | SERVER sync | SERVER sync (1 pokušaj) | SERVER sync |

  Za par bez tunela „SERVER sync” znači da podaci idu preko poslužitelja u dva kruga, do 10 min kasnije.
- **`natsim`, uz tablicu:**
  - 20 % gubitka → uspjeh unutar 3 pokušaja;
  - NAT nepoznat kod jedne strane → 3 pokušaja; u 2. pokušaju B kreće 1 s kasnije;
  - dvostruki NAT kod kuće, s putem koji radi samo u jednom smjeru;
  - ured s dvije javne adrese;
  - Docker (MASQUERADE, objavljen port, conntrack 30 s), i istodobno udaranje kroz conntrack u mostu;
  - dva ili tri računala s istim portom iza istog kućnog NAT-a;
  - dva računala na istoj privatnoj adresi 192.168.1.23:4713, u dvije kuće;
  - vatrozid na računalu (ufw) koji baca pakete s adrese viđene tek kod druge strane;
  - hotspot;
  - promjena mreže usred sesije; spavanje i buđenje (lažni sat);
  - laptop dođe kući usred WEB sync sesije: iduća razmjena ide LAN syncom;
  - istek preslikavanja nakon 30 s uz održavanje veze svakih 15 s;
  - dva računala iza istog NAT-a bez hairpina → lokalni kandidat; lokalni kandidat pobijedi javni kad stigne unutar 500 ms;
  - ruta podmreže 192.168.1.0/24 preko Tailscalea, i uključen izlazni čvor;
  - UDP blokiran i Tailscale adresa nedostupna → razmjena završi kroz tunel, koji kreće odmah nakon LAN-a;
  - cilj je sam Unraid → krug ide tunelom odmah nakon LAN-a, a idući WEB syncom.
- **Razmjena ne čeka:**
  - kad LAN ne odgovori, razmjena ide Tailscaleom ili tunelom, a priprema WEB synca ne drži nijedno od četiri mjesta;
  - kad priprema uspije, razmjena s tim računalom kreće odmah;
  - „WEB sync se priprema” ne povećava broj neuspjeha u `due()`;
  - pauza se briše kad se promijeni stanje druge strane; najdulja je 1 h; krivi ključ na kandidatu nije neuspjeh;
  - sesija preživi dva kruga od 5 min;
  - LAN pobijedi živu WEB sync sesiju;
  - od tri adrese koje sve odgovore razgovor ide samo na jednoj;
  - javna adresa koju je upisao čovjek zove se zajedno s Tailscaleom i tunelom, a pločica kaže SERVER sync;
  - `SyncWith` završi unutar 45 s i kad nijedna adresa ne odgovara.
- **Sigurnost:**
  - stranac dobiva Retry, a zatim odbijanje;
  - nečlan s ispravnim TLS-om prođe `Dial`, ali nikad nije primljen;
  - jednosmjerni tok se odbija;
  - član opozvan usred sesije dobiva 0x0104 čim se opoziv primijeni, i kad je opozvana ovlast onoga tko ga je primio;
  - član C s ispravnim ključem ne može ući u sesiju A i B;
  - vlastiti udarac vraćen pošiljatelju se odbija;
  - vlastita adresa među kandidatima druge strane ne dobiva udarac;
  - odgovor s adrese na koju nije poslan udarac se ne prima;
  - na valjan udarac s adrese koja nije potpisani kandidat odgovor ide samo na tu adresu i samo jednom po udarcu; bez valjanog udarca s te adrese ništa se na nju ne šalje;
  - QUIC veza čiji `pozdrav` nosi sesiju starijeg pokušaja se zatvara;
  - računalo koje drži adresu kandidata, ali ne šalje udarac, odbija se nakon Retryja;
  - ponovljeni udarci se odbacuju; ponovljeni sinkro 1. pokušaja nakon 2. se odbacuje;
  - podmetnut ključ u zapisu čvora ne mijenja `expect` za QUIC;
  - 33 paketa naglo;
  - broj gorutina nakon zatvaranja QUIC sesija.
- **Isti razgovor na svakom putu** (TCP, tunel, QUIC 0x01):
  - poruke su iste;
  - nečlan je odbijen;
  - mjesta su zajednička, pa peti dobiva „zauzet”;
  - `noteSync` ne pamti adresu;
  - Tailscale adrese idu samo preko Tailscalea.
- **Unraid zove ThinkCentre**, u dva slučaja: Unraid nema nijednu adresu ThinkCentrea, i Unraid ima samo nedostupnu 192.168.24.70. U oba Unraid pripremi WEB sync u pozadini i odmah zatim razmijeni s ThinkCentreom.
- **Prije PR-a:** `-count=20` s više sjemena.

**Vidi se:**
- na Unraidu uz ThinkCentre piše „zadnja razmjena: WEB sync, 15 ms”, a brojač kaže da je zvao Unraid;
- laptop na hotspotu i ThinkCentre razmjenjuju izravno;
- u dnevniku: `povezivost: WEB sync s cop-osijek-ured uspio u 1. pokušaju (318 ms), pozvao=ovaj`;
- kad WEB sync ne uspije, pločica kaže zašto. Uz Unraid razmjena ide SERVER syncom; na laptopu uz ThinkCentre piše „SERVER sync, preko cop-osijek.com: podaci stižu do 10 min kasnije.”

**Prekidač.** `web_sync = false` je zadano. Na testnim računalima uključuje se ručno, redom iz odjeljka 6.

**Kompatibilnost.** QUIC ide samo među novim računalima. Portovi 4710, 4711 i 4712 i `/razmjena/tunel` ostaju, a staro računalo ne vidi ništa novo.

### F4 Posrednik: odgođeno (točka 3)

Odluke (2. 10. 2026.) predvidjele su rezervu: posrednika preko WebSocketa na Unraidu, a mjerenje u uredu trebalo je reći treba li. Mjerenje 12. 10. pokazalo je lak NAT, pa je posrednik odgođen (točka 3). Bez WEB synca par razmjenjuje SERVER syncom, kao danas, ili Tailscaleom, kad ga imaju oba računala. Svi izmjereni NAT-ovi su laki, a Cloudflareovi uvjeti zabranjuju VPN i slične posredničke usluge (§2.2.1(j)). Razmjena ionako ne čeka probijanje (F3b). SERVER sync ostaje današnji tunel do Unraida, koji je puni član mreže. Ako neki par jednom ne uspije probiti, posrednik dobiva zaseban plan, po mogućnosti izvan Cloudflarea (copB ili VPS). G4 je do tada već gotov, pa se stara očitanja ne bi plaćala svaki krug.

Taj plan treba uzeti ova pravila iz uzora:
- sastajalište objema nogama javi da je spoj gotov tek kad su obje tu. Noga koja prije toga pošalje išta osim žetona odmah se prekida, a pad jedne noge zatvara drugu (Magic Wormhole);
- kad padne signalna veza računala, zatvaraju se sve njegove noge i poništavaju njegovi neiskorišteni žetoni (Syncthing);
- žeton unaprijed javlja granice sesije: MiB, minute i ostatak dnevnog proračuna (libp2p). Svaka granica ima test koji je prekorači; Magic Wormhole granice zapisuje, ali ih ne provodi;
- posrednik nosi TLS tok, pa sesija s njega ne prelazi na UDP. To je svjesno odstupanje od iroha, gdje su posrednik i izravni put dva puta iste QUIC veze. Za prelazak bez prekida trebali bi QUIC paketi u okvirima WebSocketa (§9) i proba gubi li quic-go s vlastitim `net.PacketConn` GSO i ECN.

### F5 Teren i dokumentacija (1,5–2 dana, okvirno 25.–30. 11.; ostatak nakon terena)

- Rokove ugoditi prema zapisima s terena: odgoda signala, pauza, trajanje sesije, ritam udaraca.
- govulncheck s quic-go-om. Ozbiljna greška u jezgri quic-go-a znači hitno izdanje.
- Dokumentacija:
  - `plan-povezivost.md` (§3 i korak 8);
  - `plan-testiranja-mreze.md`;
  - `INSTALACIJA.md` i `linux.md`;
  - dio o WEB syncu u `protokol-razmjene.md`, s popisom izvora iz uzora (projekt, datoteka, redak), koji je sad u lokalnoj datoteci `uzori-sync.md`;
  - skill razmjena-cvorova, s tri nova pravila: adresa s puta koji nije LAN ne ide u knjigu, javna adresa se nikad ne nauči sama, a razmjena nikad ne čeka probijanje.
- Jedna stranica za IT Hrvatskih voda, za predstavljanje bete. Opisuje 4713/udp, STUN poslužitelje, TLS 1.3 s ključevima računala, to da se spajaju samo članovi, što vidi sastajalište, i to da računala u uredu primaju QUIC veze započete izvana probijanjem, ali samo od članova mreže. Piše i da goCOP ne mijenja pravila vatrozida ni druge sigurnosne postavke računala. Opisuje i Tailscale kao mogućnost: ako IT postavi Tailscale na neka računala, sa svojim računom, goCOP razmjenjuje i njime, s natpisom „Tailscale”, a Tailscale adrese ne ulaze u knjigu.
- `web_sync_prijava = true` i `web_sync = true` postaju zadani tek zasebnom odlukom, nakon dva tjedna bez grešaka na svim testnim računalima i predstavljanja bete IT-u (točka 6).

### Što ne radimo sada

- Posrednika (F4; točka 3).
- Roj (F7): `sadrzaj/1`, dijelove, Bloom, `posluzujeSadrzaj`. Ostaju samo rezervirani kanali 0x02 i 0x03 i `pozdrav.kanali`.
- Više sastajališta i prioritete. Poruke već nose ključ sastajališta, pa drugo sastajalište može doći kasnije bez promjene protokola.
- QUIC u LAN-u i `quic_port` u beaconu: odgođeno. LAN sync ostaje TCP 4710, a QUIC na lokalni kandidat ionako se prikazuje kao LAN sync.
- DTLS, UPnP/NAT-PMP, IPv6 i predviđanje porta.
- Kandidat „javna adresa : 4713” kad NAT nije lak (Tailscale i iroh ga imaju). Svi izmjereni NAT-ovi su laki, a kandidat se može dodati kasnije bez promjene protokola.
- Obavijesti sustava o promjeni mreže (macOS AF_ROUTE, Linux netlink, Windows). Umjesto njih adrese sučelja gledaju se svakih 10 s, uz razmak sata i grešku slanja. Kod samo za jedan sustav u kritičnom paketu ne bi imao pokrivenost u CI-ju na Linuxu.
- Nova polja u zapisu čvora.
- Polje u članstvu o tome kako je računalo ušlo u mrežu. Ne treba: primanje na daljinu je isključeno, a uparivanje u LAN-u i preko Tailscalea ide istim kodom (točka 4).
- Sesije dulje od 6 min i ograničenje broja susjeda. To treba tek za 34 ispostave.
- Tajne udaraca koje sastajalište ne zna (po Magic Wormholeu: tajne šifrirane ključem druge strane). To treba prije nego copB ili VPS postane sastajalište; Unraid je član mreže.
- PAKE za primanje na daljinu (po Magic Wormholeu). To bi značilo razgovor preko sastajališta s računalom koje još nije član, pa traži zasebnu odluku, uz odluku o tome kako ulaze ispostave.
- copB ili VPS kao drugo sastajalište (§10.10): nije na vidiku.
- Isključen glasnik na računalu s javnom adresom: tek kad dođe copB ili VPS.

### Zbroj

| dio | dani |
|---|---|
| G1–G6, pravila na granici | 5,5–7,5 |
| G7, zapis odbijenih i djelomično podržanih poruka | 1 |
| F0 (s naučenim adresama samo na računalu i vezivanjem baze uz protokol) | 3–3,5 |
| proba QUIC-a u synctestu | 0,5–1 |
| LAN1, pronalaženje i glasnik | 0,5–1 |
| F1 (s prekidačem pri padu) | 5–5,5 |
| F2 (F2a i F2b) | 4,5–5,5 |
| dva mala PR-a prije F2b: primanje na daljinu (0,5) i ispravak zapisa čvora (0,5–1) | 1–1,5 |
| F3 (F3a jezgra 4,5–5,5 i F3b redoslijed 3–3,5) | 7,5–9 |
| F5 | 1,5–2 |
| alat za fazu M | 1 |
| izdanja, 4 × 0,5 (provjera registara, Unraid prvi, zatim L, T i W) | 2 |
| **ukupno** | **33–40,5** |
| od toga sam WEB sync (F0, proba, F1–F3, F5) | 22–26,5 |

G0 traje sat-dva i ne broji se.

Stari plan imao je za F0–F5 16–21,5 dana, s posrednikom (2–3 dana) i QUIC-om u LAN-u. Novi ih nema, a ipak je veći. Dodani su simulator NAT-a, proba prije F1, zaštite iz pregleda (dva ključa udarca, prekidač pri padu, sastajalište samo iz lokalne postavke, baza vezana uz protokol), brojači po putu, provjera stvarnog puta i buđenje laptopa. Prva inačica ovog plana imala je 29,5–36 dana. Uzori dodaju 2,5–3 dana: razmjena koja ne čeka probijanje, pauza po paru i ritam pokušaja (F3), promjena mreže i buđenje (F2), histereza i greška po adresi (F1) i novi korak LAN1. Prekidač pri padu prešao je iz F3a u F1, pa se zbroj nije promijenio. Odluke (14. 10. 2026.) dodaju 0,5 dana za PR koji isključuje primanje na daljinu; pola dana za točku 2 bilo je u F0 već u prvoj inačici. Ispravak zapisa čvora dodaje 0,5–1 dan.

Od 15. 10. do 30. 11. ima 32 radna dana; 18. 11. je blagdan. Uz mrežni posao idu koraci 1–5 faze 3, oko 10 dana. To stane samo uz četiri usporedna radna stabla: granica, WEB sync, jezgra F3 i faza 3. Svaka faza kreće čim je spojena faza o kojoj ovisi, pa sve može završiti i ranije. Kad nešto kasni, prvo se odgađaju F5 dokumentacija i G7, zatim uključivanje Windowsa. Ako jezgra F3 ne stigne usporedo, F3b, izdanje 3 i teren idu u prosinac.

---

## 5. Okvirni redoslijed

Datumi u ovom odjeljku su okvirni redoslijed, ne rokovi (Odluke 14. 10. 2026.). Faza kreće čim je spojena faza o kojoj ovisi, pa sve može biti gotovo i puno prije. Kraj studenog je okvirna gornja granica; što ne stane, ide u prosinac. Bez obzira na datume vrijede ovisnosti:
- PR upozorenja o malo mjesta na disku prije G4 i F1;
- G0 prije G1; G1 i G3 prije F0; G2 prije F2; G7 nakon F0;
- proba QUIC-a nakon F0, a prije F1 i jezgre F3;
- LAN1 prije kandidata u F1;
- F2a nakon spoja F1, a spojen prije nego korak 5 faze 3 počne mijenjati `internal/web`;
- oba mala PR-a (primanje na daljinu, ispravak zapisa čvora) prije F2b;
- razdvajanje `applyOne` (faza 3) i G3 jedan za drugim, jer rade na istom putu primjene;
- F3b nakon spoja F2b i F3a;
- za svako izdanje Unraid se nadograđuje prvi.

**Pravilo bez preklapanja:**
- **Granica, LAN1 i WEB sync diraju:** `internal/peers`, `internal/razmjena`, `internal/ledger`, `internal/config`, `internal/db/schema.go`, `cmd/gocop` (`main.go`, `arhiva_razmjena.go`) i `Dockerfile`. U `internal/web` diraju `server.go` (nova funkcija za rute povezivosti), `rokovi.go`, novi `ograda_signala.go`, `razmjena_plocica.go`, `handlers_sync.go`, `postavke_cvora.go` i predloške pločice, Sinkronizacije i Administracije. Uz to i `handlers_postavljanje.go`, za PR koji isključuje primanje na daljinu. U `internal/repository` diraju samo `apply_mreza.go` (ispravak zapisa čvora) i novu `apply_nadogradnja.go` (G8, prosinac).
- **Jezgra F3 dira samo nove datoteke:** `sesija.go`, `udarac.go`, `probijanje.go`, `quic_slusalica.go` i `kandidati_druge.go` u `internal/razmjena`, te `internal/natsim`. `quic.go` i `kandidati.go` iz F1 ne dira, nego koristi njihova sučelja. Kreće od grane s probom i nakon spoja F1 presloži se na glavnu granu. Nova utičnica nakon promjene mreže je u zasebnoj datoteci (`nova_uticnica.go`, F2b), da se jezgra i F2b ne sudare.
- **LAN1 ide prije kandidata iz F1**, jer donosi `sucelja.go`. F1 prvih dana radi utičnicu, STUN i provjeru mreže, a kandidate nakon spoja LAN1.
- **Faza 3 dira:** `internal/repository/apply.go` (`applyOne` i `removeFromSurface`, dani 6–8), `internal/service`, `internal/importer/ugovor`, `internal/prognoza` i `internal/models`, a korak 5 i `internal/web`. Razdvajanje `applyOne` i G3 rade na istom putu primjene, ali u različitim datotekama: `applyOne` u `internal/repository`, G3 u `internal/ledger` i `internal/peers`. Spajaju se jedan za drugim, a drugi se presloži na glavnu granu i ponovno prođe testove primjene.
- **Zajednički su `internal/web` i `internal/db/schema.go`.** Dio WEB synca u `internal/web` (F1 i F2a) spaja se prije nego korak 5 počne. Dok korak 5 traje, WEB sync ne dira `internal/web`; svi natpisi i razlozi za F3 već su u F2a. Ako F3 ipak zatreba promjenu pločice, čeka kraj koraka 5. Za `schema.go` vrijedi pravilo o migracijama niže.
- **Pojednostavljenja i errcheck:** lagana i srednja pojednostavljenja i nalazi errchecka u `internal/peers`, `internal/razmjena` i `internal/ledger` čekaju izdanje nakon F3. Nalazi errchecka u `cmd/gocop`, `internal/db` i `internal/config` čekaju izdanje 1, a u datotekama `internal/web` s gornjeg popisa spoj F2a.
- **Migracije u `internal/db/schema.go`:** spajaju se jedna po jedna, za sve grane, i mrežne i faze 3. Grana koja kasni presloži svoju migraciju na novu glavnu granu prije spajanja. Očekivani red mrežnih: G2, F0, G7, F1; G4 je bez migracije. LAN1 i F3b nemaju migraciju; oznaka prekidača pri padu je datoteka uz bazu, ne migracija.

| okvirno | granica i WEB sync | jezgra F3 (zasebno stablo) | faza 3 (drugi paketi) | teren |
|---|---|---|---|---|
| 15.–16. 10. | Prvo PR upozorenja o malo mjesta na disku: dira istu pločicu i donosi testove za `stanjeRazmjene`. PR dokumentacije s ovim planom. G0, zatim G1 od 16. | — | dani 4–5: testovi za kritične funkcije, po paketima | alat za fazu M; M: ThinkCentre, uz probu pronalaženja kroz ufw |
| 19.–23. 10. | G1 16.–19.; G3 20.; G2 20.–21.; G4 22.–23.; F0 21.–23. (zasebno stablo, nakon G1 i G3) | — | dani 6–8: testovi i razdvajanje `applyOne` i `removeFromSurface` (`internal/repository`), jedan za drugim s G3; dani 9–10 su koraci G iz lijevog stupca | M: Unraid (probni spremnici), Windows, laptop, parovi; ishod faze M kaže ostaje li Unraid u mostu |
| 26.–30. 10. | proba QUIC-a 26.; G5 26.; G6 27.; LAN1 27.–28.; **izdanje 0** 28. (G1–G6, F0); G7 29.–30.; F1 od 27. (kandidati nakon spoja LAN1) | od 27., od grane s probom: `natsim`, udarci, stroj probijanja | dani 11–15: koraci 1–3 (`internal/service`, `internal/importer/ugovor`, `internal/prognoza`); errcheck izvan mrežnih paketa i izvan datoteka s popisa | Windows ostaje na 0.0.38 do izdanja 1 |
| 2.–6. 11. | F1 27. 10.–3. 11.; **izdanje 1** 4. (F1, G7, LAN1); F2a 3.–5., nakon spoja F1; prije F2b dva mala PR-a (primanje na daljinu, zapis čvora); F2b od 5. | QUIC slušalica, sesija i kanali, kandidati druge strane; preslaganje na glavnu granu nakon spoja F1 | dani 16–20: korak 4 (`internal/prognoza`, osvježavanje); lagana pojednostavljenja izvan mrežnih paketa; korak 5, prvi komadi, a u `internal/web` tek nakon spoja F2a | „Provjeri vezu” na svim testnim računalima; laptop: Wi-Fi → hotspot bez ponovnog pokretanja; 24 h dnevnik bez tuđih adresa |
| 9.–13. 11. | F2b 5.–10.; **izdanje 2** 11.; F3b od 11. (samo `internal/peers`) | spaja se oko 9.; `-count=20`; ispravci | rezerva faze 3: korak 5, nastavak (`internal/web`, `internal/service`, `internal/models`); srednja pojednostavljenja izvan mrežnih paketa; novo mjerenje kvalitete | signalizacija 24–72 h od 11.; laptop: spavanje i buđenje |
| 16.–20. 11. (18. 11. je blagdan) | F3b 11.–17.; **izdanje 3** 19. | — | poslije mjeseca faze 3 (plan faze 3) | 20.: korak 1 uključivanja (Unraid i laptop na hotspotu) |
| 23.–27. 11. | ispravci s terena; F5 od 25. | — | — | korak 2 od 23.; korak 3 od 25.; koraci 4 i 5 od 27. |
| 30. 11. | F5: dokumentacija, stranica za IT | — | — | zapisi s terena |
| prosinac | dva tjedna bez grešaka; G8; zadano uključivanje tek zasebnom odlukom (točka 6) | — | — | — |

Izdanja nemaju unaprijed dodijeljene brojeve. Svako izdanje nosi sve što je do tada spojeno, pa i korake faze 3. Za svako izdanje Unraid se nadograđuje prvi, a zatim ostala računala. Od 14. 11. radi se u manjem opsegu, pa su za mrežni dio tada predviđeni samo F3b, izdanje 3, ispravci i dokumentacija. Kad posao ide brže, sve se pomiče naprijed.

---

## 6. Proba na terenu

| oznaka | računalo | mreža |
|---|---|---|
| U | Unraid kod kuće, Docker | vanjski kućni LAN 192.168.1.x; javna adresa nije iza CGNAT-a |
| W | Windows kod kuće | 192.168.1.x |
| L | laptop, macOS | kuća (iza drugog usmjerivača, 192.168.0.x), ured (kabel 192.168.24.x ili Wi-Fi 192.168.10.x), hotspot |
| T | ThinkCentre, Ubuntu, ured | 192.168.24.70, iza vatrozida HV-a |
| H | po želji drugi Linux kod kuće | — |

Na domenska računala HV-a ne instalira se ništa.

**Faza M (sada, bez promjene programa).** Za nju se dopunjuje alat `tools/diagnostics/mreza-ured`, koji je izvan gita (1 dan). Dopune:
- `-port 4713`;
- kandidati kroz zapisana pravila filtra: samo RFC 1918, bez sučelja s popisa iz LAN1 (`docker*`, `br-*`, `veth*`, `virbr*`, `utun*`, `tailscale*`, `vEthernet*`, `bridge*`). Kasnije se `kandidati.go` provjerava prema istoj tablici;
- bez stun1;
- mjerenje koliko dugo NAT drži preslikavanje: probe nakon 10, 30 i 60 s, zatim do 5 min;
- način rada za par: oba računala kreću u dogovorenu sekundu, s dijeljenom tajnom;
- proba filtriranja s tri strane;
- proba pronalaženja s prolaznog porta prema 4712, da se vidi pušta li ufw odgovor;
- ima li mreža IPv6 (samo podatak; v1 radi s IPv4);
- jedan JSON redak po rezultatu;
- izgradnja za linux/amd64 i windows/amd64, uz `mjeri-linux.sh` i `mjeri.ps1`.

Redoslijed mjerenja:
1. **T, prvo.** U uredu je dosad mjeren samo laptop. Uz to provjera sata (`timedatectl`), jer potpisani pozivi trpe najviše ±10 min, i proba pronalaženja između L na kabelu i T: stiže li odgovor kroz ufw. O tome ovisi uputa u LAN1.
2. **U, u probnim spremnicima:** jedan u mostu s `-p 4713:4713/udp`, jedan s `--network host` i jedan s `--network container:gocop` (samo gleda mrežu postojećeg spremnika). Mjeri se:
   - koji su portovi objavljeni;
   - STUN s porta 4713 prema Googleu i Cloudflareu;
   - vidi li spremnik pravu adresu pošiljatelja;
   - koliko dugo NAT drži put kad se ništa ne šalje (od 10 s do 5 min);
   - oporavak nakon ponovnog pokretanja probnog spremnika;
   - hairpin;
   - odgovara li spremnik na 4712 (samo podatak, vidi ishod niže);
   - jednom dok alat radi samo na U, a jednom dok istodobno radi i na W i L s istim portom 4713. Kod kuće sva tri izlaze kroz isti vanjski NAT, a port 4713 zadrži samo prvi.

   Na U se još pogleda koje adrese gocop ima za T (stranica Sinkronizacija). Spremnici gocop i BP16 se ne diraju.
3. **W:** proxy, profil vatrozida i pravila za gocop, samo čitanjem.
4. **L:** kuća na Wi-Fi-ju; kuća kabelom na vanjski LAN, ako se može; hotspot (i ima li IPv6); ured na kabelu; ured na Wi-Fi-ju.
5. **Parovi, svaki po 5 puta:**
   - L na hotspotu ↔ U u mostu;
   - L na hotspotu ↔ U na mreži računala;
   - L na hotspotu ↔ T;
   - T ↔ U;
   - L kod kuće ↔ U, preko lokalnog kandidata;
   - W ↔ L na hotspotu.

**Ishod faze M** odlučuje o Unraidu. Most ostaje ako su ispunjena tri uvjeta:
- prema Googleu i Cloudflareu vidi se isti preslikani port (EIM); je li to baš 4713, samo je podatak;
- spremnik vidi pravu adresu pošiljatelja;
- nakon ponovnog pokretanja veza se vraća za manje od 30 s.

Inače prelazak na mrežu računala dobiva zaseban mali plan, jer mijenja web port i cilj tunela. Pronalaženje na 4712 nije uvjet. Syncthing u Dockeru traži mrežu računala, ali W i L do U-a dolaze spremljenom adresom ili lokalnim kandidatom, pa im pronalaženje ne treba.

**Nakon izdanja 0 (granica i F0):** W ostaje na 0.0.38 do izdanja 1, da se mješovita mreža isproba na pravim računalima.

**Nakon izdanja 1 (F1, G7, LAN1):**
- „Provjeri vezu” na svim testnim računalima uspoređuje se s alatom; na U s probnim spremnikom u mostu. Dnevnik se 24 h prati da u njemu nema tuđih adresa.
- Na L: prelazak s Wi-Fi-ja na hotspot i natrag, bez ponovnog pokretanja programa. Gleda se odgovara li glasnik na 4712 i radi li STUN s utičnice 4713. Po Nebuli, Darwin veže UDP utičnicu uz sučelje. Ako se to potvrdi, F2b to rješava novom utičnicom; ako se ne potvrdi, nova utičnica ostaje, jer ne smeta.
- L u uredu na kabelu nađe T pronalaženjem.

**Nakon izdanja 2 (F2):**
- Na U se postavljaju `web_sync_prijava = true` i `posluzi_sastajaliste = true`, a na L, T i W `web_sync_prijava = true`. `web_sync` ostaje `false` svugdje.
- Signalizacija se prati 24–72 h, uz broj prekida.
- Provjerava se ponovno pokretanje spremnika gocop, promjena mreže na L i prijava T iz mreže HV-a.
- L: zatvoren poklopac kraće od 10 min ne otvara signalnu vezu iznova; dulje od 10 min otvara, i L je opet prijavljen za manje od minute.

**Nakon izdanja 3 (F3): uključivanje WEB synca, redom.** Uključuje se samo `web_sync = true`, računalo po računalo.

| korak | računala | smije kad |
|---|---|---|
| 1 | U i L na hotspotu | izdanje 3 je na Unraidu, a mjerenje Dockera u fazi M je prošlo |
| 2 | L kod kuće ↔ U | korak 1 dva dana bez greške |
| 3 | T: T ↔ U, zatim L izvan ureda ↔ T | korak 2 dva dana bez greške |
| 4 | W | korak 3 bez greške i vatrozid provjeren |
| 5 | L ↔ T s `[sync] tailscale = true`, jedan dan | korak 3 bez greške |

**Greška** je neuspjela priprema WEB synca dok su oba računala bila uključena i imala `web_sync = true`. Čita se iz brojača neuspjelih priprema u `peer_sync`, ne iz natpisa zadnje razmjene.

Prije uključivanja na svakom računalu:
- „Provjeri vezu” kaže da UDP radi i da je NAT lak;
- signalizacija je 24 h radila bez neočekivanih prekida;
- na testnim parovima je `[sync] tailscale = false`, da Tailscale ne sakrije grešku. Tada L ↔ T, kad WEB sync ne uspije, ide preko Unraida. Iznimka je korak 5.

**Mjeri se**, po 10 pokušaja za svaki par, gumbom „Provjeri vezu s računalom” (svaki pokušaj je nova veza): put, broj pokušaja, ms do veze, ms do prve razmjene, RTT, tko je zvao, i MiB mobilnih podataka na sat iz brojača bajtova. Iz brojača se čita i koliko je krugova otišlo starim putem dok se WEB sync pripremao.

**Mora vrijediti:**
- L kod kuće ↔ U nikad ne ide SERVER syncom, nego LAN syncom: TCP na 192.168.1.2:4710 ili QUIC na lokalni kandidat. Lokalni kandidat Unraida stiže laptopu u potpisanom pozivu, pa ne mora biti u knjizi;
- L u uredu na kabelu ↔ T ide LAN syncom. Na Wi-Fi-ju ide LAN syncom na spremljenu adresu ili preko lokalnog kandidata, nikad SERVER syncom;
- kad je U ugašen, L i T u uredu razmjenjuju LAN syncom, ako L zna adresu T-a (kabel ili spremljena adresa);
- L koji dođe kući usred WEB sync sesije iduću razmjenu radi LAN syncom;
- nijedna razmjena ne čeka probijanje: ms do prve razmjene nakon promjene mreže nisu veći nego danas;
- nakon prelaska L s Wi-Fi-ja na hotspot WEB sync se vrati unutar 1 min, bez ponovnog pokretanja programa;
- promjena mreže usred sesije daje novu sesiju, bez dvostrukih zapisa;
- razmjena preživi ponovno pokretanje U usred sesije te spavanje i buđenje L;
- U sam započne razmjenu s T (brojač „zvao: ovaj” na U);
- u koraku 5 (L ↔ T s `[sync] tailscale = true`) razmjena ide Tailscaleom kad WEB sync nije spreman, a pločica kaže „Tailscale”. Taj dan ulazi u dva tjedna bez grešaka.

Rezultati idu u tablicu u `plan-testiranja-mreze.md`, sa stupcima: datum, par, mjesto, NAT obiju strana, put, pokušaja, ms do veze, ms do razmjene, RTT, tko je zvao, napomena.

**Ručni koraci za korisnika:**

| kad | korak |
|---|---|
| M | probni spremnici na Unraidu (`--network host`, `--network container:gocop`) iz root ljuske, i njihovo uklanjanje nakon mjerenja |
| M | pokretanje alata za mjerenje na T i W; provjera sata na T (`timedatectl`) |
| LAN1 | pravilo u ufw-u na T za odgovore na pronalaženje, ako mjerenje u fazi M pokaže da treba |
| F1 | 4713/udp u predlošku za Unraid; provjera da je `--restart unless-stopped` u Extra Parameters. Ponovno stvaranje spremnika ponovno pokreće računalo koje izdaje prognoze, pa treba dogovoreno vrijeme |
| F1 | `[povezivost] lan_adresa = "192.168.1.2"` u `gocop.toml` na U; bez toga spremnik nudi adrese 172.17.x |
| F1 | `QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING=1` u systemd jedinici na T |
| F1 | dopuštenje „Lokalna mreža” za gocop na macOS-u (L) |
| F1 | `ufw allow 4713/udp` na T, ako mjerenje pokaže da treba; provjera pravila vatrozida na W |
| F2 | pravilo brzine u Cloudflareu samo za `/razmjena/signal`, s pragom oko 30 zahtjeva u minuti s iste adrese; provjera da ulaz cloudflared nema pravilo puta za `/razmjena/signal` |
| F2 | `web_sync_prijava = true` i `posluzi_sastajaliste = true` na U; `web_sync_prijava = true` na L, T i W |
| F3 | `web_sync = true` redom iz tablice; `[sync] tailscale = false` na testnim parovima, a u koraku 5 `true` na L i T |

---

## 7. Rizici i mjere

| rizik | vjerojatnost | što se dogodi | mjera | kad |
|---|---|---|---|---|
| Greška u quic-go-u sruši cijeli program, ne samo jednu vezu. U jezgri je bilo otprilike jedno ozbiljno sigurnosno upozorenje godišnje. quic-go nema `recover()`. | mala do srednja | Unraid ne izdaje prognoze; ponovno pokretanje vrati istu grešku, pa program pada u krug; Postava vrati stari program ako padne u prvih 90 s | Do TLS-a dolazi samo adresa s ispravnim udarcem druge strane. Bez `http3`. Prekidač pri padu od F1, prije nego prijenos proradi na testnim računalima: nakon dva pada WEB sync ostaje isključen dok ga čovjek ne uključi. govulncheck u CI-ju i hitno izdanje. Zadano isključeno. Docker (`--restart unless-stopped`) i systemd ponovno pokreću program. | F1, F3a, F5 |
| Kopirana mapa podataka (simulacija ili pogreška) radi kao pravo računalo. | srednja kad krene simulacija | testni podaci uđu u pravu mrežu; dvije kopije izbacuju jedna drugu kod sastajališta | baza pamti ime protokola; provjera CN-a; drugi proces s istim ključem dobije puls staroj vezi i, ako stara odgovara, odbijen je uz upozorenje na oba računala i čeka 10 min; simulacija dobiva novu mrežu, nikad kopiju | F0, F2 |
| Član mreže postavi sebe za sastajalište kroz knjigu. | mala | vidi tko s kim razmjenjuje, javne adrese i tajne udaraca | sastajalište samo iz lokalne postavke, s ključem unaprijed poznatim; poziv za drugo sastajalište se odbija | F2b |
| Član mreže podmetne drugom računalu tuđi ključ u zapisu čvora (`primijeniCvor`). | mala | razmjena ide s krivim članom, a WEB sync bi pozvao krivo računalo | ključ cilja i `expect` za WEB sync iz `memberships`; za TCP i tunel isto, zasebnim malim PR-om prije F2b | prije F2b, F2b, F3b |
| Naš poslužitelj prekida WebSocket nakon 60 s (`rokovi.go`). | velika ako se zaboravi | izgleda kao prekid Cloudflarea, pa uzrok tražimo na krivom mjestu | `netaknuto` i test za sve WebSocket rute | F2a |
| Javna adresa ode u knjigu. | velika bez ispravka | kućne adrese ljudi stignu na sva računala | ista vrata u `noteSync` i `Discover`: samo privatne adrese, ne preko Tailscalea; adresa iz pronalaženja tek nakon TLS-a; naučene adrese samo u `peer_sync` ovog računala, a kandidati druge strane samo u memoriji; bez tuđih adresa u dnevniku | F0, F3b |
| Pločica pokaže adrese svakom prijavljenom korisniku. | velika bez ispravka | adrese kolega i raspored mreže vidi i vodočuvar | razlog je kod s gotovom rečenicom; adrese, greška po adresi i sirovi tekst greške samo na Sinkronizaciji; test bez adresa u HTML-u | F1 |
| Udarac se odbije natrag pošiljatelju (javna Wi-Fi mreža), ili računalo primi vlastiti udarac kad dvije kuće imaju istu podmrežu. | srednja | napadač otvori vrata za svoju adresu i dođe do TLS-a; ili probijanje krene prema samom sebi | dva ključa, po jedan za svaki smjer; vlastite adrese se ne udaraju; vrata za QUIC otvara samo valjan udarac druge strane; odgovor na adresu koja nije potpisani kandidat ide samo nakon valjanog udarca s nje; test s istom privatnom adresom u dvije kuće | F3a |
| Tailscale sakrije WEB sync na probi, ili promet potiho ide kroz Tailscale (izlazni čvor, rute podmreža). | velika | proba ništa ne dokaže, a promet ide Tailscaleom, iako pločica kaže drugi put | `tailscale = false` na testnim parovima; provjera stvarnog puta za TCP (lokalna adresa veze), vlastite kandidate, kandidate druge strane i STUN; QUIC preko Tailscale adrese vodi se kao `tailscale` | F0, F1, F3a |
| Cloudflare zatvara tihu WebSocket vezu (oko 100 s, nije službeno) i zatvara veze kad nadograđuje svoje poslužitelje. | sigurno | kratki prekidi signalizacije | puls svakih 25 s; ponovno spajanje je obično stanje, s odgodom najviše 60 s; isti proces zamijeni svoju staru vezu; brojač prekida; prolazni prekid ne dolazi na pločicu prije 10 s; 24–72 h mjerenja | F2 |
| Pravilo brzine u Cloudflareu blokira kućnu ili uredsku adresu. | mala | računala iza te adrese ne mogu se prijaviti kod sastajališta; tunel i web rade | prag oko 30 zahtjeva u minuti; odgoda 2 s · 2ⁿ do 60 s, ±20 %; kratko buđenje ne otvara vezu iznova | F2 |
| Cloudflareovi uvjeti zabranjuju VPN i slične posrednike. | mala uz mali promet | gubi se sav pristup izvana, i web i tunel | posrednik odgođen (točka 3); signalom idu samo kratke poruke | F2 |
| NAT Dockera na Unraidu ne drži isto preslikavanje prema svima, krivo javlja pošiljatelja ili conntrack u mostu napravi krivo preslikavanje kod istodobnog slanja. | srednja | Unraid se ne da probiti | mjerenje u probnim spremnicima prije F3; uvjet je EIM, ne očuvan port; mjerenje i dok W i L rade na istom portu; `natsim` s conntrackom; B kasni 1 s u 2. pokušaju | M, F3a |
| Unraid mora prijeći na mrežu računala. | samo ako mjerenje ne prođe | mijenja se web port i cilj tunela, a spremnik vidi Tailscale | zaseban mali plan, ne usput | nakon M |
| Vatrozid na računalu: Windows na profilu javne mreže, ufw na ThinkCentreu, dopuštenje za lokalnu mrežu na macOS-u. | srednja | prvi paketi, lokalni kandidati ili odgovori na pronalaženje tiho ne prolaze | provjera na terenu; upute u `INSTALACIJA.md` i `linux.md`; `natsim` s vatrozidom na računalu; pločica javlja kad udarci druge strane ne stižu; program sam ne dira vatrozid | M, F1, F3 |
| macOS veže UDP utičnicu uz sučelje (po Nebuli; nije izmjereno). | srednja | nakon selidbe laptop šalje u prazno: provjera mreže javi lažni „UDP blokiran”, glasnik ne odgovara | mjerenje nakon izdanja 1; nova utičnica pri promjeni adresa i grešci slanja; glasnik se sam ponovno pokrene | LAN1, F1, F2b |
| Stare privatne adrese troše vrijeme, do 15 s svaka. | sigurno, već danas | 4 odlazna mjesta stoje zauzeta, a krug kasni | usporedno, svaka iduća 250 ms kasnije, po 2–3 s; ukupni rok 45 s; naučene adrese samo na računalu (točka 2); greška po adresi na Sinkronizaciji | F0, F3b |
| Tunel dobije premalo vremena. | mala | prognoze ne stignu u ured, jer je tunel jedini put ThinkCentre → Unraid | tunel kreće odmah nakon LAN-a, usporedo s Tailscaleom; WEB sync se priprema u pozadini i ništa ga ne čeka; test u `natsim` | F3b |
| Priprema u pozadini ne stigne do razmjene. | mala | WEB sync se rijetko koristi, a sve ide kao danas | nakon uspjeha razmjena kreće odmah; sesija živi 6 min, dulje od kruga; brojač krugova starim putem dok se priprema | F3b |
| Pauza nakon neuspjeha drži par predugo izvan WEB synca. | srednja bez mjere | laptop koji dođe s mobilne mreže satima ide starim putem | pauza po stanju obiju strana, najviše 1 h, briše se kad se stanje promijeni | F3b |
| Žive sesije troše mobilne podatke. | mala | održavanje veze svakih 15 s po sesiji | sesija se gasi 6 min nakon zadnjeg toka; MiB na sat mjeri se na hotspotu | F3a, teren |
| Laptop se budi iz spavanja ili mijenja mrežu. | svaki dan | minute s mrtvom vezom | razmak sata (provjera 15 s, prag 22,5 s, puno ponovno spajanje nakon 10 min); adrese sučelja svakih 10 s; čekanje rute prije provjere mreže; nova UDP utičnica; odgoda signala kreće ispočetka | F2b |
| Spor početak, pa Postava vrati stari program. | mala | računalo se vrati na staro izdanje | WEB sync kreće u pozadini, tek nakon weba | F1 |
| Sat na ThinkCentreu odstupa ako mreža HV-a ne pušta NTP. | mala | potpisani pozivi (±10 min) se odbijaju | provjera u fazi M; pločica pokazuje odstupanje od sastajališta veće od 2 min | M, F2 |
| Napad na `/razmjena/signal` (preskakanje WAF-a vrijedi i za tu rutu). | mala do srednja | prijava je spora ili ne uspije, a stari tunel radi | ruta samo na Unraidu; rukovanja po adresi; granice iz §6.2; pravilo brzine u Cloudflareu samo za signal; stari tunel neovisan | F2 |
| Izdanje 1 nosi previše promjena odjednom. | srednja | mrežnu grešku teško je pripisati jednoj promjeni, a povratak skida i fazu 3 | izdanje 0 zasebno (granica, F0); LAN1 zaseban PR; W na 0.0.38 do izdanja 1; četiri izdanja | stalno |
| Preklapanje s fazom 3 i s pravilima na granici. | velika bez reda | sukobi pri spajanju grana | redoslijed iz odjeljka 5; jezgra F3 samo u novim datotekama; migracije jedna po jedna za sve grane; pojednostavljenja i errcheck ne diraju mrežne datoteke | stalno |
| Granica kvalitete: kod CC 7 traži se 100 % pokrivenosti, a velike funkcije ne smiju dobiti granu. Vrata to za `run`, `SyncWith` i `exchange` danas ne provode. | srednja | posao se oduži, ili nova grana u velikoj funkciji prođe nezapaženo | G0; cilj CC ≤ 6; nove funkcije umjesto grana; `premjestaji` za G1; tablice stanja; `natsim` s ubacivanjem grešaka; bez koda samo za jedan sustav u kritičnim paketima | sve faze |
| quic-go često mijenja sučelje, a `testutils/simnet` nije stabilno sučelje. | srednja | nadogradnja slomi kod ili testove | prikvačiti v0.63.0; `testutils/simnet` uvoziti iz istog modula quic-go, kao i ostatak biblioteke; sve u dvije datoteke `quic*.go`. Ako se sučelje simneta promijeni, prilagoditi testove ili u `internal/natsim` napisati vlastitu malu mrežu u memoriji, po ideji, bez prepisivanja | proba, F1 |
| Od 14. 11. radi se u manjem opsegu. | sigurno | F3 i teren kasne | jezgra F3 usporedo, čim je proba QUIC-a gotova; nakon 13. 11. u mrežnom dijelu samo F3b, izdanje 3, ispravci i F5; ako jezgra kasni, F3b i teren idu u prosinac | F3 |
| Unraid je jedino sastajalište. | sigurno | dok je ugašen, nema novih WEB sync veza | prihvatljivo za probu; LAN sync i Tailscale rade; zapisano u planu | — |
| Opoziv ne stiže odmah. | sigurno | opozvano računalo radi dok opoziv ne stigne razmjenom | čim stigne, otvorene sesije i prijave se odmah zatvaraju; `trusted()` na početku svakog toka 0x01; isto piše u dokumentaciji | F2, F3a |

---

## 8. Odluke (14. 10. 2026.)

Na šest pitanja iz prve inačice plana odgovoreno je 14. 10. 2026., svuda po preporuci. Istog dana dodane su i dvije dopune, o Tailscaleu i o rokovima.

1. **Nova referentna točka kvalitete prije G1: da.** G0 je zaseban mali PR prije G1: nova točka na čistom masteru, s opisom. `internal/ledger` ostaje izvan kritičnih paketa.
2. **Naučene adrese ostaju samo na računalu koje ih je naučilo.** U knjigu idu samo adrese koje upiše čovjek i adrese poslužitelja. Adresa iz pronalaženja nauči se tek kad razmjena na njoj prođe TLS s očekivanim ključem. Javna adresa se ne nauči nikad. Stare adrese u knjizi ostaju, a F3b ih zove kratko i usporedno. To je pola dana u F0.
3. **Posrednik preko Unraida se odgađa.** Bez WEB synca par razmjenjuje preko poslužitelja (SERVER sync), kao danas. Za par bez tunela, kao laptop i ThinkCentre, podaci čekaju na poslužitelju do idućeg kruga, do 10 min. Ništa se ne gubi, jer Unraid drži sve.
4. **Ulazak u mrežu:**
   - (a) Uparivanje preko Tailscalea, s istim šesteroznamenkastim kodom na oba ekrana, vrijedi kao ulazak u mrežu, kao i uparivanje u LAN-u.
   - (b) Primanje na daljinu isključuje se dok se ne odluči kako ulaze ispostave. Kod ostaje. Zaseban mali PR prije F2b skriva korak „Postojeća mreža na daljinu” u Postavljanju i isključuje uvoz potvrde; oko pola dana. Za 34 ispostave trebat će ulazak bez putovanja, na primjer s PAKE-om preko sastajališta, a to traži zasebnu odluku. Računala koja informatičari stave u Tailscale mogu se upariti njime već sada (4a), pa bi PAKE trebao samo za računala bez Tailscalea. Provjera članstva iz certifikata ostaje, jer je trebaju i računala tek uparena u LAN-u.
5. **Natpisi na pločici:**
   - (a) Tailscale ima zaseban natpis „Tailscale”.
   - (b) Zapisi koji čekaju nadogradnju: „Stiglo je N zapisa koje ovaj program još ne razumije. Primijenit će ih nakon nadogradnje goCOP-a.” Uz drugo računalo: „Windows čuva N zapisa koje će primijeniti nakon nadogradnje.” Svuda piše „nadogradnja”, pa postojeći tekst gubi „ažurirajte”.
6. **WEB sync postaje zadano uključen tek zasebnom odlukom**, nakon dva tjedna bez grešaka na svim testnim računalima i nakon što IT Hrvatskih voda dobije opis na predstavljanju bete.

**Dopuna o Tailscaleu.** Tailscale ostaje mogućnost, uz LAN, WEB i SERVER sync. Rijetko tko će ga koristiti. Sad ga koristi testno okruženje, a informatičari Hrvatskih voda možda ga žele za neka računala, sa svojim računom. goCOP razmjenjuje njime kad ga imaju oba računala, a `[sync] tailscale = false` ga isključuje. Tailscale adrese nikad ne ulaze u knjigu.

**Dopuna o rokovima.** Datumi u planu su okvirni redoslijed, ne rokovi. Faza kreće čim je spojena faza o kojoj ovisi, pa sve može biti gotovo i puno prije. Kraj studenog je samo okvirna gornja granica; što ne stane, ide u prosinac.

Dvije stvari nisu bile pitanja. gocop na Unraidu radi u mostu, a o prelasku na mrežu računala odlučuje ishod faze M. Redoslijed u prvom krugu već je u Odlukama (14. 10. 2026.): razmjena odmah krene putem koji radi, a WEB sync se pokušava u pozadini.

---

## 9. Napomene uz pregled

- Pregled kaže da `TestRedoslijedAdresaZaSpajanje` ne postoji u `c0162ca9`. Postoji (`internal/peers/tailscale_test.go:399`), pa ga plan zadržava i uz njega provjerava `Put` u `prekidac_test.go` i `bez_tailscalea_test.go`.
- Prijedlog da se pri nadogradnji u `gocop.toml` svakog računala upiše `web_sync = false` nije u planu. Program sam, bez administratora, ne mijenja `gocop.toml`, a zadana vrijednost mijenja se samo zasebnom odlukom (točka 6), pa se ne može promijeniti potiho.
- Prijedlog da RTT ide samo na stranicu Sinkronizacija nije u planu: broj milisekundi ne otkriva ni adresu ni raspored mreže. Na Sinkronizaciju odlaze adrese i sirovi tekst greške.
- Za isti ključ dvaput kod sastajališta dva pregleda traže suprotno: odbiti novu vezu, ili pustiti da nova zamijeni staru. Plan rabi broj procesa u prijavi i puls staroj vezi. Isti proces zamijeni svoju staru vezu, jer Cloudflare zna ostaviti poluotvorenu vezu. Drugi proces s istim ključem zamijeni staru samo ako ona ne odgovori na puls; inače se odbije uz upozorenje. To je naša odluka [P]; iroh radi drukčije (nova veza aktivna, stara u pričuvi).
- Prijedlog da se lokalna adresa Unraida objavi u knjizi kao adresa koju je upisao čovjek nije u planu. Laptop je dobiva u potpisanom pozivu preko sastajališta, pa ne mora ići na sva računala.
- Prijedlog da se F1 podijeli na dva dijela nije u planu. Simulator NAT-a premješten je u jezgru F3, gdje se stvarno koristi, pa F1 ostaje jedan PR. Popravci pronalaženja idu u zaseban mali PR (LAN1), jer ne ovise o WEB syncu.
- Pregled predlaže novo pitanje o redoslijedu u prvom krugu. Nije dodano: Odluke (14. 10. 2026.) već kažu da razmjena odmah krene putem koji radi, a WEB sync se pokušava u pozadini. Redoslijed LAN → WEB → SERVER vrijedi kad je WEB sync spreman, a LAN ide prvi uvijek.
- Prijedlog da se dva prekidača zamijene jednom postavkom s tri vrijednosti nije uzet. Administracija postavke prikazuje po vrsti, a dvije postavke s imenima koja kažu što rade (`web_sync_prijava`, `web_sync`) jednostavnije su za provjeru i test.

**Napomene uz uzore:**
- Proučavanje predlaže da natpis LAN sync ovisi o podmreži, kao u Syncthingu. Plan to ne uzima: kod kuće i u uredu postoji drugi usmjerivač, pa su laptop i Unraid u istoj kući, a u različitim podmrežama. Natpis zato ovisi o tome je li adresa privatna, kao u Tailscaleu. Popis „uvijek lokalno” (Syncthingov `AlwaysLocalNets`) zato ne treba.
- Proučavanje predlaže da put na pločici dođe već u F0. Ostaje u F1, uz nove stupce u `peer_sync`, jer F0 ne dira `internal/web`. F0 već pamti grešku po adresi.
- Za Unraid se predlaže mreža računala, kao u Syncthingu. Plan čeka ishod faze M, jer prelazak mijenja web port i cilj tunela, a spremnik tada vidi Tailscale.
- Proučavanje predlaže da se pri promjeni mreže zatvore samo sesije kojima je nestala lokalna adresa (iroh). Plan zatvara sve: nova UDP utičnica ionako zatvara sve QUIC sesije, a WEB sync ih vraća u pozadini.
- Trajanje sesije od 6 min je stalna vrijednost, ne postavka. Rokovi se ugađaju u F5.
- Ritam udaraca od 10–20 ms nije uzet: granica od 20 paketa u sekundi ostaje, pa razmaci rastu od 50 do 200 ms.
- Testovi uzora nisu pokretani. Tvrdnje na kojima stoje najvažnije izmjene (razmjena ne čeka probijanje, pauza, promjena mreže) provjerene su u izvoru. Za isti ključ dvaput izvor je provjeren, ali plan od njega uzima samo dio (vidi gore).
