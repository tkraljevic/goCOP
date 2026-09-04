package web

import (
	"net/http"
	"net/url"
	"strconv"
)

// Pager je jedna stranica popisa: što se prikazuje i kamo vode poveznice.
//
// Registri su mali, nekoliko stotina zapisa, pa se cijeli filtrirani popis
// učita i reže u memoriji. To drži repozitorije jednostavnima; kad dođu
// mjerenja i dnevnici, koji rastu bez granice, oni dobivaju listanje u
// samom upitu. Ovdje je bitno drugo: na telefonu popis od pet stotina
// kartica nije popis nego zid, a stranica od dva tuceta se može pregledati.
type Pager struct {
	Page    int // tekuća stranica, od 1
	PerPage int
	Total   int
	Pages   int
	From    int // redni broj prve stavke na stranici, od 1
	To      int // redni broj zadnje stavke na stranici

	base url.Values
	path string
}

const registryPerPage = 24

// paginate reže popis prema parametru "page" iz upita i vraća stranicu s
// opisom. Ostali parametri upita ostaju u poveznicama, pa filtar preživi
// listanje.
func paginate[T any](items []T, r *http.Request, perPage int) ([]T, Pager) {
	if perPage <= 0 {
		perPage = registryPerPage
	}
	total := len(items)
	pages := (total + perPage - 1) / perPage
	if pages < 1 {
		pages = 1
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}

	from := (page-1)*perPage + 1
	to := page * perPage
	if to > total {
		to = total
	}
	if total == 0 {
		from = 0
	}

	q := r.URL.Query()
	q.Del("page")
	p := Pager{Page: page, PerPage: perPage, Total: total, Pages: pages, From: from, To: to,
		base: q, path: r.URL.Path}

	if total == 0 {
		return nil, p
	}
	return items[from-1 : to], p
}

func (p Pager) HasPrev() bool { return p.Page > 1 }
func (p Pager) HasNext() bool { return p.Page < p.Pages }
func (p Pager) Prev() int     { return p.Page - 1 }
func (p Pager) Next() int     { return p.Page + 1 }
func (p Pager) Multi() bool   { return p.Pages > 1 }

// URL vraća poveznicu na zadanu stranicu uz sve postojeće filtre
func (p Pager) URL(page int) string {
	q := url.Values{}
	for k, v := range p.base {
		q[k] = v
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if enc := q.Encode(); enc != "" {
		return p.path + "?" + enc
	}
	return p.path
}

// Numbers vraća brojeve stranica za prikaz: prvu, zadnju i prozor oko
// tekuće. Nula označava razmak ("…"), pa predložak zna gdje ga staviti.
func (p Pager) Numbers() []int {
	if p.Pages <= 7 {
		out := make([]int, 0, p.Pages)
		for i := 1; i <= p.Pages; i++ {
			out = append(out, i)
		}
		return out
	}
	var out []int
	add := func(n int) {
		if len(out) > 0 && out[len(out)-1] == n {
			return
		}
		out = append(out, n)
	}
	add(1)
	lo, hi := p.Page-1, p.Page+1
	if lo < 2 {
		lo = 2
	}
	if hi > p.Pages-1 {
		hi = p.Pages - 1
	}
	if lo > 2 {
		add(0)
	}
	for i := lo; i <= hi; i++ {
		add(i)
	}
	if hi < p.Pages-1 {
		add(0)
	}
	add(p.Pages)
	return out
}
