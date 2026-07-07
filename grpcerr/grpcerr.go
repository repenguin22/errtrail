// Package grpcerr は errtrail のエラーを gRPC の *status.Status へ変換する。
// google.golang.org/grpc に依存するため、コアの errtrail とは別モジュールに分離して
// いる。gRPC を使わない利用者はこのパッケージを import しなければ grpc 依存を負わない。
package grpcerr

import (
	"github.com/repenguin22/errtrail"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToStatus は err を *status.Status へ変換する。
//
//	Code    = codes.Code(errtrail.CodeOf(err).GRPCCode())
//	Message = errtrail.PublicMessage(err)
//
// err == nil のときは status.New(codes.OK, "") を返す。内部メッセージ・attrs・trace
// は含めない。
func ToStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}
	c := codes.Code(errtrail.CodeOf(err).GRPCCode())
	return status.New(c, errtrail.PublicMessage(err))
}

// ToError は ToStatus(err).Err() を返す。gRPC ハンドラの return 用。
// err == nil なら nil を返す。
func ToError(err error) error {
	if err == nil {
		return nil
	}
	return ToStatus(err).Err()
}
