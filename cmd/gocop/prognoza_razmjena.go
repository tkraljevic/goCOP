package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/prognoza"
)

// Izdanje prognoze putuje razmjenom (vidi prognoza/razmjena.go). Čvor koji
// izdaje prognozu svako izdanje zapiše u knjigu; ostali ga iz knjige upišu u
// svoju bazu prognoza.

// DrziIzdanjaDana je koliko dana izdanja stoje u knjizi za razmjenu. Čvor
// koji je bio isključen dulje dobije samo zadnja; u bazi prognoza ostaju sva.
const DrziIzdanjaDana = 7

// objaviPrognozu zapisuje izdanje u knjigu, a prije njega model, ako se od
// zadnjeg poslanog promijenio.
func objaviPrognozu(ctx context.Context, baza *sql.DB, rec *ledger.Recorder, pb, ob *sql.DB, ishod *prognoza.Ishod, cvor string) error {
	pm, otisak, err := prognoza.SastaviModel(pb)
	if err != nil {
		return err
	}
	zadnji, err := rec.Latest(ctx, prognoza.EntitetModela, prognoza.KljucModela)
	if err != nil && !errors.Is(err, ledger.ErrNoVersion) {
		return err
	}
	var bio prognoza.Omotnica
	if zadnji != nil {
		bio, _ = prognoza.Odmotaj(zadnji.Payload, &struct{}{})
	}
	if bio.Otisak != otisak {
		om, err := prognoza.Zamotaj(prognoza.Omotnica{Cvor: cvor, Otisak: otisak}, pm)
		if err != nil {
			return err
		}
		if _, err := rec.Record(ctx, baza, prognoza.EntitetModela, prognoza.KljucModela, om); err != nil {
			return err
		}
	}
	paket, err := prognoza.SastaviIzdanje(pb, ishod)
	if err != nil {
		return err
	}
	if ob != nil {
		// kiša bez koje izdanje i dalje vrijedi: greška se javi, izdanje ide
		if paket.Oborine, err = prognoza.SastaviOborine(ob, ishod.Sada); err != nil {
			log.Printf("prognoza: kiša uz izdanje: %v", err)
		}
	}
	om, err := prognoza.Zamotaj(prognoza.Omotnica{Izdano: ishod.Sada, Cvor: cvor}, paket)
	if err != nil {
		return err
	}
	_, err = rec.Record(ctx, baza, prognoza.EntitetIzdanja, strconv.FormatInt(ishod.Sada, 10), om)
	return err
}

// primateljIzdanja upisuje primljena izdanja u bazu prognoza, jedno po jedno
// i u pozadini, da razmjena ne čeka na upis tisuća vrijednosti.
type primateljIzdanja struct {
	mu     sync.Mutex // jedan upis u isto vrijeme
	pb     *sql.DB
	ob     *sql.DB // baza oborina, za kišu iz izdanja
	rec    *ledger.Recorder
	izdaje func() bool
}

var primateljPrognoze = &primateljIzdanja{}

// postavi otvara primanje kad je baza prognoza otvorena i nadoknadi ono što
// je stiglo prije toga ili dok čvor nije primao.
func (p *primateljIzdanja) postavi(pb *sql.DB, rec *ledger.Recorder, izdaje func() bool) {
	p.mu.Lock()
	p.pb, p.rec, p.izdaje = pb, rec, izdaje
	p.mu.Unlock()
	go p.nadoknadi()
}

// postaviOborine daje bazu oborina u koju ide kiša iz izdanja
func (p *primateljIzdanja) postaviOborine(ob *sql.DB) {
	p.mu.Lock()
	p.ob = ob
	p.mu.Unlock()
}

// nadoknadi upiše zadnji model i sva izdanja iz knjige kojih baza prognoza
// još nema. Zove se pri pokretanju i kad čvor prestane izdavati prognozu.
func (p *primateljIzdanja) nadoknadi() {
	if p.rec == nil {
		return
	}
	verzije, err := p.rec.LatestOf(context.Background(), []string{prognoza.EntitetModela, prognoza.EntitetIzdanja})
	if err != nil {
		log.Printf("prognoza: primljena izdanja: %v", err)
		return
	}
	p.upisi(verzije)
}

// primi preuzima verzije upravo primljene razmjenom.
func (p *primateljIzdanja) primi(verzije []ledger.Version) {
	var nase []ledger.Version
	for _, v := range verzije {
		if v.Entity == prognoza.EntitetIzdanja || v.Entity == prognoza.EntitetModela {
			nase = append(nase, v)
		}
	}
	if len(nase) > 0 {
		go p.upisi(nase)
	}
}

func (p *primateljIzdanja) upisi(verzije []ledger.Version) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pb == nil || p.rec == nil || (p.izdaje != nil && p.izdaje()) {
		// Čvor koji izdaje prognozu drži se svog računa; primljena izdanja
		// ostaju u knjizi i upišu se ako prestane izdavati.
		return
	}
	ctx := context.Background()
	// zadnja verzija po zapisu, model prije izdanja, izdanja redom sati
	zadnje := map[string]ledger.Version{}
	for _, v := range verzije {
		k := v.Entity + "|" + v.EntityID
		if t, ima := zadnje[k]; !ima || v.VersionID > t.VersionID {
			zadnje[k] = v
		}
	}
	lista := make([]ledger.Version, 0, len(zadnje))
	for _, v := range zadnje {
		if v.NodeID == p.rec.Cvor() || v.Archived {
			continue
		}
		lista = append(lista, v)
	}
	sort.Slice(lista, func(i, j int) bool {
		if (lista[i].Entity == prognoza.EntitetModela) != (lista[j].Entity == prognoza.EntitetModela) {
			return lista[i].Entity == prognoza.EntitetModela
		}
		a, _ := strconv.ParseInt(lista[i].EntityID, 10, 64)
		b, _ := strconv.ParseInt(lista[j].EntityID, 10, 64)
		return a < b
	})
	upisano := 0
	var zadnjeIzdano int64
	var izdavac string
	for _, v := range lista {
		if top, err := p.rec.Latest(ctx, v.Entity, v.EntityID); err != nil || top.VersionID != v.VersionID {
			continue // stigla je i novija verzija; ona se upisuje
		}
		switch v.Entity {
		case prognoza.EntitetModela:
			var pm prognoza.PaketModela
			om, err := prognoza.Odmotaj(v.Payload, &pm)
			if err == nil {
				if _, otisak, _ := prognoza.SastaviModel(p.pb); otisak == om.Otisak {
					continue
				}
				err = prognoza.PrimiModel(p.pb, pm)
			}
			if err != nil {
				log.Printf("prognoza: primljeni model: %v", err)
				continue
			}
			log.Printf("prognoza: primljen model s čvora %s", om.Cvor)
		case prognoza.EntitetIzdanja:
			if prognoza.ImaIzdanjeIzKnjige(p.pb, v.VersionID) {
				continue
			}
			var paket prognoza.PaketIzdanja
			om, err := prognoza.Odmotaj(v.Payload, &paket)
			if err != nil {
				log.Printf("prognoza: primljeno izdanje %s: %v", v.EntityID, err)
				continue
			}
			ok, err := prognoza.PrimiIzdanje(p.pb, v.VersionID, paket)
			if err != nil {
				log.Printf("prognoza: primljeno izdanje %s: %v", v.EntityID, err)
				continue
			}
			if ok {
				upisano++
				zadnjeIzdano, izdavac = om.Izdano, om.Cvor
				if err := prognoza.PrimiOborine(p.ob, paket.Oborine); err != nil {
					log.Printf("prognoza: kiša iz izdanja %s: %v", v.EntityID, err)
				}
			}
		}
	}
	if upisano > 0 {
		log.Printf("prognoza: primljeno %d izdanja razmjenom, zadnje za %s UTC s čvora %s", upisano,
			time.Unix(zadnjeIzdano*3600, 0).UTC().Format("2006-01-02 15:04"), izdavac)
	}
}

// prorjedjujIzdanja svakih šest sati iz knjige briše izdanja starija od
// DrziIzdanjaDana.
func prorjedjujIzdanja(ctx context.Context, rec *ledger.Recorder) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		if n, err := rec.Prorijedi(ctx, prognoza.EntitetIzdanja, time.Now().AddDate(0, 0, -DrziIzdanjaDana)); err != nil {
			log.Printf("prognoza: %v", err)
		} else if n > 0 {
			log.Printf("prognoza: iz knjige uklonjeno %d izdanja starijih od %d dana", n, DrziIzdanjaDana)
		}
		// kazalo arhive: vrijedi samo zadnje izdanje letve, starije verzije
		// nemaju povijesnu vrijednost (i nakupile su se dok su se dva čvora
		// nadglasavala, 0.0.19)
		if n, err := rec.ProrijediZamijenjene(ctx, EntitetArhive); err != nil {
			log.Printf("arhiva: %v", err)
		} else if n > 0 {
			log.Printf("arhiva: iz knjige uklonjeno %d zamijenjenih verzija kazala", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
