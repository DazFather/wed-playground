package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var userID string

const oci = "podman"

type Playground struct {
	containerID string
	tempdir     string
	user        string
	edited      chan string
	sync.Mutex
}

func init() {
	usr, err := user.Current()
	if err != nil {
		panic("Cannot retrieve current user:" + err.Error())
	}
	userID = usr.Uid + ":" + usr.Gid
}

func NewPlayground(requestID string) (*Playground, error) {
	play := &Playground{
		containerID: requestID,
		tempdir:     filepath.Join(os.TempDir(), requestID+"_playground"),
		user:        userID,
		edited:      make(chan string, 1),
	}

	if err := os.Mkdir(play.tempdir, 0755); err != nil {
		return nil, err
	}

	if _, err := play.run(true, "tail", "-f", "/dev/null"); err != nil {
		os.RemoveAll(play.tempdir)
		return nil, err
	}

	if _, err := play.exec("sh", "-c", "wed init -nc . && wed build -nc"); err != nil {
		os.RemoveAll(play.tempdir)
		sh(oci, "kill", play.containerID)
		return nil, err
	}

	return play, nil
}

func (p *Playground) Edit(file string, content []byte) ([]byte, error) {
	if err := os.WriteFile(filepath.Join(p.tempdir, file), content, 0600); err != nil {
		return nil, err
	}

	select {
	case p.edited <- file:
	default:
	}

	return p.exec("wed", "build", "-nc")
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
		"run",
		"--rm",
		"--name", p.containerID,
		"-w", "/workspace",
		"-v", p.tempdir + ":/workspace",
		"--network", "none",
		"--userns=keep-id",
		"--user", p.user,
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
