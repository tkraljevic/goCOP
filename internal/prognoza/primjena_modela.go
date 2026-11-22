package prognoza

// Primjena i uklanjanje modela letve sa stranice „Računi letvi”. Primijeni upiše
// pripremljeni i probani račun iz nacrta u model_letve; od tada ga izdanje računa
// i putuje s modelom. Ukloni briše redak, pa letva radi kao prije njega.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// PrimijeniNacrt upiše račun iz nacrta kao model letve i odbaci nacrt. Račun
// mora biti pripremljen i proban, a razlog je obavezan i ostaje uz model.
// Zatečeni model postaje prethodni, a njegove promjene se nastavljaju.
func PrimijeniNacrt(baza *sql.DB, letva string, promjena PromjenaLetve, o OkvirModela) error {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return errors.New("upiši razlog; ostaje uz model")
	}
	var err error
	if o.Glavne, err = SveInacice(baza); err != nil {
		return err
	}
	n, err := probaniNacrtLetve(baza, letva)
	if err != nil {
		return err
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return err
	}
	z := zapisZaPrimjenu(n.Zapis, zapisi, promjena)
	if err := SpremiZapisLetve(baza, z, otisakProbe(z), o); err != nil {
		return err
	}
	return ObrisiNacrt(baza, letva)
}

// probaniNacrtLetve je nacrt letve koji je pripremljen i proban.
func probaniNacrtLetve(baza *sql.DB, letva string) (NacrtLetve, error) {
	nacrti, err := Nacrti(baza)
	if err != nil {
		return NacrtLetve{}, err
	}
	n, ima := nacrti[letva]
	// satni račun mora biti i pripremljen; dnevni model se uči u samoj probi
	pripremljen := n.Priprema != nil || n.Zapis.Metoda == MetodaDnevni
	if !ima || !pripremljen || n.Proba == nil {
		return NacrtLetve{}, errors.New("račun nije proban; pripremi ga i probaj, pa primijeni")
	}
	return n, nil
}

// zapisZaPrimjenu je zapis iz nacrta s poviješću zatečenog modela: stari zapis kao
// prethodni (bez svojeg prethodnog, jer se čuva jedan) i sve promjene redom.
func zapisZaPrimjenu(z ZapisLetve, zapisi map[string]ZapisLetve, promjena PromjenaLetve) ZapisLetve {
	z.Prethodni, z.Promjene = nil, nil
	if stari, ima := zapisi[z.Letva]; ima {
		z.Promjene = append(z.Promjene, stari.Promjene...)
		stari.Prethodni = nil
		if prethodni, err := json.Marshal(stari); err == nil {
			z.Prethodni = prethodni
		}
	}
	z.Promjene = append(z.Promjene, promjena)
	return z
}

// otisakProbe je otisak definicije i brojeva koje je proba mjerila.
func otisakProbe(z ZapisLetve) string {
	h := sha256.New()
	mjereno := struct {
		Velicina string
		Racun    RacunLetve
		Brojevi  BrojeviLetve
		Dnevni   json.RawMessage
	}{z.Velicina, z.Racun, z.Brojevi, z.Dnevni}
	_ = json.NewEncoder(h).Encode(mjereno) // sažetak ne vraća grešku pisanja, a zapis se da zapisati
	return hex.EncodeToString(h.Sum(nil))
}

// VratiPrethodniZapis vraća prethodni model letve umjesto sadašnjeg. Sadašnji
// postaje prethodni, pa se povratak može i poništiti. Razlog je obavezan. Model je
// već bio u radu s tim brojevima, pa se ne priprema ni ne proba iznova.
func VratiPrethodniZapis(baza *sql.DB, letva string, promjena PromjenaLetve, o OkvirModela) error {
	if strings.TrimSpace(promjena.Razlog) == "" {
		return errors.New("upiši razlog; ostaje uz model")
	}
	var err error
	if o.Glavne, err = SveInacice(baza); err != nil {
		return err
	}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return err
	}
	sadasnji := zapisi[letva]
	var prethodni ZapisLetve
	if !ImaPrethodniZapis(sadasnji) || json.Unmarshal(sadasnji.Prethodni, &prethodni) != nil || prethodni.Letva != letva {
		return errors.New("model nema prethodnu inačicu na koju bi se vratio")
	}
	prethodni.Promjene = nil
	z := zapisZaPrimjenu(prethodni, zapisi, promjena)
	return SpremiZapisLetve(baza, z, otisakProbe(z), o)
}

// ImaPrethodniZapis javlja nosi li zapis prethodnu inačicu; prazno polje se iz
// baze čita kao null.
func ImaPrethodniZapis(z ZapisLetve) bool {
	return len(z.Prethodni) > 0 && string(z.Prethodni) != "null"
}

// UkloniZapisLetve briše model letve, pa letva radi kao prije njega: iz računa u
// programu, ako ga ima, inače se samo mjeri. Letva koja iz nje računa ostaje:
// ulaz bez računa je vrh lanca, pa ona računa iz izmjerenog, s kraćim dosegom.
func UkloniZapisLetve(baza *sql.DB, letva string) error {
	redci, _, err := ZapisiLetvi(baza)
	if err != nil {
		return err
	}
	if _, ima := redci[letva]; !ima {
		return errors.New("letva nema primijenjen model")
	}
	_, err = baza.Exec(`DELETE FROM model_letve WHERE letva = ?`, letva)
	return err
}
