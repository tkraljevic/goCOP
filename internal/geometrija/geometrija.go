package geometrija

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Files sadrži ugrađene GeoJSON datoteke geometrije vodotoka i stacionaža.
//
//go:embed *.geojson
var Files embed.FS

// Ucitaj vraća GeoJSON podatke za zadano vodno tijelo (npr. "rijeka-dunav").
//
// Prvo provjerava mapu na disku (ako je navedena) kako bi se geometrija
// mogla prilagođavati bez ponovnog prevodioca; ako datoteke nema na disku,
// poseže za ugrađenim podacima.
func Ucitaj(dir, code string) ([]byte, error) {
	code = strings.TrimSpace(code)
	if code == "" || strings.Contains(code, "..") || strings.ContainsAny(code, `/\`) {
		return nil, errors.New("neispravna šifra vodnog tijela")
	}

	fileName := code + ".geojson"

	if dir != "" {
		diskPath := filepath.Join(dir, fileName)
		if data, err := os.ReadFile(diskPath); err == nil {
			return data, nil
		}
	}

	data, err := Files.ReadFile(fileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	return data, nil
}

// Ima provjerava postoji li geometrija za zadano vodno tijelo.
func Ima(dir, code string) bool {
	data, err := Ucitaj(dir, code)
	return err == nil && len(data) > 0
}
