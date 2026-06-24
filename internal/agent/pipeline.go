package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/makeding/hiraku/internal/config"
)

type Manager struct {
	cfg config.Config
}

type Consumer struct {
	pipeline *Pipeline
	ch       chan []byte
	once     sync.Once
}

type Pipeline struct {
	argvs    [][]string
	commands []*exec.Cmd

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) Acquire(modeName string, channel string) (*Consumer, error) {
	if err := config.ValidateModeName(modeName); err != nil {
		return nil, err
	}
	if err := config.ValidateChannel(channel); err != nil {
		return nil, err
	}

	mode, ok := m.cfg.Modes[modeName]
	if !ok {
		return nil, fmt.Errorf("mode is not configured: %s", modeName)
	}

	c := &Consumer{
		ch: make(chan []byte, 64),
	}
	p := newPipeline(config.ExpandPipeline(mode, channel))
	c.pipeline = p
	if err := p.start(c.ch); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Consumer) CopyTo(w io.Writer) error {
	for chunk := range c.ch {
		if _, err := w.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Consumer) Release() {
	c.once.Do(func() {
		c.pipeline.stop()
	})
}

func newPipeline(argvs [][]string) *Pipeline {
	return &Pipeline{
		argvs: argvs,
		done:  make(chan struct{}),
	}
}

func (p *Pipeline) start(output chan<- []byte) error {
	if len(p.argvs) == 0 {
		return errors.New("empty pipeline")
	}

	var previousStdout io.ReadCloser
	for i, argv := range p.argvs {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stderr = os.Stderr

		if previousStdout != nil {
			cmd.Stdin = previousStdout
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			p.stop()
			return err
		}

		if err := cmd.Start(); err != nil {
			p.stop()
			return err
		}

		p.commands = append(p.commands, cmd)
		if i > 0 && previousStdout != nil {
			previousStdout.Close()
		}
		previousStdout = stdout
	}

	go p.copyOutput(previousStdout, output)
	go p.wait()

	return nil
}

func (p *Pipeline) copyOutput(r io.ReadCloser, output chan<- []byte) {
	defer r.Close()
	defer close(output)

	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if !safeSend(output, append([]byte(nil), buf[:n]...)) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func safeSend(ch chan<- []byte, chunk []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ch <- chunk
	return true
}

func (p *Pipeline) wait() {
	defer close(p.done)
	for _, cmd := range p.commands {
		_ = cmd.Wait()
	}
	p.stop()
}

func (p *Pipeline) stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	commands := append([]*exec.Cmd(nil), p.commands...)
	p.mu.Unlock()

	for _, cmd := range commands {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

func (p *Pipeline) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}
