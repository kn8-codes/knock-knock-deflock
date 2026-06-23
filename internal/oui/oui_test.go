package oui

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.table) < 1000 {
		t.Fatalf("expected >1000 entries, got %d", len(db.table))
	}
}

func TestLookup(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		mac     string
		wantSub string // non-empty: vendor must contain this substring
		wantEmpty bool
	}{
		// Raspberry Pi Trading (AC-030)
		{"28:CD:C1:00:00:00", "Raspberry", false},
		// Unknown OUI (AC-031)
		{"02:00:00:00:00:00", "", true},
		// Locally administered MAC — bit 1 of first octet set (AC-032)
		{"02:AB:CD:01:02:03", "", true},
		// Multicast MAC — bit 0 of first octet set
		{"01:00:5E:00:00:01", "", true},
		// Apple Inc. well-known OUI
		{"00:17:F2:00:00:00", "Apple", false},
	}

	for _, tc := range cases {
		t.Run(tc.mac, func(t *testing.T) {
			got := db.Lookup(tc.mac)
			if tc.wantEmpty {
				if got != "" {
					t.Errorf("Lookup(%q) = %q, want empty", tc.mac, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("Lookup(%q) = %q, want substring %q", tc.mac, got, tc.wantSub)
			}
		})
	}
}

func TestNormalizeOUI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"b8:27:eb", "B8:27:EB"},
		{"B8-27-EB", "B8:27:EB"},
		{"b827eb", "B8:27:EB"},
		{"B8:27:EB:00:11:22", "B8:27:EB"},
		{"00:50:C2:00:00:00/28", "00:50:C2"}, // extended block with mask
		{"", ""},
		{"ZZ:ZZ:ZZ", ""},
	}
	for _, tc := range cases {
		got := normalizeOUI(tc.in)
		if got != tc.want {
			t.Errorf("normalizeOUI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
