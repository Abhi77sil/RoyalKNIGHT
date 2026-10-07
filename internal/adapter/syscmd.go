package adapter

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

var allowedServices = map[string]bool{
	"caddy": true,
	"nginx": true,
}

var allowedActions = map[string]bool{
	"start":     true,
	"stop":      true,
	"restart":   true,
	"reload":    true,
	"is-active": true,
}

// ExecSystemctl executes systemctl safely with whitelisted services and actions.
func ExecSystemctl(action, service string) error {
	if !allowedActions[action] {
		return fmt.Errorf("invalid systemctl action: %s", action)
	}
	if !allowedServices[service] {
		return fmt.Errorf("invalid service identifier: %s", service)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", action, service)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s %s failed: %w (stderr: %s)", action, service, err, stderr.String())
	}
	return nil
}
