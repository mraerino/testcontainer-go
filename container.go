package testcontainer

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"

	"github.com/testcontainers/testcontainer-go/wait"
)

// ContainerProvider allows the creation of containers on an arbitrary system
type ContainerProvider interface {
	CreateContainer(req ContainerRequest) (Container, error)
}

// Container allows getting info about and controlling a single container instance
type Container interface {
	GetHostEndpoint(context.Context, string) (string, error) // combination of IP + Port for the first exposed port
	GetIPAddress(context.Context) (string, error)            // IP address where the container port is exposed
	GetPorts(context.Context) (nat.PortSet, error)           // all exposed ports
	GetMappedPort(context.Context, uint16) (string, error)   // the externally mapped port for a container port
	Start(context.Context) error                             // start the container
	Terminate(context.Context) error                         // terminate the container
}

// ContainerRequest represents the parameters used to get a running container
type ContainerRequest struct {
	Image        string
	Env          map[string]string
	ExportedPort []string
	Cmd          string
	Labels       map[string]string
	RegistryCred string
	WaitingFor   wait.WaitStrategy
	Mounts       []mount.Mount
}

// StackProvider allows the creation of a stack of containers on an arbitrary system
type StackProvider interface {
	CreateStack(spec io.Reader) (Stack, error)
}

type Stack interface {
	// tbd
}
