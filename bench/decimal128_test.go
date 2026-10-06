package bench

import (
	"fmt"

	"github.com/woodsbury/decimal128"
)

func init() {
	register(lib{
		name:   "decimal128",
		add:    decimal128Binary(decimal128.Decimal.Add),
		mul:    decimal128Binary(decimal128.Decimal.Mul),
		quo:    decimal128Binary(decimal128.Decimal.Quo),
		powInt: decimal128PowInt,
		sqrt:   decimal128Unary(decimal128.Sqrt),
		exp:    decimal128Unary(decimal128.Exp),
		log:    decimal128Unary(decimal128.Log),
		parse:  decimal128Parse,
		string: decimal128String,
		telco:  decimal128Telco,
	})
}

// decimal128Result converts NaN and infinite results, which the package
// returns instead of errors, into an error.
func decimal128Result(z decimal128.Decimal) (string, error) {
	if z.IsNaN() || z.IsInf(0) {
		return "", fmt.Errorf("result is %v", z)
	}
	return z.String(), nil
}

func decimal128Binary(op func(d, e decimal128.Decimal) decimal128.Decimal) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z decimal128.Decimal
		for range n {
			d := decimal128.New(x.coef, -x.scale)
			e := decimal128.New(y.coef, -y.scale)
			z = op(d, e)
		}
		return decimal128Result(z)
	}
}

func decimal128Unary(op func(d decimal128.Decimal) decimal128.Decimal) unaryFunc {
	return func(n int, x num) (string, error) {
		var z decimal128.Decimal
		for range n {
			d := decimal128.New(x.coef, -x.scale)
			z = op(d)
		}
		return decimal128Result(z)
	}
}

func decimal128PowInt(n int, x num, power int) (string, error) {
	var z decimal128.Decimal
	for range n {
		d := decimal128.New(x.coef, -x.scale)
		z = d.Pow(decimal128.FromInt64(int64(power)))
	}
	return decimal128Result(z)
}

func decimal128Parse(n int, s string) (string, error) {
	var z decimal128.Decimal
	var err error
	for range n {
		z, err = decimal128.Parse(s)
	}
	if err != nil {
		return "", err
	}
	return decimal128Result(z)
}

func decimal128String(n int, s string) (string, error) {
	d, err := decimal128.Parse(s)
	if err != nil {
		return "", err
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func decimal128Telco(n int, durations []int64) (string, error) {
	var (
		baseRate    = decimal128.MustParse("0.0013")
		distRate    = decimal128.MustParse("0.00894")
		baseTaxRate = decimal128.MustParse("0.0675")
		distTaxRate = decimal128.MustParse("0.0341")
		total       decimal128.Decimal
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := decimal128.New(d, 0).Mul(rate).Round(2, decimal128.ToNearestEven)
		baseTax := price.Mul(baseTaxRate).Round(2, decimal128.ToZero)
		finalPrice := price.Add(baseTax)
		if d&1 != 0 {
			distTax := price.Mul(distTaxRate).Round(2, decimal128.ToZero)
			finalPrice = finalPrice.Add(distTax)
		}
		total = total.Add(finalPrice)
	}
	return decimal128Result(total)
}
