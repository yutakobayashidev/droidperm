#!/usr/bin/env nu

const kept_appops = [
  AUDIO_MEDIA_VOLUME
  START_FOREGROUND
  TAKE_AUDIO_FOCUS
  TOAST_WINDOW
  WAKE_LOCK
  WRITE_CLIPBOARD
  READ_MEDIA_IMAGES
]

def main [--apply, --exclude: string = ""] {
  let excluded = $exclude | split row "," | where { not ($in | is-empty) }
  let packages = (
    ^adb shell pm list packages -3
    | lines
    | parse 'package:{package}'
    | get package
    | where {|package| $package not-in $excluded }
    | sort
  )

  for package in $packages {
    print $"\n($package)"
    if $apply {
      ^adb shell cmd appops reset $package
    }

    let appops = (
      ^adb shell cmd appops get $package
      | lines
      | parse --regex '^\s*(?:Uid mode:\s*)?(?<op>[A-Z][A-Z0-9_]*)(?:\s+\([^)]*\))?:\s*allow\b'
      | get op
      | uniq
      | sort
    )
    for appop in $appops {
      print $appop
      if $apply and $appop not-in $kept_appops {
        ^adb shell cmd appops set $package $appop ignore
      }
    }
  }

  if not $apply {
    print "\nDry run only. Run again with --apply to reset and update AppOps."
  }
}
