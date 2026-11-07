package gnomock

import (
	"context"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/orlangure/gnomock/internal/cleaner"
	"github.com/stretchr/testify/require"
)

// requireContainerGone polls docker until the container with the provided id
// (or id prefix) no longer exists.
func requireContainerGone(t *testing.T, cli *client.Client, id string) {
	t.Helper()

	require.Eventually(t, func() bool {
		_, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})

		return cerrdefs.IsNotFound(err)
	}, 30*time.Second, 250*time.Millisecond, "container %s still exists", id)
}

func TestSidecarCleaner(t *testing.T) {
	t.Parallel()

	cli, err := client.New(client.FromEnv)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, cli.Close()) })

	ctx := context.Background()

	c, err := StartCustom(testImage, DefaultTCP(80), WithTimeout(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, c)

	stopped := false

	t.Cleanup(func() {
		if !stopped {
			_ = Stop(c)
		}
	})

	// The ID has the "<id>-<sidecar>" form, both parts truncated to 10
	// characters.
	id, sidecar := parseID(c.ID)
	require.Len(t, id, 10)
	require.Len(t, sidecar, 10)
	require.Equal(t, id+"-"+sidecar, c.ID)
	require.Equal(t, id, c.DockerID())

	mainInfo, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(mainInfo.Container.ID, id))
	require.NotNil(t, mainInfo.Container.State)
	require.True(t, mainInfo.Container.State.Running)
	require.NotNil(t, mainInfo.Container.HostConfig)
	require.True(t, mainInfo.Container.HostConfig.AutoRemove)

	sc, err := cli.ContainerInspect(ctx, sidecar, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(sc.Container.ID, sidecar))
	require.NotNil(t, sc.Container.State)
	require.True(t, sc.Container.State.Running)
	require.NotNil(t, sc.Container.Config)
	require.Equal(t, cleaner.Image+":"+defaultTag, sc.Container.Config.Image)
	require.NotNil(t, sc.Container.HostConfig)
	require.True(t, sc.Container.HostConfig.AutoRemove)
	require.Len(t, sc.Container.HostConfig.Mounts, 1)
	require.Equal(t, dockerSockAddr, sc.Container.HostConfig.Mounts[0].Source)
	require.Equal(t, dockerSockAddr, sc.Container.HostConfig.Mounts[0].Target)

	require.NoError(t, Stop(c))

	stopped = true

	requireContainerGone(t, cli, id)
	requireContainerGone(t, cli, sidecar)
}

func TestSidecarCleaner_disabled(t *testing.T) {
	t.Parallel()

	cli, err := client.New(client.FromEnv)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, cli.Close()) })

	c, err := StartCustom(
		testImage, DefaultTCP(80),
		WithTimeout(time.Minute),
		WithDisableAutoCleanup(),
	)
	require.NoError(t, err)
	require.NotNil(t, c)

	stopped := false

	t.Cleanup(func() {
		if !stopped {
			_ = Stop(c)
		}
	})

	// Without a sidecar, the ID is the full docker container ID.
	require.NotContains(t, c.ID, "-")
	require.Len(t, c.ID, 64)

	id, sidecar := parseID(c.ID)
	require.Equal(t, c.ID, id)
	require.Empty(t, sidecar)
	require.Equal(t, c.ID, c.DockerID())

	mainInfo, err := cli.ContainerInspect(context.Background(), c.ID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.Equal(t, c.ID, mainInfo.Container.ID)
	require.NotNil(t, mainInfo.Container.State)
	require.True(t, mainInfo.Container.State.Running)

	require.NoError(t, Stop(c))

	stopped = true

	requireContainerGone(t, cli, c.ID)
}
