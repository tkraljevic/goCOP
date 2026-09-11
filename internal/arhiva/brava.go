package arhiva

// Brava nad arhivom: jedan posao u jednom trenutku.
//
// Gradnja letve, ugradnja paketa, micanje niza i izdavanje diraju istu bazu i
// istu mapu izdanja. Dvoje istodobno može:
//   - presresti se usred gradnje, koja niz briše pa upisuje iznova;
//   - izgubiti katalog, jer ga oba pročitaju pa oba zapišu;
//   - pregaziti privremenu datoteku, jer oba izdavanja pišu isto *.novo ime;
//   - izdati krivi otisak, ako izdavanje pročita arhivu usred gradnje i taj
//     polovičan sadržaj zapiše u katalog kao pravo izdanje.
//
// Brava je DATOTEKA, ne mutex u programu. Naredbeni redak i poslužitelj su dva
// procesa: paket-arhive se pokreće iz terminala dok program radi, pa brava u
// memoriji ne bi vidjela drugu stranu.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ImeBrave je datoteka koja stoji uz arhivu dok posao traje.
const ImeBrave = ".brava"

// stanjeBrave je ono što piše u njoj.
type stanjeBrave struct {
	Sto string    `json:"sto"` // čime se posao bavi
	Tko string    `json:"tko"` // čvor ili čovjek
	PID int       `json:"pid"`
	Od  time.Time `json:"od"`
}

// Brava je uzeta brava; pušta se s Pusti.
type Brava struct {
	put      string
	preuzeta *stanjeBrave // ako je zatečena brava bila mrtva, tko ju je držao
}

// Preuzeta javlja je li brava oduzeta procesu kojeg više nema, i kome.
func (b *Brava) Preuzeta() (bool, string) {
	if b == nil || b.preuzeta == nil {
		return false, ""
	}
	return true, fmt.Sprintf("%s (%s, pid %d, od %s)", b.preuzeta.Sto, b.preuzeta.Tko,
		b.preuzeta.PID, b.preuzeta.Od.In(time.Local).Format("15:04:05"))
}

// Zauzeto je greška koja kaže tko drži bravu.
type Zauzeto struct {
	Sto string
	Tko string
	PID int
	Od  time.Time
}

func (z *Zauzeto) Error() string {
	return fmt.Sprintf("arhiva je zauzeta: %s (%s, pokrenuo pid %d u %s)",
		z.Sto, z.Tko, z.PID, z.Od.In(time.Local).Format("15:04:05"))
}

// Uzmi uzima bravu uz zadanu datoteku ili mapu.
//
// Ne čeka. Posao koji naiđe na zauzeto mora to javiti čovjeku, a ne tiho stati
// u red: red se ne vidi, pa izgleda kao da se ništa ne događa.
func Uzmi(uz, sto, tko string) (*Brava, error) {
	put := putBrave(uz)
	b := &Brava{put: put}

	f, err := os.OpenFile(put, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		staro, procitana := procitajBravu(put)
		if procitana && ziv(staro.PID) {
			return nil, &Zauzeto{Sto: staro.Sto, Tko: staro.Tko, PID: staro.PID, Od: staro.Od}
		}
		// Proces kojeg više nema ne drži ništa. Brava se preuzima, ali se
		// zapisuje čija je bila — prekinuti posao je mogao ostaviti nered.
		if procitana {
			b.preuzeta = staro
		}
		if err := os.Remove(put); err != nil {
			return nil, err
		}
		f, err = os.OpenFile(put, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return b, json.NewEncoder(f).Encode(stanjeBrave{
		Sto: sto, Tko: tko, PID: os.Getpid(), Od: time.Now(),
	})
}

// Pusti otpušta bravu. Puštanje brave koje nema nije greška: posao je gotov
// tako ili tako.
func (b *Brava) Pusti() {
	if b == nil || b.put == "" {
		return
	}
	_ = os.Remove(b.put)
	b.put = ""
}

// putBrave stavlja bravu uz datoteku ili u mapu.
func putBrave(uz string) string {
	if st, err := os.Stat(uz); err == nil && st.IsDir() {
		return filepath.Join(uz, ImeBrave)
	}
	return uz + ImeBrave
}

func procitajBravu(put string) (*stanjeBrave, bool) {
	b, err := os.ReadFile(put)
	if err != nil {
		return nil, false
	}
	var s stanjeBrave
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, false
	}
	return &s, true
}

// ziv javlja radi li proces s tim brojem.
//
// Signal 0 ne šalje ništa nego samo provjerava; to je isti postupak kojim se i
// rukom gleda "ps -p". Brava bez broja procesa drži se živom, jer se o njoj ne
// zna dovoljno da bi se oduzela.
func ziv(pid int) bool {
	if pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
