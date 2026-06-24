"""Load and validate kkd-leaf NDJSON capture files."""
from __future__ import annotations

import json
import sys
from pathlib import Path

from models import CaptureEvent

_REQUIRED: frozenset[str] = frozenset({
    "ts", "session_id", "type", "mac", "vendor", "ssid",
    "probe_ssids", "rssi", "channel", "enc", "ble_name",
    "ble_mfr", "lat", "lon", "node_id", "iface_id",
})


def load_files(paths: list[Path]) -> tuple[list[CaptureEvent], int]:
    """Parse all input NDJSON files. Returns (events, total_skipped)."""
    events: list[CaptureEvent] = []
    skipped = 0
    for path in paths:
        file_events, file_skipped = _load_one(path)
        events.extend(file_events)
        skipped += file_skipped
    return events, skipped


def _load_one(path: Path) -> tuple[list[CaptureEvent], int]:
    events: list[CaptureEvent] = []
    skipped = 0
    try:
        fh = path.open("r", encoding="utf-8")
    except OSError as exc:
        print(f"  [error] cannot open {path}: {exc}", file=sys.stderr)
        return events, skipped

    with fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError as exc:
                print(f"  [warn] {path.name}:{lineno}: bad JSON — {exc}", file=sys.stderr)
                skipped += 1
                continue

            missing = _REQUIRED - obj.keys()
            if missing:
                print(
                    f"  [warn] {path.name}:{lineno}: missing fields "
                    f"{sorted(missing)}",
                    file=sys.stderr,
                )
                skipped += 1
                continue

            events.append(CaptureEvent(
                ts=obj["ts"],
                session_id=obj["session_id"],
                type=obj["type"],
                mac=obj["mac"],
                vendor=obj.get("vendor") or "",
                ssid=obj.get("ssid") or "",
                probe_ssids=[s for s in (obj.get("probe_ssids") or []) if s],
                rssi=int(obj.get("rssi") or 0),
                channel=int(obj.get("channel") or 0),
                enc=obj.get("enc") or "",
                ble_name=obj.get("ble_name") or "",
                ble_mfr=obj.get("ble_mfr") or "",
                lat=float(obj.get("lat") or 0.0),
                lon=float(obj.get("lon") or 0.0),
                node_id=obj.get("node_id") or "",
                iface_id=obj.get("iface_id") or "",
            ))
    return events, skipped
