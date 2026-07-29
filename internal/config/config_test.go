package config_test

import (
	"strings"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/config"
	"github.com/yutakobayashidev/droidperm/internal/policy"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	input := `
version: 1
packages:
  com.example.app:
    permissions:
      android.permission.CAMERA: deny
    appops:
      CAMERA: ignore
`

	got, err := config.Load(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != policy.Version {
		t.Errorf("Version = %d, want %d", got.Version, policy.Version)
	}
	pkg := got.Packages["com.example.app"]
	if pkg.Permissions["android.permission.CAMERA"] != policy.PermissionDeny {
		t.Errorf("permission mode = %q, want %q", pkg.Permissions["android.permission.CAMERA"], policy.PermissionDeny)
	}
	if pkg.AppOps["CAMERA"] != policy.AppOpIgnore {
		t.Errorf("appop mode = %q, want %q", pkg.AppOps["CAMERA"], policy.AppOpIgnore)
	}
}

func TestLoadRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty document":       "",
		"missing version":      "packages: {}\n",
		"wrong version":        "version: 2\npackages: {}\n",
		"unknown root field":   "version: 1\nunknown: true\n",
		"unknown nested field": "version: 1\npackages:\n  example:\n    unknown: {}\n",
		"duplicate root key":   "version: 1\nversion: 1\n",
		"duplicate nested key": "version: 1\npackages:\n  example:\n    permissions:\n      CAMERA: allow\n      CAMERA: deny\n",
		"blank package":        "version: 1\npackages:\n  ' ': {appops: {CAMERA: allow}}\n",
		"empty package":        "version: 1\npackages:\n  example: {}\n",
		"blank permission":     "version: 1\npackages:\n  example:\n    permissions:\n      ' ': allow\n",
		"invalid permission":   "version: 1\npackages:\n  example:\n    permissions:\n      CAMERA: foreground\n",
		"blank appop":          "version: 1\npackages:\n  example:\n    appops:\n      ' ': allow\n",
		"invalid appop":        "version: 1\npackages:\n  example:\n    appops:\n      CAMERA: ask\n",
		"multiple documents":   "version: 1\npackages: {}\n---\nversion: 1\npackages: {}\n",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := config.Load(strings.NewReader(input)); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadAcceptsEveryMode(t *testing.T) {
	t.Parallel()

	input := `
version: 1
packages:
  example:
    permissions:
      ALLOW: allow
      DENY: deny
    appops:
      ALLOW: allow
      IGNORE: ignore
      DENY: deny
      DEFAULT: default
      FOREGROUND: foreground
`

	if _, err := config.Load(strings.NewReader(input)); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	t.Parallel()

	file := &policy.File{
		Version: 1,
		Packages: map[string]policy.Package{
			"z.package": {
				AppOps: map[string]policy.AppOpMode{
					"Z_OP": policy.AppOpIgnore,
					"A_OP": policy.AppOpAllow,
				},
			},
			"a.package": {
				Permissions: map[string]policy.PermissionMode{
					"z.permission": policy.PermissionDeny,
					"a.permission": policy.PermissionAllow,
				},
			},
		},
	}

	got, err := config.Marshal(file)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `version: 1
packages:
    a.package:
        permissions:
            a.permission: allow
            z.permission: deny
    z.package:
        appops:
            A_OP: allow
            Z_OP: ignore
`
	if string(got) != want {
		t.Errorf("Marshal() =\n%s\nwant:\n%s", got, want)
	}

	roundTrip, err := config.Load(strings.NewReader(string(got)))
	if err != nil {
		t.Fatalf("Load(Marshal()) error = %v", err)
	}
	if roundTrip.Packages["a.package"].Permissions["a.permission"] != policy.PermissionAllow {
		t.Error("round trip lost permission")
	}
}

func TestMarshalRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	_, err := config.Marshal(&policy.File{
		Version: 1,
		Packages: map[string]policy.Package{
			"example": {},
		},
	})
	if err == nil {
		t.Fatal("Marshal() error = nil, want error")
	}
}
