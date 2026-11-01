package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Docker output may contain credentials. Never propagate it to logs or HTTP.
func docker(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("Docker %s failed; inspect container state locally", args[0])
	}
	return out, nil
}
func inspectOwned(ctx context.Context, kind, name, tenant string) (bool, error) {
	out, err := docker(ctx, kind, "inspect", name)
	if err != nil {
		// Distinguish an absent resource from an unavailable daemon.
		if _, health := docker(ctx, "info", "--format", "{{.ServerVersion}}"); health != nil {
			return false, health
		}
		return false, nil
	}
	var items []struct {
		Labels map[string]string
		Config struct{ Labels map[string]string }
	}
	if err := json.Unmarshal(out, &items); err != nil || len(items) != 1 {
		return false, fmt.Errorf("invalid Docker inspect response")
	}
	labels := items[0].Labels
	if kind == "container" {
		labels = items[0].Config.Labels
	}
	if labels["io.axyra.tenant"] != tenant || labels["io.axyra.managed"] != "true" {
		return false, fmt.Errorf("resource name is owned by another workload")
	}
	return true, nil
}
func container(ctx context.Context, tenant, name, image, network string, env []string, extra []string, command ...string) error {
	exists, err := inspectOwned(ctx, "container", name, tenant)
	if err != nil {
		return err
	}
	// Agent containers are stateless. A retry after an agent image update should
	// use the fixed binary; database containers and their volumes are retained.
	if exists && strings.HasSuffix(name, "-agent") {
		current, err := docker(ctx, "container", "inspect", "--format", "{{.Image}}", name)
		if err != nil {
			return err
		}
		wanted, err := docker(ctx, "image", "inspect", "--format", "{{.Id}}", image)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(current)) != strings.TrimSpace(string(wanted)) {
			if _, err = docker(ctx, "stop", "--time", "10", name); err != nil {
				return err
			}
			if _, err = docker(ctx, "rm", name); err != nil {
				return err
			}
			exists = false
		}
	}
	if !exists {
		f, err := os.CreateTemp("", "axyra-env-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err = f.WriteString(strings.Join(env, "\n") + "\n"); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		args := []string{"create", "--name", name, "--network", network, "--restart", "unless-stopped", "--label", "io.axyra.managed=true", "--label", "io.axyra.tenant=" + tenant, "--env-file", f.Name()}
		args = append(args, extra...)
		args = append(args, image)
		args = append(args, command...)
		if _, err = docker(ctx, args...); err != nil {
			return err
		}
	}
	_, err = docker(ctx, "start", name)
	return err
}
