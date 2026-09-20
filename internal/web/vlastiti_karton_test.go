package web

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Svoja zaduženja dio su profila: vodočuvar bez modula „Djelatnici” otvara
// svoj karton, a tuđi ni svoj za izmjenu ne otvara.
func TestVlastitiKartonProlaziBezModula(t *testing.T) {
	id := uuid.New()
	u := &models.User{ID: id}
	zahtjev := func(metoda, put string) bool {
		return vlastitiKarton(httptest.NewRequest(metoda, put, nil), u)
	}
	if !zahtjev("GET", "/users/"+id.String()) {
		t.Error("vlastiti karton ne prolazi")
	}
	if zahtjev("GET", "/users/"+uuid.New().String()) {
		t.Error("tuđi karton prolazi")
	}
	if zahtjev("GET", "/users") {
		t.Error("imenik prolazi")
	}
	if zahtjev("GET", "/users/"+id.String()+"/edit") {
		t.Error("izmjena vlastitog kartona prolazi")
	}
	if zahtjev("POST", "/users/"+id.String()) {
		t.Error("upis prolazi")
	}
	if vlastitiKarton(httptest.NewRequest("GET", "/users/"+id.String(), nil), nil) {
		t.Error("bez korisnika prolazi")
	}
}
