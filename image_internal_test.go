package gnomock

import (
	"context"
	"io"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

// localOnlyTestImage is a tag that never exists in any registry. It is created
// locally from testImage, so starting a container from it proves that no pull
// happened.
const localOnlyTestImage = "gnomock-local-only-test:phase1"

func TestHasMatchingRepoTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		image    string
		repoTags []string
		want     bool
	}{
		{
			name:     "exact match",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"docker.io/library/postgres:16"},
			want:     true,
		},
		{
			name:     "fully qualified library image vs short local tag",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"postgres:16"},
			want:     true,
		},
		{
			name:     "short image vs short local tag",
			image:    "postgres:16",
			repoTags: []string{"postgres:16"},
			want:     true,
		},
		{
			name:     "fully qualified user image vs local tag without registry",
			image:    "docker.io/orlangure/gnomock-test-image:latest",
			repoTags: []string{"orlangure/gnomock-test-image:latest"},
			want:     true,
		},
		{
			name:     "match in second repo tag of the same image",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"postgres:15", "postgres:16"},
			want:     true,
		},
		{
			name:     "different tag",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"postgres:15"},
			want:     false,
		},
		{
			name:     "different repo",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"mysql:16"},
			want:     false,
		},
		{
			name:     "different user namespace",
			image:    "docker.io/orlangure/gnomock-test-image:latest",
			repoTags: []string{"someone/gnomock-test-image:latest"},
			want:     false,
		},
		{
			name:     "nil repo tags",
			image:    "docker.io/library/postgres:16",
			repoTags: nil,
			want:     false,
		},
		{
			name:     "empty repo tags",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{},
			want:     false,
		},
		{
			name:     "dangling image tag",
			image:    "docker.io/library/postgres:16",
			repoTags: []string{"<none>:<none>"},
			want:     false,
		},
		{
			// Current behavior: the dangling placeholder is compared like any
			// other tag, so an exact string match is reported as found.
			name:     "dangling image tag vs dangling placeholder",
			image:    "<none>:<none>",
			repoTags: []string{"<none>:<none>"},
			want:     true,
		},
		{
			// Current behavior: "library/postgres:16" is not a suffix of
			// "library/xpostgres:16", so this is correctly not a match.
			name:     "repo name that ends with the local repo name",
			image:    "docker.io/library/xpostgres:16",
			repoTags: []string{"postgres:16"},
			want:     false,
		},
		{
			// Current behavior: suffix matching ignores namespace boundaries,
			// so "foolibrary/postgres:16" matches the local "postgres:16".
			name:     "namespace that ends with library",
			image:    "docker.io/foolibrary/postgres:16",
			repoTags: []string{"postgres:16"},
			want:     true,
		},
		{
			// Current behavior: suffix matching ignores namespace boundaries,
			// so "notorlangure/..." matches the local "orlangure/...".
			name:     "namespace that ends with the local namespace",
			image:    "docker.io/notorlangure/gnomock-test-image:latest",
			repoTags: []string{"orlangure/gnomock-test-image:latest"},
			want:     true,
		},
		{
			// Current behavior: the registry host is ignored, so an image from
			// another registry matches a local Docker Hub image.
			name:     "different registry vs short local tag",
			image:    "registry.example.com/library/postgres:16",
			repoTags: []string{"postgres:16"},
			want:     true,
		},
		{
			// Current behavior: the registry host is ignored for namespaced
			// images too.
			name:     "different registry vs local tag without registry",
			image:    "registry.example.com/orlangure/gnomock-test-image:latest",
			repoTags: []string{"orlangure/gnomock-test-image:latest"},
			want:     true,
		},
		{
			// Current behavior: a fully qualified local tag (as some image
			// stores may report it) does not match a short requested image.
			name:     "short image vs fully qualified local tag",
			image:    "postgres:16",
			repoTags: []string{"docker.io/library/postgres:16"},
			want:     false,
		},
		{
			// Current behavior: a requested image without a tag does not match
			// the local "latest" tag. StartCustom never passes such an image
			// because buildImage appends ":latest" first.
			name:     "image without tag vs latest local tag",
			image:    "postgres",
			repoTags: []string{"postgres:latest"},
			want:     false,
		},
		{
			// Current behavior: a requested tagless image matches a local
			// image that is also tagless only by exact string comparison.
			name:     "image without tag vs local tag without tag",
			image:    "postgres",
			repoTags: []string{"postgres"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, hasMatchingRepoTag(tt.image, tt.repoTags))
		})
	}
}

func TestUseLocalImagesFirst_localOnlyImage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseImage := testImage + ":latest"

	cli, err := client.New(client.FromEnv)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, cli.Close())
	})

	ensureLocalImage(ctx, t, cli, baseImage)

	_, err = cli.ImageTag(ctx, client.ImageTagOptions{
		Source: baseImage,
		Target: localOnlyTestImage,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		// Only the local tag is removed, the base image stays in place.
		_, err := cli.ImageRemove(context.Background(), localOnlyTestImage, client.ImageRemoveOptions{})
		require.NoError(t, err)
	})

	t.Run("starts without pulling when local images are preferred", func(t *testing.T) {
		t.Parallel()

		c, err := StartCustom(
			localOnlyTestImage,
			NamedPorts{"web80": TCP(80)},
			WithUseLocalImagesFirst(),
			WithTimeout(time.Minute),
		)
		require.NoError(t, err)
		require.NotNil(t, c)

		require.NotEmpty(t, c.Host)
		require.NotZero(t, c.Port("web80"))
		require.NotEmpty(t, c.Address("web80"))

		require.NoError(t, Stop(c))
	})

	t.Run("fails to pull when local images are not preferred", func(t *testing.T) {
		t.Parallel()

		c, err := StartCustom(
			localOnlyTestImage,
			NamedPorts{"web80": TCP(80)},
			WithTimeout(time.Minute),
		)
		require.ErrorContains(t, err, "can't pull image")
		require.Nil(t, c)
	})

	t.Run("isExistingLocalImage", func(t *testing.T) {
		t.Parallel()

		gg, err := newG(false)
		require.NoError(t, err)

		d, err := gg.dockerConnect()
		require.NoError(t, err)

		defer func() {
			require.NoError(t, d.stopClient())
		}()

		tests := []struct {
			name  string
			image string
			want  bool
		}{
			{name: "local only tag", image: localOnlyTestImage, want: true},
			{name: "fully qualified base image", image: baseImage, want: true},
			{name: "absent tag of existing repo", image: testImage + ":absent-tag", want: false},
			{name: "absent image", image: "docker.io/orlangure/gnomock-absent-image:latest", want: false},
		}

		for _, tt := range tests {
			ok, err := d.isExistingLocalImage(ctx, tt.image)
			require.NoError(t, err, tt.name)
			require.Equal(t, tt.want, ok, tt.name)
		}
	})
}

// ensureLocalImage pulls the image unless it is already available locally.
func ensureLocalImage(ctx context.Context, t *testing.T, cli *client.Client, image string) {
	t.Helper()

	_, err := cli.ImageInspect(ctx, image)
	if err == nil {
		return
	}

	require.True(t, cerrdefs.IsNotFound(err), "unexpected inspect error: %v", err)

	resp, err := cli.ImagePull(ctx, image, client.ImagePullOptions{})
	require.NoError(t, err)

	_, err = io.Copy(io.Discard, resp)
	require.NoError(t, err)
	require.NoError(t, resp.Close())
}
