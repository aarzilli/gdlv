package rpc2

import (
	"fmt"
	"io"
	"time"
)

type LogClient struct {
	client io.ReadWriteCloser
	logw   io.Writer
}

func (c *LogClient) Read(buf []byte) (int, error) {
	n, err := c.client.Read(buf)
	if err == nil {
		fmt.Fprintf(c.logw, "<- %s %d %s\n", time.Now().Format(time.RFC3339), n, buf[:n])
	}
	return n, err
}

func (c *LogClient) Write(buf []byte) (int, error) {
	n, err := c.client.Write(buf)
	if err == nil {
		fmt.Fprintf(c.logw, "-> %s %d %s\n", time.Now().Format(time.RFC3339), n, buf[:n])
	}
	return n, err
}

func (c *LogClient) Close() error {
	return c.client.Close()
}
