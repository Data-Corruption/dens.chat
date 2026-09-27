#!/usr/bin/env python3
"""Print spike JSON reports as compact tables. Nested service reports (the
control socket or pipe reply) are printed after the client's own checks."""
import json
import sys


def rows(report, indent=""):
    print(f"{indent}[{report.get('role', '?')}]")
    for c in report.get("checks", []):
        want = c.get("want", "")
        if "pass" in c:
            mark = "PASS" if c["pass"] else "FAIL"
        else:
            mark = "info"
        name = c["name"]
        got = c["got"].replace("\n", " ")
        print(f"{indent}  {mark:4}  {name}" + (f"  [want {want}]" if want else ""))
        print(f"{indent}        {got}")
    nested = report.get("nested")
    if isinstance(nested, dict):
        if "checks" in nested:
            rows(nested, indent + "  ")
        else:
            print(f"{indent}  nested: {json.dumps(nested)}")


for path in sys.argv[1:]:
    print(f"== {path}")
    with open(path, encoding="utf-8") as f:
        text = f.read().strip()
    if not text:
        print("  (empty)")
        continue
    rows(json.loads(text))
