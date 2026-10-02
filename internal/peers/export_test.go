package peers

// SuziDeltu privremeno smanjuje ogradu veličine delte, za test skraćenih
// razgovora; vraća funkciju koja je vraća
func SuziDeltu(n int) func() {
	prije := najviseBajtovaDelte
	najviseBajtovaDelte = n
	return func() { najviseBajtovaDelte = prije }
}
