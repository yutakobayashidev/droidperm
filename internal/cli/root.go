package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yutakobayashidev/droidperm/internal/config"
	"github.com/yutakobayashidev/droidperm/internal/device"
	"github.com/yutakobayashidev/droidperm/internal/engine"
	"github.com/yutakobayashidev/droidperm/internal/policy"
)

type options struct {
	file      string
	serial    string
	adbPath   string
	user      int
	json      bool
	noColor   bool
	verbose   bool
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
	newDevice func(context.Context, string, string, int) (engine.Device, device.Info, error)
}

func New(version string, stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	return newCommand(version, stdin, stdout, stderr, device.Open)
}

func newCommand(
	version string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	newDevice func(context.Context, string, string, int) (engine.Device, device.Info, error),
) *cobra.Command {
	opts := &options{
		file:      "droidperm.yaml",
		adbPath:   "adb",
		user:      0,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		newDevice: newDevice,
	}
	root := &cobra.Command{
		Use:           "droidperm",
		Short:         "Manage Android app permissions as code",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitError(2, err)
	})

	flags := root.PersistentFlags()
	flags.StringVarP(&opts.file, "file", "f", opts.file, "policy YAML file")
	flags.StringVarP(&opts.serial, "serial", "s", "", "ADB device serial (defaults to ANDROID_SERIAL or the only connected device)")
	flags.StringVar(&opts.adbPath, "adb", opts.adbPath, "path to the adb executable")
	flags.IntVar(&opts.user, "user", opts.user, "numeric Android user ID")
	flags.BoolVar(&opts.json, "json", false, "write stable JSON output")
	flags.BoolVar(&opts.noColor, "no-color", false, "disable color output")
	flags.BoolVar(&opts.verbose, "verbose", false, "write device details to stderr")

	commands := []*cobra.Command{
		newValidateCommand(opts),
		newPlanCommand(opts, false),
		newPlanCommand(opts, true),
		newApplyCommand(opts),
		newCaptureCommand(opts),
		newCompletionCommand(root),
	}
	for _, command := range commands {
		wrapRuntimeErrors(command)
		root.AddCommand(command)
	}
	return root
}

func wrapRuntimeErrors(command *cobra.Command) {
	run := command.RunE
	if run == nil {
		return
	}
	command.RunE = func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err == nil {
			return nil
		}
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			return err
		}
		return exitError(1, err)
	}
}

func newValidateCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate a policy without connecting to a device",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := config.LoadFile(opts.file); err != nil {
				return exitError(2, err)
			}
			_, err := fmt.Fprintf(opts.stdout, "%s is valid.\n", opts.file)
			return err
		},
	}
}

func newPlanCommand(opts *options, check bool) *cobra.Command {
	var packages []string
	name := "plan"
	short := "Show the changes required by a policy"
	if check {
		name = "check"
		short = "Exit with code 3 when a device has drifted"
	}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			policyFile, dev, err := opts.loadPolicyAndDevice(cmd.Context(), packages)
			if err != nil {
				return err
			}
			opts.reportPlanStart(len(policyFile.Packages))
			plan, err := engine.BuildPlan(cmd.Context(), dev, *policyFile, opts.progress())
			if err != nil {
				if outputErr := writePlan(opts.stdout, plan, opts.json); outputErr != nil {
					return outputErr
				}
				return err
			}
			if err := writePlan(opts.stdout, plan, opts.json); err != nil {
				return err
			}
			if check && plan.Changes > 0 {
				return exitError(3, errors.New("device state differs from policy"))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&packages, "package", nil, "package to inspect (repeatable or comma-separated)")
	return cmd
}

func newApplyCommand(opts *options) *cobra.Command {
	var (
		yes      bool
		packages []string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply and verify the changes required by a policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.json && !yes {
				return exitError(2, errors.New("--json apply requires --yes"))
			}
			policyFile, dev, err := opts.loadPolicyAndDevice(cmd.Context(), packages)
			if err != nil {
				return err
			}
			opts.reportPlanStart(len(policyFile.Packages))
			plan, err := engine.BuildPlan(cmd.Context(), dev, *policyFile, opts.progress())
			if err != nil {
				if outputErr := writeApplied(opts.stdout, plan, opts.json); outputErr != nil {
					return outputErr
				}
				return err
			}
			if !opts.json {
				if err := writePlan(opts.stdout, plan, false); err != nil {
					return err
				}
			}
			if plan.Changes == 0 {
				if opts.json {
					return writeApplied(opts.stdout, plan, true)
				}
				return nil
			}
			if !yes {
				ok, err := confirm(opts.stdin, opts.stdout, plan.Changes)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("apply cancelled")
				}
			}
			applied, err := engine.Apply(cmd.Context(), dev, plan)
			if err != nil {
				if outputErr := writeApplied(opts.stdout, applied, opts.json); outputErr != nil {
					return outputErr
				}
				return err
			}
			return writeApplied(opts.stdout, applied, opts.json)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "apply without an interactive confirmation")
	cmd.Flags().StringSliceVar(&packages, "package", nil, "package to apply (repeatable or comma-separated)")
	return cmd
}

func newCaptureCommand(opts *options) *cobra.Command {
	var (
		packages  []string
		allAppOps bool
		output    string
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture selected packages as a reviewable policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dev, err := opts.openDevice(cmd.Context())
			if err != nil {
				return err
			}
			captured, err := engine.Capture(cmd.Context(), dev, packages, allAppOps)
			if err != nil {
				return err
			}
			data, err := config.Marshal(&captured)
			if err != nil {
				return err
			}
			if output == "" || output == "-" {
				_, err = opts.stdout.Write(data)
				return err
			}
			if err := writeFile(output, data, force); err != nil {
				return err
			}
			_, err = fmt.Fprintf(opts.stdout, "Captured %d package(s) to %s.\n", len(captured.Packages), output)
			return err
		},
	}
	cmd.Flags().StringSliceVar(&packages, "package", nil, "package to capture (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&allAppOps, "all-appops", false, "include observed allow/default AppOps as well as restrictions")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (defaults to stdout)")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing output file")
	_ = cmd.MarkFlagRequired("package")
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion <bash|zsh|fish|powershell>",
		Short:     "Generate a shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return exitError(2, fmt.Errorf("unsupported shell %q", args[0]))
			}
		},
	}
}

func (opts *options) loadPolicyAndDevice(
	ctx context.Context,
	packages []string,
) (*policy.File, engine.Device, error) {
	file, err := config.LoadFile(opts.file)
	if err != nil {
		return nil, nil, exitError(2, err)
	}
	file, err = selectPackages(file, packages)
	if err != nil {
		return nil, nil, exitError(2, err)
	}
	dev, err := opts.openDevice(ctx)
	return file, dev, err
}

func selectPackages(file *policy.File, selected []string) (*policy.File, error) {
	if len(selected) == 0 {
		return file, nil
	}
	names := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		names[name] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	filtered := policy.File{
		Version:  file.Version,
		Packages: make(map[string]policy.Package, len(sorted)),
	}
	for _, name := range sorted {
		pkg, ok := file.Packages[name]
		if !ok {
			return nil, fmt.Errorf("package %q is not present in the policy", name)
		}
		filtered.Packages[name] = pkg
	}
	return &filtered, nil
}

func (opts *options) reportPlanStart(packages int) {
	_, _ = fmt.Fprintf(opts.stderr, "Inspecting %d package(s)...\n", packages)
}

func (opts *options) progress() engine.ProgressFunc {
	if !opts.verbose {
		return nil
	}
	return func(completed, total int, packageName string) {
		_, _ = fmt.Fprintf(opts.stderr, "Inspected %d/%d %s\n", completed, total, packageName)
	}
}

func (opts *options) openDevice(ctx context.Context) (engine.Device, error) {
	dev, info, err := opts.newDevice(ctx, opts.adbPath, opts.serial, opts.user)
	if err != nil {
		return nil, err
	}
	if opts.verbose {
		_, _ = fmt.Fprintf(opts.stderr, "device %s: Android API %d, %s %s\n",
			info.Serial, info.SDK, info.Manufacturer, info.Model)
	}
	return dev, nil
}

func confirm(stdin io.Reader, stdout io.Writer, changes int) (bool, error) {
	file, ok := stdin.(*os.File)
	if !ok {
		return false, errors.New("non-interactive apply requires --yes")
	}
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return false, errors.New("non-interactive apply requires --yes")
	}
	if _, err := fmt.Fprintf(stdout, "Apply %d change(s)? [y/N] ", changes); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func writeFile(path string, data []byte, force bool) error {
	if !force {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%s already exists; use --force to replace it", path)
			}
			return err
		}
		if _, err = file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	}

	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".droidperm-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = os.Remove(tempName)
	}()
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
