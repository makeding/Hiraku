package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/makeding/hiraku/internal/client"
	"github.com/makeding/hiraku/internal/config"
)

func TestPipeServerRoundTrip(t *testing.T) {
	addr, serve := startPipeTestServer(t, "UPPER", pipeServerHelperCommand("upper"))
	defer serve()

	input := bytes.Repeat([]byte("remote pipe 123\n"), 8192)
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.RunPipe(ctx, addr, "secret", "UPPER", bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if want := bytes.ToUpper(input); !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("pipe output length = %d, want %d", output.Len(), len(want))
	}
}

func TestPipeServerSurvivesFiveSecondNetworkStall(t *testing.T) {
	serverAddr, stopServer := startPipeTestServer(t, "UPPER", pipeServerHelperCommand("upper"))
	defer stopServer()
	proxyAddr, stopProxy := startStallingProxy(t, serverAddr, 32*1024, 5*time.Second)
	defer stopProxy()

	input := bytes.Repeat([]byte("network stall\n"), 16*1024)
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	if err := client.RunPipe(ctx, proxyAddr, "secret", "UPPER", bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 5*time.Second {
		t.Fatalf("pipe completed in %v; test proxy did not inject the 5 second stall", elapsed)
	}
	if want := bytes.ToUpper(input); !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("pipe output length = %d, want %d after network recovery", output.Len(), len(want))
	}
}

func TestPipeServerReturnsProcessExitCode(t *testing.T) {
	addr, serve := startPipeTestServer(t, "FAIL", pipeServerHelperCommand("fail"))
	defer serve()

	var output bytes.Buffer
	err := client.RunPipe(context.Background(), addr, "secret", "FAIL", strings.NewReader("input"), &output)
	var remoteExit *client.RemoteExitError
	if !errors.As(err, &remoteExit) || remoteExit.Code != 7 {
		t.Fatalf("RunPipe() error = %v, want remote exit code 7", err)
	}
	if output.String() != "partial" {
		t.Fatalf("pipe output = %q, want partial output", output.String())
	}
}

func startPipeTestServer(t *testing.T, name string, command []string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{
		Secret:              "secret",
		AllowIPv4CidrRanges: []string{"127.0.0.0/8"},
		Pipes: map[string]config.Pipe{
			name: {Command: [][]string{command}},
		},
	}, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err == nil {
			server.handle(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("pipe test server did not stop")
		}
		server.manager.Shutdown()
	}
}

func startStallingProxy(t *testing.T, backendAddr string, stallAfter int, delay time.Duration) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		front, err := ln.Accept()
		if err != nil {
			return
		}
		defer front.Close()
		back, err := net.Dial("tcp", backendAddr)
		if err != nil {
			return
		}
		defer back.Close()

		var copies sync.WaitGroup
		copies.Add(2)
		go func() {
			defer copies.Done()
			copyWithOneStall(back, front, stallAfter, delay)
			_ = back.(*net.TCPConn).CloseWrite()
		}()
		go func() {
			defer copies.Done()
			_, _ = io.Copy(front, back)
			_ = front.(*net.TCPConn).CloseWrite()
		}()
		copies.Wait()
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("stalling proxy did not stop")
		}
	}
}

func copyWithOneStall(dst io.Writer, src io.Reader, stallAfter int, delay time.Duration) {
	buf := make([]byte, 4*1024)
	total := 0
	stalled := false
	for {
		n, err := src.Read(buf)
		if n > 0 {
			total += n
			if !stalled && total >= stallAfter {
				stalled = true
				time.Sleep(delay)
			}
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func pipeServerHelperCommand(action string) []string {
	return []string{os.Args[0], "-test.run=TestPipeServerHelperProcess", "--", action}
}

func TestPipeServerHelperProcess(t *testing.T) {
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
	switch os.Args[sep+1] {
	case "upper":
		data, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.Write(bytes.ToUpper(data))
	case "fail":
		_, _ = io.WriteString(os.Stdout, "partial")
		os.Exit(7)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
