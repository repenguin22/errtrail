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
	// Wrap(nil) returns a nil *Error, so chained builder calls are safe.
	var noErr error
	fmt.Println(errtrail.Wrap(noErr, "layer") == nil)

	// But never return Wrap(err, ...) unconditionally from a function
	// declared to return error: a nil *Error stored in an error interface
	// is a non-nil error. Guard with if err != nil instead.
	broken := func() error {
		return errtrail.Wrap(noErr, "layer") // wrong when noErr is nil
	}
	fmt.Println(broken() == nil)
	// Output:
	// true
	// false
}
