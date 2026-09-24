# Security Policy

The Hit Endpoint team takes the security of our software, dependencies, and our users' systems very seriously. We appreciate the efforts of the security community in helping us maintain a safe and reliable API testing platform.

---

## Supported Versions

Security updates and patches are actively maintained for the following versions:

| Version | Supported | Notes |
| :--- | :---: | :--- |
| `v0.1.x` | :white_check_mark: | Current active release branch |
| `< v0.1.0` | :x: | Prerelease and developmental versions (unsupported) |

If you are using an older version, we strongly recommend updating to the latest stable release via:
```bash
curl -fsSL https://raw.githubusercontent.com/hit-endpoint/hit-endpoint/main/install.sh | sh
```

---

## Reporting a Vulnerability

**Please do NOT file public GitHub Issues or discuss vulnerabilities on public forums for security vulnerabilities.**

Instead, please report security issues through one of the following secure channels:

### 1. GitHub Private Vulnerability Reporting (Preferred)
Submit a confidential advisory directly to repository maintainers:
* Navigate to [**Security > Report a vulnerability**](https://github.com/hit-endpoint/hit-endpoint/security/advisories/new)
* This opens a private, encrypted discussion with the core maintainers.

### 2. Direct Security Contact
If you cannot use GitHub Security Advisories, send an encrypted or direct email to:
* **Contact**: Mark Jordan (`majordan1973@gmail.com`)
* **Subject line**: `[SECURITY VULNERABILITY] hit-endpoint: <brief description>`

---

## What to Include in Your Report

To help us triage and resolve the issue quickly, please provide as much context as possible:

1. **Description**: Clear description of the vulnerability, including attack vector and potential impact.
2. **Affected Version / Component**: Specific binary version, commit hash, OS, and subcommand involved (e.g. `hit mock`, `hit run`, `hit hub`, `hit ws`).
3. **Reproduction Steps**: Step-by-step instructions or minimal test specification YAML / CLI flags to reproduce the issue.
4. **Proof of Concept**: Minimal runnable PoC script or request payload (redact any sensitive credentials or proprietary keys).
5. **Mitigation / Suggested Fix**: If you have identified a potential patch or workaround, please include it.

---

## Response Timelines & SLAs

We commit to the following response timeline for all verified vulnerability reports:

| Milestone | Target Timeline | Details |
| :--- | :--- | :--- |
| **Initial Acknowledgment** | **Within 48 hours** | Maintainer confirms receipt of your report. |
| **Triage & Assessment** | **Within 5 business days** | Verification of vulnerability, severity score (CVSS), and impact scope. |
| **Patch Development & Testing**| **7–14 days** | Development of targeted fix and regression testing across Linux, macOS, and Windows. |
| **Release & Coordinated Disclosure**| **Negotiated (typically 30 days)** | Public release of patched version, release notes, and GitHub Security Advisory with attribution. |

---

## Coordinated Disclosure & Safe Harbor

We fully support coordinated vulnerability disclosure:

* **Safe Harbor**: If you conduct security research in good faith and comply with this policy, we consider your actions authorized, will not initiate legal action against you, and will work cooperatively with you to understand and address the issue.
* **Embargo**: We request that you maintain confidentiality and refrain from public disclosure until an official patch has been released and users have had reasonable opportunity to update.
* **Credit & Attribution**: With your consent, we will publicly credit your contribution in our release notes, security advisories, and CHANGELOG.

---

## Built-in Security Best Practices for Users

Hit Endpoint is designed with several defensive features built-in to protect developer credentials and testing environments:

1. **Secrets Isolation**: Keep credentials in `local.secrets.yaml`. This file is matched by `.gitignore` by default so secrets are never committed into Git version control.
2. **Automatic Secret Masking**: The execution engine automatically masks Authorization headers, bearer tokens, passwords, and API keys in terminal output, HTML reports, and `.hit/history.jsonl`.
3. **Telemetry Sanitization**: When publishing test runs to a telemetry hub (`hit run --publish=...`), all sensitive request headers and authorization parameters are scrubbed on the client side before network dispatch.
4. **Mock Server Binding**: When running `hit mock`, bind only to `127.0.0.1` unless explicitly running within a secure, firewalled test container.
