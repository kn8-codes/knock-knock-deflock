"""
Scoring model. Four pure dimension functions: (BusinessRecord) -> int.

To add a new dimension:
  1. Write one function with signature (BusinessRecord) -> int.
  2. Add it to score_business() and include in the sum.
  No other files need changes.
"""
from __future__ import annotations

from models import BusinessRecord
from oui_categories import ap_vendor_tier, ble_device_type

# enc field values emitted by kkd-leaf: open | wep | wpa | wpa2 | wpa3
# WPA3-Enterprise (40 pts per spec) is indistinguishable from WPA3-Personal
# in v0 — the leaf reports both as "wpa3". Max reachable score is 32 pts.
ENC_SCORES: dict[str, int] = {
    "wpa3": 32,
    "wpa2": 20,
    "wpa":  10,
    "wep":   5,
    "open":  0,
}

ENC_ORDER = ["wpa3", "wpa2", "wpa", "wep", "open"]


# ── Dimension functions ───────────────────────────────────────────────────────


def score_encryption(record: BusinessRecord) -> int:
    """Best encryption seen across all APs. Max 40."""
    best_score = 0
    best_enc = ""
    for ap in record.aps.values():
        s = ENC_SCORES.get(ap.enc, 0)
        if s > best_score:
            best_score = s
            best_enc = ap.enc
    record.best_encryption = best_enc
    return min(best_score, 40)


def score_vendor(record: BusinessRecord) -> int:
    """Best AP vendor tier seen across all APs. Max 30."""
    best_score = 0
    best_tier = ""
    for ap in record.aps.values():
        tier, s = ap_vendor_tier(ap.vendor, ap.ssid)
        if s > best_score:
            best_score = s
            best_tier = tier
    record.best_vendor_tier = best_tier
    return min(best_score, 30)


def score_ble(record: BusinessRecord) -> int:
    """Highest-value BLE device type seen. Max 20."""
    best_score = 0
    best_type = ""
    for ble in record.ble_devices.values():
        device_type, s = ble_device_type(ble.ble_mfr, ble.ble_name, ble.vendor)
        if s > best_score:
            best_score = s
            best_type = device_type
    record.best_ble_type = best_type
    return min(best_score, 20)


def score_density(record: BusinessRecord) -> int:
    """Unique AP count → density score. Max 10."""
    n = len(record.aps)
    if n >= 3:
        return 10
    if n == 2:
        return 6
    if n == 1:
        return 3
    return 0


# ── Aggregator ────────────────────────────────────────────────────────────────


def score_business(record: BusinessRecord) -> None:
    """Compute and store total score and tier. Mutates record in place."""
    record.score_enc     = score_encryption(record)
    record.score_vendor  = score_vendor(record)
    record.score_ble     = score_ble(record)
    record.score_density = score_density(record)
    record.score = min(
        100,
        record.score_enc + record.score_vendor + record.score_ble + record.score_density,
    )

    if record.score >= 60:
        record.tier = "managed"
    elif record.score >= 35:
        record.tier = "semi"
    elif record.score >= 15:
        record.tier = "consumer"
    else:
        record.tier = "unknown"
