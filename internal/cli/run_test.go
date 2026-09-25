package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
)

func TestProjectOfNamesTheRepository(t *testing.T) {
	t.Parallel()

	require.Equal(t, "cove", cli.ProjectOf("https://gitlab.com/hich-hich/cove.git"))
	for _, url := range []string{
		"https://github.com/org/repo", "https://github.com/org/repo/", "git@github.com:org/repo.git",
		"git@host:repo.git",
	} {
		require.Equal(t, "repo", cli.ProjectOf(url), url)
	}
}

func TestEnvironTakesABareNameFromTheHost(t *testing.T) {
	t.Setenv("COVE_TEST_SET", "from host")

	got := cli.Environ([]string{"A=1", "COVE_TEST_SET", "COVE_TEST_UNSET", "B="})

	require.Equal(t, []string{"A=1", "COVE_TEST_SET=from host", "B="}, got,
		"a name the host does not set is left out, as docker does")
}
