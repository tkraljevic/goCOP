package web

import (
	"fmt"
	"time"

	"gocop/internal/models"
)

// humanAgo kaže koliko je trenutak star, kako bi to rekao dežurni:
// „prije 20 min“, „danas 07:25“, „jučer 07:10“, „prije 3 d“, inače datum
func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	now := time.Now().In(models.Zagreb)
	lt := t.In(models.Zagreb)
	d := now.Sub(lt)
	switch {
	case d < 0:
		return lt.Format("02.01. 15:04")
	case d < time.Minute:
		return "upravo sad"
	case d < time.Hour:
		return fmt.Sprintf("prije %d min", int(d.Minutes()))
	case lt.YearDay() == now.YearDay() && lt.Year() == now.Year():
		return "danas " + lt.Format("15:04")
	case now.AddDate(0, 0, -1).YearDay() == lt.YearDay() && now.AddDate(0, 0, -1).Year() == lt.Year():
		return "jučer " + lt.Format("15:04")
	case d < 14*24*time.Hour:
		return fmt.Sprintf("prije %d d", int(d.Hours()/24))
	}
	return lt.Format("02.01.2006.")
}
