package client

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"github.com/makeding/hiraku/internal/protocol"
)

func Run(addr string, secret string, mode string, channel string, out io.Writer) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := protocol.WriteRequest(conn, protocol.Request{
		Secret:  secret,
		Mode:    mode,
		Channel: channel,
	}); err != nil {
		return err
	}

	br := bufio.NewReader(conn)
	res, err := protocol.ReadResponse(br)
	if err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("remote rejected request: %s", res.Error)
	}

	_, err = io.Copy(out, br)
	return err
}
