"""Rich terminal live display."""
from __future__ import annotations

from rich import box
from rich.columns import Columns
from rich.panel import Panel
from rich.table import Table

from models import BusinessRecord, UnassignedBucket

TIER_COLORS: dict[str, str] = {
    "managed":  "bright_green",
    "semi":     "yellow",
    "consumer": "cyan",
    "unknown":  "dim",
}


def make_live_display(
    businesses: dict[str, BusinessRecord],
    unassigned: UnassignedBucket,
    events_total: int,
    output_dir: str = "",
) -> Columns:
    """Live view: stats panel + top-5 table side by side."""
    return Columns(
        [
            _stats_panel(events_total, len(businesses), unassigned, output_dir),
            _top5_table(businesses),
        ],
        equal=False,
        expand=True,
    )


def make_final_table(businesses: dict[str, BusinessRecord]) -> Table:
    """Full ranked table printed after the live display exits."""
    ranked = sorted(businesses.values(), key=lambda b: b.score, reverse=True)
    t = Table(
        title=f"\n[bold]Final Rankings — {len(ranked)} business(es)[/bold]",
        box=box.ROUNDED,
        header_style="bold",
    )
    t.add_column("Rank",     style="dim", width=4)
    t.add_column("Business", min_width=22)
    t.add_column("Score",    justify="right", width=8)
    t.add_column("Tier",     width=10)
    t.add_column("APs",      justify="right", width=4)
    t.add_column("Best Enc", width=8)
    t.add_column("Vendor",   width=10)
    t.add_column("BLE",      justify="right", width=4)
    t.add_column("Sessions", justify="right", width=8)

    for i, rec in enumerate(ranked, 1):
        color = TIER_COLORS.get(rec.tier, "white")
        t.add_row(
            str(i),
            rec.name,
            f"[{color}]{rec.score}/100[/{color}]",
            f"[{color}]{rec.tier}[/{color}]",
            str(len(rec.aps)),
            rec.best_encryption or "—",
            rec.best_vendor_tier or "—",
            str(len(rec.ble_devices)),
            str(len(rec.session_ids)),
        )
    return t


# ── Internal helpers ──────────────────────────────────────────────────────────


def _stats_panel(
    events_total: int,
    biz_count: int,
    unassigned: UnassignedBucket,
    output_dir: str,
) -> Panel:
    lines = [
        f"Events    : [bold]{events_total:,}[/bold]",
        f"Businesses: [bold]{biz_count}[/bold]",
        f"Unassigned: [bold]{unassigned.event_count:,}[/bold]",
    ]
    if output_dir:
        lines.append(f"Output dir: [dim]{output_dir}[/dim]")
    return Panel(
        "\n".join(lines),
        title="[bold blue]Processing[/bold blue]",
        padding=(0, 1),
    )


def _top5_table(businesses: dict[str, BusinessRecord]) -> Table:
    ranked = sorted(businesses.values(), key=lambda b: b.score, reverse=True)[:5]
    t = Table(
        title="[bold]Top 5 Prospects[/bold]",
        box=box.SIMPLE_HEAVY,
        header_style="bold",
        min_width=64,
    )
    t.add_column("Rank",     style="dim", width=4, no_wrap=True)
    t.add_column("Business", min_width=22, no_wrap=True)
    t.add_column("Score",    justify="right", width=8)
    t.add_column("Tier",     width=10)
    t.add_column("APs",      justify="right", width=4)
    t.add_column("Best Enc", width=8)
    t.add_column("Sessions", justify="right", width=8)

    for i, rec in enumerate(ranked, 1):
        color = TIER_COLORS.get(rec.tier, "white")
        t.add_row(
            str(i),
            rec.name,
            f"[{color}]{rec.score}/100[/{color}]",
            f"[{color}]{rec.tier}[/{color}]",
            str(len(rec.aps)),
            rec.best_encryption or "—",
            str(len(rec.session_ids)),
        )
    if not ranked:
        t.add_row("—", "[dim]no data yet[/dim]", "—", "—", "—", "—", "—")
    return t
