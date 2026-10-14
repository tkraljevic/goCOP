package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/disk"
	"gocop/internal/ledger"
	"gocop/internal/peers"
	"gocop/internal/prognoza"
	"gocop/internal/sadrzaj"
	"gocop/internal/web"
)

// cvorPlocice je čvor s glavnom bazom, knjigom, razmjenom i bazom prognoza
func cvorPlocice(t *testing.T) (*sql.DB, *ledger.Recorder, *peers.Service, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gocop.db")
	baza, err := db.OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	rec := ledger.New(baza, "pperic-thinkpad")
	cvor, err := peers.LoadNode(dbPath, "pperic-thinkpad", "Probni", "test")
	if err != nil {
		t.Fatal(err)
	}
	mreza, err := peers.NewService(baza, rec, cvor, peers.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	mreza.PostaviProgram("0.0.38-alfa", "")
	pb, err := prognoza.Otvori(filepath.Join(dir, "prognoze.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pb.Close() })
	return baza, rec, mreza, pb
}

// stalanMjerac javlja isti disk za svaku mapu
type stalanMjerac disk.Mjera

func (m stalanMjerac) Izmjeri(string) (disk.Mjera, error) { return disk.Mjera(m), nil }

func nadzorMaloMjesta(t *testing.T) *disk.Nadzor {
	t.Helper()
	n := disk.NoviNadzor(stalanMjerac{Disk: "sda", Ukupno: 128e9, Slobodno: 8_290_000_000}, t.Logf,
		mjestaDiska("/data/gocop.db", "/data/vodostaji.db", "", "")...)
	n.Provjeri()
	return n
}

// upozorenjeDiska je disk iz nadzorMaloMjesta na pločici čvora „Probni”
var upozorenjeDiska = web.DiskRazmjene{
	Poruka: "Na disku čvora Probni s bazom i arhivom ostalo je 8,2 GB (6 % diska).",
	Savjet: "Oslobodite mjesto ili premjestite arhivu na drugi disk.",
}

// Čvor bez mreže: pločica kaže da nije u mreži i ipak nosi upozorenje o disku
func TestStanjeRazmjeneBezMreze(t *testing.T) {
	_, rec, mreza, _ := cvorPlocice(t)
	st := stanjeRazmjene(context.Background(), mreza, rec, nil, nil, nil, nadzorMaloMjesta(t))
	if st.UMrezi || st.Verzija != "0.0.38-alfa" || len(st.Cvorovi) != 0 {
		t.Errorf("bez mreže: %+v", st)
	}
	if len(st.Disk) != 1 || st.Disk[0] != upozorenjeDiska {
		t.Errorf("disk: %+v", st.Disk)
	}
	if st.Prognoza.Ima || st.Prognoza.Izdaje {
		t.Errorf("prognoza bez izdanja: %+v", st.Prognoza)
	}
}

// Čvor u mreži: upareni čvorovi s nazivom ili imenom, dostupnost, zaostatak,
// zapisi koje program ne razumije, izdavač prognoze; bez nadzora diska nema
// upozorenja o disku
func TestStanjeRazmjeneUMrezi(t *testing.T) {
	ctx := context.Background()
	baza, rec, mreza, pb := cvorPlocice(t)
	if err := mreza.CreateNetwork(ctx, "Proba"); err != nil {
		t.Fatal(err)
	}
	if err := mreza.PostaviUloge(ctx, peers.Uloge{Izdaje: true}); err != nil {
		t.Fatal(err)
	}
	sad := time.Now().UTC()
	if _, err := baza.Exec(`INSERT INTO peers (node_id, name, public_key, created_at) VALUES ('laptop-ana', '', 'k1', ?), ('cop-ured', 'COP Ured', 'k2', ?)`, sad, sad); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`INSERT INTO peer_sync (node_id, their_frontier, last_ok, last_error, applied, sent, fails)
		VALUES ('laptop-ana', '{"treci-cvor":"v-zzz"}', ?, 'dial tcp: no route to host', 3, 2, 1)`, sad); err != nil {
		t.Fatal(err)
	}
	// zapis novije sheme, kakav šalje noviji program
	if _, err := rec.Apply(ctx, []ledger.Version{{VersionID: "v-novi", Entity: "proba_novije_sheme", EntityID: "z-1", NodeID: "cop-ured",
		Payload: []byte(`{}`), CreatedAt: sad, SchemaVersion: ledger.SchemaVersion + 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pb.Exec(`INSERT INTO izdanja (izdano, verzija, nastalo, cvor) VALUES (?, 1, ?, 'cop-osijek')`, sad.Unix()/3600, sad.Unix()); err != nil {
		t.Fatal(err)
	}
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	ra := &razmjenaArhive{rec: rec, arhivaPut: filepath.Join(t.TempDir(), "vodostaji.db"), javljeno: map[string]bool{}}

	st := stanjeRazmjene(ctx, mreza, rec, sp, ra, pb, nil)
	if !st.UMrezi || st.Disk != nil {
		t.Errorf("u mreži: UMrezi %v, disk %+v", st.UMrezi, st.Disk)
	}
	po := map[string]web.CvorRazmjene{}
	for _, c := range st.Cvorovi {
		po[c.Naziv] = c
	}
	ured, laptop := po["COP Ured"], po["laptop-ana"]
	if len(st.Cvorovi) != 2 || len(po) != 2 {
		t.Fatalf("čvorovi: %+v", st.Cvorovi)
	}
	if ured.Naziv != "COP Ured" || ured.Dostupnost != "never" || ured.JosPrima {
		t.Errorf("ured: %+v", ured)
	}
	if laptop.Naziv != "laptop-ana" || laptop.Dostupnost != "online" || !laptop.JosPrima || laptop.Primljeno != 3 || laptop.Poslano != 2 ||
		laptop.Neuspjelih != 1 || !laptop.SamoDolazi || laptop.Greska == "" || laptop.Zadnja == nil {
		t.Errorf("laptop: %+v", laptop)
	}
	nerazumljivo := false
	for _, u := range st.Upozorenja {
		nerazumljivo = nerazumljivo || strings.Contains(u, "ne razumije do kraja (proba_novije_sheme")
	}
	if !nerazumljivo {
		t.Errorf("upozorenja bez zapisa novije sheme: %q", st.Upozorenja)
	}
	if p := st.Prognoza; !p.Izdaje || !p.Ima || p.Izdavac != "cop-osijek" || p.Primljeno || p.Nastalo.Unix() != sad.Unix() {
		t.Errorf("prognoza: %+v", p)
	}
	if st.Arhiva.UKazalu != 0 || st.SadrzajCeka != 0 {
		t.Errorf("arhiva %+v, sadržaj čeka %d", st.Arhiva, st.SadrzajCeka)
	}
}

// Kad se stanje razmjene ne da pročitati, pločica to kaže, a upozorenje o
// disku ostaje
func TestStanjeRazmjeneKojeSeNeCita(t *testing.T) {
	baza, rec, mreza, _ := cvorPlocice(t)
	baza.Close()
	st := stanjeRazmjene(context.Background(), mreza, rec, nil, nil, nil, nadzorMaloMjesta(t))
	if len(st.Upozorenja) != 1 || !strings.HasPrefix(st.Upozorenja[0], "Stanje razmjene se ne da pročitati: ") {
		t.Errorf("upozorenja: %q", st.Upozorenja)
	}
	if len(st.Disk) != 1 || st.Disk[0] != upozorenjeDiska {
		t.Errorf("disk: %+v", st.Disk)
	}
}

// Na pločici su samo diskovi s malo mjesta, a čvor bez naziva se zove po
// imenu u mreži
func TestUpozorenjaDiska(t *testing.T) {
	dir := t.TempDir()
	baza, arhiva := filepath.Join(dir, "data"), filepath.Join(dir, "arhiva")
	m := mjeracPoMapi{
		baza:   {Disk: "sda", Ukupno: 128e9, Slobodno: 8_290_000_000},
		arhiva: {Disk: "sdb", Ukupno: 4e12, Slobodno: 2e12},
	}
	n := disk.NoviNadzor(m, t.Logf, mjestaDiska(filepath.Join(baza, "gocop.db"), filepath.Join(arhiva, "vodostaji.db"), "", "")...)
	n.Provjeri()
	u := upozorenjaDiska(n, &peers.Node{ID: "pperic-thinkpad"})
	if len(u) != 1 || u[0].Poruka != "Na disku čvora pperic-thinkpad s bazom ostalo je 8,2 GB (6 % diska)." ||
		u[0].Savjet != "Oslobodite mjesto na tom disku." {
		t.Errorf("upozorenja: %+v", u)
	}
	if u := upozorenjaDiska(nil, &peers.Node{ID: "pperic-thinkpad"}); u != nil {
		t.Errorf("bez nadzora: %+v", u)
	}
}

// mjeracPoMapi daje mjeru po mapi
type mjeracPoMapi map[string]disk.Mjera

func (m mjeracPoMapi) Izmjeri(put string) (disk.Mjera, error) { return m[put], nil }
