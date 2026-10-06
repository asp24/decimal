package bench

import (
	"fmt"

	"github.com/ericlagergren/decimal"
)

func init() {
	register(lib{
		name:   "ericlagergren",
		add:    ericlagergrenBinary((*decimal.Big).Add),
		mul:    ericlagergrenBinary((*decimal.Big).Mul),
		quo:    ericlagergrenBinary((*decimal.Big).Quo),
		powInt: ericlagergrenPowInt,
		sqrt:   ericlagergrenUnary(ericlagergrenCtx.Sqrt),
		exp:    ericlagergrenUnary(ericlagergrenCtx.Exp),
		log:    ericlagergrenUnary(ericlagergrenCtx.Log),
		parse:  ericlagergrenParse,
		string: ericlagergrenString,
		telco:  ericlagergrenTelco,
	})
}

// ericlagergrenCtx rounds results to 19 significant digits, half to even.
// Every exceptional condition except Inexact and Rounded is reported as an error.
var ericlagergrenCtx = decimal.Context{
	Precision:     19,
	RoundingMode:  decimal.ToNearestEven,
	OperatingMode: decimal.GDA,
	Traps:         ^(decimal.Inexact | decimal.Rounded),
}

func ericlagergrenResult(z *decimal.Big) (string, error) {
	if err := z.Context.Err(); err != nil {
		return "", err
	}
	if !z.IsFinite() {
		return "", fmt.Errorf("result is %v", z)
	}
	return z.String(), nil
}

func ericlagergrenBinary(op func(z, x, y *decimal.Big) *decimal.Big) binaryFunc {
	return func(n int, x, y num) (string, error) {
		var z *decimal.Big
		for range n {
			d := decimal.New(x.coef, x.scale)
			e := decimal.New(y.coef, y.scale)
			z = decimal.WithContext(ericlagergrenCtx)
			op(z, d, e)
		}
		return ericlagergrenResult(z)
	}
}

func ericlagergrenUnary(op func(z, x *decimal.Big) *decimal.Big) unaryFunc {
	return func(n int, x num) (string, error) {
		var z *decimal.Big
		for range n {
			d := decimal.New(x.coef, x.scale)
			z = decimal.WithContext(ericlagergrenCtx)
			op(z, d)
		}
		return ericlagergrenResult(z)
	}
}

func ericlagergrenPowInt(n int, x num, power int) (string, error) {
	var z *decimal.Big
	for range n {
		d := decimal.New(x.coef, x.scale)
		p := decimal.New(int64(power), 0)
		z = decimal.WithContext(ericlagergrenCtx)
		ericlagergrenCtx.Pow(z, d, p)
	}
	return ericlagergrenResult(z)
}

func ericlagergrenParse(n int, s string) (string, error) {
	var z *decimal.Big
	for range n {
		z = decimal.WithContext(ericlagergrenCtx)
		if _, ok := z.SetString(s); !ok {
			return "", fmt.Errorf("cannot parse %q", s)
		}
	}
	return ericlagergrenResult(z)
}

func ericlagergrenString(n int, s string) (string, error) {
	d, ok := decimal.WithContext(ericlagergrenCtx).SetString(s)
	if !ok {
		return "", fmt.Errorf("cannot parse %q", s)
	}
	var z string
	for range n {
		z = d.String()
	}
	return z, nil
}

func ericlagergrenTelco(n int, durations []int64) (string, error) {
	// truncCtx is used for taxes, which are truncated toward zero.
	truncCtx := ericlagergrenCtx
	truncCtx.RoundingMode = decimal.ToZero
	var (
		baseRate    = decimal.New(13, 4)
		distRate    = decimal.New(894, 5)
		baseTaxRate = decimal.New(675, 4)
		distTaxRate = decimal.New(341, 4)
		total       = decimal.WithContext(ericlagergrenCtx)
	)
	for i := range n {
		d := durations[i%len(durations)]
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := decimal.WithContext(ericlagergrenCtx)
		price.Mul(decimal.New(d, 0), rate).Quantize(2)
		baseTax := decimal.WithContext(truncCtx)
		baseTax.Mul(price, baseTaxRate).Quantize(2)
		finalPrice := decimal.WithContext(ericlagergrenCtx)
		finalPrice.Add(price, baseTax)
		if d&1 != 0 {
			distTax := decimal.WithContext(truncCtx)
			distTax.Mul(price, distTaxRate).Quantize(2)
			finalPrice.Add(finalPrice, distTax)
		}
		total.Add(total, finalPrice)
	}
	// A failed operation produces NaN, which propagates to the total.
	return ericlagergrenResult(total)
}
