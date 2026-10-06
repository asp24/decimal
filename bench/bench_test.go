// Package bench compares the performance of github.com/asp24/decimal
// with other decimal packages.
//
// Every package is registered as a [lib] in its own file.
// A nil benchmark function means that the package does not support the operation.
// Before measuring, every benchmark verifies the result of a single iteration
// against a reference value, so that only correct implementations are compared.
//
// To compare the packages, run:
//
//	go test -bench . -count 10 > bench.txt
//	benchstat -col /mod bench.txt
package bench

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"testing"
)

// num is a decimal operand given as coef * 10^(-scale).
type num struct {
	coef  int64
	scale int
}

func (x num) String() string {
	r := new(big.Rat).SetFrac(big.NewInt(x.coef), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(x.scale)), nil))
	return r.FloatString(x.scale)
}

// Every benchmark function performs the operation n times
// and returns the string representation of the last result.
type (
	binaryFunc func(n int, x, y num) (string, error)
	powIntFunc func(n int, x num, power int) (string, error)
	unaryFunc  func(n int, x num) (string, error)
	stringFunc func(n int, s string) (string, error)
	telcoFunc  func(n int, durations []int64) (string, error)
)

// lib describes a decimal package under benchmark.
type lib struct {
	name string
	// tol is the relative tolerance for inexact results.
	// Zero means [tolerance]; set it only for packages that cannot
	// compute 19 significant digits and document why.
	tol    float64
	add    binaryFunc
	mul    binaryFunc
	quo    binaryFunc
	powInt powIntFunc
	sqrt   unaryFunc
	exp    unaryFunc
	log    unaryFunc
	// parse converts the string to a decimal.
	parse stringFunc
	// string converts the decimal, parsed once in advance, to a string.
	string stringFunc
	// telco computes the total final price of the calls, see [BenchmarkTelco].
	telco telcoFunc
}

// libOrder defines the order of packages in benchmark results.
var libOrder = []string{
	"asp24",
	"apd",
	"shopspring",
	"udecimal",
	"alpacadecimal",
	"decimal128",
	"ericlagergren",
}

var libs []lib

func register(l lib) {
	if !slices.Contains(libOrder, l.name) {
		panic(fmt.Sprintf("unknown lib %q", l.name))
	}
	libs = append(libs, l)
	slices.SortFunc(libs, func(a, b lib) int {
		return slices.Index(libOrder, a.name) - slices.Index(libOrder, b.name)
	})
}

// sink prevents the compiler from optimizing away benchmark results.
var sink string

// tolerance is the maximum relative error allowed for inexact results.
// Packages working with 19 digits of precision differ from the true
// mathematical value in the last digit at most.
const tolerance = 1e-17

// verify checks that got equals want within the relative tolerance.
func verify(b *testing.B, got, want string, tol float64) {
	b.Helper()
	g, ok := new(big.Rat).SetString(got)
	if !ok {
		b.Fatalf("result %q is not a number", got)
	}
	w, ok := new(big.Rat).SetString(want)
	if !ok {
		b.Fatalf("reference %q is not a number", want)
	}
	diff := new(big.Rat).Sub(g, w)
	diff.Abs(diff)
	limit := new(big.Rat).Abs(w)
	limit.Mul(limit, new(big.Rat).SetFloat64(tol))
	if diff.Cmp(limit) > 0 {
		b.Fatalf("result = %v, want %v", got, want)
	}
}

func (l lib) relTol() float64 {
	if l.tol != 0 {
		return l.tol
	}
	return tolerance
}

// run verifies the result of a single iteration and then measures f.
func run(b *testing.B, want string, tol float64, f func(n int) (string, error)) {
	b.Helper()
	got, err := f(1)
	if err != nil {
		b.Skipf("not supported: %v", err)
	}
	verify(b, got, want, tol)
	b.ReportAllocs()
	b.ResetTimer()
	sink, _ = f(b.N)
}

type binaryCase struct {
	name string
	x, y num
	want string
}

func benchBinary(b *testing.B, fn func(lib) binaryFunc, tests []binaryCase) {
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for _, l := range libs {
				f := fn(l)
				if f == nil {
					continue
				}
				b.Run("mod="+l.name, func(b *testing.B) {
					run(b, tt.want, l.relTol(), func(n int) (string, error) { return f(n, tt.x, tt.y) })
				})
			}
		})
	}
}

type unaryCase struct {
	name string
	x    num
	want string
}

func benchUnary(b *testing.B, fn func(lib) unaryFunc, tests []unaryCase) {
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for _, l := range libs {
				f := fn(l)
				if f == nil {
					continue
				}
				b.Run("mod="+l.name, func(b *testing.B) {
					run(b, tt.want, l.relTol(), func(n int) (string, error) { return f(n, tt.x) })
				})
			}
		})
	}
}

func BenchmarkAdd(b *testing.B) {
	benchBinary(b, func(l lib) binaryFunc { return l.add }, []binaryCase{
		{"5+6", num{5, 0}, num{6, 0}, "11"},
	})
}

func BenchmarkMul(b *testing.B) {
	benchBinary(b, func(l lib) binaryFunc { return l.mul }, []binaryCase{
		{"2*3", num{2, 0}, num{3, 0}, "6"},
	})
}

func BenchmarkQuo(b *testing.B) {
	benchBinary(b, func(l lib) binaryFunc { return l.quo }, []binaryCase{
		{"2÷4", num{2, 0}, num{4, 0}, "0.5"},
		{"2÷3", num{2, 0}, num{3, 0}, "0.666666666666666666666666666666666666666666666666666666666667"},
	})
}

func BenchmarkPowInt(b *testing.B) {
	tests := []struct {
		name  string
		x     num
		power int
		want  string
	}{
		{"1.1^60", num{11, 1}, 60, "304.481639541418099574449295360278774639038415066698088621948"},
		{"1.01^600", num{101, 2}, 600, "391.583396999319774257668921878069861111124307452872474467032"},
		{"1.001^6000", num{1001, 3}, 6000, "402.221124566355292341228046626329347329979347004480121426441"},
	}
	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for _, l := range libs {
				if l.powInt == nil {
					continue
				}
				b.Run("mod="+l.name, func(b *testing.B) {
					run(b, tt.want, l.relTol(), func(n int) (string, error) { return l.powInt(n, tt.x, tt.power) })
				})
			}
		})
	}
}

func BenchmarkSqrt(b *testing.B) {
	benchUnary(b, func(l lib) unaryFunc { return l.sqrt }, []unaryCase{
		{"2", num{2, 0}, "1.41421356237309504880168872420969807856967187537694807317668"},
	})
}

func BenchmarkExp(b *testing.B) {
	benchUnary(b, func(l lib) unaryFunc { return l.exp }, []unaryCase{
		{"0.5", num{5, 1}, "1.64872127070012814684865078781416357165377610071014801157508"},
	})
}

func BenchmarkLog(b *testing.B) {
	benchUnary(b, func(l lib) unaryFunc { return l.log }, []unaryCase{
		{"0.5", num{5, 1}, "-0.693147180559945309417232121458176568075500134360255254120680"},
	})
}

var stringCases = []string{
	"1",
	"123.456",
	"123456789.1234567890",
}

func benchString(b *testing.B, fn func(lib) stringFunc) {
	for _, s := range stringCases {
		b.Run(s, func(b *testing.B) {
			for _, l := range libs {
				f := fn(l)
				if f == nil {
					continue
				}
				b.Run("mod="+l.name, func(b *testing.B) {
					run(b, s, 0, func(n int) (string, error) { return f(n, s) })
				})
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	benchString(b, func(l lib) stringFunc { return l.parse })
}

func BenchmarkString(b *testing.B) {
	benchString(b, func(l lib) stringFunc { return l.string })
}

// telcoDurations returns call durations in seconds, exponentially distributed
// with the mean of 180 seconds, like the "expon180.1e6b" file of the
// [Telco benchmark].
//
// [Telco benchmark]: https://speleotrove.com/decimal/telco.html
func telcoDurations() []int64 {
	r := rand.New(rand.NewPCG(180, 1_000_000))
	durations := make([]int64, 1_000_000)
	for i := range durations {
		durations[i] = int64(r.ExpFloat64() * 180)
	}
	return durations
}

// BenchmarkTelco implements the computational part of the [Telco benchmark]
// by Mike Cowlishaw. The I/O part is not implemented.
// Every iteration prices a single call:
//
//	price      = round_half_even(duration * rate, 2)
//	baseTax    = trunc(price * 0.0675, 2)
//	distTax    = trunc(price * 0.0341, 2) // distance calls only
//	finalPrice = price + baseTax + distTax
//
// The rate is 0.0013 for local calls (even durations)
// and 0.00894 for distance calls (odd durations).
// The function returns the sum of final prices of all calls.
//
// [Telco benchmark]: https://speleotrove.com/decimal/telco.html
func BenchmarkTelco(b *testing.B) {
	durations := telcoDurations()
	// Reference total for the first 100_000 calls, computed with math/big.
	const checkCalls = 100_000
	want := telcoReference(durations[:checkCalls])
	for _, l := range libs {
		if l.telco == nil {
			continue
		}
		b.Run("mod="+l.name, func(b *testing.B) {
			got, err := l.telco(checkCalls, durations)
			if err != nil {
				b.Skipf("not supported: %v", err)
			}
			verify(b, got, want, 0)
			b.ReportAllocs()
			b.ResetTimer()
			sink, _ = l.telco(b.N, durations)
		})
	}
}

// telcoReference computes the Telco total with exact rational arithmetic.
func telcoReference(durations []int64) string {
	rat := func(s string) *big.Rat {
		r, _ := new(big.Rat).SetString(s)
		return r
	}
	// round rounds x to 2 decimal places, half to even if halfEven
	// is true, toward zero otherwise.
	round := func(x *big.Rat, halfEven bool) *big.Rat {
		scaled := new(big.Rat).Mul(x, big.NewRat(100, 1))
		q, m := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
		if halfEven {
			twice := new(big.Int).Mul(m.Abs(m), big.NewInt(2))
			switch twice.Cmp(scaled.Denom()) {
			case 1:
				q.Add(q, big.NewInt(int64(scaled.Sign())))
			case 0:
				if q.Bit(0) == 1 {
					q.Add(q, big.NewInt(int64(scaled.Sign())))
				}
			}
		}
		return new(big.Rat).SetFrac(q, big.NewInt(100))
	}
	baseRate, distRate := rat("0.0013"), rat("0.00894")
	baseTaxRate, distTaxRate := rat("0.0675"), rat("0.0341")
	total := new(big.Rat)
	for _, d := range durations {
		rate := baseRate
		if d&1 != 0 {
			rate = distRate
		}
		price := round(new(big.Rat).Mul(big.NewRat(d, 1), rate), true)
		finalPrice := new(big.Rat).Add(price, round(new(big.Rat).Mul(price, baseTaxRate), false))
		if d&1 != 0 {
			finalPrice.Add(finalPrice, round(new(big.Rat).Mul(price, distTaxRate), false))
		}
		total.Add(total, finalPrice)
	}
	return total.FloatString(2)
}
