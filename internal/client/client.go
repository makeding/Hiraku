package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"

	"github.com/makeding/hiraku/internal/protocol"
)

func Run(ctx context.Context, addr string, secret string, mode string, channel string, out io.Writer) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
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
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
