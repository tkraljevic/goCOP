package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Djelatnici na zidu: otvoren i obrisan račun, isključena i ponovno
// uključena prijava, odlazak iz službe, oznaka globalnog administratora, te
// dodijeljeno, izmijenjeno i opozvano zaduženje. Ostale izmjene računa
// (telefon, adresa, lozinka, zadnja prijava) nisu redak. Istek roka nije
// upis, pa ga zid ne pokazuje; vidi se na stranici osobe.

// racunUKnjizi je verzija računa kako je zapisuje UserRepository
// (userVersion), bez lozinke
type racunUKnjizi struct {
	models.User
	Izmjena *models.IzmjenaZapisa `json:"izmjena"`
}

// zaduzenjeUKnjizi je verzija zaduženja kako je zapisuje UserRepository
// (dutyVersion)
type zaduzenjeUKnjizi struct {
	models.Duty
	Izmjena *models.IzmjenaZapisa `json:"izmjena"`
}

// prethodna čita verziju koju je ova zamijenila u x; false kad je u knjizi
// nema (sažimanje) ili se ne da pročitati
func (c *opisivac) prethodna(versionID string, x any) bool {
	if versionID == "" {
		return false
	}
	prethodne, err := c.s.rec.Verzije(c.ctx, []string{versionID})
	if err != nil || len(prethodne) == 0 || prethodne[0].Archived {
		return false
	}
	return json.Unmarshal(prethodne[0].Payload, x) == nil
}

// tkoJeMijenjao je ime osobe koja je napisala verziju, kad ga verzija nosi
// i kad pripada baš njoj (models.IzmjenaZapisa.VrijediZa)
func (c *opisivac) tkoJeMijenjao(i *models.IzmjenaZapisa, v ledger.Version) string {
	if !i.VrijediZa(v.CreatedAt) {
		return ""
	}
	return c.ime(i.Tko.String())
}

// racun: račun na zidu je kad je otvoren ili obrisan, kad mu je prijava
// isključena ili ponovno uključena, kad je osoba otišla iz službe i kad
// dobije ili izgubi oznaku globalnog administratora
func (c *opisivac) racun(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var u racunUKnjizi
	if json.Unmarshal(v.Payload, &u) != nil {
		return false
	}
	d.Link, d.Tko, d.Tekst = "/users/"+u.ID.String(), c.tkoJeMijenjao(u.Izmjena, v), u.FullName
	switch {
	case v.Archived:
		d.Naslov, d.Link = "Obrisan račun", ""
		return true
	case nova:
		d.Naslov = "Otvoren račun"
		if u.IsGlobalAdmin {
			d.Tekst += " · globalni administrator"
		}
		return true
	}
	var prije racunUKnjizi
	if !c.prethodna(v.Supersedes, &prije) {
		return false
	}
	naslov, promjene, vazno := promjeneRacuna(prije.User, u.User)
	if naslov == "" {
		return false
	}
	d.Naslov, d.Vazno = naslov, vazno
	if len(promjene) > 0 {
		d.Tekst += " · " + strings.Join(promjene, " · ")
	}
	return true
}

// promjeneRacuna su naslov retka, ostale promjene za tekst i je li redak
// istaknut, za izmjenu računa iz prije u sad; prazan naslov kad izmjena nije
// promjena ovlasti
func promjeneRacuna(prije, sad models.User) (string, []string, bool) {
	naslov, promjene := prijavaRacuna(prije, sad)
	vazno := naslov == "Odlazak iz službe"
	if sad.IsGlobalAdmin != prije.IsGlobalAdmin {
		oznaka := "Oduzeta oznaka globalnog administratora"
		if sad.IsGlobalAdmin {
			oznaka = "Dana oznaka globalnog administratora"
		}
		if naslov == "" {
			naslov = oznaka
		} else {
			promjene = append(promjene, maloPrvo(oznaka))
		}
		vazno = true
	}
	return naslov, promjene, vazno
}

// prijavaRacuna: odlazak iz službe, isključena i ponovno uključena prijava
func prijavaRacuna(prije, sad models.User) (string, []string) {
	switch {
	case sad.IzvanSluzbe() && !prije.IzvanSluzbe():
		opis := sad.Odlazak.Opis()
		if sad.OdlazakDan != nil {
			opis += " " + sad.OdlazakDan.In(models.Zagreb).Format("2. 1. 2006.")
		}
		return "Odlazak iz službe", []string{opis}
	case !sad.IsActive && prije.IsActive:
		return "Isključena prijava", nil
	case sad.IsActive && !prije.IsActive:
		return "Ponovno uključena prijava", nil
	}
	return "", nil
}

// zaduzenje: zaduženje na zidu je kad je dodijeljeno, opozvano i izmijenjeno
// (uloga, doseg, rok ili privremenost). Zaduženje obrisano s računom koji se
// nikad nije prijavio nije zaseban redak (vidi se „Obrisan račun”), a
// izmjenu koju je napisao program sam (istek s obranom) zid ne pokazuje.
func (c *opisivac) zaduzenje(d *Dogadjaj, v ledger.Version, nova bool) bool {
	var z zaduzenjeUKnjizi
	if json.Unmarshal(v.Payload, &z) != nil || v.Archived {
		return false
	}
	d.Link, d.Tko = "/users/"+z.UserID.String(), c.tkoJeMijenjao(z.Izmjena, v)
	c.mjestoZaduzenja(d, z.Duty)
	d.Tekst = c.opisZaduzenja(z.Duty)
	var prije *models.Duty
	var p zaduzenjeUKnjizi
	if !nova && c.prethodna(v.Supersedes, &p) {
		prije = &p.Duty
	}
	d.Naslov = naslovZaduzenja(prije, z.Duty, nova, d.Tko != "")
	if d.Naslov == "Dodijeljeno zaduženje" && d.Tko == "" && nova && z.AssignedBy != nil {
		d.Tko = c.ime(z.AssignedBy.String())
	}
	return d.Naslov != ""
}

// mjestoZaduzenja: sektor i branjeno područje retka; zaduženje samo s
// područjem pripada sektoru tog područja
func (c *opisivac) mjestoZaduzenja(d *Dogadjaj, z models.Duty) {
	if z.SectorID != nil {
		d.Sektor = *z.SectorID
	}
	if z.AreaID != nil {
		d.AreaID = *z.AreaID
		if d.Sektor == "" {
			d.Sektor = c.podrucja[*z.AreaID]
		}
	}
}

// naslovZaduzenja je naslov retka za verziju zaduženja sad, uz prethodnu
// (nil kad je nema); prazno kad verzija nije redak. Izmjena je redak samo
// kad se zna tko ju je napravio, jer program sam mijenja istek.
func naslovZaduzenja(prije *models.Duty, sad models.Duty, nova, znaSeTko bool) string {
	if !sad.IsActive {
		if prije != nil && prije.IsActive {
			return "Opozvano zaduženje"
		}
		return ""
	}
	switch {
	case nova || (prije != nil && !prije.IsActive):
		return "Dodijeljeno zaduženje"
	case prije != nil && znaSeTko && kljucZaduzenja(*prije) != kljucZaduzenja(sad):
		return "Izmijenjeno zaduženje"
	}
	return ""
}

// kljucZaduzenja je ono što zaduženje daje (uloga, doseg, naziv, rok,
// privremenost) kao tekst, da se dvije verzije daju usporediti
func kljucZaduzenja(d models.Duty) string {
	sektor, podrucje, rok := "", 0, ""
	if d.SectorID != nil {
		sektor = *d.SectorID
	}
	if d.AreaID != nil {
		podrucje = *d.AreaID
	}
	if r := d.ZadaniRok(); r != nil {
		rok = r.UTC().Format("2006-01-02T15")
	}
	return fmt.Sprintf("%s|%s|%s|%d|%s|%s|%v|%v|%s", d.Role, d.ScopeType, sektor, podrucje,
		strings.TrimSpace(d.SectionCodes), d.Title, d.IsTemporary, d.IsticeSObranom, rok)
}

// opisZaduzenja je „Ime · uloga · sektor B · BP 16 · dionice · do 15. 12.
// 2026.” za redak zaduženja
func (c *opisivac) opisZaduzenja(z models.Duty) string {
	var dijelovi []string
	if ime := c.ime(z.UserID.String()); ime != "" {
		dijelovi = append(dijelovi, ime)
	}
	uloga := z.Role.Label()
	if t := strings.TrimSpace(z.Title); t != "" && t != uloga {
		uloga = t + " (" + maloPrvo(uloga) + ")"
	}
	dijelovi = append(dijelovi, uloga)
	dijelovi = append(dijelovi, dosegZaduzenja(z)...)
	if r := rokZaduzenja(z); r != "" {
		dijelovi = append(dijelovi, r)
	}
	return strings.Join(dijelovi, " · ")
}

// dosegZaduzenja su sektor, branjeno područje i dionice zaduženja
func dosegZaduzenja(z models.Duty) []string {
	var out []string
	if z.SectorID != nil && *z.SectorID != "" {
		out = append(out, models.Terms().Get("sektor")+" "+*z.SectorID)
	}
	if z.AreaID != nil {
		out = append(out, fmt.Sprintf("BP %d", *z.AreaID))
	}
	if s := strings.TrimSpace(z.SectionCodes); s != "" {
		out = append(out, s)
	}
	return out
}

// rokZaduzenja je „do 15. 12. 2026.”, „dok traje obrana” ili „privremeno”;
// prazno za stalno zaduženje
func rokZaduzenja(z models.Duty) string {
	switch r := z.ZadaniRok(); {
	case z.IsticeSObranom:
		return "dok traje obrana"
	case r != nil:
		return "do " + models.ZadnjiDanVazenja(*r).Format("2. 1. 2006.")
	case z.IsTemporary:
		return "privremeno"
	}
	return ""
}

// maloPrvo je tekst s malim prvim slovom, za nastavak rečenice
func maloPrvo(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}
