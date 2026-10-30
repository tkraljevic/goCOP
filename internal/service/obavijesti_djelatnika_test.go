package service_test

import (
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/service"
)

// Obavijesti o djelatnicima idu svim prijavljenima, pa nose samo vrstu
// promjene i oznaku računa, bez imena osobe; poništena lozinka javlja se
// kao i svaka druga izmjena podataka
func TestObavijestiDjelatnikaBezImena(t *testing.T) {
	o, _ := okolinaSektora(t)
	tok := service.NewSSEBroker()
	users := service.NewUserService(o.repo, o.auth, tok)
	ch := tok.Subscribe()
	const ime = "Ivka Primjerić"

	novi, err := users.CreateUser(o.admin, service.CreateUserRequest{Username: "ivka", Password: "lozinka-ivka", FullName: ime,
		OrgType: models.OrgHrvatskeVode})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.UpdateUser(o.admin, service.UpdateUserRequest{ID: novi.ID, Username: novi.Username, FullName: ime,
		OrgType: novi.OrgType, Phone: "031 000 000", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := users.ResetPassword(o.admin, novi.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := users.DeleteUser(o.admin, novi.ID); err != nil {
		t.Fatal(err)
	}
	poruke := make([]string, 0, 4)
	for i := range 4 {
		select {
		case poruka := <-ch:
			poruke = append(poruke, poruka)
			if strings.Contains(poruka, "Ivka") {
				t.Errorf("obavijest %d nosi ime: %s", i+1, poruka)
			}
			if !strings.Contains(poruka, novi.ID.String()) {
				t.Errorf("obavijest %d bez oznake računa: %s", i+1, poruka)
			}
		default:
			t.Fatalf("stiglo je %d obavijesti od 4", i)
		}
	}
	if izmjena := "Ažurirani su podaci djelatnika"; !strings.Contains(poruke[1], izmjena) || !strings.Contains(poruke[2], izmjena) {
		t.Errorf("poništena lozinka razlikuje se od izmjene podataka: %q, %q", poruke[1], poruke[2])
	}
}
