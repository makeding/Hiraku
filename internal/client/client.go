package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/makeding/hiraku/internal/protocol"
)

var ErrUnexpectedStreamEnd = errors.New("remote stream closed unexpectedly")

type RemoteExitError struct {
	Code    int
	Message string
}

func (e *RemoteExitError) Error() string {
	return e.Message
}

func Run(ctx context.Context, addr string, secret string, mode string, channel string, out io.Writer) error {
	conn, done, err := dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer close(done)

	if err := protocol.WriteRequest(conn, protocol.Request{
		Secret:  secret,
		Mode:    mode,
		Channel: channel,
	}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	br := bufio.NewReader(conn)
	res, err := protocol.ReadResponse(br)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if !res.OK {
		return fmt.Errorf("remote rejected request: %s", res.Error)
	}

	_, err = io.Copy(out, br)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return ErrUnexpectedStreamEnd
}

func RunPipe(ctx context.Context, addr string, secret string, pipeName string, in io.Reader, out io.Writer) error {
	conn, done, err := dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer close(done)

	if err := protocol.WriteRequest(conn, protocol.Request{Secret: secret, Pipe: pipeName}); err != nil {
		return contextError(ctx, err)
	}

	br := bufio.NewReader(conn)
	res, err := protocol.ReadResponse(br)
	if err != nil {
		return contextError(ctx, err)
	}
	if !res.OK {
		return fmt.Errorf("remote rejected request: %s", res.Error)
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return errors.New("pipe connection is not TCP")
	}
	uploadErr := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(tcpConn, in)
		if copyErr == nil {
			copyErr = tcpConn.CloseWrite()
		}
		uploadErr <- copyErr
	}()

	for {
		frameType, payload, err := protocol.ReadFrame(br)
		if err != nil {
			select {
			case uploadErr := <-uploadErr:
				if uploadErr != nil {
					return contextError(ctx, uploadErr)
				}
			default:
			}
			return contextError(ctx, err)
		}
		switch frameType {
		case protocol.FrameStdout:
			n, err := out.Write(payload)
			if err != nil {
				return err
			}
			if n != len(payload) {
				return io.ErrShortWrite
			}
		case protocol.FrameExit:
			exit, err := protocol.ParsePipeExit(payload)
			if err != nil {
				return err
			}
			if exit.Code != 0 {
				return &RemoteExitError{Code: exit.Code, Message: exit.Error}
			}
			return nil
		default:
			return fmt.Errorf("unknown pipe frame type: %d", frameType)
		}
	}
}

func dial(ctx context.Context, addr string) (net.Conn, chan struct{}, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, contextError(ctx, err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return conn, done, nil
}

func contextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
