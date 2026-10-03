package posta

import (
	"net/mail"
	"regexp"
	"testing"
)

// PIN za prijavu ne smije ostati u mapi Poslano računa koji ga šalje: poruka
// BezKopije ide kao SendOnly, bez SavedItemFolderId; ostale i dalje spremaju
// kopiju kao Outlook.
func TestSlanjeBezKopijeEWS(t *testing.T) {
	sadrzaj := regexp.MustCompile(`(<t:MimeContent CharacterSet="UTF-8">)[^<]*(</t:MimeContent>)`)
	zlatno := func(m Poruka) string { return sadrzaj.ReplaceAllString(ewsSlanje(m), "${1}MIME${2}") }
	m := Poruka{Od: mail.Address{Address: "a@voda.hr"}, Za: mail.Address{Address: "b@voda.hr"}, Predmet: "x", Tekst: "y"}

	if got, want := zlatno(m), `<m:CreateItem MessageDisposition="SendAndSaveCopy"><m:SavedItemFolderId><t:DistinguishedFolderId Id="sentitems"/></m:SavedItemFolderId><m:Items><t:Message><t:MimeContent CharacterSet="UTF-8">MIME</t:MimeContent></t:Message></m:Items></m:CreateItem>`; got != want {
		t.Errorf("s kopijom:\n%s\nočekivano:\n%s", got, want)
	}
	m.BezKopije = true
	if got, want := zlatno(m), `<m:CreateItem MessageDisposition="SendOnly"><m:Items><t:Message><t:MimeContent CharacterSet="UTF-8">MIME</t:MimeContent></t:Message></m:Items></m:CreateItem>`; got != want {
		t.Errorf("bez kopije:\n%s\nočekivano:\n%s", got, want)
	}
}
