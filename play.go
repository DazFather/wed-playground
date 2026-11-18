package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	PLAYGROUND_RAM  = "256m"
	PLAYGROUND_SWAP = "256m"
	PLAYGROUND_USER = "10001:10001"
	PLAYGROUND_PIDS = "64"
)

type Playground struct {
	containerID string
	tempdir     string
	edited      chan string
	sync.Mutex
}

func NewPlayground(requestID string) (*Playground, error) {
	play := &Playground{
		containerID: requestID,
		tempdir:     filepath.Join(os.TempDir(), requestID+"_playground"),
		edited:      make(chan string, 1),
	}

	if err := os.Mkdir(play.tempdir, 0777); err != nil {
		return nil, err
	}
	if err := os.Chmod(play.tempdir, 0777); err != nil {
		os.RemoveAll(play.tempdir)
		return nil, err
	}

	if _, err := play.run(true, "tail", "-f", "/dev/null"); err != nil {
		os.RemoveAll(play.tempdir)
		return nil, err
	}

	if _, err := play.exec("sh", "-c", "wed init -nc -s /etc/wed-settings.json && wed build -nc -s /etc/wed-settings.json && (chmod -R a+rwX ./* 2>/dev/null || true)"); err != nil {
		os.RemoveAll(play.tempdir)
		sh(oci, "kill", play.containerID)
		return nil, err
	}

	return play, nil
}

func (p *Playground) Edit(file string, content []byte) ([]byte, error) {
	dir := filepath.Join(p.tempdir, "input")
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0777); err != nil && !errors.Is(err, os.ErrPermission) {
		return nil, err
	}

	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, content, 0666); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0666); err != nil && !errors.Is(err, os.ErrPermission) {
		return nil, err
	}

	select {
	case p.edited <- file:
	default:
	}

	return p.exec("sh", "-c", "wed build -nc -s /etc/wed-settings.json && (chmod -R a+rwX ./* 2>/dev/null || true)")
}

func (p *Playground) Delete(file string) ([]byte, error) {
	if err := os.Remove(filepath.Join(p.tempdir, "input", file)); err != nil {
		return nil, err
	}

	select {
	case p.edited <- file:
	default:
	}

	return p.exec("sh", "-c", "wed build -nc -s /etc/wed-settings.json && (chmod -R a+rwX ./* 2>/dev/null || true)")
}

func (p *Playground) Close() error {
	defer close(p.edited)

	err := os.RemoveAll(p.tempdir)
	if _, e := sh(oci, "kill", p.containerID); e != nil {
		err = errors.Join(err, e)
	}
	return err
}

func (p *Playground) Alive() bool {
	out, err := sh(oci, "container", "inspect", "--format", "{{.State.Running}}", p.containerID)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func (p *Playground) liveness(ctx context.Context, each time.Duration) <-chan struct{} {
	notify := make(chan struct{}, 1)

	go func() {
		defer close(notify)

		ping := time.NewTicker(each)
		defer ping.Stop()

		for {
			select {
			case <-ping.C:
				if !p.Alive() {
					notify <- struct{}{}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return notify
}

func (p *Playground) run(detached bool, args ...string) ([]byte, error) {
	p.Lock()
	defer p.Unlock()

	cmd := []string{
		"run", "--rm",
		"--name", p.containerID,
		"--network", "none",
		"--read-only",
		"--user", PLAYGROUND_USER,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", PLAYGROUND_PIDS,
		"--memory", PLAYGROUND_RAM,
		"--memory-swap", PLAYGROUND_SWAP,
		"--cpus", "1",
		"--mount", "type=bind,src=" + p.tempdir + ",dst=/workspace",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m",
	}
	if detached {
		cmd = append(cmd, "-d")
	}
	cmd = append(cmd, "wedimg")

	return sh(oci, append(cmd, args...)...)
}

func (p *Playground) exec(args ...string) ([]byte, error) {
	p.Lock()
	defer p.Unlock()

	return sh(oci, append([]string{
		"exec", p.containerID,
	}, args...)...)
}

func sh(command string, args ...string) ([]byte, error) {
	cmd := exec.Command(command, args...)

	if err := cmd.Err; err != nil {
		return nil, fmt.Errorf("cannot prepare command: %w", err)
	}

	return cmd.CombinedOutput()
}
