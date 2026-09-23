package systemone

import (
	"runtime/debug"
	"strings"
)

// modulePath must match the module line in go.mod. A fork that renames the
// module gets "devel" rather than a wrong version.
const modulePath = "github.com/justintout/systemone"

// version reports the module version resolved for this package, for the
// User-Agent header. Builds where that is not a released version — a replace
// directive, a fork, this module's own tests — report "devel".
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	for _, dep := range info.Deps {
		if dep.Path == modulePath {
			if dep.Replace != nil {
				return "devel"
			}
			return strings.TrimPrefix(dep.Version, "v")
		}
	}
	return "devel"
}
