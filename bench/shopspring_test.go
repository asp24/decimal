package bench

import (
	"github.com/shopspring/decimal"
)

func init() {
	// Inexact results are computed with 19 digits after the decimal point.
	decimal.DivisionPrecision = 19
	decimal.PowPrecisionNegativeExponent = 19

	register(lib{
		name:   "shopspring",
		add:    shopspringBinary(func(d, e decimal.Decimal) (decimal.Decimal, error) { return d.Add(e), nil }),
		mul:    shopspringBinary(func(d, e decimal.Decimal) (decimal.Decimal, error) { return d.Mul(e), nil }),
		quo:    shopspringBinary(func(d, e decimal.Decimal) (decimal.Decimal, error) { return d.Div(e), nil }),
		powInt: shopspringPowInt,
		// Sqrt is not implemented.
		exp:    shopspringUnary(func(d decimal.Decimal) (decimal.Decimal, error) { return d.ExpTaylor(19) }),
		log:    shopspringUnary(func(d decimal.Decimal) (decimal.Decimal, error) { return d.Ln(19) }),
		parse:  shopspringParse,
		string: shopspringString,
		telco:  shopspringTelco,
	})
}

func shopspringBinary(op func(d, e decimal.Decimal) (decimal.Decimal, error)) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z decimal.Decimal
		var err error
		for range n {
			d := decimal.New(x.coef, int32(-x.scale))
			e := decimal.New(y.coef, int32(-y.scale))
			z, err = op(d, e)
		}
		return z.String(), err
	}
}

func shopspringUnary(op func(d decimal.Decimal) (decimal.Decimal, error)) unaryFunc {
	return func(n int, x num) (string, error) {
		var z decimal.Decimal
		var err error
		for range n {
			d := decimal.New(x.coef, int32(-x.scale))
			z, err = op(d)
		}
		return z.String(), err
	}
}

func shopspringPowInt(n int, x num, power int) (string, error) {
	var z decimal.Decimal
	var err error
	for range n {
		d := decimal.New(x.coef, int32(-x.scale))
		z, err = d.PowInt32(int32(power))
	}
	return z.String(), err
}

func shopspringParse(n int, s string) (string, error) {
	var z decimal.Decimal
	var err error
	for range n {
		z, err = decimal.NewFromString(s)
	}
	return z.String(), err
}

func shopspringString(n int, s string) (string, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func shopspringTelco(n int, durations []int64) (string, error) {
	var (
		baseRate    = decimal.RequireFromString("0.0013")
		distRate    = decimal.RequireFromString("0.00894")
		baseTaxRate = decimal.RequireFromString("0.0675")
		distTaxRate = decimal.RequireFromString("0.0341")
		total       = decimal.Zero
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := decimal.NewFromInt(d).Mul(rate).RoundBank(2)
		finalPrice := price.Add(price.Mul(baseTaxRate).RoundDown(2))
		if d&1 != 0 {
			finalPrice = finalPrice.Add(price.Mul(distTaxRate).RoundDown(2))
		}
		total = total.Add(finalPrice)
	}
	return total.String(), nil
}
