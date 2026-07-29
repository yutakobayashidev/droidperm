package adb

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	runtimePermissionPattern = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_.]*):\s+granted=(true|false)\b`)
	appOpPattern             = regexp.MustCompile(`^\s*(?:Uid mode:\s*)?([A-Z][A-Z0-9_]*)(?:\s+\([^)]*\))?:\s*(allow|ignore|deny|errored|default|foreground)\b`)
	packageHeaderPattern     = regexp.MustCompile(`(?m)^\s*Package\s+\[([^\]]+)\]`)
	userHeaderPattern        = regexp.MustCompile(`^\s*User\s+(\d+):`)
	packagePattern           = regexp.MustCompile(`^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+$`)
	permissionPattern        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)+$`)
	appOpNamePattern         = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

func parseDevices(output string) []string {
	var devices []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] != "List" && fields[1] == "device" {
			devices = append(devices, fields[0])
		}
	}
	return devices
}

func parsePackageState(pkg string, user int, output string) (PackageState, bool) {
	state := PackageState{
		Package:     pkg,
		Permissions: make(map[string]bool),
	}
	if packageMissing(output) {
		return state, false
	}

	hasPackageHeader := packageHeaderPattern.MatchString(output)
	found := !hasPackageHeader && strings.Contains(output, "requested permissions:")
	inTargetPackage := found
	inRuntimePermissions := false
	runtimeIndent := 0
	currentUser := -1

	for _, line := range strings.Split(output, "\n") {
		if match := packageHeaderPattern.FindStringSubmatch(line); match != nil {
			inTargetPackage = match[1] == pkg
			if inTargetPackage {
				found = true
			}
			inRuntimePermissions = false
			currentUser = -1
			continue
		}
		if !inTargetPackage {
			continue
		}

		trimmed := strings.TrimSpace(line)
		indent := leadingSpaces(line)
		if match := userHeaderPattern.FindStringSubmatch(line); match != nil {
			currentUser, _ = strconv.Atoi(match[1])
			inRuntimePermissions = false
			continue
		}
		if strings.HasSuffix(trimmed, "runtime permissions:") {
			// A runtime section without a User header is a legacy user-0
			// representation. Modern dumpsys output identifies each user.
			inRuntimePermissions = currentUser == user || (currentUser == -1 && user == 0)
			runtimeIndent = indent
			continue
		}
		if !inRuntimePermissions {
			continue
		}
		if trimmed == "" {
			continue
		}
		if indent <= runtimeIndent {
			inRuntimePermissions = false
			continue
		}
		if match := runtimePermissionPattern.FindStringSubmatch(line); match != nil {
			state.Permissions[match[1]] = match[2] == "true"
		}
	}

	return state, found
}

func parseAppOps(output string) map[string]string {
	ops := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		match := appOpPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		mode := match[2]
		if mode == "errored" {
			mode = "deny"
		}
		ops[match[1]] = mode
	}
	return ops
}

func packageMissing(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "unable to find package") ||
		strings.Contains(lower, "no uid for") ||
		strings.Contains(lower, "unknown package")
}

func unknownAppOp(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "unknown operation") ||
		strings.Contains(lower, "unknown appop")
}

func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t"))
}

func validatePackage(pkg string) error {
	if !packagePattern.MatchString(pkg) {
		return fmt.Errorf("%w: package %q", ErrInvalidArgument, pkg)
	}
	return nil
}

func validateAppOp(op string) error {
	if !appOpNamePattern.MatchString(op) {
		return fmt.Errorf("%w: AppOp %q", ErrInvalidArgument, op)
	}
	return nil
}
