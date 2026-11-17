package web

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"gocop/internal/models"
	"gocop/internal/service"
)

// MjestoNaTerenu je kartica jednog mjesta na stranici Na terenu (crtež 19a,
// 6): naslov bez opisa, opis ispod, dionica desno i redci sredstava
type MjestoNaTerenu struct {
	Naslov      string
	Opis        string
	SectionCode string
	Redci       []RedakNaTerenu
}

// RedakNaTerenu je jedno sredstvo na mjestu, s poveznicama na povrat i
// utrošak kad ih gledatelj smije upisati u skladište koje je izdalo
type RedakNaTerenu struct {
	models.Stanje
	Naziv    string
	Jedinica string
	Povrat   string
	Utrosak  string
}

// mjestaNaTerenu slaže stanje na terenu u kartice po mjestu, redom kojim
// stiže; smije javlja smije li gledatelj pisati u skladište
func mjestaNaTerenu(stanja []models.Stanje, vrste []models.VrstaSredstva, journalID string, smije func(skladisteID string) bool) []MjestoNaTerenu {
	poVrsti := map[string]models.VrstaSredstva{}
	for _, v := range vrste {
		poVrsti[v.ID] = v
	}
	var out []MjestoNaTerenu
	indeks := map[string]int{}
	for _, st := range stanja {
		kljuc := st.Teren().Kljuc()
		i, ima := indeks[kljuc]
		if !ima {
			i = len(out)
			indeks[kljuc] = i
			out = append(out, MjestoNaTerenu{Naslov: st.NaslovMjesta(), Opis: st.Mjesto, SectionCode: st.SectionCode})
		}
		r := RedakNaTerenu{Stanje: st, Naziv: poVrsti[st.VrstaID].Naziv, Jedinica: poVrsti[st.VrstaID].Jedinica}
		if st.IzdaloID != "" && smije(st.IzdaloID) {
			r.Povrat = zahvatNaMjestu(st, models.PrometPovrat, journalID)
			r.Utrosak = zahvatNaMjestu(st, models.PrometUtrosak, journalID)
		}
		out[i].Redci = append(out[i].Redci, r)
	}
	return out
}

// zahvatNaMjestu je adresa obrasca zahvata u skladištu koje je izdalo, s
// vrstom, sredstvom, oblikom, mjestom i obranom već upisanima
func zahvatNaMjestu(st models.Stanje, vrsta, journalID string) string {
	q := url.Values{"vrsta": {vrsta}, "sredstvo": {st.VrstaID}}
	dodaj := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	dodaj("oblik", st.Oblik)
	if st.AreaID > 0 {
		q.Set("podrucje", strconv.Itoa(st.AreaID))
	}
	dodaj("dionica", st.SectionCode)
	dodaj("objekt", st.StructureID)
	dodaj("mjesto", st.Mjesto)
	dodaj("obrana", journalID)
	return "/sredstva/skladista/" + st.IzdaloID + "/promet/novo?" + q.Encode()
}

// zahvatIzAdrese je obrazac zahvata popunjen iz adrese: vrsta, sredstvo,
// oblik, a uz povrat i utrošak s Na terenu i mjesto i obrana
func zahvatIzAdrese(q url.Values, skladisteID string) service.Zahvat {
	z := service.Zahvat{Vrsta: q.Get("vrsta"), SkladisteID: skladisteID, VrstaID: q.Get("sredstvo"), Oblik: q.Get("oblik"),
		SectionCode: q.Get("dionica"), StructureID: q.Get("objekt"), Mjesto: q.Get("mjesto"), JournalID: q.Get("obrana"),
		Datum: time.Now().In(models.Zagreb)}
	z.AreaID, _ = strconv.Atoi(q.Get("podrucje")) // nečitljivo područje: obrazac se otvara bez njega
	if z.Vrsta == "" {
		z.Vrsta = models.PrometPrimka
	}
	if z.Oblik == "" {
		z.Oblik = zadaniOblik(z.Vrsta)
	}
	return z
}

// smijePisatiU javlja smije li gledatelj pisati u skladište; skladišta čita
// jednom po zahtjevu
func (h *MtsHandler) smijePisatiU(r *http.Request, perms *models.UserPermissions) func(string) bool {
	var poID map[string]*models.Skladiste
	return func(id string) bool {
		if poID == nil {
			poID = map[string]*models.Skladiste{}
			sva, _ := h.svc().Skladista(r.Context(), "", 0, false) // bez popisa skladišta nema gumba, a stranica se ipak prikaže
			for i := range sva {
				poID[sva[i].ID] = &sva[i]
			}
		}
		sk := poID[id]
		return sk != nil && h.svc().SmijePisati(perms, sk)
	}
}
