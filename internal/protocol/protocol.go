package protocol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Version           = 2
	FrameStdout  byte = 1
	FrameExit    byte = 2
	maxFrameSize      = 1 << 20
)

type Request struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`
	Mode    string `json:"mode,omitempty"`
	Channel string `json:"channel,omitempty"`
	Pipe    string `json:"pipe,omitempty"`
}

type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type PipeExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Request{}, err
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, err
	}
	if req.Version != Version {
		return Request{}, fmt.Errorf("unsupported protocol version: %d", req.Version)
	}

	return req, nil
}

func WriteRequest(w io.Writer, req Request) error {
	req.Version = Version
	return writeJSONLine(w, req)
}

func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}

	var res Response
	if err := json.Unmarshal(line, &res); err != nil {
		return Response{}, err
	}
	if !res.OK && res.Error == "" {
		return Response{}, errors.New("remote rejected request")
	}

	return res, nil
}

func WriteResponse(w io.Writer, res Response) error {
	return writeJSONLine(w, res)
}

func WriteFrame(w io.Writer, frameType byte, payload []byte) error {
	if len(payload) > maxFrameSize {
		return fmt.Errorf("frame is too large: %d bytes", len(payload))
	}
	header := [5]byte{frameType}
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, payload)
}

func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > maxFrameSize {
		return 0, nil, fmt.Errorf("frame is too large: %d bytes", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return header[0], payload, nil
}

func WritePipeExit(w io.Writer, exit PipeExit) error {
	payload, err := json.Marshal(exit)
	if err != nil {
		return err
	}
	return WriteFrame(w, FrameExit, payload)
}

func ParsePipeExit(payload []byte) (PipeExit, error) {
	var exit PipeExit
	if err := json.Unmarshal(payload, &exit); err != nil {
		return PipeExit{}, err
	}
	return exit, nil
}

func writeJSONLine(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return writeAll(w, b)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
