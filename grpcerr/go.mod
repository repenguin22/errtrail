module github.com/repenguin22/errtrail/grpcerr

go 1.25.0

replace github.com/repenguin22/errtrail => ../

require (
	github.com/repenguin22/errtrail v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.82.0
)

require (
	golang.org/x/sys v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
