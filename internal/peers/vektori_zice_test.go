package peers

// Ispitni vektori poruka razgovora: svaka poruka kako
// je piše razgovor (razgovorSCvorom.posalji: omotnica s imenom pošiljatelja,
// jedan JSON po retku), iz stalnog ulaza. Vektori ključeva, potpisa,
// uparivanja, signala i udaraca su u internal/razmjena (vektori_zice_test.go).
//
// Test uspoređuje izrađeno sa zapisom u testdata/vektori-razgovora.json.
// Zapis se piše iznova samo uz SYNCNET_VEKTORI_PISI=1, i to je izmjena žice.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

const datotekaVektoraRazgovora = "testdata/vektori-razgovora.json"

type vektoriRazgovora struct {
	Posiljatelj string `json:"posiljatelj"` // ime računala u omotnici

	// poruke redom kojim idu u razgovoru; svaka završava novim retkom
	Granica          string `json:"frontier"`
	GranicaStarog    string `json:"frontierBezZnanja"` // kako je šalje čvor bez oznaka
	GranicaOdbijenih string `json:"granicaOdbijenih"`
	Rupe             string `json:"rupe"`
	Delta            string `json:"delta"`
	PraznaDelta      string `json:"praznaDelta"`
	Zelje            string `json:"zelje"`
	Sadrzaj          string `json:"sadrzaj"`
	Kraj             string `json:"done"`

	Verzija      string `json:"verzija"`      // jedna verzija iz delte
	Vjerodajnice string `json:"vjerodajnice"` // {clanstvo, ovlast} za certifikat

	// DeltaPosebni: verzija čiji sadržaj nosi <, > i & doslovno. Go ih pri
	// slanju zapiše kao \u003c, \u003e i \u0026, i u sadržaju verzije, pa
	// se bajtovi sadržaja na žici razlikuju od onih koje je autor zadao.
	SadrzajPosebni string `json:"sadrzajSPosebnimZnakovima"` // kako ga je autor zadao
	DeltaPosebni   string `json:"deltaSPosebnimZnakovima"`   // kako ide žicom

	// SadrzajBezKanala: stavka sadržaja u zajedničkom kanalu. Polje kanal
	// tada izostaje, pa ga primatelj ne smije tražiti.
	SadrzajBezKanala string `json:"sadrzajBezKanala"`

	Cop      vektoriCopa     `json:"cop"`      // paket datotekom
	Primanje vektoriPrimanja `json:"primanje"` // primanje na daljinu
}

// vektoriCopa: dijelovi paketa .cop koji se potpisuju ili otiskuju.
// Inačica 3 je paket bez rupa, inačica 4 s rupama.
type vektoriCopa struct {
	Zapisi       string `json:"zapisi"`       // zapisi.jsonl
	Sadrzaji     string `json:"sadrzaji"`     // sadrzaji.json
	Otisak       string `json:"otisak"`       // otisak izdanja
	Kanonski     string `json:"kanonski"`     // potpisani zapis manifesta
	Potpis       string `json:"potpis"`       // ključ A nad kanonskim zapisom
	Manifest     string `json:"manifest"`     // manifest.json
	KanonskiRupe string `json:"kanonskiRupe"` // isto, s rupama (inačica 4)
	PotpisRupe   string `json:"potpisRupe"`
	ManifestRupe string `json:"manifestRupe"`
}

// vektoriPrimanja: kod za primanje, zahtjev novog računala i potvrda
// primatelja. Sol i kod su stalni; ključ dokaza je scrypt (N = 2^16, r = 8,
// p = 1) nad kodom bez crtice i soli s oznakom gocop-primanje/1|.
type vektoriPrimanja struct {
	Kod            string `json:"kod"`            // kako ga čovjek prepiše
	Sol            string `json:"sol"`            // base64, 16 bajtova
	KljucDokaza    string `json:"kljucDokaza"`    // hex
	ZahtjevBajtovi string `json:"zahtjevBajtovi"` // pokrivaju ih potpis i dokaz
	ZahtjevDokaz   string `json:"zahtjevDokaz"`
	ZahtjevPotpis  string `json:"zahtjevPotpis"` // ključ B
	Zahtjev        string `json:"zahtjev"`       // datoteka zahtjeva
	PotvrdaKanon   string `json:"potvrdaKanon"`  // pokriva ga dokaz potvrde
	PotvrdaDokaz   string `json:"potvrdaDokaz"`
	Potvrda        string `json:"potvrda"` // datoteka potvrde
}

// redakPoruke zapiše poruku kako je piše razgovor: omotnica s imenom
// pošiljatelja, json.Encoder, novi red
func redakPoruke(t *testing.T, posiljatelj, kind string, v any) string {
	t.Helper()
	e, err := razmjena.NewEnvelope(kind, v)
	if err != nil {
		t.Fatal(err)
	}
	e.DeviceID = posiljatelj
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(e); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func izradiVektoreRazgovora(t *testing.T) vektoriRazgovora {
	t.Helper()
	const ja = "admin-1"
	od := time.Unix(1_760_000_000, 0).UTC()

	prva := ledger.Version{
		VersionID: "0199c82c-0000-7000-8000-000000000001", Entity: "biljeske", EntityID: "biljeska-1",
		NodeID: ja, Payload: json.RawMessage(`{"id":"biljeska-1","tekst":"Subota"}`),
		CreatedAt: od, SchemaVersion: 1,
	}
	druga := ledger.Version{
		VersionID: "0199c82c-0000-7000-8000-000000000002", Entity: "biljeske", EntityID: "biljeska-1",
		NodeID: ja, Supersedes: prva.VersionID, Archived: true, Payload: json.RawMessage(`{"id":"biljeska-1"}`),
		CreatedAt: od.Add(time.Second), SchemaVersion: 1, Channel: "primjer/7/2026",
	}
	granica := map[string]string{
		ledger.FrontierKey(ja, ""):            prva.VersionID,
		ledger.FrontierKey(ja, druga.Channel): druga.VersionID,
		ledger.FrontierKey("racunalo-b", ""):  "0199c82c-0000-7000-8000-00000000000b",
	}

	var v vektoriRazgovora
	v.Posiljatelj = ja
	v.Granica = redakPoruke(t, ja, kindFrontier, frontierMsg{
		Frontier: granica, Wants: &Wants{All: true}, Program: "1.0.0", Gradnja: "1.0.0+abc1234",
		Znanje: razmjena.Znanje{
			Mogucnosti:      []string{razmjena.OznakaGranicaOdbijenih, razmjena.OznakaRazmjena, razmjena.OznakaRupe},
			Vrste:           map[string]int{"biljeske": 1},
			CekaNadogradnju: 2,
		},
	})
	v.GranicaStarog = redakPoruke(t, ja, kindFrontier, frontierMsg{Frontier: granica, Wants: &Wants{All: true}})
	v.GranicaOdbijenih = redakPoruke(t, ja, kindGranicaOdbijenih, odbijeneMsg{
		Granica: map[string]string{ledger.FrontierKey("racunalo-b", ""): "0199c82c-0000-7000-8000-00000000000a"}})
	v.Rupe = redakPoruke(t, ja, kindRupe, rupeMsg{
		Trazim: map[string]raspon{ledger.FrontierKey("racunalo-b", ""): {Od: "", Do: "0199c82c-0000-7000-8000-000000000009"}}})
	v.Delta = redakPoruke(t, ja, kindDelta, deltaMsg{
		Versions: []ledger.Version{prva, druga}, Skraceno: true,
		Rupe:   map[string]raspon{ledger.FrontierKey(ja, ""): {Od: "0199c82c-0000-7000-8000-000000000000", Do: beskraj}},
		Popuna: map[string]raspon{ledger.FrontierKey("racunalo-b", ""): {Od: "", Do: "0199c82c-0000-7000-8000-000000000005"}},
	})
	v.PraznaDelta = redakPoruke(t, ja, kindDelta, deltaMsg{})
	otisak := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" // sha256("hello")
	v.Zelje = redakPoruke(t, ja, kindZelje, zeljeMsg{Otisci: []string{otisak}})
	v.Sadrzaj = redakPoruke(t, ja, kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
		{Otisak: otisak, Vrsta: "text/plain", Kanal: druga.Channel, Bajtova: 5, Podaci: []byte("hello")}}})
	v.Kraj = redakPoruke(t, ja, kindDone, doneMsg{Applied: 2})

	b, err := json.Marshal(druga)
	if err != nil {
		t.Fatal(err)
	}
	v.Verzija = string(b)

	v.SadrzajPosebni = `{"id":"biljeska-2","tekst":"a<b>&c"}`
	treca := ledger.Version{
		VersionID: "0199c82c-0000-7000-8000-000000000003", Entity: "biljeske", EntityID: "biljeska-2",
		NodeID: ja, Payload: json.RawMessage(v.SadrzajPosebni), CreatedAt: od.Add(2 * time.Second), SchemaVersion: 1,
	}
	v.DeltaPosebni = redakPoruke(t, ja, kindDelta, deltaMsg{Versions: []ledger.Version{treca}})
	v.SadrzajBezKanala = redakPoruke(t, ja, kindSadrzaj, sadrzajMsg{Stavke: []stavkaSadrzaja{
		{Otisak: otisak, Vrsta: "text/plain", Bajtova: 5, Podaci: []byte("hello")}}})

	// vjerodajnice za certifikat: isti ulaz kao u vektorima paketa razmjena
	privA := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x01}, ed25519.SeedSize))
	mreza := razmjena.LoadNetworkKey("Obitelj", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x10}, ed25519.SeedSize)))
	pubA := privA.Public().(ed25519.PublicKey)
	ovlast, err := mreza.OvlastiOd(ja, pubA, "root", od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	clan, err := mreza.AdmitOd(ja, pubA, "root", od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	vj, err := json.Marshal(vjerodajniceCvora{Clanstvo: &clan, Ovlast: &ovlast})
	if err != nil {
		t.Fatal(err)
	}
	v.Vjerodajnice = string(vj)
	v.Cop = izradiVektoreCopa(t, privA, []ledger.Version{prva, druga})
	v.Primanje = izradiVektorePrimanja(t, mreza, od)
	return v
}

func izradiVektoreCopa(t *testing.T, priv ed25519.PrivateKey, verzije []ledger.Version) vektoriCopa {
	t.Helper()
	zapisi, ispravne, neispravne := zapisiZaCop(verzije)
	if len(ispravne) != len(verzije) || len(neispravne) != 0 {
		t.Fatalf("zapisi paketa: %d ispravnih, %d neispravnih", len(ispravne), len(neispravne))
	}
	sadrzaji := []CopSadrzaj{
		{Otisak: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", Vrsta: "text/plain", Bajtova: 5, Kanal: "primjer/7/2026", Ukljucen: true},
		{Otisak: "0000000000000000000000000000000000000000000000000000000000000001", Vrsta: "application/pdf", Bajtova: 12},
	}
	popis, _ := json.MarshalIndent(sadrzaji, "", " ")
	m := CopManifest{
		Inacica: 3, Obuhvat: CopObuhvat{Vrsta: "primjer", AreaID: 7, Od: 2026}, Kanali: []string{"primjer/7/2026", ""},
		Izdanje: 2, Prethodno: "aa", Izdao: "admin-1", Nastalo: time.Unix(1_760_000_000, 0).UTC(),
		Otisak: otisakIzdanja(zapisi, sadrzaji), Zapisa: len(ispravne), Sadrzaja: len(sadrzaji), Ukljuceno: 1,
	}
	potpisi := func(m CopManifest) (string, string, string) {
		kanon := kanonskiCop(m)
		m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
			Potpis: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, kanon))}
		if _, err := ProvjeriCopPotpis(m); err != nil {
			t.Fatal(err)
		}
		b, _ := json.MarshalIndent(m, "", " ")
		return string(kanon), m.Potpis.Potpis, string(b)
	}
	var v vektoriCopa
	v.Zapisi, v.Sadrzaji, v.Otisak = string(zapisi), string(popis), m.Otisak
	v.Kanonski, v.Potpis, v.Manifest = potpisi(m)
	m.Rupe = map[string]raspon{ledger.FrontierKey("racunalo-b", ""): {Od: "", Do: "0199c82c-0000-7000-8000-000000000009"}}
	m.Inacica = inacicaCopa(m.Rupe)
	v.KanonskiRupe, v.PotpisRupe, v.ManifestRupe = potpisi(m)
	return v
}

func izradiVektorePrimanja(t *testing.T, mreza razmjena.NetworkKey, od time.Time) vektoriPrimanja {
	t.Helper()
	if scryptN != 1<<16 {
		t.Fatalf("cijena izvoda ključa iz koda je %d, a vektori traže 2^16", scryptN)
	}
	privB := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x02}, ed25519.SeedSize))
	pubB := privB.Public().(ed25519.PublicKey)
	var v vektoriPrimanja
	v.Kod = "7KQ4-M2XD"
	sol := bytes.Repeat([]byte{0xCC}, 16)
	v.Sol = base64.StdEncoding.EncodeToString(sol)
	kljuc, err := kljucIzKoda(v.Kod, sol)
	if err != nil {
		t.Fatal(err)
	}
	v.KljucDokaza = hex.EncodeToString(kljuc)

	z := Zahtjev{V: verzijaZahtjeva, Cvor: "racunalo-b", Naziv: "Računalo B", Kljuc: razmjena.PublicKeyString(pubB),
		Izdanje: "1.0.0", Vrijeme: od, Sol: v.Sol}
	v.ZahtjevBajtovi = string(z.signedBytes())
	z.Dokaz = dokaz(kljuc, verzijaZahtjeva, z.signedBytes())
	z.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(privB, z.signedBytes()))
	v.ZahtjevDokaz, v.ZahtjevPotpis = z.Dokaz, z.Potpis
	b, _ := json.MarshalIndent(z, "", "  ")
	v.Zahtjev = string(b)
	if _, k, err := procitajZahtjevSKodom(b, "7kq4 m2xd"); err != nil || !bytes.Equal(k, kljuc) {
		t.Fatalf("zahtjev iz vektora se ne da pročitati s kodom: %v", err)
	}

	clan, err := mreza.AdmitOd(z.Cvor, pubB, "admin-1", od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p := Potvrda{V: verzijaPotvrde, Mreza: mreza.Name, KljucMreze: razmjena.PublicKeyString(mreza.Public), Clanstvo: clan,
		Izdao: "admin-1", Izdano: od, Sol: z.Sol,
		Cvorovi: []CvorPotvrde{{Peer: Peer{NodeID: "admin-1", Name: "Admin 1", PublicKey: razmjena.PublicKeyString(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x01}, ed25519.SeedSize)).Public().(ed25519.PublicKey)),
			Addresses: []string{"192.0.2.1:4710"}, CreatedAt: od}}}}
	bez, _ := json.Marshal(p)
	kanon, err := kanonPotvrde(bez)
	if err != nil {
		t.Fatal(err)
	}
	v.PotvrdaKanon = string(kanon)
	datoteka := p.potpisanKodom(kljuc)
	v.Potvrda = string(datoteka)
	var procitana Potvrda
	if err := json.Unmarshal(datoteka, &procitana); err != nil {
		t.Fatal(err)
	}
	v.PotvrdaDokaz = procitana.Dokaz
	return v
}

func zapisVektoraRazgovora(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestVektoriRazgovora(t *testing.T) {
	prvi := zapisVektoraRazgovora(t, izradiVektoreRazgovora(t))
	if drugi := zapisVektoraRazgovora(t, izradiVektoreRazgovora(t)); !bytes.Equal(prvi, drugi) {
		t.Fatal("isti ulaz dao je dva različita zapisa vektora")
	}
	if os.Getenv("SYNCNET_VEKTORI_PISI") == "1" {
		if err := os.MkdirAll(filepath.Dir(datotekaVektoraRazgovora), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(datotekaVektoraRazgovora, prvi, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	zapisano, err := os.ReadFile(datotekaVektoraRazgovora)
	if err != nil {
		t.Fatalf("zapisa vektora nema (%v); prvi put ga zapiši uz SYNCNET_VEKTORI_PISI=1", err)
	}
	if !bytes.Equal(prvi, zapisano) {
		t.Fatalf("vektori razgovora više nisu isti kao u %s: poruka razgovora promijenila je oblik", datotekaVektoraRazgovora)
	}

	// vjerodajnice su isti bajtovi koje vektori paketa razmjena stavljaju u certifikat
	var zice struct {
		Vjerodajnice string `json:"vjerodajnice"`
	}
	b, err := os.ReadFile("../razmjena/testdata/vektori-zice.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &zice); err != nil {
		t.Fatal(err)
	}
	if got := izradiVektoreRazgovora(t).Vjerodajnice; got != zice.Vjerodajnice {
		t.Fatalf("vjerodajnice čvora nisu iste kao u vektorima žice:\n%s\n%s", got, zice.Vjerodajnice)
	}
}
