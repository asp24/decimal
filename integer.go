package decimal

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"sync"
)

// fint (Fast INTeger) is a wrapper around uint64.
type fint uint64

// maxFint is a maximum value of fint.
const maxFint = 9_999_999_999_999_999_999

// pow10 is a cache of powers of 10, where pow10[x] = 10^x.
var pow10 = [...]fint{
	1,                          // 10^0
	10,                         // 10^1
	100,                        // 10^2
	1_000,                      // 10^3
	10_000,                     // 10^4
	100_000,                    // 10^5
	1_000_000,                  // 10^6
	10_000_000,                 // 10^7
	100_000_000,                // 10^8
	1_000_000_000,              // 10^9
	10_000_000_000,             // 10^10
	100_000_000_000,            // 10^11
	1_000_000_000_000,          // 10^12
	10_000_000_000_000,         // 10^13
	100_000_000_000_000,        // 10^14
	1_000_000_000_000_000,      // 10^15
	10_000_000_000_000_000,     // 10^16
	100_000_000_000_000_000,    // 10^17
	1_000_000_000_000_000_000,  // 10^18
	10_000_000_000_000_000_000, // 10^19
}

// add calculates x + y and checks overflow.
func (x fint) add(y fint) (z fint, ok bool) {
	if maxFint-x < y {
		return 0, false
	}
	z = x + y
	return z, true
}

// mul calculates x * y and checks overflow.
func (x fint) mul(y fint) (z fint, ok bool) {
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	if hi != 0 || lo > maxFint {
		return 0, false
	}
	return fint(lo), true
}

// quo calculates x / y and checks division by zero and inexact division.
func (x fint) quo(y fint) (z fint, ok bool) {
	if y == 0 {
		return 0, false
	}
	z = x / y
	if z*y != x {
		return 0, false
	}
	return z, true
}

// quoRem calculates q = ⌊x / y⌋, r = x - y * q and checks division by zero.
func (x fint) quoRem(y fint) (q, r fint, ok bool) {
	if y == 0 {
		return 0, 0, false
	}
	q = x / y
	r = x - q*y
	return q, r, true
}

// subAbs calculates |x - y|.
func (x fint) subAbs(y fint) fint {
	if x > y {
		return x - y
	}
	return y - x
}

// lsh (Left Shift) calculates x * 10^shift and checks overflow.
func (x fint) lsh(shift int) (z fint, ok bool) {
	// Special cases
	switch {
	case shift <= 0:
		return x, true
	case shift == 1 && x < maxFint/10: // to speed up common case
		return x * 10, true
	case shift >= len(pow10):
		return 0, false
	}
	// General case
	y := pow10[shift]
	return x.mul(y)
}

// fsa (Fused Shift and Addition) calculates x * 10^shift + b and checks overflow.
func (x fint) fsa(shift int, b byte) (z fint, ok bool) {
	z, ok = x.lsh(shift)
	if !ok {
		return 0, false
	}
	z, ok = z.add(fint(b))
	if !ok {
		return 0, false
	}
	return z, true
}

func (x fint) isOdd() bool {
	return x&1 != 0
}

// rshHalfEven (Right Shift) calculates round(x / 10^shift) and rounds result
// using "half to even" rule.
func (x fint) rshHalfEven(shift int) fint {
	// Special cases
	switch {
	case x == 0:
		return 0
	case shift <= 0:
		return x
	case shift >= len(pow10):
		return 0
	}
	// General case
	y := pow10[shift]
	z := x / y
	r := x - z*y                        // r = x % y
	y = y >> 1                          // y = y / 2, which is safe as y is a multiple of 10
	if y < r || (y == r && z.isOdd()) { // half-to-even
		z++
	}
	return z
}

// rshUp (Right Shift) calculates ⌈x / 10^shift⌉ and rounds result away from zero.
func (x fint) rshUp(shift int) fint {
	// Special cases
	switch {
	case x == 0:
		return 0
	case shift <= 0:
		return x
	case shift >= len(pow10):
		return 1
	}
	// General case
	y := pow10[shift]
	z := x / y
	r := x - z*y // r = x % y
	if r > 0 {
		z++
	}
	return z
}

// rshDown (Right Shift) calculates ⌊x / 10^shift⌋ and rounds result towards zero.
func (x fint) rshDown(shift int) fint {
	// Special cases
	switch {
	case x == 0:
		return 0
	case shift <= 0:
		return x
	case shift >= len(pow10):
		return 0
	}
	// General case
	y := pow10[shift]
	return x / y
}

// prec returns length of x in decimal digits.
// prec assumes that 0 has no digits.
func (x fint) prec() int {
	// 1233 / 4096 is an approximation of log10(2) from below,
	// so p is either the length of x or one less than it.
	p := bits.Len64(uint64(x)) * 1233 >> 12
	if x >= pow10[p] {
		p++
	}
	return p
}

// rshExact calculates x / 10^shift and adds shift to n if x is a multiple
// of 10^shift, otherwise it returns x and n unchanged.
// inv must be the multiplicative inverse of 5^shift modulo 2^64,
// and bound must be equal to ⌊(2^64 - 1) / 10^shift⌋.
//
// If x = 10^shift * y, then x * inv = 2^shift * y (mod 2^64), and rotating
// it right by shift bits gives y, which does not exceed bound.
// Conversely, if the rotated value r does not exceed this bound,
// then x = r * 10^shift (mod 2^64), and since both sides are less
// than 2^64, x is a multiple of 10^shift.
func (x fint) rshExact(shift int, inv, bound uint64, n int) (fint, int) {
	r := bits.RotateLeft64(uint64(x)*inv, -shift)
	if r <= bound {
		return fint(r), n + shift
	}
	return x, n
}

// trimZeros removes at most limit trailing zeros from x and
// returns the result together with the number of removed zeros.
// Since x has at most 19 trailing zeros, the zeros are removed in groups
// of 16, 8, 4, 2 and 1.
func (x fint) trimZeros(limit int) (z fint, n int) {
	if limit >= 16 {
		x, n = x.rshExact(16, 0xe4a4d1417cd9a041, 1_844, n)
	}
	if limit-n >= 8 {
		x, n = x.rshExact(8, 0xc767074b22e90e21, 184_467_440_737, n)
	}
	if limit-n >= 4 {
		x, n = x.rshExact(4, 0xd288ce703afb7e91, 1_844_674_407_370_955, n)
	}
	if limit-n >= 2 {
		x, n = x.rshExact(2, 0x8f5c28f5c28f5c29, 184_467_440_737_095_516, n)
	}
	if limit-n >= 1 {
		x, n = x.rshExact(1, 0xcccccccccccccccd, 1_844_674_407_370_955_161, n)
	}
	return x, n
}

// ntz returns number of trailing zeros in x.
// ntz assumes that 0 has no trailing zeros.
func (x fint) ntz() int {
	if x == 0 {
		return 0
	}
	_, n := x.trimZeros(len(pow10))
	return n
}

// hasPrec returns true if x has given number of digits or more.
// hasPrec assumes that 0 has no digits.
//
// x.hasPrec(p) is significantly faster than x.prec() >= p.
func (x fint) hasPrec(prec int) bool {
	// Special cases
	switch {
	case prec < 1:
		return true
	case prec > len(pow10):
		return false
	}
	// General case
	return x >= pow10[prec-1]
}

// mulPow10 calculates x * 10^n as a 128-bit integer.
// The result must not exceed 2^128 - 1, otherwise it is undefined.
func mulPow10(x uint64, n int) (hi, lo uint64) {
	if n <= MaxPrec {
		return bits.Mul64(x, uint64(pow10[n]))
	}
	hi, lo = bits.Mul64(x, uint64(pow10[MaxPrec]))
	p := uint64(pow10[n-MaxPrec])
	carry, lo := bits.Mul64(lo, p)
	return hi*p + carry, lo
}

// isqrt128 calculates the integer square root q = ⌊√x⌋ and the remainder r = x - q²
// of a 128-bit integer x.
// x must be less than 10^38, otherwise the result is undefined.
func isqrt128(xhi, xlo uint64) (q, rhi, rlo uint64) {
	// Initial guess has 53 correct bits.
	q = uint64(math.Sqrt(float64(xhi)*(1<<64) + float64(xlo)))

	// One step of Newton's method doubles the number of correct bits.
	if q != 0 {
		t, _ := bits.Div64(xhi, xlo, q)
		q = (q>>1 + t>>1) + (q & t & 1)
	}

	// Correction
	for {
		hi, lo := bits.Mul64(q, q)
		if hi < xhi || hi == xhi && lo <= xlo {
			break
		}
		q--
	}
	for {
		hi, lo := bits.Mul64(q+1, q+1)
		if hi > xhi || hi == xhi && lo > xlo {
			break
		}
		q++
	}

	hi, lo := bits.Mul64(q, q)
	rlo, borrow := bits.Sub64(xlo, lo, 0)
	rhi, _ = bits.Sub64(xhi, hi, borrow)
	return q, rhi, rlo
}

// bint (Big INTeger) is a wrapper around big.Int.
type bint big.Int

// mustParseBint converts a string to *big.Int, panicking on error.
// Use only for package variable initialization and test code!
func mustParseBint(s string) *bint {
	z, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(fmt.Errorf("mustParseBint(%q) failed: parsing error", s))
	}
	if z.Sign() < 0 {
		panic(fmt.Errorf("mustParseBint(%q) failed: negative number", s))
	}
	return (*bint)(z)
}

// bpow10 is a cache of powers of 10, where bpow10[x] = 10^x.
var bpow10 = [...]*bint{
	mustParseBint("1"),
	mustParseBint("10"),
	mustParseBint("100"),
	mustParseBint("1000"),
	mustParseBint("10000"),
	mustParseBint("100000"),
	mustParseBint("1000000"),
	mustParseBint("10000000"),
	mustParseBint("100000000"),
	mustParseBint("1000000000"),
	mustParseBint("10000000000"),
	mustParseBint("100000000000"),
	mustParseBint("1000000000000"),
	mustParseBint("10000000000000"),
	mustParseBint("100000000000000"),
	mustParseBint("1000000000000000"),
	mustParseBint("10000000000000000"),
	mustParseBint("100000000000000000"),
	mustParseBint("1000000000000000000"),
	mustParseBint("10000000000000000000"),
	mustParseBint("100000000000000000000"),
	mustParseBint("1000000000000000000000"),
	mustParseBint("10000000000000000000000"),
	mustParseBint("100000000000000000000000"),
	mustParseBint("1000000000000000000000000"),
	mustParseBint("10000000000000000000000000"),
	mustParseBint("100000000000000000000000000"),
	mustParseBint("1000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("10000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
	mustParseBint("1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
}

// bscale is a scale of intermediate *big.Int results and precomputed values in blogTwo and blogTen.
const bscale = 41

// blogTwo is the natural logarithm of 2, where blogTwo = round(ln(2) * 10^bscale).
var blogTwo = mustParseBint("69314718055994530941723212145817656807550")

// blogTen is the natural logarithm of 10, where blogTen = round(ln(10) * 10^bscale).
var blogTen = mustParseBint("230258509299404568401799145468436420760110")

// lnFracBits is the number of fractional bits of binary fixed-point numbers
// used in the computation of logarithms and exponentials.
const lnFracBits = 192

// blnTwo is the natural logarithm of 2, where blnTwo = round(ln(2) * 2^lnFracBits).
var blnTwo = mustParseBint("4350955369971217654477563090224794165364344896676135745070")

// blnTen is the natural logarithm of 10, where blnTen = round(ln(10) * 2^lnFracBits).
var blnTen = mustParseBint("14453560883108425870374435737727394665043423914827363902522")

// binvTen is the reciprocal of 10^41, where binvTen = round(2^(2 * lnFracBits) / 10^41).
var binvTen = mustParseBint("394020061963944792122790401001436138050797392704654466679482934042457217715")

// bexp16 is a cache of powers of e used for argument reduction,
// where bexp16[x] = round(exp(x/16) * 2^lnFracBits).
var bexp16 = [...]*bint{
	mustParseBint("6277101735386680763835789423207666416102355444464034512896"),
	mustParseBint("6681940015382801178271792642492595790711866080030890280697"),
	mustParseBint("7112888121196508253682244550202254052270114968331364009341"),
	mustParseBint("7571629991916346798465651547438821442842581257720189718296"),
	mustParseBint("8059958171371227951818126787375099076842964917831539239691"),
	mustParseBint("8579780812534395024844671050952416483692552531675591157516"),
	mustParseBint("9133129133672633160701668318815661493442582174397409844094"),
	mustParseBint("9722165355375787882595496116111671906914486357041126637042"),
	mustParseBint("10349191149480707855612639538487286292672956784895387715418"),
	mustParseBint("11016656632903965775121572098118692639249137110889949424271"),
	mustParseBint("11727169941526954186611565144271709116639637086938056822753"),
	mustParseBint("12483507421543520302190779699450747865279296439559064438628"),
}

// bexp1024 is a cache of powers of e used for argument reduction,
// where bexp1024[x] = round(exp(x/1024) * 2^lnFracBits).
var bexp1024 = [...]*bint{
	mustParseBint("6277101735386680763835789423207666416102355444464034512896"),
	mustParseBint("6283234711680069918928567613631902034170513129183962944548"),
	mustParseBint("6289373680133503769605165692546443132278161427480117681421"),
	mustParseBint("6295518646601559324413600009363694211231870220617862781983"),
	mustParseBint("6301669616944533744850632184703709376778305367825537830622"),
	mustParseBint("6307826597028449934177102697421308081628917045868865145251"),
	mustParseBint("6313989592725062131693757768370808930632608470002546133015"),
	mustParseBint("6320158609911861512482904658507369964607504819161589607993"),
	mustParseBint("6326333654472081792621235711544514736508651760521094231181"),
	mustParseBint("6332514732294704839869166689100950568065306373121833188426"),
	mustParseBint("6338701849274466289842040169081224625546033796610270661149"),
	mustParseBint("6344895011311861167668550005949095741426979856256105136209"),
	mustParseBint("6351094224313149515141748084574710962471758591137514865035"),
	mustParseBint("6357299494190362023367999837471756223356856607729971335335"),
	mustParseBint("6363510826861305670919260238493695803783380013962033024302"),
	mustParseBint("6369728228249569367494047234434025682070061685869459496826"),
	mustParseBint("6375951704284529603092494829479146791134766447675505602251"),
	mustParseBint("6382181260901356102710873296099025645472062165697114243672"),
	mustParseBint("6388416904041017486560969249735266867249306140993955938496"),
	mustParseBint("6394658639650286935819723593563594722879129375422995401796"),
	mustParseBint("6400906473681747863914530613673053719955023328187845568936"),
	mustParseBint("6407160412093799593349606784222521349290671125821224803383"),
	mustParseBint("6413420460850663038078843126511413843270068913183708143359"),
	mustParseBint("6419686625922386391430560255440797934138750657596074146154"),
	mustParseBint("6425958913284850819589590541548542532454118209286689561620"),
	mustParseBint("6432237328919776160642117116682703433546512953150872776568"),
	mustParseBint("6438521878814726629188704756436085958893248453372020533181"),
	mustParseBint("6444812568963116526530962982706934148456048962257937015422"),
	mustParseBint("6451109405364215956437287045181014981093515374997440697549"),
	mustParseBint("6457412394023156546493127761154071302913929704178523343445"),
	mustParseBint("6463721540950937175041246518935781830458082148963106604100"),
	mustParseBint("6470036852164429703717417081102069867112721095974532489234"),
	mustParseBint("6476358333686384715587041160096928289850042500738946859115"),
	mustParseBint("6482685991545437258888150080132965959294744887569579673761"),
	mustParseBint("6489019831776112596386270186006723981448288357104280272164"),
	mustParseBint("6495359860418831960346635011335558183402153508834229380160"),
	mustParseBint("6501706083519918313129232575842640719204242333969892602992"),
	mustParseBint("6508058507131602113412181543670507843632858486879506640978"),
	mustParseBint("6514417137312027088048935342296686520552599597807965061311"),
	mustParseBint("6520781980125256009564818714462388604868563476772529654769"),
	mustParseBint("6527153041641276479298406553612191790354808810782609342823"),
	mustParseBint("6533530327936006716193260256684160293594604483887760767024"),
	mustParseBint("6539913845091301351245542216691129301898867086141809375391"),
	mustParseBint("6546303599194957227613034471400024521407293962601872520161"),
	mustParseBint("6552699596340719206391092923552255712643678116250558064442"),
	mustParseBint("6559101842628285978061073952479559912495722101388549112780"),
	mustParseBint("6565510344163315879616775646661330162617169780642400682112"),
	mustParseBint("6571925107057432717374441301746608079012189153511410113938"),
	mustParseBint("6578346137428231595471878248831707631093817396906052536579"),
	mustParseBint("6584773441399284750062250503348042221399174907431705438783"),
	mustParseBint("6591207025100147389208109155779321790467131359595067985944"),
	mustParseBint("6597646894666363538481229861598050491630557328282094075728"),
	mustParseBint("6604093056239471892273832229293372824494287838628727643574"),
	mustParseBint("6610545515967011670826761352160976385812370802342503164407"),
	mustParseBint("6617004280002528482980217181646157064745828649046134854815"),
	mustParseBint("6623469354505580194652622897478487123681459319094523154311"),
	mustParseBint("6629940745641742803053228892616002793692931091001652591469"),
	mustParseBint("6636418459582616316634054459133655488224246823154301842378"),
	mustParseBint("6642902502505830640786774734650164302664473637796988015387"),
	mustParseBint("6649392880595051469290165947694587019221828671855827760511"),
	mustParseBint("6655889600039986181513727484574117374001706445194509586401"),
	mustParseBint("6662392667036389745383104789823047969583453102043410398353"),
	mustParseBint("6668902087786070626112942607194746144828375413566422210825"),
	mustParseBint("6675417868496896700712803568409114672300358557471409418532"),
}

// bln16 is a cache of logarithms used for argument reduction,
// where bln16[x] = round(ln(1 + x/16) * 2^lnFracBits).
var bln16 = [...]*bint{
	mustParseBint("0"),
	mustParseBint("380546918811104376948409517351555779880567136396458141078"),
	mustParseBint("739336097517795880505266504821725180218627987938932619608"),
	mustParseBint("1078721545980979560918563104604714200081958947766431639090"),
	mustParseBint("1400694773194772906941746467053012168950389224798956667313"),
	mustParseBint("1706955597372515585296642989618144614808722135357452842293"),
	mustParseBint("1998966468244517059555333284141775893160188215769088407869"),
	mustParseBint("2277994704218894841745712647957536130356004575474584344508"),
	mustParseBint("2545145733744506767491414797523259672791486442307534182339"),
	mustParseBint("2801389546389545813883492934106024337900778449597913334627"),
	mustParseBint("3047581952987111054958238113855334540540811664872874395570"),
	mustParseBint("3284481831262302647996681302344984853010114430246466801947"),
	mustParseBint("3512765233599226472282791282319679107381580589726054405024"),
	mustParseBint("3733037018083559048254539972441640677572818328213184428689"),
	mustParseBint("3945840506939279674433161264576271841741875667106490849652"),
	mustParseBint("4151665560684497459373085265267378978866639598822849360671"),
}

// bln1024 is a cache of logarithms used for argument reduction,
// where bln1024[x] = round(ln(1 + x/1024) * 2^lnFracBits).
var bln1024 = [...]*bint{
	mustParseBint("0"),
	mustParseBint("6126990955353017956687363141772283079499635150052871856"),
	mustParseBint("12248007272064554437681316724904887727728481336762695967"),
	mustParseBint("18363060590933558103182934828837585762727009976424542927"),
	mustParseBint("24472162518771232710088978762068865197285724479879796773"),
	mustParseBint("30575324628533220907099373807914498843193521457290162201"),
	mustParseBint("36672558459451146048971757675617874856082210225762229413"),
	mustParseBint("42763875517163515767804944083085936687667321373756192569"),
	mustParseBint("48849287273845991013866989440210346396382790777374274472"),
	mustParseBint("54928805168341024253313936923577555392238797905714347756"),
	mustParseBint("61002440606286870485170528793591526667252214016157754926"),
	mustParseBint("67070204960245974715162529607479314160676891536337886360"),
	mustParseBint("73132109569832739499400111399053768604485146878418884895"),
	mustParseBint("79188165741840676146511354496364114071796747976560629421"),
	mustParseBint("85238384750368943142612668022824524085446989649435021702"),
	mustParseBint("91282777836948275339477201721155498410121409319227559308"),
	mustParseBint("97321356210666307422421490683552544126855992995695317814"),
	mustParseBint("103354131048292295150773047517098806105498049215111033404"),
	mustParseBint("109381113494401237840305808427968914939751077198834285745"),
	mustParseBint("115402314661497405533734681873022907721784193489249128466"),
	mustParseBint("121417745630137274282243387038571692427101419475981303228"),
	mustParseBint("127427417449051872938079765574708096828191491658340969202"),
	mustParseBint("133431341135268544835488279570116400679322095768838151920"),
	mustParseBint("139429527674232127714658962091635364310250426543504145930"),
	mustParseBint("145421988019925555220954168541508385775410847505747173927"),
	mustParseBint("151408733094989883289427606669985150552544095397790326262"),
	mustParseBint("157389773790843744702572833499374787345155688279230431850"),
	mustParseBint("163365120967802235087329245808523310760526687156558828770"),
	mustParseBint("169334785455195233595631118151758063263200799836906249676"),
	mustParseBint("175298778051485161491208033273990735613722744074603889716"),
	mustParseBint("181257109524384181843931692394140593382861838543171756680"),
	mustParseBint("187209790610970843511753188711296836859298154752757371375"),
	mustParseBint("193156832017806172569184991440241400587329761400486845138"),
	mustParseBint("199098244421049214320351747614503320270210641612215324810"),
	mustParseBint("205034038466572029013862205690278095347664254273032943946"),
	mustParseBint("210964224770074144356139752367612237398701767221573108157"),
	mustParseBint("216888813917196467899389898447234782168716434222543324195"),
	mustParseBint("222807816463634662360078229968242583465420113688657188804"),
	mustParseBint("228721242935251986903640548774051439801664008487853585803"),
	mustParseBint("234629103828191607411146865797916970798797872219655742633"),
	mustParseBint("240531409608988378723791296699599142099194640816350299044"),
	mustParseBint("246428170714680101841379471038520292291851263562998985573"),
	mustParseBint("252319397552918259031432542891082485719532850425013932799"),
	mustParseBint("258205100502078229786121034480477223527315941501085140985"),
	mustParseBint("264085289911368990544981318449182510425857448239533411703"),
	mustParseBint("269959976100942301082251323902897208137095202294158667871"),
	mustParseBint("275829169362001380438688822779036472299128596189914890578"),
	mustParseBint("281692879956909075258904214268165902116430817385309420640"),
	mustParseBint("287551118119295523376548884987507325780635169586639899824"),
	mustParseBint("293403894054165315471148801521015338795722885365548908561"),
	mustParseBint("299251217938004157601959820940312001531124205372836610493"),
	mustParseBint("305093099918885037405945126022975011505674798067753426761"),
	mustParseBint("310929550116573896728835057874226028223560421962796159064"),
	mustParseBint("316760578622634813440224292976791006207186191362932939461"),
	mustParseBint("322586195500534695165789668331545777194659601868925727777"),
	mustParseBint("328406410785747487651972881738988147489284506417069387707"),
	mustParseBint("334221234485857900460864679173202856596158758369287724190"),
	mustParseBint("340030676580664652675549892609351763863068573721653691566"),
	mustParseBint("345834747022283241278824724701266773724821250044799184414"),
	mustParseBint("351633455735248234850977916507893907591059441164261715449"),
	mustParseBint("357426812616615095215234816096842293305085888623260007092"),
	mustParseBint("363214827536061529642496834190481924469919619995588942869"),
	mustParseBint("368997510335988376210167282665392437820422248574182510293"),
	mustParseBint("374774870831620024893137106891699641214042008996620310211"),
}

// bpool is a cache of reusable *big.Int instances.
var bpool = sync.Pool{
	New: func() any {
		return (*bint)(new(big.Int))
	},
}

// getBint obtains a *big.Int from the pool.
func getBint() *bint {
	return bpool.Get().(*bint)
}

// putBint returns the *big.Int into the pool.
func putBint(b *bint) {
	bpool.Put(b)
}

func (z *bint) sign() int {
	return (*big.Int)(z).Sign()
}

func (z *bint) cmp(x *bint) int {
	return (*big.Int)(z).Cmp((*big.Int)(x))
}

func (z *bint) string() string {
	return (*big.Int)(z).String()
}

func (z *bint) setBint(x *bint) {
	(*big.Int)(z).Set((*big.Int)(x))
}

func (z *bint) setInt64(x int64) {
	(*big.Int)(z).SetInt64(x)
}

func (z *bint) setBytes(buf []byte) {
	(*big.Int)(z).SetBytes(buf)
}

func (z *bint) setFint(x fint) {
	(*big.Int)(z).SetUint64(uint64(x))
}

// fint converts *big.Int to uint64.
// If z cannot be represented as uint64, the result is undefined.
func (z *bint) fint() fint {
	f := (*big.Int)(z).Uint64()
	return fint(f)
}

// add calculates z = x + y.
func (z *bint) add(x, y *bint) {
	(*big.Int)(z).Add((*big.Int)(x), (*big.Int)(y))
}

// inc calcualtes z = x + 1.
func (z *bint) inc(x *bint) {
	y := bpow10[0]
	z.add(x, y)
}

// sub calculates z = x - y.
func (z *bint) sub(x, y *bint) {
	(*big.Int)(z).Sub((*big.Int)(x), (*big.Int)(y))
}

// neg calculates z = -x.
func (z *bint) neg(x *bint) {
	(*big.Int)(z).Neg((*big.Int)(x))
}

// subAbs calculates z = |x - y|.
func (z *bint) subAbs(x, y *bint) {
	switch x.cmp(y) {
	case 1:
		z.sub(x, y)
	default:
		z.sub(y, x)
	}
}

// dbl (Double) calculates z = x * 2.
func (z *bint) dbl(x *bint) {
	(*big.Int)(z).Lsh((*big.Int)(x), 1)
}

// hlf (Half) calculates z = ⌊x / 2⌋.
func (z *bint) hlf(x *bint) {
	(*big.Int)(z).Rsh((*big.Int)(x), 1)
}

// mul calculates z = x * y.
func (z *bint) mul(x, y *bint) {
	// Copying x, y to prevent heap allocations.
	if z == x {
		b := getBint()
		defer putBint(b)
		b.setBint(x)
		x = b
	}
	if z == y {
		b := getBint()
		defer putBint(b)
		b.setBint(y)
		y = b
	}
	(*big.Int)(z).Mul((*big.Int)(x), (*big.Int)(y))
}

// pow calculates z = x^y.
// If y is negative, the result is unpredictable.
func (z *bint) pow(x, y *bint) {
	(*big.Int)(z).Exp((*big.Int)(x), (*big.Int)(y), nil)
}

// pow10 calculates z = 10^power.
// If power is negative, the result is unpredictable.
func (z *bint) pow10(power int) {
	x := getBint()
	defer putBint(x)
	x.setInt64(10)
	y := getBint()
	defer putBint(y)
	y.setInt64(int64(power))
	z.pow(x, y)
}

// quoRem calculates z = ⌊x / y⌋, r = x - y * z.
func (z *bint) quoRem(x, y, r *bint) {
	(*big.Int)(z).QuoRem((*big.Int)(x), (*big.Int)(y), (*big.Int)(r))
}

// quo calculates z = ⌊x / y⌋.
func (z *bint) quo(x, y *bint) {
	// Passing r to prevent heap allocations.
	r := getBint()
	defer putBint(r)
	z.quoRem(x, y, r)
}

func (z *bint) isOdd() bool {
	return (*big.Int)(z).Bit(0) != 0
}

// lsh (Left Shift) calculates z = x * 10^shift.
func (z *bint) lsh(x *bint, shift int) {
	var y *bint
	if shift < len(bpow10) {
		y = bpow10[shift]
	} else {
		y = getBint()
		defer putBint(y)
		y.pow10(shift)
	}
	z.mul(x, y)
}

// fsa (Fused Shift and Addition) calculates z = x * 10^shift + f.
func (z *bint) fsa(x *bint, shift int, f fint) {
	y := getBint()
	defer putBint(y)
	y.setFint(f)
	z.lsh(x, shift)
	z.add(z, y)
}

// rshDown (Right Shift) calculates z = ⌊x / 10^shift⌋ and rounds
// result towards zero.
func (z *bint) rshDown(x *bint, shift int) {
	// Special cases
	switch {
	case x.sign() == 0:
		z.setFint(0)
		return
	case shift <= 0:
		z.setBint(x)
		return
	}
	// General case
	var y *bint
	if shift < len(bpow10) {
		y = bpow10[shift]
	} else {
		y = getBint()
		defer putBint(y)
		y.pow10(shift)
	}
	z.quo(x, y)
}

// rshHalfEven (Right Shift) calculates z = round(x / 10^shift) and
// rounds result using "half to even" rule.
func (z *bint) rshHalfEven(x *bint, shift int) {
	// Special cases
	switch {
	case x.sign() == 0:
		z.setFint(0)
		return
	case shift <= 0:
		z.setBint(x)
		return
	}
	// General case
	var y, r *bint
	r = getBint()
	defer putBint(r)
	if shift < len(bpow10) {
		y = bpow10[shift]
	} else {
		y = getBint()
		defer putBint(y)
		y.pow10(shift)
	}
	z.quoRem(x, y, r)
	r.dbl(r) // r = r * 2
	switch y.cmp(r) {
	case -1:
		z.inc(z) // z = z + 1
	case 0:
		// half-to-even
		if z.isOdd() {
			z.inc(z) // z = z + 1
		}
	}
}

// prec returns length of z in decimal digits.
// prec assumes that 0 has no digits.
// If z is negative, the result is unpredictable.
//
// z.prec() is significantly faster than len(z.string()),
// if z has less than len(bpow10) digits.
func (z *bint) prec() int {
	// Special case
	if z.cmp(bpow10[len(bpow10)-1]) > 0 {
		return len(z.string())
	}
	// General case
	left, right := 0, len(bpow10)
	for left < right {
		mid := (left + right) / 2
		if z.cmp(bpow10[mid]) < 0 {
			right = mid
		} else {
			left = mid + 1
		}
	}
	return left
}

// hasPrec checks if z has a given number of digits or more.
// hasPrec assumes that 0 has no digits.
// If z is negative, the result is unpredictable.
//
// z.hasPrec(p) is significantly faster than z.prec() >= p,
// if z has no more than len(bpow10) digits.
func (z *bint) hasPrec(prec int) bool {
	// Special cases
	switch {
	case prec < 1:
		return true
	case prec > len(bpow10):
		return len(z.string()) >= prec
	}
	// General case
	return z.cmp(bpow10[prec-1]) >= 0
}
