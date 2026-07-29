package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type Kind string

const (
	KindPermission Kind = "permission"
	KindAppOp      Kind = "appop"
)

type Status string

const (
	StatusChange    Status = "change"
	StatusUnchanged Status = "unchanged"
	StatusApplied   Status = "applied"
)

type Action struct {
	Package string `json:"package"`
	Kind    Kind   `json:"kind"`
	Name    string `json:"name"`
	Current string `json:"current"`
	Desired string `json:"desired"`
	Status  Status `json:"status"`
	Warning string `json:"warning,omitempty"`
}

type Plan struct {
	Actions []Action `json:"actions"`
	Changes int      `json:"changes"`
}

type Snapshot struct {
	Permissions map[string]policy.PermissionMode
	AppOps      map[string]policy.AppOpMode
}

type Device interface {
	Permission(ctx context.Context, packageName, permission string) (policy.PermissionMode, error)
	AppOp(ctx context.Context, packageName, appOp string) (policy.AppOpMode, error)
	SetPermission(ctx context.Context, packageName, permission string, mode policy.PermissionMode) error
	SetAppOp(ctx context.Context, packageName, appOp string, mode policy.AppOpMode) error
	Capture(ctx context.Context, packageName string, allAppOps bool) (Snapshot, error)
}

func BuildPlan(ctx context.Context, device Device, desired policy.File) (Plan, error) {
	actions := make([]Action, 0)
	var errs []error

	for packageName, pkg := range desired.Packages {
		for name, mode := range pkg.Permissions {
			current, err := device.Permission(ctx, packageName, name)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s permission %s: %w", packageName, name, err))
				continue
			}
			actions = append(actions, newAction(packageName, KindPermission, name, string(current), string(mode)))
		}
		for name, mode := range pkg.AppOps {
			current, err := device.AppOp(ctx, packageName, name)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s appop %s: %w", packageName, name, err))
				continue
			}
			action := newAction(packageName, KindAppOp, name, string(current), string(mode))
			if mode == policy.AppOpDeny {
				action.Warning = "deny may cause the app to fail with a security error; prefer ignore when possible"
			}
			actions = append(actions, action)
		}
	}

	sortActions(actions)
	plan := Plan{Actions: actions}
	for _, action := range actions {
		if action.Status == StatusChange {
			plan.Changes++
		}
	}
	if len(errs) > 0 {
		return plan, errors.Join(errs...)
	}
	return plan, nil
}

func Apply(ctx context.Context, device Device, plan Plan) (Plan, error) {
	result := Plan{Actions: append([]Action(nil), plan.Actions...), Changes: plan.Changes}
	for i := range result.Actions {
		action := &result.Actions[i]
		if action.Status != StatusChange {
			continue
		}

		var err error
		switch action.Kind {
		case KindPermission:
			err = device.SetPermission(ctx, action.Package, action.Name, policy.PermissionMode(action.Desired))
		case KindAppOp:
			err = device.SetAppOp(ctx, action.Package, action.Name, policy.AppOpMode(action.Desired))
		default:
			err = fmt.Errorf("unknown action kind %q", action.Kind)
		}
		if err != nil {
			return result, fmt.Errorf("apply %s %s %s: %w", action.Package, action.Kind, action.Name, err)
		}
		if err := verify(ctx, device, *action); err != nil {
			return result, err
		}
		action.Status = StatusApplied
		action.Current = action.Desired
	}
	return result, nil
}

func Capture(ctx context.Context, device Device, packages []string, allAppOps bool) (policy.File, error) {
	out := policy.File{Version: policy.Version, Packages: make(map[string]policy.Package, len(packages))}
	var errs []error
	seen := make(map[string]struct{}, len(packages))

	for _, packageName := range packages {
		if _, ok := seen[packageName]; ok {
			continue
		}
		seen[packageName] = struct{}{}
		snapshot, err := device.Capture(ctx, packageName, allAppOps)
		if err != nil {
			errs = append(errs, fmt.Errorf("capture %s: %w", packageName, err))
			continue
		}
		if len(snapshot.Permissions) == 0 && len(snapshot.AppOps) == 0 {
			continue
		}
		out.Packages[packageName] = policy.Package{
			Permissions: snapshot.Permissions,
			AppOps:      snapshot.AppOps,
		}
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

func newAction(packageName string, kind Kind, name, current, desired string) Action {
	status := StatusChange
	if current == desired {
		status = StatusUnchanged
	}
	return Action{
		Package: packageName,
		Kind:    kind,
		Name:    name,
		Current: current,
		Desired: desired,
		Status:  status,
	}
}

func sortActions(actions []Action) {
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Package != actions[j].Package {
			return actions[i].Package < actions[j].Package
		}
		if actions[i].Kind != actions[j].Kind {
			return actions[i].Kind == KindPermission
		}
		return actions[i].Name < actions[j].Name
	})
}

func verify(ctx context.Context, device Device, action Action) error {
	var (
		current string
		err     error
	)
	switch action.Kind {
	case KindPermission:
		var mode policy.PermissionMode
		mode, err = device.Permission(ctx, action.Package, action.Name)
		current = string(mode)
	case KindAppOp:
		var mode policy.AppOpMode
		mode, err = device.AppOp(ctx, action.Package, action.Name)
		current = string(mode)
	}
	if err != nil {
		return fmt.Errorf("verify %s %s %s: %w", action.Package, action.Kind, action.Name, err)
	}
	if current != action.Desired {
		return fmt.Errorf("verify %s %s %s: got %q, want %q", action.Package, action.Kind, action.Name, current, action.Desired)
	}
	return nil
}
