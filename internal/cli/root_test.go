package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/device"
	"github.com/yutakobayashidev/droidperm/internal/engine"
	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type cliDevice struct {
	permissions map[string]policy.PermissionMode
	appOps      map[string]policy.AppOpMode
	inspected   []string
	setCalls    int
	writes      int
	failSetAt   int
	inspectErr  error
}

func (d *cliDevice) key(pkg, name string) string { return pkg + "/" + name }

func (d *cliDevice) InspectPackage(_ context.Context, pkg string) (engine.Snapshot, error) {
	d.inspected = append(d.inspected, pkg)
	if d.inspectErr != nil {
		return engine.Snapshot{}, d.inspectErr
	}
	snapshot := engine.Snapshot{
		Permissions: make(map[string]policy.PermissionMode),
		AppOps:      make(map[string]policy.AppOpMode),
	}
	prefix := pkg + "/"
	for key, mode := range d.permissions {
		if strings.HasPrefix(key, prefix) {
			snapshot.Permissions[strings.TrimPrefix(key, prefix)] = mode
		}
	}
	for key, mode := range d.appOps {
		if strings.HasPrefix(key, prefix) {
			snapshot.AppOps[strings.TrimPrefix(key, prefix)] = mode
		}
	}
	return snapshot, nil
}

func (d *cliDevice) ValidateAppOp(context.Context, string, string) error {
	return nil
}

func (d *cliDevice) SetPermission(_ context.Context, pkg, name string, mode policy.PermissionMode) error {
	d.setCalls++
	if d.setCalls == d.failSetAt {
		return errors.New("write failed")
	}
	d.writes++
	d.permissions[d.key(pkg, name)] = mode
	return nil
}

func (d *cliDevice) SetAppOp(_ context.Context, pkg, name string, mode policy.AppOpMode) error {
	d.setCalls++
	if d.setCalls == d.failSetAt {
		return errors.New("write failed")
	}
	d.writes++
	d.appOps[d.key(pkg, name)] = mode
	return nil
}

func TestValidateAndCheckExitCodes(t *testing.T) {
	policyPath := writeTestPolicy(t)
	dev := newCLIDevice()

	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "validate"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "is valid") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	command = newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "check"})
	err := command.Execute()
	if ExitCode(err) != 3 {
		t.Fatalf("ExitCode(check) = %d, error = %v", ExitCode(err), err)
	}
	if !strings.Contains(stdout.String(), "1 change(s)") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestApplyJSONIsOneDocumentAndConverges(t *testing.T) {
	policyPath := writeTestPolicy(t)
	dev := newCLIDevice()
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "--json", "apply", "--yes"})

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	got := decodeSinglePlan(t, stdout.Bytes())
	if got.Changes != 1 || got.Actions[0].Status != engine.StatusApplied {
		t.Fatalf("result = %#v", got)
	}

	plan, err := engine.BuildPlan(context.Background(), dev, policy.File{
		Version: policy.Version,
		Packages: map[string]policy.Package{
			"com.example": {
				Permissions: map[string]policy.PermissionMode{
					"android.permission.CAMERA": policy.PermissionDeny,
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 0 {
		t.Fatalf("post-apply changes = %d", plan.Changes)
	}
}

func TestPackageSelectorLimitsInspectionAndWrites(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.selected:
    permissions:
      android.permission.CAMERA: deny
  com.stale:
    permissions:
      android.permission.READ_MEDIA_AUDIO: deny
`)
	dev := &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.selected/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{},
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{
		"--file", policyPath, "apply",
		"--package", "com.selected,com.selected",
		"--package", "com.selected", "--yes",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range dev.inspected {
		if pkg != "com.selected" {
			t.Fatalf("inspected unrelated package %q", pkg)
		}
	}
	if dev.writes != 1 {
		t.Fatalf("writes = %d, want 1", dev.writes)
	}
}

func TestNoSelectorStrictlyRejectsStalePermissionWithoutWrites(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.selected:
    permissions:
      android.permission.CAMERA: deny
  com.stale:
    permissions:
      android.permission.READ_MEDIA_AUDIO: deny
`)
	dev := &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.selected/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{},
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "--json", "apply", "--yes"})
	err := command.Execute()
	if ExitCode(err) != 1 || dev.writes != 0 {
		t.Fatalf("exit=%d writes=%d error=%v", ExitCode(err), dev.writes, err)
	}
	got := decodeSinglePlan(t, stdout.Bytes())
	if got.Writes != 0 || !strings.Contains(got.Error, "remove it from the policy or recapture") {
		t.Fatalf("result = %#v", got)
	}
}

func TestUnknownSelectedPackageIsUsageErrorBeforeOpeningDevice(t *testing.T) {
	policyPath := writeTestPolicy(t)
	opened := false
	var stdout, stderr bytes.Buffer
	command := newCommand(
		"test",
		bytes.NewReader(nil),
		&stdout,
		&stderr,
		func(context.Context, string, string, int) (engine.Device, device.Info, error) {
			opened = true
			return newCLIDevice(), device.Info{}, nil
		},
	)
	command.SetArgs([]string{"--file", policyPath, "plan", "--package", "com.missing"})
	err := command.Execute()
	if ExitCode(err) != 2 || opened {
		t.Fatalf("exit=%d opened=%v error=%v", ExitCode(err), opened, err)
	}
}

func TestSelectorDoesNotBypassWholePolicyValidation(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.selected:
    permissions:
      android.permission.CAMERA: deny
  com.invalid:
    appops:
      CAMERA: sometimes
`)
	opened := false
	var stdout, stderr bytes.Buffer
	command := newCommand(
		"test",
		bytes.NewReader(nil),
		&stdout,
		&stderr,
		func(context.Context, string, string, int) (engine.Device, device.Info, error) {
			opened = true
			return newCLIDevice(), device.Info{}, nil
		},
	)
	command.SetArgs([]string{"--file", policyPath, "plan", "--package", "com.selected"})
	err := command.Execute()
	if ExitCode(err) != 2 || opened {
		t.Fatalf("exit=%d opened=%v error=%v", ExitCode(err), opened, err)
	}
}

func TestCheckPackageOnlyJudgesSelectedScope(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.clean:
    permissions:
      android.permission.CAMERA: deny
  com.drift:
    permissions:
      android.permission.CAMERA: deny
`)
	dev := &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.clean/android.permission.CAMERA": policy.PermissionDeny,
			"com.drift/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{},
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "check", "--package", "com.clean"})
	if err := command.Execute(); err != nil {
		t.Fatalf("selected clean scope: %v", err)
	}

	stdout.Reset()
	command = newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "check", "--package", "com.drift"})
	if err := command.Execute(); ExitCode(err) != 3 {
		t.Fatalf("selected drift exit=%d error=%v", ExitCode(err), err)
	}
}

func TestApplyJSONPartialFailureIsOneDocument(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.a:
    permissions:
      android.permission.CAMERA: deny
  com.b:
    permissions:
      android.permission.CAMERA: deny
`)
	dev := &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.a/android.permission.CAMERA": policy.PermissionAllow,
			"com.b/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps:    map[string]policy.AppOpMode{},
		failSetAt: 2,
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "--json", "apply", "--yes"})
	err := command.Execute()
	if ExitCode(err) != 1 {
		t.Fatalf("exit=%d error=%v", ExitCode(err), err)
	}
	got := decodeSinglePlan(t, stdout.Bytes())
	if got.Applied != 1 || got.Writes != 1 || got.Failure == nil || got.Failure.Package != "com.b" {
		t.Fatalf("partial result = %#v", got)
	}
}

func TestApplyHumanPartialFailureShowsPositionAndCounts(t *testing.T) {
	policyPath := writePolicy(t, `version: 1
packages:
  com.a:
    permissions:
      android.permission.CAMERA: deny
  com.b:
    permissions:
      android.permission.CAMERA: deny
`)
	dev := &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.a/android.permission.CAMERA": policy.PermissionAllow,
			"com.b/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps:    map[string]policy.AppOpMode{},
		failSetAt: 2,
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "apply", "--yes"})
	err := command.Execute()
	if ExitCode(err) != 1 {
		t.Fatalf("exit=%d error=%v", ExitCode(err), err)
	}
	for _, want := range []string{"1 applied", "failed at com.b permission", "0 pending", "writes=1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, missing %q", stdout.String(), want)
		}
	}
}

func TestJSONApplyWithoutYesIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(newCLIDevice()))
	command.SetArgs([]string{"--json", "apply"})
	err := command.Execute()
	if ExitCode(err) != 2 {
		t.Fatalf("exit=%d error=%v", ExitCode(err), err)
	}
}

func TestPlanProgressStaysOnStderr(t *testing.T) {
	policyPath := writeTestPolicy(t)
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(newCLIDevice()))
	command.SetArgs([]string{"--file", policyPath, "--json", "--verbose", "plan"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	_ = decodeSinglePlan(t, stdout.Bytes())
	if !strings.Contains(stderr.String(), "Inspecting 1 package(s)") ||
		!strings.Contains(stderr.String(), "Inspected 1/1 com.example") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCancellationKeepsExitCode130(t *testing.T) {
	policyPath := writeTestPolicy(t)
	dev := newCLIDevice()
	dev.inspectErr = context.Canceled
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(dev))
	command.SetArgs([]string{"--file", policyPath, "--json", "plan"})
	err := command.Execute()
	if ExitCode(err) != 130 {
		t.Fatalf("exit=%d error=%v", ExitCode(err), err)
	}
	_ = decodeSinglePlan(t, stdout.Bytes())
}

func TestApplyWithoutTTYRequiresYes(t *testing.T) {
	policyPath := writeTestPolicy(t)
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(newCLIDevice()))
	command.SetArgs([]string{"--file", policyPath, "apply"})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("error = %v", err)
	}
}

func TestCaptureIsDeterministic(t *testing.T) {
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(newCLIDevice()))
	command.SetArgs([]string{"capture", "--package", "com.z,com.a"})

	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	got := stdout.String()
	if strings.Index(got, "com.a:") > strings.Index(got, "com.z:") {
		t.Fatalf("capture is not sorted:\n%s", got)
	}
	if strings.Contains(got, "WAKE_LOCK") {
		t.Fatalf("capture included allow AppOp without --all-appops:\n%s", got)
	}
}

func TestCaptureRefusesOverwrite(t *testing.T) {
	output := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(output, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	command := newCommand("test", bytes.NewReader(nil), &stdout, &stderr, factory(newCLIDevice()))
	command.SetArgs([]string{"capture", "--package", "com.example", "--output", output})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	data, readErr := os.ReadFile(output)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("output changed: %q, %v", data, readErr)
	}
}

func factory(dev engine.Device) func(context.Context, string, string, int) (engine.Device, device.Info, error) {
	return func(context.Context, string, string, int) (engine.Device, device.Info, error) {
		return dev, device.Info{Serial: "test", SDK: 35}, nil
	}
}

func newCLIDevice() *cliDevice {
	return &cliDevice{
		permissions: map[string]policy.PermissionMode{
			"com.example/android.permission.CAMERA": policy.PermissionAllow,
		},
		appOps: map[string]policy.AppOpMode{
			"com.example/CAMERA":    policy.AppOpIgnore,
			"com.example/WAKE_LOCK": policy.AppOpAllow,
		},
	}
}

func writeTestPolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "droidperm.yaml")
	data := []byte(`version: 1
packages:
  com.example:
    permissions:
      android.permission.CAMERA: deny
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePolicy(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "droidperm.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeSinglePlan(t *testing.T, data []byte) engine.Plan {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var got engine.Plan
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("JSON output = %q: %v", data, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON output has another document: %q (%v)", data, err)
	}
	return got
}
