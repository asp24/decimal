# decimal

[![githubb]][github]
[![codecovb]][codecov]
[![godocb]][godoc]
[![licenseb]][license]
[![versionb]][version]

Package decimal implements correctly rounded decimal floating-point numbers for Go.
This package is designed specifically for use in transactional financial systems.

This is a maintained fork of [govalues/decimal], which is no longer developed.
To migrate, replace the `github.com/govalues/decimal` import path with `github.com/asp24/decimal`.

## Key Features

- **BSON, JSON, XML, SQL** - Implements the necessary interfaces for direct compatibility
  with the [mongo-driver/bson], [encoding/json], [encoding/xml], and [database/sql] packages.
- **No Heap Allocations** - Addition, subtraction, multiplication, division,
  square root and parsing avoid heap allocations, preventing garbage collector impact.
- **Correct Rounding** - For all methods, the result is the one that would
  be obtained if the true mathematical value were rounded to 19 digits of
  precision using the [half-to-even] rounding (a.k.a. "banker's rounding").
- **No Panics** - All methods are panic-free, returning errors instead of crashing
  your application in cases such as overflow or division by zero.
- **Immutability** - Once set, a decimal remains constant,
  ensuring safe concurrent access across goroutines.
- **Simple String Representation** - Decimals are represented in a straightforward
  format avoiding the complexities of scientific or engineering notations.
- **Rigorous Testing** - All methods are cross-validated against
  the [cockroachdb/apd] and [shopspring/decimal] packages through extensive [fuzz testing].

## Getting Started

### Installation

To add the decimal package to your Go workspace:

```bash
go get github.com/asp24/decimal
```

### Basic Usage

Create decimal values using one of the constructors.
After creating a decimal, you can perform various operations as shown below:

```go
package main

import (
    "fmt"
    "github.com/asp24/decimal"
)

func main() {
    // Constructors
    d, _ := decimal.New(8, 0)               // d = 8
    e, _ := decimal.Parse("12.5")           // e = 12.5
    f, _ := decimal.NewFromFloat64(2.567)   // f = 2.567
    g, _ := decimal.NewFromInt64(7, 896, 3) // g = 7.896

    // Arithmetic operations
    fmt.Println(d.Add(e))              // 8 + 12.5
    fmt.Println(d.Sub(e))              // 8 - 12.5
    fmt.Println(d.SubAbs(e))           // abs(8 - 12.5)

    fmt.Println(d.Mul(e))              // 8 * 12.5
    fmt.Println(d.AddMul(e, f))        // 8 + 12.5 * 2.567
    fmt.Println(d.SubMul(e, f))        // 8 - 12.5 * 2.567
    fmt.Println(d.PowInt(2))           // 8²

    fmt.Println(d.Quo(e))              // 8 / 12.5
    fmt.Println(d.AddQuo(e, f))        // 8 + 12.5 / 2.567
    fmt.Println(d.SubQuo(e, f))        // 8 - 12.5 / 2.567
    fmt.Println(d.QuoRem(e))           // 8 div 12.5, 8 mod 12.5
    fmt.Println(d.Inv())               // 1 / 8

    fmt.Println(decimal.Sum(d, e, f))  // 8 + 12.5 + 2.567
    fmt.Println(decimal.Mean(d, e, f)) // (8 + 12.5 + 2.567) / 3
    fmt.Println(decimal.Prod(d, e, f)) // 8 * 12.5 * 2.567

    // Transcendental functions
    fmt.Println(e.Sqrt())              // √12.5
    fmt.Println(e.Exp())               // exp(12.5)
    fmt.Println(e.Expm1())             // exp(12.5) - 1
    fmt.Println(e.Log())               // ln(12.5)
    fmt.Println(e.Log1p())             // ln(12.5 + 1)
    fmt.Println(e.Log2())              // log₂(12.5)
    fmt.Println(e.Log10())             // log₁₀(12.5)
    fmt.Println(e.Pow(d))              // 12.5⁸

    // Rounding to 2 decimal places
    fmt.Println(g.Round(2))            // 7.90
    fmt.Println(g.Ceil(2))             // 7.90
    fmt.Println(g.Floor(2))            // 7.89
    fmt.Println(g.Trunc(2))            // 7.89

    // Conversions
    fmt.Println(f.Int64(9))            // 2 567000000
    fmt.Println(f.Float64())           // 2.567
    fmt.Println(f.String())            // 2.567

    // Formatting
    fmt.Printf("%.2f", f)              // 2.57
    fmt.Printf("%.2k", f)              // 256.70%
}
```

## Documentation

For detailed documentation and additional examples, visit the package
[documentation](https://pkg.go.dev/github.com/asp24/decimal#section-documentation).
For examples related to financial calculations, see the `money` package
[documentation](https://pkg.go.dev/github.com/govalues/money#section-documentation).

## Comparison

Comparison with other popular packages:

| Feature              | decimal   | [apd] v3.2.3 | [shopspring] v1.5.0 | [udecimal] v1.10.1  | [alpacadecimal] v0.0.9    | [decimal128] v1.5.0 | [ericlagergren][^abandoned] |
| -------------------- | --------- | ------------ | ------------------- | ------------------- | ------------------------- | ------------------- | --------------------------- |
| Correctly Rounded    | Yes       | No           | No                  | No[^truncate]       | No[^halfup]               | Partly[^partly]     | No[^erl]                    |
| Precision            | 19 digits | Arbitrary    | Arbitrary           | 19 decimal places   | 12 decimal places[^fast]  | 34 digits           | Arbitrary                   |
| Heap Allocations     | No        | Medium       | High                | No                  | No[^fast]                 | No                  | Medium                      |
| Panic Free           | Yes       | Yes          | No[^divzero]        | Yes                 | No[^divzero]              | Yes[^nan]           | Yes                         |
| Mutability           | Immutable | Mutable      | Immutable           | Immutable           | Immutable                 | Immutable           | Mutable                     |
| Mathematical Context | Implicit  | Explicit     | Implicit            | Implicit            | Implicit                  | Implicit            | Explicit                    |
| Sqrt, Exp, Log       | Yes       | Yes          | Exp, Log            | Sqrt                | Exp                       | Yes                 | Yes                         |

[^truncate]: [udecimal] truncates the results of multiplication, division and
square root to 19 decimal places, the rounding mode cannot be changed.

[^halfup]: [alpacadecimal] rounds quotients half away from zero to a fixed number
of decimal places, the rounding mode cannot be changed.

[^partly]: [decimal128] correctly rounds addition, subtraction, multiplication and
division; mathematical functions are computed with extra precision, but without a guarantee.

[^erl]: [ericlagergren] occasionally rounds results of `Exp` and `Pow` incorrectly,
for example, exp(-1.28961) = 0.2753781596322221254 instead of 0.2753781596322221255.

[^fast]: [alpacadecimal] uses a fast allocation-free path for values with up to
12 decimal places and absolute value up to 9,223,372; other values fall back to [shopspring].

[^divzero]: Panics on division by zero.

[^nan]: [decimal128] returns NaN or infinity instead of errors.

[^abandoned]: [ericlagergren] has not been updated since April 2024.

### Benchmarks

Median time per operation (lower is better, the best result is in bold):

| Test Case | Expression            | decimal   | [apd] | [shopspring] | [udecimal] | [alpacadecimal] | [decimal128] | [ericlagergren] |
| --------- | --------------------- | --------: | ----: | -----------: | ---------: | --------------: | -----------: | --------------: |
| Add       | 5 + 6                 |  **3.5n** | 77.2n |         108n |      10.7n |            3.8n |        28.8n |            166n |
| Mul       | 2 * 3                 |  **3.6n** | 74.5n |        97.8n |      12.1n |            5.8n |        27.1n |            161n |
| Quo       | 2 / 4 (exact)         |     10.1n |  120n |         158n |      13.5n |        **7.0n** |        29.9n |            241n |
| Quo       | 2 / 3 (inexact)       | **11.0n** |  130n |         222n |      13.5n |            210n |        73.7n |            265n |
| PowInt    | 1.1^60                |      501n | 1.12µ |     **154n** |       397n |            749n |        12.2µ |            946n |
| PowInt    | 1.01^600              |     1.58µ | 3.63µ |    **1.08µ** |      2.94µ |           11.2µ |        19.2µ |           2.23µ |
| PowInt    | 1.001^6000            | **2.81µ** | 7.32µ |        42.7µ |      91.5µ |          343.1µ |        18.9µ |           3.78µ |
| Sqrt      | √2                    | **13.1n** | 1.20µ |            — |      38.6n |               — |        19.0µ |            764n |
| Exp       | exp(0.5)              |  **476n** | 15.0µ |        5.09µ |          — |           12.3µ |        10.8µ |           10.1µ |
| Log       | ln(0.5)               |  **710n** | 47.1µ |        33.0µ |          — |               — |         745n |           25.8µ |
| Parse     | 1                     |      3.8n | 41.9n |        27.6n |       5.3n |        **2.8n** |        17.4n |           80.9n |
| Parse     | 123.456               |      5.4n | 86.0n |        34.0n |       8.1n |        **4.6n** |        20.7n |           93.1n |
| Parse     | 123456789.1234567890  | **10.8n** | 95.0n |         172n |      17.2n |            195n |        27.6n |            138n |
| String    | 1                     |  **1.2n** |  8.4n |        38.1n |       7.6n |            1.4n |        18.9n |           79.4n |
| String    | 123.456               | **14.2n** | 20.5n |        67.0n |      20.8n |           20.0n |        42.5n |           89.9n |
| String    | 123456789.1234567890  | **26.3n** | 70.9n |        91.0n |      33.5n |           91.6n |        46.4n |            100n |
| Telco     | (see [specification]) | **24.2n** |  268n |         427n |      44.2n |            511n |         217n |           97.9n |

Heap allocations per operation:

| Test Case | Expression            | decimal | [apd] | [shopspring] | [udecimal] | [alpacadecimal] | [decimal128] | [ericlagergren] |
| --------- | --------------------- | ------: | ----: | -----------: | ---------: | --------------: | -----------: | --------------: |
| Add       | 5 + 6                 |       0 |     3 |            6 |          0 |               0 |            0 |               3 |
| Mul       | 2 * 3                 |       0 |     3 |            6 |          0 |               0 |            0 |               3 |
| Quo       | 2 / 4 (exact)         |       0 |     3 |            8 |          0 |               0 |            0 |               6 |
| Quo       | 2 / 3 (inexact)       |       0 |     3 |           11 |          0 |              10 |            0 |               7 |
| PowInt    | 1.1^60                |       2 |    12 |            5 |         12 |              12 |            0 |              15 |
| PowInt    | 1.01^600              |      16 |    58 |           10 |         22 |              16 |            0 |              39 |
| PowInt    | 1.001^6000            |      30 |   130 |           16 |         34 |              22 |            0 |              69 |
| Sqrt      | √2                    |       0 |     9 |            — |          0 |               — |            0 |              12 |
| Exp       | exp(0.5)              |       2 |   211 |          210 |          — |             281 |            0 |             130 |
| Log       | ln(0.5)               |       4 |   773 |          690 |          — |               — |            0 |             381 |
| Parse     | 1                     |       0 |     1 |            2 |          0 |               0 |            0 |               2 |
| Parse     | 123.456               |       0 |     2 |            2 |          0 |               0 |            0 |               2 |
| Parse     | 123456789.1234567890  |       0 |     2 |            4 |          0 |               5 |            0 |               2 |
| String    | 1                     |       0 |     0 |            1 |          0 |               0 |            1 |               4 |
| String    | 123.456               |       1 |     1 |            2 |          1 |               1 |            2 |               4 |
| String    | 123456789.1234567890  |       1 |     3 |            2 |          1 |               2 |            1 |               4 |
| Telco     | (see [specification]) |       0 |     0 |           20 |          0 |              27 |            0 |               0 |

Every package computes inexact results with at least 19 significant digits
(19 decimal places for [udecimal], [shopspring] and [alpacadecimal]), and
every result is verified against a reference value before measuring.
[shopspring] and [alpacadecimal] compute integer powers exactly, with all 61 digits of 1.1^60.
[decimal128] computes integer powers as exp(y·ln(x)).
The argument 0.5 of `Exp` is reduced exactly by the lookup tables of decimal;
for arbitrary arguments its `Exp` takes about 1µs.

The results were obtained with Go 1.27.1 on AMD Ryzen AI MAX+ 395, one physical core per benchmark.
To reproduce them, run:

```bash
cd bench
go test -bench . -count 10 > bench.txt
benchstat -col /mod bench.txt
```

The benchmark results shown in the table are provided for informational purposes only and may vary depending on your specific use case.

[codecov]: https://codecov.io/gh/asp24/decimal
[codecovb]: https://img.shields.io/codecov/c/github/asp24/decimal/main?color=brightcolor
[github]: https://github.com/asp24/decimal/actions/workflows/go.yml
[githubb]: https://img.shields.io/github/actions/workflow/status/asp24/decimal/go.yml
[godoc]: https://pkg.go.dev/github.com/asp24/decimal#section-documentation
[godocb]: https://img.shields.io/badge/go.dev-reference-blue
[version]: https://go.dev/dl
[versionb]: https://img.shields.io/github/go-mod/go-version/asp24/decimal?label=go
[license]: https://en.wikipedia.org/wiki/MIT_License
[licenseb]: https://img.shields.io/github/license/asp24/decimal?color=blue
[cockroachdb/apd]: https://pkg.go.dev/github.com/cockroachdb/apd
[shopspring/decimal]: https://pkg.go.dev/github.com/shopspring/decimal
[apd]: https://pkg.go.dev/github.com/cockroachdb/apd/v3
[shopspring]: https://pkg.go.dev/github.com/shopspring/decimal
[udecimal]: https://pkg.go.dev/github.com/quagmt/udecimal
[alpacadecimal]: https://pkg.go.dev/github.com/alpacahq/alpacadecimal
[decimal128]: https://pkg.go.dev/github.com/woodsbury/decimal128
[ericlagergren]: https://pkg.go.dev/github.com/ericlagergren/decimal
[mongo-driver/bson]: https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/bson#ValueUnmarshaler
[encoding/json]: https://pkg.go.dev/encoding/json#Unmarshaler
[encoding/xml]: https://pkg.go.dev/encoding#TextUnmarshaler
[database/sql]: https://pkg.go.dev/database/sql#Scanner
[specification]: https://speleotrove.com/decimal/telcoSpec.html
[fuzz testing]: https://github.com/govalues/decimal-tests
[govalues/decimal]: https://github.com/govalues/decimal
[half-to-even]: https://en.wikipedia.org/wiki/Rounding#Rounding_half_to_even
