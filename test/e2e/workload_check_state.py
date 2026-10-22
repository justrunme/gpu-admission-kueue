#!/usr/bin/env python3
"""Print AdmissionCheck outcome for a Job-backed Kueue Workload (stdin: list JSON).

Workload matching is exact via ownerReference (Job) or job-name labels only —
never a substring of the Workload name.

Prints the raw state, or REJECTED_EFFECTIVE when Kueue has already reset a
Rejected check to Pending after deactivating the Workload.

Optional 3rd argument: reason_prefix. When set, exit 0 only if the check
message (or REJECTED_EFFECTIVE with message containing the original reason)
starts with that prefix. For Previously: Rejected, callers should also check
evidence ConfigMaps.
"""
from __future__ import annotations

import json
import sys


def match_workload(w: dict, job: str) -> bool:
    for o in w.get("metadata", {}).get("ownerReferences") or []:
        if o.get("kind") == "Job" and o.get("name") == job:
            return True
    labels = w.get("metadata", {}).get("labels") or {}
    if labels.get("batch.kubernetes.io/job-name") == job:
        return True
    if labels.get("job-name") == job:
        return True
    return False


def main() -> int:
    if len(sys.argv) < 2:
        print(
            "usage: workload_check_state.py <job-name> [check-name] [reason_prefix]",
            file=sys.stderr,
        )
        return 2
    job = sys.argv[1]
    check = sys.argv[2] if len(sys.argv) > 2 else "gpu-admission"
    reason_prefix = sys.argv[3] if len(sys.argv) > 3 else ""
    doc = json.load(sys.stdin)
    for w in doc.get("items", []):
        if not match_workload(w, job):
            continue
        active = w.get("spec", {}).get("active", True)
        for c in w.get("status", {}).get("admissionChecks") or []:
            if c.get("name") != check:
                continue
            state = c.get("state", "")
            msg = c.get("message") or ""
            out_state = state
            if state == "Rejected":
                out_state = "Rejected"
            elif "Previously: Rejected" in msg or (
                active is False and state == "Pending" and "Rejected" in msg
            ):
                out_state = "REJECTED_EFFECTIVE"
            if reason_prefix:
                # Accept current message prefix, or Previously line that still
                # embeds the original reason text after "Previously: Rejected: ".
                ok = msg.startswith(reason_prefix) or reason_prefix in msg
                if out_state in ("Rejected", "REJECTED_EFFECTIVE") and ok:
                    print(out_state)
                    return 0
                print(f"REASON_MISS state={out_state} msg={msg}", file=sys.stderr)
                print(out_state)
                return 1
            print(out_state)
            return 0
    print("")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
