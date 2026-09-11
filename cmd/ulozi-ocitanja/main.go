// ulozi-ocitanja pretvara operativna očitanja u arhivski niz.
//
// Očitanje u knjizi verzija stoji 1.186 bajta: redak, kazala i verzija. Ista
// vrijednost u arhivi stoji tri. Dvadeset i dvije letve sa satnim očitanjima
// znače 230 MB godišnje u knjizi naspram 600 kB u arhivi — bez ulaganja baza s
// vremenom postane neupotrebljiva.
//
// Ulaže se u stablo s datotekama, ne izravno u arhivsku bazu: ondje su podaci
// obnovljivi i arhiva se iz njih uvijek može izgraditi iznova. To je i jedina
// mreža koja brisanje čini sigurnim.
//
// Ulaganje i zaboravljanje su dva koraka. Uloženo se ne briše dok se ne
// provjeri da je doista u stablu i da se arhiva iz njega izgradila.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	_ "modernc.org/sqlite"
)

func main() {
	dbPath := flag.String("db", "data/gocop.db", "putanja do baze programa")
	arhivaPut := flag.String("arhiva", "data/vodostaji.db", "arhivska baza")
	koren := flag.String("podaci", "vodostaji", "stablo s izvornim datotekama arhive")
	nodeID := flag.String("node", "cop-osijek-node", "oznaka čvora")
	sifra := flag.String("letva", "", "šifra postaje, npr. vukovar")
	odS := flag.String("od", "", "od datuma, YYYY-MM-DD")
	doS := flag.String("do", "", "do datuma, YYYY-MM-DD (uključivo)")
	izvor := flag.String("izvor", "cop", "pod kojim izvorom se ulaže ono što je stiglo dojavom")
	izvorRucno := flag.String("izvor-rucno", "cop-rucno", "pod kojim izvorom se ulaže ono što je čovjek očitao na letvi")
	vrsta := flag.String("vrsta", "", "vrsta niza; prazno znači pogodi iz gustoće očitanja")
	izdanje := flag.String("izdanje", "", "oznaka izdanja koja se upisuje uz uloženo očitanje")
	zaboravi := flag.Bool("zaboravi", false, "obriši uložena očitanja i njihove verzije")
	suho := flag.Bool("probno", false, "samo javi što bi se dogodilo")
	flag.Parse()

	if *sifra == "" || *odS == "" || *doS == "" {
		log.Fatal("zadajte -letva, -od i -do")
	}
	od, err := time.ParseInLocation("2006-01-02", *odS, time.UTC)
	if err != nil {
		log.Fatalf("-od: %v", err)
	}
	do, err := time.ParseInLocation("2006-01-02", *doS, time.UTC)
	if err != nil {
		log.Fatalf("-do: %v", err)
	}
	do = do.AddDate(0, 0, 1).Add(-time.Second)

	baza, err := db.OpenDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer baza.Close()
	ctx := context.Background()

	postaja, err := postajaPoSifri(ctx, baza, *sifra)
	if err != nil {
		log.Fatal(err)
	}
	letve, err := arhiva.Letve(*koren)
	if err != nil {
		log.Fatal(err)
	}
	sliv := letve[postaja.Code]
	if sliv == "" {
		log.Fatalf("letva %q nije u stablu %s — ondje se ulaže, pa mora imati mjesto", postaja.Code, *koren)
	}

	ocitanja, err := ocitanjaZaUlaganje(ctx, baza, postaja.ID.String(), od, do)
	if err != nil {
		log.Fatal(err)
	}
	if len(ocitanja) == 0 {
		fmt.Println("nema očitanja za ulaganje u tom razdoblju")
		return
	}

	iz := razvrstaj(ocitanja)
	fmt.Printf("%s (%s), %s – %s\n", postaja.Name, postaja.Code, *odS, *doS)
	fmt.Printf("  očitanja:      %d\n", len(ocitanja))
	fmt.Printf("  dojavljeno:    %d  → %s\n", len(iz.mjereno), *izvor)
	if len(iz.rucno) > 0 {
		fmt.Printf("  ručno s letve: %d  → %s\n", len(iz.rucno), *izvorRucno)
	}
	if len(iz.preracunato) > 0 {
		fmt.Printf("  rekonstruirano:%d  → ulaže se odvojeno, kao preracun-%s\n", len(iz.preracunato), *izvor)
	}
	if iz.sumnjivo > 0 {
		fmt.Printf("  sumnjivo:      %d  → NE ulaže se; ostaje u operativi\n", iz.sumnjivo)
	}
	if iz.bezVrijednosti > 0 {
		fmt.Printf("  bez vodostaja: %d  → NE ulaže se (građevine, prazna očitanja)\n", iz.bezVrijednosti)
	}
	fmt.Printf("  s bilješkom:   %d  → prelazi u bilješke uz vrijednost\n", len(iz.biljeske))

	odabranaVrsta := *vrsta
	if odabranaVrsta == "" {
		odabranaVrsta = pogodiVrstu(iz.mjereno)
	}
	// Gdje niz već postoji, ulaže se u njega. Pogađanje iz gustoće vrijedi samo
	// kad se ulaže na prazno: jedno jedino očitanje izgleda kao jutarnje, pa bi
	// razdvojilo izvor na dva niza i vrijednost bi pala u dnevni umjesto u
	// satni — što je provjera i uhvatila.
	if v := zatecenaVrsta(*arhivaPut, postaja.Code, *izvor); v != "" {
		odabranaVrsta = v
	}
	vrstaRucnog := odabranaVrsta
	if v := zatecenaVrsta(*arhivaPut, postaja.Code, *izvorRucno); v != "" {
		vrstaRucnog = v
	}
	fmt.Printf("  vrsta niza:    %s\n", odabranaVrsta)

	if *suho {
		fmt.Println("\nproba — ništa nije zapisano; ponovite bez -probno")
		return
	}

	if len(iz.mjereno) > 0 {
		put, err := arhiva.Dopuni(*koren, sliv, postaja.Code, *izvor, "vodostaj", odabranaVrsta, iz.mjereno)
		if err != nil {
			log.Fatalf("ulaganje: %v", err)
		}
		fmt.Printf("\nzapisano: %s\n", put)
	}
	if len(iz.rucno) > 0 {
		// Ručno očitanje ide kao satni niz, iako nije satno: arhiva u satnom
		// nizu drži trenutke, a ne pune sate — VITUKI ondje stoji u 03:30.
		// Kao "jutarnji" bi palo u dnevni niz i izgubilo svoj sat, a 8. rujna
		// su na Batini tri očitanja u danu: 05, 13 i 21 h.
		put, err := arhiva.Dopuni(*koren, sliv, postaja.Code, *izvorRucno, "vodostaj", vrstaRucnog, iz.rucno)
		if err != nil {
			log.Fatalf("ulaganje ručnih: %v", err)
		}
		fmt.Printf("zapisano: %s\n", put)
		if err := upisiIzvorRucnog(*arhivaPut, *izvorRucno); err != nil {
			log.Fatalf("izvor %s: %v", *izvorRucno, err)
		}
	}
	if len(iz.preracunato) > 0 {
		p2, err := arhiva.Dopuni(*koren, sliv, postaja.Code, "preracun-"+*izvor, "vodostaj",
			odabranaVrsta, iz.preracunato)
		if err != nil {
			log.Fatalf("ulaganje rekonstruiranog: %v", err)
		}
		fmt.Printf("zapisano: %s\n", p2)
	}

	fmt.Println("gradim letvu iz stabla…")
	izvjestaj, err := arhiva.Izgradi(*koren, *arhivaPut, postaja.Code, os.Stdout)
	if err != nil {
		log.Fatalf("gradnja: %v", err)
	}
	fmt.Printf("nizova %d, očitanja %d, spojenih %d\n",
		izvjestaj.Nizova, izvjestaj.Ocitanja, izvjestaj.Spojenih)

	// Provjera prije ikakvog brisanja: je li svako uloženo očitanje doista u
	// arhivi. Mjerenje se ne može ponoviti, pa se ne vjeruje na riječ.
	svi := append(append([]arhiva.Redak{}, iz.mjereno...), iz.rucno...)
	nedostaje, err := provjeriUArhivi(*arhivaPut, postaja.Code, svi)
	if err != nil {
		log.Fatal(err)
	}
	if nedostaje > 0 {
		log.Fatalf("PROVJERA PALA: %d od %d vrijednosti nije u arhivi — ništa se ne briše",
			nedostaje, len(svi))
	}
	fmt.Printf("provjera: svih %d vrijednosti je u arhivi\n", len(svi))

	rec := ledger.New(baza, *nodeID)
	if len(iz.biljeske) > 0 {
		bilRepo := repository.NewBiljeskaRepository(baza, rec)
		n, err := bilRepo.Spremi(ctx, biljeskeZa(postaja.Code, vrstaRucnog, iz.biljeske))
		if err != nil {
			log.Fatalf("bilješke: %v", err)
		}
		fmt.Printf("bilješki uz vrijednosti: %d\n", n)
	}

	oznaka := *izdanje
	if oznaka == "" {
		oznaka = time.Now().UTC().Format("2006-01-02")
	}
	n, err := oznaciUlozeno(ctx, baza, iz.ulozeniID, oznaka)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("označeno kao uloženo (%s): %d očitanja\n", oznaka, n)

	if !*zaboravi {
		fmt.Println("\nuloženo je, ali ništa nije obrisano. Za pospremanje ponovite s -zaboravi")
		return
	}
	obrisano, verzija, err := zaboraviUlozena(ctx, baza, iz.ulozeniID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("obrisano: %d očitanja i %d verzija\n", obrisano, verzija)
	fmt.Println("prostor se vraća tek nakon VACUUM (Administracija → Održavanje baze)")
}

type razvrstano struct {
	mjereno        []arhiva.Redak
	rucno          []arhiva.Redak
	preracunato    []arhiva.Redak
	biljeske       []ocitanjeSBiljeskom
	ulozeniID      []string
	sumnjivo       int
	bezVrijednosti int
}

type ocitanjeSBiljeskom struct {
	Vrijeme time.Time
	Vrsta   string
	Tekst   string
	Tko     string
}

// razvrstaj dijeli očitanja po tome smiju li i kako u arhivu. Sumnjivo se ne
// ulaže: arhiva nema mjesto za sumnju po vrijednosti, pa bi ondje izgledalo
// kao mjerenje.
func razvrstaj(o []models.Reading) razvrstano {
	var iz razvrstano
	for _, r := range o {
		if r.LevelCm == nil {
			iz.bezVrijednosti++
			continue
		}
		if r.Quality == models.QualityUncertain {
			iz.sumnjivo++
			continue
		}
		red := arhiva.Redak{Vrijeme: r.MeasuredAt.UTC(), Vrijednost: float64(*r.LevelCm)}
		switch {
		case r.Quality == models.QualityReconstructed:
			iz.preracunato = append(iz.preracunato, red)
		case r.Source == models.ReadingSourceManual:
			// Čovjek pred letvom nije dojava. Podrijetlo se ne smije stopiti:
			// pri maloj vodi je ručno očitanje jedina neovisna provjera onoga
			// što telemetrija javlja.
			iz.rucno = append(iz.rucno, red)
		default:
			iz.mjereno = append(iz.mjereno, red)
		}
		if r.Note != "" || r.VrstaBiljeske != "" {
			iz.biljeske = append(iz.biljeske, ocitanjeSBiljeskom{
				Vrijeme: r.MeasuredAt.UTC(), Vrsta: r.VrstaBiljeske, Tekst: r.Note, Tko: r.Observer})
		}
		iz.ulozeniID = append(iz.ulozeniID, r.ID.String())
	}
	return iz
}

// zatecenaVrsta javlja pod kojom vrstom taj izvor već stoji u arhivi. Prazno
// znači da ga ondje nema, pa se vrsta tek bira.
func zatecenaVrsta(arhivaPut, letva, izvor string) string {
	db, err := sql.Open("sqlite", arhivaPut+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()
	var v string
	err = db.QueryRow(`SELECT vrsta FROM nizovi WHERE letva=? AND izvor=? AND velicina='vodostaj'
		ORDER BY zapisa DESC LIMIT 1`, letva, izvor).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}

// pogodiVrstu bira vrstu niza po gustoći očitanja. Jedno dnevno je jutarnje
// očitanje, više od toga je satni niz.
func pogodiVrstu(r []arhiva.Redak) string {
	if len(r) < 2 {
		return "jutarnji"
	}
	sort.Slice(r, func(a, b int) bool { return r[a].Vrijeme.Before(r[b].Vrijeme) })
	raspon := r[len(r)-1].Vrijeme.Sub(r[0].Vrijeme).Hours()
	if raspon <= 0 {
		return "jutarnji"
	}
	poDanu := float64(len(r)) / (raspon / 24)
	if poDanu > 1.5 {
		return "satni"
	}
	return "jutarnji"
}

func biljeskeZa(letva, vrsta string, o []ocitanjeSBiljeskom) []models.ArhivaBiljeska {
	korak := "satni"
	if vrsta != "satni" {
		korak = "dnevni"
	}
	out := make([]models.ArhivaBiljeska, 0, len(o))
	for _, b := range o {
		out = append(out, models.ArhivaBiljeska{
			Letva: letva, Velicina: "vodostaj", Korak: korak,
			Vrijeme: b.Vrijeme, Vrsta: b.Vrsta, Tekst: b.Tekst, Tko: b.Tko,
		})
	}
	return out
}

// upisiIzvorRucnog otvara mjesto ručnom očitanju u tablici izvora, uključeno.
// Nepoznat izvor inače ulazi isključen i čeka odluku — ali za ono što je naš
// čovjek očitao na letvi odluka je već donesena time što je upisano.
func upisiIzvorRucnog(arhivaPut, naziv string) error {
	db, err := sql.Open("sqlite", arhivaPut)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT OR IGNORE INTO izvori (naziv, tocnost, red, ukljucen, napomena)
		VALUES (?, 1, 15, 1, 'očitanje s letve, upisano u programu; ispred telemetrije jer
			je čovjek pred letvom jedina neovisna provjera onoga što mjerilo javlja')`, naziv)
	return err
}

func postajaPoSifri(ctx context.Context, baza *sql.DB, sifra string) (models.Station, error) {
	var st models.Station
	var id string
	err := baza.QueryRowContext(ctx, `SELECT id, code, name FROM stations WHERE lower(code)=lower(?)`,
		sifra).Scan(&id, &st.Code, &st.Name)
	if err == sql.ErrNoRows {
		return st, fmt.Errorf("postaja %q nije u registru", sifra)
	}
	if err != nil {
		return st, err
	}
	st.ID, err = uuid.Parse(id)
	return st, err
}

func ocitanjaZaUlaganje(ctx context.Context, baza *sql.DB, stationID string,
	od, do time.Time) ([]models.Reading, error) {
	rows, err := baza.QueryContext(ctx, `
		SELECT id, measured_at, level_cm, quality, source, observer, note, vrsta_biljeske, izdanje
		FROM readings WHERE station_id = ? AND measured_at BETWEEN ? AND ?
		ORDER BY measured_at`, stationID, od.UTC(), do.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Reading
	for rows.Next() {
		var r models.Reading
		var id string
		var level sql.NullInt64
		if err := rows.Scan(&id, &r.MeasuredAt, &level, &r.Quality, &r.Source, &r.Observer,
			&r.Note, &r.VrstaBiljeske, &r.Izdanje); err != nil {
			return nil, err
		}
		if r.Izdanje != "" {
			continue // već uloženo
		}
		if level.Valid {
			v := int(level.Int64)
			r.LevelCm = &v
		}
		r.ID, _ = uuid.Parse(id)
		out = append(out, r)
	}
	return out, rows.Err()
}

func provjeriUArhivi(put, letva string, redci []arhiva.Redak) (int, error) {
	a, err := sql.Open("sqlite", put+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer a.Close()
	nedostaje := 0
	for _, r := range redci {
		var n int
		if err := a.QueryRow(`SELECT count(*) FROM spoj WHERE letva=? AND velicina='vodostaj'
			AND vrijeme=?`, letva, r.Vrijeme.Unix()).Scan(&n); err != nil {
			return nedostaje, err
		}
		if n == 0 {
			nedostaje++
		}
	}
	return nedostaje, nil
}

func oznaciUlozeno(ctx context.Context, baza *sql.DB, ids []string, oznaka string) (int, error) {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `UPDATE readings SET izdanje=?, updated_at=? WHERE id=?`,
			oznaka, time.Now().UTC(), id)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, tx.Commit()
}

// zaboraviUlozena briše očitanja i njihove verzije. Radi se tek nakon što je
// provjereno da su u arhivi, i samo nad onima koji su označeni izdanjem.
func zaboraviUlozena(ctx context.Context, baza *sql.DB, ids []string) (int, int, error) {
	tx, err := baza.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var ocitanja, verzija int
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `DELETE FROM readings WHERE id=? AND izdanje<>''`, id)
		if err != nil {
			return ocitanja, verzija, err
		}
		k, _ := res.RowsAffected()
		if k == 0 {
			continue // nije označeno kao uloženo; ne dira se
		}
		ocitanja += int(k)
		res, err = tx.ExecContext(ctx,
			`DELETE FROM record_versions WHERE entity='readings' AND entity_id=?`, id)
		if err != nil {
			return ocitanja, verzija, err
		}
		k, _ = res.RowsAffected()
		verzija += int(k)
	}
	return ocitanja, verzija, tx.Commit()
}
