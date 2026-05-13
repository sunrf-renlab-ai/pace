package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"
)

type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

func Dial() (*Client, error) {
	p, err := socketPath()
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("unix", p, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c, r: bufio.NewReader(c)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Call(method string, params map[string]any) (Response, error) {
	req := Request{Method: method, Params: params}
	b, _ := json.Marshal(req)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, errors.New("bad response: " + string(line))
	}
	return resp, nil
}
