package safehttp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestUnsafeURLs(t *testing.T) {
	for _, raw := range []string{"http://push.example/x", "https://localhost/x", "https://127.0.0.1/x", "https://[::1]/", "https://[::ffff:127.0.0.1]/", "https://169.254.169.254/", "https://10.1.2.3/", "https://172.17.0.1/", "https://192.168.2.1/", "https://100.64.0.1/", "https://[64:ff9b::7f00:1]/", "https://user:pass@push.example/x", "https://push.example:8443/x", "https://push.example/#x", "https://redis/", "https://push.example:bad/x"} {
		t.Run(raw, func(t *testing.T) {
			if ValidateURL(raw) == nil {
				t.Fatal("unsafe destination accepted")
			}
		})
	}
	if err := ValidateURL("https://push.example/subscription"); err != nil {
		t.Fatal(err)
	}
}

type fakeResolver []netip.Addr

func (r fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r, nil
}

type fakeDialer struct{ address string }

func (d *fakeDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.address = address
	return nil, errors.New("test dial")
}

func TestResolvedPrivateAddressNeverDialed(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1"}, {"1.1.1.1", "10.0.0.1"}, {"::ffff:192.168.1.1"}, {"fe80::1"}, {"fc00::1"}, {"fec0::1"}, {"3fff::1"}, {"198.18.0.1"}} {
		var ips fakeResolver
		for _, address := range addresses {
			ips = append(ips, netip.MustParseAddr(address))
		}
		d := &fakeDialer{}
		_, err := publicDial(ips, d)(t.Context(), "tcp", "push.example:443")
		if !errors.Is(err, ErrUnsafeDestination) || d.address != "" {
			t.Fatalf("private address dialed: %s, %v", d.address, err)
		}
	}
}

func TestDialPinsResolvedPublicIP(t *testing.T) {
	d := &fakeDialer{}
	_, _ = publicDial(fakeResolver{netip.MustParseAddr("1.1.1.1")}, d)(t.Context(), "tcp", "push.example:443")
	if d.address != "1.1.1.1:443" {
		t.Fatalf("dial destination = %s", d.address)
	}
}
