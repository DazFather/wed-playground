package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DazFather/Wednesday/pkg/shared"
)

var actives shared.Smap[string, *Playground]

const oci = "podman"

type Playground struct {
	containerID string
	tempdir     string
	user        string
	edited      chan string
	sync.Mutex
}

func NewPlayground(requestID string) (*Playground, error) {
	dir := filepath.Join(os.TempDir(), requestID)
	if err := os.Mkdir(dir, 0755); err != nil {
		return nil, err
	}

	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("Cannot retrieve current user: %w", err)
	}

	p := &Playground{
		containerID: requestID,
		tempdir:     dir,
		user:        usr.Uid + ":" + usr.Gid,
		edited:      make(chan string),
	}
	actives.Store(requestID, p)

	return p, nil
}

func ConnectPlayground(requestID string) (*Playground, error) {
	p, found := actives.Load(requestID)
	if !found || p == nil {
		return nil, fmt.Errorf("Cannot connect to %q playground", requestID)
	}
	return p, nil
}

func (p *Playground) Edit(file string, content []byte) ([]byte, error) {
	if err := os.WriteFile(filepath.Join(p.tempdir, file), content, 0600); err != nil {
		return nil, err
	}

	go func() { p.edited <- file }()

	return p.exec("wed", "build", "-nc")
}

func (p *Playground) Init() (out []byte, err error) {
	if out, err = p.run(true, "tail", "-f", "/dev/null"); err == nil {
		out, err = p.exec("sh", "-c", "wed init -nc . && wed build -nc")
	}
	return
}

func (p *Playground) Wait(ctx context.Context) bool {
	max, cancel := context.WithTimeout(ctx, MAX_PLAYTIME)
	defer cancel()

	inactive := time.NewTimer(MAX_INACTIVE_TIME)
	defer inactive.Stop()

	dead := p.liveness(max, LIVENESS_PROBE)

	for {
		select {
		case _, ok := <-p.edited:
			if !ok {
				return false
			}
			inactive.Reset(MAX_INACTIVE_TIME)
		case <-dead:
			return false
		case <-inactive.C:
			return false
		case <-max.Done():
			fmt.Println("Gracefully closing server")
			return true
		}
	}
	return false
}

func (p *Playground) Close() error {
	defer actives.Delete(p.containerID)
	defer os.RemoveAll(p.tempdir)
	_, err := sh(oci, "kill", p.containerID)
	return err
}

func (p *Playground) Alive() bool {
	out, err := sh(oci, "container", "inspect", "--format", "{{.State.Running}}", p.containerID)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func (p *Playground) liveness(ctx context.Context, each time.Duration) chan struct{} {
	notify := make(chan struct{})

	go func() {
		ping := time.NewTicker(each)
		defer ping.Stop()
		defer close(notify)

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
