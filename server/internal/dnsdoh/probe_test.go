package dnsdoh

import (
	"errors"
	"net"
	"testing"
)

func TestIsSinkholed(t *testing.T) {
	tests := []struct {
		name   string
		lookup func(string) ([]net.IP, error)
		want   bool
	}{
		{
			name:   "loopback v4",
			lookup: stubLookup([]net.IP{net.IPv4(127, 0, 0, 1)}, nil),
			want:   true,
		},
		{
			name:   "loopback v6",
			lookup: stubLookup([]net.IP{net.IPv6loopback}, nil),
			want:   true,
		},
		{
			name:   "unspecified v4",
			lookup: stubLookup([]net.IP{net.IPv4zero}, nil),
			want:   true,
		},
		{
			name:   "unspecified v6",
			lookup: stubLookup([]net.IP{net.IPv6unspecified}, nil),
			want:   true,
		},
		{
			name:   "mixed (any loopback → sinkhole)",
			lookup: stubLookup([]net.IP{net.IPv4(192, 0, 2, 1), net.IPv4(127, 0, 0, 1)}, nil),
			want:   true,
		},
		{
			name:   "routable v4 only",
			lookup: stubLookup([]net.IP{net.IPv4(138, 199, 36, 11)}, nil),
			want:   false,
		},
		{
			name:   "multiple routable",
			lookup: stubLookup([]net.IP{net.IPv4(65, 9, 62, 20), net.IPv4(65, 9, 62, 11)}, nil),
			want:   false,
		},
		{
			name:   "resolver error",
			lookup: stubLookup(nil, errors.New("no such host")),
			want:   false,
		},
		{
			name:   "empty answer",
			lookup: stubLookup(nil, nil),
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSinkholedWith("image.tmdb.org", tc.lookup); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func stubLookup(ips []net.IP, err error) func(string) ([]net.IP, error) {
	return func(string) ([]net.IP, error) { return ips, err }
}
