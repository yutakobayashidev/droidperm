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

### Bulk AppOps restriction

The included Nushell script discovers all third-party packages and prints their
currently allowed AppOps:

```sh
nix develop -c nu scripts/restrict-third-party-appops.nu
```

This is a dry run and does not change the device. Pass `--apply` to reset each
package's AppOps and change allowed entries to `ignore`, except for the small
allowlist defined in the script:

```sh
nix develop -c nu scripts/restrict-third-party-appops.nu --apply
```

This is a broad device change, so review the dry run first. Capture the result
separately with `droidperm capture`; the script intentionally contains no
policy or output-file orchestration. Packages that should not be touched can be
passed as a comma-separated `--exclude` value. From the development shell:

```nu
let packages = (
  adb shell pm list packages -3
  | lines
  | str replace 'package:' ''
  | str join ','
)
droidperm capture --package $packages --output droidperm.yaml
```

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
