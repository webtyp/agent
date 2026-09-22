package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/mcp"
	"webtyp.com/time"
)

// HTTPMCPClient adapts mcp.Client's callback-based Call to mcpCaller's blocking shape,
// with an explicit millisecond timeout (webtyp.com/time.AfterFunc) instead of a cancelable
// context — see docs/PLAN.md Cambio 1/2 for why context can't carry this here.
type HTTPMCPClient struct {
	client    *mcp.Client
	timeoutMS int
}

func NewHTTPMCPClient(url string, timeoutMS int) *HTTPMCPClient {
	return &HTTPMCPClient{client: mcp.NewClient(url, ""), timeoutMS: timeoutMS}
}

func (c *HTTPMCPClient) Call(ctx *context.Context, method string, params any) ([]byte, error) {
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	c.client.Call(ctx, method, params, func(body []byte, err error) {
		done <- result{body, err}
	})

	timedOut := make(chan struct{})
	timer := time.AfterFunc(c.timeoutMS, func() { close(timedOut) })

	select {
	case r := <-done:
		timer.Stop()
		return r.body, r.err
	case <-timedOut:
		return nil, fmt.Err("mcp: call to ", method, " timed out")
	}
}
