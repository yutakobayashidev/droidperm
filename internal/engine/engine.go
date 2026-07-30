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
	StatusFailed    Status = "failed"
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
	Writes  int      `json:"writes"`
	Applied int      `json:"applied"`
	Pending int      `json:"pending"`
	Failure *Action  `json:"failure,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type Snapshot struct {
	Permissions map[string]policy.PermissionMode
	AppOps      map[string]policy.AppOpMode
}

type Device interface {
	InspectPackage(ctx context.Context, packageName string) (Snapshot, error)
	ValidateAppOp(ctx context.Context, packageName, appOp string) error
	SetPermission(ctx context.Context, packageName, permission string, mode policy.PermissionMode) error
	SetAppOp(ctx context.Context, packageName, appOp string, mode policy.AppOpMode) error
}

type RequestedPermissionError struct {
	Package    string
	Permission string
}

func (e *RequestedPermissionError) Error() string {
	return fmt.Sprintf(
		"%q is not a requested runtime permission for %q; remove it from the policy or recapture %s",
		e.Permission, e.Package, e.Package,
	)
}

type ProgressFunc func(completed, total int, packageName string)

func BuildPlan(
	ctx context.Context,
	device Device,
	desired policy.File,
	progress ProgressFunc,
) (Plan, error) {
	if err := validateAppOps(ctx, device, desired); err != nil {
		plan := Plan{Actions: []Action{}, Error: err.Error()}
		return plan, err
	}

	packageNames := sortedPackageNames(desired)
	actions := make([]Action, 0)
	var errs []error
	for i, packageName := range packageNames {
		pkg := desired.Packages[packageName]
		snapshot, err := device.InspectPackage(ctx, packageName)
		if err != nil {
			errs = append(errs, fmt.Errorf("inspect %s: %w", packageName, err))
			if progress != nil {
				progress(i+1, len(packageNames), packageName)
			}
			continue
		}
		for name, mode := range pkg.Permissions {
			current, ok := snapshot.Permissions[name]
			if !ok {
				errs = append(errs, &RequestedPermissionError{
					Package: packageName, Permission: name,
				})
				continue
			}
			actions = append(actions, newAction(
				packageName, KindPermission, name, string(current), string(mode),
			))
		}
		for name, mode := range pkg.AppOps {
			current := snapshot.AppOps[name]
			if current == "" {
				current = policy.AppOpDefault
			}
			action := newAction(packageName, KindAppOp, name, string(current), string(mode))
			if mode == policy.AppOpDeny {
				action.Warning = "deny may cause the app to fail with a security error; prefer ignore when possible"
			}
			actions = append(actions, action)
		}
		if progress != nil {
			progress(i+1, len(packageNames), packageName)
		}
	}

	sortActions(actions)
	plan := newPlan(actions)
	if len(errs) > 0 {
		err := errors.Join(errs...)
		plan.Error = err.Error()
		plan.Pending = plan.Changes
		return plan, err
	}
	return plan, nil
}

func Apply(ctx context.Context, device Device, plan Plan) (Plan, error) {
	result := newPlan(append([]Action(nil), plan.Actions...))

	// Android permission changes can mutate related AppOps, so all permissions
	// must be written before any AppOp is evaluated or written.
	for i := range result.Actions {
		action := &result.Actions[i]
		if action.Kind != KindPermission || action.Status != StatusChange {
			continue
		}
		if err := device.SetPermission(
			ctx, action.Package, action.Name, policy.PermissionMode(action.Desired),
		); err != nil {
			return failed(result, i, fmt.Errorf(
				"apply %s %s %s: %w", action.Package, action.Kind, action.Name, err,
			))
		}
		result.Writes++
		snapshot, err := device.InspectPackage(ctx, action.Package)
		if err != nil {
			return failed(result, i, fmt.Errorf(
				"verify %s %s %s: %w", action.Package, action.Kind, action.Name, err,
			))
		}
		if current, ok := snapshotValue(snapshot, *action); !ok || current != action.Desired {
			return failed(result, i, fmt.Errorf(
				"verify %s %s %s: got %q, want %q",
				action.Package, action.Kind, action.Name, current, action.Desired,
			))
		}
		action.Status = StatusApplied
		action.Current = action.Desired
		result.Applied++
	}

	afterPermissions, err := inspectPackages(ctx, device, result.Actions)
	if err != nil {
		return failedWithoutAction(result, fmt.Errorf("verify permissions: %w", err))
	}
	for i := range result.Actions {
		action := &result.Actions[i]
		if action.Kind != KindPermission {
			continue
		}
		current := afterPermissions[action.Package].Permissions[action.Name]
		if string(current) != action.Desired {
			return failed(result, i, fmt.Errorf(
				"verify %s %s %s: got %q, want %q",
				action.Package, action.Kind, action.Name, current, action.Desired,
			))
		}
	}

	// Re-evaluate every managed AppOp after permission side effects, including
	// operations that were unchanged during preflight.
	for i := range result.Actions {
		action := &result.Actions[i]
		if action.Kind != KindAppOp {
			continue
		}
		current := afterPermissions[action.Package].AppOps[action.Name]
		if current == "" {
			current = policy.AppOpDefault
		}
		action.Current = string(current)
		if action.Current == action.Desired {
			action.Status = StatusUnchanged
			continue
		}
		action.Status = StatusChange
		if err := device.SetAppOp(
			ctx, action.Package, action.Name, policy.AppOpMode(action.Desired),
		); err != nil {
			return failed(result, i, fmt.Errorf(
				"apply %s %s %s: %w", action.Package, action.Kind, action.Name, err,
			))
		}
		result.Writes++
		snapshot, err := device.InspectPackage(ctx, action.Package)
		if err != nil {
			return failed(result, i, fmt.Errorf(
				"verify %s %s %s: %w", action.Package, action.Kind, action.Name, err,
			))
		}
		if current, ok := snapshotValue(snapshot, *action); !ok || current != action.Desired {
			return failed(result, i, fmt.Errorf(
				"verify %s %s %s: got %q, want %q",
				action.Package, action.Kind, action.Name, current, action.Desired,
			))
		}
		action.Status = StatusApplied
		action.Current = action.Desired
		result.Applied++
	}

	// A final package-wide read proves the whole selected policy converged and
	// catches shared-UID or other cross-action side effects.
	final, err := inspectPackages(ctx, device, result.Actions)
	if err != nil {
		return failedWithoutAction(result, fmt.Errorf("final verification: %w", err))
	}
	for i := range result.Actions {
		action := &result.Actions[i]
		current, ok := snapshotValue(final[action.Package], *action)
		if !ok || current != action.Desired {
			return failed(result, i, fmt.Errorf(
				"final verification %s %s %s: got %q, want %q",
				action.Package, action.Kind, action.Name, current, action.Desired,
			))
		}
	}
	result.Pending = 0
	return result, nil
}

func Capture(
	ctx context.Context,
	device Device,
	packages []string,
	allAppOps bool,
) (policy.File, error) {
	out := policy.File{Version: policy.Version, Packages: make(map[string]policy.Package, len(packages))}
	var errs []error
	seen := make(map[string]struct{}, len(packages))

	for _, packageName := range packages {
		if _, ok := seen[packageName]; ok {
			continue
		}
		seen[packageName] = struct{}{}
		snapshot, err := device.InspectPackage(ctx, packageName)
		if err != nil {
			errs = append(errs, fmt.Errorf("capture %s: %w", packageName, err))
			continue
		}
		appOps := make(map[string]policy.AppOpMode)
		for name, mode := range snapshot.AppOps {
			if allAppOps || mode == policy.AppOpIgnore || mode == policy.AppOpDeny || mode == policy.AppOpForeground {
				appOps[name] = mode
			}
		}
		if len(snapshot.Permissions) == 0 && len(appOps) == 0 {
			continue
		}
		out.Packages[packageName] = policy.Package{
			Permissions: snapshot.Permissions,
			AppOps:      appOps,
		}
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

func validateAppOps(ctx context.Context, device Device, desired policy.File) error {
	representative := make(map[string]string)
	for packageName, pkg := range desired.Packages {
		for name := range pkg.AppOps {
			if current, ok := representative[name]; !ok || packageName < current {
				representative[name] = packageName
			}
		}
	}
	names := make([]string, 0, len(representative))
	for name := range representative {
		names = append(names, name)
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		if err := device.ValidateAppOp(ctx, representative[name], name); err != nil {
			errs = append(errs, fmt.Errorf("validate appop %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func inspectPackages(
	ctx context.Context,
	device Device,
	actions []Action,
) (map[string]Snapshot, error) {
	names := make(map[string]struct{})
	for _, action := range actions {
		names[action.Package] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	snapshots := make(map[string]Snapshot, len(sorted))
	for _, name := range sorted {
		snapshot, err := device.InspectPackage(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		snapshots[name] = snapshot
	}
	return snapshots, nil
}

func snapshotValue(snapshot Snapshot, action Action) (string, bool) {
	switch action.Kind {
	case KindPermission:
		mode, ok := snapshot.Permissions[action.Name]
		return string(mode), ok
	case KindAppOp:
		mode := snapshot.AppOps[action.Name]
		if mode == "" {
			mode = policy.AppOpDefault
		}
		return string(mode), true
	default:
		return "", false
	}
}

func failed(result Plan, index int, err error) (Plan, error) {
	if result.Actions[index].Status == StatusApplied {
		result.Applied--
	}
	result.Actions[index].Status = StatusFailed
	failure := result.Actions[index]
	result.Failure = &failure
	result.Error = err.Error()
	result.Pending = pending(result.Actions)
	return result, err
}

func failedWithoutAction(result Plan, err error) (Plan, error) {
	result.Error = err.Error()
	result.Pending = pending(result.Actions)
	return result, err
}

func pending(actions []Action) int {
	count := 0
	for _, action := range actions {
		if action.Status == StatusChange {
			count++
		}
	}
	return count
}

func newPlan(actions []Action) Plan {
	plan := Plan{Actions: actions}
	for _, action := range actions {
		if action.Status == StatusChange {
			plan.Changes++
		}
	}
	plan.Pending = plan.Changes
	return plan
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
		if actions[i].Kind != actions[j].Kind {
			return actions[i].Kind == KindPermission
		}
		if actions[i].Package != actions[j].Package {
			return actions[i].Package < actions[j].Package
		}
		return actions[i].Name < actions[j].Name
	})
}

func sortedPackageNames(desired policy.File) []string {
	names := make([]string, 0, len(desired.Packages))
	for name := range desired.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
