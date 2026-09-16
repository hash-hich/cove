package sandbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

func TestSeedSpecSteps(t *testing.T) {
	t.Parallel()

	spec := sandbox.SeedSpec{Target: demo, Branch: "main", Author: "agent", Email: "agent@cove.invalid"}
	want := []string{
		"exec demo git -C /work init -q -b main",
		"exec --interactive demo cp /dev/stdin /tmp/cove.bundle",
		"exec demo git -C /work fetch -q --update-head-ok --no-write-fetch-head /tmp/cove.bundle" +
			" +refs/heads/*:refs/heads/* +refs/tags/*:refs/tags/*",
		"exec demo rm /tmp/cove.bundle",
		"exec demo git -C /work reset -q --hard",
		"exec demo git -C /work config user.name agent",
		"exec demo git -C /work config user.email agent@cove.invalid",
	}

	steps := spec.Steps()

	require.Len(t, steps, len(want))
	for i, step := range steps {
		require.Equal(t, strings.Fields(want[i]), step.Args, "step %d", i)
		// The bundle goes to the copy and nowhere else.
		require.Equal(t, step.Args[3] == "cp", step.Bundle, "step %d", i)
	}
}

// TestSeedSpecStepsNeverRunAShellOrAnotherUser: every step is a fixed program of the image run as
// its user, so no SeedSpec, however hostile, may reach a shell, another user, or a working
// directory.
func TestSeedSpecStepsNeverRunAShellOrAnotherUser(t *testing.T) {
	t.Parallel()

	spec := sandbox.SeedSpec{Target: "-u", Branch: "-c", Author: "sh", Email: "--user=root"}
	programs := []string{"git", "cp", "rm"}

	for _, step := range spec.Steps() {
		require.Equal(t, "exec", step.Args[0])
		// The only flag of exec is the one that hands the bundle over; the target comes right after.
		rest := step.Args[1:]
		if rest[0] == "--interactive" {
			rest = rest[1:]
		}
		require.Equal(t, spec.Target, rest[0])
		require.Contains(t, programs, rest[1])
	}
}
