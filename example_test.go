package errtrail_test

import (
	"errors"
	"fmt"

	"github.com/repenguin22/errtrail"
)

func ExampleNew() {
	err := errtrail.New(errtrail.NotFound, "user 42 missing").WithPublic("User not found")

	fmt.Println(errtrail.CodeOf(err))              // 分類
	fmt.Println(errtrail.CodeOf(err).HTTPStatus()) // HTTP へ
	fmt.Println(errtrail.PublicMessage(err))       // クライアント向け
	// Output:
	// NOT_FOUND
	// 404
	// User not found
}

func ExampleWrap() {
	base := errors.New("sql: no rows in result set")

	// 発生源でコードを付け、中間層はラップして文脈を足す。
	repoErr := errtrail.Wrap(base, "query user").WithCode(errtrail.NotFound)
	svcErr := errtrail.Wrap(repoErr, "load profile")

	fmt.Println(svcErr.Error())          // 連結メッセージ
	fmt.Println(errtrail.CodeOf(svcErr)) // 内側のコードを引き継ぐ
	fmt.Println(errors.Is(svcErr, base))
	// Output:
	// load profile: query user: sql: no rows in result set
	// NOT_FOUND
	// true
}

func ExampleWrap_nil() {
	// Wrap(nil) は nil を返すため、if err != nil を省ける。
	var noErr error
	fmt.Println(errtrail.Wrap(noErr, "layer") == nil)
	// Output:
	// true
}
