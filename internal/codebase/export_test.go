package codebase

// The argument builders, exposed to the tests of the package.
var (
	ResolveArgs  = resolveArgs
	InitArgs     = initArgs
	FetchArgs    = fetchArgs
	BundleArgs   = bundleArgs
	ParseResolve = parseResolve
)

// Transcript exposes transcript to the tests of the package.
type Transcript = transcript

// Kept returns what t holds.
func Kept(t *Transcript) []byte { return t.said }

// Contains exposes contains to the tests of the package.
func Contains(t *Transcript, marker string) bool { return t.contains(marker) }

// Limit exposes the size a transcript keeps.
const Limit = limit

// GitDir exposes the bare repository of r to the tests of the package.
func GitDir(r *Repo) string { return r.gitDir() }

// Dir exposes the temporary directory of r to the tests of the package.
func Dir(r *Repo) string { return r.dir }
