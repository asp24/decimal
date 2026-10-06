module github.com/asp24/decimal/bench

go 1.27.0

require (
	github.com/alpacahq/alpacadecimal v0.0.9
	github.com/asp24/decimal v0.0.0
	github.com/cockroachdb/apd/v3 v3.2.3
	github.com/ericlagergren/decimal v0.0.0-20240411145413-00de7ca16731
	github.com/quagmt/udecimal v1.10.1
	github.com/shopspring/decimal v1.5.0
	github.com/woodsbury/decimal128 v1.5.0
)

replace github.com/asp24/decimal => ../
