#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Account-wide Claude usage for the current rolling window, computed locally from
# ~/.claude/projects/*/*.jsonl with zero network calls. This is the budget
# guard's fallback when the ccusage binary is not installed. costUSD is an
# approximation (an installed ccusage is preferred for accuracy); the guard's
# safety-margin and warn/pause fractions absorb the slack.
#
# Usage: usage-window.py [--hours N] [--projects-root DIR]
# Output: JSON {window_start, hours, window_cost_usd, tokens{...}, by_model{...}}

import argparse
import datetime
import glob
import json
import os
import sys

# Approximate Anthropic API USD per million tokens, used only by this fallback.
PRICES = {
    "opus": {"in": 15.0, "out": 75.0, "cw": 18.75, "cr": 1.50},
    "sonnet": {"in": 3.0, "out": 15.0, "cw": 3.75, "cr": 0.30},
    "haiku": {"in": 0.80, "out": 4.0, "cw": 1.0, "cr": 0.08},
}


def tier_for(model):
    m = (model or "").lower()
    for tier in ("opus", "sonnet", "haiku"):
        if tier in m:
            return tier
    return "sonnet"


def window_start(now, hours):
    # Fixed UTC grid aligned to the hour; the block is `hours` wide. This is an
    # approximation of ccusage's activity-anchored blocks, deterministic enough
    # for a guard with safety margins.
    grid = (now.hour // hours) * hours
    return now.replace(hour=grid, minute=0, second=0, microsecond=0)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--hours", type=int, default=5)
    ap.add_argument(
        "--projects-root",
        default=os.path.expanduser("~/.claude/projects"),
    )
    args = ap.parse_args()

    now = datetime.datetime.now(datetime.timezone.utc)
    start = window_start(now, args.hours)

    tokens = {"in": 0, "out": 0, "cw": 0, "cr": 0}
    by_model = {}
    cost = 0.0

    for fpath in glob.glob(os.path.join(args.projects_root, "*", "*.jsonl")):
        try:
            with open(fpath, "r", encoding="utf-8") as fh:
                for line in fh:
                    if '"usage"' not in line:
                        continue
                    try:
                        obj = json.loads(line)
                    except ValueError:
                        continue
                    ts = obj.get("timestamp") or obj.get("ts")
                    if not ts:
                        continue
                    try:
                        when = datetime.datetime.fromisoformat(ts.replace("Z", "+00:00"))
                    except ValueError:
                        continue
                    if when < start:
                        continue
                    msg = obj.get("message") or {}
                    usage = msg.get("usage") or obj.get("usage") or {}
                    if not usage:
                        continue
                    model = msg.get("model") or obj.get("model") or "sonnet"
                    tier = tier_for(model)
                    p = PRICES[tier]
                    i = usage.get("input_tokens", 0) or 0
                    o = usage.get("output_tokens", 0) or 0
                    cw = usage.get("cache_creation_input_tokens", 0) or 0
                    cr = usage.get("cache_read_input_tokens", 0) or 0
                    tokens["in"] += i
                    tokens["out"] += o
                    tokens["cw"] += cw
                    tokens["cr"] += cr
                    line_cost = (
                        i * p["in"] + o * p["out"] + cw * p["cw"] + cr * p["cr"]
                    ) / 1_000_000.0
                    cost += line_cost
                    by_model[tier] = round(by_model.get(tier, 0.0) + line_cost, 4)
        except OSError:
            continue

    print(
        json.dumps(
            {
                "window_start": start.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "hours": args.hours,
                "window_cost_usd": round(cost, 4),
                "tokens": tokens,
                "by_model": by_model,
                "source": "transcript-fallback",
            }
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
