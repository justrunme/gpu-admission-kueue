#!/usr/bin/env python3
"""Print AdmissionCheck outcome for a Job-backed Kueue Workload (stdin: list JSON).

Prints the raw state, or REJECTED_EFFECTIVE when Kueue has already reset a
Rejected check to Pending after deactivating the Workload.
"""
from __future__ import annotations

import json
import sys


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: workload_check_state.py <job-name> [check-name]", file=sys.stderr)
        return 2
    job = sys.argv[1]
    check = sys.argv[2] if len(sys.argv) > 2 else "gpu-admission"
    doc = json.load(sys.stdin)
    for w in doc.get("items", []):
        name = w.get("metadata", {}).get("name", "")
        owner_ok = job in name
        for o in w.get("metadata", {}).get("ownerReferences") or []:
            if o.get("name") == job:
                owner_ok = True
        labels = w.get("metadata", {}).get("labels") or {}
        if labels.get("job-name") == job or labels.get("batch.kubernetes.io/job-name") == job:
            owner_ok = True
        if not owner_ok:
            continue
        active = w.get("spec", {}).get("active", True)
        for c in w.get("status", {}).get("admissionChecks") or []:
            if c.get("name") != check:
                continue
            state = c.get("state", "")
            msg = c.get("message") or ""
            if state == "Rejected":
                print("Rejected")
                return 0
            if "Previously: Rejected" in msg or active is False and state == "Pending" and "Rejected" in msg:
                print("REJECTED_EFFECTIVE")
                return 0
            print(state)
            return 0
    print("")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
