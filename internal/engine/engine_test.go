package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type fakeDevice struct {
	permissions map[string]policy.PermissionMode
	appOps      map[string]policy.AppOpMode
	calls       []string
	fail        string
}

func (f *fakeDevice) key(pkg, name string) string { return pkg + "/" + name }

func (f *fakeDevice) Permission(_ context.Context, pkg, name string) (policy.PermissionMode, error) {
	if f.fail == f.key(pkg, name) {
		return "", errors.New("read failed")
	}
	return f.permissions[f.key(pkg, name)], nil
}

func (f *fakeDevice) AppOp(_ context.Context, pkg, name string) (policy.AppOpMode, error) {
	if f.fail == f.key(pkg, name) {
		return "", errors.New("read failed")
	}
	return f.appOps[f.key(pkg, name)], nil
}

func (f *fakeDevice) SetPermission(_ context.Context, pkg, name string, mode policy.PermissionMode) error {
	f.calls = append(f.calls, "permission:"+f.key(pkg, name))
	f.permissions[f.key(pkg, name)] = mode
	return nil
}

func (f *fakeDevice) SetAppOp(_ context.Context, pkg, name string, mode policy.AppOpMode) error {
	f.calls = append(f.calls, "appop:"+f.key(pkg, name))
	f.appOps[f.key(pkg, name)] = mode
	return nil
}

func (f *fakeDevice) Capture(_ context.Context, pkg string, _ bool) (Snapshot, error) {
	return Snapshot{
		Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny},
		AppOps:      map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore},
	}, nil
}

func TestBuildPlanIsDeterministicAndPermissionsComeFirst(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.example/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{"com.example/CAMERA": policy.AppOpAllow},
	}
	desired := policy.File{
		Version: 1,
		Packages: map[string]policy.Package{
			"com.example": {
				Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny},
				AppOps:      map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore},
			},
		},
	}

	plan, err := BuildPlan(context.Background(), device, desired)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 2 {
		t.Fatalf("changes = %d, want 2", plan.Changes)
	}
	if plan.Actions[0].Kind != KindPermission || plan.Actions[1].Kind != KindAppOp {
		t.Fatalf("unexpected order: %#v", plan.Actions)
	}

	applied, err := Apply(context.Background(), device, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(device.calls, []string{
		"permission:com.example/android.permission.CAMERA",
		"appop:com.example/CAMERA",
	}) {
		t.Fatalf("calls = %#v", device.calls)
	}
	for _, action := range applied.Actions {
		if action.Status != StatusApplied {
			t.Fatalf("action not applied: %#v", action)
		}
	}

	second, err := BuildPlan(context.Background(), device, desired)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changes != 0 {
		t.Fatalf("second plan changes = %d, want 0", second.Changes)
	}
}

func TestBuildPlanAggregatesPreflightErrors(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{},
		appOps:      map[string]policy.AppOpMode{},
		fail:        "com.example/CAMERA",
	}
	desired := policy.File{
		Version: 1,
		Packages: map[string]policy.Package{
			"com.example": {AppOps: map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore}},
		},
	}
	_, err := BuildPlan(context.Background(), device, desired)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCaptureDeduplicatesPackages(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{},
		appOps:      map[string]policy.AppOpMode{},
	}
	got, err := Capture(context.Background(), device, []string{"com.example", "com.example"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 1 {
		t.Fatalf("packages = %d, want 1", len(got.Packages))
	}
}

func TestCaptureOmitsPackagesWithoutManagedState(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{},
		appOps:      map[string]policy.AppOpMode{},
	}
	deviceCapture := emptyCaptureDevice{fakeDevice: device}
	got, err := Capture(context.Background(), deviceCapture, []string{"com.empty"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 0 {
		t.Fatalf("packages = %#v, want none", got.Packages)
	}
}

type emptyCaptureDevice struct{ *fakeDevice }

func (emptyCaptureDevice) Capture(context.Context, string, bool) (Snapshot, error) {
	return Snapshot{}, nil
}
