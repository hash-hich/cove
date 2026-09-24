package spec_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

func valid() *spec.Run {
	return &spec.Run{
		Hostname:     "sandbox",
		Nameservers:  []string{"10.0.2.3"},
		Layers:       []spec.Layer{{Disk: spec.Disk{Size: 8192}, DiffID: "sha256:aa", Mountpoint: "/l/00"}},
		MountOptions: []string{"xino=on"},
		Write:        spec.Disk{Size: 1 << 30},
		User:         "node",
		Env:          []string{"PATH=/usr/bin"},
		WorkingDir:   "/home/node",
		Volumes:      []string{"/var/lib/docker"},
		StopSignal:   "SIGINT",
		Project:      "front",
		Agent:        "claude",
	}
}

func TestReadWhatWasWritten(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "run.json")
	var buf bytes.Buffer
	require.NoError(t, valid().Encode(&buf))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	got, err := spec.Read(path)
	require.NoError(t, err)
	require.Equal(t, valid(), got)
}

func TestDecodeRefusesAFieldItDoesNotKnow(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, valid().Encode(&buf))
	withMore := strings.Replace(buf.String(), `"hostname"`, `"network": "tap", "hostname"`, 1)

	_, err := spec.Decode(strings.NewReader(withMore))
	require.ErrorContains(t, err, `unknown field "network"`)
}

func TestDecodeRefusesWhatTheInitCannotActOn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*spec.Run)
		want   string
	}{
		{"no layer", func(s *spec.Run) { s.Layers = nil }, "names no layer"},
		{"layer without size", func(s *spec.Run) { s.Layers[0].Size = 0 }, "gives layer sha256:aa no size"},
		{"relative mountpoint", func(s *spec.Run) { s.Layers[0].Mountpoint = "l/00" }, `on "l/00"`},
		{"unclean mountpoint", func(s *spec.Run) { s.Layers[0].Mountpoint = "/l/../00" }, `on "/l/../00"`},
		{"root mountpoint", func(s *spec.Run) { s.Layers[0].Mountpoint = "/" }, `on "/"`},
		{"write disk without size", func(s *spec.Run) { s.Write.Size = 0 }, "gives the write disk no size"},
		{"no host", func(s *spec.Run) { s.Hostname = "" }, "names no host"},
		{"no agent", func(s *spec.Run) { s.Agent = "" }, `names the agent ""`},
		{"agent as a path", func(s *spec.Run) { s.Agent = "/usr/bin/claude" }, `names the agent "/usr/bin/claude"`},
		{"no project", func(s *spec.Run) { s.Project = "" }, `names the project ""`},
		{"project climbing", func(s *spec.Run) { s.Project = ".." }, `names the project ".."`},
		{"project as a path", func(s *spec.Run) { s.Project = "a/b" }, `names the project "a/b"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := valid()
			tt.change(s)
			var buf bytes.Buffer
			require.NoError(t, s.Encode(&buf))

			_, err := spec.Decode(&buf)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
