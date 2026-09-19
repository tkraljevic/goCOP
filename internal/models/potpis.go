package models

import "time"

// PotpisniKljuc je osobni ključ za elektronički potpis: certifikat javno,
// privatni ključ šifriran lozinkom osobe. Putuje knjigom verzija, pa osoba
// potpisuje s bilo kojeg čvora; bez lozinke je neupotrebljiv.
type PotpisniKljuc struct {
	UserID    string    `json:"user_id"`
	Ime       string    `json:"ime"`
	Cert      []byte    `json:"cert"`  // DER
	Kljuc     []byte    `json:"kljuc"` // PKCS#8, AES-256-GCM ključem iz lozinke
	Sol       []byte    `json:"sol"`
	Izdao     string    `json:"izdao"` // čvor čiji je izdavatelj potpisao certifikat
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IzdavateljPotpisa je certifikat izdavatelja jednog čvora, da svaki čvor
// može provjeriti potpise dane na drugom
type IzdavateljPotpisa struct {
	Cvor      string    `json:"cvor"`
	Cert      []byte    `json:"cert"` // DER
	CreatedAt time.Time `json:"created_at"`
}

// IzvornikLista je PDF dnevnog lista kako je potpisan: prvo vodočuvar pri
// predaji, pa rukovoditelj pri ovjeri kao dodatak na iste bajtove
type IzvornikLista struct {
	ListID    string    `json:"list_id"`
	PDF       []byte    `json:"pdf"`
	Sazetak   string    `json:"sazetak"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IzvornikDnevnika struct {
	JournalID string    `json:"journal_id"`
	PDF       []byte    `json:"pdf"`
	Sazetak   string    `json:"sazetak"`
	UpdatedAt time.Time `json:"updated_at"`
}
