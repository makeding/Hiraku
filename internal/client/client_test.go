package client

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/makeding/hiraku/internal/protocol"
)

func TestRunCancellationClosesConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverReady := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)

		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		br := bufio.NewReader(conn)
		if _, err := protocol.ReadRequest(br); err != nil {
			t.Errorf("ReadRequest() error = %v", err)
			return
		}
		if err := protocol.WriteResponse(conn, protocol.Response{OK: true}); err != nil {
			t.Errorf("WriteResponse() error = %v", err)
			return
		}

		close(serverReady)
		_, _ = io.Copy(io.Discard, br)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- Run(ctx, ln.Addr().String(), "secret", "BS", "BS17_2", io.Discard)
	}()

	select {
	case <-serverReady:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for server response")
	}

	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server did not observe client disconnect")
	}
}
