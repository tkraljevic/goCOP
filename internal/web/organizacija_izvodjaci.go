package web

import (
	"slices"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/repository"
)

// Redak Priloga 2 Plana obrane na stranici Administrativna organizacija:
// „Pravna osoba za provedbu mjera obrane od poplava i rukovoditelji na
// branjenom području”. Pravna osoba je aktivna firma vezana na područje u
// registru licenciranih firmi (neaktivna se ne pokazuje), a rukovoditelj i
// zamjenici su osobe s aktivnom dužnošću rukovoditelja obrane izvođača ili
// zamjenika na tom području. Svaka firma ima svoje ljude, pa osoba stoji uz
// firmu koju navodi firma s njezina računa.

// OsobaIzvodjaca je rukovoditelj obrane izvođača ili zamjenik na području
type OsobaIzvodjaca struct {
	ID       uuid.UUID
	Ime      string
	Zvanje   string
	Firma    string // firma s računa osobe
	Zamjenik bool
}

// FirmaPodrucja je aktivna firma vezana na područje s rukovoditeljima
// obrane i zamjenicima iz te firme
type FirmaPodrucja struct {
	models.Contractor
	Rukovoditelji []OsobaIzvodjaca
	Zamjenici     []OsobaIzvodjaca
}

// IzvodjacPodrucja je redak Priloga 2 za jedno branjeno područje
type IzvodjacPodrucja struct {
	Firme []FirmaPodrucja
	// BezFirme imaju dužnost na području, a firma s njihova računa nije
	// među aktivnim firmama vezanima na područje (npr. firma je u
	// međuvremenu postala neaktivna ili joj je promijenjen naziv)
	BezFirme []OsobaIzvodjaca
}

// ulogeObraneIzvodjaca: rukovoditelj obrane izvođača pa zamjenik, redom
// kao u Prilogu 2
var ulogeObraneIzvodjaca = []models.Role{models.RoleContractorAreaLeader, models.RoleContractorAreaDeputy}

// osobeUloge su računi s aktivnom dužnošću u ulozi, sa svim dužnostima,
// po abecedi (service.UserService.ListUsers)
func (s *Server) osobeUloge(role models.Role) ([]models.User, error) {
	if s.userService == nil {
		return nil, nil
	}
	return s.userService.ListUsers("", 0, string(role), "", "")
}

// dodajIzvodjace upisuje područjima redak Priloga 2. Kad se djelatnici ne
// mogu pročitati, firme se i dalje vide, a stranica kaže da rukovoditelji
// trenutačno nisu poznati.
func (h *OrgHandler) dodajIzvodjace(data *OrgPageData, idx repository.ContractorIndex) {
	osobe, err := h.osobeObraneIzvodjaca()
	data.IzvodjaciNepoznati = err != nil
	for i := range data.Sectors {
		for j := range data.Sectors[i].Areas {
			a := &data.Sectors[i].Areas[j]
			a.Izvodjac = izvodjacPodrucja(idx.ByArea[a.ID], osobe[a.ID])
		}
	}
}

// osobeObraneIzvodjaca su rukovoditelji obrane izvođača i zamjenici po
// području dužnosti; bez izvora djelatnika nema nikoga
func (h *OrgHandler) osobeObraneIzvodjaca() (map[int][]OsobaIzvodjaca, error) {
	po := map[int][]OsobaIzvodjaca{}
	if h.osobeUloge == nil {
		return po, nil
	}
	for _, role := range ulogeObraneIzvodjaca {
		racuni, err := h.osobeUloge(role)
		if err != nil {
			return nil, err
		}
		for _, u := range racuni {
			dodajDuznostiIzvodjaca(po, u, role)
		}
	}
	return po, nil
}

// dodajDuznostiIzvodjaca dodaje osobu svakom području na kojem ima aktivnu
// dužnost u ulozi, jednom po području
func dodajDuznostiIzvodjaca(po map[int][]OsobaIzvodjaca, u models.User, role models.Role) {
	vec := map[int]bool{}
	for _, d := range u.Duties {
		if d.Role != role || !d.IsActive || d.AreaID == nil || vec[*d.AreaID] {
			continue
		}
		vec[*d.AreaID] = true
		po[*d.AreaID] = append(po[*d.AreaID], OsobaIzvodjaca{ID: u.ID, Ime: u.FullName, Zvanje: u.Title, Firma: u.OrgName,
			Zamjenik: role == models.RoleContractorAreaDeputy})
	}
}

// izvodjacPodrucja slaže aktivne firme područja i osobe s dužnošću: osoba
// ide uz prvu firmu koju navodi firma s njezina računa, a bez takve među
// BezFirme. Ista firma vezana dvaput na područje pokazuje se jednom.
func izvodjacPodrucja(firme []models.Contractor, osobe []OsobaIzvodjaca) IzvodjacPodrucja {
	var out IzvodjacPodrucja
	for _, c := range firme {
		if !slices.ContainsFunc(out.Firme, func(f FirmaPodrucja) bool { return f.ID == c.ID }) {
			out.Firme = append(out.Firme, FirmaPodrucja{Contractor: c})
		}
	}
	for _, o := range osobe {
		i := slices.IndexFunc(out.Firme, func(f FirmaPodrucja) bool { return f.Navodi(o.Firma) })
		switch {
		case i < 0:
			out.BezFirme = append(out.BezFirme, o)
		case o.Zamjenik:
			out.Firme[i].Zamjenici = append(out.Firme[i].Zamjenici, o)
		default:
			out.Firme[i].Rukovoditelji = append(out.Firme[i].Rukovoditelji, o)
		}
	}
	return out
}
