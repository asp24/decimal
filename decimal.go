package decimal

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"unicode/utf8"
	"unsafe"
)

// Decimal represents a finite floating-point decimal number.
// Its zero value corresponds to the numeric value of 0.
// Decimal is designed to be safe for concurrent use by multiple goroutines.
type Decimal struct {
	neg   bool // indicates whether the decimal is negative
	scale int8 // position of the floating decimal point
	coef  fint // numeric value without decimal point
}

const (
	MaxPrec  = 19      // MaxPrec is the maximum length of the coefficient in decimal digits.
	MinScale = 0       // MinScale is the minimum number of digits after the decimal point.
	MaxScale = 19      // MaxScale is the maximum number of digits after the decimal point.
	maxCoef  = maxFint // maxCoef is the maximum absolute value of the coefficient, which is equal to (10^MaxPrec - 1).
)

var (
	NegOne              = MustNew(-1, 0)                         // NegOne represents the decimal value of -1.
	Zero                = MustNew(0, 0)                          // Zero represents the decimal value of 0. For comparison purposes, use the IsZero method.
	One                 = MustNew(1, 0)                          // One represents the decimal value of 1.
	Two                 = MustNew(2, 0)                          // Two represents the decimal value of 2.
	Ten                 = MustNew(10, 0)                         // Ten represents the decimal value of 10.
	Hundred             = MustNew(100, 0)                        // Hundred represents the decimal value of 100.
	Thousand            = MustNew(1_000, 0)                      // Thousand represents the decimal value of 1,000.
	E                   = MustNew(2_718_281_828_459_045_235, 18) // E represents Euler’s number rounded to 18 digits.
	Pi                  = MustNew(3_141_592_653_589_793_238, 18) // Pi represents the value of π rounded to 18 digits.
	errDecimalOverflow  = errors.New("decimal overflow")
	errInvalidDecimal   = errors.New("invalid decimal")
	errScaleRange       = errors.New("scale out of range")
	errInvalidOperation = errors.New("invalid operation")
	errInexactDivision  = errors.New("inexact division")
	errDivisionByZero   = errors.New("division by zero")
)

// newUnsafe creates a new decimal without checking the scale and coefficient.
// Use it only if you are absolutely sure that the arguments are valid.
func newUnsafe(neg bool, coef fint, scale int) Decimal {
	if coef == 0 {
		neg = false
	}
	//nolint:gosec
	return Decimal{neg: neg, coef: coef, scale: int8(scale)}
}

// newSafe creates a new decimal and checks the scale and coefficient.
func newSafe(neg bool, coef fint, scale int) (Decimal, error) {
	switch {
	case scale < MinScale || scale > MaxScale:
		return Decimal{}, errScaleRange
	case coef > maxCoef:
		return Decimal{}, errDecimalOverflow
	}
	return newUnsafe(neg, coef, scale), nil
}

// newFromFint creates a new decimal from a uint64 coefficient.
// This method does not use overflowError to return descriptive errors,
// as it must be as fast as possible.
func newFromFint(neg bool, coef fint, scale, minScale int) (Decimal, error) {
	var ok bool
	// Scale normalization
	switch {
	case scale < minScale:
		coef, ok = coef.lsh(minScale - scale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
		scale = minScale
	case scale > MaxScale:
		coef = coef.rshHalfEven(scale - MaxScale)
		scale = MaxScale
	}
	return newSafe(neg, coef, scale)
}

func overflowError(gotPrec, gotScale, wantScale int) error {
	maxDigits := MaxPrec - wantScale
	gotDigits := gotPrec - gotScale
	switch wantScale {
	case 0:
		return fmt.Errorf("%w: the integer part of a %T can have at most %v digits, but it has %v digits", errDecimalOverflow, Decimal{}, maxDigits, gotDigits)
	default:
		return fmt.Errorf("%w: with %v significant digits after the decimal point, the integer part of a %T can have at most %v digits, but it has %v digits", errDecimalOverflow, wantScale, Decimal{}, maxDigits, gotDigits)
	}
}

func unknownOverflowError() error {
	return fmt.Errorf("%w: the integer part of a %T can have at most %v digits, but it has significantly more digits", errDecimalOverflow, Decimal{}, MaxPrec)
}

// newFromBint creates a new decimal from a *big.Int coefficient.
// This method uses overflowError to return descriptive errors.
func newFromBint(neg bool, coef *bint, scale, minScale int) (Decimal, error) {
	// Overflow validation
	prec := coef.prec()
	if prec-scale > MaxPrec-minScale {
		return Decimal{}, overflowError(prec, scale, minScale)
	}
	// Scale normalization
	switch {
	case scale < minScale:
		coef.lsh(coef, minScale-scale)
		scale = minScale
	case scale >= prec && scale > MaxScale: // no integer part
		coef.rshHalfEven(coef, scale-MaxScale)
		scale = MaxScale
	case prec > scale && prec > MaxPrec: // there is an integer part
		coef.rshHalfEven(coef, prec-MaxPrec)
		scale = MaxPrec - prec + scale
	}
	// Handling the rare case when rshHalfEven rounded
	// a 19-digit coefficient to a 20-digit coefficient.
	if coef.hasPrec(MaxPrec + 1) {
		return newFromBint(neg, coef, scale, minScale)
	}
	return newSafe(neg, coef.fint(), scale)
}

// New returns a decimal equal to value / 10^scale.
// New keeps trailing zeros in the fractional part to preserve scale.
//
// New returns an error if the scale is negative or greater than [MaxScale].
func New(value int64, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, errScaleRange
	}
	// The absolute value of any int64, including math.MinInt64,
	// fits into fint and does not exceed maxCoef.
	neg := value < 0
	coef := fint(value) //nolint:gosec
	if neg {
		coef = -coef
	}
	return newUnsafe(neg, coef, scale), nil
}

// MustNew is like [New] but panics if the decimal cannot be constructed.
// It simplifies safe initialization of global variables holding decimals.
func MustNew(value int64, scale int) Decimal {
	d, err := New(value, scale)
	if err != nil {
		panic(fmt.Sprintf("New(%v, %v) failed: %v", value, scale, err))
	}
	return d
}

// Scale returns the number of digits after the decimal point.
// See also methods [Decimal.Prec], [Decimal.MinScale].
func (d Decimal) Scale() int {
	return int(d.scale)
}

// Coef returns the coefficient of the decimal.
// See also method [Decimal.Prec].
func (d Decimal) Coef() uint64 {
	return uint64(d.coef)
}

// Prec returns the number of digits in the coefficient.
// See also method [Decimal.Coef].
func (d Decimal) Prec() int {
	return d.coef.prec()
}

// IsNeg returns:
//
//	true  if d < 0
//	false otherwise
func (d Decimal) IsNeg() bool {
	return d.neg
}

// IsZero returns:
//
//	true  if d = 0
//	false otherwise
func (d Decimal) IsZero() bool {
	return d.coef == 0
}

// IsPos returns:
//
//	true  if d > 0
//	false otherwise
func (d Decimal) IsPos() bool {
	return d.coef != 0 && !d.neg
}

// Sign returns:
//
//	-1 if d < 0
//	 0 if d = 0
//	+1 if d > 0
//
// See also methods [Decimal.IsPos], [Decimal.IsNeg], [Decimal.IsZero].
func (d Decimal) Sign() int {
	switch {
	case d.neg:
		return -1
	case d.coef == 0:
		return 0
	}
	return 1
}

// IsInt returns true if there are no significant digits after the decimal point.
func (d Decimal) IsInt() bool {
	return d.Scale() == 0 || d.coef%pow10[d.Scale()] == 0
}

// IsOne returns:
//
//	true  if d = -1 or d = 1
//	false otherwise
func (d Decimal) IsOne() bool {
	return d.coef == pow10[d.Scale()]
}

// WithinOne returns:
//
//	true  if -1 < d < 1
//	false otherwise
func (d Decimal) WithinOne() bool {
	return d.coef < pow10[d.Scale()]
}

// MinScale returns the smallest scale that the decimal can be rescaled to
// without rounding.
// See also method [Decimal.Trim].
func (d Decimal) MinScale() int {
	// Special case: zero
	if d.IsZero() {
		return MinScale
	}
	// General case
	dcoef := d.coef
	return max(MinScale, d.Scale()-dcoef.ntz())
}

// Zero returns a decimal with a value of 0, having the same scale as decimal d.
// See also methods [Decimal.One], [Decimal.ULP].
func (d Decimal) Zero() Decimal {
	return newUnsafe(false, 0, d.Scale())
}

// One returns a decimal with a value of 1, having the same scale as decimal d.
// See also methods [Decimal.Zero], [Decimal.ULP].
func (d Decimal) One() Decimal {
	return newUnsafe(false, pow10[d.Scale()], d.Scale())
}

// ULP (Unit in the Last Place) returns the smallest representable positive
// difference between two decimals with the same scale as decimal d.
// It can be useful for implementing rounding and comparison algorithms.
// See also methods [Decimal.Zero], [Decimal.One].
func (d Decimal) ULP() Decimal {
	return newUnsafe(false, 1, d.Scale())
}

// Round returns a decimal rounded to the specified number of digits after
// the decimal point using [rounding half to even] (banker's rounding).
// If the given scale is negative, it is redefined to zero.
// For financial calculations, the scale should be equal to or greater than
// the scale of the currency.
// See also method [Decimal.Rescale].
//
// [rounding half to even]: https://en.wikipedia.org/wiki/Rounding#Rounding_half_to_even
func (d Decimal) Round(scale int) Decimal {
	scale = max(scale, MinScale)
	if scale >= d.Scale() {
		return d
	}
	coef := d.coef
	coef = coef.rshHalfEven(d.Scale() - scale)
	return newUnsafe(d.IsNeg(), coef, scale)
}

// Pad returns a decimal zero-padded to the specified number of digits after
// the decimal point.
// The total number of digits in the result is limited by [MaxPrec].
// See also method [Decimal.Trim].
func (d Decimal) Pad(scale int) Decimal {
	scale = min(scale, MaxScale, MaxPrec-d.Prec()+d.Scale())
	if scale <= d.Scale() {
		return d
	}
	coef := d.coef
	coef, ok := coef.lsh(scale - d.Scale())
	if !ok {
		return d // Should never happen
	}
	return newUnsafe(d.IsNeg(), coef, scale)
}

// Rescale returns a decimal rounded or zero-padded to the given number of digits
// after the decimal point.
// If the given scale is negative, it is redefined to zero.
// For financial calculations, the scale should be equal to or greater than
// the scale of the currency.
// See also methods [Decimal.Round], [Decimal.Pad].
func (d Decimal) Rescale(scale int) Decimal {
	if scale > d.Scale() {
		return d.Pad(scale)
	}
	return d.Round(scale)
}

// Quantize returns a decimal rescaled to the same scale as decimal e.
// The sign and the coefficient of decimal e are ignored.
// See also methods [Decimal.SameScale] and [Decimal.Rescale].
func (d Decimal) Quantize(e Decimal) Decimal {
	return d.Rescale(e.Scale())
}

// SameScale returns true if decimals have the same scale.
// See also methods [Decimal.Scale], [Decimal.Quantize].
func (d Decimal) SameScale(e Decimal) bool {
	return d.Scale() == e.Scale()
}

// Trunc returns a decimal truncated to the specified number of digits
// after the decimal point using [rounding toward zero].
// If the given scale is negative, it is redefined to zero.
// For financial calculations, the scale should be equal to or greater than
// the scale of the currency.
//
// [rounding toward zero]: https://en.wikipedia.org/wiki/Rounding#Rounding_toward_zero
func (d Decimal) Trunc(scale int) Decimal {
	scale = max(scale, MinScale)
	if scale >= d.Scale() {
		return d
	}
	coef := d.coef
	coef = coef.rshDown(d.Scale() - scale)
	return newUnsafe(d.IsNeg(), coef, scale)
}

// Trim returns a decimal with trailing zeros removed up to the given number of
// digits after the decimal point.
// If the given scale is negative, it is redefined to zero.
// See also method [Decimal.Pad].
func (d Decimal) Trim(scale int) Decimal {
	if d.Scale() <= scale {
		return d
	}
	scale = max(scale, MinScale)
	coef, n := d.coef.trimZeros(d.Scale() - scale)
	return newUnsafe(d.IsNeg(), coef, d.Scale()-n)
}

// Ceil returns a decimal rounded up to the given number of digits
// after the decimal point using [rounding toward positive infinity].
// If the given scale is negative, it is redefined to zero.
// For financial calculations, the scale should be equal to or greater than
// the scale of the currency.
// See also method [Decimal.Floor].
//
// [rounding toward positive infinity]: https://en.wikipedia.org/wiki/Rounding#Rounding_up
func (d Decimal) Ceil(scale int) Decimal {
	scale = max(scale, MinScale)
	if scale >= d.Scale() {
		return d
	}
	coef := d.coef
	if d.IsNeg() {
		coef = coef.rshDown(d.Scale() - scale)
	} else {
		coef = coef.rshUp(d.Scale() - scale)
	}
	return newUnsafe(d.IsNeg(), coef, scale)
}

// Floor returns a decimal rounded down to the specified number of digits
// after the decimal point using [rounding toward negative infinity].
// If the given scale is negative, it is redefined to zero.
// For financial calculations, the scale should be equal to or greater than
// the scale of the currency.
// See also method [Decimal.Ceil].
//
// [rounding toward negative infinity]: https://en.wikipedia.org/wiki/Rounding#Rounding_down
func (d Decimal) Floor(scale int) Decimal {
	scale = max(scale, MinScale)
	if scale >= d.Scale() {
		return d
	}
	coef := d.coef
	if d.IsNeg() {
		coef = coef.rshUp(d.Scale() - scale)
	} else {
		coef = coef.rshDown(d.Scale() - scale)
	}
	return newUnsafe(d.IsNeg(), coef, scale)
}

// Neg returns a decimal with the opposite sign.
func (d Decimal) Neg() Decimal {
	return newUnsafe(!d.IsNeg(), d.coef, d.Scale())
}

// Abs returns the absolute value of the decimal.
func (d Decimal) Abs() Decimal {
	return newUnsafe(false, d.coef, d.Scale())
}

// CopySign returns a decimal with the same sign as decimal e.
// CopySign treates 0 as positive.
// See also method [Decimal.Sign].
func (d Decimal) CopySign(e Decimal) Decimal {
	if d.IsNeg() == e.IsNeg() {
		return d
	}
	return d.Neg()
}

// cmpFint compares decimals using uint64 arithmetic.
func (d Decimal) cmpFint(e Decimal) (int, error) {
	dcoef := d.coef
	ecoef := e.coef

	// Alignment
	var ok bool
	switch {
	case d.Scale() > e.Scale():
		ecoef, ok = ecoef.lsh(d.Scale() - e.Scale())
		if !ok {
			return 0, errDecimalOverflow
		}
	case d.Scale() < e.Scale():
		dcoef, ok = dcoef.lsh(e.Scale() - d.Scale())
		if !ok {
			return 0, errDecimalOverflow
		}
	}

	// Comparison
	switch {
	case dcoef > ecoef:
		return d.Sign(), nil
	case ecoef > dcoef:
		return -e.Sign(), nil
	}
	return 0, nil
}

// cmpBint compares decimals using *big.Int arithmetic.
func (d Decimal) cmpBint(e Decimal) int {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	ecoef.setFint(e.coef)

	// Alignment
	switch {
	case d.Scale() > e.Scale():
		ecoef.lsh(ecoef, d.Scale()-e.Scale())
	case d.Scale() < e.Scale():
		dcoef.lsh(dcoef, e.Scale()-d.Scale())
	}

	// Comparison
	switch dcoef.cmp(ecoef) {
	case 1:
		return d.Sign()
	case -1:
		return -e.Sign()
	}
	return 0
}

// Cmp compares decimals and returns:
//
//	-1 if d < e
//	 0 if d = e
//	+1 if d > e
//
// See also methods [Decimal.CmpAbs], [Decimal.CmpTotal].
func (d Decimal) Cmp(e Decimal) int {
	// Special case: different signs
	switch {
	case d.Sign() > e.Sign():
		return 1
	case d.Sign() < e.Sign():
		return -1
	}

	// General case
	r, err := d.cmpFint(e)
	if err != nil {
		r = d.cmpBint(e)
	}
	return r
}

// CmpAbs compares absolute values of decimals and returns:
//
//	-1 if |d| < |e|
//	 0 if |d| = |e|
//	+1 if |d| > |e|
//
// See also method [Decimal.Cmp].
func (d Decimal) CmpAbs(e Decimal) int {
	d, e = d.Abs(), e.Abs()
	return d.Cmp(e)
}

// CmpTotal compares decimal representations and returns:
//
//	-1 if d < e
//	-1 if d = e and d.scale > e.scale
//	 0 if d = e and d.scale = e.scale
//	+1 if d = e and d.scale < e.scale
//	+1 if d > e
//
// See also method [Decimal.Cmp].
func (d Decimal) CmpTotal(e Decimal) int {
	switch d.Cmp(e) {
	case -1:
		return -1
	case 1:
		return 1
	}
	switch {
	case d.Scale() > e.Scale():
		return -1
	case d.Scale() < e.Scale():
		return 1
	}
	return 0
}

// Equal compares decimals and returns:
//
//	 true if d = e
//	false otherwise
//
// See also method [Decimal.Cmp].
func (d Decimal) Equal(e Decimal) bool {
	return d.Cmp(e) == 0
}

// Less compares decimals and returns:
//
//	 true if d < e
//	false otherwise
//
// See also method [Decimal.Cmp].
func (d Decimal) Less(e Decimal) bool {
	return d.Cmp(e) < 0
}

// Max returns the larger decimal.
// See also method [Decimal.CmpTotal].
func (d Decimal) Max(e Decimal) Decimal {
	if d.CmpTotal(e) >= 0 {
		return d
	}
	return e
}

// Min returns the smaller decimal.
// See also method [Decimal.CmpTotal].
func (d Decimal) Min(e Decimal) Decimal {
	if d.CmpTotal(e) <= 0 {
		return d
	}
	return e
}

// Clamp compares decimals and returns:
//
//	min if d < min
//	max if d > max
//	  d otherwise
//
// See also method [Decimal.CmpTotal].
//
// Clamp returns an error if min is greater than max numerically.
//
//nolint:revive
func (d Decimal) Clamp(min, max Decimal) (Decimal, error) {
	if min.Cmp(max) > 0 {
		return Decimal{}, fmt.Errorf("clamping %v: invalid range", d)
	}
	if min.CmpTotal(max) > 0 {
		// min and max are equal numerically but have different scales.
		// Swaping min and max to ensure total ordering.
		min, max = max, min
	}
	if d.CmpTotal(min) < 0 {
		return min, nil
	}
	if d.CmpTotal(max) > 0 {
		return max, nil
	}
	return d, nil
}

// addFint computes the sum of two decimals using uint64 arithmetic.
func (d Decimal) addFint(e Decimal, minScale int) (Decimal, error) {
	dcoef := d.coef
	dscale := d.Scale()
	dneg := d.IsNeg()

	ecoef := e.coef

	// Alignment
	var ok bool
	switch {
	case dscale > e.Scale():
		ecoef, ok = ecoef.lsh(dscale - e.Scale())
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	case dscale < e.Scale():
		dcoef, ok = dcoef.lsh(e.Scale() - dscale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
		dscale = e.Scale()
	}

	// Compute d = d + e
	if dneg == e.IsNeg() {
		dcoef, ok = dcoef.add(ecoef)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	} else {
		if ecoef > dcoef {
			dneg = e.IsNeg()
		}
		dcoef = dcoef.subAbs(ecoef)
	}

	return newFromFint(dneg, dcoef, dscale, minScale)
}

// addBint computes the sum of two decimals using *big.Int arithmetic.
func (d Decimal) addBint(e Decimal, minScale int) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	dscale := d.Scale()
	ecoef.setFint(e.coef)
	dneg := d.IsNeg()

	// Alignment
	switch {
	case dscale > e.Scale():
		ecoef.lsh(ecoef, dscale-e.Scale())
	case dscale < e.Scale():
		dcoef.lsh(dcoef, e.Scale()-dscale)
		dscale = e.Scale()
	}

	// Compute d = d + e
	if dneg == e.IsNeg() {
		dcoef.add(dcoef, ecoef)
	} else {
		if ecoef.cmp(dcoef) > 0 {
			dneg = e.IsNeg()
		}
		dcoef.subAbs(dcoef, ecoef)
	}

	return newFromBint(dneg, dcoef, dscale, minScale)
}

// AddExact is similar to [Decimal.Add], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) AddExact(e Decimal, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("computing [%v + %v]: %w", d, e, errScaleRange)
	}

	// General case
	f, err := d.addFint(e, scale)
	if err != nil {
		f, err = d.addBint(e, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v + %v]: %w", d, e, err)
		}
	}

	return f, nil
}

// Add returns the (possibly rounded) sum of decimals d and e.
//
// Add returns an error if the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) Add(e Decimal) (Decimal, error) {
	// Fast path: equal scales and signs
	if d.scale == e.scale && d.neg == e.neg {
		if coef, ok := d.coef.add(e.coef); ok {
			return newUnsafe(d.neg, coef, d.Scale()), nil
		}
	}
	return d.AddExact(e, 0)
}

// SubExact is similar to [Decimal.Sub], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) SubExact(e Decimal, scale int) (Decimal, error) {
	return d.AddExact(e.Neg(), scale)
}

// Sub returns the (possibly rounded) difference between decimals d and e.
//
// Sub returns an error if the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) Sub(e Decimal) (Decimal, error) {
	return d.Add(e.Neg())
}

// SubAbs returns the (possibly rounded) absolute difference between decimals d and e.
//
// SubAbs returns an error if the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) SubAbs(e Decimal) (Decimal, error) {
	f, err := d.Sub(e)
	if err != nil {
		return Decimal{}, fmt.Errorf("computing [abs(%v - %v)]: %w", d, e, err)
	}
	return f.Abs(), nil
}

// sumFint computes the sum of decimals using uint64 arithmetic.
func sumFint(d ...Decimal) (Decimal, error) {
	ecoef := Zero.coef
	escale := Zero.Scale()
	eneg := Zero.IsNeg()

	for _, f := range d {
		fcoef := f.coef

		// Alignment
		var ok bool
		switch {
		case escale > f.Scale():
			fcoef, ok = fcoef.lsh(escale - f.Scale())
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
		case escale < f.Scale():
			ecoef, ok = ecoef.lsh(f.Scale() - escale)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
			escale = f.Scale()
		}

		// Compute e = e + f
		if eneg == f.IsNeg() {
			ecoef, ok = ecoef.add(fcoef)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
		} else {
			if fcoef > ecoef {
				eneg = f.IsNeg()
			}
			ecoef = ecoef.subAbs(fcoef)
		}
	}

	return newFromFint(eneg, ecoef, escale, 0)
}

// sumBint computes the sum of decimals using *big.Int arithmetic.
func sumBint(d ...Decimal) (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	ecoef.setFint(Zero.coef)
	escale := Zero.Scale()
	eneg := Zero.IsNeg()

	for _, f := range d {
		fcoef.setFint(f.coef)

		// Alignment
		switch {
		case escale > f.Scale():
			fcoef.lsh(fcoef, escale-f.Scale())
		case escale < f.Scale():
			ecoef.lsh(ecoef, f.Scale()-escale)
			escale = f.Scale()
		}

		// Compute e = e + f
		if eneg == f.IsNeg() {
			ecoef.add(ecoef, fcoef)
		} else {
			if fcoef.cmp(ecoef) > 0 {
				eneg = f.IsNeg()
			}
			ecoef.subAbs(ecoef, fcoef)
		}
	}

	return newFromBint(eneg, ecoef, escale, 0)
}

// Sum returns the (possibly rounded) sum of decimals.
// It computes d1 + d2 + ... + dn without intermediate rounding.
//
// Sum returns an error if:
//   - no argements are provided;
//   - the integer part of the result has more than [MaxPrec] digits.
func Sum(d ...Decimal) (Decimal, error) {
	// Special cases
	switch len(d) {
	case 0:
		return Decimal{}, fmt.Errorf("computing [sum([])]: %w", errInvalidOperation)
	case 1:
		return d[0], nil
	}

	// General case
	e, err := sumFint(d...)
	if err != nil {
		e, err = sumBint(d...)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [sum(%v)]: %w", d, err)
		}
	}

	return e, nil
}

// mulFint computes the product of two decimals using uint64 arithmetic.
func (d Decimal) mulFint(e Decimal, minScale int) (Decimal, error) {
	dcoef := d.coef
	dscale := d.Scale()
	dneg := d.IsNeg()

	ecoef := e.coef

	// Compute d = d * e
	dcoef, ok := dcoef.mul(ecoef)
	if !ok {
		return Decimal{}, errDecimalOverflow
	}
	dscale = dscale + e.Scale()
	dneg = dneg != e.IsNeg()

	return newFromFint(dneg, dcoef, dscale, minScale)
}

// mulBint computes the product of two decimals using *big.Int arithmetic.
func (d Decimal) mulBint(e Decimal, minScale int) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	dscale := d.Scale()
	dneg := d.IsNeg()
	ecoef.setFint(e.coef)

	// Compute d = d * e
	dcoef.mul(dcoef, ecoef)
	dneg = dneg != e.IsNeg()
	dscale = dscale + e.Scale()

	return newFromBint(dneg, dcoef, dscale, minScale)
}

// MulExact is similar to [Decimal.Mul], but it allows you to specify the number
// of digits after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will
// return an overflow error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) MulExact(e Decimal, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("computing [%v * %v]: %w", d, e, errScaleRange)
	}

	// General case
	f, err := d.mulFint(e, scale)
	if err != nil {
		f, err = d.mulBint(e, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v * %v]: %w", d, e, err)
		}
	}
	return f, nil
}

// Mul returns the (possibly rounded) product of decimals d and e.
//
// Mul returns an overflow error if the integer part of the result has
// more than [MaxPrec] digits.
func (d Decimal) Mul(e Decimal) (Decimal, error) {
	// Fast path: the product needs no rounding
	if scale := d.Scale() + e.Scale(); scale <= MaxScale {
		if coef, ok := d.coef.mul(e.coef); ok {
			return newUnsafe(d.neg != e.neg, coef, scale), nil
		}
	}
	return d.MulExact(e, 0)
}

// prodFint computes the product of decimals using uint64 arithmetic.
func prodFint(d ...Decimal) (Decimal, error) {
	ecoef := One.coef
	escale := One.Scale()
	eneg := One.IsNeg()

	for _, f := range d {
		fcoef := f.coef

		// Compute e = e * f
		var ok bool
		ecoef, ok = ecoef.mul(fcoef)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
		eneg = eneg != f.IsNeg()
		escale = escale + f.Scale()
	}

	return newFromFint(eneg, ecoef, escale, 0)
}

// prodBint computes the product of decimals using *big.Int arithmetic.
func prodBint(d ...Decimal) (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	ecoef.setFint(One.coef)
	escale := One.Scale()
	eneg := One.IsNeg()

	for _, f := range d {
		fcoef.setFint(f.coef)

		// Compute e = e * f
		ecoef.mul(ecoef, fcoef)
		eneg = eneg != f.IsNeg()
		escale = escale + f.Scale()

		// Intermediate truncation
		if escale > bscale {
			ecoef.rshDown(ecoef, escale-bscale)
			escale = bscale
		}

		// Check if e >= 10^59
		if ecoef.hasPrec(len(bpow10)) {
			return Decimal{}, unknownOverflowError()
		}
	}

	return newFromBint(eneg, ecoef, escale, 0)
}

// Prod returns the (possibly rounded) product of decimals.
// It computes d1 * d2 * ... * dn with at least double precision
// during the intermediate rounding.
//
// Prod returns an error if:
//   - no arguments are provided;
//   - the integer part of the result has more than [MaxPrec] digits.
func Prod(d ...Decimal) (Decimal, error) {
	// Special cases
	switch len(d) {
	case 0:
		return Decimal{}, fmt.Errorf("computing [prod([])]: %w", errInvalidOperation)
	case 1:
		return d[0], nil
	}

	// General case
	e, err := prodFint(d...)
	if err != nil {
		e, err = prodBint(d...)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [prod(%v)]: %w", d, err)
		}
	}

	return e, nil
}

// meanFint computes the mean of decimals using uint64 arithmetic.
func meanFint(d ...Decimal) (Decimal, error) {
	ecoef := Zero.coef
	escale := Zero.Scale()
	eneg := Zero.IsNeg()

	ncoef := fint(len(d))

	for _, f := range d {
		fcoef := f.coef

		// Alignment
		var ok bool
		switch {
		case escale > f.Scale():
			fcoef, ok = fcoef.lsh(escale - f.Scale())
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
		case escale < f.Scale():
			ecoef, ok = ecoef.lsh(f.Scale() - escale)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
			escale = f.Scale()
		}

		// Compute e = e + f
		if eneg == f.IsNeg() {
			ecoef, ok = ecoef.add(fcoef)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
		} else {
			if fcoef > ecoef {
				eneg = f.IsNeg()
			}
			ecoef = ecoef.subAbs(fcoef)
		}
	}

	// Alignment
	var ok bool
	if shift := MaxPrec - ecoef.prec(); shift > 0 {
		ecoef, ok = ecoef.lsh(shift)
		if !ok {
			return Decimal{}, errDecimalOverflow // Should never happen
		}
		escale = escale + shift
	}

	// Compute e = e / n
	ecoef, ok = ecoef.quo(ncoef)
	if !ok {
		return Decimal{}, errInexactDivision
	}

	return newFromFint(eneg, ecoef, escale, 0)
}

// meanBint computes the mean of decimals using *big.Int arithmetic.
func meanBint(d ...Decimal) (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	ncoef := getBint()
	defer putBint(ncoef)

	ecoef.setFint(Zero.coef)
	escale := Zero.Scale()
	eneg := Zero.IsNeg()
	ncoef.setInt64(int64(len(d)))

	for _, f := range d {
		fcoef.setFint(f.coef)

		// Alignment
		switch {
		case escale > f.Scale():
			fcoef.lsh(fcoef, escale-f.Scale())
		case escale < f.Scale():
			ecoef.lsh(ecoef, f.Scale()-escale)
			escale = f.Scale()
		}

		// Compute e = e + f
		if eneg == f.IsNeg() {
			ecoef.add(ecoef, fcoef)
		} else {
			if fcoef.cmp(ecoef) > 0 {
				eneg = f.IsNeg()
			}
			ecoef.subAbs(ecoef, fcoef)
		}
	}

	// Alignment
	ecoef.lsh(ecoef, bscale-escale)

	// Compute e = e / n
	ecoef.quo(ecoef, ncoef)

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Mean returns the (possibly rounded) mean of decimals.
// It computes (d1 + d2 + ... + dn) / n with at least double precision
// during the intermediate rounding.
//
// Mean returns an error if:
//   - no arguments are provided;
//   - the integer part of the result has more than [MaxPrec] digits.
func Mean(d ...Decimal) (Decimal, error) {
	// Special cases
	switch len(d) {
	case 0:
		return Decimal{}, fmt.Errorf("computing [mean([])]: %w", errInvalidOperation)
	case 1:
		return d[0], nil
	}

	// General case
	e, err := meanFint(d...)
	if err != nil {
		e, err = meanBint(d...)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [mean(%v)]: %w", d, err)
		}
	}

	// Preferred scale
	scale := 0
	for _, f := range d {
		scale = max(scale, f.Scale())
	}
	e = e.Trim(scale)

	return e, nil
}

// addMulFint computes the fused multiply-addition of three decimals using uint64 arithmetic.
func (d Decimal) addMulFint(e, f Decimal, minScale int) (Decimal, error) {
	dcoef := d.coef
	dscale := d.Scale()
	dneg := d.IsNeg()

	ecoef := e.coef
	escale := e.Scale()
	eneg := e.IsNeg()

	fcoef := f.coef

	// Compute e = e * f
	var ok bool
	ecoef, ok = ecoef.mul(fcoef)
	if !ok {
		return Decimal{}, errDecimalOverflow
	}
	escale = escale + f.Scale()
	eneg = eneg != f.IsNeg()

	// Alignment
	switch {
	case dscale > escale:
		ecoef, ok = ecoef.lsh(dscale - escale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	case dscale < escale:
		dcoef, ok = dcoef.lsh(escale - dscale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
		dscale = escale
	}

	// Compute d = d + e
	if dneg == eneg {
		dcoef, ok = dcoef.add(ecoef)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	} else {
		if ecoef > dcoef {
			dneg = eneg
		}
		dcoef = dcoef.subAbs(ecoef)
	}

	return newFromFint(dneg, dcoef, dscale, minScale)
}

// addMulBint computes the fused multiply-addition of three decimals using *big.Int arithmetic.
func (d Decimal) addMulBint(e, f Decimal, minScale int) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	dcoef.setFint(d.coef)
	dscale := d.Scale()
	dneg := d.IsNeg()
	ecoef.setFint(e.coef)
	escale := e.Scale()
	eneg := e.IsNeg()
	fcoef.setFint(f.coef)

	// Compute e = e * f
	ecoef.mul(ecoef, fcoef)
	escale = escale + f.Scale()
	eneg = eneg != f.IsNeg()

	// Alignment
	switch {
	case dscale > escale:
		ecoef.lsh(ecoef, dscale-escale)
	case dscale < escale:
		dcoef.lsh(dcoef, escale-d.Scale())
		dscale = escale
	}

	// Compute d = d + e
	if dneg == eneg {
		dcoef.add(dcoef, ecoef)
	} else {
		if ecoef.cmp(dcoef) > 0 {
			dneg = eneg
		}
		dcoef.subAbs(dcoef, ecoef)
	}

	return newFromBint(dneg, dcoef, dscale, minScale)
}

// AddMulExact is similar to [Decimal.AddMul], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) AddMulExact(e, f Decimal, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("computing [%v + %v * %v]: %w", d, e, f, errScaleRange)
	}

	// General case
	g, err := d.addMulFint(e, f, scale)
	if err != nil {
		g, err = d.addMulBint(e, f, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v + %v * %v]: %w", d, e, f, err)
		}
	}

	return g, nil
}

// AddMul returns the (possibly rounded) [fused multiply-addition] of decimals d, e, and f.
// It computes d + e * f without any intermediate rounding.
// This method is useful for improving the accuracy and performance of algorithms
// that involve the accumulation of products, such as daily interest accrual.
//
// AddMul returns an error if the integer part of the result has more than [MaxPrec] digits.
//
// [fused multiply-addition]: https://en.wikipedia.org/wiki/Multiply%E2%80%93accumulate_operation#Fused_multiply%E2%80%93add
func (d Decimal) AddMul(e, f Decimal) (Decimal, error) {
	return d.AddMulExact(e, f, 0)
}

// SubMulExact is similar to [Decimal.SubMul], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) SubMulExact(e, f Decimal, scale int) (Decimal, error) {
	return d.AddMulExact(e.Neg(), f, scale)
}

// SubMul returns the (possibly rounded) [fused multiply-subtraction] of decimals d, e, and f.
// It computes d - e * f without any intermediate rounding.
// This method is useful for improving the accuracy and performance of algorithms
// that involve the accumulation of products, such as daily interest accrual.
//
// SubMul returns an error if the integer part of the result has more than [MaxPrec] digits.
//
// [fused multiply-subtraction]: https://en.wikipedia.org/wiki/Multiply%E2%80%93accumulate_operation#Fused_multiply%E2%80%93add
func (d Decimal) SubMul(e, f Decimal) (Decimal, error) {
	return d.AddMulExact(e.Neg(), f, 0)
}

// quoFint computes the quotient of two decimals using 128-bit arithmetic.
// The quotient is computed as ⌊d * 10^k / e⌋, where k is chosen so that
// the quotient has 19 digits but its scale does not exceed [MaxScale],
// and then rounded half to even using the remainder.
func (d Decimal) quoFint(e Decimal, minScale int) (Decimal, error) {
	dcoef := uint64(d.coef)
	ecoef := uint64(e.coef)
	if ecoef == 0 {
		return Decimal{}, errDivisionByZero
	}

	// Alignment
	k := min(MaxPrec-d.coef.prec()+e.coef.prec(), MaxScale-d.Scale()+e.Scale())
	hi, lo := mulPow10(dcoef, k)
	// The quotient is less than 10^20, it must be less than 10^19.
	if ehi, elo := bits.Mul64(ecoef, uint64(pow10[MaxPrec])); hi > ehi || hi == ehi && lo >= elo {
		k--
		hi, lo = mulPow10(dcoef, k)
	}

	// Compute q = ⌊d / e⌋, r = d - q * e
	q, r := bits.Div64(hi, lo, ecoef)
	scale := d.Scale() - e.Scale() + k
	neg := d.IsNeg() != e.IsNeg()

	// Exact quotient
	if r == 0 {
		return newFromFint(neg, fint(q), scale, minScale)
	}

	// Inexact quotient must not lose any of the digits required by minScale
	if scale < minScale {
		return Decimal{}, errInexactDivision
	}

	// Rounding half to even.
	// An inexact quotient never rounds up to 10^19, but even if it did,
	// newFromFint would return an error and the caller would fall back to quoBint.
	if r > ecoef-r || r == ecoef-r && q&1 == 1 {
		q++
	}

	return newFromFint(neg, fint(q), scale, minScale)
}

// quoBint computes the quotient of two decimals using *big.Int arithmetic.
func (d Decimal) quoBint(e Decimal, minScale int) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	dneg := d.IsNeg()
	ecoef.setFint(e.coef)

	// Alignment
	dcoef.lsh(dcoef, bscale+e.Scale()-d.Scale())

	// Compute d = ⌊d / e⌋
	dcoef.quo(dcoef, ecoef)
	dneg = dneg != e.IsNeg()

	return newFromBint(dneg, dcoef, bscale, minScale)
}

// QuoExact is similar to [Decimal.Quo], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) QuoExact(e Decimal, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("computing [%v / %v]: %w", d, e, errScaleRange)
	}

	// Special case: zero divisor
	if e.IsZero() {
		return Decimal{}, fmt.Errorf("computing [%v / %v]: %w", d, e, errDivisionByZero)
	}

	// Special case: zero dividend
	if d.IsZero() {
		scale = max(scale, d.Scale()-e.Scale())
		return newSafe(false, 0, scale)
	}

	// General case
	f, err := d.quoFint(e, scale)
	if err != nil {
		f, err = d.quoBint(e, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v / %v]: %w", d, e, err)
		}
	}

	// Preferred scale
	scale = max(scale, d.Scale()-e.Scale())
	f = f.Trim(scale)

	return f, nil
}

// Quo returns the (possibly rounded) quotient of decimals d and e.
//
// Quo returns an error if:
//   - the divisor is 0;
//   - the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) Quo(e Decimal) (Decimal, error) {
	return d.QuoExact(e, 0)
}

// Inv returns the (possibly rounded) inverse of the decimal.
//
// Inv returns an error if:
//   - the integer part of the result has more than [MaxPrec] digits;
//   - the decimal is 0.
func (d Decimal) Inv() (Decimal, error) {
	f, err := One.Quo(d)
	if err != nil {
		return Decimal{}, fmt.Errorf("inverting %v: %w", d, err)
	}
	return f, nil
}

// quoRemFint computes the quotient and remainder of two decimals using uint64 arithmetic.
func (d Decimal) quoRemFint(e Decimal) (q, r Decimal, err error) {
	dcoef := d.coef
	ecoef := e.coef
	rscale := d.Scale()

	// Alignment
	var ok bool
	switch {
	case d.Scale() > e.Scale():
		ecoef, ok = ecoef.lsh(d.Scale() - e.Scale())
		if !ok {
			return Decimal{}, Decimal{}, errDecimalOverflow
		}
	case d.Scale() < e.Scale():
		dcoef, ok = dcoef.lsh(e.Scale() - d.Scale())
		if !ok {
			return Decimal{}, Decimal{}, errDecimalOverflow
		}
		rscale = e.Scale()
	}

	// Compute q = ⌊d / e⌋, r = d - e * q
	qcoef, rcoef, ok := dcoef.quoRem(ecoef)
	if !ok {
		return Decimal{}, Decimal{}, errDivisionByZero // Should never happen
	}
	qsign := d.IsNeg() != e.IsNeg()
	rsign := d.IsNeg()

	q, err = newFromFint(qsign, qcoef, 0, 0)
	if err != nil {
		return Decimal{}, Decimal{}, err
	}
	r, err = newFromFint(rsign, rcoef, rscale, rscale)
	if err != nil {
		return Decimal{}, Decimal{}, err
	}
	return q, r, nil
}

// quoRemBint computes the quotient and remainder of two decimals using *big.Int arithmetic.
func (d Decimal) quoRemBint(e Decimal) (q, r Decimal, err error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	qcoef := getBint()
	defer putBint(qcoef)

	rcoef := getBint()
	defer putBint(rcoef)

	dcoef.setFint(d.coef)
	ecoef.setFint(e.coef)
	rscale := d.Scale()

	// Alignment
	switch {
	case d.Scale() > e.Scale():
		ecoef.lsh(ecoef, d.Scale()-e.Scale())
	case d.Scale() < e.Scale():
		dcoef.lsh(dcoef, e.Scale()-d.Scale())
		rscale = e.Scale()
	}

	// Compute q = ⌊d / e⌋, r = d - e * q
	qcoef.quoRem(dcoef, ecoef, rcoef)
	qsign := d.IsNeg() != e.IsNeg()
	rsign := d.IsNeg()

	q, err = newFromBint(qsign, qcoef, 0, 0)
	if err != nil {
		return Decimal{}, Decimal{}, err
	}
	r, err = newFromBint(rsign, rcoef, rscale, rscale)
	if err != nil {
		return Decimal{}, Decimal{}, err
	}
	return q, r, nil
}

// QuoRem returns the quotient q and remainder r of decimals d and e
// such that d = e * q + r, where q is an integer and the sign of the
// reminder r is the same as the sign of the dividend d.
//
// QuoRem returns an error if:
//   - the divisor is 0;
//   - the integer part of the quotient has more than [MaxPrec] digits.
func (d Decimal) QuoRem(e Decimal) (q, r Decimal, err error) {
	// Special case: zero divisor
	if e.IsZero() {
		return Decimal{}, Decimal{}, fmt.Errorf("computing [%v div %v] and [%v mod %v]: %w", d, e, d, e, errDivisionByZero)
	}

	// General case
	q, r, err = d.quoRemFint(e)
	if err != nil {
		q, r, err = d.quoRemBint(e)
		if err != nil {
			return Decimal{}, Decimal{}, fmt.Errorf("computing [%v div %v] and [%v mod %v]: %w", d, e, d, e, err)
		}
	}

	return q, r, nil
}

// addQuoFint computes the fused quotient-addition of three decimals using uint64 arithmetic.
func (d Decimal) addQuoFint(e, f Decimal, minScale int) (Decimal, error) {
	dcoef := d.coef
	dscale := d.Scale()
	dneg := d.IsNeg()

	ecoef := e.coef
	escale := e.Scale()
	eneg := e.IsNeg()

	fcoef := f.coef

	// Alignment
	var ok bool
	if shift := MaxPrec - ecoef.prec(); shift > 0 {
		ecoef, ok = ecoef.lsh(shift)
		if !ok {
			return Decimal{}, errDecimalOverflow // Should never happen
		}
		escale = escale + shift
	}
	if shift := fcoef.ntz(); shift > 0 {
		fcoef = fcoef.rshDown(shift)
		escale = escale + shift
	}

	// Compute e = e / f
	ecoef, ok = ecoef.quo(fcoef)
	if !ok {
		return Decimal{}, errInexactDivision
	}
	escale = escale - f.Scale()
	eneg = eneg != f.IsNeg()

	// Alignment
	switch {
	case dscale > escale:
		ecoef, ok = ecoef.lsh(dscale - escale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	case dscale < escale:
		if shift := min(escale-e.Scale()+f.Scale(), escale-dscale, ecoef.ntz()); shift > 0 {
			ecoef = ecoef.rshDown(shift)
			escale = escale - shift
		}
		dcoef, ok = dcoef.lsh(escale - dscale)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
		dscale = escale
	}

	// Compute d = d + e
	if dneg == eneg {
		dcoef, ok = dcoef.add(ecoef)
		if !ok {
			return Decimal{}, errDecimalOverflow
		}
	} else {
		if ecoef > dcoef {
			dneg = eneg
		}
		dcoef = dcoef.subAbs(ecoef)
	}

	return newFromFint(dneg, dcoef, dscale, minScale)
}

// addQuoBint computes the fused quotient-addition of three decimals using *big.Int arithmetic.
func (d Decimal) addQuoBint(e, f Decimal, minScale int) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	dcoef.setFint(d.coef)
	dneg := d.IsNeg()
	ecoef.setFint(e.coef)
	eneg := e.IsNeg()
	fcoef.setFint(f.coef)

	// Alignment
	ecoef.lsh(ecoef, bscale-e.Scale()+f.Scale())

	// Compute e = ⌊e / f⌋
	ecoef.quo(ecoef, fcoef)
	eneg = eneg != f.IsNeg()

	// Alignment
	dcoef.lsh(dcoef, bscale-d.Scale())

	// Compute d = d + e
	if dneg == eneg {
		dcoef.add(dcoef, ecoef)
	} else {
		if ecoef.cmp(dcoef) > 0 {
			dneg = eneg
		}
		dcoef.subAbs(dcoef, ecoef)
	}

	return newFromBint(dneg, dcoef, bscale, minScale)
}

// AddQuoExact is similar to [Decimal.AddQuo], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) AddQuoExact(e, f Decimal, scale int) (Decimal, error) {
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("computing [%v + %v / %v]: %w", d, e, f, errScaleRange)
	}

	// Special case: zero divisor
	if f.IsZero() {
		return Decimal{}, fmt.Errorf("computing [%v + %v / %v]: %w", d, e, f, errDivisionByZero)
	}

	// Special case: zero dividend
	if e.IsZero() {
		scale = max(scale, e.Scale()-f.Scale())
		return d.Pad(scale), nil
	}

	// General case
	g, err := d.addQuoFint(e, f, scale)
	if err != nil {
		g, err = d.addQuoBint(e, f, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v + %v / %v]: %w", d, e, f, err)
		}
	}

	// Preferred scale
	scale = max(scale, d.Scale(), e.Scale()-f.Scale())
	g = g.Trim(scale)

	return g, nil
}

// AddQuo returns the (possibly rounded) fused quotient-addition of decimals d, e, and f.
// It computes d + e / f with at least double precision during the intermediate rounding.
// This method is useful for improving the accuracy and performance of algorithms
// that involve the accumulation of quotients, such as internal rate of return.
//
// AddQuo returns an error if:
//   - the divisor is 0;
//   - the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) AddQuo(e, f Decimal) (Decimal, error) {
	return d.AddQuoExact(e, f, 0)
}

// SubQuoExact is similar to [Decimal.SubQuo], but it allows you to specify the number of digits
// after the decimal point that should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for financial calculations where the scale should be
// equal to or greater than the currency's scale.
func (d Decimal) SubQuoExact(e, f Decimal, scale int) (Decimal, error) {
	return d.AddQuoExact(e.Neg(), f, scale)
}

// SubQuo returns the (possibly rounded) fused quotient-subtraction of decimals d, e, and f.
// It computes d - e / f with at least double precision during intermediate rounding.
// This method is useful for improving the accuracy and performance of algorithms
// that involve the accumulation of quotients, such as internal rate of return.
//
// AddQuo returns an error if:
//   - the divisor is 0;
//   - the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) SubQuo(e, f Decimal) (Decimal, error) {
	return d.AddQuoExact(e.Neg(), f, 0)
}

// powIntFint computes the integer power of a decimal using uint64 arithmetic.
// powIntFint does not support negative powers.
func (d Decimal) powIntFint(pow uint64, inv bool) (Decimal, error) {
	if inv {
		return Decimal{}, errInvalidOperation
	}

	dcoef := d.coef
	dneg := d.IsNeg()
	dscale := d.Scale()

	ecoef := One.coef
	eneg := One.IsNeg()
	escale := One.Scale()

	// Exponentiation by squaring
	var ok bool
	for pow > 0 {
		if pow%2 == 1 {
			pow = pow - 1

			// Compute e = e * d
			ecoef, ok = ecoef.mul(dcoef)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
			eneg = eneg != dneg
			escale = escale + dscale
		}
		if pow > 0 {
			pow = pow / 2

			// Compute d = d * d
			dcoef, ok = dcoef.mul(dcoef)
			if !ok {
				return Decimal{}, errDecimalOverflow
			}
			dneg = false
			dscale = dscale * 2
		}
	}

	return newFromFint(eneg, ecoef, escale, 0)
}

// powIntBint computes the integer power of a decimal using *big.Int arithmetic.
// powIntBint supports negative powers.
func (d Decimal) powIntBint(pow uint64, inv bool) (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	dneg := d.IsNeg()
	dscale := d.Scale()

	ecoef.setFint(One.coef)
	eneg := One.IsNeg()
	escale := One.Scale()

	// Exponentiation by squaring
	for pow > 0 {
		if pow%2 == 1 {
			pow = pow - 1

			// Compute e = e * d
			ecoef.mul(ecoef, dcoef)
			eneg = eneg != dneg
			escale = escale + dscale

			// Intermediate truncation
			if escale > bscale {
				ecoef.rshDown(ecoef, escale-bscale)
				escale = bscale
			}

			// Check if e <= -10^59 or e >= 10^59
			if ecoef.hasPrec(len(bpow10)) {
				if !inv {
					return Decimal{}, unknownOverflowError()
				}
				return newSafe(false, 0, MaxScale)
			}
		}
		if pow > 0 {
			pow = pow / 2

			// Compute d = d * d
			dcoef.mul(dcoef, dcoef)
			dneg = false
			dscale = dscale * 2

			// Intermediate truncation
			if dscale > bscale {
				dcoef.rshDown(dcoef, dscale-bscale)
				dscale = bscale
			}

			// Check if d <= -10^59 or d >= 10^59
			if dcoef.hasPrec(len(bpow10)) {
				if !inv {
					return Decimal{}, unknownOverflowError()
				}
				return newSafe(false, 0, MaxScale)
			}
		}
	}

	if inv {
		if ecoef.sign() == 0 {
			return Decimal{}, unknownOverflowError()
		}

		// Compute e = ⌊1 / e⌋
		ecoef.quo(bpow10[bscale+escale], ecoef)
		escale = bscale
	}

	return newFromBint(eneg, ecoef, escale, 0)
}

// PowInt returns the (possibly rounded) decimal raised to the given integer power.
// If zero is raised to zero power then the result is one.
//
// PowInt returns an error if:
//   - the integer part of the result has more than [MaxPrec] digits;
//   - zero is raised to a negative power.
func (d Decimal) PowInt(power int) (Decimal, error) {
	var pow uint64
	var neg bool
	if power >= 0 {
		neg = false
		pow = uint64(power)
	} else {
		neg = true
		if power == math.MinInt {
			pow = uint64(math.MaxInt) + 1
		} else {
			pow = uint64(-power)
		}
	}

	// Special case: zero to a negative power
	if neg && d.IsZero() {
		return Decimal{}, fmt.Errorf("computing [%v^%v]: %w: zero to negative power", d, power, errInvalidOperation)
	}

	// General case
	e, err := d.powIntFint(pow, neg)
	if err != nil {
		e, err = d.powIntBint(pow, neg)
		if err != nil {
			return Decimal{}, fmt.Errorf("computing [%v^%v]: %w", d, power, err)
		}
	}

	// Preferred scale
	if neg {
		e = e.Trim(0)
	}

	return e, nil
}

// sqrtFint computes the square root of a decimal using 128-bit arithmetic.
// The square root is computed as ⌊√(d * 10^m)⌋, where m is chosen so that
// the root has 19 digits but its scale does not exceed [MaxScale],
// and then rounded half to even using the remainder.
func (d Decimal) sqrtFint() (Decimal, error) {
	dcoef := uint64(d.coef)
	dscale := d.Scale()

	// Alignment
	m := min(2*MaxPrec-d.coef.prec(), 2*MaxScale-dscale)
	if (m+dscale)%2 != 0 {
		m--
	}
	hi, lo := mulPow10(dcoef, m)

	// Compute q = ⌊√d⌋, r = d - q²
	q, rhi, rlo := isqrt128(hi, lo)

	// Rounding half to even.
	// The root of an integer is never exactly halfway between two integers,
	// so it is rounded up if and only if r > q.
	if rhi != 0 || rlo > q {
		q++
	}

	return newFromFint(false, fint(q), (dscale+m)/2, 0)
}

// sqrtBint computes the square root of a decimal using *big.Int arithmetic.
func (d Decimal) sqrtBint() (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	dcoef.setFint(d.coef)
	fcoef.setFint(0)

	// Alignment
	dcoef.lsh(dcoef, 2*bscale-d.Scale())

	// Initial guess is calculated as 10^(n/2),
	// where n is the position of the most significant digit.
	n := dcoef.prec() - 2*bscale
	ecoef.setBint(bpow10[n/2+bscale])

	// Newton's method
	for range 50 {
		if ecoef.cmp(fcoef) == 0 {
			break
		}
		fcoef.setBint(ecoef)
		ecoef.quo(dcoef, ecoef)
		ecoef.add(ecoef, fcoef)
		ecoef.hlf(ecoef)
	}

	return newFromBint(false, ecoef, bscale, 0)
}

// Sqrt computes the (possibly rounded) square root of a decimal.
// d.Sqrt() is significantly faster than d.Pow(0.5).
//
// Sqrt returns an error if the decimal is negative.
func (d Decimal) Sqrt() (Decimal, error) {
	// Special case: negative
	if d.IsNeg() {
		return Decimal{}, fmt.Errorf("computing sqrt(%v): %w: square root of negative", d, errInvalidOperation)
	}

	// Special case: zero
	if d.IsZero() {
		return newSafe(false, 0, d.Scale()/2)
	}

	// General case
	e, err := d.sqrtFint()
	if err != nil {
		e, err = d.sqrtBint()
		if err != nil {
			return Decimal{}, fmt.Errorf("computing sqrt(%v): %w", d, err)
		}
	}

	// Preferred scale
	e = e.Trim(d.Scale() / 2)

	return e, nil
}

// log calculates z = log(x).
// The argument x must satisfy x >= 1, otherwise the result is undefined.
// x must be represented as a big integer: round(x * 10^41).
// The result z is represented as a big integer: round(z * 10^41).
//
// The argument is reduced as x = 10^n * 2^b * (1 + j/16) * (1 + k/1024) * f,
// where 1 <= f < 1 + 1/1024, so that
//
//	log(x) = n * log(10) + b * log(2) + log(1 + j/16) + log(1 + k/1024) + log(f),
//
// and log(f) = 2 * atanh(u), where u = (f - 1) / (f + 1) < 2^-11,
// is computed using Taylor series expansion.
// All intermediate values are binary fixed-point numbers with [lnFracBits]
// fractional bits, so that multiplications require only shifts.
func (z *bint) log(x *bint) {
	fcoef := getBint()
	defer putBint(fcoef)

	ucoef := getBint()
	defer putBint(ucoef)

	vcoef := getBint()
	defer putBint(vcoef)

	pcoef := getBint()
	defer putBint(pcoef)

	scoef := getBint()
	defer putBint(scoef)

	tcoef := getBint()
	defer putBint(tcoef)

	rcoef := getBint()
	defer putBint(rcoef)

	// The destination of Mul and QuoRem must not alias their operands,
	// otherwise math/big allocates a new buffer for the result.
	// t is a temporary buffer that is swapped with the destination,
	// r is a buffer for unused remainders.
	f := (*big.Int)(fcoef)
	u := (*big.Int)(ucoef)
	v := (*big.Int)(vcoef)
	p := (*big.Int)(pcoef)
	s := (*big.Int)(scoef)
	t := (*big.Int)(tcoef)
	r := (*big.Int)(rcoef)

	// Compute f = x / 10^n, where 1 <= f < 10
	n := x.prec() - bscale - 1
	t.Lsh((*big.Int)(x), lnFracBits)
	if n >= 0 {
		f.QuoRem(t, (*big.Int)(bpow10[n+bscale]), r)
	} else {
		f.QuoRem(t, (*big.Int)(bpow10[bscale]), r)
		f, t = t.Mul(f, (*big.Int)(bpow10[-n])), f
	}

	// Compute f = f / 2^b, where 1 <= f < 2
	b := f.BitLen() - lnFracBits - 1
	if b >= 0 {
		f.Rsh(f, uint(b))
	} else {
		f.Lsh(f, uint(-b))
	}

	// Compute f = f / (1 + j/16), where 1 <= f < 1 + 1/16
	j := v.Rsh(f, lnFracBits-4).Uint64() - 16
	f.Lsh(f, 4)
	t.QuoRem(f, v.SetUint64(16+j), r)
	f, t = t, f

	// Compute f = f / (1 + k/1024), where 1 <= f < 1 + 1/1024
	k := v.Rsh(f, lnFracBits-10).Uint64() - 1024
	f.Lsh(f, 10)
	t.QuoRem(f, v.SetUint64(1024+k), r)
	f, t = t, f

	// Compute u = (f - 1) / (f + 1)
	v.SetBit(v.SetUint64(0), lnFracBits, 1)
	t.Sub(f, v)
	t.Lsh(t, lnFracBits)
	f.Add(f, v)
	u.QuoRem(t, f, r)

	// Compute s = 2 * atanh(u) = 2 * (u + u^3/3 + u^5/5 + ...)
	s.Set(u)
	p.Set(u)
	t.Mul(u, u)
	u.Rsh(t, lnFracBits)
	for i := uint64(3); ; i += 2 {
		t.Mul(p, u)
		p.Rsh(t, lnFracBits)
		if p.Sign() == 0 {
			break
		}
		f.QuoRem(p, v.SetUint64(i), r)
		s.Add(s, f)
	}
	s.Lsh(s, 1)

	// Compute s = s + n * log(10) + b * log(2) + log(1 + j/16) + log(1 + k/1024)
	s.Add(s, t.Mul(v.SetInt64(int64(n)), (*big.Int)(blnTen)))
	s.Add(s, t.Mul(v.SetInt64(int64(b)), (*big.Int)(blnTwo)))
	s.Add(s, (*big.Int)(bln16[j]))
	s.Add(s, (*big.Int)(bln1024[k]))

	// Compute z = round(s * 10^41 / 2^lnFracBits)
	t.Mul(s, (*big.Int)(bpow10[bscale]))
	t.Add(t, v.SetBit(v.SetUint64(0), lnFracBits-1, 1))
	(*big.Int)(z).Rsh(t, lnFracBits)
}

// logCoef calculates z = |log(d)| and reports whether log(d) is negative.
// The decimal d must be positive, otherwise the result is undefined.
// The result z is represented as a big integer: round(z * 10^41).
func (d Decimal) logCoef(z *bint) (neg bool) {
	dcoef := getBint()
	defer putBint(dcoef)

	dcoef.setFint(d.coef)

	// Alignment
	if d.WithinOne() {
		// Compute d = ⌊1 / d⌋
		dcoef.quo(bpow10[bscale+d.Scale()], dcoef)
		neg = true
	} else {
		dcoef.lsh(dcoef, bscale-d.Scale())
	}

	// Compute z = log(d)
	z.log(dcoef)

	return neg
}

// logBint computes the natural logarithm of a decimal using *big.Int arithmetic.
func (d Decimal) logBint() (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	// Compute e = |log(d)|
	eneg := d.logCoef(ecoef)

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Log returns the (possibly rounded) natural logarithm of a decimal.
//
// Log returns an error if the decimal is zero or negative.
func (d Decimal) Log() (Decimal, error) {
	// Special case: zero or negative
	if !d.IsPos() {
		return Decimal{}, fmt.Errorf("computing log(%v): %w: logarithm of non-positive", d, errInvalidOperation)
	}

	// Special case: one
	if d.IsOne() {
		return newSafe(false, 0, 0)
	}

	// General case
	e, err := d.logBint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing log(%v): %w", d, err)
	}

	return e, nil
}

// log2Bint computes the binary logarithm of a decimal using *big.Int arithmetic.
func (d Decimal) log2Bint() (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	// Compute e = |log(d)|
	eneg := d.logCoef(ecoef)

	// Compute e = ⌊e / log(2)⌋
	ecoef.lsh(ecoef, bscale)
	ecoef.quo(ecoef, blogTwo)

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Log2 returns the (possibly rounded) binary logarithm of a decimal.
//
// Log2 returns an error if the decimal is zero or negative.
func (d Decimal) Log2() (Decimal, error) {
	// Special case: zero or negative
	if !d.IsPos() {
		return Decimal{}, fmt.Errorf("computing log2(%v): %w: logarithm of non-positive", d, errInvalidOperation)
	}

	// Special case: one
	if d.IsOne() {
		return newSafe(false, 0, 0)
	}

	// General case
	e, err := d.log2Bint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing log2(%v): %w", d, err)
	}

	// Preferred scale
	if e.IsInt() {
		// According to the GDA, only integer powers of 2 should be trimmed to zero scale.
		// However, such validation is slow, so we will trim all integers.
		e = e.Trunc(0)
	}

	return e, nil
}

// log10Bint computes the decimal logarithm of a decimal using *big.Int arithmetic.
func (d Decimal) log10Bint() (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	// Compute e = |log(d)|
	eneg := d.logCoef(ecoef)

	// Compute e = ⌊e / log(10)⌋
	ecoef.lsh(ecoef, bscale)
	ecoef.quo(ecoef, blogTen)

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Log10 returns the (possibly rounded) decimal logarithm of a decimal.
//
// Log10 returns an error if the decimal is zero or negative.
func (d Decimal) Log10() (Decimal, error) {
	// Special case: zero or negative
	if !d.IsPos() {
		return Decimal{}, fmt.Errorf("computing log10(%v): %w: logarithm of non-positive", d, errInvalidOperation)
	}

	// Special case: one
	if d.IsOne() {
		return newSafe(false, 0, 0)
	}

	// General case
	e, err := d.log10Bint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing log10(%v): %w", d, err)
	}

	// Preferred scale
	if e.IsInt() {
		// According to the GDA, only integer powers of 10 should be trimmed to zero scale.
		// However, such validation is slow, so we will trim all integers.
		e = e.Trunc(0)
	}

	return e, nil
}

// log1pBint computes the shifted natural logarithm of a decimal using *big.Int arithmetic.
func (d Decimal) log1pBint() (Decimal, error) {
	dcoef := getBint()
	defer putBint(dcoef)

	ecoef := getBint()
	defer putBint(ecoef)

	dcoef.setFint(d.coef)
	eneg := false

	// Alignment
	if d.IsNeg() {
		// Compute d = ⌊1 / (d + 1)⌋
		dcoef.subAbs(dcoef, bpow10[d.Scale()])
		dcoef.quo(bpow10[bscale+d.Scale()], dcoef)
		eneg = true
	} else {
		// Compute d = d + 1
		dcoef.add(dcoef, bpow10[d.Scale()])
		dcoef.lsh(dcoef, bscale-d.Scale())
	}

	// Compute e = log(d)
	ecoef.log(dcoef)

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Log1p returns the (possibly rounded) shifted natural logarithm of a decimal.
//
// Log1p returns an error if the decimal is equal to or less than negative one.
func (d Decimal) Log1p() (Decimal, error) {
	if d.IsNeg() && d.Cmp(NegOne) <= 0 {
		return Decimal{}, fmt.Errorf("computing log1p(%v): %w: logarithm of a decimal less than or equal to -1", d, errInvalidOperation)
	}

	// Special case: zero
	if d.IsZero() {
		return newSafe(false, 0, 0)
	}

	// General case
	e, err := d.log1pBint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing log1p(%v): %w", d, err)
	}

	return e, nil
}

// exp calculates z = exp(x).
// The argument x must satisfy -100 < x < 100, otherwise the result is undefined.
// x must be represented as a big integer: round(x * 10^41).
// The result z is represented as a big integer: round(z * 10^41).
//
// The argument is reduced as x = n * log(2) + j/16 + k/1024 + r,
// where 0 <= r < 1/1024, so that
//
//	exp(x) = 2^n * exp(j/16) * exp(k/1024) * exp(r),
//
// and exp(r) is computed using Taylor series expansion.
// All intermediate values are binary fixed-point numbers with [lnFracBits]
// fractional bits, so that multiplications require only shifts.
func (z *bint) exp(x *bint) {
	rcoef := getBint()
	defer putBint(rcoef)

	pcoef := getBint()
	defer putBint(pcoef)

	scoef := getBint()
	defer putBint(scoef)

	tcoef := getBint()
	defer putBint(tcoef)

	vcoef := getBint()
	defer putBint(vcoef)

	qcoef := getBint()
	defer putBint(qcoef)

	// The destination of Mul and QuoRem must not alias their operands,
	// otherwise math/big allocates a new buffer for the result.
	// t is a temporary buffer, q is a buffer for unused remainders.
	r := (*big.Int)(rcoef)
	p := (*big.Int)(pcoef)
	s := (*big.Int)(scoef)
	t := (*big.Int)(tcoef)
	v := (*big.Int)(vcoef)
	q := (*big.Int)(qcoef)

	// Compute r = x / 10^41 using the reciprocal of 10^41
	t.Mul((*big.Int)(x), (*big.Int)(binvTen))
	r.Rsh(t, lnFracBits)

	// Compute r = r - n * log(2), where 0 <= r < log(2)
	n := int(math.Floor(float64(v.Rsh(r, lnFracBits-32).Int64()) / (1 << 32) / math.Ln2))
	r.Sub(r, t.Mul(v.SetInt64(int64(n)), (*big.Int)(blnTwo)))
	for r.Sign() < 0 {
		n--
		r.Add(r, (*big.Int)(blnTwo))
	}
	for r.Cmp((*big.Int)(blnTwo)) >= 0 {
		n++
		r.Sub(r, (*big.Int)(blnTwo))
	}

	// Compute r = r - j/16, where 0 <= r < 1/16
	j := v.Rsh(r, lnFracBits-4).Uint64()
	r.Sub(r, v.Lsh(v, lnFracBits-4))

	// Compute r = r - k/1024, where 0 <= r < 1/1024
	k := v.Rsh(r, lnFracBits-10).Uint64()
	r.Sub(r, v.Lsh(v, lnFracBits-10))

	// Find the number of terms m, such that r^(m+1) / (m+1)! < 2^-lnFracBits,
	// using r < 2^-e and log2(i!) >= log2(2) + log2(3) + ... + log2(i).
	e := lnFracBits - r.BitLen()
	m, b := 0, e
	for b < lnFracBits {
		m++
		b += e + bits.Len(uint(m+1)) - 1
	}

	// Compute s = m! * exp(r) = m!/0! + m!/1! * r + m!/2! * r^2 + ... + m!/m! * r^m
	// using Horner's method, where all coefficients m!/i! are integers.
	s.SetBit(s.SetUint64(0), lnFracBits, 1)
	c := uint64(1)
	for i := m; i > 0; i-- {
		c *= uint64(i)
		t.Mul(s, r)
		s.Rsh(t, lnFracBits)
		s.Add(s, v.Lsh(v.SetUint64(c), lnFracBits))
	}

	// Compute s = s / m!
	t.QuoRem(s, v.SetUint64(c), q)
	s, t = t, s

	// Compute s = s * exp(j/16) * exp(k/1024)
	t.Mul((*big.Int)(bexp16[j]), (*big.Int)(bexp1024[k]))
	p.Rsh(t, lnFracBits)
	t.Mul(s, p)

	// Compute z = round(s * 2^n * 10^41)
	s.Mul(t, (*big.Int)(bpow10[bscale]))
	shift := uint(2*lnFracBits - n) //nolint:gosec
	s.Add(s, v.SetBit(v.SetUint64(0), int(shift-1), 1))
	(*big.Int)(z).Rsh(s, shift)
}

// expCoef calculates z = exp(d).
// The decimal d must satisfy -100 < d < 100, otherwise the result is undefined.
// The result z is represented as a big integer: round(z * 10^41).
func (d Decimal) expCoef(z *bint) {
	dcoef := getBint()
	defer putBint(dcoef)

	dcoef.setFint(d.coef)

	// Alignment
	dcoef.lsh(dcoef, bscale-d.Scale())
	if d.IsNeg() {
		dcoef.neg(dcoef)
	}

	// Compute z = exp(d)
	z.exp(dcoef)
}

// expBint computes exponential of a decimal using *big.Int arithmetic.
func (d Decimal) expBint() (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	// Compute e = exp(d)
	d.expCoef(ecoef)

	return newFromBint(false, ecoef, bscale, 0)
}

// Exp returns the (possibly rounded) exponential of a decimal.
//
// Exp returns an error if the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) Exp() (Decimal, error) {
	// Special case: zero
	if d.IsZero() {
		return newSafe(false, 1, 0)
	}

	// Special case: overflow
	if d.CmpAbs(Hundred) >= 0 {
		if !d.IsNeg() {
			return Decimal{}, fmt.Errorf("computing exp(%v): %w", d, unknownOverflowError())
		}
		return newSafe(false, 0, MaxScale)
	}

	// General case
	e, err := d.expBint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing exp(%v): %w", d, err)
	}

	return e, nil
}

// expm1Bint computes shifted exponential of a decimal using *big.Int arithmetic.
func (d Decimal) expm1Bint() (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	// Compute e = exp(d)
	d.expCoef(ecoef)

	// Compute e = e - 1
	eneg := ecoef.cmp(bpow10[bscale]) < 0
	ecoef.subAbs(ecoef, bpow10[bscale])

	return newFromBint(eneg, ecoef, bscale, 0)
}

// Expm1 returns the (possibly rounded) shifted exponential of a decimal.
//
// Expm1 returns an error if the integer part of the result has more than [MaxPrec] digits.
func (d Decimal) Expm1() (Decimal, error) {
	// Special case: zero
	if d.IsZero() {
		return newSafe(false, 0, 0)
	}

	// Special case: overflow
	if d.CmpAbs(Hundred) >= 0 {
		if !d.IsNeg() {
			return Decimal{}, fmt.Errorf("computing expm1(%v): %w", d, unknownOverflowError())
		}
		return newSafe(true, pow10[MaxScale-1], MaxScale-1)
	}

	// General case
	e, err := d.expm1Bint()
	if err != nil {
		return Decimal{}, fmt.Errorf("computing expm1(%v): %w", d, err)
	}

	return e, nil
}

// powBint computes the power of a decimal using *big.Int arithmetic.
func (d Decimal) powBint(e Decimal) (Decimal, error) {
	ecoef := getBint()
	defer putBint(ecoef)

	fcoef := getBint()
	defer putBint(fcoef)

	ecoef.setFint(e.coef)

	// Compute f = |log(d)|
	inv := d.logCoef(fcoef)

	// Compute f = ⌊f * e⌋
	fcoef.mul(fcoef, ecoef)
	fcoef.rshDown(fcoef, e.Scale())
	inv = inv != e.IsNeg()

	// Check if f <= -100 or f >= 100
	if fcoef.hasPrec(3 + bscale) {
		if !inv {
			return Decimal{}, unknownOverflowError()
		}
		return newSafe(false, 0, MaxScale)
	}

	// Compute f = exp(f)
	if inv {
		fcoef.neg(fcoef)
	}
	fcoef.exp(fcoef)

	return newFromBint(false, fcoef, bscale, 0)
}

// Pow returns the (possibly rounded) decimal raised to the given decimal power.
// If zero is raised to zero power then the result is one.
//
// Pow returns an error if:
//   - the integer part of the result has more than [MaxPrec] digits;
//   - zero is raised to a negative power;
//   - negative is raised to a fractional power.
func (d Decimal) Pow(e Decimal) (Decimal, error) {
	// Special case: zero to a negative power
	if e.IsNeg() && d.IsZero() {
		return Decimal{}, fmt.Errorf("computing [%v^%v]: %w: zero to negative power", d, e, errInvalidOperation)
	}

	// Special case: integer power
	if e.IsInt() {
		power := e.Trunc(0).Coef()
		f, err := d.powIntFint(power, e.IsNeg())
		if err != nil {
			f, err = d.powIntBint(power, e.IsNeg())
			if err != nil {
				return Decimal{}, fmt.Errorf("computing [%v^%v]: %w", d, e, err)
			}
		}

		// Preferred scale
		if e.IsNeg() {
			f = f.Trim(0)
		}

		return f, nil
	}

	// Special case: zero to a fractional power
	if d.IsZero() {
		return newSafe(false, 0, 0)
	}

	// Special case: negative to a fractional power
	if d.IsNeg() {
		return Decimal{}, fmt.Errorf("computing [%v^%v]: %w: negative to fractional power", d, e, errInvalidOperation)
	}

	// General case
	f, err := d.powBint(e)
	if err != nil {
		return Decimal{}, fmt.Errorf("computing [%v^%v]: %w", d, e, err)
	}

	return f, nil
}

// NewFromInt64 converts a pair of integers, representing the whole and
// fractional parts, to a (possibly rounded) decimal equal to whole + frac / 10^scale.
// NewFromInt64 removes all trailing zeros from the fractional part.
// This method is useful for converting amounts from [protobuf] format.
// See also method [Decimal.Int64].
//
// NewFromInt64 returns an error if:
//   - the whole and fractional parts have different signs;
//   - the scale is negative or greater than [MaxScale];
//   - frac / 10^scale is not within the range (-1, 1).
//
// [protobuf]: https://github.com/googleapis/googleapis/blob/master/google/type/money.proto
func NewFromInt64(whole, frac int64, scale int) (Decimal, error) {
	// Whole
	d, err := New(whole, 0)
	if err != nil {
		return Decimal{}, fmt.Errorf("converting integers: %w", err) // should never happen
	}
	// Fraction
	f, err := New(frac, scale)
	if err != nil {
		return Decimal{}, fmt.Errorf("converting integers: %w", err)
	}
	if !f.IsZero() {
		if !d.IsZero() && d.Sign() != f.Sign() {
			return Decimal{}, fmt.Errorf("converting integers: inconsistent signs")
		}
		if !f.WithinOne() {
			return Decimal{}, fmt.Errorf("converting integers: inconsistent fraction")
		}
		f = f.Trim(0)
		d, err = d.Add(f)
		if err != nil {
			return Decimal{}, fmt.Errorf("converting integers: %w", err) // should never happen
		}
	}
	return d, nil
}

// Int64 returns a pair of integers representing the whole and
// (possibly rounded) fractional parts of the decimal.
// If given scale is greater than the scale of the decimal, then the fractional part
// is zero-padded to the right.
// If given scale is smaller than the scale of the decimal, then the fractional part
// is rounded using [rounding half to even] (banker's rounding).
// The relationship between the decimal and the returned values can be expressed
// as d = whole + frac / 10^scale.
// This method is useful for converting amounts to [protobuf] format.
// See also constructor [NewFromInt64].
//
// If the result cannot be represented as a pair of int64 values,
// then false is returned.
//
// [rounding half to even]: https://en.wikipedia.org/wiki/Rounding#Rounding_half_to_even
// [protobuf]: https://github.com/googleapis/googleapis/blob/master/google/type/money.proto
func (d Decimal) Int64(scale int) (whole, frac int64, ok bool) {
	if scale < MinScale || scale > MaxScale {
		return 0, 0, false
	}
	x := d.coef
	y := pow10[d.Scale()]
	if scale < d.Scale() {
		x = x.rshHalfEven(d.Scale() - scale)
		y = pow10[scale]
	}
	q, r, ok := x.quoRem(y)
	if !ok {
		return 0, 0, false // Should never happen
	}
	if scale > d.Scale() {
		r, ok = r.lsh(scale - d.Scale())
		if !ok {
			return 0, 0, false // Should never happen
		}
	}
	if d.IsNeg() {
		if q > -math.MinInt64 || r > -math.MinInt64 {
			return 0, 0, false
		}
		//nolint:gosec
		return -int64(q), -int64(r), true
	}
	if q > math.MaxInt64 || r > math.MaxInt64 {
		return 0, 0, false
	}
	//nolint:gosec
	return int64(q), int64(r), true
}

// parseSign parses an optional sign at position pos and returns true if it is
// negative, together with the position of the next character.
func parseSign(text []byte, pos int) (bool, int) {
	if pos < len(text) {
		switch text[pos] {
		case '-':
			return true, pos + 1
		case '+':
			return false, pos + 1
		}
	}
	return false, pos
}

// skipZeros returns the position of the first non-zero character at or after pos.
func skipZeros(text []byte, pos int) int {
	for pos < len(text) && text[pos] == '0' {
		pos++
	}
	return pos
}

// parseDigits appends the digits starting at position pos to coef without
// checking overflow, and returns the result together with the position of
// the first non-digit character.
func parseDigits(text []byte, pos int, coef fint) (fint, int) {
	for ; pos < len(text); pos++ {
		d := text[pos] - '0'
		if d > 9 {
			break
		}
		coef = coef*10 + fint(d)
	}
	return coef, pos
}

// parseExp parses the exponent that follows 'e' or 'E' at position pos.
// The exponent must end the text and its absolute value must not exceed 330.
func parseExp(text []byte, pos int) (int, bool) {
	neg, pos := parseSign(text, pos)
	if pos == len(text) {
		return 0, false
	}
	var exp int
	for ; pos < len(text); pos++ {
		d := text[pos] - '0'
		if d > 9 {
			return 0, false
		}
		exp = exp*10 + int(d)
		if exp > 330 {
			return 0, false
		}
	}
	if neg {
		exp = -exp
	}
	return exp, true
}

// parseFint parses a decimal string using uint64 arithmetic.
// It accumulates digits without per-digit overflow checks, since any number
// of at most [MaxPrec] significant digits fits into uint64.
// parseFint returns an error for inputs that do not fit into uint64
// or are not valid, and the caller is expected to fall back to [parseBint],
// which handles all cases and produces descriptive errors.
//
//nolint:gocyclo
func parseFint(text []byte, minScale int) (Decimal, error) {
	width := len(text)

	// Sign
	neg, pos := parseSign(text, 0)

	// Integer
	start := pos
	pos = skipZeros(text, pos)
	sig := pos
	coef, pos := parseDigits(text, pos, 0)
	prec := pos - sig     // number of significant digits
	digits := pos - start // number of all digits

	// Fraction
	var scale int
	if pos < width && text[pos] == '.' {
		pos++
		start = pos
		if coef == 0 {
			pos = skipZeros(text, pos)
		}
		sig = pos
		coef, pos = parseDigits(text, pos, coef)
		scale = pos - start
		prec += pos - sig
		digits += scale
	}

	// Exponent
	if pos < width && text[pos]|0x20 == 'e' {
		exp, ok := parseExp(text, pos+1)
		if !ok {
			return Decimal{}, errInvalidDecimal
		}
		scale -= exp
		pos = width
	}

	switch {
	case pos != width || digits == 0:
		return Decimal{}, errInvalidDecimal
	case prec > MaxPrec:
		return Decimal{}, errDecimalOverflow
	case minScale <= scale && MinScale <= scale && scale <= MaxScale:
		// Fast path: the coefficient has at most MaxPrec digits and needs no rescaling
		return newUnsafe(neg, coef, scale), nil
	}
	return newFromFint(neg, coef, scale, minScale)
}

// parseBint parses a decimal string using *big.Int arithmetic.
//
//nolint:gocyclo
func parseBint(text []byte, minScale int) (Decimal, error) {
	width := len(text)

	// Sign
	neg, pos := parseSign(text, 0)

	// Coefficient
	bcoef := getBint()
	defer putBint(bcoef)
	var fcoef fint
	var shift, scale int
	var hasCoef, ok bool

	bcoef.setFint(0)

	// Algorithm:
	// 	1. Add as many digits as possible to the uint64 coefficient (fast).
	// 	2. Once the uint64 coefficient has reached its maximum value,
	//     add it to the *big.Int coefficient (slow).
	// 	3. Repeat until all digits are processed.

	// Integer
	for pos < width && text[pos] >= '0' && text[pos] <= '9' {
		fcoef, ok = fcoef.fsa(1, text[pos]-'0')
		if !ok {
			return Decimal{}, errDecimalOverflow // Should never happen
		}
		pos++
		shift++
		hasCoef = true
		if fcoef.hasPrec(MaxPrec) {
			bcoef.fsa(bcoef, shift, fcoef)
			fcoef, shift = 0, 0
		}
	}

	// Fraction
	if pos < width && text[pos] == '.' {
		pos++
		for pos < width && text[pos] >= '0' && text[pos] <= '9' {
			fcoef, ok = fcoef.fsa(1, text[pos]-'0')
			if !ok {
				return Decimal{}, errDecimalOverflow // Should never happen
			}
			pos++
			scale++
			shift++
			hasCoef = true
			if fcoef.hasPrec(MaxPrec) {
				bcoef.fsa(bcoef, shift, fcoef)
				fcoef, shift = 0, 0
			}
		}
	}
	if shift > 0 {
		bcoef.fsa(bcoef, shift, fcoef)
	}

	// Exponent
	var exp int
	var eneg, hasExp, hasE bool
	if pos < width && (text[pos] == 'e' || text[pos] == 'E') {
		hasE = true
		// Sign
		eneg, pos = parseSign(text, pos+1)
		// Integer
		for pos < width && text[pos] >= '0' && text[pos] <= '9' {
			exp = exp*10 + int(text[pos]-'0')
			if exp > 330 {
				return Decimal{}, errInvalidDecimal
			}
			pos++
			hasExp = true
		}
	}

	if pos != width {
		return Decimal{}, fmt.Errorf("%w: unexpected character %q", errInvalidDecimal, text[pos])
	}
	if !hasCoef {
		return Decimal{}, fmt.Errorf("%w: no coefficient", errInvalidDecimal)
	}
	if hasE && !hasExp {
		return Decimal{}, fmt.Errorf("%w: no exponent", errInvalidDecimal)
	}

	if eneg {
		scale = scale + exp
	} else {
		scale = scale - exp
	}

	return newFromBint(neg, bcoef, scale, minScale)
}

func parseExact(text []byte, scale int) (Decimal, error) {
	if len(text) > 330 {
		return Decimal{}, fmt.Errorf("parsing decimal: %w", errInvalidDecimal)
	}
	if scale < MinScale || scale > MaxScale {
		return Decimal{}, fmt.Errorf("parsing decimal: %w", errScaleRange)
	}
	d, err := parseFint(text, scale)
	if err != nil {
		d, err = parseBint(text, scale)
		if err != nil {
			return Decimal{}, fmt.Errorf("parsing decimal: %w", err)
		}
	}
	return d, nil
}

func parse(text []byte) (Decimal, error) {
	return parseExact(text, 0)
}

// Parse converts a string to a (possibly rounded) decimal.
// The input string must be in one of the following formats:
//
//	1.234
//	-1234
//	+0.000001234
//	1.83e5
//	0.22e-9
//
// The formal EBNF grammar for the supported format is as follows:
//
//	sign           ::= '+' | '-'
//	digits         ::= { '0' | '1' | '2' | '3' | '4' | '5' | '6' | '7' | '8' | '9' }
//	significand    ::= digits '.' digits | '.' digits | digits '.' | digits
//	exponent       ::= ('e' | 'E') [sign] digits
//	numeric-string ::= [sign] significand [exponent]
//
// Parse removes leading zeros from the integer part of the input string,
// but tries to maintain trailing zeros in the fractional part to preserve scale.
//
// Parse returns an error if:
//   - the string contains any whitespaces;
//   - the string is longer than 330 bytes;
//   - the exponent is less than -330 or greater than 330;
//   - the string does not represent a valid decimal number;
//   - the integer part of the result has more than [MaxPrec] digits.
func Parse(s string) (Decimal, error) {
	text := unsafe.Slice(unsafe.StringData(s), len(s))
	return parseExact(text, 0)
}

// MustParse is like [Parse] but panics if the string cannot be parsed.
// It simplifies safe initialization of global variables holding decimals.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(fmt.Sprintf("Parse(%q) failed: %v", s, err))
	}
	return d
}

// ParseExact is similar to [Parse], but it allows you to specify how many digits
// after the decimal point should be considered significant.
// If any of the significant digits are lost during rounding, the method will return an error.
// This method is useful for parsing monetary amounts, where the scale should be
// equal to or greater than the currency's scale.
func ParseExact(s string, scale int) (Decimal, error) {
	text := unsafe.Slice(unsafe.StringData(s), len(s))
	return parseExact(text, scale)
}

// digitPairs contains decimal representations of numbers 00 to 99 concatenated.
const digitPairs = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// format writes a string representation of the decimal to the end of buf
// and returns the position of its first byte.
func (d Decimal) format(buf *[24]byte) int {
	pos := len(buf)
	coef := uint64(d.coef)

	// Coefficient, 8 digits at a time.
	// Each group uses only 32-bit arithmetic, and its 4 pairs of digits
	// are computed independently of each other.
	for coef >= 1e8 {
		x76543210 := uint32(coef % 1e8)
		coef /= 1e8
		x7654, x3210 := x76543210/1e4, x76543210%1e4
		x76, x54 := (x7654/100)*2, (x7654%100)*2
		x32, x10 := (x3210/100)*2, (x3210%100)*2
		pos -= 8
		b := buf[pos : pos+8]
		b[7], b[6] = digitPairs[x10+1], digitPairs[x10]
		b[5], b[4] = digitPairs[x32+1], digitPairs[x32]
		b[3], b[2] = digitPairs[x54+1], digitPairs[x54]
		b[1], b[0] = digitPairs[x76+1], digitPairs[x76]
	}

	// Coefficient, remaining digits 2 at a time
	x := uint32(coef)
	for x >= 100 {
		x10 := (x % 100) * 2
		x /= 100
		pos -= 2
		b := buf[pos : pos+2]
		b[1], b[0] = digitPairs[x10+1], digitPairs[x10]
	}
	if x >= 10 {
		x10 := x * 2
		pos -= 2
		b := buf[pos : pos+2]
		b[1], b[0] = digitPairs[x10+1], digitPairs[x10]
	} else {
		pos--
		buf[pos] = byte(x) + '0'
	}

	// Decimal point
	if scale := d.Scale(); scale > 0 {
		dot := len(buf) - scale - 1
		if pos > dot {
			// No integer digits, add leading zeros
			for pos > dot+1 {
				pos--
				buf[pos] = '0'
			}
			buf[dot], buf[dot-1] = '.', '0'
			pos = dot - 1
		} else {
			// Shift integer digits to make room for the decimal point
			pos--
			b := buf[pos : dot+1]
			for i := 1; i < len(b); i++ {
				b[i-1] = b[i]
			}
			b[len(b)-1] = '.'
		}
	}

	// Sign
	if d.IsNeg() {
		pos--
		buf[pos] = '-'
	}

	return pos
}

// append appends a string representation of the decimal to the byte slice.
func (d Decimal) append(text []byte) []byte {
	var buf [24]byte
	pos := d.format(&buf)
	return append(text, buf[pos:]...)
}

// bytes returns a string representation of the decimal as a byte slice.
func (d Decimal) bytes() []byte {
	text := make([]byte, 0, 24)
	return d.append(text)
}

// String implements the [fmt.Stringer] interface and returns
// a string representation of the decimal.
// The returned string does not use scientific or engineering notation and is
// formatted according to the following formal EBNF grammar:
//
//	sign           ::= '-'
//	digits         ::= { '0' | '1' | '2' | '3' | '4' | '5' | '6' | '7' | '8' | '9' }
//	significand    ::= digits '.' digits | digits
//	numeric-string ::= [sign] significand
//
// See also method [Decimal.Format].
//
// [fmt.Stringer]: https://pkg.go.dev/fmt#Stringer
func (d Decimal) String() string {
	// Fast path: small non-negative integers are substrings of digitPairs
	if d.coef < 100 && d.scale == 0 && !d.neg {
		end := int(d.coef)*2 + 2
		if d.coef < 10 {
			return digitPairs[end-1 : end]
		}
		return digitPairs[end-2 : end]
	}
	var buf [24]byte
	pos := d.format(&buf)
	return string(buf[pos:])
}

// Format implements the [fmt.Formatter] interface.
// The following [format verbs] are available:
//
//	| Verb       | Example | Description    |
//	| ---------- | ------- | -------------- |
//	| %f, %s, %v | 5.67    | Decimal        |
//	| %q         | "5.67"  | Quoted decimal |
//	| %k         | 567%    | Percentage     |
//
// The following format flags can be used with all verbs: '+', ' ', '0', '-'.
//
// Precision is only supported for %f and %k verbs.
// For %f verb, the default precision is equal to the actual scale of the decimal,
// whereas, for verb %k the default precision is the actual scale of the decimal minus 2.
//
// [format verbs]: https://pkg.go.dev/fmt#hdr-Printing
// [fmt.Formatter]: https://pkg.go.dev/fmt#Formatter
//
//nolint:gocyclo
func (d Decimal) Format(state fmt.State, verb rune) {
	var err error

	// Percentage multiplier
	if verb == 'k' || verb == 'K' {
		d, err = d.Mul(Hundred)
		if err != nil {
			// This panic is handled inside the fmt package.
			panic(fmt.Errorf("formatting percent: %w", err))
		}
	}

	// Rescaling
	var tzeros int
	if verb == 'f' || verb == 'F' || verb == 'k' || verb == 'K' {
		var scale int
		switch p, ok := state.Precision(); {
		case ok:
			scale = p
		case verb == 'k' || verb == 'K':
			scale = d.Scale() - 2
		case verb == 'f' || verb == 'F':
			scale = d.Scale()
		}
		scale = max(scale, MinScale)
		switch {
		case scale < d.Scale():
			d = d.Round(scale)
		case scale > d.Scale():
			tzeros = scale - d.Scale()
		}
	}

	// Integer and fractional digits
	var intdigs int
	fracdigs := d.Scale()
	if dprec := d.Prec(); dprec > fracdigs {
		intdigs = dprec - fracdigs
	}
	if d.WithinOne() {
		intdigs++ // leading 0
	}

	// Decimal point
	var dpoint int
	if fracdigs > 0 || tzeros > 0 {
		dpoint = 1
	}

	// Arithmetic sign
	var rsign int
	if d.IsNeg() || state.Flag('+') || state.Flag(' ') {
		rsign = 1
	}

	// Percentage sign
	var psign int
	if verb == 'k' || verb == 'K' {
		psign = 1
	}

	// Openning and closing quotes
	var lquote, tquote int
	if verb == 'q' || verb == 'Q' {
		lquote, tquote = 1, 1
	}

	// Calculating padding
	width := lquote + rsign + intdigs + dpoint + fracdigs + tzeros + psign + tquote
	var lspaces, tspaces, lzeros int
	if w, ok := state.Width(); ok && w > width {
		switch {
		case state.Flag('-'):
			tspaces = w - width
		case state.Flag('0'):
			lzeros = w - width
		default:
			lspaces = w - width
		}
		width = w
	}

	buf := make([]byte, width)
	pos := width - 1

	// Trailing spaces
	for range tspaces {
		buf[pos] = ' '
		pos--
	}

	// Closing quote
	for range tquote {
		buf[pos] = '"'
		pos--
	}

	// Percentage sign
	for range psign {
		buf[pos] = '%'
		pos--
	}

	// Trailing zeros
	for range tzeros {
		buf[pos] = '0'
		pos--
	}

	// Fractional digits
	dcoef := d.Coef()
	for range fracdigs {
		buf[pos] = byte(dcoef%10) + '0'
		pos--
		dcoef /= 10
	}

	// Decimal point
	for range dpoint {
		buf[pos] = '.'
		pos--
	}

	// Integer digits
	for range intdigs {
		buf[pos] = byte(dcoef%10) + '0'
		pos--
		dcoef /= 10
	}

	// Leading zeros
	for range lzeros {
		buf[pos] = '0'
		pos--
	}

	// Arithmetic sign
	for range rsign {
		if d.IsNeg() {
			buf[pos] = '-'
		} else if state.Flag(' ') {
			buf[pos] = ' '
		} else {
			buf[pos] = '+'
		}
		pos--
	}

	// Opening quote
	for range lquote {
		buf[pos] = '"'
		pos--
	}

	// Leading spaces
	for range lspaces {
		buf[pos] = ' '
		pos--
	}

	// Writing result
	//nolint:errcheck
	switch verb {
	case 'q', 'Q', 's', 'S', 'v', 'V', 'f', 'F', 'k', 'K':
		state.Write(buf)
	default:
		state.Write([]byte("%!"))
		state.Write(utf8.AppendRune(nil, verb))
		state.Write([]byte("(decimal.Decimal="))
		state.Write(buf)
		state.Write([]byte(")"))
	}
}

// NewFromFloat64 converts a float to a (possibly rounded) decimal.
// See also method [Decimal.Float64].
//
// NewFromFloat64 returns an error if:
//   - the float is a special value (NaN or Inf);
//   - the integer part of the result has more than [MaxPrec] digits.
func NewFromFloat64(f float64) (Decimal, error) {
	// Float
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Decimal{}, fmt.Errorf("converting float: special value %v", f)
	}
	text := make([]byte, 0, 32)
	text = strconv.AppendFloat(text, f, 'f', -1, 64)

	// Decimal
	d, err := parse(text)
	if err != nil {
		return Decimal{}, fmt.Errorf("converting float: %w", err)
	}
	return d, nil
}

// Float64 returns the nearest binary floating-point number rounded
// using [rounding half to even] (banker's rounding).
// See also constructor [NewFromFloat64].
//
// This conversion may lose data, as float64 has a smaller precision
// than the decimal type.
//
// [rounding half to even]: https://en.wikipedia.org/wiki/Rounding#Rounding_half_to_even
func (d Decimal) Float64() (f float64, ok bool) {
	// Fast path: the coefficient and the power of ten are exactly representable
	// as float64, so their quotient is correctly rounded.
	if d.coef <= 1<<53 {
		f = float64(d.coef) / float64(pow10[d.scale])
		if d.neg {
			f = -f
		}
		return f, true
	}
	var buf [24]byte
	pos := d.format(&buf)
	f, err := strconv.ParseFloat(string(buf[pos:]), 64)
	if err != nil {
		return 0, false // Should never happen
	}
	return f, true
}

// UnmarshalJSON implements the [json.Unmarshaler] interface.
// UnmarshalJSON supports the following types: [number] and [numeric string].
// See also constructor [Parse].
//
// [number]: https://datatracker.ietf.org/doc/html/rfc8259#section-6
// [numeric string]: https://datatracker.ietf.org/doc/html/rfc8259#section-7
// [json.Unmarshaler]: https://pkg.go.dev/encoding/json#Unmarshaler
func (d *Decimal) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		data = data[1 : len(data)-1]
	}
	var err error
	*d, err = parse(data)
	if err != nil {
		return fmt.Errorf("unmarshaling %T: %w", Decimal{}, err)
	}
	return nil
}

// MarshalJSON implements the [json.Marshaler] interface.
// MarshalJSON always returns a [numeric string].
// See also method [Decimal.String].
//
// [numeric string]: https://datatracker.ietf.org/doc/html/rfc8259#section-7
// [json.Marshaler]: https://pkg.go.dev/encoding/json#Marshaler
func (d Decimal) MarshalJSON() ([]byte, error) {
	text := make([]byte, 0, 26)
	text = append(text, '"')
	text = d.append(text)
	text = append(text, '"')
	return text, nil
}

// MarshalJSONTo implements the [json.MarshalerTo] interface.
// MarshalJSONTo always writes a [numeric string].
// See also method [Decimal.MarshalJSON].
//
// [numeric string]: https://datatracker.ietf.org/doc/html/rfc8259#section-7
// [json.MarshalerTo]: https://pkg.go.dev/encoding/json/v2#MarshalerTo
func (d Decimal) MarshalJSONTo(enc *jsontext.Encoder) error {
	text := enc.AvailableBuffer()
	text = append(text, '"')
	text = d.append(text)
	text = append(text, '"')
	return enc.WriteValue(text)
}

// UnmarshalText implements the [encoding.TextUnmarshaler] interface.
// UnmarshalText supports only numeric strings.
// See also constructor [Parse].
//
// [encoding.TextUnmarshaler]: https://pkg.go.dev/encoding#TextUnmarshaler
func (d *Decimal) UnmarshalText(text []byte) error {
	var err error
	*d, err = parse(text)
	if err != nil {
		return fmt.Errorf("unmarshaling %T: %w", Decimal{}, err)
	}
	return nil
}

// AppendText implements the [encoding.TextAppender] interface.
// AppendText always appends a numeric string.
// See also method [Decimal.String].
//
// [encoding.TextAppender]: https://pkg.go.dev/encoding#TextAppender
func (d Decimal) AppendText(text []byte) ([]byte, error) {
	return d.append(text), nil
}

// MarshalText implements the [encoding.TextMarshaler] interface.
// MarshalText always returns a numeric string.
// See also method [Decimal.String].
//
// [encoding.TextMarshaler]: https://pkg.go.dev/encoding#TextMarshaler
func (d Decimal) MarshalText() ([]byte, error) {
	return d.bytes(), nil
}

// UnmarshalGQL implements the [graphql.Unmarshaler] interface.
// UnmarshalGQL supports the following types: string, [json.Number], int64, int, and float64.
// Values of type float64 come from GraphQL float literals and may already be rounded,
// use strings or variables to preserve precision.
// UnmarshalGQL does not support null values, use [NullDecimal] or *[Decimal] instead.
// See also constructor [Parse].
//
// [graphql.Unmarshaler]: https://pkg.go.dev/github.com/99designs/gqlgen/graphql#Unmarshaler
func (d *Decimal) UnmarshalGQL(v any) error {
	var err error
	switch v := v.(type) {
	case string:
		*d, err = Parse(v)
	case json.Number:
		// gqlgen decodes numeric variables using json.Decoder.UseNumber
		*d, err = Parse(string(v))
	case int64:
		*d, err = New(v, 0)
	case int:
		*d, err = New(int64(v), 0)
	case float64:
		*d, err = NewFromFloat64(v)
	case nil:
		err = fmt.Errorf("%T does not support null values, use %T or *%T", Decimal{}, NullDecimal{}, Decimal{})
	default:
		err = fmt.Errorf("type %T is not supported", v)
	}
	if err != nil {
		err = fmt.Errorf("unmarshaling %T to %T: %w", v, Decimal{}, err)
	}
	return err
}

// MarshalGQL implements the [graphql.Marshaler] interface.
// MarshalGQL always writes a [numeric string].
// See also method [Decimal.MarshalJSON].
//
// [numeric string]: https://datatracker.ietf.org/doc/html/rfc8259#section-7
// [graphql.Marshaler]: https://pkg.go.dev/github.com/99designs/gqlgen/graphql#Marshaler
func (d Decimal) MarshalGQL(w io.Writer) {
	text, _ := d.MarshalJSON()
	_, _ = w.Write(text) // graphql.Marshaler cannot report write errors
}

// UnmarshalBinary implements the [encoding.BinaryUnmarshaler] interface.
// UnmarshalBinary supports only numeric strings.
// See also constructor [Parse].
//
// [encoding.BinaryUnmarshaler]: https://pkg.go.dev/encoding#BinaryUnmarshaler
func (d *Decimal) UnmarshalBinary(data []byte) error {
	var err error
	*d, err = parse(data)
	if err != nil {
		return fmt.Errorf("unmarshaling %T: %w", Decimal{}, err)
	}
	return nil
}

// AppendBinary implements the [encoding.BinaryAppender] interface.
// AppendBinary always appends a numeric string.
// See also method [Decimal.String].
//
// [encoding.BinaryAppender]: https://pkg.go.dev/encoding#BinaryAppender
func (d Decimal) AppendBinary(data []byte) ([]byte, error) {
	return d.append(data), nil
}

// MarshalBinary implements the [encoding.BinaryMarshaler] interface.
// MarshalBinary always returns a numeric string.
// See also method [Decimal.String].
//
// [encoding.BinaryMarshaler]: https://pkg.go.dev/encoding#BinaryMarshaler
func (d Decimal) MarshalBinary() ([]byte, error) {
	return d.bytes(), nil
}

// parseBSONInt32 parses a BSON int32 to a decimal.
// The byte order of the input data must be little-endian.
func parseBSONInt32(data []byte) (Decimal, error) {
	if len(data) != 4 {
		return Decimal{}, fmt.Errorf("%w: invalid data length %v", errInvalidDecimal, len(data))
	}
	i := int64(int32(binary.LittleEndian.Uint32(data))) //nolint:gosec
	return New(i, 0)
}

// parseBSONInt64 parses a BSON int64 to a decimal.
// The byte order of the input data must be little-endian.
func parseBSONInt64(data []byte) (Decimal, error) {
	if len(data) != 8 {
		return Decimal{}, fmt.Errorf("%w: invalid data length %v", errInvalidDecimal, len(data))
	}
	i := int64(binary.LittleEndian.Uint64(data)) //nolint:gosec
	return New(i, 0)
}

// parseBSONFloat64 parses a BSON float64 to a (possibly rounded) decimal.
// The byte order of the input data must be little-endian.
func parseBSONFloat64(data []byte) (Decimal, error) {
	if len(data) != 8 {
		return Decimal{}, fmt.Errorf("%w: invalid data length %v", errInvalidDecimal, len(data))
	}
	f := math.Float64frombits(binary.LittleEndian.Uint64(data))
	return NewFromFloat64(f)
}

// parseBSONString parses a BSON string to a (possibly rounded) decimal.
// The byte order of the input data must be little-endian.
func parseBSONString(data []byte) (Decimal, error) {
	if len(data) < 4 {
		return Decimal{}, fmt.Errorf("%w: invalid data length %v", errInvalidDecimal, len(data))
	}
	l := int(int32(binary.LittleEndian.Uint32(data))) //nolint:gosec
	if l < 1 || l > 330 || len(data) < l+4 {
		return Decimal{}, fmt.Errorf("%w: invalid string length %v", errInvalidDecimal, l)
	}
	if data[l+4-1] != 0 {
		return Decimal{}, fmt.Errorf("%w: invalid null terminator %v", errInvalidDecimal, data[l+4-1])
	}
	s := string(data[4 : l+4-1])
	return Parse(s)
}

// parseIEEEDecimal128 converts a 128-bit IEEE 754-2008 decimal
// floating point with binary integer decimal encoding to
// a (possibly rounded) decimal.
// The byte order of the input data must be little-endian.
//
// parseIEEEDecimal128 returns an error if:
//   - the data length is not equal to 16 bytes;
//   - the decimal a special value (NaN or Inf);
//   - the integer part of the result has more than [MaxPrec] digits.
func parseIEEEDecimal128(data []byte) (Decimal, error) {
	if len(data) != 16 {
		return Decimal{}, fmt.Errorf("%w: invalid data length %v", errInvalidDecimal, len(data))
	}
	if data[15]&0b0111_1100 == 0b0111_1100 {
		return Decimal{}, fmt.Errorf("%w: special value NaN", errInvalidDecimal)
	}
	if data[15]&0b0111_1100 == 0b0111_1000 {
		return Decimal{}, fmt.Errorf("%w: special value Inf", errInvalidDecimal)
	}
	if data[15]&0b0110_0000 == 0b0110_0000 {
		return Decimal{}, fmt.Errorf("%w: unsupported encoding", errInvalidDecimal)
	}

	// Sign
	neg := data[15]&0b1000_0000 == 0b1000_0000

	// Scale
	var scale int
	scale |= int(data[14]) >> 1
	scale |= int(data[15]&0b0111_1111) << 7
	scale = 6176 - scale

	// TODO fint optimization

	// Coefficient
	coef := getBint()
	defer putBint(coef)

	buf := make([]byte, 15)
	for i := range 15 {
		buf[i] = data[14-i]
	}
	buf[0] &= 0b0000_0001
	coef.setBytes(buf)

	// Scale normalization
	if coef.sign() == 0 {
		scale = max(scale, MinScale)
	}

	return newFromBint(neg, coef, scale, 0)
}

// ieeeDecimal128 returns a 128-bit IEEE 754-2008 decimal
// floating point with binary integer decimal encoding.
// The byte order of the result is little-endian.
func (d Decimal) ieeeDecimal128() []byte {
	var buf [16]byte
	scale := d.Scale()
	coef := d.Coef()

	// Sign
	if d.IsNeg() {
		buf[15] = 0b1000_0000
	}

	// Scale
	scale = 6176 - scale
	buf[15] |= byte((scale >> 7) & 0b0111_1111)
	buf[14] |= byte((scale << 1) & 0b1111_1110)

	// Coefficient
	for i := range 8 {
		buf[i] = byte(coef & 0b1111_1111)
		coef >>= 8
	}

	return buf[:]
}

// UnmarshalBSONValue implements the [v2/bson.ValueUnmarshaler] interface.
// UnmarshalBSONValue supports the following [types]: Double, String, 32-bit Integer, 64-bit Integer, and [Decimal128].
//
// [v2/bson.ValueUnmarshaler]: https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/bson#ValueUnmarshaler
// [types]: https://bsonspec.org/spec.html
// [Decimal128]: https://github.com/mongodb/specifications/blob/master/source/bson-decimal128/decimal128.md
func (d *Decimal) UnmarshalBSONValue(typ byte, data []byte) error {
	// constants are from https://bsonspec.org/spec.html
	var err error
	switch typ {
	case 1:
		*d, err = parseBSONFloat64(data)
	case 2:
		*d, err = parseBSONString(data)
	case 10:
		// null, do nothing
	case 16:
		*d, err = parseBSONInt32(data)
	case 18:
		*d, err = parseBSONInt64(data)
	case 19:
		*d, err = parseIEEEDecimal128(data)
	default:
		err = fmt.Errorf("BSON type %d is not supported", typ)
	}
	if err != nil {
		err = fmt.Errorf("converting from BSON type %d to %T: %w", typ, Decimal{}, err)
	}
	return err
}

// MarshalBSONValue implements the [v2/bson.ValueMarshaler] interface.
// MarshalBSONValue always returns [Decimal128].
//
// [v2/bson.ValueMarshaler]: https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/bson#ValueMarshaler
// [Decimal128]: https://github.com/mongodb/specifications/blob/master/source/bson-decimal128/decimal128.md
func (d Decimal) MarshalBSONValue() (typ byte, data []byte, err error) {
	return 19, d.ieeeDecimal128(), nil
}

// Scan implements the [sql.Scanner] interface.
//
// [sql.Scanner]: https://pkg.go.dev/database/sql#Scanner
func (d *Decimal) Scan(value any) error {
	var err error
	switch value := value.(type) {
	case string:
		*d, err = Parse(value)
	case int64:
		*d, err = New(value, 0)
	case float64:
		*d, err = NewFromFloat64(value)
	case []byte:
		// Special case: MySQL driver sends DECIMAL as []byte
		*d, err = parse(value)
	case float32:
		// Special case: MySQL driver sends FLOAT as float32
		*d, err = NewFromFloat64(float64(value))
	case uint64:
		// Special case: ClickHouse driver sends 0 as uint64
		*d, err = newSafe(false, fint(value), 0)
	case nil:
		err = fmt.Errorf("%T does not support null values, use %T or *%T", Decimal{}, NullDecimal{}, Decimal{})
	default:
		err = fmt.Errorf("type %T is not supported", value)
	}
	if err != nil {
		err = fmt.Errorf("converting from %T to %T: %w", value, Decimal{}, err)
	}
	return err
}

// Value implements the [driver.Valuer] interface.
//
// [driver.Valuer]: https://pkg.go.dev/database/sql/driver#Valuer
func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}

// NullDecimal represents a decimal that can be null.
// Its zero value is null.
// NullDecimal is not thread-safe.
type NullDecimal struct {
	Decimal Decimal
	Valid   bool
}

// Scan implements the [sql.Scanner] interface.
// See also method [Decimal.Scan].
//
// [sql.Scanner]: https://pkg.go.dev/database/sql#Scanner
func (n *NullDecimal) Scan(value any) error {
	if value == nil {
		n.Decimal = Decimal{}
		n.Valid = false
		return nil
	}
	n.Valid = true
	return n.Decimal.Scan(value)
}

// Value implements the [driver.Valuer] interface.
// See also method [Decimal.Value].
//
// [driver.Valuer]: https://pkg.go.dev/database/sql/driver#Valuer
func (n NullDecimal) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Decimal.Value()
}

// UnmarshalJSON implements the [json.Unmarshaler] interface.
// See also method [Decimal.UnmarshalJSON].
//
// [json.Unmarshaler]: https://pkg.go.dev/encoding/json#Unmarshaler
func (n *NullDecimal) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		n.Decimal = Decimal{}
		n.Valid = false
		return nil
	}
	n.Valid = true
	return n.Decimal.UnmarshalJSON(data)
}

// MarshalJSON implements the [json.Marshaler] interface.
// See also method [Decimal.MarshalJSON].
//
// [json.Marshaler]: https://pkg.go.dev/encoding/json#Marshaler
func (n NullDecimal) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return n.Decimal.MarshalJSON()
}

// MarshalJSONTo implements the [json.MarshalerTo] interface.
// See also method [Decimal.MarshalJSONTo].
//
// [json.MarshalerTo]: https://pkg.go.dev/encoding/json/v2#MarshalerTo
func (n NullDecimal) MarshalJSONTo(enc *jsontext.Encoder) error {
	if !n.Valid {
		return enc.WriteToken(jsontext.Null)
	}
	return n.Decimal.MarshalJSONTo(enc)
}

// UnmarshalGQL implements the [graphql.Unmarshaler] interface.
// See also method [Decimal.UnmarshalGQL].
//
// [graphql.Unmarshaler]: https://pkg.go.dev/github.com/99designs/gqlgen/graphql#Unmarshaler
func (n *NullDecimal) UnmarshalGQL(v any) error {
	if v == nil {
		n.Decimal = Decimal{}
		n.Valid = false
		return nil
	}
	n.Valid = true
	return n.Decimal.UnmarshalGQL(v)
}

// MarshalGQL implements the [graphql.Marshaler] interface.
// See also method [Decimal.MarshalGQL].
//
// [graphql.Marshaler]: https://pkg.go.dev/github.com/99designs/gqlgen/graphql#Marshaler
func (n NullDecimal) MarshalGQL(w io.Writer) {
	if !n.Valid {
		_, _ = io.WriteString(w, "null") // graphql.Marshaler cannot report write errors
		return
	}
	n.Decimal.MarshalGQL(w)
}

// UnmarshalBSONValue implements the [v2/bson.ValueUnmarshaler] interface.
// UnmarshalBSONValue supports the following [types]: Null, Double, String, 32-bit Integer, 64-bit Integer, and [Decimal128].
// See also method [Decimal.UnmarshalBSONValue].
//
// [v2/bson.ValueUnmarshaler]: https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/bson#ValueUnmarshaler
// [types]: https://bsonspec.org/spec.html
// [Decimal128]: https://github.com/mongodb/specifications/blob/master/source/bson-decimal128/decimal128.md
func (n *NullDecimal) UnmarshalBSONValue(typ byte, data []byte) error {
	// constants are from https://bsonspec.org/spec.html
	if typ == 10 {
		n.Decimal = Decimal{}
		n.Valid = false
		return nil
	}
	n.Valid = true
	return n.Decimal.UnmarshalBSONValue(typ, data)
}

// MarshalBSONValue implements the [v2/bson.ValueMarshaler] interface.
// MarshalBSONValue returns [Null] or [Decimal128].
// See also method [Decimal.MarshalBSONValue].
//
// [v2/bson.ValueMarshaler]: https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/bson#ValueMarshaler
// [Null]: https://bsonspec.org/spec.html
// [Decimal128]: https://github.com/mongodb/specifications/blob/master/source/bson-decimal128/decimal128.md
func (n NullDecimal) MarshalBSONValue() (typ byte, data []byte, err error) {
	// constants are from https://bsonspec.org/spec.html
	if !n.Valid {
		return 10, nil, nil
	}
	return n.Decimal.MarshalBSONValue()
}
