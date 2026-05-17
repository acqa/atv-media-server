package dnsdoh

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeDoH spins up a httptest.Server that speaks wire-format DoH. The handler
// returns the configured A records for any incoming query, ignoring the
// question name (sufficient for tests, which fix the name to a single host).
type fakeDoH struct {
	server   *httptest.Server
	calls    atomic.Int32 // request counter — useful for cache-hit assertions
	handler  http.HandlerFunc
	respFunc func(query []byte) (status int, body []byte)
}

// newFakeDoH constructs a server. respFunc gets the raw query bytes and
// returns the HTTP status and response bytes. Tests can override respFunc to
// simulate failures, sinkhole answers, slow responses, etc.
func newFakeDoH(t *testing.T, respFunc func(query []byte) (int, []byte)) *fakeDoH {
	t.Helper()
	f := &fakeDoH{respFunc: respFunc}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		status, resp := f.respFunc(body)
		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(f.server.Close)
	return f
}

// URL returns the DoH endpoint URL to plug into Config.Providers.
func (f *fakeDoH) URL() string { return f.server.URL }

// answer builds a wire-format response containing the given A records,
// echoing the question from the query.
func answer(t *testing.T, query []byte, ips []net.IP, ttl uint32) []byte {
	t.Helper()
	if len(query) < 12 {
		t.Fatalf("query too short: %d bytes", len(query))
	}
	var buf bytes.Buffer

	// Header: echo ID, set QR=1, RD=1, RA=1; QDCOUNT=1, ANCOUNT=len(ips)
	header := make([]byte, 12)
	copy(header[0:2], query[0:2])
	binary.BigEndian.PutUint16(header[2:], 0x8180)
	binary.BigEndian.PutUint16(header[4:], 1)
	binary.BigEndian.PutUint16(header[6:], uint16(len(ips)))
	buf.Write(header)

	// Question: copy from query (name + QTYPE + QCLASS).
	qEnd, err := skipName(query, 12)
	if err != nil {
		t.Fatalf("malformed query: %v", err)
	}
	qEnd += 4
	if qEnd > len(query) {
		t.Fatalf("question overruns query")
	}
	buf.Write(query[12:qEnd])

	// Answers: NAME=pointer to offset 12 (the question name), TYPE=A, CLASS=IN.
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			t.Fatalf("not an IPv4 address: %s", ip)
		}
		buf.WriteByte(0xC0)
		buf.WriteByte(0x0C)
		_ = binary.Write(&buf, binary.BigEndian, uint16(1))
		_ = binary.Write(&buf, binary.BigEndian, uint16(1))
		_ = binary.Write(&buf, binary.BigEndian, ttl)
		_ = binary.Write(&buf, binary.BigEndian, uint16(4))
		buf.Write(v4)
	}
	return buf.Bytes()
}
