module github.com/openfoundry/runtime

go 1.25.0

require (
	github.com/openfoundry/cel-evaluator v0.0.0
	github.com/vektah/gqlparser/v2 v2.5.30
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/google/uuid v1.6.0
	github.com/graph-gophers/graphql-go v1.8.0
	modernc.org/sqlite v1.57.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/cel-go v0.27.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.1 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/openfoundry/cel-evaluator => ../packages/cel-evaluator
