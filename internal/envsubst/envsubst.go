// Package envsubst expands ${VAR} references in raw talstomize.yaml and
// patch content using values from the process environment, so secrets
// (registry credentials, tokens, ...) don't have to be committed in plain
// text. Pair it with a tool like `op run` or `direnv` to populate the
// environment before running talstomize.
package envsubst

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var pattern = regexp.MustCompile(`\$\{(\w+)\}`)

// Expand replaces every ${VAR} in in with the value of the VAR environment
// variable. If any referenced variable is undefined, Expand returns an
// error naming all of them instead of substituting an empty string -
// a missing registry password should fail the build, not render blank.
func Expand(in []byte) ([]byte, error) {
	missing := map[string]struct{}{}

	out := pattern.ReplaceAllFunc(in, func(match []byte) []byte {
		name := string(pattern.FindSubmatch(match)[1])

		val, ok := os.LookupEnv(name)
		if !ok {
			missing[name] = struct{}{}
			return match
		}

		return []byte(val)
	})

	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}

		sort.Strings(names)

		return nil, fmt.Errorf("undefined environment variable(s): %s", strings.Join(names, ", "))
	}

	return out, nil
}
