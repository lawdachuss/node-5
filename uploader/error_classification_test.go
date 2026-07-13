package uploader

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsProxyError_GenuineProxyOnly(t *testing.T) {
	genuine := []error{
		errors.New("dial tcp: connection refused"),
		errors.New("actively refused by target machine"),
		errors.New("socks: unknown code 0"),
		errors.New("proxy SOCKS5 handshake failed"),
	}
	for _, err := range genuine {
		if !IsProxyError(err) {
			t.Errorf("expected proxy error for %q", err)
		}
	}

	// A plain transient TCP drop on a DIRECT connection (StreamWish/VidHide use
	// newDirectClient, no proxy) must NOT be treated as a proxy error, or the
	// retry loop will permanently skip the host on the first blip.
	transient := []error{
		errors.New(`Post "https://x.cdn/upload/01": use of closed network connection`),
		errors.New("wsasend: An existing connection was forcibly closed"),
		errors.New("Patch \"https://x/upload/abc\": EOF"),
		errors.New("connection reset by peer"),
	}
	for _, err := range transient {
		if IsProxyError(err) {
			t.Errorf("transient error %q must NOT be a proxy error", err)
		}
	}
}

func TestIsTransientNetworkError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New(`Post "https://x.cdn/upload/01": use of closed network connection`), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("dial tcp 1.2.3.4:443: i/o timeout"), true},
		{errors.New("wsasend: forcibly closed"), true},
		{errors.New("EOF"), true},
		{fmt.Errorf("tus upload chunk at offset 0: Patch %q: EOF", "https://x/upload/abc"), true},
		{errors.New("dial tcp: connection refused"), false},
		{errors.New("socks: unknown code"), false},
		{errors.New("daily limit reached"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := IsTransientNetworkError(c.err); got != c.want {
			t.Errorf("IsTransientNetworkError(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}
