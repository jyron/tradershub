#!/usr/bin/env python3
"""Print the next 09:00, 13:00, or 17:00 America/Bogota queue slot."""

from __future__ import annotations

import argparse
import datetime as dt
import json
from pathlib import Path


BOGOTA = dt.timezone(dt.timedelta(hours=-5), name="America/Bogota")
SLOT_HOURS = (9, 13, 17)


def parse_timestamp(value: str) -> dt.datetime:
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(dt.timezone.utc)


def as_rfc3339(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def next_slot(manifest: Path, now: dt.datetime) -> dt.datetime:
    payload = json.loads(manifest.read_text(encoding="utf-8"))
    scheduled = [parse_timestamp(article["publish_at"]) for article in payload.get("articles", [])]
    cursor = max([now.astimezone(dt.timezone.utc), *scheduled])
    local_cursor = cursor.astimezone(BOGOTA)

    for offset in range(367):
        day = local_cursor.date() + dt.timedelta(days=offset)
        for hour in SLOT_HOURS:
            candidate = dt.datetime.combine(day, dt.time(hour=hour), tzinfo=BOGOTA).astimezone(dt.timezone.utc)
            if candidate > cursor:
                return candidate
    raise RuntimeError("no publication slot found within one year")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--after", help="Optional RFC3339 clock override for deterministic testing")
    args = parser.parse_args()

    now = parse_timestamp(args.after) if args.after else dt.datetime.now(dt.timezone.utc)
    print(as_rfc3339(next_slot(args.manifest, now)))


if __name__ == "__main__":
    main()
