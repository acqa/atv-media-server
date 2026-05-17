package dnsdoh

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// buildQuery encodes a minimal RFC 1035 DNS query for an A record. The DoH
// transport (RFC 8484) carries this byte sequence verbatim with
// Content-Type: application/dns-message.
func buildQuery(name string) ([]byte, error) {
	var idBuf [2]byte
	if _, err := rand.Read(idBuf[:]); err != nil {
		return nil, err
	}
	id := binary.BigEndian.Uint16(idBuf[:])

	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:], id)
	binary.BigEndian.PutUint16(header[2:], 0x0100) // RD=1
	binary.BigEndian.PutUint16(header[4:], 1)      // QDCOUNT
	// ANCOUNT, NSCOUNT, ARCOUNT default to 0 (zeroed).

	buf := make([]byte, 0, 32+len(name))
	buf = append(buf, header...)

	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid label %q", label)
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	buf = append(buf, 0) // root label

	buf = append(buf, 0x00, 0x01) // QTYPE = A
	buf = append(buf, 0x00, 0x01) // QCLASS = IN
	return buf, nil
}

// parseAnswers walks a DNS response message and extracts every A record from
// the answer section, regardless of which name it is attached to. CNAME chains
// (e.g. image.tmdb.org → tmdb-image-prod.b-cdn.net → 138.x.x.x) resolve
// implicitly because each step is its own answer record. Returns the minimum
// TTL across the A records (clamped by the cache layer).
func parseAnswers(msg []byte) ([]net.IP, time.Duration, error) {
	if len(msg) < 12 {
		return nil, 0, errors.New("response too short")
	}
	flags := binary.BigEndian.Uint16(msg[2:])
	if rcode := int(flags & 0x000F); rcode != 0 {
		return nil, 0, fmt.Errorf("DNS rcode %d", rcode)
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	an := int(binary.BigEndian.Uint16(msg[6:]))

	off := 12
	for i := 0; i < qd; i++ {
		n, err := skipName(msg, off)
		if err != nil {
			return nil, 0, err
		}
		off = n + 4 // QTYPE + QCLASS
		if off > len(msg) {
			return nil, 0, errors.New("question truncated")
		}
	}

	var (
		ips    []net.IP
		minTTL uint32 = 0
	)
	for i := 0; i < an; i++ {
		n, err := skipName(msg, off)
		if err != nil {
			return nil, 0, err
		}
		off = n
		if off+10 > len(msg) {
			return nil, 0, errors.New("answer header truncated")
		}
		typ := binary.BigEndian.Uint16(msg[off:])
		ttl := binary.BigEndian.Uint32(msg[off+4:])
		rdLen := int(binary.BigEndian.Uint16(msg[off+8:]))
		off += 10
		if off+rdLen > len(msg) {
			return nil, 0, errors.New("rdata truncated")
		}
		if typ == 1 && rdLen == 4 {
			ips = append(ips, net.IPv4(msg[off], msg[off+1], msg[off+2], msg[off+3]).To4())
			if minTTL == 0 || ttl < minTTL {
				minTTL = ttl
			}
		}
		off += rdLen
	}
	return ips, time.Duration(minTTL) * time.Second, nil
}

// skipName advances past a (possibly compressed) DNS name and returns the
// offset of the next byte after the name. Handles three label types:
//   - 0x00            end of name
//   - 0x01..0x3F      length-prefixed label
//   - 0xC0..0xFF      compression pointer (2 bytes total)
func skipName(msg []byte, off int) (int, error) {
	for {
		if off >= len(msg) {
			return 0, errors.New("name overruns message")
		}
		b := msg[off]
		switch {
		case b == 0:
			return off + 1, nil
		case b&0xC0 == 0xC0:
			if off+1 >= len(msg) {
				return 0, errors.New("pointer truncated")
			}
			return off + 2, nil
		case b&0xC0 == 0:
			off += 1 + int(b)
		default:
			return 0, fmt.Errorf("invalid label byte 0x%02x", b)
		}
	}
}
