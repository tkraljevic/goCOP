package posta

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"
)

// Primjeri iz MS-NLMP 4.2.4 (NTLMv2): User/Domain/Password, izazov
// 0123456789abcdef, klijentov aaaaaaaaaaaaaaaa, vrijeme 0
func TestNTLMv2PremaSpecifikaciji(t *testing.T) {
	if got := hex.EncodeToString(ntowfv2("Password", "User", "Domain")); got != "0c868a403bfd7a93a3001ef22ef02e3f" {
		t.Fatalf("NTOWFv2: %s", got)
	}
	info := append(avPar(avNbDomain, utf16le("Domain")), avPar(avNbComputer, utf16le("Server"))...)
	z := &ntlmIzazov{Zastavice: ntlmUnicode | ntlmNTLM | ntlmTargetInfo, TargetInfo: info, AV: map[uint16][]byte{avNbDomain: utf16le("Domain"), avNbComputer: utf16le("Server")}}
	copy(z.Izazov[:], []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef})
	kl := bytes.Repeat([]byte{0xaa}, 8)
	// bez MIC-zastavice, vezanja i SPN-a, da odgovara primjeru: temp se gradi isto
	b, err := ntlmAuthenticate(ntlmNegotiate(), nil, z, `Domain\User`, "Password", "", nil, kl, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	ntLen := binary.LittleEndian.Uint16(b[20:])
	ntOff := binary.LittleEndian.Uint32(b[24:])
	nt := b[ntOff : ntOff+uint32(ntLen)]
	// primjer iz spec. nema zastavice MIC u AV parovima; ovdje je dodana, pa
	// se NTProofStr računa nad drugačijim temp-om. Provjeri gradivne dijelove:
	temp := nt[16:]
	if !bytes.Equal(temp[:8], []byte{1, 1, 0, 0, 0, 0, 0, 0}) || !bytes.Equal(temp[8:16], make([]byte, 8)) || !bytes.Equal(temp[16:24], kl) {
		t.Errorf("temp zaglavlje: %x", temp[:24])
	}
	if !bytes.Contains(temp, avPar(avFlags, []byte{2, 0, 0, 0})) {
		t.Error("nema zastavice MIC")
	}
	// isti temp bez zastavice MIC daje NTProofStr iz specifikacije
	tempSpec := append([]byte{}, temp[:28]...)
	tempSpec = append(tempSpec, info...)
	tempSpec = append(tempSpec, make([]byte, 8)...)
	proof := hmacMD5(ntowfv2("Password", "User", "Domain"), append(append([]byte{}, z.Izazov[:]...), tempSpec...))
	if hex.EncodeToString(proof) != "68cd0ab851e51c96aabc927bebef6a1c" {
		t.Errorf("NTProofStr: %x", proof)
	}
	if k := hmacMD5(ntowfv2("Password", "User", "Domain"), proof); hex.EncodeToString(k) != "8de40ccadbc14a82f15cb0ad0de95ca3" {
		t.Errorf("SessionBaseKey: %x", k)
	}
	// zaglavlje poruke
	if !bytes.Equal(b[:8], ntlmPotpis) || binary.LittleEndian.Uint32(b[8:]) != 3 {
		t.Error("zaglavlje poruke 3")
	}
	uLen := binary.LittleEndian.Uint16(b[36:])
	uOff := binary.LittleEndian.Uint32(b[40:])
	if !bytes.Equal(b[uOff:uOff+uint32(uLen)], utf16le("User")) {
		t.Error("korisničko ime")
	}
	dLen := binary.LittleEndian.Uint16(b[28:])
	dOff := binary.LittleEndian.Uint32(b[32:])
	if !bytes.Equal(b[dOff:dOff+uint32(dLen)], utf16le("Domain")) {
		t.Error("domena")
	}
	// MIC = HMAC_MD5(SessionBaseKey, neg||chal||auth s nulama)
	mic := append([]byte{}, b[72:88]...)
	bez := append([]byte{}, b...)
	copy(bez[72:], make([]byte, 16))
	proofNas := nt[:16]
	if !bytes.Equal(mic, hmacMD5(hmacMD5(ntowfv2("Password", "User", "Domain"), proofNas), append(append([]byte{}, ntlmNegotiate()...), bez...))) {
		t.Error("MIC")
	}
}

func TestIzazovSeCita(t *testing.T) {
	// izazov koji je vratio owa.voda.hr (bez tajni: samo imena i nasumičan izazov)
	const b64 = "TlRMTVNTUAACAAAACAAIADgAAAAFgomiXOCnHpmLTSYAAAAAAAAAAH4AfgBAAAAABgOAJQAAAA9WAE8ARABBAAIACABWAE8ARABBAAEAEABFAFgAQwAyADAAMQA2AFAABAAQAHYAbwBkAGEALgBpAG4AdAADACIARQBYAEMAMgAwADEANgBQAC4AdgBvAGQAYQAuAGkAbgB0AAUAEAB2AG8AZABhAC4AaQBuAHQABwAIAIvFzmClR90BAAAAAA=="
	raw, _ := base64Decode(b64)
	z, err := procitajIzazov(raw)
	if err != nil {
		t.Fatal(err)
	}
	if z.NetBIOSDomena() != "VODA" || z.tekst(avDnsDomain) != "voda.int" || z.AV[avTimestamp] == nil {
		t.Errorf("%q %q", z.NetBIOSDomena(), z.tekst(avDnsDomain))
	}
	if z.Zastavice&ntlmKeyExch != 0 {
		t.Error("poslužitelj ne traži razmjenu ključa")
	}
	if got := ntlmDrugoIme("tkraljevic@voda.hr", z); got != `VODA\tkraljevic` {
		t.Errorf("drugo ime: %s", got)
	}
	if got := ntlmDrugoIme(`VODA\x`, z); got != "" {
		t.Errorf("s domenom nema drugog imena: %s", got)
	}
}

func base64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
