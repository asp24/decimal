package decimal

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

var stringBenchInputs = []struct {
	name string
	d    Decimal
}{
	{"zero", MustNew(0, 0)},
	{"int", MustNew(123, 0)},
	{"money", MustNew(-123456, 2)},
	{"frac", MustNew(123, 6)},
	{"pi16", MustNew(3141592653589793, 15)},
	{"prec19", MustNew(1234567890123456789, 9)},
	{"max", newUnsafe(true, maxCoef, 0)},
	{"lz19", MustNew(12, 19)},
}

var stringSink string

func BenchmarkDecimal_String(b *testing.B) {
	for _, in := range stringBenchInputs {
		b.Run(in.name, func(b *testing.B) {
			for range b.N {
				stringSink = in.d.String()
			}
		})
	}
}

func BenchmarkDecimal_AppendText(b *testing.B) {
	buf := make([]byte, 0, 64)
	for _, in := range stringBenchInputs {
		b.Run(in.name, func(b *testing.B) {
			for range b.N {
				buf, _ = in.d.AppendText(buf[:0])
			}
		})
	}
}

var jsonSink []byte

func BenchmarkDecimal_MarshalJSON(b *testing.B) {
	type account struct {
		Balance Decimal `json:"balance"`
	}
	for _, in := range stringBenchInputs {
		b.Run("v1/"+in.name, func(b *testing.B) {
			for range b.N {
				jsonSink, _ = json.Marshal(account{Balance: in.d})
			}
		})
		b.Run("v2/"+in.name, func(b *testing.B) {
			for range b.N {
				jsonSink, _ = jsonv2.Marshal(account{Balance: in.d})
			}
		})
	}
}

func BenchmarkDecimal_UnmarshalJSON(b *testing.B) {
	type account struct {
		Balance Decimal `json:"balance"`
	}
	for _, in := range stringBenchInputs {
		data := []byte(`{"balance":"` + in.d.String() + `"}`)
		var a account
		b.Run("v1/"+in.name, func(b *testing.B) {
			for range b.N {
				_ = json.Unmarshal(data, &a)
			}
		})
		b.Run("v2/"+in.name, func(b *testing.B) {
			for range b.N {
				_ = jsonv2.Unmarshal(data, &a)
			}
		})
	}
}
