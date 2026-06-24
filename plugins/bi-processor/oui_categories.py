"""
Static lookup tables for AP vendor tier and BLE device classification.

AP tier: matched against the vendor string already resolved by kkd-leaf's
OUI lookup (the leaf embeds the full Wireshark manuf file, so vendor strings
are authoritative — no need to re-lookup MAC prefixes here).

BLE category: matched first against ble_mfr (Bluetooth SIG company ID in
"0x004C" format), then against ble_name/vendor name substrings as fallback.

To add a vendor or BLE category: edit the relevant tuple or frozenset below.
No logic changes needed elsewhere.
"""
from __future__ import annotations

import re

# ── AP vendor tiers ───────────────────────────────────────────────────────────
# Substrings matched against ap.vendor (lowercased).

MANAGED_VENDOR_PATS: tuple[str, ...] = (
    "ubiquiti", "unifi",            # Ubiquiti / UniFi line
    "meraki",                       # Cisco Meraki
    "cisco",                        # Cisco enterprise APs
    "aruba",                        # Aruba (HPE)
    "ruckus",                       # Ruckus (CommScope)
    "extreme networks",             # Extreme Networks
    "fortinet wireless",            # FortiAP
    "mist",                         # Juniper Mist
    "aerohive",                     # Aerohive (Extreme)
)

SEMI_VENDOR_PATS: tuple[str, ...] = (
    "omada",            # TP-Link Omada
    "eero",             # Amazon Eero Pro
    "orbi",             # Netgear Orbi (prosumer mesh)
    "insight",          # Netgear Insight managed
    "amplifi",          # Ubiquiti AmpliFi (prosumer)
    "deco",             # TP-Link Deco (prosumer)
)

CONSUMER_VENDOR_PATS: tuple[str, ...] = (
    "netgear", "linksys", "asus", "belkin",
    "tp-link", "tplink", "d-link", "dlink",
    "zyxel", "tenda", "buffalo", "actiontec",
)

# SSID patterns that suggest unconfigured / OEM factory firmware.
DEFAULT_SSID_RE = re.compile(
    r"^(NETGEAR|Linksys|ASUS_|ASUS |BelkinN|belkin\.|"
    r"xfinitywifi|ATT|XFINITY|FRITZ!Box|dlink|D-Link|"
    r"HUAWEI-|default|HOME-[0-9A-F]{4})",
    re.IGNORECASE,
)


def ap_vendor_tier(vendor: str, ssid: str = "") -> tuple[str, int]:
    """Return (tier_name, score) for an AP.

    tier_name values: managed | semi | consumer | default | unknown
    """
    v = vendor.lower()
    if any(p in v for p in MANAGED_VENDOR_PATS):
        return "managed", 30
    if any(p in v for p in SEMI_VENDOR_PATS):
        return "semi", 18
    if any(p in v for p in CONSUMER_VENDOR_PATS):
        return "consumer", 8
    if v:
        # Known vendor string but not in any tier — treat as consumer-like.
        return "consumer", 8
    if ssid and DEFAULT_SSID_RE.match(ssid):
        return "default", 2
    return "unknown", 2


# ── BLE device categories ─────────────────────────────────────────────────────
# Bluetooth SIG company IDs (ble_mfr field = "0x004C" format, uppercase).
# Reference: https://www.bluetooth.com/specifications/assigned-numbers/

# [confirmed] = cross-referenced against the official SIG registry.
POS_BLE_MFRS: frozenset[str] = frozenset({
    "0x0082",   # Square, Inc. [confirmed]
})

INDUSTRIAL_BLE_MFRS: frozenset[str] = frozenset({
    "0x01DB",   # Zebra Technologies [confirmed]
    "0x0689",   # Honeywell [confirmed]
})

MEDICAL_BLE_MFRS: frozenset[str] = frozenset({
    "0x0201",   # Masimo [confirmed]
    "0x0060",   # Nonin Medical [confirmed]
    "0x00A5",   # Bioventus / cardiac monitor vendors
})

CONSUMER_BLE_MFRS: frozenset[str] = frozenset({
    "0x004C",   # Apple Inc. [confirmed]
    "0x0075",   # Samsung Electronics [confirmed]
    "0x00E0",   # Google [confirmed]
    "0x0006",   # Microsoft [confirmed]
    "0x004F",   # Sound United (Bose/Denon) [confirmed]
})

# Name/vendor substring fallback (ble_name + vendor, lowercased).
# Applied when ble_mfr is absent or not in the lookup tables above.
POS_NAME_PATS: tuple[str, ...] = (
    "square", "clover", "verifone", "ingenico", "pax ", "toast",
)
INDUSTRIAL_NAME_PATS: tuple[str, ...] = (
    "zebra", "honeywell", "brother", "dymo", "bixolon", "datalogic",
)
MEDICAL_NAME_PATS: tuple[str, ...] = (
    "philips", "masimo", "welch allyn", "natus", "nihon kohden",
    "ge healthcare", "spacelabs",
)
CONSUMER_NAME_PATS: tuple[str, ...] = (
    "iphone", "ipad", "macbook", "airpods",
    "galaxy", "pixel", "surface",
)


def ble_device_type(ble_mfr: str, ble_name: str, vendor: str) -> tuple[str, int]:
    """Return (device_type, score) for a BLE device.

    device_type values: pos_terminal | industrial | medical | consumer | unknown | none
    """
    mfr = (ble_mfr or "").strip()
    combined = f"{ble_name} {vendor}".lower()

    if mfr in POS_BLE_MFRS or any(p in combined for p in POS_NAME_PATS):
        return "pos_terminal", 20
    if mfr in INDUSTRIAL_BLE_MFRS or any(p in combined for p in INDUSTRIAL_NAME_PATS):
        return "industrial", 18
    if mfr in MEDICAL_BLE_MFRS or any(p in combined for p in MEDICAL_NAME_PATS):
        return "medical", 15
    if mfr in CONSUMER_BLE_MFRS or any(p in combined for p in CONSUMER_NAME_PATS):
        return "consumer", 3
    if ble_mfr or ble_name:
        return "unknown", 1
    return "none", 0
