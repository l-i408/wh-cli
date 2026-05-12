package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		code := exitCode(err)
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(code)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return runHelp(ctx, nil)
	}

	switch args[0] {
	case "help", "--help", "-h":
		return runHelp(ctx, args[1:])
	case "daemon":
		return runDaemon(ctx, args[1:])
	case "setup":
		return runSetup(ctx, args[1:])
	case "login":
		return runLogin(ctx, args[1:])
	case "logout":
		return runLogout(ctx, args[1:])
	case "unlock":
		return runUnlock(ctx, args[1:])
	case "status":
		return runStatus(ctx, args[1:])
	case "qr":
		return runQR(ctx, args[1:])
	case "pair-code":
		return runPairCode(ctx, args[1:])
	case "send":
		return runSend(ctx, args[1:])
	case "react":
		return runReact(ctx, args[1:])
	case "reply":
		return runReply(ctx, args[1:])
	case "forward":
		return runForward(ctx, args[1:])
	case "chats":
		return runChats(ctx, args[1:])
	case "messages":
		return runMessages(ctx, args[1:])
	case "resolve":
		return runResolve(ctx, args[1:])
	case "contacts":
		return runContacts(ctx, args[1:])
	case "contact":
		return runContact(ctx, args[1:])
	case "groups":
		return runGroups(ctx, args[1:])
	case "group":
		return runGroup(ctx, args[1:])
	case "watch":
		return runWatch(ctx, args[1:])
	case "devices":
		return runDevices(ctx, args[1:])
	case "rotate-jwt-secret":
		return runRotateJWT(ctx, args[1:])
	case "wipe":
		return runWipe(ctx, args[1:])
	case "export":
		return runExport(ctx, args[1:])
	case "import":
		return runImport(ctx, args[1:])
	case "install":
		return runInstall(ctx, args[1:])
	default:
		return fmt.Errorf("%w: %s", errInvalidInput, args[0])
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errAuth):
		return 2
	case errors.Is(err, errDaemonUnavailable):
		return 3
	case errors.Is(err, errInvalidInput):
		return 4
	case errors.Is(err, errLocked):
		return 5
	default:
		return 1
	}
}
