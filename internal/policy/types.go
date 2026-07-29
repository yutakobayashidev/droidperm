package policy

const Version = 1

type PermissionMode string

const (
	PermissionAllow PermissionMode = "allow"
	PermissionDeny  PermissionMode = "deny"
)

type AppOpMode string

const (
	AppOpAllow      AppOpMode = "allow"
	AppOpIgnore     AppOpMode = "ignore"
	AppOpDeny       AppOpMode = "deny"
	AppOpDefault    AppOpMode = "default"
	AppOpForeground AppOpMode = "foreground"
)

type File struct {
	Version  int                `yaml:"version" json:"version"`
	Packages map[string]Package `yaml:"packages" json:"packages"`
}

type Package struct {
	Permissions map[string]PermissionMode `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	AppOps      map[string]AppOpMode      `yaml:"appops,omitempty" json:"appops,omitempty"`
}

func ValidPermissionMode(mode PermissionMode) bool {
	return mode == PermissionAllow || mode == PermissionDeny
}

func ValidAppOpMode(mode AppOpMode) bool {
	switch mode {
	case AppOpAllow, AppOpIgnore, AppOpDeny, AppOpDefault, AppOpForeground:
		return true
	default:
		return false
	}
}
