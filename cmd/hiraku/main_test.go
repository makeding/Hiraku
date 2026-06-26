package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestCancelOnStdinDataCancelsOnData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnStdinData(strings.NewReader("x"), cancel)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected stdin data to cancel context")
	}
}

func TestCancelOnStdinDataCancelsOnEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancelOnStdinData(bytes.NewReader(nil), cancel)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected stdin EOF to cancel context")
	}
}
