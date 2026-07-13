#!/usr/bin/env python3
"""Validate the BotTrade article manifest and three-per-day cadence."""

from __future__ import annotations

import argparse
import collections
import datetime as dt
import json
import re
from pathlib import Path


BOGOTA = dt.timezone(dt.timedelta(hours=-5), name="America/Bogota")
SLOT_HOURS = {9, 13, 17}
SLUG = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
REQUIRED = {
    "slug",
    "title",
    "description",
    "kicker",
    "deck",
    "abstract",
    "conclusion_title",
    "conclusion",
    "publish_at",
    "items",
}


def parse_timestamp(value: str) -> dt.datetime:
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(dt.timezone.utc)


def validate(path: Path) -> tuple[int, dict[str, int]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    articles = payload.get("articles")
    if not isinstance(articles, list):
        raise ValueError("top-level articles must be a list")

    slugs: set[str] = set()
    titles: set[str] = set()
    timestamps: set[dt.datetime] = set()
    by_day: collections.Counter[str] = collections.Counter()

    for index, article in enumerate(articles):
        missing = REQUIRED - article.keys()
        if missing:
            raise ValueError(f"article {index} missing fields: {sorted(missing)}")
        slug = article["slug"]
        title = article["title"]
        if not SLUG.fullmatch(slug):
            raise ValueError(f"invalid slug: {slug!r}")
        if slug in slugs:
            raise ValueError(f"duplicate slug: {slug}")
        if title in titles:
            raise ValueError(f"duplicate title: {title}")
        if not isinstance(article["items"], list) or len(article["items"]) < 6:
            raise ValueError(f"{slug} must contain at least six ranked items")
        for item in article["items"]:
            if not item.get("name") or not item.get("body"):
                raise ValueError(f"{slug} contains an incomplete ranked item")

        published = parse_timestamp(article["publish_at"])
        if published in timestamps:
            raise ValueError(f"duplicate publication timestamp: {article['publish_at']}")
        local = published.astimezone(BOGOTA)
        if local.minute != 0 or local.second != 0 or local.hour not in SLOT_HOURS:
            raise ValueError(f"{slug} is outside the 09:00/13:00/17:00 Bogota slots")

        slugs.add(slug)
        titles.add(title)
        timestamps.add(published)
        by_day[local.date().isoformat()] += 1

    overfilled = {day: count for day, count in by_day.items() if count > 3}
    if overfilled:
        raise ValueError(f"publication days exceed three articles: {overfilled}")
    return len(articles), dict(sorted(by_day.items()))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    count, days = validate(args.manifest)
    print(f"validated {count} articles across {len(days)} publication days: {days}")


if __name__ == "__main__":
    main()
