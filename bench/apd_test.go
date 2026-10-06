package bench

import (
	"github.com/cockroachdb/apd/v3"
)

// apdContext computes results with 19 digits of precision,
// rounding half to even, like asp24.
var apdContext = apd.Context{
	Precision:   19,
	MaxExponent: apd.MaxExponent,
	MinExponent: apd.MinExponent,
	Traps:       apd.DefaultTraps,
	Rounding:    apd.RoundHalfEven,
}

func init() {
	register(lib{
		name:   "apd",
		add:    apdBinary(apdContext.Add),
		mul:    apdBinary(apdContext.Mul),
		quo:    apdBinary(apdContext.Quo),
		powInt: apdPowInt,
		sqrt:   apdUnary(apdContext.Sqrt),
		exp:    apdUnary(apdContext.Exp),
		log:    apdUnary(apdContext.Ln),
		parse:  apdParse,
		string: apdString,
		telco:  apdTelco,
	})
}

func apdBinary(op func(z, x, y *apd.Decimal) (apd.Condition, error)) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z *apd.Decimal
		var err error
		for range n {
			d := apd.New(x.coef, int32(-x.scale))
			e := apd.New(y.coef, int32(-y.scale))
			z = apd.New(0, 0)
			_, err = op(z, d, e)
		}
		return z.Text('f'), err
	}
}

func apdUnary(op func(z, x *apd.Decimal) (apd.Condition, error)) unaryFunc {
	return func(n int, x num) (string, error) {
		var z *apd.Decimal
		var err error
		for range n {
			d := apd.New(x.coef, int32(-x.scale))
			z = apd.New(0, 0)
			_, err = op(z, d)
		}
		return z.Text('f'), err
	}
}

func apdPowInt(n int, x num, power int) (string, error) {
	var z *apd.Decimal
	var err error
	for range n {
		d := apd.New(x.coef, int32(-x.scale))
		e := apd.New(int64(power), 0)
		z = apd.New(0, 0)
		_, err = apdContext.Pow(z, d, e)
	}
	return z.Text('f'), err
}

func apdParse(n int, s string) (string, error) {
	var z *apd.Decimal
	var err error
	for range n {
		z, _, err = apd.NewFromString(s)
	}
	return z.Text('f'), err
}

func apdString(n int, s string) (string, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.Text('f')
	}
	return z, nil
}

func apdTelco(n int, durations []int64) (string, error) {
	var (
		baseRate    = apd.New(13, -4)
		distRate    = apd.New(894, -5)
		baseTaxRate = apd.New(675, -4)
		distTaxRate = apd.New(341, -4)
		total       = apd.New(0, 0)
		truncate    = apdContext.WithPrecision(19)
	)
	truncate.Rounding = apd.RoundDown
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := new(apd.Decimal)
		if _, err := apdContext.Mul(price, apd.New(d, 0), rate); err != nil {
			return "", err
		}
		if _, err := apdContext.Quantize(price, price, -2); err != nil {
			return "", err
		}
		baseTax := new(apd.Decimal)
		if _, err := apdContext.Mul(baseTax, price, baseTaxRate); err != nil {
			return "", err
		}
		if _, err := truncate.Quantize(baseTax, baseTax, -2); err != nil {
			return "", err
		}
		finalPrice := new(apd.Decimal)
		if _, err := apdContext.Add(finalPrice, price, baseTax); err != nil {
			return "", err
		}
		if d&1 != 0 {
			distTax := new(apd.Decimal)
			if _, err := apdContext.Mul(distTax, price, distTaxRate); err != nil {
				return "", err
			}
			if _, err := truncate.Quantize(distTax, distTax, -2); err != nil {
				return "", err
			}
			if _, err := apdContext.Add(finalPrice, finalPrice, distTax); err != nil {
				return "", err
			}
		}
		if _, err := apdContext.Add(total, total, finalPrice); err != nil {
			return "", err
		}
	}
	return total.Text('f'), nil
}
