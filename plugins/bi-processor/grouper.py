"""Assign CaptureEvents to BusinessRecords.

Pipeline:
  1. WiFi beacons and probes → businesses by SSID match, else Unassigned.
  2. BLE events staged by session_id.
  3. After WiFi pass, BLE promoted to any business that saw WiFi in the
     same session (corridor proximity heuristic). Remaining BLE stays
     Unassigned — never dropped.
"""
from __future__ import annotations

from models import AP, BLEDevice, BusinessRecord, CaptureEvent, UnassignedBucket
from boundary import find_business


def group_events(
    events: list[CaptureEvent],
    index: list[tuple[str, str]],
) -> tuple[dict[str, BusinessRecord], UnassignedBucket]:
    businesses: dict[str, BusinessRecord] = {}
    unassigned = UnassignedBucket()
    ble_staged: dict[str, list[CaptureEvent]] = {}  # session_id → ble events

    for ev in events:
        if ev.type == "wifi_beacon":
            _handle_beacon(ev, businesses, unassigned, index)
        elif ev.type == "wifi_probe":
            _handle_probe(ev, businesses, unassigned, index)
        elif ev.type == "ble_adv":
            ble_staged.setdefault(ev.session_id, []).append(ev)
            unassigned.event_count += 1
            unassigned.ble_devices[ev.mac] = BLEDevice(
                mac=ev.mac,
                vendor=ev.vendor,
                ble_name=ev.ble_name,
                ble_mfr=ev.ble_mfr,
            )

    _associate_ble(businesses, unassigned, ble_staged)
    return businesses, unassigned


# ── Handlers ──────────────────────────────────────────────────────────────────


def _handle_beacon(
    ev: CaptureEvent,
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    index: list[tuple[str, str]],
) -> None:
    biz_name = find_business(ev.ssid, index)
    if biz_name:
        rec = _ensure(biz_name, businesses)
        rec.event_count += 1
        rec.session_ids.add(ev.session_id)
        rec.aps[ev.mac] = AP(
            mac=ev.mac,
            ssid=ev.ssid,
            enc=ev.enc,
            vendor=ev.vendor,
            channel=ev.channel,
        )
    else:
        unassigned.event_count += 1
        if ev.ssid:
            unassigned.ssids.add(ev.ssid)


def _handle_probe(
    ev: CaptureEvent,
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    index: list[tuple[str, str]],
) -> None:
    biz_name = None
    for ssid in ev.probe_ssids:
        biz_name = find_business(ssid, index)
        if biz_name:
            break

    if biz_name:
        rec = _ensure(biz_name, businesses)
        rec.event_count += 1
        rec.session_ids.add(ev.session_id)
        rec.probe_ssids.update(ev.probe_ssids)
    else:
        unassigned.event_count += 1
        unassigned.ssids.update(ev.probe_ssids)


def _associate_ble(
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    ble_staged: dict[str, list[CaptureEvent]],
) -> None:
    # Build reverse map: session_id → [business_names]
    session_to_biz: dict[str, list[str]] = {}
    for biz_name, rec in businesses.items():
        for sid in rec.session_ids:
            session_to_biz.setdefault(sid, []).append(biz_name)

    for session_id, evs in ble_staged.items():
        biz_names = session_to_biz.get(session_id)
        if not biz_names:
            continue  # no WiFi match in this session — stays Unassigned
        for ev in evs:
            ble = BLEDevice(
                mac=ev.mac,
                vendor=ev.vendor,
                ble_name=ev.ble_name,
                ble_mfr=ev.ble_mfr,
            )
            for biz_name in biz_names:
                businesses[biz_name].ble_devices[ev.mac] = ble
            unassigned.ble_devices.pop(ev.mac, None)
            unassigned.event_count = max(0, unassigned.event_count - 1)


def _ensure(name: str, businesses: dict[str, BusinessRecord]) -> BusinessRecord:
    if name not in businesses:
        businesses[name] = BusinessRecord(name=name)
    return businesses[name]
