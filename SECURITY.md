# Security policy

## Supported versions

Security fixes apply to the latest commit on `main` and the newest published release.

## Reporting a vulnerability

Please do not open a public issue for vulnerabilities involving arbitrary code execution, unsafe USB handling, malicious document processing, or sensitive-data exposure.

Use GitHub's private vulnerability reporting feature for this repository. Include:

- affected commit or release;
- reproduction steps;
- expected and actual behavior;
- security impact; and
- a minimal synthetic fixture when possible.

Do not include proprietary vendor files or private documents.

## Hardware safety

Generated printer data is treated as untrusted until independently decoded and validated. Changes that bypass generated-stream validation or loosen USB short-write/error checks require explicit security review.
