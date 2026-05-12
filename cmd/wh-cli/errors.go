package main

import "errors"

var (
	errAuth              = errors.New("authentication failed")
	errDaemonUnavailable = errors.New("daemon unavailable")
	errInvalidInput      = errors.New("invalid input")
	errLocked            = errors.New("locked")
)
