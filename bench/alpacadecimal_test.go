package bench

import (
	"github.com/alpacahq/alpacadecimal"
)

func init() {
	// Div rounds inexact quotients to DivisionPrecision digits after the
	// decimal point (half away from zero, the rounding mode cannot be changed).
	alpacadecimal.DivisionPrecision = 19
	register(lib{
		name:   "alpacadecimal",
		add:    alpacadecimalBinary(alpacadecimal.Decimal.Add),
		mul:    alpacadecimalBinary(alpacadecimal.Decimal.Mul),
		quo:    alpacadecimalBinary(alpacadecimal.Decimal.Div),
		powInt: alpacadecimalPowInt,
		exp:    alpacadecimalExp,
		parse:  alpacadecimalParse,
		string: alpacadecimalString,
		telco:  alpacadecimalTelco,
	})
}

func alpacadecimalBinary(op func(d, e alpacadecimal.Decimal) alpacadecimal.Decimal) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z alpacadecimal.Decimal
		for range n {
			d := alpacadecimal.New(x.coef, int32(-x.scale))
			e := alpacadecimal.New(y.coef, int32(-y.scale))
			z = op(d, e)
		}
		return z.String(), nil
	}
}

func alpacadecimalPowInt(n int, x num, power int) (string, error) {
	var z alpacadecimal.Decimal
	for range n {
		d := alpacadecimal.New(x.coef, int32(-x.scale))
		z = d.Pow(alpacadecimal.NewFromInt(int64(power)))
	}
	return z.String(), nil
}

func alpacadecimalExp(n int, x num) (string, error) {
	var z alpacadecimal.Decimal
	var err error
	for range n {
		d := alpacadecimal.New(x.coef, int32(-x.scale))
		z, err = d.ExpHullAbrham(19)
	}
	return z.String(), err
}

func alpacadecimalParse(n int, s string) (string, error) {
	var z alpacadecimal.Decimal
	var err error
	for range n {
		z, err = alpacadecimal.NewFromString(s)
	}
	return z.String(), err
}

func alpacadecimalString(n int, s string) (string, error) {
	d, err := alpacadecimal.NewFromString(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func alpacadecimalTelco(n int, durations []int64) (string, error) {
	var (
		baseRate    = alpacadecimal.RequireFromString("0.0013")
		distRate    = alpacadecimal.RequireFromString("0.00894")
		baseTaxRate = alpacadecimal.RequireFromString("0.0675")
		distTaxRate = alpacadecimal.RequireFromString("0.0341")
		total       = alpacadecimal.Zero
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := alpacadecimal.NewFromInt(d).Mul(rate).RoundBank(2)
		finalPrice := price.Add(price.Mul(baseTaxRate).Truncate(2))
		if d&1 != 0 {
			finalPrice = finalPrice.Add(price.Mul(distTaxRate).Truncate(2))
		}
		total = total.Add(finalPrice)
	}
	return total.String(), nil
}
