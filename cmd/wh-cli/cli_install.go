package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func runInstall(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", defaultInstallDir(), "installation directory")
	noPath := fs.Bool("no-path", false, "do not update the user PATH")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errInvalidInput, err)
	}

	source, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve executable symlink: %w", err)
	}
	if err := os.MkdirAll(*dir, 0o750); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}

	target := filepath.Join(*dir, installBinaryName())
	if err := copyExecutable(source, target); err != nil {
		return err
	}

	pathUpdated := false
	if !*noPath {
		updated, err := ensureUserPath(*dir)
		if err != nil {
			return err
		}
		pathUpdated = updated
	}

	_, _ = fmt.Fprintf(os.Stdout, "Installed %s\n", target)
	if pathUpdated {
		_, _ = fmt.Fprintln(os.Stdout, "Updated the user PATH. Open a new terminal and run: wh-cli status")
	} else if directoryInPath(*dir) {
		_, _ = fmt.Fprintln(os.Stdout, "The install directory is already in PATH. Run: wh-cli status")
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "Add this directory to PATH to run wh-cli globally: %s\n", *dir)
	}
	return nil
}

func defaultInstallDir() string {
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "Programs", "wh-cli")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "bin")
	}
	return "."
}

func installBinaryName() string {
	if runtime.GOOS == "windows" {
		return "wh-cli.exe"
	}
	return "wh-cli"
}

func copyExecutable(source string, target string) error {
	if samePath(source, target) {
		return nil
	}
	// #nosec G304 -- source is the current executable path resolved from os.Executable.
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open current executable: %w", err)
	}
	defer func() { _ = in.Close() }()

	tmp := target + ".tmp"
	// #nosec G304 -- target is the requested install path for this wh-cli binary.
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create target executable: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy executable: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close target executable: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(target)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace target executable: %w", err)
	}
	if runtime.GOOS != "windows" {
		// #nosec G302 -- installed CLI binaries must be executable by the user.
		if err := os.Chmod(target, 0o755); err != nil {
			return fmt.Errorf("mark target executable: %w", err)
		}
	}
	return nil
}

func samePath(left string, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil {
		left = leftAbs
		right = rightAbs
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func ensureUserPath(dir string) (bool, error) {
	if directoryInPath(dir) {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		return false, nil
	}

	current, err := windowsUserPath()
	if err != nil {
		return false, err
	}
	next := dir
	if current != "" {
		next = current + ";" + dir
	}
	// #nosec G204 -- command is fixed; only registry value data is user PATH content.
	cmd := exec.Command("reg", "add", `HKCU\Environment`, "/v", "Path", "/t", "REG_EXPAND_SZ", "/d", next, "/f")
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("update user PATH: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

func directoryInPath(dir string) bool {
	dir = filepath.Clean(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if samePath(entry, dir) {
			return true
		}
	}
	return false
}

func windowsUserPath() (string, error) {
	cmd := exec.Command("reg", "query", `HKCU\Environment`, "/v", "Path")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && strings.EqualFold(fields[0], "Path") {
			return strings.Join(fields[2:], " "), nil
		}
	}
	return "", nil
}
