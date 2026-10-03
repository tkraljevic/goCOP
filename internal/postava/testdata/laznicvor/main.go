// Lažni čvor za testove Postave: drži se ugovora (-version, -upravitelj,
// /zdravlje) i ništa drugo. Izdanje i ponašanje zadaju se pri prevođenju:
//
//	-ldflags "-X main.izdanje=0.0.28-alfa -X main.nacin=pada"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var (
	izdanje = "0.0.0-alfa"
	nacin   = "radi" // radi | pada (izađe odmah nakon pokretanja)
)

func main() {
	verzija := flag.Bool("version", false, "")
	_ = flag.Bool("upravitelj", false, "")
	db := flag.String("db", "", "")
	flag.Parse()
	if *verzija {
		fmt.Println("goCOP " + izdanje)
		return
	}
	if nacin == "pada" {
		fmt.Println("lažni čvor: padam")
		os.Exit(3)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(*db), "gocop.toml"))
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	m := regexp.MustCompile(`addr\s*=\s*"([^"]+)"`).FindSubmatch(b)
	if m == nil {
		os.Exit(2)
	}
	http.HandleFunc("/zdravlje", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"izdanje": izdanje, "radi": true})
	})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}()
	fmt.Println(http.ListenAndServe(string(m[1]), nil))
	os.Exit(1)
}
