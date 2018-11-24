package testcontainer

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/docker/docker/api/types/mount"

	"github.com/pkg/errors"
)

// TestcontainerLabel is used as a base for docker labels
const (
	TestcontainerLabel          = "org.testcontainers.golang"
	TestcontainerLabelSessionID = TestcontainerLabel + ".sessionId"
	ReaperDefaultImage          = "quay.io/testcontainers/ryuk:0.2.2"
)

type ReaperProvider interface {
	RunContainer(ctx context.Context, req ContainerRequest) (Container, error)
}

type Reaper struct {
	Provider  ReaperProvider
	SessionID string
	Endpoint  string
}

func NewReaper(ctx context.Context, sessionID string, provider ReaperProvider) (*Reaper, error) {
	r := &Reaper{
		Provider:  provider,
		SessionID: sessionID,
	}

	req := ContainerRequest{
		Image:  ReaperDefaultImage,
		Labels: r.GetLabels(),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: "/var/run/docker.sock",
				Target: "/var/run/docker.sock",
			},
		},
	}

	c, err := provider.RunContainer(ctx, req)
	if err != nil {
		return nil, err
	}

	endpoint, err := c.GetHostEndpoint(ctx, "8080")
	if err != nil {
		return nil, err
	}
	r.Endpoint = endpoint

	return r, nil
}

// Connect runs a goroutine which can be terminated by sending true into the returned channel
func (r *Reaper) Connect() (chan bool, error) {
	conn, err := net.Dial("tcp", r.Endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "Connecting to Ryuk on "+r.Endpoint+" failed")
	}

	terminationSignal := make(chan bool)
	go func() {
		sock := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		defer conn.Close()

		labelFilters := []string{}
		for l, v := range r.GetLabels() {
			labelFilters = append(labelFilters, fmt.Sprintf("label=%s=%s", l, v))
		}

		retryLimit := 3
		for {
			if retryLimit <= 0 {
				fmt.Println("Warning: Could not instrument reaper sidecar. Check for zombie containers!")
				return
			}
			retryLimit--

			sock.WriteString(strings.Join(labelFilters, "&"))
			if err := sock.Flush(); err != nil {
				continue
			}

			resp, err := sock.ReadString('\n')
			if err != nil {
				continue
			}
			if resp == "ACK" {
				break
			}
		}

		<-terminationSignal
	}()
	return terminationSignal, nil
}

func (r *Reaper) GetLabels() map[string]string {
	return map[string]string{
		TestcontainerLabel:          "true",
		TestcontainerLabelSessionID: r.SessionID,
	}
}
