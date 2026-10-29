package posta

import (
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Testovi lažnog Exchangea: šalju se pravi SOAP zahtjevi, a odgovor se
// uspoređuje cijeli, jer ga klijent čita kao Exchangeov.

const probniPocetak = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>`
const probniKraj = `</s:Body></s:Envelope>`

func probniOmot(s string) string { return probniPocetak + s + probniKraj }

// probniKratki je odgovor bez podataka, samo ishod i poruka
func probniKratki(vrsta, ishod, tekst, kod string) string {
	return probniOmot(`<m:` + vrsta + `Response xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:` + vrsta + `ResponseMessage ResponseClass="` + ishod +
		`"><m:MessageText>` + tekst + `</m:MessageText><m:ResponseCode>` + kod + `</m:ResponseCode></m:` + vrsta + `ResponseMessage></m:ResponseMessages></m:` + vrsta + `Response>`)
}

func probniZahtjev(tijelo string) string {
	return `<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:t="t" xmlns:m="m"><soap:Body>` +
		tijelo + `</soap:Body></soap:Envelope>`
}

type probniIshod struct {
	status    int
	zaglavlje http.Header
	tijelo    string
}

func probniPosalji(t *testing.T, srv *ProbniPosluzitelj, korisnik, lozinka, tijelo string) probniIshod {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.Postavke().Posluzitelj, strings.NewReader(tijelo))
	if err != nil {
		t.Fatal(err)
	}
	if korisnik != "" {
		req.SetBasicAuth(korisnik, lozinka)
	}
	res, err := srv.web.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return probniIshod{res.StatusCode, res.Header, string(b)}
}

func probniEWS(t *testing.T) *ProbniPosluzitelj {
	t.Helper()
	srv, err := PokreniProbniEWS(`VODA\ihorvat`, "tajna-7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Zatvori)
	return srv
}

func (s *ProbniPosluzitelj) probniPitaj(t *testing.T, tijelo string) string {
	t.Helper()
	o := probniPosalji(t, s, s.Korisnik, s.Lozinka, probniZahtjev(tijelo))
	if o.status != http.StatusOK || o.zaglavlje.Get("Content-Type") != "text/xml; charset=utf-8" {
		t.Fatalf("status %d, vrsta %q", o.status, o.zaglavlje.Get("Content-Type"))
	}
	return o.tijelo
}

// pismaZaProbu: dva u inboxu (jedno pročitano, s privitkom), jedno u arhivi
func pismaZaProbu(s *ProbniPosluzitelj) {
	kad := time.Date(2026, 3, 14, 8, 30, 0, 0, time.FixedZone("CET", 3600))
	s.Pisma = []Pismo{
		{ID: "p1", MessageID: "<p1@voda.hr>", Predmet: "Vodostaj Batina", Od: "Ivana Horvat", OdAdresa: "ivana.horvat@voda.hr", Kad: kad, Velicina: 120, Tekst: "Dunav raste\n5 cm & više"},
		{ID: "p2", Predmet: "Dežurstvo", Od: "Marko Kovač", OdAdresa: "marko.kovac@voda.hr", Kad: kad.Add(time.Hour), Procitano: true, Velicina: 900,
			HTML: "<p>Raspored</p>", Tekst: "Raspored", Privitci: []PrivitakPisma{{ID: "a1", Ime: "raspored.pdf", Vrsta: "application/pdf", Velicina: 4, ContentID: "c<1>", Ugradjen: true}}},
		{ID: "p3", Predmet: "Stari zapisnik", Od: "Ana Babić", OdAdresa: "ana.babic@voda.hr", Kad: kad.Add(-24 * time.Hour), Tekst: "zapisnik"},
	}
	s.Mapa = map[string]string{"p3": "archive"}
	s.Datoteke = map[string][]byte{"a1": []byte("%PDF")}
}

func TestProbniEWSPrijava(t *testing.T) {
	srv := probniEWS(t)
	for _, c := range []struct{ ime, korisnik, lozinka string }{
		{"bez prijave", "", ""},
		{"kriva lozinka", `VODA\ihorvat`, "kriva"},
		{"krivi korisnik", `VODA\netko`, "tajna-7"},
	} {
		o := probniPosalji(t, srv, c.korisnik, c.lozinka, probniZahtjev(ewsMapaPoslano))
		if o.status != http.StatusUnauthorized || o.zaglavlje.Get("WWW-Authenticate") != `Basic realm="probni"` || o.tijelo != "" {
			t.Errorf("%s: %d %v %q", c.ime, o.status, o.zaglavlje, o.tijelo)
		}
	}
	// prazno tijelo i nepoznata operacija: 400 bez sadržaja
	for _, tijelo := range []string{"", probniZahtjev(`<m:SyncFolderItems/>`)} {
		o := probniPosalji(t, srv, srv.Korisnik, srv.Lozinka, tijelo)
		if o.status != http.StatusBadRequest || o.tijelo != "" || o.zaglavlje.Get("Content-Type") != "" {
			t.Errorf("%q: %d %v %q", tijelo, o.status, o.zaglavlje, o.tijelo)
		}
	}
}

func TestProbniEWSMapa(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	// jedna mapa ili nijedna: kratki uspjeh bez brojeva (provjera prijave)
	uspjeh := probniKratki("GetFolder", "Success", "", "NoError")
	if got := srv.probniPitaj(t, ewsMapaPoslano); got != uspjeh {
		t.Errorf("jedna mapa:\n%s", got)
	}
	if got := srv.probniPitaj(t, `<m:GetFolder><m:FolderIds/></m:GetFolder>`); got != uspjeh {
		t.Errorf("bez mapa:\n%s", got)
	}
	vise := `<m:GetFolder><m:FolderIds><t:DistinguishedFolderId Id="inbox"/><t:DistinguishedFolderId Id="sentitems"/><t:DistinguishedFolderId Id="archive"/></m:FolderIds></m:GetFolder>`
	mapa := func(ime string, uk, nepr int) string {
		return `<m:GetFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Folders><t:Folder><t:DisplayName>` + ime +
			`</t:DisplayName><t:TotalCount>` + strconv.Itoa(uk) + `</t:TotalCount><t:UnreadCount>` + strconv.Itoa(nepr) + `</t:UnreadCount></t:Folder></m:Folders></m:GetFolderResponseMessage>`
	}
	nema := `<m:GetFolderResponseMessage ResponseClass="Error"><m:MessageText>The specified folder could not be found in the store.</m:MessageText><m:ResponseCode>ErrorFolderNotFound</m:ResponseCode></m:GetFolderResponseMessage>`
	pocetak := `<m:GetFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages>`
	kraj := `</m:ResponseMessages></m:GetFolderResponse>`
	if got := srv.probniPitaj(t, vise); got != probniOmot(pocetak+mapa("inbox", 2, 1)+mapa("sentitems", 0, 0)+nema+kraj) {
		t.Errorf("bez arhive:\n%s", got)
	}
	srv.ImaArhivu = true
	if got := srv.probniPitaj(t, vise); got != probniOmot(pocetak+mapa("inbox", 2, 1)+mapa("sentitems", 0, 0)+mapa("archive", 1, 1)+kraj) {
		t.Errorf("s arhivom:\n%s", got)
	}
}

func TestProbniEWSKorisnikoveMape(t *testing.T) {
	srv := probniEWS(t)
	zahtjev := `<m:FindFolder Traversal="Shallow"><m:ParentFolderIds><t:DistinguishedFolderId Id="msgfolderroot"/></m:ParentFolderIds></m:FindFolder>`
	pocetak := `<m:FindFolderResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindFolderResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder><t:Folders>` +
		`<t:Folder><t:FolderId Id="AAMk-inbox"/><t:DisplayName>Inbox</t:DisplayName><t:FolderClass>IPF.Note</t:FolderClass><t:TotalCount>0</t:TotalCount><t:UnreadCount>0</t:UnreadCount></t:Folder>`
	kraj := `</t:Folders></m:RootFolder></m:FindFolderResponseMessage></m:ResponseMessages></m:FindFolderResponse>`
	if got := srv.probniPitaj(t, zahtjev); got != probniOmot(pocetak+kraj) {
		t.Errorf("samo inbox:\n%s", got)
	}
	srv.Korisnikove = map[string]string{"AAMk-1&2": "Obrana <2026>"}
	mapa := `<t:Folder><t:FolderId Id="AAMk-1&amp;2"/><t:DisplayName>Obrana &lt;2026&gt;</t:DisplayName><t:FolderClass>IPF.Note</t:FolderClass><t:TotalCount>0</t:TotalCount><t:UnreadCount>0</t:UnreadCount></t:Folder>`
	if got := srv.probniPitaj(t, zahtjev); got != probniOmot(pocetak+mapa+kraj) {
		t.Errorf("korisnikova mapa:\n%s", got)
	}
}

func TestProbniEWSAdresar(t *testing.T) {
	srv := probniEWS(t)
	srv.Adresar = []Kontakt{
		{Ime: "Ivana Horvat", Email: "ivana.horvat@voda.hr", Telefon: "031 000 001", Mobitel: "091 000 001", Funkcija: "Voditeljica", Odjel: "Obrana & zaštita"},
		{Ime: "Marko Kovač", Email: "marko.kovac@voda.hr"},
	}
	trazi := func(upit string) string {
		return `<m:ResolveNames ReturnFullContactData="true"><m:UnresolvedEntry>` + upit + `</m:UnresolvedEntry></m:ResolveNames>`
	}
	nemaNikoga := probniOmot(`<m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:ResolveNamesResponseMessage ResponseClass="Error"><m:MessageText>No results were found.</m:MessageText><m:ResponseCode>ErrorNameResolutionNoResults</m:ResponseCode></m:ResolveNamesResponseMessage></m:ResponseMessages></m:ResolveNamesResponse>`)
	if got := srv.probniPitaj(t, trazi("Babić")); got != nemaNikoga {
		t.Errorf("nema nikoga:\n%s", got)
	}
	osoba := func(k Kontakt) string {
		return `<t:Resolution><t:Mailbox><t:Name>` + xmlAttr(k.Ime) + `</t:Name><t:EmailAddress>` + k.Email + `</t:EmailAddress></t:Mailbox><t:Contact><t:DisplayName>` + xmlAttr(k.Ime) +
			`</t:DisplayName><t:PhoneNumbers><t:Entry Key="BusinessPhone">` + k.Telefon + `</t:Entry><t:Entry Key="MobilePhone">` + k.Mobitel + `</t:Entry></t:PhoneNumbers><t:JobTitle>` + k.Funkcija +
			`</t:JobTitle><t:Department>` + xmlAttr(k.Odjel) + `</t:Department></t:Contact></t:Resolution>`
	}
	nadjeni := func(n string, osobe ...string) string {
		return probniOmot(`<m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:ResolveNamesResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:ResolutionSet TotalItemsInView="` + n + `">` +
			strings.Join(osobe, "") + `</m:ResolutionSet></m:ResolveNamesResponseMessage></m:ResponseMessages></m:ResolveNamesResponse>`)
	}
	// traži se po imenu i adresi, bez obzira na velika slova
	if got := srv.probniPitaj(t, trazi("HORVAT")); got != nadjeni("1", osoba(srv.Adresar[0])) {
		t.Errorf("po imenu:\n%s", got)
	}
	if got := srv.probniPitaj(t, trazi("kovac@")); got != nadjeni("1", osoba(srv.Adresar[1])) {
		t.Errorf("po adresi:\n%s", got)
	}
	// bez UnresolvedEntry upit je prazan, pa odgovara svima
	if got := srv.probniPitaj(t, `<m:ResolveNames/>`); got != nadjeni("2", osoba(srv.Adresar[0]), osoba(srv.Adresar[1])) {
		t.Errorf("bez upita:\n%s", got)
	}
}

func TestProbniEWSPopisPisama(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	pismo := func(i int) string {
		x := srv.Pisma[i]
		priv := "false"
		if len(x.Privitci) > 0 {
			priv = "true"
		}
		procitano := "false"
		if x.Procitano {
			procitano = "true"
		}
		return `<t:Message><t:ItemId Id="` + x.ID + `" ChangeKey="ck1"/><t:Subject>` + xmlAttr(x.Predmet) + `</t:Subject><t:HasAttachments>` + priv + `</t:HasAttachments><t:Size>` +
			strconv.Itoa(x.Velicina) + `</t:Size><t:DateTimeReceived>` + x.Kad.UTC().Format(time.RFC3339) + `</t:DateTimeReceived><t:From><t:Mailbox><t:Name>` + xmlAttr(x.Od) +
			`</t:Name><t:EmailAddress>` + x.OdAdresa + `</t:EmailAddress></t:Mailbox></t:From><t:IsRead>` + procitano + `</t:IsRead><t:Preview>` + xmlAttr(x.Tekst) + `</t:Preview></t:Message>`
	}
	popis := func(n string, pisma ...string) string {
		return probniOmot(`<m:FindItemResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:FindItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:RootFolder TotalItemsInView="` + n + `"><t:Items>` +
			strings.Join(pisma, "") + `</t:Items></m:RootFolder></m:FindItemResponseMessage></m:ResponseMessages></m:FindItemResponse>`)
	}
	trazi := func(mapa, upit string) string {
		s := `<m:FindItem Traversal="Shallow"><m:IndexedPageItemView MaxEntriesReturned="1" Offset="5" BasePoint="Beginning"/><m:ParentFolderIds>` + mapa + `</m:ParentFolderIds>`
		if upit != "" {
			s += `<m:QueryString>` + upit + `</m:QueryString>`
		}
		return s + `</m:FindItem>`
	}
	if !strings.Contains(pismo(0), "<t:Preview>Dunav raste&#xA;5 cm &amp; više</t:Preview>") || !strings.Contains(pismo(0), "2026-03-14T07:30:00Z") {
		t.Fatalf("očekivani oblik pisma: %s", pismo(0))
	}
	// stranica (MaxEntriesReturned, Offset) se ne gleda: uvijek sva pisma mape
	if got := srv.probniPitaj(t, trazi("", "")); got != popis("2", pismo(0), pismo(1)) {
		t.Errorf("bez mape je inbox:\n%s", got)
	}
	if got := srv.probniPitaj(t, trazi(`<t:DistinguishedFolderId Id="archive"/>`, "")); got != popis("1", pismo(2)) {
		t.Errorf("arhiva:\n%s", got)
	}
	if got := srv.probniPitaj(t, trazi(`<t:FolderId Id="AAMk-9"/>`, "")); got != popis("0") {
		t.Errorf("prazna mapa:\n%s", got)
	}
	// pretraga po predmetu, pošiljatelju i tekstu, bez obzira na velika slova
	for upit, i := range map[string]int{"BATINA": 0, "kovač": 1, "dunav raste": 0} {
		if got := srv.probniPitaj(t, trazi(`<t:DistinguishedFolderId Id="inbox"/>`, upit)); got != popis("1", pismo(i)) {
			t.Errorf("pretraga %q:\n%s", upit, got)
		}
	}
	if got := srv.probniPitaj(t, trazi("", "zapisnik")); got != popis("0") {
		t.Errorf("pretraga traži samo u zadanoj mapi:\n%s", got)
	}
}

func TestProbniEWSOtvorenoPismo(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	otvori := func(id string) string {
		return `<m:GetItem><m:ItemShape><t:BaseShape>Default</t:BaseShape></m:ItemShape><m:ItemIds><t:ItemId Id="` + id + `"/></m:ItemIds></m:GetItem>`
	}
	pocetak := func(id, msgid, predmet, html, tekst, kad, od, adresa string) string {
		return `<m:GetItemResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetItemResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Items><t:Message><t:ItemId Id="` + id +
			`" ChangeKey="ck1"/><t:InternetMessageId>` + msgid + `</t:InternetMessageId><t:Subject>` + predmet + `</t:Subject><t:Body BodyType="HTML">` + html + `</t:Body><t:TextBody>` + tekst +
			`</t:TextBody><t:DateTimeReceived>` + kad + `</t:DateTimeReceived><t:From><t:Mailbox><t:Name>` + od + `</t:Name><t:EmailAddress>` + adresa +
			`</t:EmailAddress></t:Mailbox></t:From><t:ToRecipients><t:Mailbox><t:EmailAddress>VODA\ihorvat</t:EmailAddress></t:Mailbox></t:ToRecipients><t:Attachments>`
	}
	kraj := `</t:Attachments></t:Message></m:Items></m:GetItemResponseMessage></m:ResponseMessages></m:GetItemResponse>`
	// bez HTML-a tijelo se slaže iz teksta i još jednom kodira; xmlAttr novi
	// redak već pretvori u &#xA;, pa <br> nikad ne nastane
	bezHTML := pocetak("p1", "&lt;p1@voda.hr&gt;", "Vodostaj Batina", "&lt;html&gt;&lt;body&gt;&lt;p&gt;Dunav raste&amp;#xA;5 cm &amp;amp; više&lt;/p&gt;&lt;/body&gt;&lt;/html&gt;",
		"Dunav raste&#xA;5 cm &amp; više", "2026-03-14T07:30:00Z", "Ivana Horvat", "ivana.horvat@voda.hr") + kraj
	if got := srv.probniPitaj(t, otvori("p1")); got != probniOmot(bezHTML) {
		t.Errorf("bez HTML-a:\n%s", got)
	}
	privitak := `<t:FileAttachment><t:AttachmentId Id="a1"/><t:Name>raspored.pdf</t:Name><t:ContentType>application/pdf</t:ContentType><t:ContentId>c&lt;1&gt;</t:ContentId><t:Size>4</t:Size><t:IsInline>true</t:IsInline></t:FileAttachment>`
	sHTML := pocetak("p2", "", "Dežurstvo", "&lt;p&gt;Raspored&lt;/p&gt;", "Raspored", "2026-03-14T08:30:00Z", "Marko Kovač", "marko.kovac@voda.hr") + privitak + kraj
	if got := srv.probniPitaj(t, otvori("p2")); got != probniOmot(sHTML) {
		t.Errorf("s HTML-om i privitkom:\n%s", got)
	}
	nema := probniKratki("GetItem", "Error", "The specified object was not found in the store.", "ErrorInvalidRecipients")
	if got := srv.probniPitaj(t, otvori("p9")); got != nema {
		t.Errorf("nepoznato pismo:\n%s", got)
	}
	if got := srv.probniPitaj(t, `<m:GetItem><m:ItemIds/></m:GetItem>`); got != nema {
		t.Errorf("bez ItemId:\n%s", got)
	}
}

func TestProbniEWSOznakaProcitano(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	oznaci := func(ck, procitano string, ids ...string) string {
		var b strings.Builder
		b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>`)
		for _, id := range ids {
			b.WriteString(`<t:ItemChange><t:ItemId Id="` + id + `" ChangeKey="` + ck + `"/><t:Updates><t:SetItemField><t:Message><t:IsRead>` + procitano + `</t:IsRead></t:Message></t:SetItemField></t:Updates></t:ItemChange>`)
		}
		return b.String() + `</m:ItemChanges></m:UpdateItem>`
	}
	uspjeh := probniKratki("UpdateItem", "Success", "", "NoError")
	if got := srv.probniPitaj(t, oznaci("ck1", "true", "p1", "p3", "p9")); got != uspjeh || !srv.Pisma[0].Procitano || !srv.Pisma[2].Procitano {
		t.Errorf("pročitano:\n%s %+v", got, srv.Pisma)
	}
	if got := srv.probniPitaj(t, oznaci("ck1", "false", "p2")); got != uspjeh || srv.Pisma[1].Procitano {
		t.Errorf("nepročitano:\n%s", got)
	}
	// zastarjeli ChangeKey: sukob, a pisma prije njega su već promijenjena
	sukob := probniKratki("UpdateItem", "Error", "ErrorIrresolvableConflict", "ErrorIrresolvableConflict")
	if got := srv.probniPitaj(t, strings.Replace(oznaci("ck1", "false", "p1", "p3"), `"p3" ChangeKey="ck1"`, `"p3" ChangeKey="ck0"`, 1)); got != sukob || srv.Pisma[0].Procitano || !srv.Pisma[2].Procitano {
		t.Errorf("sukob:\n%s %+v", got, srv.Pisma)
	}
	if got := srv.probniPitaj(t, oznaci("ck1", "true")); got != uspjeh {
		t.Errorf("bez pisama:\n%s", got)
	}
}

func TestProbniEWSBrisanjeIPremjestanje(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	premjesti := func(cilj string, ids ...string) string {
		s := `<m:MoveItem>` + cilj + `<m:ItemIds>`
		for _, id := range ids {
			s += `<t:ItemId Id="` + id + `"/>`
		}
		return s + `</m:ItemIds></m:MoveItem>`
	}
	nemaMape := probniKratki("MoveItem", "Error", "The specified folder could not be found in the store. ErrorFolderNotFound", "ErrorFolderNotFound")
	if got := srv.probniPitaj(t, premjesti("", "p1")); got != nemaMape {
		t.Errorf("bez cilja:\n%s", got)
	}
	if got := srv.probniPitaj(t, premjesti(`<m:ToFolderId><t:DistinguishedFolderId Id="archive"/></m:ToFolderId>`, "p1")); got != nemaMape || srv.Mapa["p1"] != "" {
		t.Errorf("arhive nema:\n%s", got)
	}
	// bez ijednog premještaja Mapa je nil, pa je premještaj stvara
	srv.Mapa = nil
	uspjeh := probniKratki("MoveItem", "Success", "", "NoError")
	if got := srv.probniPitaj(t, premjesti(`<m:ToFolderId><t:FolderId Id="AAMk-7"/></m:ToFolderId>`, "p1", "p9")); got != uspjeh || srv.Mapa["p1"] != "AAMk-7" || srv.Mapa["p9"] != "AAMk-7" {
		t.Errorf("u korisnikovu mapu:\n%s %v", got, srv.Mapa)
	}
	srv.ImaArhivu = true
	if got := srv.probniPitaj(t, premjesti(`<m:ToFolderId><t:DistinguishedFolderId Id="archive"/></m:ToFolderId>`, "p2")); got != uspjeh || srv.Mapa["p2"] != "archive" {
		t.Errorf("u arhivu:\n%s %v", got, srv.Mapa)
	}

	obrisi := `<m:DeleteItem DeleteType="SoftDelete"><m:ItemIds><t:ItemId Id="p1"/><t:ItemId Id="p3"/><t:ItemId Id="p9"/></m:ItemIds></m:DeleteItem>`
	if got := srv.probniPitaj(t, obrisi); got != probniKratki("DeleteItem", "Success", "", "NoError") || len(srv.Pisma) != 1 || srv.Pisma[0].ID != "p2" {
		t.Errorf("brisanje:\n%s %+v", got, srv.Pisma)
	}
	// obrisana sva pisma: popis ostaje nil, ne prazan
	if srv.probniPitaj(t, `<m:DeleteItem><m:ItemIds><t:ItemId Id="p2"/></m:ItemIds></m:DeleteItem>`); srv.Pisma != nil {
		t.Errorf("ostalo: %+v", srv.Pisma)
	}
}

func TestProbniEWSPrivitak(t *testing.T) {
	srv := probniEWS(t)
	pismaZaProbu(srv)
	uzmi := func(id string) string {
		return `<m:GetAttachment><m:AttachmentIds><t:AttachmentId Id="` + id + `"/></m:AttachmentIds></m:GetAttachment>`
	}
	nadjen := probniOmot(`<m:GetAttachmentResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages><m:GetAttachmentResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:Attachments><t:FileAttachment><t:AttachmentId Id="a1"/><t:Name>raspored.pdf</t:Name><t:ContentType>application/pdf</t:ContentType><t:Content>` +
		base64.StdEncoding.EncodeToString([]byte("%PDF")) + `</t:Content></t:FileAttachment></m:Attachments></m:GetAttachmentResponseMessage></m:ResponseMessages></m:GetAttachmentResponse>`)
	if got := srv.probniPitaj(t, uzmi("a1")); got != nadjen {
		t.Errorf("privitak:\n%s", got)
	}
	nema := probniKratki("GetAttachment", "Error", "The specified object was not found in the store.", "ErrorInvalidRecipients")
	if got := srv.probniPitaj(t, uzmi("a9")); got != nema {
		t.Errorf("nepoznat privitak:\n%s", got)
	}
	if got := srv.probniPitaj(t, `<m:GetAttachment><m:AttachmentIds/></m:GetAttachment>`); got != nema {
		t.Errorf("bez AttachmentId:\n%s", got)
	}
}

func TestProbniEWSSlanje(t *testing.T) {
	srv := probniEWS(t)
	srv.Odbij = []string{"nema@primjer.hr"}
	posalji := func(mime string) string {
		return `<m:CreateItem MessageDisposition="SendAndSaveCopy"><m:Items><t:Message><t:MimeContent CharacterSet="UTF-8">` +
			base64.StdEncoding.EncodeToString([]byte(mime)) + `</t:MimeContent></t:Message></m:Items></m:CreateItem>`
	}
	neispravni := probniKratki("CreateItem", "Error", "One or more recipients are invalid.", "ErrorInvalidRecipients")
	for _, c := range []struct{ ime, zahtjev, odgovor string }{
		{"bez MimeContent", `<m:CreateItem><m:Items/></m:CreateItem>`, probniKratki("CreateItem", "Error", "nema MimeContent", "ErrorInvalidRecipients")},
		{"prazan MIME", posalji(""), probniKratki("CreateItem", "Error", "EOF", "ErrorInvalidRecipients")},
		{"neispravan base64", `<m:CreateItem><t:MimeContent>!!!</t:MimeContent></m:CreateItem>`, probniKratki("CreateItem", "Error", "EOF", "ErrorInvalidRecipients")},
		{"krivo zaglavlje", posalji("bez dvotocke\r\n\r\n"), probniKratki("CreateItem", "Error", `malformed header line: "bez dvotocke"`, "ErrorInvalidRecipients")},
		{"bez primatelja", posalji("From: a@voda.hr\r\n\r\ntekst"), neispravni},
		{"bez pošiljatelja", posalji("To: b@voda.hr\r\n\r\ntekst"), neispravni},
		{"odbijen u kopiji", posalji("From: a@voda.hr\r\nTo: b@voda.hr\r\nCc: NEMA@primjer.hr\r\n\r\ntekst"), neispravni},
	} {
		if got := srv.probniPitaj(t, c.zahtjev); got != c.odgovor {
			t.Errorf("%s:\n%s", c.ime, got)
		}
	}
	if len(srv.Poruke()) != 0 {
		t.Fatalf("odbijeno ne smije stići: %+v", srv.Poruke())
	}
	mime := "From: Ivana Horvat <Ivana.Horvat@voda.hr>\r\nTo: B@voda.hr, c@voda.hr\r\nCc: d@voda.hr\r\nSubject: Proba\r\n\r\ntekst"
	if got := srv.probniPitaj(t, posalji(mime)); got != probniKratki("CreateItem", "Success", "", "NoError") {
		t.Errorf("poslano:\n%s", got)
	}
	prim := srv.Poruke()
	if len(prim) != 3 || prim[0] != (Primljena{"ivana.horvat@voda.hr", "b@voda.hr", mime}) || prim[1].Za != "c@voda.hr" || prim[2].Za != "d@voda.hr" {
		t.Errorf("primljeno: %+v", prim)
	}
}

// Operacija se prepoznaje po prvoj oznaci s popisa koja se nađe u tijelu,
// ne po tome što je zapravo pozvano
func TestProbniEWSRedoslijedOperacija(t *testing.T) {
	srv := probniEWS(t)
	zahtjev := `<m:CreateItem><t:MimeContent>eA==</t:MimeContent><m:GetFolder></m:GetFolder></m:CreateItem>`
	if got := srv.probniPitaj(t, zahtjev); got != probniKratki("GetFolder", "Success", "", "NoError") {
		t.Errorf("GetFolder ima prednost:\n%s", got)
	}
	// <m:GetItem bez > odmah iza nije GetItem
	if o := probniPosalji(t, srv, srv.Korisnik, srv.Lozinka, probniZahtjev(`<m:GetItem Traversal="x"/>`)); o.status != http.StatusBadRequest {
		t.Errorf("status %d", o.status)
	}
}
