//go:build !linux

package iface

import "errors"

func SetMonitor(_ string) error { return errors.New("iface: monitor mode requires Linux") }
func SetManaged(_ string) error { return nil }
