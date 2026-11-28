package razmjena

// Ispitni vektori žice: iz stalnih ključeva i stalnog
// ulaza izrađuje sve što druga izvedba protokola mora dobiti bajt po bajt
// isto. Ed25519 potpis i json.Marshal daju isti izlaz za isti ulaz, pa se
// vektori izrađuju ponovljivo.
//
// Test uspoređuje izrađeno sa zapisom u testdata/vektori-zice.json. Zapis
// se piše iznova samo uz SYNCNET_VEKTORI_PISI=1, i to je izmjena žice: tko
// je radi, mora znati zašto. Uz SYNCNET_VEKTORI_PROTOKOL i
// SYNCNET_VEKTORI_IZLAZ vektori se izrade pod drugim imenom protokola u
// zadanu datoteku, bez usporedbe (za usporedbu s drugom izvedbom).

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const datotekaVektora = "testdata/vektori-zice.json"

// ulazVektora je stalni ulaz: isti kao u ispitnim vektorima Rust izvedbe,
// da se dvije izvedbe mogu usporediti izravno
type ulazVektora struct {
	Protokol     string `json:"protokol"` // daje aplikacija
	SjemeA       string `json:"sjemeA"`
	SjemeB       string `json:"sjemeB"`
	SjemeMreze   string `json:"sjemeMreze"`
	KljucA       string `json:"kljucA"`
	KljucB       string `json:"kljucB"`
	KljucMreze   string `json:"kljucMreze"`
	Vrijeme      int64  `json:"vrijeme"`
	Sesija       string `json:"sesija"`
	TajnaPoziva  string `json:"tajnaPoziva"`
	TajnaOdziva  string `json:"tajnaOdziva"`
	BrojPoziva   string `json:"brojPozivatelja"`
	BrojSlusa    string `json:"brojSlusalice"`
	OznakaFormat string `json:"oznakaVjerodajnica"` // daje aplikacija (danas stalna u kodu)
}

type vektorPotpisa struct {
	Bajtovi string `json:"bajtovi"` // što se potpisuje, kao tekst
	Potpis  string `json:"potpis"`  // base64
	Zapis   string `json:"zapis"`   // cijeli zapis kako putuje (JSON)
}

type vektorUdarca struct {
	Smjer       byte   `json:"smjer"` // 0: A→B, 1: B→A
	KljucSlanja string `json:"kljucSlanja"`
	Udarac7     string `json:"udarac7"`  // vrsta 0x10, brojač 7
	Odgovor7    string `json:"odgovor7"` // vrsta 0x11, brojač 7
}

type vektoriZice struct {
	Ulaz ulazVektora `json:"ulaz"`

	Certifikat struct {
		CN  string `json:"cn"`
		URI string `json:"uriVjerodajnica"` // SAN URI za vjerodajnice ispod
	} `json:"certifikat"`
	Vjerodajnice string `json:"vjerodajnice"` // JSON {clanstvo, ovlast} računala A

	ALPN struct {
		Signal string `json:"signal"`
		Web    string `json:"web"`
		Proba  string `json:"proba"`
	} `json:"alpn"`

	Pronalazenje struct {
		Proba  string `json:"proba"`
		Beacon string `json:"beacon"`
	} `json:"pronalazenje"`

	Uparivanje struct {
		Hello      string `json:"hello"`
		Obveza     string `json:"obveza"` // heks, sha256
		PorukaObv  string `json:"porukaObveze"`
		PorukaBroj string `json:"porukaBroja"`
		Kod        string `json:"kod"`
		Potvrda    string `json:"potvrda"`
		// KodSNulom: isti ključevi i isti broj pozivatelja, a broj pozvanog
		// takav da kod počinje nulom. Izvedba koja kod ne piše s vodećim
		// nulama dobije kraći tekst.
		KodSNulom struct {
			BrojPozvanog string `json:"brojPozvanog"` // heks
			Kod          string `json:"kod"`
		} `json:"kodSNulom"`
	} `json:"uparivanje"`

	Ovlast          vektorPotpisa  `json:"ovlast"`
	ClanstvoMreze   vektorPotpisa  `json:"clanstvoKljucemMreze"`
	ClanstvoPrimat  vektorPotpisa  `json:"clanstvoOdPrimatelja"`
	Opoziv          vektorPotpisa  `json:"opoziv"`
	OznakaOpoziva   string         `json:"oznakaOpoziva"`
	Omotnica        string         `json:"omotnica"`   // jedna poruka razgovora, s novim retkom
	Odbijenica      string         `json:"odbijenica"` // s novim retkom
	Kodovi          map[string]int `json:"kodoviOdbijanja"`
	Mogucnosti      []string       `json:"mogucnosti"`
	DogovorSaStarim map[string]int `json:"dogovorSCvoromBezOznaka"`

	Signal struct {
		Prefiks      string `json:"prefiksPotpisa"`
		Tijelo       string `json:"tijelo"`       // bajtovi koji se potpisuju iza prefiksa
		Potpis       string `json:"potpis"`       // base64
		Poruka       string `json:"poruka"`       // {vrsta, tijelo} kako ide u okvir
		Okvir        string `json:"okvir"`        // heks: 4 bajta duljine i poruka
		OkvirPulsa   string `json:"okvirPulsa"`   // heks
		NajveciOkvir int    `json:"najveciOkvir"` // bajtova sadržaja
	} `json:"signal"`

	UdarciA vektorUdarca `json:"udarciA"`
	UdarciB vektorUdarca `json:"udarciB"`

	// PosebniZnakovi: Go u JSON-u znakove <, > i & piše kao \u003c, \u003e
	// i \u0026, i u kanonskim bajtovima koji se potpisuju i u porukama.
	// Izvedba koja ih piše doslovno dobije druge bajtove i drugi potpis.
	PosebniZnakovi struct {
		Ime        string        `json:"ime"` // ime računala i izdavatelja u ovim vektorima
		Ovlast     vektorPotpisa `json:"ovlast"`
		Clanstvo   vektorPotpisa `json:"clanstvo"`
		Odbijenica string        `json:"odbijenica"` // s novim retkom
		// razlog nosi U+2028 i U+2029; Go ih piše kao \u2028 i \u2029
		OdbijenicaRazdjelnici string `json:"odbijenicaSRazdjelnicima"`
	} `json:"posebniZnakovi"`

	// PotpisVerzije: verzija iz knjige koju autor (računalo A) potpiše.
	// Druga ima sadržaj s razmacima, s <, > i & i s U+2028, vrijeme s
	// nanosekundama i druge vrijednosti ostalih polja.
	//
	// Rubovi su ishodi provjere potpisa nad običnom verzijom, za potpis i
	// ključ zapisane na razne načine. SadrzajSBrojevima ima brojeve 1.0 i
	// 1e2 i bijegove \u0041, \/ i \u003C koji u obliku sadržaja ostaju kako
	// su napisani. RazdjelnikUPolju ima U+2028 u entity_id i u kanalu.
	//
	// Odluke primatelja koje ovise o knjizi i članstvima (ključ koji nije
	// vezan uz node_id, članstvo iz iste delte, strože pravilo za računalo
	// s oznakom, dopuna) ovdje nisu: vektori ih ne pokrivaju.
	PotpisVerzije struct {
		Obicna            vektorVerzije `json:"obicna"`
		Rubovi            []rubPotpisa  `json:"rubovi"`
		SadrzajSBrojevima vektorVerzije `json:"sadrzajSBrojevimaIBijegovima"`
		RazdjelnikUPolju  vektorVerzije `json:"razdjelnikUPoljima"`
		PosebniZnakovi    vektorVerzije `json:"posebniZnakovi"`
	} `json:"potpisVerzije"`
}

// rubPotpisa je jedan redak tablice rubova: potpis i ključ autora kako
// stoje uz običnu verziju i što provjera kaže
type rubPotpisa struct {
	Opis        string `json:"opis"`
	Potpis      string `json:"potpis"`
	KljucAutora string `json:"kljuc_autora"`
	// Ishod: "nepotpisana" kad su oba polja prazna (tako ih primatelj
	// čita, peers.provjeraPotpisa), inače "vrijedi" ili "kriv" prema
	// ProvjeriPotpisVerzije
	Ishod string `json:"ishod"`
}

// ishodPotpisa je ono što primatelj zna iz samog potpisa verzije, prije
// pitanja o knjizi i članstvima
func ishodPotpisa(protokol string, v PodaciVerzije, potpis, kljucAutora string) string {
	if potpis == "" && kljucAutora == "" {
		return "nepotpisana"
	}
	if ProvjeriPotpisVerzije(protokol, v, potpis, kljucAutora) != nil {
		return "kriv"
	}
	return "vrijedi"
}

// visakBitova promijeni najniži bit zadnjeg znaka ispred dopune: bajtovi
// ostaju isti, a tekst više nije kanonski
func visakBitova(t *testing.T, tekst string) string {
	t.Helper()
	const abeceda = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	podaci := strings.TrimRight(tekst, "=")
	zadnji := len(podaci) - 1
	i := strings.IndexByte(abeceda, podaci[zadnji])
	if i < 0 {
		t.Fatalf("%q nije base64", tekst)
	}
	izmijenjen := podaci[:zadnji] + string(abeceda[i^1]) + tekst[len(podaci):]
	if a, b := iz64(t, tekst), iz64(t, izmijenjen); !bytes.Equal(a, b) || izmijenjen == tekst {
		t.Fatalf("višak bitova promijenio je bajtove: %q", izmijenjen)
	}
	return izmijenjen
}

func iz64(t *testing.T, tekst string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(tekst)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rubovePotpisa izradi tablicu rubova nad verzijom v koju je potpisao
// autor (ključ A); drugi je ključ B, koji u base64 ima + i /
func rubovePotpisa(t *testing.T, protokol string, autor, drugi ed25519.PrivateKey, v PodaciVerzije) []rubPotpisa {
	t.Helper()
	nad := func(priv ed25519.PrivateKey, kljuc string) string { // potpis nad bilo kojim tekstom ključa
		b, err := BajtoviVerzije(protokol, v, kljuc)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b))
	}
	s, k, err := PotpisiVerziju(autor, protokol, v)
	if err != nil {
		t.Fatal(err)
	}
	k2 := PublicKeyString(drugi.Public().(ed25519.PublicKey))
	if !strings.Contains(k2, "+") || !strings.Contains(k2, "/") {
		t.Fatalf("ključ %s nema i + i /", k2)
	}
	kljuc, potpis := iz64(t, k), iz64(t, s)
	b64 := base64.StdEncoding.EncodeToString
	urlSafe := strings.NewReplacer("+", "-", "/", "_").Replace

	// točka identiteta u tri zapisa; uz R = identitet i s = 0 potpis
	// vrijedi pod njom za svaku poruku
	identitet := make([]byte, 32)
	identitet[0] = 1
	identitetYP := bytes.Repeat([]byte{0xff}, 32) // y + p
	identitetYP[0], identitetYP[31] = 0xee, 0x7f
	identitetPredznak := bytes.Clone(identitet)
	identitetPredznak[31] = 0x80
	potpisIdentiteta := b64(append(bytes.Clone(identitet), make([]byte, 32)...))

	nijeTocka := make([]byte, 32) // y = 2 nije točka krivulje
	nijeTocka[0] = 2

	// s + L: isti potpis s nesvedenim skalarom
	l := [32]byte{0xed, 0xd3, 0xf5, 0x5c, 0x1a, 0x63, 0x12, 0x58, 0xd6, 0x9c, 0xf7, 0xa2, 0xde, 0xf9, 0xde, 0x14, 31: 0x10}
	sPlusL := bytes.Clone(potpis)
	prijenos := 0
	for i := range 32 {
		zbroj := int(potpis[32+i]) + int(l[i]) + prijenos
		sPlusL[32+i], prijenos = byte(zbroj), zbroj>>8
	}
	if prijenos != 0 {
		t.Fatal("s + L ne stane u 32 bajta")
	}
	visokiBit := bytes.Clone(potpis)
	visokiBit[63] |= 0x80

	k31, k33 := b64(kljuc[:31]), b64(append(bytes.Clone(kljuc), 0))
	kBezDopune, kVisakDopune := strings.TrimRight(k, "="), k+"="
	kRazmakIspred, kRazmakIza, kTabIza := " "+k, k+" ", k+"\t"
	kNoviRedIza, kCRLFIza, kNoviRedUSredini := k+"\n", k+"\r\n", k[:20]+"\n"+k[20:]
	kVisakBitova := visakBitova(t, k)
	k2URL := urlSafe(k2)

	redovi := []struct{ opis, potpis, kljuc string }{
		{"oba_prazna", "", ""},
		{"samo_kljuc", "", k},
		{"samo_potpis", s, ""},
		{"ispravno", s, k},
		{"kljuc_razmak_bez_potpisa", "", " "},
		{"kljuc_novi_red_bez_potpisa", "", "\n"},
		{"potpis_novi_red_bez_kljuca", "\n", ""},
		{"kljuc_nije_base64", s, "nije kljuc"},
		{"kljuc_31B", nad(autor, k31), k31},
		{"kljuc_33B", nad(autor, k33), k33},
		{"kljuc_bez_dopune", nad(autor, kBezDopune), kBezDopune},
		{"kljuc_visak_dopune", nad(autor, kVisakDopune), kVisakDopune},
		{"kljuc_razmak_ispred", nad(autor, kRazmakIspred), kRazmakIspred},
		{"kljuc_razmak_iza", nad(autor, kRazmakIza), kRazmakIza},
		{"kljuc_tab_iza", nad(autor, kTabIza), kTabIza},
		{"kljuc_novi_red_iza", nad(autor, kNoviRedIza), kNoviRedIza},
		{"kljuc_crlf_iza", nad(autor, kCRLFIza), kCRLFIza},
		{"kljuc_novi_red_u_sredini", nad(autor, kNoviRedUSredini), kNoviRedUSredini},
		{"kljuc_novi_red_iza_stari_potpis", s, kNoviRedIza},
		{"kljuc_visak_bitova", nad(autor, kVisakBitova), kVisakBitova},
		{"kljuc_visak_bitova_stari_potpis", s, kVisakBitova},
		{"kljuc_url_safe", nad(drugi, k2URL), k2URL},
		{"kljuc_url_safe_kanonski", nad(drugi, k2), k2},
		{"kljuc_nije_tocka", nad(autor, b64(nijeTocka)), b64(nijeTocka)},
		{"kljuc_identitet", potpisIdentiteta, b64(identitet)},
		{"kljuc_identitet_nekanonski_y", potpisIdentiteta, b64(identitetYP)},
		{"kljuc_identitet_s_predznakom", potpisIdentiteta, b64(identitetPredznak)},
		{"potpis_nije_base64", "nije potpis", k},
		{"potpis_razmak", " ", k},
		{"potpis_63B", b64(potpis[:63]), k},
		{"potpis_65B", b64(append(bytes.Clone(potpis), 0)), k},
		{"potpis_bez_dopune", strings.TrimRight(s, "="), k},
		{"potpis_visak_dopune", s + "=", k},
		{"potpis_razmak_ispred", " " + s, k},
		{"potpis_razmak_iza", s + " ", k},
		{"potpis_novi_red_iza", s + "\n", k},
		{"potpis_crlf_iza", s + "\r\n", k},
		{"potpis_novi_red_u_sredini", s[:30] + "\n" + s[30:], k},
		{"potpis_novi_red_u_dopuni", s[:len(s)-1] + "\n=", k},
		{"potpis_visak_bitova", visakBitova(t, s), k},
		{"potpis_url_safe", urlSafe(s), k},
		{"potpis_nekanonski_s", b64(sPlusL), k},
		{"potpis_visoki_bit", b64(visokiBit), k},
	}
	rubovi := make([]rubPotpisa, 0, len(redovi))
	for _, r := range redovi {
		rubovi = append(rubovi, rubPotpisa{r.opis, r.potpis, r.kljuc, ishodPotpisa(protokol, v, r.potpis, r.kljuc)})
	}
	return rubovi
}

// ulazVerzije su podaci verzije kako stoje u knjizi
type ulazVerzije struct {
	VersionID     string `json:"version_id"`
	Entity        string `json:"entity"`
	EntityID      string `json:"entity_id"`
	NodeID        string `json:"node_id"`
	Supersedes    string `json:"supersedes"`
	Archived      bool   `json:"archived"`
	Channel       string `json:"channel"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     string `json:"created_at"` // RFC 3339 s nanosekundama
	Payload       string `json:"payload"`    // kako je spremljen
}

type vektorVerzije struct {
	Ulaz        ulazVerzije `json:"ulaz"`
	OblikSadrz  string      `json:"oblikSadrzaja"` // sadržaj kako ga Go šalje žicom; nad njim je sažetak
	Sadrzaj     string      `json:"sadrzaj"`       // sažetak sadržaja, heks SHA-256
	Bajtovi     string      `json:"bajtovi"`       // što se potpisuje, kao tekst
	KljucAutora string      `json:"kljucAutora"`
	Potpis      string      `json:"potpis"` // base64
}

func vektorPotpisaVerzije(t *testing.T, autor ed25519.PrivateKey, protokol string, v PodaciVerzije) vektorVerzije {
	t.Helper()
	sazetak, err := SazetakSadrzaja(v.Payload)
	if err != nil {
		t.Fatal(err)
	}
	potpis, kljuc, err := PotpisiVerziju(autor, protokol, v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BajtoviVerzije(protokol, v, kljuc)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProvjeriPotpisVerzije(protokol, v, potpis, kljuc); err != nil {
		t.Fatalf("potpis verzije ne prolazi provjeru: %v", err)
	}
	oblik, err := json.Marshal(json.RawMessage(v.Payload))
	if err != nil {
		t.Fatal(err)
	}
	if h := sha256.Sum256(oblik); hex.EncodeToString(h[:]) != sazetak {
		t.Fatal("sažetak sadržaja nije SHA-256 Go-ova oblika sadržaja")
	}
	return vektorVerzije{
		Ulaz: ulazVerzije{v.VersionID, v.Entity, v.EntityID, v.NodeID, v.Supersedes, v.Archived, v.Channel,
			v.SchemaVersion, v.CreatedAt.Format(time.RFC3339Nano), string(v.Payload)},
		OblikSadrz: string(oblik), Sadrzaj: sazetak, Bajtovi: string(b), KljucAutora: kljuc, Potpis: potpis,
	}
}

func sjeme(b byte) []byte { return bytes.Repeat([]byte{b}, ed25519.SeedSize) }

func niz(od byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = od + byte(i)
	}
	return b
}

// izradiVektore izradi sve vektore pod imenom protokola protokol
func izradiVektore(t *testing.T, protokol string) vektoriZice {
	t.Helper()
	privA := ed25519.NewKeyFromSeed(sjeme(0x01))
	privB := ed25519.NewKeyFromSeed(sjeme(0x02))
	privM := ed25519.NewKeyFromSeed(sjeme(0x10))
	pubA := privA.Public().(ed25519.PublicKey)
	pubB := privB.Public().(ed25519.PublicKey)
	pubM := privM.Public().(ed25519.PublicKey)
	const vrijeme = int64(1_760_000_000)
	od := time.Unix(vrijeme, 0).UTC()
	sesija := "00112233445566778899aabbccddeeff"
	tajnaPoziva, tajnaOdziva := bytes.Repeat([]byte{0xAA}, 32), bytes.Repeat([]byte{0xBB}, 32)
	brojP, brojS := niz(0x00, velicinaBroja), niz(0x80, velicinaBroja)

	var v vektoriZice
	v.Ulaz = ulazVektora{
		Protokol: protokol,
		SjemeA:   hex.EncodeToString(sjeme(0x01)), SjemeB: hex.EncodeToString(sjeme(0x02)), SjemeMreze: hex.EncodeToString(sjeme(0x10)),
		KljucA: PublicKeyString(pubA), KljucB: PublicKeyString(pubB), KljucMreze: PublicKeyString(pubM),
		Vrijeme: vrijeme, Sesija: sesija,
		TajnaPoziva: hex.EncodeToString(tajnaPoziva), TajnaOdziva: hex.EncodeToString(tajnaOdziva),
		BrojPoziva: hex.EncodeToString(brojP), BrojSlusa: hex.EncodeToString(brojS),
		OznakaFormat: prefiksVjerodajnica,
	}

	// ovlast i članstva
	mreza := LoadNetworkKey("Obitelj", privM)
	ovlast, err := mreza.OvlastiOd("admin-1", pubA, "root", od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v.Ovlast = vektorPotpisa{string(ovlast.signedBytes()), ovlast.Signature, uJSON(t, ovlast)}
	clanA, err := mreza.AdmitOd("admin-1", pubA, "root", od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v.ClanstvoMreze = vektorPotpisa{string(clanA.signedBytes()), clanA.Signature, uJSON(t, clanA)}
	clanB, err := AdmitAsOd(privA, ovlast, "racunalo-b", pubB, od, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	v.ClanstvoPrimat = vektorPotpisa{string(clanB.signedBytes()), clanB.Signature, uJSON(t, clanB)}

	// opoziv članstva računala B, ključem mreže
	op := Opoziv{Vrsta: "clanstvo", NodeID: "racunalo-b", PublicKey: PublicKeyString(pubB),
		IssuedAt: od, OpozvanoAt: od.Add(time.Minute), Opozvao: "root"}.Potpisi(privM)
	v.Opoziv = vektorPotpisa{string(op.signedBytes()), op.Potpis, uJSON(t, op)}
	v.OznakaOpoziva = op.Oznaka()

	// certifikat: CN i vjerodajnice u SAN-u. Serijski broj i rok ovise o
	// satu, pa cijeli certifikat nije vektor.
	vj := uJSON(t, struct {
		Clanstvo *Membership `json:"clanstvo,omitempty"`
		Ovlast   *Ovlast     `json:"ovlast,omitempty"`
	}{&clanA, &ovlast})
	v.Vjerodajnice = vj
	cert, err := selfSignedCert(privA, protokol, []byte(vj))
	if err != nil {
		t.Fatal(err)
	}
	x, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	v.Certifikat.CN = x.Subject.CommonName
	v.Certifikat.URI = x.URIs[0].String()
	if got := peerVjerodajnice([][]byte{x.Raw}); string(got) != vj {
		t.Fatalf("vjerodajnice iz certifikata nisu one koje su u njega stavljene:\n%s", got)
	}

	v.ALPN.Signal, v.ALPN.Web, v.ALPN.Proba = alpnSignala(protokol), alpnWeb(protokol), alpnProbe(protokol)

	v.Pronalazenje.Proba = probeText(protokol)
	v.Pronalazenje.Beacon = uJSON(t, Beacon{Protocol: protokol, DeviceID: "admin-1", Name: "Računalo A", ExchangePort: 4710})

	// uparivanje
	v.Uparivanje.Hello = uJSON(t, Identity{Protocol: protokol, DeviceID: "admin-1", Name: "Računalo A", Version: "1.0.0"}.hello())
	obveza := obvezaZa(protokol, brojP)
	v.Uparivanje.Obveza = hex.EncodeToString(obveza)
	v.Uparivanje.PorukaObv = uJSON(t, sasPoruka{Obveza: obveza})
	v.Uparivanje.PorukaBroj = uJSON(t, sasPoruka{Broj: brojS})
	v.Uparivanje.Kod = SASCode(protokol, pubA, pubB, brojP, brojS)
	if SASCode(protokol, pubB, pubA, brojP, brojS) != v.Uparivanje.Kod {
		t.Fatal("kod uparivanja ovisi o redoslijedu ključeva")
	}
	v.Uparivanje.Potvrda = uJSON(t, Confirm{Approved: true, Payload: json.RawMessage(`{"primjer":1}`)})
	// broj pozvanog: 0x80 pa 31 nula
	brojSNulom := make([]byte, velicinaBroja)
	brojSNulom[0] = 0x80
	v.Uparivanje.KodSNulom.BrojPozvanog = hex.EncodeToString(brojSNulom)
	v.Uparivanje.KodSNulom.Kod = SASCode(protokol, pubA, pubB, brojP, brojSNulom)
	if k := v.Uparivanje.KodSNulom.Kod; len(k) != 6 || k[0] != '0' {
		t.Fatalf("kod %q nije od šest znamenki s vodećom nulom", k)
	}

	// omotnica razgovora, kako je piše Conn.Send (json.Encoder dodaje novi red)
	e, err := NewEnvelope("done", struct {
		Applied int `json:"applied"`
	}{3})
	if err != nil {
		t.Fatal(err)
	}
	e.DeviceID = "admin-1"
	v.Omotnica = kaoRedak(t, e)
	v.Odbijenica = kaoRedak(t, Odbijenica(KodZauzet, RazlogZauzet))
	v.Kodovi = map[string]int{
		"opce": int(KodOpce), "nepoznat-kanal": int(KodNepoznatKanal), "zauzet": int(KodZauzet),
		"odbijeno": int(KodOdbijeno), "nije-clan": int(KodNijeClan), "upis-zauzet": int(KodUpisZauzet),
		"bez-tailscalea": int(KodBezTailscalea),
	}
	v.Mogucnosti = NaseMogucnosti()
	v.DogovorSaStarim = Dogovor(NaseMogucnosti(), nil)

	// signal: potpisani poziv i okvir
	p := Potpisi(privA, protokol, VrstaPoziv, TijeloPoruke{
		Sesija: sesija, Za: PublicKeyString(pubB), Sastajaliste: PublicKeyString(pubM), Izdano: vrijeme})
	v.Signal.Prefiks = string(prefiksPoruke(protokol, VrstaPoziv))
	v.Signal.Tijelo = string(p.Tijelo)
	v.Signal.Potpis = base64.StdEncoding.EncodeToString(p.Potpis)
	if !ed25519.Verify(pubA, append(prefiksPoruke(protokol, VrstaPoziv), p.Tijelo...), p.Potpis) {
		t.Fatal("potpis poziva ne vrijedi nad prefiksom i tijelom")
	}
	poruka := okvirSignala(VrstaPoziv, p)
	v.Signal.Poruka = string(poruka)
	v.Signal.Okvir = uOkvir(t, poruka)
	v.Signal.OkvirPulsa = uOkvir(t, okvirSignala(SignalPuls, struct {
		Odgovori bool `json:"odgovori,omitempty"`
	}{true}))
	v.Signal.NajveciOkvir = NajveciOkvir

	// udarci
	v.UdarciA = udarci(t, protokol, sesija, tajnaPoziva, tajnaOdziva, pubA, pubB)
	v.UdarciB = udarci(t, protokol, sesija, tajnaPoziva, tajnaOdziva, pubB, pubA)

	// isti zapisi s imenom koje nosi <, > i &
	const ime = "a<b>&c"
	v.PosebniZnakovi.Ime = ime
	ovlastP, err := mreza.OvlastiOd(ime, pubA, ime, od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v.PosebniZnakovi.Ovlast = vektorPotpisa{string(ovlastP.signedBytes()), ovlastP.Signature, uJSON(t, ovlastP)}
	clanP, err := mreza.AdmitOd(ime, pubA, ime, od, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	v.PosebniZnakovi.Clanstvo = vektorPotpisa{string(clanP.signedBytes()), clanP.Signature, uJSON(t, clanP)}
	if err := clanP.Verify(pubM, pubA, od.Add(time.Minute)); err != nil {
		t.Fatalf("članstvo s posebnim znakovima u imenu ne prolazi provjeru: %v", err)
	}
	v.PosebniZnakovi.Odbijenica = kaoRedak(t, Odbijenica(KodOdbijeno, ime))
	v.PosebniZnakovi.OdbijenicaRazdjelnici = kaoRedak(t, Odbijenica(KodOdbijeno, "a\u2028b\u2029c"))

	// potpis verzije, ključem računala A
	obicna := PodaciVerzije{
		VersionID: "019a2b3c-4d5e-7f00-8000-000000000001", Entity: "users", EntityID: "korisnik-1", NodeID: "admin-1",
		SchemaVersion: 1, CreatedAt: od, Payload: []byte(`{"full_name":"Ana Horvat","username":"ahorvat"}`),
	}
	v.PotpisVerzije.Obicna = vektorPotpisaVerzije(t, privA, protokol, obicna)
	v.PotpisVerzije.Rubovi = rubovePotpisa(t, protokol, privA, privB, obicna)
	v.PotpisVerzije.SadrzajSBrojevima = vektorPotpisaVerzije(t, privA, protokol, PodaciVerzije{
		VersionID: "019a2b3c-4d5e-7f00-8000-000000000003", Entity: "readings", EntityID: "ocitanje-2", NodeID: "admin-1",
		SchemaVersion: 1, CreatedAt: od,
		Payload: []byte("{ \"cm\" : 1.0 ,\n\t\"razina\" : 1e2 ,\n  \"slovo\" : \"\\u0041\" , \"kosa\" : \"a\\/b\" , \"manje\" : \"\\u003C\" }"),
	})
	for _, ostaje := range []string{`:1.0,`, `:1e2,`, `"\u0041"`, `"a\/b"`, `"\u003C"`} {
		if !strings.Contains(v.PotpisVerzije.SadrzajSBrojevima.OblikSadrz, ostaje) {
			t.Fatalf("oblik sadržaja nema %s kako je napisan: %s", ostaje, v.PotpisVerzije.SadrzajSBrojevima.OblikSadrz)
		}
	}
	v.PotpisVerzije.RazdjelnikUPolju = vektorPotpisaVerzije(t, privA, protokol, PodaciVerzije{
		VersionID: "019a2b3c-4d5e-7f00-8000-000000000004", Entity: "readings", EntityID: "ocitanje\u20283", NodeID: "admin-1",
		Channel: "ocitanja\u202816", SchemaVersion: 1, CreatedAt: od, Payload: []byte(`{"cm":312}`),
	})
	if b := v.PotpisVerzije.RazdjelnikUPolju.Bajtovi; !strings.Contains(b, `"ocitanje\u20283"`) || !strings.Contains(b, `"ocitanja\u202816"`) {
		t.Fatalf("U+2028 u potpisanim bajtovima nije zapisan kao \\u2028: %s", b)
	}
	v.PotpisVerzije.PosebniZnakovi = vektorPotpisaVerzije(t, privA, protokol, PodaciVerzije{
		VersionID: "019a2b3c-4d5e-7f00-8000-000000000002", Entity: "readings", EntityID: "ocitanje-1", NodeID: ime,
		Supersedes: "019a2b3c-4d5e-7f00-8000-000000000001", Archived: true, Channel: "ocitanja/16/2026",
		SchemaVersion: 2, CreatedAt: od.Add(1500 * time.Millisecond),
		Payload: []byte("{ \"napomena\": \"a<b>&c\u2028d\",\n  \"cm\": 312 }"),
	})
	return v
}

func udarci(t *testing.T, protokol, sesija string, tajnaPoziva, tajnaOdziva []byte, moj, drugi ed25519.PublicKey) vektorUdarca {
	t.Helper()
	k, err := NoviKljuceviUdarca(protokol, sesija, tajnaPoziva, tajnaOdziva, moj, drugi)
	if err != nil {
		t.Fatal(err)
	}
	return vektorUdarca{
		Smjer:       k.smjer,
		KljucSlanja: hex.EncodeToString(k.saljem[:]),
		Udarac7:     hex.EncodeToString(k.sastavi(vrstaUdarac, 7)),
		Odgovor7:    hex.EncodeToString(k.sastavi(vrstaOdgovor, 7)),
	}
}

func uJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// kaoRedak zapiše v kako ga piše Conn.Send: json.Encoder, s novim retkom
func kaoRedak(t *testing.T, v any) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func uOkvir(t *testing.T, sadrzaj []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := PisiOkvir(&b, sadrzaj); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b.Bytes())
}

func zapisVektora(t *testing.T, v any) []byte {
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

func TestVektoriZice(t *testing.T) {
	if izlaz := os.Getenv("SYNCNET_VEKTORI_IZLAZ"); izlaz != "" {
		protokol := os.Getenv("SYNCNET_VEKTORI_PROTOKOL")
		if protokol == "" {
			protokol = "gocop"
		}
		if err := os.WriteFile(izlaz, zapisVektora(t, izradiVektore(t, protokol)), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	prvi := zapisVektora(t, izradiVektore(t, "gocop"))
	if drugi := zapisVektora(t, izradiVektore(t, "gocop")); !bytes.Equal(prvi, drugi) {
		t.Fatal("isti ulaz dao je dva različita zapisa vektora")
	}
	if os.Getenv("SYNCNET_VEKTORI_PISI") == "1" {
		if err := os.MkdirAll(filepath.Dir(datotekaVektora), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(datotekaVektora, prvi, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	zapisano, err := os.ReadFile(datotekaVektora)
	if err != nil {
		t.Fatalf("zapisa vektora nema (%v); prvi put ga zapiši uz SYNCNET_VEKTORI_PISI=1", err)
	}
	if !bytes.Equal(prvi, zapisano) {
		t.Fatalf("vektori žice više nisu isti kao u %s: nešto što ide žicom ili se potpisuje promijenilo je oblik", datotekaVektora)
	}
}
