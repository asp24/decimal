package bench

import (
	"github.com/quagmt/udecimal"
)

// udecimal has a fixed precision of 19 digits after the decimal point
// (the default and maximum, see [udecimal.SetDefaultPrecision]).
// Mul, Div, PowInt32 and Sqrt truncate results to this precision,
// the rounding mode is not configurable.
// Exp and Log are not implemented.
func init() {
	register(lib{
		name:   "udecimal",
		add:    udecimalBinary(func(d, e udecimal.Decimal) (udecimal.Decimal, error) { return d.Add(e), nil }),
		mul:    udecimalBinary(func(d, e udecimal.Decimal) (udecimal.Decimal, error) { return d.Mul(e), nil }),
		quo:    udecimalBinary(udecimal.Decimal.Div),
		powInt: udecimalPowInt,
		sqrt:   udecimalUnary(udecimal.Decimal.Sqrt),
		parse:  udecimalParse,
		string: udecimalString,
		telco:  udecimalTelco,
	})
}

func udecimalBinary(op func(d, e udecimal.Decimal) (udecimal.Decimal, error)) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z udecimal.Decimal
		var err error
		for range n {
			d := udecimal.MustFromInt64(x.coef, uint8(x.scale))
			e := udecimal.MustFromInt64(y.coef, uint8(y.scale))
			z, err = op(d, e)
		}
		return z.String(), err
	}
}

func udecimalUnary(op func(d udecimal.Decimal) (udecimal.Decimal, error)) unaryFunc {
	return func(n int, x num) (string, error) {
		var z udecimal.Decimal
		var err error
		for range n {
			d := udecimal.MustFromInt64(x.coef, uint8(x.scale))
			z, err = op(d)
		}
		return z.String(), err
	}
}

func udecimalPowInt(n int, x num, power int) (string, error) {
	var z udecimal.Decimal
	var err error
	for range n {
		d := udecimal.MustFromInt64(x.coef, uint8(x.scale))
		z, err = d.PowInt32(int32(power))
	}
	return z.String(), err
}

func udecimalParse(n int, s string) (string, error) {
	var z udecimal.Decimal
	var err error
	for range n {
		z, err = udecimal.Parse(s)
	}
	return z.String(), err
}

func udecimalString(n int, s string) (string, error) {
	d, err := udecimal.Parse(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func udecimalTelco(n int, durations []int64) (string, error) {
	var (
		baseRate    = udecimal.MustParse("0.0013")
		distRate    = udecimal.MustParse("0.00894")
		baseTaxRate = udecimal.MustParse("0.0675")
		distTaxRate = udecimal.MustParse("0.0341")
		total       = udecimal.Zero
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := udecimal.MustFromInt64(d, 0).Mul(rate).RoundBank(2)
		finalPrice := price.Add(price.Mul(baseTaxRate).Trunc(2))
		if d&1 != 0 {
			finalPrice = finalPrice.Add(price.Mul(distTaxRate).Trunc(2))
		}
		total = total.Add(finalPrice)
	}
	return total.String(), nil
}
