//go:build windows

package main

import "syscall"

func hiddenProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
