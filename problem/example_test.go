package problem_test

import (
	"fmt"
	"net/http/httptest"

	"github.com/repenguin22/errtrail"
	"github.com/repenguin22/errtrail/problem"
)

func ExampleWrite() {
	// At the source: mark validation details public via WithPublicField —
	// attrs (With) stay internal, public fields become extension members.
	err := errtrail.New(errtrail.InvalidArgument, "email failed regexp check").
		WithPublic("Validation failed").
		WithPublicField("errors", []map[string]string{
			{"detail": "must be a valid email address", "pointer": "#/email"},
		})

	// At the boundary: instance comes from the request, not the error.
	rec := httptest.NewRecorder()
	_ = problem.Write(rec, err, problem.Instance("/users"))
	fmt.Println(rec.Body.String())
	// Output:
	// {"code":"INVALID_ARGUMENT","detail":"Validation failed","errors":[{"detail":"must be a valid email address","pointer":"#/email"}],"instance":"/users","status":400,"title":"Bad Request"}
}
