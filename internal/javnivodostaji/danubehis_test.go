package javnivodostaji

import (
	"testing"
	"time"
)

const danubeHISUzorak = `<tbody>
          <tr class="odd first">
                  <td class="views-field views-field-time" >
            2026-09-30 08:00          </td>
                  <td class="views-field views-field-value views-align-right" >
            -37          </td>
                  <td class="views-field views-field-unit" >
            cm          </td>
                  <td class="views-field views-field-description" >
            Current water level          </td>
              </tr>
          <tr class="even">
                  <td class="views-field views-field-time" >
            2026-09-30 07:00          </td>
                  <td class="views-field views-field-value views-align-right" >
            -36          </td>
                  <td class="views-field views-field-unit" >
            cm          </td>
                  <td class="views-field views-field-description" >
            Current water level          </td>
              </tr>
          <tr class="odd">
                  <td class="views-field views-field-time" >
            2026-09-30 07:00          </td>
                  <td class="views-field views-field-value views-align-right" >
            590          </td>
                  <td class="views-field views-field-unit" >
            m³/s          </td>
                  <td class="views-field views-field-description" >
            Current river discharge          </td>
              </tr>
</tbody>`

func TestCitajDanubeHIS(t *testing.T) {
	redci, err := CitajDanubeHIS(danubeHISUzorak)
	if err != nil {
		t.Fatal(err)
	}
	if len(redci) != 2 {
		t.Fatalf("očekivana 2 vodostaja (protok se preskače), dobiveno %d", len(redci))
	}
	// 8 h po bečkom (ljetnom) vremenu je 6 h UTC; najstarije prvo
	if want := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC); !redci[0].Kad.Equal(want) || *redci[0].LevelCm != -36 {
		t.Fatalf("prvi redak %v %d", redci[0].Kad, *redci[0].LevelCm)
	}
	if want := time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC); !redci[1].Kad.Equal(want) || *redci[1].LevelCm != -37 {
		t.Fatalf("drugi redak %v %d", redci[1].Kad, *redci[1].LevelCm)
	}
}

func TestPostajaDanubeHISIzAdrese(t *testing.T) {
	if g := PostajaDanubeHISIzAdrese("https://www.danubehis.org/results/HU442522_HYDRO/h"); g != "HU442522_HYDRO" {
		t.Fatalf("dobiveno %q", g)
	}
	if PostajaDanubeHISIzAdrese("https://www.vizugy.hu/?AllomasVOA=x") != "" {
		t.Fatal("vizugy nije DanubeHIS")
	}
	if AdresaDanubeHIS("hu442522_hydro") != "https://www.danubehis.org/results/HU442522_HYDRO/h" {
		t.Fatal(AdresaDanubeHIS("hu442522_hydro"))
	}
}
