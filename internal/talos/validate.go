package talos

import (
	tconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/validation"
)

// metalMode is the "metal" runtime mode, the only platform talstomize
// supports: an installed, non-container node.
type metalMode struct{}

func (metalMode) String() string        { return "metal" }
func (metalMode) RequiresInstall() bool { return true }
func (metalMode) InContainer() bool     { return false }

// Validate runs the same static checks as `talosctl validate --mode metal`
// against cfg - schema errors, and settings declared both in the legacy
// v1alpha1 fields and in their multi-document equivalent - returning any
// non-fatal warnings. Checks that need the node itself (e.g. that the
// install disk exists) are out of reach until the config is applied.
func Validate(cfg tconfig.Provider) (warnings []string, err error) {
	return cfg.ValidateAsClient(metalMode{}, validation.WithLocal())
}
