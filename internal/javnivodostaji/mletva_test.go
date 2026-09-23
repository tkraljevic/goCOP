package javnivodostaji

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"gocop/internal/models"
)

// Izmišljen mletva: prijava postavlja kolačić, a stranice bez njega
// preusmjere na prijavu, kao pravi sustav kad prijava istekne.
func laziMLetvu(t *testing.T, vodostaj, protok string) (*httptest.Server, *int) {
	t.Helper()
	prijava := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Account/LogOn" {
			r.ParseForm()
			if r.Form.Get("UserName") != "pero" || r.Form.Get("Password") != "tajna" {
				w.Write([]byte("<form>Prijava</form>"))
				return
			}
			prijava++
			http.SetCookie(w, &http.Cookie{Name: ".ASPXAUTH", Value: "da", Path: "/"})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if c, err := r.Cookie(".ASPXAUTH"); err != nil || c.Value != "da" {
			http.Redirect(w, r, "/Account/LogOn?ReturnUrl=%2f", http.StatusFound)
			return
		}
		switch r.URL.Path {
		case "/Home/PregledVodostajaPostaje":
			w.Write([]byte(vodostaj))
		case "/Home/PregledProtokaPostaje":
			w.Write([]byte(protok))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s, &prijava
}

func tablica(zaglavlje string, redci ...[3]string) string {
	var b strings.Builder
	b.WriteString(`<table><tr><td colspan="4">Maksimum: 2004 cm, 06.08.2023.g.</td></tr>` +
		`<tr><td><strong>Datum</strong></td><td><strong>Vrijeme</strong></td><td><strong>` + zaglavlje + `</strong></td></tr>`)
	for _, r := range redci {
		b.WriteString(`<tr><td><div class=""></div></td><td>` + r[0] + `</td><td>` + r[1] +
			`</td><td><span class="">` + r[2] + `</span></td><td><div class="blue"><span>0</span></div></td></tr>`)
	}
	return b.String() + "</table>"
}

func mletvaZaTest(s *httptest.Server) *MLetva {
	jar, _ := cookiejar.New(nil)
	return &MLetva{Base: s.URL, HTTP: &http.Client{Jar: jar},
		Racun: func() (string, string, bool) { return "pero", "tajna", true }}
}

// Elektrana nema vodostaj: protok je tada sam očitanje, i nosi oznaku da ga
// javlja elektrana, a ne da je preračunat krivuljom.
func TestMLetvaElektranaSamoProtok(t *testing.T) {
	s, _ := laziMLetvu(t, tablica("Vodostaj"),
		tablica("Protok", [3]string{"15.07.00.", "19:00", "300,50"}, [3]string{"15.07.00.", "18:00", "150,25"}))
	m := mletvaZaTest(s)
	adresa := "https://mletva.voda.hr/Home/PregledProtokaPostaje?sektorID=1&bpID=0&postajaID=662"
	if !m.Prepoznaje(adresa) {
		t.Fatal("adresa nije prepoznata")
	}
	r, err := m.Ocitanja(context.Background(), adresa)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].Kad.After(r[1].Kad) {
		t.Fatalf("redci %+v", r)
	}
	if r[1].FlowM3s == nil || *r[1].FlowM3s != 300.50 || r[1].LevelCm != nil {
		t.Errorf("zadnji redak %+v", r[1])
	}
	// 19:00 po ljetnom računanju vremena je 17:00 UTC
	if r[1].Kad.Hour() != 17 {
		t.Errorf("vrijeme %v, a treba 17:00 UTC", r[1].Kad)
	}
	if r[1].FlowMetoda != models.FlowMethodDrugo || !strings.Contains(r[1].FlowBiljeska, "elektrana") {
		t.Errorf("protok elektrane označen kao %q / %q", r[1].FlowMetoda, r[1].FlowBiljeska)
	}
}

// Letva s vodostajem dobiva protok po satu, označen kao krivulja; kota
// akumulacije (18000 cm n.m.) čita se kao i vodostaj.
func TestMLetvaVodostajSProtokom(t *testing.T) {
	s, _ := laziMLetvu(t,
		tablica("Vodostaj", [3]string{"15.07.00.", "20:00", "-50"}, [3]string{"15.07.00.", "19:00", "18000"}),
		tablica("Protok", [3]string{"15.07.00.", "20:00", "111,11"}))
	r, err := mletvaZaTest(s).Ocitanja(context.Background(),
		"https://mletva.voda.hr/Home/PregledVodostajaPostaje?sektorID=1&bpID=0&postajaID=286")
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || *r[0].LevelCm != 18000 || *r[1].LevelCm != -50 {
		t.Fatalf("redci %+v", r)
	}
	if r[1].FlowM3s == nil || *r[1].FlowM3s != 111.11 || r[1].FlowMetoda != models.FlowMethodKrivulja {
		t.Errorf("protok uz vodostaj %+v", r[1])
	}
}

// Kad sustav odbaci kolačić, klijent se prijavi iznova i ponovi zahtjev;
// dok kolačić vrijedi, ne prijavljuje se svaki put.
func TestMLetvaObnavljaPrijavu(t *testing.T) {
	s, prijava := laziMLetvu(t, tablica("Vodostaj", [3]string{"15.07.00.", "20:00", "10"}), tablica("Protok"))
	m := mletvaZaTest(s)
	adresa := "https://mletva.voda.hr/Home/PregledVodostajaPostaje?sektorID=1&bpID=0&postajaID=143"
	for i := 0; i < 2; i++ {
		if _, err := m.Ocitanja(context.Background(), adresa); err != nil {
			t.Fatal(err)
		}
	}
	if *prijava != 1 {
		t.Fatalf("%d prijava umjesto jedne", *prijava)
	}
	m.HTTP.Jar, _ = cookiejar.New(nil) // sustav je zaboravio prijavu
	if _, err := m.Ocitanja(context.Background(), adresa); err != nil {
		t.Fatal(err)
	}
	if *prijava != 2 {
		t.Fatalf("prijava nije obnovljena (%d)", *prijava)
	}
}

// Odbijena prijava javlja grešku, a ne praznu letvu.
func TestMLetvaKrivaLozinka(t *testing.T) {
	s, _ := laziMLetvu(t, "", "")
	m := mletvaZaTest(s)
	m.Racun = func() (string, string, bool) { return "pero", "kriva", true }
	_, err := m.Ocitanja(context.Background(), "https://mletva.voda.hr/Home/PregledVodostajaPostaje?sektorID=1&bpID=0&postajaID=143")
	if err == nil || !strings.Contains(err.Error(), "odbio prijavu") {
		t.Fatalf("greška %v", err)
	}
}
