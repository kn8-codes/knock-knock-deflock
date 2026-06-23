//go:build linux

package capture

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const hopDwell = 100 * time.Millisecond

// Hop cycles through every channel supported by iface at hopDwell per channel.
// Returns when ctx is cancelled. Errors from individual channel sets are
// logged and skipped so a single bad channel doesn't stall the hopper.
func Hop(ctx context.Context, iface string) {
	channels, err := supportedChannels(iface)
	if err != nil {
		log.Printf("hopper: %s: channel list failed (%v), using 2.4 GHz defaults", iface, err)
		channels = defaultChannels()
	}
	log.Printf("hopper: %s: %d channels", iface, len(channels))

	i := 0
	for {
		ch := channels[i%len(channels)]
		if err := setChannel(iface, ch); err != nil {
			// transient — driver may reject a channel that's currently radar-detected
			log.Printf("hopper: %s: ch %d: %v", iface, ch, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(hopDwell):
		}
		i++
	}
}

func setChannel(iface string, ch int) error {
	out, err := exec.Command("iw", "dev", iface, "set", "channel", strconv.Itoa(ch)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iw set channel %d: %w — %s", ch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// supportedChannels queries iw for the channels the interface's phy supports.
// It skips disabled channels and channels that require radar detection (DFS) or
// have no-IR restrictions.
func supportedChannels(iface string) ([]int, error) {
	phy, err := phyForIface(iface)
	if err != nil {
		return nil, err
	}

	out, err := exec.Command("iw", "phy", phy, "channels").Output()
	if err != nil {
		return nil, fmt.Errorf("iw phy %s channels: %w", phy, err)
	}

	var channels []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "*") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "disabled") {
			continue
		}
		// [N] is the channel number
		start := strings.IndexByte(line, '[')
		end := strings.IndexByte(line, ']')
		if start < 0 || end <= start {
			continue
		}
		n, err := strconv.Atoi(line[start+1 : end])
		if err != nil {
			continue
		}
		channels = append(channels, n)
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("no usable channels found for phy %s", phy)
	}
	return channels, nil
}

// phyForIface resolves an interface name to its wiphy name (e.g. "phy0").
func phyForIface(iface string) (string, error) {
	out, err := exec.Command("iw", "dev", iface, "info").Output()
	if err != nil {
		return "", fmt.Errorf("iw dev %s info: %w", iface, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "wiphy ") {
			return "phy" + strings.TrimPrefix(line, "wiphy "), nil
		}
	}
	return "", fmt.Errorf("wiphy not found in iw dev %s info output", iface)
}

func defaultChannels() []int {
	return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
}
