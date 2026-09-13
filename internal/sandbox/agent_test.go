package sandbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

func TestAgentArgs(t *testing.T) {
	t.Parallel()

	require.Equal(t, strings.Fields("exec demo claude --version"), sandbox.AgentArgs(demo))
}
