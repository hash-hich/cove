package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

func TestDeleteArgs(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"delete", "--force", demo}, sandbox.DeleteArgs(demo))
}
