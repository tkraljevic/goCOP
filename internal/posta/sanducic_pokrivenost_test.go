package posta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Sandučić, adresar i radnje nad pismima kroz klijent, s lažnim Exchangeom
// (probni EWS) ili poslužiteljem koji vraća zadani odgovor.

// sandučićProbe: probni EWS s pismima i postavke klijenta za njega
func sanducicProbe(t *testing.T) (*ProbniPosluzitelj, Postavke, Racun) {
	t.Helper()
	srv := probniEWS(t)
	pismaZaProbu(srv)
	return srv, srv.Postavke(), Racun{Korisnik: srv.Korisnik, Lozinka: srv.Lozinka}
}

// zadaniEWS je poslužitelj s prijavom Basic koji na svaki zahtjev vrati što
// odgovor kaže; bez prijave je 401.
func zadaniEWS(t *testing.T, odgovor func(tijelo string) (int, string)) (Postavke, Racun) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if k, l, ok := r.BasicAuth(); !ok || k != "mkovac" || l != "lozinka-9" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		status, tijelo := odgovor(b.String())
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(tijelo))
	}))
	t.Cleanup(srv.Close)
	p := Postavke{Nacin: NacinEWS, Posluzitelj: srv.URL + "/EWS/Exchange.asmx", DopustiBasic: true, Istek: 10 * time.Second,
		TLS: srv.Client().Transport.(*http.Transport).TLSClientConfig}
	return p, Racun{Korisnik: "mkovac", Lozinka: "lozinka-9"}
}

func omot(s string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` + s + `</s:Body></s:Envelope>`
}

// Popis pisma: najnovije iz mape, s pošiljateljem, vremenom, oznakom
// pročitanog i pregledom; pretraga i mapa idu poslužitelju.
func TestSanducicPopisPisama(t *testing.T) {
	_, p, r := sanducicProbe(t)
	ctx := context.Background()
	pisma, ukupno, err := Sanducic(ctx, p, r, "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ukupno != 2 || len(pisma) != 2 {
		t.Fatalf("inbox: %d pisama, ukupno %d", len(pisma), ukupno)
	}
	p1 := pisma[0]
	kad := time.Date(2026, 3, 14, 7, 30, 0, 0, time.UTC)
	if p1.ID != "p1" || p1.ChangeKey != "ck1" || p1.Predmet != "Vodostaj Batina" || p1.Od != "Ivana Horvat" || p1.OdAdresa != "ivana.horvat@voda.hr" ||
		!p1.Kad.Equal(kad) || p1.Procitano || p1.ImaPrivitke || p1.Velicina != 120 || p1.Pregled != "Dunav raste\n5 cm & više" {
		t.Errorf("prvo pismo: %+v", p1)
	}
	if p2 := pisma[1]; p2.ID != "p2" || !p2.Procitano || !p2.ImaPrivitke || !p2.Kad.Equal(kad.Add(time.Hour)) {
		t.Errorf("drugo pismo: %+v", p2)
	}

	nadjena, n, err := Sanducic(ctx, p, r, "inbox", "  kovač ", 0, 10)
	if err != nil || n != 1 || len(nadjena) != 1 || nadjena[0].ID != "p2" {
		t.Errorf("pretraga: %+v %d %v", nadjena, n, err)
	}
	arhiva, n, err := Sanducic(ctx, p, r, "archive", "", 0, 10)
	if err != nil || n != 1 || arhiva[0].ID != "p3" {
		t.Errorf("arhiva: %+v %d %v", arhiva, n, err)
	}
	prazna, n, err := Sanducic(ctx, p, r, "AAMk-moja", "", 0, 10)
	if err != nil || n != 0 || prazna != nil {
		t.Errorf("prazna korisnikova mapa: %+v %d %v", prazna, n, err)
	}

	if _, _, err := Sanducic(ctx, p, Racun{Korisnik: r.Korisnik, Lozinka: "kriva"}, "inbox", "", 0, 10); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka: %v", err)
	}
	smtp := Postavke{Nacin: NacinSMTP, Posluzitelj: "mail.voda.hr"}
	if _, _, err := Sanducic(ctx, smtp, r, "inbox", "", 0, 10); err == nil || !strings.Contains(err.Error(), "Exchange Web Services") {
		t.Errorf("SMTP nema sandučić: %v", err)
	}
}

// Zahtjev popisa nosi stranicu, zadanu veličinu 50, ugrađenu ili
// korisnikovu mapu i pretragu kodiranu za XML.
func TestSanducicZahtjev(t *testing.T) {
	var zahtjevi []string
	p, r := zadaniEWS(t, func(tijelo string) (int, string) {
		zahtjevi = append(zahtjevi, tijelo)
		return 200, omot(`<m:FindItemResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder TotalItemsInView="7"><t:Items/></m:RootFolder></m:FindItemResponseMessage></m:ResponseMessages></m:FindItemResponse>`)
	})
	ctx := context.Background()
	if _, n, err := Sanducic(ctx, p, r, "", "a<b & \"c\"", 20, 0); err != nil || n != 7 {
		t.Fatalf("%d %v", n, err)
	}
	if _, _, err := Sanducic(ctx, p, r, `AAMk"x`, " ", 0, 5); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`MaxEntriesReturned="50" Offset="20"`, `<t:DistinguishedFolderId Id="inbox"/>`, `<m:QueryString>a&lt;b &amp; &#34;c&#34;</m:QueryString>`} {
		if !strings.Contains(zahtjevi[0], s) {
			t.Errorf("prvi zahtjev nema %s", s)
		}
	}
	for _, s := range []string{`MaxEntriesReturned="5" Offset="0"`, `<t:FolderId Id="AAMk&#34;x"/>`} {
		if !strings.Contains(zahtjevi[1], s) {
			t.Errorf("drugi zahtjev nema %s", s)
		}
	}
	if strings.Contains(zahtjevi[1], "QueryString") {
		t.Error("prazna pretraga ne smije ići poslužitelju")
	}
}

// Otvoreno pismo: HTML tijelo, tekst, primatelji, Message-ID i privitci;
// ugrađena slika prepoznaje se po oznaci poslužitelja.
func TestProcitajPismo(t *testing.T) {
	_, p, r := sanducicProbe(t)
	ctx := context.Background()
	p2, err := ProcitajPismo(ctx, p, r, "p2")
	if err != nil {
		t.Fatal(err)
	}
	if p2.HTML != "<p>Raspored</p>" || p2.Tekst != "Raspored" || p2.Predmet != "Dežurstvo" || len(p2.Za) != 1 || p2.Za[0] != `VODA\ihorvat` {
		t.Errorf("pismo: %+v", p2)
	}
	if len(p2.Privitci) != 1 {
		t.Fatalf("privitci: %+v", p2.Privitci)
	}
	if a := p2.Privitci[0]; a.ID != "a1" || a.Ime != "raspored.pdf" || !a.JePDF() || a.JeSlika() || a.Velicina != 4 || !a.Ugradjen || a.ContentID != "c<1" {
		t.Errorf("privitak: %+v", a)
	}
	p1, err := ProcitajPismo(ctx, p, r, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if p1.MessageID != "<p1@voda.hr>" || p1.Tekst != "Dunav raste\n5 cm & više" || !strings.HasPrefix(p1.HTML, "<html>") || p1.Privitci != nil {
		t.Errorf("pismo bez HTML-a: %+v", p1)
	}
	if _, err := ProcitajPismo(ctx, p, r, "p9"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("nepoznato pismo: %v", err)
	}
}

// Odgovor bez pisma ili nečitljiv odgovor daju grešku, a ne prazno pismo.
func TestProcitajPismoBezPisma(t *testing.T) {
	odgovor := ""
	p, r := zadaniEWS(t, func(string) (int, string) { return 200, odgovor })
	odgovor = omot(`<m:GetItemResponse xmlns:m="m"><m:ResponseMessages><m:GetItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Items/></m:GetItemResponseMessage></m:ResponseMessages></m:GetItemResponse>`)
	if _, err := ProcitajPismo(context.Background(), p, r, "x"); err == nil || err.Error() != "pismo nije pronađeno" {
		t.Errorf("bez pisma: %v", err)
	}
	odgovor = `<?xml version="1.0"?><nije zatvoreno`
	if _, err := ProcitajPismo(context.Background(), p, r, "x"); err == nil {
		t.Error("nečitljiv odgovor mora biti greška")
	}
}

// Privitak dolazi kao base64, i s prijelomima redaka; nepoznat ili
// pokvaren privitak je greška.
func TestPreuzmiPrivitak(t *testing.T) {
	_, p, r := sanducicProbe(t)
	ctx := context.Background()
	a, b, err := PreuzmiPrivitak(ctx, p, r, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "%PDF" || a.ID != "a1" || a.Ime != "raspored.pdf" || a.Vrsta != "application/pdf" || a.Velicina != 4 {
		t.Errorf("privitak %+v %q", a, b)
	}
	if _, _, err := PreuzmiPrivitak(ctx, p, r, "a9"); err == nil {
		t.Error("nepoznat privitak mora biti greška")
	}

	sadrzaj := ""
	pz, rz := zadaniEWS(t, func(string) (int, string) {
		return 200, omot(`<m:GetAttachmentResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetAttachmentResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Attachments>` +
			sadrzaj + `</m:Attachments></m:GetAttachmentResponseMessage></m:ResponseMessages></m:GetAttachmentResponse>`)
	})
	sadrzaj = "<t:FileAttachment><t:Name>slika.png</t:Name><t:ContentType>image/png</t:ContentType><t:Content>aGVs\r\nbG8g\n  c3Zp amV0ZQ==</t:Content></t:FileAttachment>"
	if a, b, err := PreuzmiPrivitak(ctx, pz, rz, "x"); err != nil || string(b) != "hello svijete" || a.Velicina != 13 || !a.JeSlika() {
		t.Errorf("base64 s prijelomima: %+v %q %v", a, b, err)
	}
	sadrzaj = "<t:FileAttachment><t:Name>x</t:Name><t:Content>nije*base64</t:Content></t:FileAttachment>"
	if _, _, err := PreuzmiPrivitak(ctx, pz, rz, "x"); err == nil || !strings.Contains(err.Error(), "nije čitljiv") {
		t.Errorf("pokvaren base64: %v", err)
	}
	sadrzaj = ""
	if _, _, err := PreuzmiPrivitak(ctx, pz, rz, "x"); err == nil || err.Error() != "privitak nije pronađen" {
		t.Errorf("bez privitka: %v", err)
	}
}

// Označavanje pročitanog mijenja pismo na poslužitelju; zastarjeli ključ
// promjene je greška, a prazan popis ne šalje ništa.
func TestOznaciProcitano(t *testing.T) {
	srv, p, r := sanducicProbe(t)
	ctx := context.Background()
	if err := OznaciProcitano(ctx, p, r, "p1", "ck1", true); err != nil || !srv.Pisma[0].Procitano {
		t.Fatalf("pročitano: %v %+v", err, srv.Pisma[0])
	}
	if err := OznaciProcitanoVise(ctx, p, r, []Stavka{{"p1", "ck1"}, {"p2", "ck1"}}, false); err != nil || srv.Pisma[0].Procitano || srv.Pisma[1].Procitano {
		t.Fatalf("nepročitano: %v %+v", err, srv.Pisma)
	}
	if err := OznaciProcitano(ctx, p, r, "p1", "ck0", true); err == nil || !strings.Contains(err.Error(), "ErrorIrresolvableConflict") {
		t.Errorf("zastarjeli ključ: %v", err)
	}
	if err := OznaciProcitanoVise(ctx, p, Racun{Korisnik: "nitko", Lozinka: "x"}, nil, true); err != nil {
		t.Errorf("prazan popis: %v", err)
	}
}

// Premještanje u ugrađenu i korisnikovu mapu, brisanje u Obrisano i
// trajno brisanje; bez mape Arhiva korisnik dobije uputu.
func TestPremjestiIBrisi(t *testing.T) {
	srv, p, r := sanducicProbe(t)
	ctx := context.Background()
	if err := Premjesti(ctx, p, r, []string{"p1"}, "archive"); err == nil || !strings.Contains(err.Error(), "nema mape Arhiva") {
		t.Errorf("bez arhive: %v", err)
	}
	if err := Premjesti(ctx, p, r, []string{"p1", "p2"}, "AAMk-zapisnici"); err != nil || srv.Mapa["p1"] != "AAMk-zapisnici" || srv.Mapa["p2"] != "AAMk-zapisnici" {
		t.Errorf("u korisnikovu mapu: %v %v", err, srv.Mapa)
	}
	if err := Obrisi(ctx, p, r, "p2"); err != nil || srv.Mapa["p2"] != "deleteditems" {
		t.Errorf("u Obrisano: %v %v", err, srv.Mapa)
	}
	if err := ObrisiTrajno(ctx, p, r, []string{"p2", "p3"}); err != nil || len(srv.Pisma) != 1 || srv.Pisma[0].ID != "p1" {
		t.Errorf("trajno: %v %+v", err, srv.Pisma)
	}
	if err := Premjesti(ctx, p, r, nil, "inbox"); err != nil {
		t.Errorf("prazan premještaj: %v", err)
	}
	if err := ObrisiTrajno(ctx, p, r, nil); err != nil {
		t.Errorf("prazno brisanje: %v", err)
	}
	// druga greška premještaja ostaje kakva jest, i za arhivu
	pz, rz := zadaniEWS(t, func(string) (int, string) {
		return 200, omot(`<m:MoveItemResponse xmlns:m="m"><m:ResponseMessages><m:MoveItemResponseMessage ResponseClass="Error"><m:MessageText>Access is denied.</m:MessageText><m:ResponseCode>ErrorAccessDenied</m:ResponseCode></m:MoveItemResponseMessage></m:ResponseMessages></m:MoveItemResponse>`)
	})
	if err := Premjesti(ctx, pz, rz, []string{"x"}, "archive"); err == nil || err.Error() != "Exchange: Access is denied. (ErrorAccessDenied)" {
		t.Errorf("odbijen premještaj: %v", err)
	}
}

// Mape sandučića: ugrađene s brojevima, Arhiva samo kad postoji, i
// korisnikove mape bez sustavnih.
func TestSveMape(t *testing.T) {
	srv, p, r := sanducicProbe(t)
	srv.Korisnikove = map[string]string{"AAMk-1": "Zapisnici"}
	ctx := context.Background()
	mape, err := SveMape(ctx, p, r)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range mape {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); got != "inbox,sentitems,drafts,deleteditems,junkemail,AAMk-1" {
		t.Errorf("mape bez arhive: %s", got)
	}
	if m := mape[0]; m.Naziv != "Ulazna pošta" || m.Ukupno != 2 || m.Neprocitano != 1 || !m.Ugradjena {
		t.Errorf("ulazna pošta: %+v", m)
	}
	if m := mape[5]; m.Naziv != "Zapisnici" || m.Ugradjena {
		t.Errorf("korisnikova mapa: %+v", m)
	}

	srv.ImaArhivu = true
	srv.Korisnikove = map[string]string{"AAMk-2": "Sync Issues"}
	mape, err = SveMape(ctx, p, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(mape) != 6 || mape[3].ID != "archive" || mape[3].Ukupno != 1 || mape[3].Neprocitano != 1 {
		t.Errorf("s arhivom, bez sustavne mape: %+v", mape)
	}
	if _, err := SveMape(ctx, p, Racun{Korisnik: r.Korisnik, Lozinka: "kriva"}); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka: %v", err)
	}
}

// Kad popis korisnikovih mapa ne uspije ili je nečitljiv, ugrađene mape
// ipak stižu; kalendari, kontakti i mape istog imena kao ugrađene se
// preskaču; nečitljiv prvi odgovor je greška.
func TestSveMapeRubovi(t *testing.T) {
	mape := `<t:Folder><t:FolderId Id="k1"/><t:DisplayName>Kalendar tima</t:DisplayName><t:FolderClass>IPF.Appointment</t:FolderClass></t:Folder>` +
		`<t:Folder><t:FolderId Id="k2"/><t:DisplayName>Poslano</t:DisplayName><t:FolderClass>IPF.Note</t:FolderClass></t:Folder>` +
		`<t:Folder><t:FolderId Id="k3"/><t:DisplayName>Obrana 2026</t:DisplayName><t:UnreadCount>3</t:UnreadCount><t:TotalCount>9</t:TotalCount></t:Folder>`
	nacin := "dobro"
	p, r := zadaniEWS(t, func(tijelo string) (int, string) {
		if strings.Contains(tijelo, "<m:GetFolder>") {
			if nacin == "pokvareno" {
				// ispravan XML, ali broj nepročitanih nije broj
				return 200, omot(`<m:GetFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetFolderResponseMessage ResponseClass="Success"><m:Folders><t:Folder><t:UnreadCount>mnogo</t:UnreadCount></t:Folder></m:Folders></m:GetFolderResponseMessage></m:ResponseMessages></m:GetFolderResponse>`)
			}
			// samo prva poruka; ostale mape bez podataka
			return 200, omot(`<m:GetFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetFolderResponseMessage ResponseClass="Success"><m:Folders><t:Folder><t:UnreadCount>4</t:UnreadCount><t:TotalCount>10</t:TotalCount></t:Folder></m:Folders></m:GetFolderResponseMessage></m:ResponseMessages></m:GetFolderResponse>`)
		}
		switch nacin {
		case "pao":
			return 500, "greška"
		case "nečitljivo":
			return 200, omot(`<m:FindFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:RootFolder><t:Folders><t:Folder><t:TotalCount>puno</t:TotalCount></t:Folder></t:Folders></m:RootFolder></m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse>`)
		}
		return 200, omot(`<m:FindFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:RootFolder><t:Folders>` + mape + `</t:Folders></m:RootFolder></m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse>`)
	})
	ctx := context.Background()
	out, err := SveMape(ctx, p, r)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range out {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); got != "inbox,sentitems,drafts,deleteditems,junkemail,k3" {
		t.Errorf("mape: %s", got)
	}
	if out[0].Ukupno != 10 || out[0].Neprocitano != 4 || out[1].Ukupno != 0 || out[5].Neprocitano != 3 || out[5].Ukupno != 9 {
		t.Errorf("brojevi: %+v", out)
	}
	for _, n := range []string{"pao", "nečitljivo"} {
		nacin = n
		out, err := SveMape(ctx, p, r)
		if err != nil || len(out) != 5 {
			t.Errorf("%s: %d mapa, %v", n, len(out), err)
		}
	}
	nacin = "pokvareno"
	if _, err := SveMape(ctx, p, r); err == nil || !strings.Contains(err.Error(), "nije čitljiv") {
		t.Errorf("nečitljive mape: %v", err)
	}
}

// Greške Exchangea dolaze do korisnika s porukom i kodom: SOAP fault,
// odbijen zahtjev bez poruke, nečitljiv odgovor i HTTP greška.
func TestEWSSirovoGreske(t *testing.T) {
	odgovor, status := "", 200
	p, r := zadaniEWS(t, func(string) (int, string) { return status, odgovor })
	ctx := context.Background()
	for _, c := range []struct {
		status        int
		odgovor, want string
	}{
		{500, omot(`<s:Fault><faultcode>a:ErrorSchemaValidation</faultcode><faultstring>The request failed schema validation.</faultstring></s:Fault>`), "Exchange: The request failed schema validation."},
		{200, omot(`<m:X xmlns:m="m"><m:R ResponseClass="Error"><m:ResponseCode>ErrorServerBusy</m:ResponseCode></m:R></m:X>`), "Exchange nije izvršio zahtjev (ErrorServerBusy)"},
		{200, omot(`<m:X xmlns:m="m"><m:R ResponseClass="Warning"><m:MessageText>Pazi</m:MessageText><m:ResponseCode>ErrorX</m:ResponseCode></m:R></m:X>`), "Exchange: Pazi (ErrorX)"},
		{503, "Service Unavailable", "je odgovorio 503 Service Unavailable"},
		{200, "<a><b></a>", "je odgovorio 200 OK"},
	} {
		status, odgovor = c.status, c.odgovor
		if _, err := ewsSirovo(ctx, p, r, "<m:Bilo/>"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d %q: %v, želim %q", c.status, c.odgovor, err, c.want)
		}
	}
	status, odgovor = 200, omot(`<m:X xmlns:m="m"><m:R ResponseClass="Success"/><m:R ResponseClass="Success"/></m:X>`)
	if b, err := ewsSirovo(ctx, p, r, "<m:Bilo/>"); err != nil || !strings.Contains(string(b), "ResponseClass") {
		t.Errorf("uspjeh: %v", err)
	}
	if _, err := ewsSirovo(ctx, p, Racun{Korisnik: "mkovac", Lozinka: "kriva"}, "<m:Bilo/>"); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka: %v", err)
	}
}

// Adresar: osobe s telefonima, funkcijom i odjelom, adresa malim slovima;
// bez rezultata prazno, bez greške.
func TestImenikPrekoProbnogEWS(t *testing.T) {
	srv, p, r := sanducicProbe(t)
	srv.Adresar = []Kontakt{
		{Ime: "Ivana Horvat", Email: "Ivana.Horvat@voda.hr", Telefon: "031 000 001", Mobitel: "091 000 001", Funkcija: "Voditeljica", Odjel: "Obrana & zaštita"},
		{Ime: "Marko Kovač", Email: "marko.kovac@voda.hr"},
	}
	ctx := context.Background()
	k, err := Imenik(ctx, p, r, "  horvat ")
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 1 {
		t.Fatalf("nađeno %+v", k)
	}
	if want := (Kontakt{Ime: "Ivana Horvat", Email: "ivana.horvat@voda.hr", Telefon: "031 000 001", Mobitel: "091 000 001", Funkcija: "Voditeljica", Odjel: "Obrana & zaštita"}); k[0] != want {
		t.Errorf("kontakt %+v", k[0])
	}
	if k, err := Imenik(ctx, p, r, "Babić"); err != nil || k != nil {
		t.Errorf("nema nikoga: %+v %v", k, err)
	}
	if k, err := Imenik(ctx, p, r, "   "); err != nil || k != nil {
		t.Errorf("prazan upit: %+v %v", k, err)
	}
	if _, err := Imenik(ctx, Postavke{Nacin: NacinSMTP, Posluzitelj: "mail.voda.hr"}, r, "x"); err == nil || !strings.Contains(err.Error(), "adresar se čita samo preko Exchange Web Services") {
		t.Errorf("SMTP nema adresar: %v", err)
	}
	if _, err := Imenik(ctx, p, Racun{Korisnik: r.Korisnik, Lozinka: "kriva"}, "horvat"); !errors.Is(err, ErrPrijava) {
		t.Errorf("kriva lozinka: %v", err)
	}
}

// Adresar: ime iz sandučića kad kontakt nema ime, glavni telefon kad nema
// poslovnog, više rezultata nije greška, a druga greška jest.
func TestImenikOdgovori(t *testing.T) {
	odgovor := ""
	p, r := zadaniEWS(t, func(string) (int, string) { return 200, odgovor })
	poruka := func(ishod, kod, rez string) string {
		return omot(`<m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:ResolveNamesResponseMessage ResponseClass="` + ishod + `"><m:MessageText>Poruka ` + kod +
			`</m:MessageText><m:ResponseCode>` + kod + `</m:ResponseCode><m:ResolutionSet>` + rez + `</m:ResolutionSet></m:ResolveNamesResponseMessage></m:ResponseMessages></m:ResolveNamesResponse>`)
	}
	rez := `<t:Resolution><t:Mailbox><t:Name>Ana Babić</t:Name><t:EmailAddress>ANA.BABIC@voda.hr</t:EmailAddress></t:Mailbox><t:Contact><t:PhoneNumbers>` +
		`<t:Entry Key="PrimaryPhone"> 031 000 010 </t:Entry><t:Entry Key="BusinessPhone">031 000 011</t:Entry><t:Entry Key="MobilePhone"> </t:Entry><t:Entry Key="HomePhone">01 000 012</t:Entry></t:PhoneNumbers>` +
		`<t:OfficeLocation>Osijek</t:OfficeLocation><t:CompanyName>Hrvatske vode</t:CompanyName></t:Contact></t:Resolution>` +
		`<t:Resolution><t:Mailbox><t:Name>Ana Anić</t:Name><t:EmailAddress>ana.anic@voda.hr</t:EmailAddress></t:Mailbox><t:Contact><t:DisplayName>Ana Anić (VGO)</t:DisplayName></t:Contact></t:Resolution>`
	odgovor = poruka("Warning", "ErrorNameResolutionMultipleResults", rez)
	k, err := Imenik(context.Background(), p, r, "ana")
	if err != nil {
		t.Fatal(err)
	}
	want := []Kontakt{
		{Ime: "Ana Babić", Email: "ana.babic@voda.hr", Telefon: "031 000 010", Ured: "Osijek", Tvrtka: "Hrvatske vode"},
		{Ime: "Ana Anić (VGO)", Email: "ana.anic@voda.hr"},
	}
	if len(k) != 2 || k[0] != want[0] || k[1] != want[1] {
		t.Errorf("kontakti %+v", k)
	}
	odgovor = poruka("Error", "ErrorAccessDenied", "")
	if _, err := Imenik(context.Background(), p, r, "ana"); err == nil || err.Error() != "Exchange: Poruka ErrorAccessDenied" {
		t.Errorf("odbijen upit: %v", err)
	}
	odgovor = omot(`<m:ResolveNamesResponse xmlns:m="m"><m:ResponseMessages/></m:ResolveNamesResponse>`)
	if _, err := Imenik(context.Background(), p, r, "ana"); err == nil || !strings.Contains(err.Error(), "nije odgovorio") {
		t.Errorf("bez poruke: %v", err)
	}
}

// Pismo iz odgovora Exchangea: tekstno tijelo, tekst iz HTML-a, adresa
// kad nema imena, vrijeme slanja kad nema vremena primitka, kopije, i
// ugrađena slika po pozivu cid: u tijelu.
func TestPismoIzOdgovora(t *testing.T) {
	var m ewsMessage
	m.Body = ewsBody{Tip: "Text", Tekst: "običan tekst"}
	m.From.Mailbox = ewsMailbox{EmailAddress: "dezurni@voda.hr"}
	m.DateTimeSent = "2026-10-30T06:15:00+01:00"
	m.To = []ewsMailbox{{Name: "Ivana Horvat", EmailAddress: "ivana.horvat@voda.hr"}, {Name: "marko.kovac@voda.hr", EmailAddress: "marko.kovac@voda.hr"}}
	m.Cc = []ewsMailbox{{EmailAddress: "arhiva@voda.hr"}}
	p := m.pismo()
	if p.Tekst != "običan tekst" || p.HTML != "" || p.Od != "dezurni@voda.hr" || !p.Kad.Equal(time.Date(2026, 10, 30, 5, 15, 0, 0, time.UTC)) {
		t.Errorf("tekstno pismo: %+v", p)
	}
	if strings.Join(p.Za, "|") != "Ivana Horvat <ivana.horvat@voda.hr>|marko.kovac@voda.hr" || strings.Join(p.Kopija, "|") != "arhiva@voda.hr" {
		t.Errorf("primatelji %q, kopija %q", p.Za, p.Kopija)
	}

	var h ewsMessage
	h.Body = ewsBody{Tip: "html", Tekst: `<p>Vodostaj <b>Batina</b></p><img src="cid:logo">`}
	h.DateTimeReceived = "nije vrijeme"
	h.Attachments = append(h.Attachments, struct {
		AttachmentId struct {
			Id string `xml:"Id,attr"`
		} `xml:"AttachmentId"`
		Name        string `xml:"Name"`
		ContentType string `xml:"ContentType"`
		ContentId   string `xml:"ContentId"`
		IsInline    bool   `xml:"IsInline"`
		Size        int    `xml:"Size"`
	}{Name: "logo.png", ContentType: "image/png", ContentId: "<logo>"}, struct {
		AttachmentId struct {
			Id string `xml:"Id,attr"`
		} `xml:"AttachmentId"`
		Name        string `xml:"Name"`
		ContentType string `xml:"ContentType"`
		ContentId   string `xml:"ContentId"`
		IsInline    bool   `xml:"IsInline"`
		Size        int    `xml:"Size"`
	}{Name: "zapisnik.PDF", ContentType: "application/octet-stream", ContentId: "nepozvan"})
	ph := h.pismo()
	if ph.Tekst != "Vodostaj Batina" || !ph.Kad.IsZero() {
		t.Errorf("HTML pismo: tekst %q, kad %v", ph.Tekst, ph.Kad)
	}
	if len(ph.Privitci) != 2 || !ph.Privitci[0].Ugradjen || ph.Privitci[0].ContentID != "logo" || ph.Privitci[1].Ugradjen || !ph.Privitci[1].JePDF() {
		t.Errorf("privitci %+v", ph.Privitci)
	}
}

// Ugrađene slike dobiju adresu s koje se privitak poslužuje, u oba oblika
// navodnika; privitak bez oznake i pismo bez HTML-a ostaju isti.
func TestUgradiSlike(t *testing.T) {
	p := Pismo{HTML: `<img src="cid:logo"><img src='cid:potpis'><img src="cid:nema">`, Privitci: []PrivitakPisma{
		{ID: "A1", ContentID: "logo"}, {ID: "A2", ContentID: "potpis"}, {ID: "A3"},
	}}
	adresa := func(id string) string { return "/posta/privitak?id=" + id }
	p.UgradiSlike(adresa)
	if want := `<img src="/posta/privitak?id=A1"><img src="/posta/privitak?id=A2"><img src="cid:nema">`; p.HTML != want {
		t.Errorf("HTML %s", p.HTML)
	}
	prazno := Pismo{Privitci: []PrivitakPisma{{ID: "A1", ContentID: "logo"}}}
	prazno.UgradiSlike(func(string) string { t.Error("bez HTML-a se adresa ne traži"); return "" })
	if prazno.HTML != "" {
		t.Error("pismo bez HTML-a mora ostati bez HTML-a")
	}
}

// Vrsta privitka: slika po MIME vrsti, PDF po vrsti ili nastavku imena.
func TestVrstaPrivitka(t *testing.T) {
	for _, c := range []struct {
		p           PrivitakPisma
		slika, pdfa bool
	}{
		{PrivitakPisma{Vrsta: "IMAGE/JPEG"}, true, false},
		{PrivitakPisma{Vrsta: "Application/PDF", Ime: "akt"}, false, true},
		{PrivitakPisma{Vrsta: "application/octet-stream", Ime: "Rješenje.Pdf"}, false, true},
		{PrivitakPisma{Vrsta: "text/plain", Ime: "pdf.txt"}, false, false},
	} {
		if c.p.JeSlika() != c.slika || c.p.JePDF() != c.pdfa {
			t.Errorf("%+v: slika %v, PDF %v", c.p, c.p.JeSlika(), c.p.JePDF())
		}
	}
}

// Tekst iz HTML-a: bez skripti, stilova i zaglavlja, prijelomi na kraju
// odlomka i retka, znakovni entiteti, bez praznih redaka.
func TestTekstIzHTML(t *testing.T) {
	h := `<html><head><title>x</title></head><body><style>p{color:red}</style><script>alert(1)</script>` +
		`<h1>Obavijest</h1><p>Vodostaj&nbsp;Batina &gt; 500 cm &amp; raste</p><div>  </div>red<br/>drugi<BR>` +
		`<ul><li>&quot;prvi&quot;</li><li>&#39;drugi&#39; &lt;3</li></ul></body></html>`
	want := "Obavijest\nVodostaj Batina > 500 cm & raste\nred\ndrugi\n\"prvi\"\n'drugi' <3"
	if got := TekstIzHTML(h); got != want {
		t.Errorf("tekst:\n%q\nželim\n%q", got, want)
	}
	if TekstIzHTML("") != "" || TekstIzHTML("<p> </p>") != "" {
		t.Error("prazno ostaje prazno")
	}
}

// Ugrađena mapa ide po imenu, a korisnikova po Id-u kodiranom za XML.
func TestFolderID(t *testing.T) {
	if got := folderID("junkemail"); got != `<t:DistinguishedFolderId Id="junkemail"/>` {
		t.Error(got)
	}
	if got := folderID(`AAMk<&"`); got != `<t:FolderId Id="AAMk&lt;&amp;&#34;"/>` {
		t.Error(got)
	}
	if MapaPostoji("Inbox") || !MapaPostoji("inbox") || MapaPostoji("") {
		t.Error("ugrađene mape po točnom imenu")
	}
}
