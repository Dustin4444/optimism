#!/usr/bin/env python3
"""Self-test for gas-report-diff.py. Run it with no arguments; it exits non-zero on failure.

The fixtures reproduce the table forge actually prints, including the surrounding test output
the parser has to ignore.
"""

import pathlib
import subprocess
import sys
import tempfile

SCRIPT = pathlib.Path(__file__).with_name("gas-report-diff.py")

PREAMBLE = """Compiling 2 files with Solc 0.8.30
Ran 1 test for test/Counter.t.sol:CounterTest
[PASS] test_increment_succeeds() (gas: 75992)
Suite result: ok. 1 passed; 0 failed; 0 skipped; finished in 306.38µs
"""

EPILOGUE = "\nRan 1 test suite in 1.16ms: 1 tests passed, 0 failed, 0 skipped (1 total tests)\n"


def table(identifier, deployment, functions):
    rule = "|----------------------------------+-----------------+-------+--------+-------+---------|"
    lines = [
        "╭----------------------------------+-----------------+-------+--------+-------+---------╮",
        f"| {identifier} Contract |                 |       |        |       |         |",
        "+=======================================================================================+",
        "| Deployment Cost                  | Deployment Size |       |        |       |         |",
        rule,
        f"| {deployment}                           | 491             |       |        |       |         |",
        rule,
        "|                                  |                 |       |        |       |         |",
        rule,
        "| Function Name                    | Min             | Avg   | Median | Max   | # Calls |",
    ]
    for name, gas in functions.items():
        lines += [rule, f"| {name} | {gas} | {gas} | {gas} | {gas} | 1       |"]
    lines.append("╰----------------------------------+-----------------+-------+--------+-------+---------╯")
    return "\n".join(lines)


def run(base_text, head_text, *args):
    with tempfile.TemporaryDirectory() as tmp:
        base = pathlib.Path(tmp, "base.txt")
        head = pathlib.Path(tmp, "head.txt")
        base.write_text(base_text, encoding="utf-8")
        head.write_text(head_text, encoding="utf-8")
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(base), str(head), *args],
            capture_output=True,
            text=True,
            check=False,
        )


FAILURES = []


def check(condition, description):
    if not condition:
        FAILURES.append(description)


def report(identifier="src/Counter.sol:Counter", deployment=152883, **functions):
    return PREAMBLE + table(identifier, deployment, functions) + EPILOGUE


base = report(increment=43539, setNumber=26596, willBeRemoved=1000)
head = report(deployment=161000, increment=45000, setNumber=26596, isNew=2100)
result = run(base, head)
out = result.stdout

check(result.returncode == 0, f"expected exit 0, got {result.returncode}: {result.stderr}")
check("| src/Counter.sol:Counter | increment | 43,539 | 45,000 | +1,461 | +3.36% |" in out, "increase row")
check("| src/Counter.sol:Counter | [deployment] | 152,883 | 161,000 | +8,117 | +5.31% |" in out, "deployment row")
check("| src/Counter.sol:Counter | isNew | — | 2,100 | new | — |" in out, "new function row")
check("| src/Counter.sol:Counter | willBeRemoved | 1,000 | — | removed | — |" in out, "removed function row")
check("setNumber" not in out, "unchanged functions are omitted")
check("**2 increased · 0 decreased · 1 new · 1 removed** (1 unchanged entry omitted)" in out, "counts")
# The deployment delta is larger than the increment delta, so it must sort first.
check(out.index("[deployment]") < out.index("increment"), "rows sorted by absolute delta")

decrease = run(report(increment=43539), report(increment=40000))
check("| src/Counter.sol:Counter | increment | 43,539 | 40,000 | -3,539 | -8.13% |" in decrease.stdout, "decrease row")
check("**0 increased · 1 decreased ·" in decrease.stdout, "decrease is not counted as an increase")

identical = run(report(increment=43539), report(increment=43539))
check("No gas changes against `develop` (2 entries compared)." in identical.stdout, "no-change message")

# A contract present on only one side must not be dropped.
added_contract = run(report(increment=1), report(increment=1) + table("src/New.sol:New", 5000, {"f": 700}))
check("| src/New.sol:New | [deployment] | — | 5,000 | new | — |" in added_contract.stdout, "new contract")
check("| src/New.sol:New | f | — | 700 | new | — |" in added_contract.stdout, "new contract function")

labels = run(report(increment=1), report(increment=2), "--base-label", "v1", "--head-label", "v2")
check("### Gas diff vs `v1`" in labels.stdout, "base label is used")
check("| Contract | Function | `v1` | `v2` | Δ | Δ% |" in labels.stdout, "head label is used")

capped = run(
    report(**{f"fn{i}": 100 for i in range(10)}),
    report(**{f"fn{i}": 200 for i in range(10)}),
    "--max-rows",
    "3",
)
check(capped.stdout.count("\n| src/Counter.sol:Counter |") == 3, "row cap is honoured")
check("7 more rows" in capped.stdout, "truncation notice reports the remainder")

# Nothing to compare against: the caller must be able to tell, rather than get an empty diff.
empty = run("no tables here\n", report(increment=1))
check(empty.returncode == 1, "empty base report exits non-zero")
check("skipping the diff" in empty.stderr, "empty base report explains itself")

if FAILURES:
    for failure in FAILURES:
        print(f"FAIL: {failure}", file=sys.stderr)
    sys.exit(1)
print("OK: gas-report-diff.py self-test passed")
