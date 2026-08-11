package protocol

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := []byte{0, 1, 2, 3, 255}
	if err := WriteFrame(&buf, FrameStdout, want); err != nil {
		t.Fatal(err)
	}
	frameType, got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if frameType != FrameStdout || !bytes.Equal(got, want) {
		t.Fatalf("unexpected frame: type=%d payload=%v", frameType, got)
	}
}

func TestPipeExitRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := PipeExit{Code: 7, Error: "ffmpeg failed"}
	if err := WritePipeExit(&buf, want); err != nil {
		t.Fatal(err)
	}
	frameType, payload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if frameType != FrameExit {
		t.Fatalf("unexpected frame type: %d", frameType)
	}
	got, err := ParsePipeExit(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PipeExit = %#v, want %#v", got, want)
	}
}
