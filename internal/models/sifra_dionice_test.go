package models

import (
	"reflect"
	"testing"
)

func TestSifreDionicaPoBrojevima(t *testing.T) {
	sifre := []string{"B.34.10", "B.34.2", "B.34.1", "B.15.12", "B.15.2", "A.33.1", "B.34.19", "B.34.11", "B.34.2a", "B.34"}
	SloziSifreDionica(sifre)
	htio := []string{"A.33.1", "B.15.2", "B.15.12", "B.34", "B.34.1", "B.34.2", "B.34.2a", "B.34.10", "B.34.11", "B.34.19"}
	if !reflect.DeepEqual(sifre, htio) {
		t.Errorf("redoslijed %v, htio %v", sifre, htio)
	}
	if !ManjaSifraDionice("B.34.7", "B.34.07") || ManjaSifraDionice("B.34.07", "B.34.7") {
		t.Error("isti broj u drukčijem zapisu nema stalan redoslijed")
	}
	if ManjaSifraDionice("B.34.1", "B.34.1") {
		t.Error("ista šifra nije manja od same sebe")
	}
}
