package errtrail_test

import (
	"errors"
	"fmt"

	"github.com/repenguin22/errtrail"
)

func ExampleNew() {
	err := errtrail.New(errtrail.NotFound, "user 42 missing").WithPublic("User not found")

	fmt.Println(errtrail.CodeOf(err))              // classification
	fmt.Println(errtrail.CodeOf(err).HTTPStatus()) // -> HTTP
	fmt.Println(errtrail.PublicMessage(err))       // for the client
	// Output:
	// NOT_FOUND
	// 404
	// User not found
}

func ExampleWrap() {
	base := errors.New("sql: no rows in result set")

	// Attach a code at the source; middle layers wrap to add context.
	repoErr := errtrail.Wrap(base, "query user").WithCode(errtrail.NotFound)
	svcErr := errtrail.Wrap(repoErr, "load profile")

	fmt.Println(svcErr.Error())          // concatenated message
	fmt.Println(errtrail.CodeOf(svcErr)) // inherits the inner code
	fmt.Println(errors.Is(svcErr, base))
	// Output:
	// load profile: query user: sql: no rows in result set
	// NOT_FOUND
	// true
}

func ExampleWrap_nil() {
	// Wrap(nil) returns nil, so callers can skip the if err != nil check.
	var noErr error
	fmt.Println(errtrail.Wrap(noErr, "layer") == nil)
	// Output:
	// true
}
