package errtrail

import (
	"net/http"
	"strconv"
)

// Code はエラーの分類。値 0–16 は gRPC の codes.Code と同一の意味・数値を持つ。
// HTTP ステータスや gRPC ステータスは Code から変換テーブルで導出する。
type Code uint32

// 組み込みコード。0–16 は gRPC の codes.Code に一致する。
const (
	OK                 Code = 0 // エラーなし。Error に設定することは想定しない。
	Canceled           Code = 1
	Unknown            Code = 2
	InvalidArgument    Code = 3
	DeadlineExceeded   Code = 4
	NotFound           Code = 5
	AlreadyExists      Code = 6
	PermissionDenied   Code = 7
	ResourceExhausted  Code = 8
	FailedPrecondition Code = 9
	Aborted            Code = 10
	OutOfRange         Code = 11
	Unimplemented      Code = 12
	Internal           Code = 13
	Unavailable        Code = 14
	DataLoss           Code = 15
	Unauthenticated    Code = 16
)

// customCodeMin はカスタムコードの下限。0–99 は組み込み用に予約する。
const customCodeMin Code = 100

// codeInfo は 1 つの Code のメタデータ。
type codeInfo struct {
	name       string
	httpStatus int
	grpcCode   uint32
}

// codes は Code から codeInfo への参照テーブル。
//
// このテーブルは init での組み込み投入と、Register による起動前の追記のみを
// 想定しており、並行書き込みに対する保護は持たない。読み取り(String/HTTPStatus/
// GRPCCode)はサーバ起動後に多数の goroutine から行われるが、起動前に書き込みが
// 完了していれば安全である。
var codes = map[Code]codeInfo{
	OK:                 {"OK", http.StatusOK, 0},
	Canceled:           {"CANCELED", 499, 1},
	Unknown:            {"UNKNOWN", http.StatusInternalServerError, 2},
	InvalidArgument:    {"INVALID_ARGUMENT", http.StatusBadRequest, 3},
	DeadlineExceeded:   {"DEADLINE_EXCEEDED", http.StatusGatewayTimeout, 4},
	NotFound:           {"NOT_FOUND", http.StatusNotFound, 5},
	AlreadyExists:      {"ALREADY_EXISTS", http.StatusConflict, 6},
	PermissionDenied:   {"PERMISSION_DENIED", http.StatusForbidden, 7},
	ResourceExhausted:  {"RESOURCE_EXHAUSTED", http.StatusTooManyRequests, 8},
	FailedPrecondition: {"FAILED_PRECONDITION", http.StatusBadRequest, 9},
	Aborted:            {"ABORTED", http.StatusConflict, 10},
	OutOfRange:         {"OUT_OF_RANGE", http.StatusBadRequest, 11},
	Unimplemented:      {"UNIMPLEMENTED", http.StatusNotImplemented, 12},
	Internal:           {"INTERNAL", http.StatusInternalServerError, 13},
	Unavailable:        {"UNAVAILABLE", http.StatusServiceUnavailable, 14},
	DataLoss:           {"DATA_LOSS", http.StatusInternalServerError, 15},
	Unauthenticated:    {"UNAUTHENTICATED", http.StatusUnauthorized, 16},
}

// Register はカスタムコードを登録する。init 関数内、またはサーバ起動前に呼ぶこと。
// 起動後の呼び出しは他 goroutine の Code 読み取りとデータレースを起こす。
//
// c が customCodeMin(100)未満、または既に登録済みの場合は panic する。
func Register(c Code, name string, httpStatus int, grpcCode uint32) {
	if c < customCodeMin {
		panic("errtrail: custom code must be >= 100, got " + strconv.FormatUint(uint64(c), 10))
	}
	if _, ok := codes[c]; ok {
		panic("errtrail: code already registered: " + strconv.FormatUint(uint64(c), 10))
	}
	codes[c] = codeInfo{name: name, httpStatus: httpStatus, grpcCode: grpcCode}
}

// String はコード名を返す。組み込みは "NOT_FOUND" 形式、未登録のカスタムコードは
// "CODE(123)" 形式。
func (c Code) String() string {
	if info, ok := codes[c]; ok {
		return info.name
	}
	return "CODE(" + strconv.FormatUint(uint64(c), 10) + ")"
}

// HTTPStatus は対応する HTTP ステータスコードを返す。未登録コードは 500。
func (c Code) HTTPStatus() int {
	if info, ok := codes[c]; ok {
		return info.httpStatus
	}
	return http.StatusInternalServerError
}

// GRPCCode は対応する gRPC コードの数値を返す。0–16 は恒等、カスタムコードは
// Register で指定された値、未登録は 2 (UNKNOWN)。grpc パッケージに依存しないよう
// uint32 で返す。
func (c Code) GRPCCode() uint32 {
	if info, ok := codes[c]; ok {
		return info.grpcCode
	}
	return 2 // UNKNOWN
}
