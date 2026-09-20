module github.com/Andrew1996-la/stellar-works/payment

go 1.26.0

require google.golang.org/grpc v1.79.2

require go.opentelemetry.io/otel/sdk v1.42.0 // indirect

require (
	github.com/Andrew1996-la/stellar-works/shared v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/Andrew1996-la/stellar-works/shared => ../shared
