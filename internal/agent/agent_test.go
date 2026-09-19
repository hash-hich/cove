package agent_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/agent"
)

func TestNewThreadID(t *testing.T) {
	t.Parallel()

	// The shape claude's --session-id takes: a version 4 UUID, variant 10.
	uuid4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	seen := make(map[string]bool, 64)
	for range 64 {
		id := agent.NewThreadID()
		require.Regexp(t, uuid4, id)
		require.False(t, seen[id], "drew %s twice", id)
		seen[id] = true
	}
}
