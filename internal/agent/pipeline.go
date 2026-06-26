package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/makeding/hiraku/internal/config"
)

const defaultStopTimeout = 5 * time.Second

type Manager struct {
	cfg config.Config

	mu          sync.Mutex
	shutdown    bool
	pipelines   map[*Pipeline]struct{}
	activeModes map[string]*Pipeline
}

type Consumer struct {
	pipeline *Pipeline
	ch       chan []byte
	once     sync.Once
}

type Pipeline struct {
	argvs    [][]string
	commands []*exec.Cmd

	mu        sync.Mutex
	stopped   bool
	releasing bool
	done      chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	onDone    func()
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{
		cfg:         cfg,
		pipelines:   make(map[*Pipeline]struct{}),
		activeModes: make(map[string]*Pipeline),
	}
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
	p.onDone = func() {
		m.unregister(modeName, p)
	}

	for {
		preempt, err := m.register(modeName, p)
		if err != nil {
			return nil, err
		}
		if preempt == nil {
			break
		}
		preempt.stopAndWait(defaultStopTimeout)
	}
	c.pipeline = p
	if err := p.start(c.ch); err != nil {
		m.unregister(modeName, p)
		return nil, err
	}
	return c, nil
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return
	}
	m.shutdown = true
	pipelines := make([]*Pipeline, 0, len(m.pipelines))
	for p := range m.pipelines {
		pipelines = append(pipelines, p)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, p := range pipelines {
		wg.Add(1)
		go func(p *Pipeline) {
			defer wg.Done()
			p.stopAndWait(defaultStopTimeout)
		}(p)
	}
	wg.Wait()
}

func (m *Manager) register(modeName string, p *Pipeline) (*Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return nil, errors.New("agent is shutting down")
	}
	if existing, ok := m.activeModes[modeName]; ok {
		if existing.isReleasingOrStopped() {
			return existing, nil
		}
		return nil, fmt.Errorf("mode is busy: %s", modeName)
	}
	m.pipelines[p] = struct{}{}
	m.activeModes[modeName] = p
	return nil, nil
}

func (m *Manager) unregister(modeName string, p *Pipeline) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pipelines, p)
	if m.activeModes[modeName] == p {
		delete(m.activeModes, modeName)
	}
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
	c.ReleaseAfter(0)
}

func (c *Consumer) ReleaseAfter(delay time.Duration) {
	c.once.Do(func() {
		c.pipeline.markReleasing()
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-c.pipeline.done:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}
		c.pipeline.stop()
	})
}

func newPipeline(argvs [][]string) *Pipeline {
	return &Pipeline{
		argvs:  argvs,
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
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
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if previousStdout != nil {
			cmd.Stdin = previousStdout
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			p.cleanupStartedCommands()
			return err
		}

		if err := cmd.Start(); err != nil {
			p.cleanupStartedCommands()
			return err
		}

		if err := p.addStartedCommand(cmd); err != nil {
			p.cleanupStartedCommands()
			return err
		}
		if i > 0 && previousStdout != nil {
			previousStdout.Close()
		}
		previousStdout = stdout
	}

	go p.copyOutput(previousStdout, output)
	go p.wait()

	return nil
}

func (p *Pipeline) addStartedCommand(cmd *exec.Cmd) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		if cmd.Process != nil {
			signalCommandGroup(cmd, syscall.SIGKILL)
			_ = cmd.Wait()
		}
		return errors.New("pipeline stopped")
	}
	p.commands = append(p.commands, cmd)
	return nil
}

func (p *Pipeline) copyOutput(r io.ReadCloser, output chan<- []byte) {
	defer r.Close()
	defer close(output)

	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if !safeSend(output, append([]byte(nil), buf[:n]...), p.stopCh) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func safeSend(ch chan<- []byte, chunk []byte, stopCh <-chan struct{}) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case ch <- chunk:
		return true
	case <-stopCh:
		return false
	}
}

func (p *Pipeline) wait() {
	defer func() {
		p.closeStopCh()
		if p.onDone != nil {
			p.onDone()
		}
		close(p.done)
	}()
	for _, cmd := range p.commands {
		_ = cmd.Wait()
	}
}

func (p *Pipeline) stop() {
	p.stopAndWait(defaultStopTimeout)
}

func (p *Pipeline) stopAndWait(timeout time.Duration) {
	p.requestStop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-p.done:
		return
	case <-timer.C:
		p.killCommands()
		<-p.done
	}
}

func (p *Pipeline) requestStop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	commands := append([]*exec.Cmd(nil), p.commands...)
	p.mu.Unlock()

	p.closeStopCh()
	for _, cmd := range commands {
		signalCommandGroup(cmd, syscall.SIGTERM)
	}
}

func (p *Pipeline) markReleasing() {
	p.mu.Lock()
	p.releasing = true
	p.mu.Unlock()
}

func (p *Pipeline) closeStopCh() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
}

func (p *Pipeline) killCommands() {
	p.mu.Lock()
	commands := append([]*exec.Cmd(nil), p.commands...)
	p.mu.Unlock()

	for _, cmd := range commands {
		signalCommandGroup(cmd, syscall.SIGKILL)
	}
}

func (p *Pipeline) cleanupStartedCommands() {
	p.requestStop()
	p.killCommands()
	for _, cmd := range p.commands {
		_ = cmd.Wait()
	}
	p.closeStopCh()
	close(p.done)
}

func signalCommandGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		if err := syscall.Kill(-pid, sig); err == nil || errors.Is(err, syscall.ESRCH) {
			return
		}
	}
	_ = cmd.Process.Signal(sig)
}

func (p *Pipeline) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func (p *Pipeline) isReleasingOrStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.releasing || p.stopped
}
