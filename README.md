# droidperm

`droidperm` keeps Android runtime permissions and AppOps in a reviewable YAML
file, then compares or applies that policy through ADB.

It is designed for personal devices and small device fleets where Android
Enterprise is too heavy, but a one-off shell script is too difficult to review
and reproduce.

> [!WARNING]
> Changing permissions and AppOps can break applications. Start with
> `capture`, commit the generated file, and inspect `plan` before running
> `apply`. Keep another way to recover the device.

## Why droidperm?

| Tool | Main interface | Git-friendly policy | AppOps |
| --- | --- | --- | --- |
| [Permission Manager X](https://github.com/mirfatif/PermissionManagerX) | On-device GUI | Limited | Strong |
| [App Manager profiles](https://github.com/MuntashirAkon/AppManager) | On-device GUI/profile | Partial | Strong |
| [Android Management API](https://developers.google.com/android/management/introduction) | Enterprise policy | Strong | Limited |
| droidperm | ADB CLI + YAML | Strong | Strong |

`droidperm` deliberately does less than general Android management tools. It
does not uninstall apps, block components, manage roles, require root, or run a
service on the device. Unmentioned permissions and AppOps are never changed.

## Requirements

- Android 10–16
- Android Platform Tools (`adb`) available on `PATH`
- USB debugging or wireless debugging enabled on the target device

ADB is not bundled. OEM builds can expose different AppOps or restrict ADB
permission changes; see [compatibility notes](docs/compatibility.md).

## Install

With Nix:

```sh
nix run github:yutakobayashidev/droidperm
```

To install it into your profile instead:

```sh
nix profile install github:yutakobayashidev/droidperm
```

With Go 1.25 or newer:

```sh
go install github.com/yutakobayashidev/droidperm/cmd/droidperm@latest
```

Or build the current checkout:

```sh
go build -o droidperm ./cmd/droidperm
```

## Quick start

Capture selected packages from an existing device:

```sh
droidperm capture \
  --package com.instagram.android \
  --package org.telegram.messenger \
  > droidperm.yaml
```

`capture` creates a deterministic starting point, not a complete backup. By
default it includes runtime permissions and restrictive AppOps that can be
observed through ADB. Use `--all-appops` when you also want observed `allow`
entries. Android does not always distinguish an explicit AppOps override from
usage-derived output, so review the generated policy before committing it.

Edit the YAML:

```yaml
version: 1

packages:
  com.instagram.android:
    permissions:
      android.permission.CAMERA: deny
      android.permission.RECORD_AUDIO: deny
      android.permission.POST_NOTIFICATIONS: deny
    appops:
      CAMERA: ignore
      READ_CLIPBOARD: ignore
      RECORD_AUDIO: ignore
  org.telegram.messenger:
    permissions:
      android.permission.CAMERA: allow
      android.permission.RECORD_AUDIO: allow
    appops:
      READ_CLIPBOARD: foreground
```

Validate, preview, and apply it:

```sh
droidperm validate
droidperm plan
droidperm apply
```

Only entries present in the YAML are managed. Removing an entry means “stop
managing it”; it does not reset that value on the device.

### Workflow: capture all third-party apps, then restrict AppOps

This repository includes a small Nushell script for a deliberately broad
workflow:

1. Capture the current state of every third-party package.
2. Preview the AppOps that are currently `allow`.
3. Reset each package's AppOps and change most allowed operations to `ignore`.
4. Capture the resulting restrictive policy.

The script changes AppOps only. It does not grant or revoke Android runtime
permissions. It calls `appops reset`, which removes existing AppOps overrides
before applying the restrictions, so this can disrupt applications even when
the final list looks reasonable.

> [!WARNING]
> Test this on a recoverable device first. A capture is a reviewable snapshot
> of observable state, not a complete Android backup, and observed `allow`
> entries can be usage-derived.

The workflow uses the repository checkout because the helper remains separate
from the main CLI. Enter the development shell, start Nushell, and confirm that
ADB can see exactly one authorized device:

```sh
nix develop
nu
```

```nu
adb devices -l
```

The remaining commands in this section run inside that Nushell session. This
workflow assumes Android user `0`; the helper does not expose the CLI's
`--user` option.

First, collect a deterministic list of third-party packages:

```nu
let packages = (
  adb shell pm list packages -3
  | lines
  | parse 'package:{package}'
  | get package
  | sort
)

$packages | length
```

Choose packages that must be excluded from the reset, then derive the package
list that will be managed by the resulting policy:

```nu
let excluded = [
  # 'com.example.one'
  # 'com.example.two'
]
let excluded_arg = ($excluded | str join ',')
let managed_packages = $packages | where {|package| $package not-in $excluded }
```

Capture the starting state. `--all-appops` includes observed `allow` and
`default` entries so the before-policy is useful when reviewing what changed:

```nu
droidperm capture --package ($packages | str join ',') --all-appops --output droidperm.before.yaml
droidperm validate --file droidperm.before.yaml
```

Run the helper without `--apply` to list the currently allowed AppOps without
changing the device:

```nu
nu scripts/restrict-third-party-appops.nu --exclude $excluded_arg
```

To keep a reviewable report instead of printing it to the terminal:

```nu
nu scripts/restrict-third-party-appops.nu --exclude $excluded_arg
| save --force appops-dry-run.txt
```

This dry run describes the pre-reset state, not an exact change plan. During
`--apply`, the script resets a package before reading its allowed AppOps, so
the set of operations can differ after reset. The script keeps
`AUDIO_MEDIA_VOLUME`, `START_FOREGROUND`, `TAKE_AUDIO_FOCUS`, `TOAST_WINDOW`,
`WAKE_LOCK`, `WRITE_CLIPBOARD`, and `READ_MEDIA_IMAGES` allowed; it changes
other allowed operations to `ignore`.

After reviewing the current state and allowlist, apply the reset and
restrictions:

```nu
nu scripts/restrict-third-party-appops.nu --apply --exclude $excluded_arg
```

Capture the resulting state with `--all-appops` so it has the same scope as the
starting capture, then compare them:

```nu
droidperm capture --package ($packages | str join ',') --all-appops --output droidperm.after.yaml
droidperm validate --file droidperm.after.yaml
git diff --no-index -- droidperm.before.yaml droidperm.after.yaml
```

`git diff --no-index` exits `1` when it finds differences; that is expected.

Finally, create the policy you intend to manage without `--all-appops`.
This keeps runtime-permission state and restrictive AppOps while intentionally
leaving observed allowed/default AppOps unmanaged. It uses `managed_packages`
so excluded packages do not later become managed through this policy:

```nu
droidperm capture --package ($managed_packages | str join ',') --output droidperm.yaml
droidperm validate --file droidperm.yaml
droidperm check --file droidperm.yaml
```

Checking hundreds of packages can take several minutes because every value is
read back through ADB. Capture refuses to replace an existing file; on a repeat
run, choose new filenames or pass `--force` only after checking the target.

If an application breaks, inspect the starting policy before attempting to
restore its observed values:

```nu
droidperm plan --file droidperm.before.yaml
droidperm apply --file droidperm.before.yaml
```

Applying the before-policy is not a guaranteed full rollback: Android may have
changed usage-derived state, and values that were not observable were never
captured. All generated policies reveal the device's package inventory, so
review them before deciding whether to commit any of them. Runtime permissions
and some AppOps can also affect another package with the same shared UID; see
the [compatibility notes](docs/compatibility.md).

## Commands

```text
droidperm capture  --package PACKAGE [--package PACKAGE...] [-o FILE]
droidperm validate [-f FILE]
droidperm plan     [-f FILE] [-s SERIAL] [--user USER] [--json]
droidperm check    [-f FILE] [-s SERIAL] [--user USER] [--json]
droidperm apply    [-f FILE] [-s SERIAL] [--user USER] [--yes] [--json]
```

- `capture` reads selected installed packages and emits stable, sorted YAML.
  Existing output files require `--force` before they are overwritten.
- `validate` checks the YAML without connecting to a device.
- `plan` prints the difference between the device and the desired state without
  changing anything.
- `check` is the CI-friendly drift check. It exits `0` when converged and `3`
  when drift exists.
- `apply` performs preflight checks, asks for confirmation on a terminal,
  applies runtime permissions followed by AppOps, and verifies the result.
  Non-interactive use requires `--yes`.

The default file is `droidperm.yaml`. Select a device with `--serial` or
`ANDROID_SERIAL`; if exactly one device is connected, it is selected
automatically. User `0` is the default.

Exit codes are `0` for success, `1` for ADB/device/application failure, `2` for
invalid CLI input or policy, `3` for drift, and `130` for interruption. Machine
consumers should use `--json`; human-readable output may evolve.

## Permission and AppOps modes

Runtime permissions accept `allow` or `deny`. AppOps accept `allow`, `ignore`,
`deny`, `default`, or `foreground`.

Runtime permissions and AppOps are separate Android layers. A denied runtime
permission can still have a related AppOp, and changing a permission can alter
that AppOp. Consequently, `droidperm` applies runtime permissions first and
AppOps second.

Prefer `ignore` for a quiet AppOps rejection. `deny` can make an app receive a
security exception and crash. `default` delegates to Android's operation-
specific default; it does not always mean `deny`. See
[AppOps safety](docs/appops.md).

## Development

Enter the Nix development shell or use a local Go 1.25 toolchain:

```sh
nix develop
go test ./...
go vet ./...
go build ./...
```

Contributions are welcome; read [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md) first.

## License

Apache License 2.0. See [LICENSE](LICENSE).
