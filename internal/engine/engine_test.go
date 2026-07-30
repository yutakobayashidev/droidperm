package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type fakeDevice struct {
	permissions          map[string]policy.PermissionMode
	appOps               map[string]policy.AppOpMode
	calls                []string
	fail                 string
	inspectCalls         map[string]int
	validateCalls        map[string]int
	permissionSideEffect func(*fakeDevice, string, string)
	appOpSideEffect      func(*fakeDevice, string, string)
	setCount             int
}

func (f *fakeDevice) key(pkg, name string) string { return pkg + "/" + name }

func (f *fakeDevice) InspectPackage(_ context.Context, pkg string) (Snapshot, error) {
	if f.inspectCalls == nil {
		f.inspectCalls = make(map[string]int)
	}
	f.inspectCalls[pkg]++
	if f.fail == "inspect:"+pkg {
		return Snapshot{}, errors.New("read failed")
	}
	snapshot := Snapshot{
		Permissions: make(map[string]policy.PermissionMode),
		AppOps:      make(map[string]policy.AppOpMode),
	}
	prefix := pkg + "/"
	for key, mode := range f.permissions {
		if strings.HasPrefix(key, prefix) {
			snapshot.Permissions[strings.TrimPrefix(key, prefix)] = mode
		}
	}
	for key, mode := range f.appOps {
		if strings.HasPrefix(key, prefix) {
			snapshot.AppOps[strings.TrimPrefix(key, prefix)] = mode
		}
	}
	return snapshot, nil
}

func (f *fakeDevice) ValidateAppOp(_ context.Context, pkg, name string) error {
	if f.validateCalls == nil {
		f.validateCalls = make(map[string]int)
	}
	f.validateCalls[name]++
	if f.fail == "validate:"+f.key(pkg, name) {
		return errors.New("unknown AppOp")
	}
	return nil
}

func (f *fakeDevice) SetPermission(_ context.Context, pkg, name string, mode policy.PermissionMode) error {
	f.setCount++
	if f.fail == fmt.Sprintf("set:%d", f.setCount) {
		return errors.New("write failed")
	}
	f.calls = append(f.calls, "permission:"+f.key(pkg, name))
	f.permissions[f.key(pkg, name)] = mode
	if f.permissionSideEffect != nil {
		f.permissionSideEffect(f, pkg, name)
	}
	return nil
}

func (f *fakeDevice) SetAppOp(_ context.Context, pkg, name string, mode policy.AppOpMode) error {
	f.setCount++
	if f.fail == fmt.Sprintf("set:%d", f.setCount) {
		return errors.New("write failed")
	}
	f.calls = append(f.calls, "appop:"+f.key(pkg, name))
	f.appOps[f.key(pkg, name)] = mode
	if f.appOpSideEffect != nil {
		f.appOpSideEffect(f, pkg, name)
	}
	return nil
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

	plan, err := BuildPlan(context.Background(), device, desired, nil)
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

	second, err := BuildPlan(context.Background(), device, desired, nil)
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
		fail:        "validate:com.example/CAMERA",
	}
	desired := policy.File{
		Version: 1,
		Packages: map[string]policy.Package{
			"com.example": {AppOps: map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore}},
		},
	}
	_, err := BuildPlan(context.Background(), device, desired, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildPlanInspectsEachPackageOnceAndValidatesDistinctAppOpsOnce(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.a/android.permission.CAMERA":       policy.PermissionDeny,
			"com.a/android.permission.RECORD_AUDIO": policy.PermissionDeny,
		},
		appOps: map[string]policy.AppOpMode{},
	}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.a": {
			Permissions: map[string]policy.PermissionMode{
				"android.permission.CAMERA":       policy.PermissionDeny,
				"android.permission.RECORD_AUDIO": policy.PermissionAllow,
			},
			AppOps: map[string]policy.AppOpMode{
				"CAMERA":       policy.AppOpDefault,
				"RECORD_AUDIO": policy.AppOpIgnore,
			},
		},
	}}

	plan, err := BuildPlan(context.Background(), device, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if device.inspectCalls["com.a"] != 1 {
		t.Fatalf("InspectPackage calls = %d, want 1", device.inspectCalls["com.a"])
	}
	if device.validateCalls["CAMERA"] != 1 || device.validateCalls["RECORD_AUDIO"] != 1 {
		t.Fatalf("ValidateAppOp calls = %#v", device.validateCalls)
	}
	if plan.Actions[2].Current != "default" {
		t.Fatalf("omitted valid AppOp current = %q, want default", plan.Actions[2].Current)
	}
}

func TestBuildPlanReturnsTypedRequestedPermissionError(t *testing.T) {
	device := &fakeDevice{permissions: map[string]policy.PermissionMode{}, appOps: map[string]policy.AppOpMode{}}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.example": {Permissions: map[string]policy.PermissionMode{
			"android.permission.READ_MEDIA_AUDIO": policy.PermissionDeny,
		}},
	}}

	plan, err := BuildPlan(context.Background(), device, desired, nil)
	var target *RequestedPermissionError
	if !errors.As(err, &target) {
		t.Fatalf("error = %v, want RequestedPermissionError", err)
	}
	if target.Package != "com.example" || target.Permission != "android.permission.READ_MEDIA_AUDIO" {
		t.Fatalf("typed error = %#v", target)
	}
	if plan.Writes != 0 || !strings.Contains(err.Error(), "remove it from the policy or recapture") {
		t.Fatalf("plan = %#v, error = %v", plan, err)
	}
}

func TestApplyUsesGlobalPermissionThenAppOpOrder(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.a/android.permission.CAMERA": policy.PermissionAllow,
			"com.b/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{
			"com.a/CAMERA": policy.AppOpAllow,
			"com.b/CAMERA": policy.AppOpAllow,
		},
	}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.a": {
			Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny},
			AppOps:      map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore},
		},
		"com.b": {
			Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny},
			AppOps:      map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore},
		},
	}}
	plan, err := BuildPlan(context.Background(), device, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), device, plan); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"permission:com.a/android.permission.CAMERA",
		"permission:com.b/android.permission.CAMERA",
		"appop:com.a/CAMERA",
		"appop:com.b/CAMERA",
	}
	if !reflect.DeepEqual(device.calls, want) {
		t.Fatalf("calls = %#v, want %#v", device.calls, want)
	}
}

func TestApplyRepairsAppOpChangedByPermission(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.example/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{"com.example/CAMERA": policy.AppOpIgnore},
		permissionSideEffect: func(f *fakeDevice, pkg, _ string) {
			f.appOps[f.key(pkg, "CAMERA")] = policy.AppOpAllow
		},
	}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.example": {
			Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny},
			AppOps:      map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore},
		},
	}}
	plan, err := BuildPlan(context.Background(), device, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Actions[1].Status != StatusUnchanged {
		t.Fatalf("preflight AppOp = %#v, want unchanged", plan.Actions[1])
	}
	if _, err := Apply(context.Background(), device, plan); err != nil {
		t.Fatal(err)
	}
	if got := device.appOps["com.example/CAMERA"]; got != policy.AppOpIgnore {
		t.Fatalf("AppOp = %q, want ignore", got)
	}
}

func TestApplyFinalVerificationDetectsSharedUIDSideEffect(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{},
		appOps: map[string]policy.AppOpMode{
			"com.a/CAMERA": policy.AppOpAllow,
			"com.b/CAMERA": policy.AppOpAllow,
		},
		appOpSideEffect: func(f *fakeDevice, pkg, _ string) {
			if pkg == "com.b" {
				f.appOps["com.a/CAMERA"] = policy.AppOpAllow
			}
		},
	}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.a": {AppOps: map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore}},
		"com.b": {AppOps: map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore}},
	}}
	plan, err := BuildPlan(context.Background(), device, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), device, plan)
	if err == nil || !strings.Contains(err.Error(), "final verification") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if result.Failure == nil || result.Failure.Package != "com.a" {
		t.Fatalf("failure = %#v", result.Failure)
	}
}

func TestApplyReportsPartialResultAtSecondAction(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.a/android.permission.CAMERA": policy.PermissionAllow,
			"com.b/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{},
		fail:   "set:2",
	}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.a": {Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny}},
		"com.b": {Permissions: map[string]policy.PermissionMode{"android.permission.CAMERA": policy.PermissionDeny}},
	}}
	plan, err := BuildPlan(context.Background(), device, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), device, plan)
	if err == nil {
		t.Fatal("expected error")
	}
	if result.Applied != 1 || result.Writes != 1 || result.Pending != 0 {
		t.Fatalf("partial result = %#v", result)
	}
	if result.Failure == nil || result.Failure.Package != "com.b" {
		t.Fatalf("failure = %#v", result.Failure)
	}
}

func TestCaptureDeduplicatesPackages(t *testing.T) {
	device := &fakeDevice{
		permissions: map[string]policy.PermissionMode{
			"com.example/android.permission.CAMERA": policy.PermissionDeny,
		},
		appOps: map[string]policy.AppOpMode{},
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
	got, err := Capture(context.Background(), device, []string{"com.empty"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 0 {
		t.Fatalf("packages = %#v, want none", got.Packages)
	}
}
