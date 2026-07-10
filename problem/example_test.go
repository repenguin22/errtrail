package problem_test

import (
	"fmt"
	"net/http/httptest"

	"github.com/repenguin22/errtrail"
	"github.com/repenguin22/errtrail/problem"
)

func ExampleWrite() {
	// At the source: attach validation details as typed field violations —
	// they surface here as the "errors" extension member, and the same
	// error yields an errdetails.BadRequest over gRPC (grpcerr.ToStatus).
	// Attrs (With) stay internal; extra structured data can still go
	// through WithPublicField under its own key.
	err := errtrail.New(errtrail.InvalidArgument, "email failed regexp check").
		WithPublic("Validation failed").
		WithFieldViolation("email", "must be a valid email address")

	// At the boundary: instance comes from the request, not the error.
	rec := httptest.NewRecorder()
	_ = problem.Write(rec, err, problem.Instance("/users"))
	fmt.Println(rec.Body.String())
	// Output:
	// {"code":"INVALID_ARGUMENT","detail":"Validation failed","errors":[{"field":"email","description":"must be a valid email address"}],"instance":"/users","status":400,"title":"Bad Request"}
}
