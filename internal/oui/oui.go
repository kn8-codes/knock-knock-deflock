// Package oui resolves MAC OUI prefixes to vendor names using an embedded
// copy of the Wireshark manuf file. No network calls are made at any time.
//
// To refresh the embedded manuf file:
//
//	go generate ./internal/oui/
package oui

import (
	_ "embed"
	"bufio"
	"bytes"
	"strings"
)

//go:generate sh -c "curl -fsSL https://www.wireshark.org/download/automated/data/manuf -o manuf/manuf"

//go:embed manuf/manuf
var manufData []byte

// DB is a compiled OUI lookup table. It is safe for concurrent use.
type DB struct {
	table map[string]string // "AA:BB:CC" → vendor name
}

// Load parses the embedded Wireshark manuf file and returns a ready DB.
func Load() (*DB, error) {
	return parse(manufData)
}

// LoadBytes parses a manuf-format byte slice. Used in tests to inject
// custom vendor data without touching the embedded file.
func LoadBytes(data []byte) (*DB, error) {
	return parse(data)
}

func parse(data []byte) (*DB, error) {
	db := &DB{table: make(map[string]string, 40000)}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		oui := normalizeOUI(fields[0])
		if oui == "" {
			continue
		}
		name := fields[len(fields)-1] // prefer long name (field 2); fall back to short (field 1)
		db.table[oui] = strings.TrimSpace(name)
	}
	return db, sc.Err()
}

// Lookup returns the vendor name for a MAC address, or "" if unknown.
// Locally-administered and multicast MACs always return "".
func (db *DB) Lookup(mac string) string {
	if mac == "" {
		return ""
	}
	first, ok := firstOctet(mac)
	if !ok {
		return ""
	}
	// locally administered (bit 1) or multicast (bit 0) → no vendor
	if first&0x02 != 0 || first&0x01 != 0 {
		return ""
	}
	oui := normalizeOUI(mac)
	if oui == "" {
		return ""
	}
	return db.table[oui]
}

// normalizeOUI returns the upper-cased "AA:BB:CC" prefix from a MAC or OUI
// string, or "" if the input is too short to parse.
func normalizeOUI(s string) string {
	// strip any trailing mask (e.g. "00:50:C2:00:00:00/28")
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	// collect hex digits, skip separators
	var digits [6]byte
	n := 0
	for i := 0; i < len(s) && n < 6; i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits[n] = c
			n++
		case c >= 'A' && c <= 'F':
			digits[n] = c
			n++
		case c == ':' || c == '-' || c == '.':
			// separator — skip
		default:
			// unexpected character; stop
			break
		}
	}
	if n < 6 {
		return ""
	}
	return string([]byte{
		digits[0], digits[1], ':', digits[2], digits[3], ':', digits[4], digits[5],
	})
}

func firstOctet(mac string) (byte, bool) {
	mac = strings.ToUpper(strings.TrimSpace(mac))
	digits := 0
	var hi, lo byte
	for i := 0; i < len(mac); i++ {
		c := mac[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		case c == ':' || c == '-':
			continue
		default:
			return 0, false
		}
		if digits == 0 {
			hi = v
		} else if digits == 1 {
			lo = v
			return hi<<4 | lo, true
		}
		digits++
	}
	return 0, false
}
