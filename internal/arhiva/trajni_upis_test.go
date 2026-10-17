package arhiva

import (
	"context"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"

	"modernc.org/sqlite"
)

// pracenje bilježi synchronous svake veze koja se otvori na putu koji test
// prati. Driver popis hookova čita bez brave, pa se hook prijavi jednom,
// prije svih testova.
var pracenje struct {
	sync.Mutex
	put         string
	synchronous []driver.Value
}

func init() {
	sqlite.RegisterConnectionHook(func(veza sqlite.ExecQuerierContext, dsn string) error {
		pracenje.Lock()
		defer pracenje.Unlock()
		if pracenje.put == "" || !strings.HasPrefix(dsn, pracenje.put) {
			return nil
		}
		redovi, err := veza.QueryContext(context.Background(), "PRAGMA synchronous", nil)
		if err != nil {
			return err
		}
		defer redovi.Close()
		v := make([]driver.Value, 1)
		if err := redovi.Next(v); err != nil {
			return err
		}
		pracenje.synchronous = append(pracenje.synchronous, v[0])
		return nil
	})
}

// Zaključava da gradnja piše u arhivu tako da potvrđen upis preživi nestanak
// struje. Gradnja piše ravno u arhivu koju čvor koristi, a uz
// synchronous(OFF) nestanak struje usred gradnje mogao je pokvariti cijelu
// arhivu, i to odmah nakon što je paket otišao drugim čvorovima.
func TestGradnjaPiseTrajno(t *testing.T) {
	koren, baza := stabloSKvarom(t, "")
	pracenje.Lock()
	pracenje.put, pracenje.synchronous = baza, nil
	pracenje.Unlock()
	t.Cleanup(func() {
		pracenje.Lock()
		pracenje.put = ""
		pracenje.Unlock()
	})

	if _, err := Izgradi(koren, baza, "", io.Discard); err != nil {
		t.Fatal(err)
	}
	pracenje.Lock()
	defer pracenje.Unlock()
	if len(pracenje.synchronous) == 0 {
		t.Fatal("gradnja nije otvorila nijednu vezu na arhivu")
	}
	for i, v := range pracenje.synchronous {
		if v != int64(2) {
			t.Errorf("%d. veza gradnje: synchronous %v, treba 2 (FULL)", i+1, v)
		}
	}
}
