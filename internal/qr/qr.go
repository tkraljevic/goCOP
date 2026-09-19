// Package qr kodira kratak tekst kao QR kod (ISO/IEC 18004), bajtni način,
// razina ispravljanja M, verzije 1 do 6 (do 108 bajtova). Dovoljno za
// adresu dokumenta u goCOP-u; bez vanjskih ovisnosti.
package qr

import (
	"errors"
)

// Kod je matrica modula: Moduli[y][x] je true za tamni modul
type Kod struct {
	Velicina int
	Moduli   [][]bool
}

// verzija opisuje kapacitet i raspored blokova za razinu M
type verzija struct {
	broj       int
	ecPoBloku  int
	blokovi    []int // broj podatkovnih kodnih riječi po bloku
	poravnanja []int
	ostatak    int // bitovi ostatka nakon posljednje kodne riječi
}

var verzije = []verzija{
	{1, 10, []int{16}, nil, 0},
	{2, 16, []int{28}, []int{6, 18}, 7},
	{3, 26, []int{44}, []int{6, 22}, 7},
	{4, 18, []int{32, 32}, []int{6, 26}, 7},
	{5, 24, []int{43, 43}, []int{6, 30}, 7},
	{6, 16, []int{27, 27, 27, 27}, []int{6, 34}, 7},
}

func (v verzija) podataka() int {
	n := 0
	for _, b := range v.blokovi {
		n += b
	}
	return n
}

// Kodiraj bira najmanju verziju u koju tekst stane i vraća kod s maskom
// koja daje najmanju kaznu
func Kodiraj(tekst string) (*Kod, error) {
	podaci := []byte(tekst)
	var v *verzija
	for i := range verzije {
		// zaglavlje: 4 bita način + 8 bita duljina
		if (12+8*len(podaci)+7)/8 <= verzije[i].podataka() {
			v = &verzije[i]
			break
		}
	}
	if v == nil {
		return nil, errors.New("qr: tekst je predug za verziju 6 (najviše 106 bajtova)")
	}
	// bitni niz: način 0100, duljina, bajtovi, završetak, dopuna
	var bitovi []bool
	dodaj := func(vrijednost, n int) {
		for i := n - 1; i >= 0; i-- {
			bitovi = append(bitovi, (vrijednost>>uint(i))&1 == 1)
		}
	}
	dodaj(0b0100, 4)
	dodaj(len(podaci), 8)
	for _, b := range podaci {
		dodaj(int(b), 8)
	}
	kapacitet := v.podataka() * 8
	for i := 0; i < 4 && len(bitovi) < kapacitet; i++ {
		bitovi = append(bitovi, false)
	}
	for len(bitovi)%8 != 0 {
		bitovi = append(bitovi, false)
	}
	kodne := make([]byte, 0, v.podataka())
	for i := 0; i+8 <= len(bitovi); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			if bitovi[i+j] {
				b |= 1 << uint(7-j)
			}
		}
		kodne = append(kodne, b)
	}
	for i := 0; len(kodne) < v.podataka(); i++ {
		if i%2 == 0 {
			kodne = append(kodne, 0xEC)
		} else {
			kodne = append(kodne, 0x11)
		}
	}

	// blokovi i ispravljanje pogrešaka, pa preplitanje
	var blokoviPod [][]byte
	var blokoviEC [][]byte
	poz := 0
	for _, n := range v.blokovi {
		blok := kodne[poz : poz+n]
		poz += n
		blokoviPod = append(blokoviPod, blok)
		blokoviEC = append(blokoviEC, reedSolomon(blok, v.ecPoBloku))
	}
	var niz []byte
	najdulji := 0
	for _, b := range blokoviPod {
		if len(b) > najdulji {
			najdulji = len(b)
		}
	}
	for i := 0; i < najdulji; i++ {
		for _, b := range blokoviPod {
			if i < len(b) {
				niz = append(niz, b[i])
			}
		}
	}
	for i := 0; i < v.ecPoBloku; i++ {
		for _, b := range blokoviEC {
			niz = append(niz, b[i])
		}
	}

	velicina := 17 + 4*v.broj
	najbolji := (*Kod)(nil)
	najmanja := int(^uint(0) >> 1)
	for maska := 0; maska < 8; maska++ {
		k := slozi(*v, velicina, niz, maska)
		if kazna := kazna(k); kazna < najmanja {
			najmanja, najbolji = kazna, k
		}
	}
	return najbolji, nil
}

// ---- Reed-Solomon nad GF(256), primitivni polinom 0x11D ----

var gfExp, gfLog [512]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = x
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// reedSolomon vraća n kodnih riječi za ispravljanje pogrešaka
func reedSolomon(podaci []byte, n int) []byte {
	// generator: (x - α^0)(x - α^1)…(x - α^(n-1))
	gen := []int{1}
	for i := 0; i < n; i++ {
		novi := make([]int, len(gen)+1)
		for j, g := range gen {
			novi[j] ^= g
			novi[j+1] ^= gfMul(g, gfExp[i])
		}
		gen = novi
	}
	ost := make([]int, n)
	for _, b := range podaci {
		vodeci := int(b) ^ ost[0]
		copy(ost, ost[1:])
		ost[n-1] = 0
		if vodeci != 0 {
			for j := 0; j < n; j++ {
				ost[j] ^= gfMul(gen[j+1], vodeci)
			}
		}
	}
	out := make([]byte, n)
	for i, o := range ost {
		out[i] = byte(o)
	}
	return out
}

// ---- slaganje matrice ----

func slozi(v verzija, n int, niz []byte, maska int) *Kod {
	moduli := make([][]bool, n)
	funkcija := make([][]bool, n) // moduli zauzeti uzorcima, ne podacima
	for i := range moduli {
		moduli[i] = make([]bool, n)
		funkcija[i] = make([]bool, n)
	}
	postavi := func(x, y int, tamno bool) {
		if x >= 0 && y >= 0 && x < n && y < n {
			moduli[y][x] = tamno
			funkcija[y][x] = true
		}
	}
	// uzorci za pronalaženje s odvajanjem
	for _, p := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				x, y := p[0]+dx, p[1]+dy
				rub := dx == -1 || dy == -1 || dx == 7 || dy == 7
				vanjski := dx == 0 || dy == 0 || dx == 6 || dy == 6
				unutarnji := dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4
				postavi(x, y, !rub && (vanjski || unutarnji))
			}
		}
	}
	// uzorci za poravnanje
	for _, cy := range v.poravnanja {
		for _, cx := range v.poravnanja {
			if funkcija[cy][cx] {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					postavi(cx+dx, cy+dy, dx == -2 || dx == 2 || dy == -2 || dy == 2 || (dx == 0 && dy == 0))
				}
			}
		}
	}
	// uzorci vremena
	for i := 8; i < n-8; i++ {
		postavi(i, 6, i%2 == 0)
		postavi(6, i, i%2 == 0)
	}
	// mjesta za podatke o obliku (uzorak vremena na 6 ostaje), pa tamni modul
	for i := 0; i < 8; i++ {
		if i != 6 {
			postavi(i, 8, false)
			postavi(8, i, false)
		}
		postavi(n-1-i, 8, false)
		postavi(8, n-1-i, false)
	}
	postavi(8, 8, false)
	postavi(8, n-8, true)

	// podaci cik-cak od donjeg desnog kuta, u stupcima po dva, preskačući stupac 6
	bit := 0
	ukupno := len(niz) * 8
	gore := true
	for x := n - 1; x > 0; x -= 2 {
		if x == 6 {
			x--
		}
		for i := 0; i < n; i++ {
			y := i
			if gore {
				y = n - 1 - i
			}
			for dx := 0; dx < 2; dx++ {
				xx := x - dx
				if funkcija[y][xx] {
					continue
				}
				tamno := false
				if bit < ukupno {
					tamno = niz[bit/8]>>(7-uint(bit%8))&1 == 1
				}
				bit++
				if maskiraj(maska, xx, y) {
					tamno = !tamno
				}
				moduli[y][xx] = tamno
			}
		}
		gore = !gore
	}
	upisiOblik(moduli, n, maska)
	return &Kod{Velicina: n, Moduli: moduli}
}

func maskiraj(m, x, y int) bool {
	switch m {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	}
	return ((x+y)%2+(x*y)%3)%2 == 0
}

// upisiOblik upisuje 15 bita podataka o obliku: razina M (00) i maska, s BCH zaštitom
func upisiOblik(moduli [][]bool, n, maska int) {
	podatak := (0b00 << 3) | maska
	bch := podatak << 10
	for i := 14; i >= 10; i-- {
		if bch>>uint(i)&1 == 1 {
			bch ^= 0b10100110111 << uint(i-10)
		}
	}
	oblik := ((podatak << 10) | bch) ^ 0b101010000010010
	bit := func(i int) bool { return oblik>>uint(14-i)&1 == 1 }
	// oko gornjeg lijevog uzorka
	for i := 0; i < 6; i++ {
		moduli[8][i] = bit(i)
	}
	moduli[8][7] = bit(6)
	moduli[8][8] = bit(7)
	moduli[7][8] = bit(8)
	for i := 9; i < 15; i++ {
		moduli[14-i][8] = bit(i)
	}
	// uz donji lijevi i gornji desni
	for i := 0; i < 7; i++ {
		moduli[n-1-i][8] = bit(i)
	}
	for i := 7; i < 15; i++ {
		moduli[8][n-15+i] = bit(i)
	}
}

// kazna ocjenjuje masku po četiri pravila norme; manje je bolje
func kazna(k *Kod) int {
	n := k.Velicina
	m := k.Moduli
	total := 0
	// 1: nizovi istih modula u retku i stupcu
	for i := 0; i < n; i++ {
		for _, red := range [][]bool{redak(m, i), stupac(m, i)} {
			niz := 1
			for j := 1; j < n; j++ {
				if red[j] == red[j-1] {
					niz++
					if niz == 5 {
						total += 3
					} else if niz > 5 {
						total++
					}
				} else {
					niz = 1
				}
			}
		}
	}
	// 2: blokovi 2×2
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			if m[y][x] == m[y][x+1] && m[y][x] == m[y+1][x] && m[y][x] == m[y+1][x+1] {
				total += 3
			}
		}
	}
	// 3: uzorak nalik na tražilo
	uzorak := []bool{true, false, true, true, true, false, true}
	for i := 0; i < n; i++ {
		for _, red := range [][]bool{redak(m, i), stupac(m, i)} {
			for j := 0; j+7 <= n; j++ {
				ok := true
				for t := 0; t < 7; t++ {
					if red[j+t] != uzorak[t] {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				if (j >= 4 && svijetlo(red[j-4:j])) || (j+11 <= n && svijetlo(red[j+7:j+11])) {
					total += 40
				}
			}
		}
	}
	// 4: udio tamnih
	tamnih := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if m[y][x] {
				tamnih++
			}
		}
	}
	udio := tamnih * 100 / (n * n)
	odstup := udio - 50
	if odstup < 0 {
		odstup = -odstup
	}
	total += (odstup / 5) * 10
	return total
}

func redak(m [][]bool, i int) []bool { return m[i] }

func stupac(m [][]bool, i int) []bool {
	out := make([]bool, len(m))
	for y := range m {
		out[y] = m[y][i]
	}
	return out
}

func svijetlo(b []bool) bool {
	for _, x := range b {
		if x {
			return false
		}
	}
	return true
}
