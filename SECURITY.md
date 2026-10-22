# Security Policy

## Supported versions

| Version | Supported |
| --- | --- |
| 0.1.x | Yes |

## Reporting a vulnerability

If you believe you have found a security issue in GPU Admission on Kueue, please report it privately by emailing the maintainer associated with this repository (GitHub user `justrunme`) or by using GitHub's private vulnerability reporting for this project.

Please include:

- a description of the issue and its impact
- reproduction steps (policy + workload fixtures when possible)
- whether the issue bypasses a fail-closed path (controller down, malformed policy, missing labels, undecidable inputs)

Do not open a public issue for vulnerabilities that could weaken admission enforcement until a fix is available.

## Scope notes

This project is a **fail-closed AdmissionCheck controller** for GPU AI workloads on upstream Kueue. It is not a node GPU driver, device plugin, or runtime sandbox. The kind demo advertises `nvidia.com/gpu` as a simulated extended resource; physical GPU isolation is out of scope for that demo.
