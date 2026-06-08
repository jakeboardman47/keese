#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai
#
# Parse the YAML frontmatter of a docs/plans phase file into JSON, with no
# third-party dependency (PyYAML is not assumed). Handles the subset the phase
# docs actually use: scalars, quoted scalars, flow lists ([a, b]) and block
# lists (- item). Leading HTML comments before the first '---' are skipped.
#
# Usage: frontmatter.py <file>   ->  prints a JSON object of the frontmatter.

import json
import sys


def parse_scalar(raw):
    s = raw.strip()
    if len(s) >= 2 and s[0] in "\"'" and s[-1] == s[0]:
        return s[1:-1]
    return s


def parse_flow_list(raw):
    inner = raw.strip()[1:-1].strip()
    if not inner:
        return []
    return [parse_scalar(x) for x in inner.split(",") if x.strip()]


def extract_block(lines):
    """Return the lines between the first two '---' fences (frontmatter)."""
    start = None
    for i, line in enumerate(lines):
        if line.rstrip() == "---":
            start = i + 1
            break
    if start is None:
        return []
    block = []
    for line in lines[start:]:
        if line.rstrip() == "---":
            break
        block.append(line)
    return block


def parse(block):
    data = {}
    i = 0
    n = len(block)
    while i < n:
        line = block[i]
        if not line.strip() or line.lstrip().startswith("#"):
            i += 1
            continue
        if ":" not in line:
            i += 1
            continue
        key, _, rest = line.partition(":")
        key = key.strip()
        rest = rest.strip()
        if rest == "" :
            # Possibly a block list following on subsequent indented '- ' lines.
            items = []
            j = i + 1
            while j < n and block[j].lstrip().startswith("- "):
                items.append(parse_scalar(block[j].lstrip()[2:]))
                j += 1
            if items:
                data[key] = items
                i = j
                continue
            data[key] = ""
            i += 1
            continue
        if rest.startswith("[") and rest.endswith("]"):
            data[key] = parse_flow_list(rest)
        else:
            data[key] = parse_scalar(rest)
        i += 1
    return data


def main():
    if len(sys.argv) != 2:
        print("usage: frontmatter.py <file>", file=sys.stderr)
        return 2
    try:
        with open(sys.argv[1], "r", encoding="utf-8") as fh:
            lines = fh.readlines()
    except OSError as exc:
        print(f"cannot read {sys.argv[1]}: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(parse(extract_block(lines))))
    return 0


if __name__ == "__main__":
    sys.exit(main())
