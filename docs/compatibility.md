# Android compatibility

`droidperm` targets Android 10 through Android 16 (API levels 29–36) and invokes
the Android Platform Tools `adb` installed on the host.

| Area | Expected behavior |
| --- | --- |
| Runtime permissions | Uses Android package-manager grant/revoke commands |
| AppOps | Uses operation names accepted by the device's `cmd appops` |
| Multiple devices | Requires an explicit serial unless exactly one is connected |
| Android users | User `0` by default; v1 accepts a numeric user |
| Root | Not required and not used |
| OEM extensions | Detected only when exposed through standard ADB commands |

## Important limitations

- OEM firmware can remove, rename, restrict, or add AppOps.
- Android can refuse changes to fixed, signature, privileged, policy-managed,
  or otherwise non-runtime permissions.
- Some AppOps apply to a UID rather than independently to one package. Shared
  UIDs can therefore affect more than the named package. droidperm rejects
  ambiguous cases instead of silently broadening the change.
- AppOps output is not a stable serialization format and can contain
  usage-history fields. `capture` produces a policy draft, not a lossless
  backup.
- Device-owner policies, work profiles, roles, special access screens, and
  vendor security layers are outside the v1 scope.

Before applying a policy to a new Android or OEM release, run `capture` and
`plan` on a non-critical device. Please report reproducible compatibility
problems with the Android version, OEM/build identifier, Platform Tools version,
sanitized command output, and the expected behavior.
