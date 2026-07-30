package device

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/adb"
	"github.com/yutakobayashidev/droidperm/internal/engine"
	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type countingRunner struct {
	calls   [][]string
	unknown string
}

func (r *countingRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{path}, args...))
	command := args[3:]
	if len(command) >= 3 && command[0] == "dumpsys" && command[1] == "package" {
		var out strings.Builder
		fmt.Fprintf(&out, "Package [%s]:\n  requested permissions:\n", command[2])
		for i := 0; i < 7; i++ {
			fmt.Fprintf(&out, "    android.permission.TEST_%d\n", i)
		}
		out.WriteString("  User 0:\n    runtime permissions:\n")
		for i := 0; i < 7; i++ {
			fmt.Fprintf(&out, "      android.permission.TEST_%d: granted=false\n", i)
		}
		return []byte(out.String()), nil
	}
	if len(command) >= 6 && command[0] == "cmd" && command[1] == "appops" && command[2] == "get" {
		if len(command) == 7 && command[6] == r.unknown {
			return []byte("Error: Unknown operation string: " + r.unknown + "\n"), nil
		}
		return []byte("No operations.\n"), nil
	}
	return nil, fmt.Errorf("unexpected adb call: %v", args)
}

func TestModeValidationHappensBeforeADBWrite(t *testing.T) {
	device := &androidDevice{}
	if err := device.SetPermission(context.Background(), "pkg", "perm", "ask"); err == nil {
		t.Fatal("SetPermission accepted an invalid mode")
	}
	if err := device.SetAppOp(context.Background(), "pkg", "op", policy.AppOpMode("ask")); err == nil {
		t.Fatal("SetAppOp accepted an invalid mode")
	}
}

func TestInspectPackageBatchesPermissionsAndAppOps(t *testing.T) {
	runner := &countingRunner{}
	dev := &androidDevice{client: adb.NewWithRunner("adb", "serial", 0, runner)}
	snapshot, err := dev.InspectPackage(context.Background(), "com.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Permissions) != 7 || len(runner.calls) != 2 {
		t.Fatalf("snapshot = %#v, calls = %d", snapshot, len(runner.calls))
	}
}

func TestBuildPlanDoesNotTreatUnknownAppOpAsDefault(t *testing.T) {
	runner := &countingRunner{unknown: "FUTURE_OP"}
	dev := &androidDevice{client: adb.NewWithRunner("adb", "serial", 0, runner)}
	desired := policy.File{Version: 1, Packages: map[string]policy.Package{
		"com.example": {AppOps: map[string]policy.AppOpMode{
			"FUTURE_OP": policy.AppOpDefault,
		}},
	}}
	plan, err := engine.BuildPlan(context.Background(), dev, desired, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown AppOp") {
		t.Fatalf("plan = %#v, error = %v", plan, err)
	}
	if plan.Changes != 0 || plan.Writes != 0 {
		t.Fatalf("unknown AppOp became an action: %#v", plan)
	}
}

func TestLargePolicyBuildPlanADBCallBudget(t *testing.T) {
	const (
		packageCount    = 328
		permissionCount = 2196
		appOpCount      = 13728
		distinctAppOps  = 42
	)
	desired := policy.File{Version: 1, Packages: make(map[string]policy.Package, packageCount)}
	totalPermissions := 0
	totalAppOps := 0
	for i := 0; i < packageCount; i++ {
		pkg := policy.Package{
			Permissions: make(map[string]policy.PermissionMode),
			AppOps:      make(map[string]policy.AppOpMode),
		}
		permissions := 6
		if i < 228 {
			permissions++
		}
		for j := 0; j < permissions; j++ {
			pkg.Permissions[fmt.Sprintf("android.permission.TEST_%d", j)] = policy.PermissionDeny
			totalPermissions++
		}
		ops := 41
		if i < 280 {
			ops++
		}
		for j := 0; j < ops; j++ {
			pkg.AppOps[fmt.Sprintf("OP_%03d", j)] = policy.AppOpDefault
			totalAppOps++
		}
		desired.Packages[fmt.Sprintf("com.example.app%03d", i)] = pkg
	}
	if totalPermissions != permissionCount || totalAppOps != appOpCount {
		t.Fatalf("fixture items = %d permissions, %d AppOps", totalPermissions, totalAppOps)
	}

	runner := &countingRunner{}
	dev := &androidDevice{client: adb.NewWithRunner("adb", "serial", 0, runner)}
	plan, err := engine.BuildPlan(context.Background(), dev, desired, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != permissionCount+appOpCount {
		t.Fatalf("actions = %d, want %d", len(plan.Actions), permissionCount+appOpCount)
	}
	wantCalls := packageCount*2 + distinctAppOps
	if len(runner.calls) != wantCalls {
		t.Fatalf("ADB calls = %d, want %d", len(runner.calls), wantCalls)
	}
	var dumpsys, packageAppOps, validation int
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		switch {
		case strings.Contains(joined, " shell dumpsys package "):
			dumpsys++
		case strings.Contains(joined, " shell cmd appops get ") && len(call) == 11:
			validation++
		case strings.Contains(joined, " shell cmd appops get "):
			packageAppOps++
		}
	}
	if dumpsys != packageCount || packageAppOps != packageCount || validation != distinctAppOps {
		t.Fatalf("calls: dumpsys=%d package-appops=%d validation=%d", dumpsys, packageAppOps, validation)
	}
}
