package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Version = 1

type Request struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`
	Mode    string `json:"mode"`
	Channel string `json:"channel"`
}

type Response struct {
	OK    bool   `json:"ok"`
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

func writeJSONLine(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
