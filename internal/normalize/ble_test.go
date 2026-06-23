package normalize

import (
	"strings"
	"testing"

	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

func emptyDB(t *testing.T) *oui.DB {
	t.Helper()
	db, err := oui.LoadBytes([]byte{})
	if err != nil {
		t.Fatalf("oui.LoadBytes empty: %v", err)
	}
	return db
}

// --- bleMAC ---

func TestBLEMACReversal(t *testing.T) {
	// HCI delivers address bytes little-endian (LSB first).
	// bleMAC must reverse them so the output is MSB-first (standard notation).
	addr := [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	got := bleMAC(addr)
	want := "66:55:44:33:22:11"
	if got != want {
		t.Errorf("bleMAC = %q, want %q", got, want)
	}
}

func TestBLEMACAllZero(t *testing.T) {
	addr := [6]byte{}
	got := bleMAC(addr)
	if got != "00:00:00:00:00:00" {
		t.Errorf("bleMAC(zero) = %q, want \"00:00:00:00:00:00\"", got)
	}
}

// --- parseADStructures ---

func TestADCompleteName(t *testing.T) {
	// AD type 0x09 (Complete Local Name) = "iPhone"
	name := "iPhone"
	data := append([]byte{byte(len(name) + 1), 0x09}, []byte(name)...)
	gotName, gotMfr := parseADStructures(data)
	if gotName != name {
		t.Errorf("ble_name = %q, want %q", gotName, name)
	}
	if gotMfr != "" {
		t.Errorf("ble_mfr = %q, want empty", gotMfr)
	}
}

func TestADShortenedName(t *testing.T) {
	// AD type 0x08 (Shortened Local Name)
	name := "Mac"
	data := append([]byte{byte(len(name) + 1), 0x08}, []byte(name)...)
	gotName, _ := parseADStructures(data)
	if gotName != name {
		t.Errorf("ble_name from 0x08 = %q, want %q", gotName, name)
	}
}

func TestADCompleteNameWinsOverShortened(t *testing.T) {
	// When both 0x08 and 0x09 appear, 0x09 must win (FR-021).
	// Build AD: 0x08 "Short" then 0x09 "Complete"
	short := "Short"
	complete := "Complete"
	data := append([]byte{byte(len(short) + 1), 0x08}, []byte(short)...)
	data = append(data, byte(len(complete)+1), 0x09)
	data = append(data, []byte(complete)...)
	gotName, _ := parseADStructures(data)
	if gotName != complete {
		t.Errorf("ble_name = %q, want %q (complete should win)", gotName, complete)
	}
}

func TestADNoName(t *testing.T) {
	// Only manufacturer data, no name fields.
	data := []byte{0x05, 0xFF, 0x4C, 0x00, 0x10, 0x05}
	gotName, _ := parseADStructures(data)
	if gotName != "" {
		t.Errorf("ble_name = %q, want empty (AC-021: no MAC fallback)", gotName)
	}
}

func TestADManufacturerApple(t *testing.T) {
	// Company ID 0x004C (Apple) in little-endian = [0x4C, 0x00].
	data := []byte{0x05, 0xFF, 0x4C, 0x00, 0x10, 0x05}
	_, gotMfr := parseADStructures(data)
	if gotMfr != "0x004C" {
		t.Errorf("ble_mfr = %q, want \"0x004C\"", gotMfr)
	}
}

func TestADManufacturerMicrosoft(t *testing.T) {
	// Company ID 0x0006 (Microsoft) in little-endian = [0x06, 0x00].
	data := []byte{0x03, 0xFF, 0x06, 0x00}
	_, gotMfr := parseADStructures(data)
	if gotMfr != "0x0006" {
		t.Errorf("ble_mfr = %q, want \"0x0006\"", gotMfr)
	}
}

func TestADManufacturerTooShort(t *testing.T) {
	// 0xFF with only 1 data byte — must not panic, mfr stays empty.
	data := []byte{0x02, 0xFF, 0x4C}
	_, gotMfr := parseADStructures(data)
	if gotMfr != "" {
		t.Errorf("ble_mfr = %q, want empty for too-short mfr data", gotMfr)
	}
}

func TestADMalformedTruncated(t *testing.T) {
	// Length byte says 10 bytes but there are only 3 — must not panic.
	data := []byte{0x0A, 0x09, 'A'}
	gotName, _ := parseADStructures(data)
	if gotName != "" {
		t.Errorf("ble_name = %q, want empty for truncated AD", gotName)
	}
}

func TestADZeroLengthTerminator(t *testing.T) {
	// length=0 is the list terminator, must stop parsing.
	name := "X"
	data := append([]byte{byte(len(name) + 1), 0x09}, []byte(name)...)
	data = append(data, 0x00)                          // terminator
	data = append(data, 0x05, 0xFF, 0x4C, 0x00, 0x01) // should be ignored
	gotName, gotMfr := parseADStructures(data)
	if gotName != name {
		t.Errorf("ble_name = %q, want %q", gotName, name)
	}
	if gotMfr != "" {
		t.Errorf("ble_mfr = %q, want empty (past terminator)", gotMfr)
	}
}

func TestADEmpty(t *testing.T) {
	gotName, gotMfr := parseADStructures([]byte{})
	if gotName != "" || gotMfr != "" {
		t.Errorf("empty AD: ble_name=%q ble_mfr=%q, both want empty", gotName, gotMfr)
	}
}

// --- ParseBLEReport ---

func TestParseBLEReportFields(t *testing.T) {
	addr := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	name := "Sensor"
	ad := append([]byte{byte(len(name) + 1), 0x09}, []byte(name)...)
	ad = append(ad, 0x05, 0xFF, 0x4C, 0x00, 0x01, 0x00)

	db := emptyDB(t)
	ev := ParseBLEReport(addr, 0x00, ad, -72, "sess-X", "node-Y", "hci0", db)
	if ev == nil {
		t.Fatal("ParseBLEReport returned nil")
	}
	if ev.Type != "ble_adv" {
		t.Errorf("Type = %q, want \"ble_adv\"", ev.Type)
	}
	if ev.MAC != "FF:EE:DD:CC:BB:AA" {
		t.Errorf("MAC = %q, want \"FF:EE:DD:CC:BB:AA\"", ev.MAC)
	}
	if ev.BLEName != name {
		t.Errorf("BLEName = %q, want %q", ev.BLEName, name)
	}
	if ev.BLEMfr != "0x004C" {
		t.Errorf("BLEMfr = %q, want \"0x004C\"", ev.BLEMfr)
	}
	if ev.RSSI != -72 {
		t.Errorf("RSSI = %d, want -72", ev.RSSI)
	}
	if ev.SessionID != "sess-X" || ev.NodeID != "node-Y" || ev.IfaceID != "hci0" {
		t.Errorf("session/node/iface mismatch: %q %q %q", ev.SessionID, ev.NodeID, ev.IfaceID)
	}
	if ev.Lat != 0.0 || ev.Lon != 0.0 {
		t.Errorf("GPS fields should be zero in v0")
	}
	if ev.SSID != "" || len(ev.ProbeSSIDs) != 0 {
		t.Errorf("WiFi fields should be empty on BLE event")
	}
}

func TestParseBLEReportNoNameNeverMAC(t *testing.T) {
	// When no name AD is present, BLEName must be "" — not the MAC (AC-021).
	addr := [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	ad := []byte{0x03, 0xFF, 0x06, 0x00} // only mfr, no name
	db := emptyDB(t)
	ev := ParseBLEReport(addr, 0x01, ad, -50, "s", "n", "hci0", db)
	if ev == nil {
		t.Fatal("nil event")
	}
	if ev.BLEName != "" {
		t.Errorf("BLEName = %q, must be empty when no name AD (no MAC fallback)", ev.BLEName)
	}
	if strings.Contains(ev.BLEName, ":") {
		t.Error("BLEName contains colon — MAC was substituted (violates FR-021)")
	}
}
