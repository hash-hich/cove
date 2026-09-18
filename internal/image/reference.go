package image

import (
	"errors"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
)

// ErrNoRegistry reports a reference that does not say where it comes from.
var ErrNoRegistry = errors.New("the reference names no registry")

// Parse turns ref into a reference the registry it names can be asked for, by tag or by digest, a
// tag left out being latest as for docker. It returns ErrNoRegistry when ref carries no registry,
// the rule of NamesRegistry, and the error of the parser when ref is malformed; both name ref.
func Parse(ref string) (name.Reference, error) {
	if !NamesRegistry(ref) {
		return nil, fmt.Errorf("%w: %q (expected registry/repository:tag or registry/repository@sha256:...)",
			ErrNoRegistry, ref)
	}
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("invalid reference %q: %w", ref, err)
	}
	return parsed, nil
}
