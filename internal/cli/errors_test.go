package cli

import (
	"context"
	"errors"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", want: 0},
		{name: "runtime", err: exitError(1, errors.New("failed")), want: 1},
		{name: "usage", err: errors.New("unknown command"), want: 2},
		{name: "drift", err: exitError(3, errors.New("drift")), want: 3},
		{name: "interrupt", err: exitError(1, context.Canceled), want: 130},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
