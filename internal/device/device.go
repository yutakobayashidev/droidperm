package device

import (
	"context"
	"fmt"

	"github.com/yutakobayashidev/droidperm/internal/adb"
	"github.com/yutakobayashidev/droidperm/internal/engine"
	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type Info = adb.DeviceInfo

type androidDevice struct {
	client *adb.Client
}

func Open(ctx context.Context, adbPath, serial string, user int) (engine.Device, Info, error) {
	client := adb.New(adbPath, serial, user)
	if _, err := client.ResolveDevice(ctx); err != nil {
		return nil, Info{}, err
	}
	info, err := client.Probe(ctx)
	if err != nil {
		return nil, Info{}, err
	}
	return &androidDevice{client: client}, info, nil
}

func (d *androidDevice) InspectPackage(
	ctx context.Context,
	packageName string,
) (engine.Snapshot, error) {
	state, err := d.client.PackageState(ctx, packageName)
	if err != nil {
		return engine.Snapshot{}, err
	}
	rawAppOps, err := d.client.AppOps(ctx, packageName)
	if err != nil {
		return engine.Snapshot{}, err
	}

	permissions := make(map[string]policy.PermissionMode, len(state.Permissions))
	for name, granted := range state.Permissions {
		mode := policy.PermissionDeny
		if granted {
			mode = policy.PermissionAllow
		}
		permissions[name] = mode
	}

	appOps := make(map[string]policy.AppOpMode, len(rawAppOps))
	for name, rawMode := range rawAppOps {
		mode := policy.AppOpMode(rawMode)
		if !policy.ValidAppOpMode(mode) {
			return engine.Snapshot{}, fmt.Errorf("AppOp %q returned unsupported mode %q", name, rawMode)
		}
		appOps[name] = mode
	}

	return engine.Snapshot{Permissions: permissions, AppOps: appOps}, nil
}

func (d *androidDevice) ValidateAppOp(
	ctx context.Context,
	packageName, appOp string,
) error {
	mode, err := d.client.AppOp(ctx, packageName, appOp)
	if err != nil {
		return err
	}
	if parsed := policy.AppOpMode(mode); !policy.ValidAppOpMode(parsed) {
		return fmt.Errorf("AppOp %q returned unsupported mode %q", appOp, mode)
	}
	return nil
}

func (d *androidDevice) SetPermission(
	ctx context.Context,
	packageName, permission string,
	mode policy.PermissionMode,
) error {
	if !policy.ValidPermissionMode(mode) {
		return fmt.Errorf("invalid permission mode %q", mode)
	}
	return d.client.SetPermission(ctx, packageName, permission, mode == policy.PermissionAllow)
}

func (d *androidDevice) SetAppOp(
	ctx context.Context,
	packageName, appOp string,
	mode policy.AppOpMode,
) error {
	if !policy.ValidAppOpMode(mode) {
		return fmt.Errorf("invalid AppOp mode %q", mode)
	}
	return d.client.SetAppOp(ctx, packageName, appOp, string(mode))
}
