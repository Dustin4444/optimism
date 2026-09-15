#!/usr/bin/env python3
"""Diffs two `forge test --gas-report` reports and renders the result as Markdown.

Reads the text tables forge prints (comfy_table, round corners) rather than `--gas-report
--json`, so the same run produces both the human-readable report kept as an artifact and the
numbers compared here.

Deployment cost is compared as a pseudo-function named `[deployment]`. Median gas is the
compared metric: for a function called once it equals min/avg/max, and for a fuzzed function
it is far less noisy than the average.

Usage: gas-report-diff.py BASE_REPORT HEAD_REPORT [--base-label L] [--head-label L]
"""

import argparse
import sys

TABLE_START = "╭"  # top-left round corner, first line of every gas table
TABLE_END = "╰"  # bottom-left round corner, last line of every gas table
DEPLOYMENT = "[deployment]"
FUNCTION_HEADER = "Function Name"
DEPLOYMENT_HEADER = "Deployment Cost"


def _cells(line):
    return [cell.strip() for cell in line.strip().strip("|").split("|")]


def _to_int(value):
    try:
        return int(value.replace(",", ""))
    except ValueError:
        return None


def parse_report(text):
    """Extracts {contract identifier: {function: median gas}} from a forge gas report."""
    contracts = {}
    contract = None
    section = None

    for raw in text.splitlines():
        line = raw.rstrip()
        if line.startswith(TABLE_START) or line.startswith(TABLE_END):
            contract, section = None, None
            continue
        # Row separators ('|---+---|') and the header rule ('+===+===+') carry no data.
        if not line.startswith("|") or line.startswith("|-"):
            continue

        cells = _cells(line)
        if not cells or not cells[0]:
            continue

        # A table opens with a single-cell title row: '<identifier> Contract'.
        if cells[0].endswith(" Contract") and not any(cells[1:]):
            contract = cells[0][: -len(" Contract")]
            contracts.setdefault(contract, {})
            section = None
            continue
        if contract is None:
            continue

        if cells[0] == DEPLOYMENT_HEADER:
            section = DEPLOYMENT
            continue
        if cells[0] == FUNCTION_HEADER:
            section = FUNCTION_HEADER
            continue

        if section == DEPLOYMENT:
            gas = _to_int(cells[0])
            if gas is not None:
                contracts[contract][DEPLOYMENT] = gas
            section = None
        elif section == FUNCTION_HEADER and len(cells) >= 4:
            gas = _to_int(cells[3])  # Function Name | Min | Avg | Median | Max | # Calls
            if gas is not None:
                contracts[contract][cells[0]] = gas

    return contracts


def diff_reports(base, head):
    """Pairs up every (contract, function) across both reports, skipping unchanged entries."""
    changed, added, removed, unchanged = [], [], [], 0

    for contract in sorted(set(base) | set(head)):
        base_fns = base.get(contract, {})
        head_fns = head.get(contract, {})
        for function in sorted(set(base_fns) | set(head_fns)):
            before, after = base_fns.get(function), head_fns.get(function)
            if before is None:
                added.append((contract, function, after))
            elif after is None:
                removed.append((contract, function, before))
            elif before != after:
                changed.append((contract, function, before, after))
            else:
                unchanged += 1

    # Largest movements first; they are the ones worth a reviewer's attention.
    changed.sort(key=lambda row: (-abs(row[3] - row[2]), row[0], row[1]))
    return changed, added, removed, unchanged


def _percent(before, after):
    if before == 0:
        return "n/a"
    return f"{(after - before) / before * 100:+.2f}%"


def render(changed, added, removed, unchanged, base_label, head_label, max_rows):
    increases = sum(1 for _, _, before, after in changed if after > before)
    lines = [
        f"### Gas diff vs `{base_label}`",
        "",
        f"Median gas per call; deployment cost is listed as `{DEPLOYMENT}`.",
        "",
    ]

    if not changed and not added and not removed:
        lines += [f"No gas changes against `{base_label}` ({unchanged} entries compared).", ""]
        return "\n".join(lines)

    lines += [
        f"**{increases} increased · {len(changed) - increases} decreased · "
        f"{len(added)} new · {len(removed)} removed** "
        f"({unchanged} unchanged {'entry' if unchanged == 1 else 'entries'} omitted)",
        "",
        f"| Contract | Function | `{base_label}` | `{head_label}` | Δ | Δ% |",
        "| --- | --- | ---: | ---: | ---: | ---: |",
    ]

    rows = [
        (contract, function, f"{before:,}", f"{after:,}", f"{after - before:+,}", _percent(before, after))
        for contract, function, before, after in changed
    ]
    rows += [(contract, function, "—", f"{gas:,}", "new", "—") for contract, function, gas in added]
    rows += [(contract, function, f"{gas:,}", "—", "removed", "—") for contract, function, gas in removed]

    for row in rows[:max_rows]:
        lines.append("| " + " | ".join(row) + " |")
    if len(rows) > max_rows:
        lines.append(f"| … | _{len(rows) - max_rows} more rows, see the gas report artifacts_ | | | | |")

    lines.append("")
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base_report")
    parser.add_argument("head_report")
    parser.add_argument("--base-label", default="develop")
    parser.add_argument("--head-label", default="this branch")
    parser.add_argument("--max-rows", type=int, default=200)
    args = parser.parse_args(argv)

    reports = []
    for path in (args.base_report, args.head_report):
        with open(path, encoding="utf-8") as handle:
            reports.append(parse_report(handle.read()))

    if not reports[0]:
        print(f"No gas tables found in {args.base_report}; skipping the diff.", file=sys.stderr)
        return 1

    sys.stdout.write(
        render(*diff_reports(*reports), args.base_label, args.head_label, args.max_rows)
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
