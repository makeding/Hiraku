package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
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

func TestEachAcquireStartsIndependentPipeline(t *testing.T) {
	m := NewManager(testConfig())

	c1, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Release()

	c2, err := m.Acquire("BS", "27")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Release()

	if c1.pipeline == c2.pipeline {
		t.Fatal("expected each request to start its own pipeline")
	}

	chunk1 := waitChunk(t, c1)
	chunk2 := waitChunk(t, c2)
	if !bytes.Contains(chunk1, []byte("27")) || !bytes.Contains(chunk2, []byte("27")) {
		t.Fatalf("unexpected chunks: %q %q", chunk1, chunk2)
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

func waitChunk(t *testing.T, c *Consumer) []byte {
	t.Helper()
	select {
	case chunk := <-c.ch:
		return chunk
	case <-time.After(time.Second):
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
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
