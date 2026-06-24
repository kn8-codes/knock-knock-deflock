"""Load the boundary file and match SSIDs to business names."""
from __future__ import annotations

import json
import sys
from pathlib import Path


def load_boundary(path: Path) -> dict[str, list[str]]:
    """Parse and validate the boundary JSON file.

    Expected format:
        {"Business Name": ["SSID_ONE", "SSID_TWO"], ...}

    Values that are a bare string (not a list) are normalised to [string].
    """
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError) as exc:
        print(f"ERROR: cannot read boundary file {path}: {exc}", file=sys.stderr)
        sys.exit(1)

    if not isinstance(raw, dict):
        print("ERROR: boundary file must be a JSON object", file=sys.stderr)
        sys.exit(1)

    result: dict[str, list[str]] = {}
    for name, patterns in raw.items():
        if isinstance(patterns, str):
            result[name] = [patterns]
        elif isinstance(patterns, list):
            result[name] = [str(p) for p in patterns if p]
        else:
            print(
                f"  [warn] boundary: '{name}' has unexpected type — skipping",
                file=sys.stderr,
            )
    return result


def build_index(boundary: dict[str, list[str]]) -> list[tuple[str, str]]:
    """Flatten to [(pattern_lower, business_name)] for linear scan matching."""
    return [
        (pattern.lower(), biz_name)
        for biz_name, patterns in boundary.items()
        for pattern in patterns
    ]


def find_business(ssid: str, index: list[tuple[str, str]]) -> str | None:
    """Case-insensitive substring match. Returns first matching business name."""
    if not ssid:
        return None
    ssid_lower = ssid.lower()
    for pattern, biz_name in index:
        if pattern in ssid_lower:
            return biz_name
    return None
