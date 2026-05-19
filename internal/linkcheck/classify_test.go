package linkcheck

import (
	"errors"
	"net"
	"testing"
)

// fakeTimeoutErr implements net.Error with Timeout() returning true so we
// can exercise the timeout branch of classify without standing up a real
// connection.
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string   { return "i/o timeout" }
func (fakeTimeoutErr) Timeout() bool   { return true }
func (fakeTimeoutErr) Temporary() bool { return true }

func TestClassify_TimeoutBranch(t *testing.T) {
	var r Result
	classify(&r, fakeTimeoutErr{})
	if r.Bucket != BucketBroken || r.ErrorType != "timeout" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/timeout", r.Bucket, r.ErrorType)
	}
}

func TestClassify_DNSBranch(t *testing.T) {
	var r Result
	classify(&r, &net.DNSError{Err: "no such host", Name: "nope.invalid"})
	if r.Bucket != BucketBroken || r.ErrorType != "dns" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/dns", r.Bucket, r.ErrorType)
	}
}

func TestClassify_TLSBranch(t *testing.T) {
	var r Result
	classify(&r, errors.New("x509: certificate signed by unknown authority"))
	if r.Bucket != BucketBroken || r.ErrorType != "tls" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/tls", r.Bucket, r.ErrorType)
	}
}

func TestClassify_ConnRefusedBranch(t *testing.T) {
	var r Result
	classify(&r, errors.New("dial tcp 127.0.0.1:1: connect: connection refused"))
	if r.Bucket != BucketBroken || r.ErrorType != "connection_refused" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/connection_refused", r.Bucket, r.ErrorType)
	}
}

func TestClassify_NetworkDefaultBranch(t *testing.T) {
	var r Result
	classify(&r, errors.New("something else weird"))
	if r.Bucket != BucketBroken || r.ErrorType != "network" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/network", r.Bucket, r.ErrorType)
	}
}

func TestClassify_RedirectLoopBranch(t *testing.T) {
	var r Result
	classify(&r, errRedirectLoop)
	if r.Bucket != BucketBroken || r.ErrorType != "redirect_loop" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/redirect_loop", r.Bucket, r.ErrorType)
	}
}

func TestClassify_EmptyStatusChainNoErr(t *testing.T) {
	r := Result{}
	classify(&r, nil)
	if r.Bucket != BucketBroken || r.ErrorType != "network" || r.Detail != "no response" {
		t.Fatalf("got %+v, want Broken/network/no response", r)
	}
}

func TestClassify_GenericHTTPBucket(t *testing.T) {
	r := Result{StatusChain: []int{418}}
	classify(&r, nil)
	if r.Bucket != BucketBroken || r.ErrorType != "http_418" {
		t.Fatalf("got bucket=%v error_type=%q, want Broken/http_418", r.Bucket, r.ErrorType)
	}
}
