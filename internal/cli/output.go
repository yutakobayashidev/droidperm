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
	if plan.Changes == 0 {
		_, err := fmt.Fprintln(w, "No changes.")
		return err
	}
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
	_, err := fmt.Fprintf(w, "\n%d change(s).\n", plan.Changes)
	return err
}

func writeApplied(w io.Writer, plan engine.Plan, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
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
