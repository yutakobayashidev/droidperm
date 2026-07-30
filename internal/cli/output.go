package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/yutakobayashidev/droidperm/internal/engine"
)

func writePlan(w io.Writer, plan engine.Plan, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	if plan.Changes == 0 && plan.Error == "" {
		if _, err := fmt.Fprintln(w, "No changes."); err != nil {
			return err
		}
	} else {
		for _, action := range plan.Actions {
			if action.Status == engine.StatusUnchanged {
				continue
			}
			if _, err := fmt.Fprintf(w, "~ %s %s %s: %s -> %s\n",
				action.Package, action.Kind, action.Name, action.Current, action.Desired); err != nil {
				return err
			}
			if action.Warning != "" {
				if _, err := fmt.Fprintf(w, "  warning: %s\n", action.Warning); err != nil {
					return err
				}
			}
		}
		if plan.Changes > 0 {
			if _, err := fmt.Fprintf(w, "\n%d change(s).\n", plan.Changes); err != nil {
				return err
			}
		}
	}
	if plan.Error != "" {
		if _, err := fmt.Fprintf(w, "Preflight failed; writes=0: %s\n", plan.Error); err != nil {
			return err
		}
	}
	return nil
}

func writeApplied(w io.Writer, plan engine.Plan, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	if plan.Error != "" {
		for _, action := range plan.Actions {
			if action.Status == engine.StatusApplied {
				if _, err := fmt.Fprintf(w, "✓ %s %s %s = %s\n",
					action.Package, action.Kind, action.Name, action.Desired); err != nil {
					return err
				}
			}
		}
		if plan.Failure == nil && plan.Writes == 0 {
			_, err := fmt.Fprintf(w, "Preflight failed; writes=%d: %s\n", plan.Writes, plan.Error)
			return err
		}
		if plan.Failure == nil {
			_, err := fmt.Fprintf(
				w,
				"\nPartial apply: %d applied, %d pending (writes=%d).\n%s\n",
				plan.Applied,
				plan.Pending,
				plan.Writes,
				plan.Error,
			)
			return err
		}
		_, err := fmt.Fprintf(
			w,
			"\nPartial apply: %d applied, failed at %s %s %s, %d pending (writes=%d).\n%s\n",
			plan.Applied,
			plan.Failure.Package,
			plan.Failure.Kind,
			plan.Failure.Name,
			plan.Pending,
			plan.Writes,
			plan.Error,
		)
		return err
	}
	if plan.Changes == 0 {
		_, err := fmt.Fprintln(w, "Already converged.")
		return err
	}
	applied := 0
	for _, action := range plan.Actions {
		if action.Status == engine.StatusApplied {
			applied++
			if _, err := fmt.Fprintf(w, "✓ %s %s %s = %s\n",
				action.Package, action.Kind, action.Name, action.Desired); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "\nApplied and verified %d change(s).\n", applied)
	return err
}
