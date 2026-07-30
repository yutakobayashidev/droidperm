# AppOps safety

Android AppOps are a separate enforcement layer from runtime permissions. They
are implementation-level controls, can vary between Android versions, and are
not a substitute for the permission model presented to users.

For the underlying model, see the
[AOSP AppOps documentation](https://android.googlesource.com/platform/frameworks/base/+/master/core/java/android/app/AppOps.md).

## Modes

| Policy value | General meaning | Guidance |
| --- | --- | --- |
| `allow` | Permit the operation | Use only when explicitly desired |
| `ignore` | Reject quietly or return an empty/default result | Preferred denial mode for most apps |
| `deny` | Reject with an error or security exception | May crash apps; use carefully |
| `default` | Delegate to the operation's Android-defined default | Does not universally mean allow or deny |
| `foreground` | Allow only in an Android-defined foreground state | State semantics vary by operation/version |

The exact effect is operation- and Android-version-specific. A mode being
accepted by `cmd appops` does not guarantee the application behaves safely.

## Applying a policy safely

1. Capture only the packages you intend to manage.
2. Commit or otherwise save the current generated YAML.
3. Make a small policy edit.
4. Run `droidperm plan` and inspect every change.
5. Apply while the device is recoverable.
6. Launch and exercise the affected application.

`droidperm` never resets all AppOps and never changes an unmentioned operation.
Preflight is fail-closed and performs no writes if any selected policy entry
cannot be inspected. It applies every runtime permission change first, reads
the selected packages' AppOps again to observe permission side effects, then
applies all explicitly declared AppOps that now differ.

Android offers no transaction spanning these commands. If an operation fails,
`droidperm` stops and reports the verified actions, failed action, write count,
and pending actions. It does not attempt an automatic rollback; rerunning the
same command resumes from the device's current state. After all writes, it
reads every selected package again and verifies the complete selected policy,
so shared-UID or other cross-action side effects cannot be reported as success.
