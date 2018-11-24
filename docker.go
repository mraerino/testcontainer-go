package testcontainer

import (
	"context"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/testcontainers/testcontainer-go/wait"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// DockerContainer represents a container started using Docker
type DockerContainer struct {
	// Container ID from Docker
	ID         string
	WaitingFor wait.WaitStrategy

	sessionID uuid.UUID
	// Cache to retrieve container infromation without re-fetching them from dockerd
	raw      *types.ContainerJSON
	provider *DockerProvider
}

// LivenessCheckPorts (deprecated) returns the exposed ports for the container.
func (c *DockerContainer) LivenessCheckPorts(ctx context.Context) (nat.PortSet, error) {
	return c.GetPorts(ctx)
}

// GetPorts returns the exposed ports for the container.
func (c *DockerContainer) GetPorts(ctx context.Context) (nat.PortSet, error) {
	inspect, err := c.inspectContainer(ctx)
	if err != nil {
		return nil, err
	}
	return inspect.Config.ExposedPorts, nil
}

func (c *DockerContainer) GetMappedPort(ctx context.Context, port uint16) (string, error) {
	inspect, err := c.inspectContainer(ctx)
	if err != nil {
		return "", err
	}

	for k, p := range inspect.NetworkSettings.Ports {
		if k.Port() == strconv.Itoa(int(port)) {
			return p[0].HostPort, nil
		}
	}
	return "0", nil
}

// Start will start an already created container
func (c *DockerContainer) Start(ctx context.Context) error {
	if err := c.provider.client.ContainerStart(ctx, c.ID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	// if a WaitStrategy has been specified, wait before returning
	if c.WaitingFor != nil {
		if err := c.WaitingFor.WaitUntilReady(ctx, c); err != nil {
			return err
		}
	}

	return nil
}

// Terminate is used to kill the container. It is usally triggered by as defer function.
func (c *DockerContainer) Terminate(ctx context.Context) error {
	return c.provider.client.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{
		Force: true,
	})
}

func (c *DockerContainer) inspectContainer(ctx context.Context) (*types.ContainerJSON, error) {
	if c.raw != nil {
		return c.raw, nil
	}
	inspect, err := c.provider.client.ContainerInspect(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	c.raw = &inspect
	return c.raw, nil
}

// GetIPAddress returns the ip address for the running container.
func (c *DockerContainer) GetIPAddress(ctx context.Context) (string, error) {
	inspect, err := c.inspectContainer(ctx)
	if err != nil {
		return "", err
	}
	return inspect.NetworkSettings.IPAddress, nil
}

// GetHostEndpoint returns the IP address and the port exposed on the host machine.
func (c *DockerContainer) GetHostEndpoint(ctx context.Context, port string) (string, error) {
	inspect, err := c.inspectContainer(ctx)
	if err != nil {
		return "", err
	}

	portSet, _, err := nat.ParsePortSpecs([]string{port})
	if err != nil {
		return "", err
	}

	for p := range portSet {
		ports, ok := inspect.NetworkSettings.Ports[p]
		if !ok {
			return "", fmt.Errorf("port %s not found", port)
		}
		if len(ports) == 0 {
			return "", fmt.Errorf("port %s not found", port)
		}

		return fmt.Sprintf("%s:%s", ports[0].HostIP, ports[0].HostPort), nil

	}

	return "", fmt.Errorf("port %s not found", port)
}

type DockerProvider struct {
	client *client.Client
}

// CreateContainer fulfills a request for a container without starting it
func (p *DockerProvider) CreateContainer(ctx context.Context, req ContainerRequest) (Container, error) {
	exposedPortSet, exposedPortMap, err := nat.ParsePortSpecs(req.ExportedPort)
	if err != nil {
		return nil, err
	}

	env := []string{}
	for envKey, envVar := range req.Env {
		env = append(env, envKey+"="+envVar)
	}

	sessionID := uuid.NewV4()
	r, err := NewReaper(ctx, sessionID.String(), p)
	if err != nil {
		return nil, errors.Wrap(err, "creating reaper failed")
	}

	dockerInput := &container.Config{
		Image:        req.Image,
		Env:          env,
		ExposedPorts: exposedPortSet,
		Labels:       r.GetLabels(),
	}

	if req.Cmd != "" {
		dockerInput.Cmd = strings.Split(req.Cmd, " ")
	}

	_, _, err = p.client.ImageInspectWithRaw(ctx, req.Image)
	if err != nil {
		if client.IsErrNotFound(err) {
			pullOpt := types.ImagePullOptions{}
			if req.RegistryCred != "" {
				pullOpt.RegistryAuth = req.RegistryCred
			}
			pull, err := p.client.ImagePull(ctx, req.Image, pullOpt)
			if err != nil {
				return nil, err
			}
			defer pull.Close()

			// download of docker image finishes at EOF of the pull request
			_, err = ioutil.ReadAll(pull)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: exposedPortMap,
		Mounts:       req.Mounts,
	}

	resp, err := p.client.ContainerCreate(ctx, dockerInput, hostConfig, nil, "")
	if err != nil {
		return nil, err
	}

	c := &DockerContainer{
		ID:         resp.ID,
		WaitingFor: req.WaitingFor,
		sessionID:  sessionID,
		provider:   p,
	}

	return c, nil
}

// RunContainer takes a RequestContainer as input and it runs a container via the docker sdk
func (p *DockerProvider) RunContainer(ctx context.Context, req ContainerRequest) (Container, error) {
	c, err := p.CreateContainer(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := c.Start(ctx); err != nil {
		return c, errors.Wrap(err, "could not start container")
	}

	return c, nil
}
