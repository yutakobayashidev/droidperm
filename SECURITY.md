# Security policy

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private
security advisory reporting for this repository:

1. Open the repository's **Security** tab.
2. Select **Advisories** and **Report a vulnerability**.
3. Include affected versions, impact, reproduction steps, and any suggested
   mitigation.

If private vulnerability reporting is unavailable, contact the repository
owner privately through the contact method listed on their GitHub profile.

Please allow reasonable time to investigate before public disclosure. We will
acknowledge the report, assess its impact, and coordinate remediation and
disclosure with the reporter.

## Security model

`droidperm` executes the user's installed `adb` binary and sends commands to a
device the user has authorized. It does not provide authentication, device
enrollment, rollback, or protection from a compromised host, ADB binary, or
Android device.

Policy files and captured output can reveal installed applications and security
choices. Treat them as potentially sensitive. Review `plan` before applying a
policy and avoid running untrusted policy files.

Only the latest released version receives security fixes.
