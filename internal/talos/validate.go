package talos

import (
	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/validation"

	tstomcfg "github.com/mirceanton/talstomize/internal/config"
)

// runtimeMode is a talosctl validate --mode: "metal" for a node installed to
// disk by the Talos installer, "cloud" for one booted from a platform's
// disk image.
type runtimeMode string

func (m runtimeMode) String() string        { return string(m) }
func (m runtimeMode) RequiresInstall() bool { return m == tstomcfg.PlatformMetal }
func (runtimeMode) InContainer() bool       { return false }

// ValidationMode returns the talosctl validate --mode matching platform.
func ValidationMode(platform string) string {
	if platform == tstomcfg.PlatformMetal {
		return tstomcfg.PlatformMetal
	}

	return "cloud"
}

// Validate runs the same static checks as `talosctl validate` against cfg,
// in the mode matching platform (see ValidationMode) - schema errors, a
// missing install disk on metal, and settings declared both in the legacy
// v1alpha1 fields and in their multi-document equivalent - returning any
// non-fatal warnings. Checks that need the node itself (e.g. that the
// install disk exists) are out of reach until the config is applied.
func Validate(cfg tconfig.Provider, platform string) (warnings []string, err error) {
	return cfg.ValidateAsClient(runtimeMode(ValidationMode(platform)), validation.WithLocal())
}
