package gnomock

import (
	"errors"
	"fmt"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

// These tests pin down how gnomock converts its own port types to and from
// the moby API types, and how it recognizes specific daemon errors. They don't
// need a running Docker daemon.

func TestDockerExposedPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       NamedPorts
		expected network.PortSet
	}{
		{
			name:     "nil input",
			in:       nil,
			expected: network.PortSet{},
		},
		{
			name:     "empty input",
			in:       NamedPorts{},
			expected: network.PortSet{},
		},
		{
			name: "tcp and udp",
			in: NamedPorts{
				"web":  TCP(80),
				"dns":  {Protocol: "udp", Port: 53},
				"api":  {Protocol: "tcp", Port: 8080, HostPort: 18080},
				"high": TCP(65535),
				"zero": TCP(0),
			},
			expected: network.PortSet{
				network.MustParsePort("80/tcp"):    {},
				network.MustParsePort("53/udp"):    {},
				network.MustParsePort("8080/tcp"):  {},
				network.MustParsePort("65535/tcp"): {},
				network.MustParsePort("0/tcp"):     {},
			},
		},
		{
			name: "same port with different protocols",
			in: NamedPorts{
				"tcp": TCP(1234),
				"udp": {Protocol: "udp", Port: 1234},
			},
			expected: network.PortSet{
				network.MustParsePort("1234/tcp"): {},
				network.MustParsePort("1234/udp"): {},
			},
		},
		{
			name: "upper case protocol is normalized",
			in: NamedPorts{
				"web": {Protocol: "TCP", Port: 80},
			},
			expected: network.PortSet{
				network.MustParsePort("80/tcp"): {},
			},
		},
		{
			name: "out of range ports are skipped",
			in: NamedPorts{
				"negative": TCP(-1),
				"too-high": TCP(65536),
				"huge":     {Protocol: "udp", Port: 1 << 20},
				"ok":       TCP(5432),
			},
			expected: network.PortSet{
				network.MustParsePort("5432/tcp"): {},
			},
		},
		{
			// Current behavior: a port without a protocol is silently
			// dropped because network.PortFrom rejects an empty protocol.
			name: "empty protocol is skipped",
			in: NamedPorts{
				"no-proto": {Port: 80},
				"ok":       TCP(81),
			},
			expected: network.PortSet{
				network.MustParsePort("81/tcp"): {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := (&docker{}).exposedPorts(tt.in)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestDockerPortBindings(t *testing.T) {
	// These cases use t.Setenv, so neither the test nor its subtests can run
	// in parallel.
	ports := NamedPorts{
		"web":    TCP(80),
		"custom": {Protocol: "tcp", Port: 8080, HostPort: 18080},
		"zero":   {Protocol: "udp", Port: 53, HostPort: 0},
	}

	tests := []struct {
		name       string
		gnomockEnv string
		dockerHost string
		hostIP     string
	}{
		{
			name:   "default host ip",
			hostIP: "127.0.0.1",
		},
		{
			name:       "inside gnomockd",
			gnomockEnv: "gnomockd",
			hostIP:     "0.0.0.0",
		},
		{
			name:       "inside gnomockd overrides remote docker host",
			gnomockEnv: "gnomockd",
			dockerHost: "tcp://10.1.2.3:2375",
			hostIP:     "0.0.0.0",
		},
		{
			name:       "other gnomock env value",
			gnomockEnv: "something-else",
			hostIP:     "127.0.0.1",
		},
		{
			name:       "remote docker host with ip",
			dockerHost: "tcp://10.1.2.3:2375",
			hostIP:     "10.1.2.3",
		},
		{
			name:       "remote docker host with ipv6",
			dockerHost: "tcp://[fd00::1]:2375",
			hostIP:     "fd00::1",
		},
		{
			// hostAddr returns "docker-host", which is not an IP, so the
			// binding falls back to localhost.
			name:       "remote docker host with hostname",
			dockerHost: "tcp://docker-host:2375",
			hostIP:     "127.0.0.1",
		},
		{
			name:       "unix socket",
			dockerHost: "unix:///var/run/docker.sock",
			hostIP:     "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GNOMOCK_ENV", tt.gnomockEnv)
			t.Setenv("DOCKER_HOST", tt.dockerHost)

			d := &docker{}
			exposed := d.exposedPorts(ports)
			got := d.portBindings(exposed, ports)

			hostIP := netip.MustParseAddr(tt.hostIP)
			expected := network.PortMap{
				network.MustParsePort("80/tcp"): {
					{HostIP: hostIP, HostPort: ""},
				},
				network.MustParsePort("8080/tcp"): {
					{HostIP: hostIP, HostPort: "18080"},
				},
				network.MustParsePort("53/udp"): {
					{HostIP: hostIP, HostPort: ""},
				},
			}
			require.Equal(t, expected, got)
		})
	}

	t.Run("empty input", func(t *testing.T) {
		t.Setenv("GNOMOCK_ENV", "")
		t.Setenv("DOCKER_HOST", "")

		got := (&docker{}).portBindings(network.PortSet{}, NamedPorts{})
		require.Equal(t, network.PortMap{}, got)
	})

	t.Run("exposed port without a named port", func(t *testing.T) {
		t.Setenv("GNOMOCK_ENV", "")
		t.Setenv("DOCKER_HOST", "")

		exposed := network.PortSet{network.MustParsePort("9000/tcp"): {}}
		got := (&docker{}).portBindings(exposed, NamedPorts{})
		require.Equal(t, network.PortMap{
			network.MustParsePort("9000/tcp"): {
				{HostIP: netip.MustParseAddr("127.0.0.1")},
			},
		}, got)
	})
}

func TestDockerBoundNamedPorts(t *testing.T) {
	t.Parallel()

	v4 := netip.MustParseAddr("0.0.0.0")
	v6 := netip.MustParseAddr("::")

	inspect := func(ports network.PortMap) container.InspectResponse {
		return container.InspectResponse{
			NetworkSettings: &container.NetworkSettings{Ports: ports},
		}
	}

	t.Run("bindings are mapped to named ports", func(t *testing.T) {
		t.Parallel()

		namedPorts := NamedPorts{
			"web": TCP(80),
			"dns": {Protocol: "udp", Port: 53},
		}
		resp := inspect(network.PortMap{
			// IPv4 and IPv6 bindings for the same port: the first one wins.
			network.MustParsePort("80/tcp"): {
				{HostIP: v4, HostPort: "32768"},
				{HostIP: v6, HostPort: "32769"},
			},
			network.MustParsePort("53/udp"): {
				{HostIP: v4, HostPort: "32770"},
			},
			// Ports without bindings are skipped, even if they are not
			// in namedPorts.
			network.MustParsePort("9999/tcp"): {},
			network.MustParsePort("9998/tcp"): nil,
		})

		got, err := (&docker{}).boundNamedPorts(resp, namedPorts)
		require.NoError(t, err)
		require.Equal(t, NamedPorts{
			"web": {Protocol: "tcp", Port: 32768},
			"dns": {Protocol: "udp", Port: 32770},
		}, got)
	})

	t.Run("ipv6 binding first", func(t *testing.T) {
		t.Parallel()

		resp := inspect(network.PortMap{
			network.MustParsePort("80/tcp"): {
				{HostIP: v6, HostPort: "40001"},
				{HostIP: v4, HostPort: "40000"},
			},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.NoError(t, err)
		require.Equal(t, NamedPorts{"web": {Protocol: "tcp", Port: 40001}}, got)
	})

	t.Run("no ports", func(t *testing.T) {
		t.Parallel()

		got, err := (&docker{}).boundNamedPorts(inspect(nil), NamedPorts{"web": TCP(80)})
		require.NoError(t, err)
		require.Equal(t, NamedPorts{}, got)
	})

	t.Run("only empty bindings", func(t *testing.T) {
		t.Parallel()

		resp := inspect(network.PortMap{
			network.MustParsePort("80/tcp"): {},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.NoError(t, err)
		require.Equal(t, NamedPorts{}, got)
	})

	t.Run("invalid host port", func(t *testing.T) {
		t.Parallel()

		resp := inspect(network.PortMap{
			network.MustParsePort("80/tcp"): {
				{HostIP: v4, HostPort: "abc"},
			},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "invalid host port value 'abc'")
	})

	t.Run("empty host port", func(t *testing.T) {
		t.Parallel()

		// Current behavior: an empty HostPort in the first binding is an
		// error, not a skipped port.
		resp := inspect(network.PortMap{
			network.MustParsePort("80/tcp"): {
				{HostIP: v4, HostPort: ""},
			},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.Error(t, err)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "invalid host port value ''")
	})

	t.Run("unknown port", func(t *testing.T) {
		t.Parallel()

		resp := inspect(network.PortMap{
			network.MustParsePort("8080/tcp"): {
				{HostIP: v4, HostPort: "32768"},
			},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.ErrorIs(t, err, ErrPortNotFound)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "can't find port tcp/8080")
	})

	t.Run("protocol mismatch", func(t *testing.T) {
		t.Parallel()

		resp := inspect(network.PortMap{
			network.MustParsePort("80/udp"): {
				{HostIP: v4, HostPort: "32768"},
			},
		})

		got, err := (&docker{}).boundNamedPorts(resp, NamedPorts{"web": TCP(80)})
		require.ErrorIs(t, err, ErrPortNotFound)
		require.Nil(t, got)
	})
}

func TestContainerGateway(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		networks map[string]*network.EndpointSettings
		expected string
	}{
		{
			name:     "nil networks",
			networks: nil,
			expected: "",
		},
		{
			name:     "no networks",
			networks: map[string]*network.EndpointSettings{},
			expected: "",
		},
		{
			name: "nil endpoint",
			networks: map[string]*network.EndpointSettings{
				"bridge": nil,
			},
			expected: "",
		},
		{
			name: "zero gateway",
			networks: map[string]*network.EndpointSettings{
				"bridge": {},
			},
			expected: "",
		},
		{
			name: "valid gateway",
			networks: map[string]*network.EndpointSettings{
				"bridge": {Gateway: netip.MustParseAddr("172.17.0.1")},
			},
			expected: "172.17.0.1",
		},
		{
			name: "ipv6 gateway",
			networks: map[string]*network.EndpointSettings{
				"v6": {Gateway: netip.MustParseAddr("fd00::1")},
			},
			expected: "fd00::1",
		},
		{
			// Current behavior: the unspecified address is a valid
			// netip.Addr, so it is returned as is.
			name: "unspecified gateway",
			networks: map[string]*network.EndpointSettings{
				"bridge": {Gateway: netip.MustParseAddr("0.0.0.0")},
			},
			expected: "0.0.0.0",
		},
		{
			name: "invalid endpoints are skipped",
			networks: map[string]*network.EndpointSettings{
				"a": nil,
				"b": {},
				"c": {IPv6Gateway: netip.MustParseAddr("fd00::1")},
				"d": {Gateway: netip.MustParseAddr("172.18.0.1")},
				"e": {Gateway: netip.Addr{}},
			},
			expected: "172.18.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, containerGateway(tt.networks))
		})
	}

	t.Run("multiple valid gateways", func(t *testing.T) {
		t.Parallel()

		// Map iteration order is random, so any valid gateway can be
		// returned, but it is always one of them.
		networks := map[string]*network.EndpointSettings{
			"a": {Gateway: netip.MustParseAddr("172.17.0.1")},
			"b": {Gateway: netip.MustParseAddr("172.18.0.1")},
		}

		require.Contains(t, []string{"172.17.0.1", "172.18.0.1"}, containerGateway(networks))
	})
}

func TestDuplicateContainerRegexp(t *testing.T) {
	t.Parallel()

	const (
		id         = "6a8f1e2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f"
		daemonText = `Conflict. The container name "/foo" is already in use by container "` + id +
			`". You have to remove (or rename) that container to be able to reuse that name.`
	)

	tests := []struct {
		name  string
		err   error
		match bool
	}{
		{
			name:  "daemon message",
			err:   errors.New(daemonText),
			match: true,
		},
		{
			// This is the shape moby/client returns today.
			name:  "client prefix",
			err:   fmt.Errorf("Error response from daemon: %w", errors.New(daemonText)),
			match: true,
		},
		{
			name:  "wrapped by gnomock",
			err:   fmt.Errorf("can't create container: %w", errors.New("Error response from daemon: "+daemonText)),
			match: true,
		},
		{
			name: "container name with dashes",
			err: errors.New(`Conflict. The container name "/gnomock-1234-abcd" is already in use by container "` +
				id + `". You have to remove (or rename) that container to be able to reuse that name.`),
			match: true,
		},
		{
			name:  "unrelated error",
			err:   errors.New("Error response from daemon: No such container: foo"),
			match: false,
		},
		{
			name:  "other conflict",
			err:   errors.New(`Error response from daemon: Conflict. The container "` + id + `" is already running.`),
			match: false,
		},
		{
			name:  "truncated message",
			err:   errors.New(`Conflict. The container name "/foo" is already in use by container "` + id + `".`),
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matches := duplicateContainerRegexp.FindStringSubmatch(tt.err.Error())
			if !tt.match {
				require.Nil(t, matches)
				return
			}

			require.Len(t, matches, 2)
			require.Equal(t, id, matches[1])
		})
	}
}

func TestIsDeletionAlreadyInProgessError(t *testing.T) {
	t.Parallel()

	const id = "6a8f1e2b3c4d"

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "exact message",
			err:      errors.New("Error response from daemon: removal of container " + id + " is already in progress"),
			expected: true,
		},
		{
			name: "client wrapped message",
			err: fmt.Errorf("Error response from daemon: %w",
				errors.New("removal of container "+id+" is already in progress")),
			expected: true,
		},
		{
			name:     "other container id",
			err:      errors.New("Error response from daemon: removal of container other is already in progress"),
			expected: false,
		},
		{
			// Current behavior: the match is on the exact string, so the
			// message without the client prefix doesn't match.
			name:     "missing client prefix",
			err:      errors.New("removal of container " + id + " is already in progress"),
			expected: false,
		},
		{
			// Current behavior: any extra wrapping breaks the exact match.
			name: "wrapped again",
			err: fmt.Errorf("remove: %w",
				errors.New("Error response from daemon: removal of container "+id+" is already in progress")),
			expected: false,
		},
		{
			name:     "other message",
			err:      errors.New("Error response from daemon: No such container: " + id),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, isDeletionAlreadyInProgessError(tt.err, id))
		})
	}
}
