package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func (d *cliDevice) key(pkg, name string) string { return pkg + "/" + name }

func (d *cliDevice) Permission(_ context.Context, pkg, name string) (policy.PermissionMode, error) {
	return d.permissions[d.key(pkg, name)], nil
}

func (d *cliDevice) AppOp(_ context.Context, pkg, name string) (policy.AppOpMode, error) {
	return d.appOps[d.key(pkg, name)], nil
}

func (d *cliDevice) SetPermission(_ context.Context, pkg, name string, mode policy.PermissionMode) error {
	d.permissions[d.key(pkg, name)] = mode
	return nil
}

func (d *cliDevice) SetAppOp(_ context.Context, pkg, name string, mode policy.AppOpMode) error {
	d.appOps[d.key(pkg, name)] = mode
	return nil
}

func (d *cliDevice) Capture(_ context.Context, _ string, allAppOps bool) (engine.Snapshot, error) {
	appOps := map[string]policy.AppOpMode{"CAMERA": policy.AppOpIgnore}
	if allAppOps {
		appOps["WAKE_LOCK"] = policy.AppOpAllow
	}
	return engine.Snapshot{
		Permissions: map[string]policy.PermissionMode{
			"android.permission.CAMERA": policy.PermissionDeny,
		},
		AppOps: appOps,
	}, nil
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
	var got engine.Plan
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("JSON output = %q: %v", stdout.String(), err)
	}
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes != 0 {
		t.Fatalf("post-apply changes = %d", plan.Changes)
	}
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
		appOps: map[string]policy.AppOpMode{},
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
