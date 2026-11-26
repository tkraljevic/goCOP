package web

import "sort"

// Knjiga verzija na Održavanju baze (crtež 18b · 4): koliko verzija ima
// koja vrsta zapisa, hrvatskim imenima umjesto imena tablica. Najveće vrste
// stoje same, a ostale se zbroje u jedan redak.

// naziviVrstaKnjige su vrste zapisa u knjizi riječima za čovjeka; vrsta koje
// ovdje nema pokazuje se pod imenom iz knjige
var naziviVrstaKnjige = map[string]string{
	"akti": "Akti", "akti_izvornici": "Izvornici akata", "akti_rjesenja_oznaka": "Oznake rješenja", "akti_slanja": "Slanja akata",
	"akti_sprance": "Špranci akata", "areas": "Branjena područja", "arhiva_biljeske": "Bilješke arhive", "arhiva_ispravci": "Ispravci arhive",
	"blagdani": "Blagdani", "contractor_assignments": "Zaduženja izvođača", "contractors": "Licencirane firme", "counties": "Županije",
	"defense_episodes": "Obrane", "dezurstva": "Dežurstva", "dnevna_izvjesca": "Dnevna izvješća", "duties": "Dužnosti",
	"journal_entries": "Unosi dnevnika", "journal_izvornici": "Izvornici dnevnika", "journal_sheets": "Listovi dnevnika", "journals": "Dnevnici",
	"kisomjeri": "Kišomjeri", "koeficijenti": "Koeficijenti", "maintained_waters": "Održavane vode", "memberships": "Članstva",
	"mreza": "Mreža", "mts_popisi": "Inventure", "mts_potrebe": "Potrebe za nabavom", "mts_promet": "Promet sredstava",
	"mts_skladista": "Skladišta", "mts_vrste": "Vrste sredstava", "municipalities": "Gradovi i općine", "obracun_postavke": "Postavke obračuna",
	"odbacena_ocitanja": "Odbačena očitanja", "opozivi": "Opozivi", "org_terms": "Nazivi", "ovlasti": "Ovlasti za primanje",
	"peers": "Računala", "posta_potpisi": "Potpisi e-pošte", "postavke": "Postavke", "potpisni_izdavatelji": "Izdavatelji certifikata",
	"potpisni_kljucevi": "Potpisni ključevi", "potvrde_prava": "Potvrde prava", "prijave": "Prijave s terena", "prijave_gresaka": "Prijave grešaka",
	"prijave_gresaka_odgovori": "Odgovori na prijave grešaka", "prijave_izvornici": "Izvornici prijava", "primatelji": "Primatelji",
	"propisi": "Propisi", "propisi_izmjene": "Izmjene propisa",
	"readings": "Očitanja", "role_modules": "Vidljivost modula", "section_stations": "Letve dionica", "section_structures": "Objekti dionica",
	"section_territories": "Ugroženo područje dionica", "sections": "Dionice", "sectors": "Sektori", "sektorska_izvjesca": "Izvješća sektora",
	"settlements": "Naselja", "slivovi": "Slivovi", "sluzbe": "Službe", "stations": "Letve", "structures": "Objekti",
	"user_modules": "Iznimke vidljivosti", "users": "Djelatnici", "vodocuvarski_izvornici": "Izvornici vodočuvarskih listova",
	"vodocuvarski_listovi": "Vodočuvarski listovi", "vodocuvarski_zadaci": "Vodočuvarski zadaci", "watercourses": "Vodotoci",
	"work_items": "Stavke radova", "zigovi": "Žigovi",
}

// VrstaUKnjizi je jedan redak Knjige verzija: vrsta zapisa i broj verzija
type VrstaUKnjizi struct {
	Naziv string
	Broj  int
}

// najviseVrstaUKnjizi je koliko se najvećih vrsta pokazuje zasebno
const najviseVrstaUKnjizi = 9

// vrsteUKnjizi slaže vrste po broju verzija, najveće prve; iza najviše
// vrsta ostale idu u redak „Ostale vrste”
func vrsteUKnjizi(poVrsti map[string]int, najvise int) []VrstaUKnjizi {
	out := make([]VrstaUKnjizi, 0, len(poVrsti))
	for v, n := range poVrsti {
		naziv := naziviVrstaKnjige[v]
		if naziv == "" {
			naziv = v
		}
		out = append(out, VrstaUKnjizi{Naziv: naziv, Broj: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Broj != out[j].Broj {
			return out[i].Broj > out[j].Broj
		}
		return out[i].Naziv < out[j].Naziv
	})
	if len(out) <= najvise {
		return out
	}
	ostale := VrstaUKnjizi{Naziv: "Ostale vrste"}
	for _, v := range out[najvise-1:] {
		ostale.Broj += v.Broj
	}
	return append(out[:najvise-1], ostale)
}

// VrsteUKnjizi su retci kartice Knjiga verzija
func (d DBMaintPageData) VrsteUKnjizi() []VrstaUKnjizi {
	return vrsteUKnjizi(d.Stats.ByEntity, najviseVrstaUKnjizi)
}
