package adb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeResponse struct {
	output string
	err    error
}

type fakeRunner struct {
	responses []fakeResponse
	calls     [][]string
}

func (f *fakeRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	call := append([]string{path}, args...)
	f.calls = append(f.calls, call)
	if len(f.responses) == 0 {
		return nil, errors.New("unexpected adb call")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return []byte(response.output), response.err
}

func TestResolveDevice(t *testing.T) {
	t.Setenv("ANDROID_SERIAL", "")

	tests := []struct {
		name    string
		output  string
		want    string
		wantErr error
	}{
		{
			name:   "one connected device",
			output: "List of devices attached\nemulator-5554 device product:sdk model:Pixel_8\n",
			want:   "emulator-5554",
		},
		{
			name:    "no connected device",
			output:  "List of devices attached\nphone offline\n",
			wantErr: ErrNoDevice,
		},
		{
			name:    "multiple connected devices",
			output:  "List of devices attached\none device\ntwo device\n",
			wantErr: ErrMultipleDevices,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{responses: []fakeResponse{{output: tt.output}}}
			client := NewWithRunner("custom-adb", "", 0, runner)
			got, err := client.ResolveDevice(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveDevice() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveDevice() = %q, want %q", got, tt.want)
			}
			wantCall := []string{"custom-adb", "devices", "-l"}
			if !reflect.DeepEqual(runner.calls[0], wantCall) {
				t.Fatalf("call = %#v, want %#v", runner.calls[0], wantCall)
			}
		})
	}
}

func TestResolveDevicePrecedence(t *testing.T) {
	t.Setenv("ANDROID_SERIAL", "from-env")
	runner := &fakeRunner{}

	explicit := NewWithRunner("adb", "explicit", 0, runner)
	if got, err := explicit.ResolveDevice(context.Background()); err != nil || got != "explicit" {
		t.Fatalf("explicit ResolveDevice() = %q, %v", got, err)
	}

	fromEnv := NewWithRunner("adb", "", 0, runner)
	if got, err := fromEnv.ResolveDevice(context.Background()); err != nil || got != "from-env" {
		t.Fatalf("environment ResolveDevice() = %q, %v", got, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("device discovery unexpectedly ran: %#v", runner.calls)
	}
}

func TestResolveDeviceExplainsUnavailableDevices(t *testing.T) {
	t.Setenv("ANDROID_SERIAL", "")
	tests := []struct {
		state string
		want  string
	}{
		{state: "unauthorized", want: "approve the USB debugging prompt"},
		{state: "offline", want: "reconnect it"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			runner := &fakeRunner{responses: []fakeResponse{{
				output: "List of devices attached\nphone " + tt.state + "\n",
			}}}
			client := NewWithRunner("adb", "", 0, runner)
			_, err := client.ResolveDevice(context.Background())
			if !errors.Is(err, ErrNoDevice) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ResolveDevice() error = %v", err)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{
		{output: "35\n"},
		{output: "Pixel 8\n"},
		{output: "Google\n"},
		{output: "google/shiba/shiba:15/AP3A/test:user/release-keys\n"},
	}}
	client := NewWithRunner("adb", "serial", 0, runner)

	got, err := client.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.SDK != 35 || got.Model != "Pixel 8" || got.Manufacturer != "Google" || got.Serial != "serial" {
		t.Fatalf("Probe() = %#v", got)
	}
	for _, call := range runner.calls {
		if !reflect.DeepEqual(call[:4], []string{"adb", "-s", "serial", "shell"}) {
			t.Fatalf("unsafe/unexpected invocation: %#v", call)
		}
	}
}

func TestProbeRejectsUnsupportedSDK(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{output: "28\n"}}}
	client := NewWithRunner("adb", "serial", 0, runner)
	_, err := client.Probe(context.Background())
	if !errors.Is(err, ErrUnsupportedDevice) {
		t.Fatalf("Probe() error = %v, want ErrUnsupportedDevice", err)
	}
}

func TestSetCommandsKeepValuesAsSeparateArguments(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{}, {}}}
	client := NewWithRunner("adb", "serial", 10, runner)
	ctx := context.Background()
	if err := client.SetPermission(ctx, "com.example", "android.permission.CAMERA", false); err != nil {
		t.Fatal(err)
	}
	if err := client.SetAppOp(ctx, "com.example", "CAMERA", "ignore"); err != nil {
		t.Fatal(err)
	}

	wantPermission := []string{
		"adb", "-s", "serial", "shell", "pm", "revoke", "--user", "10",
		"com.example", "android.permission.CAMERA",
	}
	if !reflect.DeepEqual(runner.calls[0], wantPermission) {
		t.Fatalf("permission call = %#v, want %#v", runner.calls[0], wantPermission)
	}
	wantAppOp := []string{
		"adb", "-s", "serial", "shell", "cmd", "appops", "set", "--user", "10",
		"com.example", "CAMERA", "ignore",
	}
	if !reflect.DeepEqual(runner.calls[1], wantAppOp) {
		t.Fatalf("AppOp call = %#v, want %#v", runner.calls[1], wantAppOp)
	}
}

func TestPackageStateAllowsSharedUID(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{output: `Packages:
  Package [com.example] (abc123):
    sharedUser=SharedUserSetting{def456 com.example.shared/10123}
    requested permissions:
      android.permission.CAMERA
Shared users:
  SharedUser [com.example.shared] (def456):
    User 0:
      runtime permissions:
        android.permission.CAMERA: granted=true, flags=[ USER_SET]
`}}}
	client := NewWithRunner("adb", "serial", 0, runner)

	state, err := client.PackageState(context.Background(), "com.example")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Permissions["android.permission.CAMERA"] {
		t.Fatalf("PackageState() = %#v", state)
	}
	wantCall := []string{
		"adb", "-s", "serial", "shell", "dumpsys", "package", "com.example",
	}
	if !reflect.DeepEqual(runner.calls[0], wantCall) {
		t.Fatalf("call = %#v, want %#v", runner.calls[0], wantCall)
	}
}

func TestSetCommandsRejectRemoteShellMetacharacters(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner("adb", "serial", 0, runner)
	err := client.SetAppOp(context.Background(), "com.example;id", "CAMERA", "ignore")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SetAppOp() error = %v, want ErrInvalidArgument", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid input reached adb: %#v", runner.calls)
	}
}

func TestAppOpDefaultsWhenSuccessfulOutputHasNoMode(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{output: "No operations.\n"}}}
	client := NewWithRunner("adb", "serial", 0, runner)
	mode, err := client.AppOp(context.Background(), "com.example", "CAMERA")
	if err != nil {
		t.Fatal(err)
	}
	if mode != "default" {
		t.Fatalf("AppOp() = %q, want default", mode)
	}
	if got := strings.Join(runner.calls[0], " "); !strings.HasSuffix(got, "com.example CAMERA") {
		t.Fatalf("targeted AppOp call = %q", got)
	}
}

func TestAppOpRejectsUnknownOperationReportedOnStdout(t *testing.T) {
	runner := &fakeRunner{responses: []fakeResponse{{
		output: "Error: Unknown operation string: FUTURE_OP\n",
	}}}
	client := NewWithRunner("adb", "serial", 0, runner)
	_, err := client.AppOp(context.Background(), "com.example", "FUTURE_OP")
	if !errors.Is(err, ErrUnknownAppOp) {
		t.Fatalf("AppOp() error = %v, want ErrUnknownAppOp", err)
	}
}
