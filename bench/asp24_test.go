package bench

import (
	"github.com/asp24/decimal"
)

func init() {
	register(lib{
		name:   "asp24",
		add:    asp24Binary(decimal.Decimal.Add),
		mul:    asp24Binary(decimal.Decimal.Mul),
		quo:    asp24Binary(decimal.Decimal.Quo),
		powInt: asp24PowInt,
		sqrt:   asp24Unary(decimal.Decimal.Sqrt),
		exp:    asp24Unary(decimal.Decimal.Exp),
		log:    asp24Unary(decimal.Decimal.Log),
		parse:  asp24Parse,
		string: asp24String,
		telco:  asp24Telco,
	})
}

func asp24Binary(op func(d, e decimal.Decimal) (decimal.Decimal, error)) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z decimal.Decimal
		var err error
		for range n {
			d := decimal.MustNew(x.coef, x.scale)
			e := decimal.MustNew(y.coef, y.scale)
			z, err = op(d, e)
		}
		return z.String(), err
	}
}

func asp24Unary(op func(d decimal.Decimal) (decimal.Decimal, error)) unaryFunc {
	return func(n int, x num) (string, error) {
		var z decimal.Decimal
		var err error
		for range n {
			d := decimal.MustNew(x.coef, x.scale)
			z, err = op(d)
		}
		return z.String(), err
	}
}

func asp24PowInt(n int, x num, power int) (string, error) {
	var z decimal.Decimal
	var err error
	for range n {
		d := decimal.MustNew(x.coef, x.scale)
		z, err = d.PowInt(power)
	}
	return z.String(), err
}

func asp24Parse(n int, s string) (string, error) {
	var z decimal.Decimal
	var err error
	for range n {
		z, err = decimal.Parse(s)
	}
	return z.String(), err
}

func asp24String(n int, s string) (string, error) {
	d, err := decimal.Parse(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func asp24Telco(n int, durations []int64) (string, error) {
	var (
		baseRate    = decimal.MustParse("0.0013")
		distRate    = decimal.MustParse("0.00894")
		baseTaxRate = decimal.MustParse("0.0675")
		distTaxRate = decimal.MustParse("0.0341")
		total       = decimal.Zero
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price, err := decimal.MustNew(d, 0).Mul(rate)
		if err != nil {
			return "", err
		}
		price = price.Round(2)
		baseTax, err := price.Mul(baseTaxRate)
		if err != nil {
			return "", err
		}
		finalPrice, err := price.Add(baseTax.Trunc(2))
		if err != nil {
			return "", err
		}
		if d&1 != 0 {
			distTax, err := price.Mul(distTaxRate)
			if err != nil {
				return "", err
			}
			finalPrice, err = finalPrice.Add(distTax.Trunc(2))
			if err != nil {
				return "", err
			}
		}
		total, err = total.Add(finalPrice)
		if err != nil {
			return "", err
		}
	}
	return total.String(), nil
}
