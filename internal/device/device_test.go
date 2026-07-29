package device

import (
	"context"
	"testing"

	"github.com/yutakobayashidev/droidperm/internal/policy"
)

func TestModeValidationHappensBeforeADBWrite(t *testing.T) {
	device := &androidDevice{}
	if err := device.SetPermission(context.Background(), "pkg", "perm", "ask"); err == nil {
		t.Fatal("SetPermission accepted an invalid mode")
	}
	if err := device.SetAppOp(context.Background(), "pkg", "op", policy.AppOpMode("ask")); err == nil {
		t.Fatal("SetAppOp accepted an invalid mode")
	}
}
