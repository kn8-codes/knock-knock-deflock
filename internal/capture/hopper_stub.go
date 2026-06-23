//go:build !linux

package capture

import "context"

func Hop(_ context.Context, _ string) {}
