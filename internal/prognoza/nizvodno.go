package prognoza

// Nizvodno kaže od kojeg sata prognoza letvi niz lanac postaje upitna kad
// gore nešto ne radi. prvi su letve na kojima nevolja počinje (letva → prvi
// upitni sat, UTC sat od epohe): karika bez mjerenja od sata poslije zadnjeg
// očitanja, rezerva ili vrh bez budućnosti od sata poslije izdanja.
//
// Letva niže mjerena je do sata izdavanja (Racunalo.U uzima mjerenje za
// t ≤ sada), pa joj nevolja odozgo stiže tek poslije njega:
//
//	A(letva) = najmanje po pojasima i ulazima od max(sada+1, A(ulaz)+PomakH)
//
// a letve iz prvi zadržavaju svoj sat ako je raniji. Zbrajati kašnjenja od
// zadnjeg očitanja vrha bilo bi krivo: Aljmaš bi uz Mohács bez mjerenja 11 h
// bio upitan od +1 h, a Batina je do izdanja izmjerena, pa je od +7 h.
//
// Uzima se najmanji namješteni pomak, pa je to najraniji mogući doseg; prozor
// prosjeka, razlomljeni pomak i ispravak po zadnjem mjerenju (do
// ZaostatakIspravka) ga samo odgađaju. Redom slaže letve samo po ulazima
// prvog pojasa, pa se prolazi dok se ništa ne mijenja, najviše
// len(pojasi)+1 put.
func Nizvodno(pojasi map[string][]Pojas, prvi map[string]int64, sada int64) map[string]int64 {
	a := make(map[string]int64, len(prvi))
	for l, t := range prvi {
		a[l] = t
	}
	for prolaz := 0; prolaz <= len(pojasi); prolaz++ {
		if !korakNizvodno(pojasi, a, sada) {
			break
		}
	}
	return a
}

// korakNizvodno jednom prođe sve letve i javlja je li ijednoj ranio sat.
func korakNizvodno(pojasi map[string][]Pojas, a map[string]int64, sada int64) bool {
	promjena := false
	for letva, ps := range pojasi {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				au, ima := a[u.Letva]
				if !ima {
					continue
				}
				t := max(sada+1, au+int64(u.PomakH))
				if staro, bilo := a[letva]; !bilo || t < staro {
					a[letva] = t
					promjena = true
				}
			}
		}
	}
	return promjena
}

// doseziUpitnog prevodi prve upitne sate u dosege od sata izdavanja: 0 znači
// da je upitna već vrijednost u satu izdanja (karika računata jer nema
// mjerenja). Srpske letve s prenesenom prognozom dobivaju najmanji doseg
// svojih izvora, jer se prenosi samo gdje svi izvori imaju vrijednost.
func doseziUpitnog(a map[string]int64, sada int64) map[string]int {
	d := make(map[string]int, len(a))
	for l, t := range a {
		d[l] = int(max(0, t-sada))
	}
	preneseniDoseg(d)
	return d
}

// preneseniDoseg dodaje srpske letve kojima je upitan barem jedan izvor.
func preneseniDoseg(d map[string]int) {
	for _, p := range PreneseneLetve {
		for _, izvor := range p.Izvori {
			v, ima := d[izvor]
			if !ima {
				continue
			}
			if staro, bilo := d[p.Letva]; !bilo || v < staro {
				d[p.Letva] = v
			}
		}
	}
}
