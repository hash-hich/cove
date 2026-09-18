package image_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

func TestParse(t *testing.T) {
	t.Parallel()

	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// tagged is a reference already in its canonical form.
	const tagged = "ghcr.io/org/repo:tag"
	tests := []struct {
		ref  string
		want string
	}{
		{ref: tagged, want: tagged},
		{ref: "ghcr.io/org/repo", want: "ghcr.io/org/repo:latest"},
		{ref: "ghcr.io/org/repo@" + digest, want: "ghcr.io/org/repo@" + digest},
		{ref: "localhost:5000/repo:v1", want: "localhost:5000/repo:v1"},
		{ref: "docker.io/library/debian:12", want: "index.docker.io/library/debian:12"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()

			ref, err := image.Parse(tt.ref)

			require.NoError(t, err)
			require.Equal(t, tt.want, ref.Name())
		})
	}
}

func TestParseRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want error
	}{
		{name: "bare name", ref: "demo", want: image.ErrNoRegistry},
		{name: "docker.io implied", ref: "org/repo:tag", want: image.ErrNoRegistry},
		{name: "empty", ref: "", want: image.ErrNoRegistry},
		{name: "bad tag", ref: "ghcr.io/org/repo:a tag"},
		{name: "bad digest", ref: "ghcr.io/org/repo@sha256:short"},
		{name: "upper case repository", ref: "ghcr.io/Org/Repo:tag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := image.Parse(tt.ref)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.ref)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}
