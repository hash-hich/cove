package image_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

func TestNamesRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		image string
		want  bool
	}{
		{image: "demo"},
		{image: "cove-sandbox:local"},
		{image: "org/repo:tag"},
		{image: "org/repo@sha256:0123456789abcdef"},
		{image: ""},
		{image: "ghcr.io/org/repo:tag", want: true},
		{image: "docker.io/library/debian", want: true},
		{image: "localhost/repo", want: true},
		{image: "localhost:5000/repo", want: true},
		{image: "registry:5000/org/repo:tag", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, image.NamesRegistry(tt.image))
		})
	}
}
