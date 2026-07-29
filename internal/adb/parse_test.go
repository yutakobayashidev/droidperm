package adb

import (
	"reflect"
	"testing"
)

func TestParsePackageState(t *testing.T) {
	output := `Packages:
  Package [com.example] (abc123):
    userId=10123
    requested permissions:
      android.permission.CAMERA
      android.permission.INTERNET
    install permissions:
      android.permission.INTERNET: granted=true
    User 0: ceDataInode=1
      runtime permissions:
        android.permission.CAMERA: granted=false, flags=[ USER_SET]
        android.permission.RECORD_AUDIO: granted=true, flags=[ USER_SET]
  Package [com.other] (def456):
    User 0:
      runtime permissions:
        android.permission.CAMERA: granted=true
`
	state, found := parsePackageState("com.example", 0, output)
	if !found {
		t.Fatal("package not found")
	}
	want := map[string]bool{
		"android.permission.CAMERA":       false,
		"android.permission.RECORD_AUDIO": true,
	}
	if !reflect.DeepEqual(state.Permissions, want) {
		t.Fatalf("permissions = %#v, want %#v", state.Permissions, want)
	}
}

func TestParsePackageStateWithSharedUID(t *testing.T) {
	output := `Packages:
  Package [com.example] (abc123):
    userId=10123
    sharedUserId=com.example.shared
    requested permissions:
      android.permission.CAMERA
Shared users:
  SharedUser [com.example.shared] (def456):
    User 0:
      runtime permissions:
        android.permission.CAMERA: granted=true, flags=[ USER_SET]
`
	state, found := parsePackageState("com.example", 0, output)
	if !found {
		t.Fatal("package not found")
	}
	if !state.Permissions["android.permission.CAMERA"] {
		t.Fatalf("permissions = %#v", state.Permissions)
	}
}

func TestParsePackageStateSeparatesUsers(t *testing.T) {
	output := `Packages:
  Package [com.example] (abc123):
    requested permissions:
      android.permission.CAMERA
    User 0: installed=true
      runtime permissions:
        android.permission.CAMERA: granted=false, flags=[ USER_SET]
    User 10: installed=true
      runtime permissions:
        android.permission.CAMERA: granted=true, flags=[ USER_SET]
`
	user0, found := parsePackageState("com.example", 0, output)
	if !found || user0.Permissions["android.permission.CAMERA"] {
		t.Fatalf("user 0 state = %#v, found = %v", user0, found)
	}
	user10, found := parsePackageState("com.example", 10, output)
	if !found || !user10.Permissions["android.permission.CAMERA"] {
		t.Fatalf("user 10 state = %#v, found = %v", user10, found)
	}
}

func TestParseScopedPackageWithoutHeader(t *testing.T) {
	output := `  userId=10123
  requested permissions:
    android.permission.CAMERA
  User 0: installed=true
    runtime permissions:
      android.permission.CAMERA: granted=true, flags=[ USER_SET]
`
	state, found := parsePackageState("com.example", 0, output)
	if !found {
		t.Fatal("package not found")
	}
	if !state.Permissions["android.permission.CAMERA"] {
		t.Fatalf("scoped permissions = %#v", state.Permissions)
	}
}

func TestParseAppOps(t *testing.T) {
	output := `Uid mode: WAKE_LOCK: allow
  CAMERA: ignore; time=+1h2m3s ago
  RECORD_AUDIO (android:record_audio): foreground
  READ_CLIPBOARD: errored
  location: allow
`
	got := parseAppOps(output)
	want := map[string]string{
		"WAKE_LOCK":      "allow",
		"CAMERA":         "ignore",
		"RECORD_AUDIO":   "foreground",
		"READ_CLIPBOARD": "deny",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseAppOps() = %#v, want %#v", got, want)
	}
}
