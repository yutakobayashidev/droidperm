package adb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

const (
	minSupportedSDK = 29
	maxSupportedSDK = 36
)

var (
	ErrNoDevice          = errors.New("no usable Android device found")
	ErrMultipleDevices   = errors.New("multiple Android devices found")
	ErrPackageNotFound   = errors.New("package not found")
	ErrUnsupportedDevice = errors.New("unsupported Android version")
	ErrInvalidArgument   = errors.New("invalid adb argument")
	ErrUnknownAppOp      = errors.New("unknown AppOp")
)

// Runner executes adb without involving a shell.
type Runner interface {
	Run(ctx context.Context, path string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return nil, fmt.Errorf("run adb: %w", err)
		}
		return nil, fmt.Errorf("run adb: %w: %s", err, detail)
	}
	return output, nil
}

type DeviceInfo struct {
	Serial       string `json:"serial"`
	SDK          int    `json:"sdk"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer"`
	Fingerprint  string `json:"fingerprint"`
}

type PackageState struct {
	Package     string          `json:"package"`
	Permissions map[string]bool `json:"permissions"`
}

type Client struct {
	path   string
	serial string
	user   int
	runner Runner
	mu     sync.Mutex
}

func New(path, serial string, user int) *Client {
	return NewWithRunner(path, serial, user, execRunner{})
}

func NewWithRunner(path, serial string, user int, runner Runner) *Client {
	if path == "" {
		path = "adb"
	}
	return &Client{
		path:   path,
		serial: serial,
		user:   user,
		runner: runner,
	}
}

// ResolveDevice selects a device in the same order as adb itself: an explicit
// serial, ANDROID_SERIAL, or the only connected device.
func (c *Client) ResolveDevice(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.serial != "" {
		return c.serial, nil
	}
	if serial := os.Getenv("ANDROID_SERIAL"); serial != "" {
		c.serial = serial
		return serial, nil
	}

	output, err := c.runner.Run(ctx, c.path, "devices", "-l")
	if err != nil {
		return "", err
	}
	devices := parseDevices(string(output))
	switch len(devices) {
	case 0:
		return "", ErrNoDevice
	case 1:
		c.serial = devices[0]
		return devices[0], nil
	default:
		return "", fmt.Errorf("%w: %s", ErrMultipleDevices, strings.Join(devices, ", "))
	}
}

func (c *Client) Probe(ctx context.Context) (DeviceInfo, error) {
	serial, err := c.ResolveDevice(ctx)
	if err != nil {
		return DeviceInfo{}, err
	}

	sdkText, err := c.shell(ctx, "getprop", "ro.build.version.sdk")
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("read Android SDK level: %w", err)
	}
	sdk, err := strconv.Atoi(strings.TrimSpace(sdkText))
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("parse Android SDK level %q: %w", strings.TrimSpace(sdkText), err)
	}
	if sdk < minSupportedSDK || sdk > maxSupportedSDK {
		return DeviceInfo{}, fmt.Errorf("%w: API %d (supported: %d-%d)", ErrUnsupportedDevice, sdk, minSupportedSDK, maxSupportedSDK)
	}

	model, err := c.getProp(ctx, "ro.product.model")
	if err != nil {
		return DeviceInfo{}, err
	}
	manufacturer, err := c.getProp(ctx, "ro.product.manufacturer")
	if err != nil {
		return DeviceInfo{}, err
	}
	fingerprint, err := c.getProp(ctx, "ro.build.fingerprint")
	if err != nil {
		return DeviceInfo{}, err
	}

	return DeviceInfo{
		Serial:       serial,
		SDK:          sdk,
		Model:        model,
		Manufacturer: manufacturer,
		Fingerprint:  fingerprint,
	}, nil
}

func (c *Client) PackageState(ctx context.Context, pkg string) (PackageState, error) {
	if err := validatePackage(pkg); err != nil {
		return PackageState{}, err
	}
	output, err := c.shell(ctx, "dumpsys", "package", pkg)
	if err != nil {
		return PackageState{}, fmt.Errorf("inspect package %q: %w", pkg, err)
	}
	state, found := parsePackageState(pkg, c.user, output)
	if !found {
		return PackageState{}, fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	return state, nil
}

func (c *Client) AppOps(ctx context.Context, pkg string) (map[string]string, error) {
	if err := validatePackage(pkg); err != nil {
		return nil, err
	}
	output, err := c.shell(ctx, "cmd", "appops", "get", "--user", strconv.Itoa(c.user), pkg)
	if err != nil {
		return nil, fmt.Errorf("inspect AppOps for %q: %w", pkg, err)
	}
	if packageMissing(output) {
		return nil, fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	return parseAppOps(output), nil
}

// AppOp reads one operation. Android omits operations in their default state,
// so an empty successful response is reported as "default".
func (c *Client) AppOp(ctx context.Context, pkg, op string) (string, error) {
	if err := validatePackage(pkg); err != nil {
		return "", err
	}
	if err := validateAppOp(op); err != nil {
		return "", err
	}
	output, err := c.shell(ctx, "cmd", "appops", "get", "--user", strconv.Itoa(c.user), pkg, op)
	if err != nil {
		return "", fmt.Errorf("inspect AppOp %q for %q: %w", op, pkg, err)
	}
	if packageMissing(output) {
		return "", fmt.Errorf("%w: %s", ErrPackageNotFound, pkg)
	}
	if unknownAppOp(output) {
		return "", fmt.Errorf("%w: %s", ErrUnknownAppOp, op)
	}
	ops := parseAppOps(output)
	if mode, ok := ops[op]; ok {
		return mode, nil
	}
	return "default", nil
}

func (c *Client) SetPermission(ctx context.Context, pkg, permission string, allow bool) error {
	if err := validatePackage(pkg); err != nil {
		return err
	}
	if !permissionPattern.MatchString(permission) {
		return fmt.Errorf("%w: permission %q", ErrInvalidArgument, permission)
	}
	action := "revoke"
	if allow {
		action = "grant"
	}
	_, err := c.shell(ctx, "pm", action, "--user", strconv.Itoa(c.user), pkg, permission)
	if err != nil {
		return fmt.Errorf("%s permission %q for %q: %w", action, permission, pkg, err)
	}
	return nil
}

func (c *Client) SetAppOp(ctx context.Context, pkg, op, mode string) error {
	if err := validatePackage(pkg); err != nil {
		return err
	}
	if err := validateAppOp(op); err != nil {
		return err
	}
	switch mode {
	case "allow", "ignore", "deny", "default", "foreground":
	default:
		return fmt.Errorf("%w: AppOp mode %q", ErrInvalidArgument, mode)
	}
	_, err := c.shell(ctx, "cmd", "appops", "set", "--user", strconv.Itoa(c.user), pkg, op, mode)
	if err != nil {
		return fmt.Errorf("set AppOp %q for %q to %q: %w", op, pkg, mode, err)
	}
	return nil
}

func (c *Client) getProp(ctx context.Context, name string) (string, error) {
	output, err := c.shell(ctx, "getprop", name)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return strings.TrimSpace(output), nil
}

func (c *Client) shell(ctx context.Context, args ...string) (string, error) {
	serial, err := c.ResolveDevice(ctx)
	if err != nil {
		return "", err
	}
	adbArgs := make([]string, 0, len(args)+3)
	adbArgs = append(adbArgs, "-s", serial, "shell")
	adbArgs = append(adbArgs, args...)
	output, err := c.runner.Run(ctx, c.path, adbArgs...)
	return string(output), err
}
