package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/7samael7/Piper/engine/internal/logs"
	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/secrets"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
)

type jobNetwork struct {
	id         string
	name       string
	containers []string
}

func createJobNetwork(ctx context.Context, cli *client.Client, jobID string, internal bool) (*jobNetwork, error) {
	name := "piper-" + envKey(jobID) + "-" + uuid.NewString()
	response, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: internal,
		Labels:   map[string]string{"piper.job": jobID},
	})
	if err != nil {
		return nil, fmt.Errorf("create job network: %w", err)
	}
	return &jobNetwork{id: response.ID, name: name}, nil
}

func (n *jobNetwork) cleanup(cli *client.Client) {
	if n == nil {
		return
	}
	for index := len(n.containers) - 1; index >= 0; index-- {
		_ = cli.ContainerRemove(context.Background(), n.containers[index], container.RemoveOptions{Force: true})
	}
	_ = cli.NetworkRemove(context.Background(), n.id)
}

func startServices(ctx context.Context, cli *client.Client, networkState *jobNetwork, services []model.ServiceSpec, emit logs.Emitter, masker secrets.Masker) error {
	for _, service := range services {
		reader, err := cli.ImagePull(ctx, service.Image, imagePullOptions())
		if err != nil {
			return fmt.Errorf("pull service %s image %s: %w", service.Name, service.Image, err)
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			_ = reader.Close()
			return fmt.Errorf("read service %s image pull: %w", service.Name, err)
		}
		_ = reader.Close()
		env := []string{}
		for key, value := range service.Env {
			env = append(env, key+"="+value)
		}
		aliases := append([]string(nil), service.Aliases...)
		if len(aliases) == 0 {
			aliases = []string{service.Name}
		}
		exposed := nat.PortSet{}
		bindings := nat.PortMap{}
		for _, configured := range service.Ports {
			hostPort, containerPort, hasHost := strings.Cut(configured, ":")
			if !hasHost {
				containerPort = hostPort
				hostPort = ""
			}
			port, err := nat.NewPort("tcp", containerPort)
			if err != nil {
				return fmt.Errorf("service %s port %q: %w", service.Name, configured, err)
			}
			exposed[port] = struct{}{}
			if hostPort != "" {
				bindings[port] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: hostPort}}
			}
		}
		response, err := cli.ContainerCreate(ctx, &container.Config{
			Image:        service.Image,
			Env:          env,
			ExposedPorts: exposed,
			Labels: map[string]string{
				"piper.service": service.Name,
			},
		}, &container.HostConfig{AutoRemove: false, PortBindings: bindings}, &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkState.name: {Aliases: aliases},
			},
		}, nil, "piper-service-"+uuid.NewString())
		if err != nil {
			return fmt.Errorf("create service %s: %w", service.Name, err)
		}
		networkState.containers = append(networkState.containers, response.ID)
		if err := cli.ContainerStart(ctx, response.ID, container.StartOptions{}); err != nil {
			return fmt.Errorf("start service %s: %w", service.Name, err)
		}
		emit(systemEvent("", "", "service_started", model.RunRunning, fmt.Sprintf("Service %s started from %s.", service.Name, service.Image)))
		streamServiceLogs(ctx, cli, response.ID, service.Name, emit, masker)
		timeout := time.Duration(service.StartupTimeout) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if err := waitForService(ctx, cli, response.ID, timeout); err != nil {
			return fmt.Errorf("service %s failed startup: %w", service.Name, err)
		}
	}
	return nil
}

func waitForService(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		inspection, err := cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return err
		}
		if inspection.State == nil || !inspection.State.Running {
			return fmt.Errorf("container stopped before becoming ready")
		}
		if inspection.State.Health == nil || inspection.State.Health.Status == "healthy" {
			return nil
		}
		if inspection.State.Health.Status == "unhealthy" {
			return fmt.Errorf("container health check reported unhealthy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("startup timed out after %s", timeout)
		case <-ticker.C:
		}
	}
}

func streamServiceLogs(ctx context.Context, cli *client.Client, containerID, serviceName string, emit logs.Emitter, masker secrets.Masker) {
	go func() {
		reader, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true, ShowStderr: true, Follow: true, Timestamps: false,
		})
		if err != nil {
			return
		}
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			message := strings.TrimSpace(scanner.Text())
			if message != "" {
				emit(logs.Event{
					Type: "service_log", Stream: "system",
					Message: masker.Mask(message),
					Data:    map[string]interface{}{"service": serviceName},
				})
			}
		}
	}()
}

func imagePullOptions() image.PullOptions {
	return image.PullOptions{}
}
