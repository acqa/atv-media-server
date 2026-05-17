package dnsdoh

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildQuery_Roundtrip(t *testing.T) {
	q, err := buildQuery("image.tmdb.org")
	if err != nil {
		t.Fatalf("buildQuery: %v", err)
	}
	if len(q) < 12 {
		t.Fatalf("query too short: %d bytes", len(q))
	}
	if got := binary.BigEndian.Uint16(q[2:]); got != 0x0100 {
		t.Errorf("flags = 0x%04x, want 0x0100 (RD)", got)
	}
	if got := binary.BigEndian.Uint16(q[4:]); got != 1 {
		t.Errorf("QDCOUNT = %d, want 1", got)
	}
	// QTYPE/QCLASS live at the end: A=1, IN=1.
	qtype := binary.BigEndian.Uint16(q[len(q)-4:])
	qclass := binary.BigEndian.Uint16(q[len(q)-2:])
	if qtype != 1 || qclass != 1 {
		t.Errorf("QTYPE/QCLASS = %d/%d, want 1/1", qtype, qclass)
	}
	// Walk the QNAME and confirm it ends at the right offset.
	off, err := skipName(q, 12)
	if err != nil {
		t.Fatalf("skipName: %v", err)
	}
	if off+4 != len(q) {
		t.Errorf("QNAME ends at %d, want %d", off, len(q)-4)
	}
}

func TestBuildQuery_RejectsBadLabels(t *testing.T) {
	cases := []string{
		"empty..label.org",
		"way-too-long-label-" + string(make([]byte, 64)) + ".org",
	}
	for _, name := range cases {
		if _, err := buildQuery(name); err == nil {
			t.Errorf("buildQuery(%q) — expected error", name)
		}
	}
}

func TestParseAnswers_Roundtrip(t *testing.T) {
	q, _ := buildQuery("api.themoviedb.org")
	want := []net.IP{net.IPv4(65, 9, 62, 20), net.IPv4(65, 9, 62, 11)}
	resp := answer(t, q, want, 300)

	ips, ttl, err := parseAnswers(resp)
	if err != nil {
		t.Fatalf("parseAnswers: %v", err)
	}
	if len(ips) != len(want) {
		t.Fatalf("got %d IPs, want %d (ips=%v)", len(ips), len(want), ips)
	}
	for i, ip := range ips {
		if !ip.Equal(want[i]) {
			t.Errorf("ip[%d] = %s, want %s", i, ip, want[i])
		}
	}
	if ttl.Seconds() != 300 {
		t.Errorf("ttl = %s, want 300s", ttl)
	}
}

func TestParseAnswers_RejectsTruncated(t *testing.T) {
	if _, _, err := parseAnswers([]byte{0, 0}); err == nil {
		t.Error("expected error for truncated message")
	}
}

func TestParseAnswers_RejectsNonZeroRcode(t *testing.T) {
	// 12-byte header with rcode=3 (NXDOMAIN).
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[2:], 0x8183)
	if _, _, err := parseAnswers(msg); err == nil {
		t.Error("expected error for NXDOMAIN")
	}
}
