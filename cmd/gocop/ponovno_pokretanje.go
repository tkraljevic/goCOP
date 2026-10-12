package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"gocop/internal/config"
	"gocop/internal/web"
)

// VarijablaNadzora je varijabla okoline kojom usluga sustava (gocop.service)
// i spremnik (Dockerfile) javljaju čvoru tko ga nadzire: systemd ili docker
const VarijablaNadzora = "GOCOP_NADZOR"

// nacinRada kaže tko je pokrenuo čvor, a time i hoće li ga netko podići kad
// izađe. Postava to javi zastavicom -upravitelj, usluga sustava i spremnik
// varijablom GOCOP_NADZOR. Stariji ih odaju sami: systemd daje INVOCATION_ID
// procesu kojem je roditelj (PID 1), a Docker i Podman ostave datoteku u
// korijenu. Sve ostalo je ručno pokrenut čvor.
func nacinRada(podPostavom bool, env func(string) string, postoji func(string) bool, roditelj int) web.NacinRada {
	switch {
	case podPostavom:
		return web.PodPostavom
	case env(VarijablaNadzora) == "systemd":
		return web.UslugaSustava
	case env(VarijablaNadzora) == "docker", postoji("/.dockerenv"), postoji("/run/.containerenv"):
		return web.USpremniku
	case env("INVOCATION_ID") != "" && roditelj == 1:
		return web.UslugaSustava
	}
	return web.RucnoPokrenut
}

func postojiDatoteka(put string) bool {
	_, err := os.Stat(put)
	return err == nil
}

// cekajKraj čeka da čvor stane: signal sustava (ctx), Postava (zatvoren
// ulaz) ili administrator koji traži ponovno pokretanje. Vraća izlazni kod:
// 0, ili web.KodPonovnogPokretanja, da ga onaj tko ga je pokrenuo podigne.
func cekajKraj(ctx context.Context, stop <-chan os.Signal, ponovno <-chan struct{}) int {
	select {
	case <-ctx.Done():
	case <-stop:
	case <-ponovno:
		log.Printf("Ponovno pokretanje: čvor se gasi i izlazi s kodom %d, da ga pokrene ono što ga je pokrenulo", web.KodPonovnogPokretanja)
		return web.KodPonovnogPokretanja
	}
	return 0
}

// zatraziPonovno je funkcija kojom web traži izlazak; drugi zahtjev dok
// prvi još čeka ne mijenja ništa
func zatraziPonovno(ponovno chan<- struct{}) func() {
	return func() {
		select {
		case ponovno <- struct{}{}:
		default:
		}
	}
}

// jaceZastavice: postavke koje zastavica pokretanja zadaje umjesto datoteke,
// s imenom zastavice (kao u odrediPostavke)
func jaceZastavice(z zastavice) map[string]string {
	out := map[string]string{}
	dodaj := func(kljuc, zastavica string, vrijedi bool) {
		if vrijedi {
			out[kljuc] = "-" + zastavica
		}
	}
	dodaj("addr", "addr", z.addr != "")
	dodaj("db", "db", z.db != "")
	dodaj("arhiva", "arhiva", z.arhiva != "")
	dodaj("skenovi", "skenovi", z.skenovi != "")
	dodaj("podaci", "podaci", z.zadane["podaci"])
	dodaj("pakete", "pakete", z.zadane["pakete"])
	dodaj("node.id", "node", z.node != "")
	dodaj("node.name", "name", z.name != "")
	dodaj("sync.exchange_port", "sync-port", z.syncPort >= 0)
	dodaj("sync.pair_port", "pair-port", z.pairPort >= 0)
	dodaj("sync.discovery_port", "discovery-port", z.discoveryPort >= 0)
	dodaj("sync.auto_sync", "auto-sync", z.autoSync != "")
	return out
}

// postavkeZaPosluzitelj: koju datoteku čvor čita (ili bi čitao kad nastane),
// s čime radi i od čega bi nastala nova datoteka
func postavkeZaPosluzitelj(cfg config.Config, cfgFrom string, z zastavice) web.PostavkeCvora {
	put := cfgFrom
	if put == "" {
		put = z.configPath
	}
	if put == "" {
		put = filepath.Join(filepath.Dir(cfg.DB), config.FileName)
	}
	vrijedi := cfg
	if z.zadane["podaci"] {
		vrijedi.Podaci = z.podaci
	}
	if z.zadane["pakete"] {
		vrijedi.Pakete = z.paketi
	}
	pocetna := config.Default()
	pocetna.Node.ID = cfg.Node.ID
	return web.PostavkeCvora{Put: put, Vrijedi: vrijedi, Zastavice: jaceZastavice(z), Pocetna: pocetna}
}
