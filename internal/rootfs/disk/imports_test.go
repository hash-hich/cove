package disk_test

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestThePackageNeverReachesForTheRulesOfTheHost holds the line the lint rule holds: path/filepath
// answers what a path means on the machine running cove, Windows included, and a layer is read
// the same way everywhere. The lint rule is what fails first; this test says why it is there and
// keeps saying it if the rule ever moves.
func TestThePackageNeverReachesForTheRulesOfTheHost(t *testing.T) {
	t.Parallel()

	files, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, entry.Name(), nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imported := range parsed.Imports {
			require.NotEqual(t, `"path/filepath"`, imported.Path.Value, "%s imports path/filepath", entry.Name())
		}
	}
}
