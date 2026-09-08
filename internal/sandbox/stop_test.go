package sandbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

func TestStopSpecArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec sandbox.StopSpec
		want string
	}{
		{
			name: "one target",
			spec: sandbox.StopSpec{Targets: []string{"demo"}},
			want: "stop demo",
		},
		{
			name: "several targets",
			spec: sandbox.StopSpec{Targets: []string{"a", "b"}},
			want: "stop a b",
		},
		{
			name: "all options",
			spec: sandbox.StopSpec{Targets: []string{"sb1"}, Signal: "SIGKILL", Timeout: new(30)},
			want: "stop --signal SIGKILL --time 30 sb1",
		},
		{
			name: "zero timeout is passed, not dropped",
			spec: sandbox.StopSpec{Targets: []string{"sb2"}, Timeout: new(0)},
			want: "stop --time 0 sb2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, strings.Fields(tt.want), tt.spec.Args())
		})
	}
}
