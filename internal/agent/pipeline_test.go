package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/makeding/hiraku/internal/config"
)

func TestAcquireRejectsUnknownMode(t *testing.T) {
	m := NewManager(testConfig())
	if _, err := m.Acquire("UNKNOWN", "27"); err == nil {
		t.Fatal("expected unknown mode to be rejected")
	}
}

func TestEachModeStartsIndependentPipeline(t *testing.T) {
	m := NewManager(testConfig())

	c1, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Release()

	c2, err := m.Acquire("S3", "27")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Release()

	if c1.pipeline == c2.pipeline {
		t.Fatal("expected each mode to start its own pipeline")
	}

	chunk1 := waitChunk(t, c1)
	chunk2 := waitChunk(t, c2)
	if !bytes.Contains(chunk1, []byte("27")) || !bytes.Contains(chunk2, []byte("27")) {
		t.Fatalf("unexpected chunks: %q %q", chunk1, chunk2)
	}
}

func TestAcquireRejectsBusyMode(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()

	if _, err := m.Acquire("BS", "28"); err == nil {
		t.Fatal("expected busy mode to be rejected")
	}
}

func TestReleaseStopsPipeline(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	p := c.pipeline
	c.Release()

	waitFor(t, time.Second, p.isStopped)
}

func TestReleaseAfterDelaysPipelineStop(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	p := c.pipeline

	released := make(chan struct{})
	go func() {
		c.ReleaseAfter(100 * time.Millisecond)
		close(released)
	}()

	select {
	case <-released:
		t.Fatal("expected delayed release to keep running briefly")
	case <-time.After(20 * time.Millisecond):
	}
	if p.isStopped() {
		t.Fatal("expected pipeline to stay running before release delay expires")
	}

	waitFor(t, time.Second, p.isStopped)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("release did not return after delay")
	}
}

func TestAcquirePreemptsDelayedRelease(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	oldPipeline := c.pipeline

	released := make(chan struct{})
	go func() {
		c.ReleaseAfter(500 * time.Millisecond)
		close(released)
	}()
	waitFor(t, time.Second, oldPipeline.isReleasingOrStopped)

	start := time.Now()
	next, err := m.Acquire("BS", "28")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if time.Since(start) >= 500*time.Millisecond {
		t.Fatal("expected acquire to preempt delayed release instead of waiting for delay")
	}
	if next.pipeline == oldPipeline {
		t.Fatal("expected a new pipeline after preempting delayed release")
	}

	waitFor(t, time.Second, oldPipeline.isStopped)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("delayed release did not return after preemption")
	}
}

func TestManagerShutdownStopsActivePipelinesAndRejectsNewAcquire(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	p := c.pipeline

	m.Shutdown()

	if !p.isStopped() {
		t.Fatal("expected active pipeline to stop during shutdown")
	}
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("expected shutdown to wait for pipeline exit")
	}

	if _, err := m.Acquire("BS", "27"); err == nil {
		t.Fatal("expected acquire after shutdown to fail")
	}
}

func TestModeSelectsCommandTemplate(t *testing.T) {
	m := NewManager(testConfig())

	c, err := m.Acquire("S3-M2TS", "BS17_1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()

	chunk := waitChunk(t, c)
	if !bytes.Contains(chunk, []byte("hantto4k:recdvb4k:BS17_1")) {
		t.Fatalf("unexpected pipeline output: %q", chunk)
	}
}

func TestAcquirePipeStreamsInputThroughCommand(t *testing.T) {
	m := NewManager(testConfig())
	c, err := m.AcquirePipe("UPPER", strings.NewReader("remote pipe\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()

	var out bytes.Buffer
	if err := c.CopyTo(&out); err != nil {
		t.Fatal(err)
	}
	code, err := c.pipeline.result()
	if err != nil || code != 0 {
		t.Fatalf("pipe result = (%d, %v)", code, err)
	}
	if got, want := out.String(), "REMOTE PIPE\n"; got != want {
		t.Fatalf("pipe output = %q, want %q", got, want)
	}
}

func TestAcquirePipeReportsRemoteExit(t *testing.T) {
	m := NewManager(testConfig())
	c, err := m.AcquirePipe("FAIL", strings.NewReader("input"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()

	if err := c.CopyTo(io.Discard); err != nil {
		t.Fatal(err)
	}
	code, err := c.pipeline.result()
	if err == nil || code != 7 {
		t.Fatalf("pipe result = (%d, %v), want exit code 7", code, err)
	}
}

func TestAcquirePipeEnforcesDefaultConcurrencyLimit(t *testing.T) {
	m := NewManager(testConfig())
	consumers := make([]*Consumer, 0, config.DefaultMaxConcurrentPipes)
	writers := make([]*io.PipeWriter, 0, config.DefaultMaxConcurrentPipes)
	for i := 0; i < config.DefaultMaxConcurrentPipes; i++ {
		reader, writer := io.Pipe()
		consumer, err := m.AcquirePipe("HOLD", reader)
		if err != nil {
			t.Fatal(err)
		}
		consumers = append(consumers, consumer)
		writers = append(writers, writer)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	if _, err := m.AcquirePipe("HOLD", reader); err == nil {
		t.Fatal("expected fifth concurrent pipe to be rejected")
	}
	for i := range consumers {
		_ = writers[i].Close()
		consumers[i].Release()
	}
}

func waitChunk(t *testing.T, c *Consumer) []byte {
	t.Helper()
	select {
	case chunk := <-c.ch:
		return chunk
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for chunk")
		return nil
	}
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func testConfig() config.Config {
	return config.Config{
		Listen: "127.0.0.1:0",
		Secret: "secret",
		Modes: map[string]config.Mode{
			"BS": {
				Record: [][]string{helperCommand("stream", "<channel>")},
			},
			"S3": {
				Record: [][]string{
					helperCommand("pipe", "recdvb4k:<channel>"),
				},
			},
			"S3-M2TS": {
				Record: [][]string{
					helperCommand("pipe", "recdvb4k:<channel>"),
					helperCommand("pipe", "hantto4k"),
				},
			},
		},
		Pipes: map[string]config.Pipe{
			"UPPER": {Command: [][]string{helperCommand("upper")}},
			"FAIL":  {Command: [][]string{helperCommand("fail")}},
			"HOLD":  {Command: [][]string{helperCommand("hold")}},
		},
	}
}

func helperCommand(args ...string) []string {
	cmd := []string{os.Args[0], "-test.run=TestHelperProcess", "--"}
	return append(cmd, args...)
}

func TestHelperProcess(t *testing.T) {
	sep := -1
	for i := range os.Args {
		if os.Args[i] == "--" {
			sep = i
			break
		}
	}
	if sep == -1 || sep+1 >= len(os.Args) {
		return
	}
	args := os.Args[sep+1:]

	switch args[0] {
	case "stream":
		for i := 0; i < 100; i++ {
			fmt.Printf("stream:%s:%03d\n", args[1], i)
			time.Sleep(5 * time.Millisecond)
		}
	case "pipe":
		if len(args) == 2 && bytes.Contains([]byte(args[1]), []byte("<channel>")) {
			os.Exit(3)
		}
		data, _ := io.ReadAll(os.Stdin)
		if len(data) == 0 {
			fmt.Print(args[1])
			break
		}
		fmt.Printf("%s:%s", args[1], data)
	case "upper":
		data, _ := io.ReadAll(os.Stdin)
		fmt.Print(strings.ToUpper(string(data)))
	case "fail":
		fmt.Print("partial")
		os.Exit(7)
	case "hold":
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
