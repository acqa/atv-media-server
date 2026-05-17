package dnsdoh

import "net"

// IsSinkholed reports whether the system resolver answers host with a sinkhole
// address (loopback or unspecified). main.go calls this once at startup to
// decide whether to wire the metadata/poster clients through the DoH dialer or
// to leave them on the default DNS path.
//
// A resolver error is treated as "not sinkholed" — a transient DNS hiccup
// should not silently flip the runtime to DoH; if DNS is genuinely broken the
// regular dial path will surface that itself with a clearer error than a
// guessed-at workaround would.
//
// Any loopback or unspecified address in the answer set is enough to trip the
// flag; a mixed [sinkhole, real] response is still treated as compromised
// because the Go default resolver does not guarantee the order in which
// addresses are tried, and a sinkhole attempt would fail with
// "connect: connection refused" before the real address is even tried.
func IsSinkholed(host string) bool {
	return isSinkholedWith(host, net.LookupIP)
}

func isSinkholedWith(host string, lookup func(string) ([]net.IP, error)) bool {
	ips, err := lookup(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return true
		}
	}
	return false
}
