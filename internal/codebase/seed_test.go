package codebase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/codebase"
)

// demo is the sandbox the seeding tests name.
const demo = "demo"

func TestSeedSpecSteps(t *testing.T) {
	t.Parallel()

	spec := codebase.SeedSpec{Target: demo, Branch: "main", Author: "agent", Email: "agent@cove.invalid"}
	want := []string{
		"git -C /work init -q -b main",
		"cp /dev/stdin /tmp/cove.bundle",
		"git -C /work fetch -q --update-head-ok --no-write-fetch-head /tmp/cove.bundle" +
			" +refs/heads/*:refs/heads/* +refs/tags/*:refs/tags/*",
		"rm /tmp/cove.bundle",
		"git -C /work reset -q --hard",
		"git -C /work config user.name agent",
		"git -C /work config user.email agent@cove.invalid",
	}

	steps := spec.Steps()

	require.Len(t, steps, len(want))
	for i, step := range steps {
		require.Equal(t, strings.Fields(want[i]), step.Args, "step %d", i)
		// The bundle goes to the copy and nowhere else.
		require.Equal(t, step.Args[0] == "cp", step.Bundle, "step %d", i)
	}
}

// TestSeedSpecStepsNeverRunAShell: every step is a fixed program of the image, so no SeedSpec,
// however hostile, may put a shell or a flag in the place of the program.
func TestSeedSpecStepsNeverRunAShell(t *testing.T) {
	t.Parallel()

	spec := codebase.SeedSpec{Target: "-u", Branch: "-c", Author: "sh", Email: "--user=root"}
	programs := []string{"git", "cp", "rm"}

	for _, step := range spec.Steps() {
		require.Contains(t, programs, step.Args[0])
	}
}
