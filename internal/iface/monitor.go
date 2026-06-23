//go:build linux

// Package iface manages wireless interface modes via `ip` and `iw`.
// Requires CAP_NET_ADMIN and both tools on PATH.
// This is the subprocess path (ID-001). Pure nl80211 via netlink is a
// future upgrade if Broadcom driver behaviour warrants it.
package iface

import (
	"fmt"
	"os/exec"
	"strings"
)

// SetMonitor puts iface into monitor mode: down → monitor → up.
func SetMonitor(iface string) error {
	if err := run("ip", "link", "set", iface, "down"); err != nil {
		return fmt.Errorf("set %s down: %w", iface, err)
	}
	if err := run("iw", "dev", iface, "set", "type", "monitor"); err != nil {
		return fmt.Errorf("set %s monitor: %w", iface, err)
	}
	if err := run("ip", "link", "set", iface, "up"); err != nil {
		return fmt.Errorf("set %s up: %w", iface, err)
	}
	return nil
}

// SetManaged restores iface to managed (station) mode.
// Errors are best-effort; all three steps run regardless.
func SetManaged(iface string) error {
	_ = run("ip", "link", "set", iface, "down")
	_ = run("iw", "dev", iface, "set", "type", "managed")
	_ = run("ip", "link", "set", iface, "up")
	return nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		cmd := name + " " + strings.Join(args, " ")
		return fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return nil
}
