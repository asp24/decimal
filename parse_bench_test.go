package decimal

import "testing"

var parseBenchInputs = []struct {
	name string
	text string
}{
	{"zero", "0"},
	{"int", "123"},
	{"money", "-1234.56"},
	{"frac", "0.000123"},
	{"pi16", "3.141592653589793"},
	{"prec19", "1234567890.123456789"},
	{"max", "9999999999999999999"},
	{"exp", "1.23e5"},
	{"long", "0.12345678901234567890123"},
	{"lz21", "0.0000000000000000012"},
}

func BenchmarkParse(b *testing.B) {
	for _, in := range parseBenchInputs {
		b.Run(in.name, func(b *testing.B) {
			for range b.N {
				_, _ = Parse(in.text)
			}
		})
	}
}
