package grpcerr

// Wire-level round-trip tests: a real grpc.Server and ClientConn over an
// in-memory bufconn listener, so ErrorInfo details actually cross the
// transport (proto-serialized into the grpc-status-details-bin trailer) —
// the path the in-process round-trip tests in grpcerr_test.go never touch.

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/repenguin22/errtrail"
)

// E2E fixture: a retryable custom code, registered once so -count=2 doesn't
// panic. Distinct from rateLimited, which is registered without Retryable.
const e2eLimited errtrail.Code = 140

var registerE2EOnce sync.Once

func registerE2ELimited() {
	registerE2EOnce.Do(func() {
		errtrail.Register(e2eLimited, "E2E_LIMITED", 429, 8, errtrail.Retryable())
	})
}

// e2eServer's single method returns whatever error the test injected.
type e2eServer struct {
	err error
}

// e2eServiceDesc registers the Fail method without any protoc-generated
// code: HandlerType (*any)(nil) satisfies RegisterService's Implements
// check for every concrete type, and emptypb.Empty is the wire message.
var e2eServiceDesc = grpc.ServiceDesc{
	ServiceName: "errtrail.e2e.Test",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Fail",
		Handler: func(srv any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
			if err := dec(new(emptypb.Empty)); err != nil {
				return nil, err
			}
			if err := srv.(*e2eServer).err; err != nil {
				return nil, err
			}
			return new(emptypb.Empty), nil
		},
	}},
	Streams: []grpc.StreamDesc{},
}

// dialE2E starts a real gRPC server over bufconn whose Fail method returns
// injected, and returns a connected client. Torn down via t.Cleanup.
func dialE2E(t *testing.T, injected error) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	s.RegisterService(&e2eServiceDesc, &e2eServer{err: injected})
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func callFail(conn *grpc.ClientConn) error {
	return conn.Invoke(context.Background(), "/errtrail.e2e.Test/Fail",
		new(emptypb.Empty), new(emptypb.Empty))
}

// The headline "same taxonomy end to end" flow across a real transport:
// the custom code survives as an ErrorInfo detail in the
// grpc-status-details-bin trailer and is recovered by FromError.
func TestE2ECustomCodeRoundTripOverWire(t *testing.T) {
	registerE2ELimited()
	setDomain(t, "errtrail.test")

	conn := dialE2E(t, ToError(
		errtrail.New(e2eLimited, "bucket empty").WithPublic("Too many requests")))

	wireErr := callFail(conn)
	if wireErr == nil {
		t.Fatal("expected an error over the wire")
	}
	got := FromError(wireErr)
	if code := errtrail.CodeOf(got); code != e2eLimited {
		t.Errorf("CodeOf = %v, want E2E_LIMITED (recovered from the wire ErrorInfo)", code)
	}
	if !errtrail.IsRetryable(got) {
		t.Error("IsRetryable = false, want true via the recovered code")
	}
	if !strings.Contains(got.Error(), "Too many requests") {
		t.Errorf("Error() = %q, want the wire message preserved", got.Error())
	}
}

func TestE2EBuiltinCodeNameFallbackOverWire(t *testing.T) {
	setDomain(t, "errtrail.test")
	conn := dialE2E(t, ToError(errtrail.New(errtrail.Unavailable, "down")))

	wireErr := callFail(conn)
	st, ok := status.FromError(wireErr)
	if !ok {
		t.Fatal("not a status error")
	}
	// The v0.4.0 code-name fallback must survive the transport.
	if st.Message() != "UNAVAILABLE" {
		t.Errorf("wire message = %q, want UNAVAILABLE", st.Message())
	}
	got := FromError(wireErr)
	if code := errtrail.CodeOf(got); code != errtrail.Unavailable {
		t.Errorf("CodeOf = %v, want Unavailable", code)
	}
	if !errtrail.IsRetryable(got) {
		t.Error("IsRetryable = false, want true")
	}
}

func TestE2ECustomCodeDegradesWithoutDomain(t *testing.T) {
	registerE2ELimited()
	setDomain(t, "") // no ErrorInfo on the wire: only the numeric code survives

	conn := dialE2E(t, ToError(errtrail.New(e2eLimited, "bucket empty")))

	got := FromError(callFail(conn))
	if code := errtrail.CodeOf(got); code != errtrail.ResourceExhausted {
		t.Errorf("CodeOf = %v, want ResourceExhausted (numeric-only wire)", code)
	}
}

// E2E fixture for the v1.1 details: a custom code with a registered retry
// delay, registered once so -count=2 doesn't panic.
const e2eThrottled errtrail.Code = 141

var registerE2EThrottledOnce sync.Once

func registerE2EThrottled() {
	registerE2EThrottledOnce.Do(func() {
		errtrail.Register(e2eThrottled, "E2E_THROTTLED", 429, 8,
			errtrail.RetryAfter(3*time.Second))
	})
}

func TestE2EAllDetailsOverWire(t *testing.T) {
	registerE2EThrottled()
	setDomain(t, "errtrail.test")

	conn := dialE2E(t, ToError(
		errtrail.New(e2eThrottled, "bucket empty").
			WithFieldViolation("query", "too broad")))

	wireErr := callFail(conn)
	st, ok := status.FromError(wireErr)
	if !ok {
		t.Fatal("not a status error")
	}
	// All three details survive the grpc-status-details-bin trailer:
	// ErrorInfo, RetryInfo, BadRequest, in attach order.
	details := st.Details()
	if len(details) != 3 {
		t.Fatalf("len(Details) = %d, want 3", len(details))
	}
	if code := errtrail.CodeOf(FromError(wireErr)); code != e2eThrottled {
		t.Errorf("CodeOf = %v, want E2E_THROTTLED (ErrorInfo recovery)", code)
	}
	if d, ok := RetryDelay(wireErr); !ok || d != 3*time.Second {
		t.Errorf("RetryDelay = %v, %v; want 3s, true", d, ok)
	}
	br, ok := details[2].(*errdetails.BadRequest)
	if !ok {
		t.Fatalf("details[2] = %T, want *errdetails.BadRequest", details[2])
	}
	if vs := br.GetFieldViolations(); len(vs) != 1 || vs[0].GetField() != "query" {
		t.Errorf("FieldViolations = %v", br.GetFieldViolations())
	}
}

func TestE2ESuccessPath(t *testing.T) {
	conn := dialE2E(t, nil)
	if err := callFail(conn); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}
