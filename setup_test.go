package agent

import (
	stdjson "encoding/json"
	stdfmt "fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	mcpContext "webtyp.com/context"
	"webtyp.com/json"
	"webtyp.com/mcp"
	"webtyp.com/model"
)

var testMemory MemoryStore
var testServer *httptest.Server

type testToolProvider struct{}

func (p testToolProvider) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "calculator",
			Description: "Calculates sum of two numbers",
			Args:        nil,
			Access:      model.AccessPublic,
			Execute: func(ctx *mcpContext.Context, req mcp.Request) (*mcp.Result, error) {
				var args struct {
					A float64 `json:"a"`
					B float64 `json:"b"`
				}
				stdjson.Unmarshal([]byte(req.Params.Arguments), &args)
				return mcp.Text(stdfmt.Sprintf("%v", args.A+args.B)), nil
			},
		},
	}
}

func TestMain(m *testing.M) {
	var err error
	testMemory, err = NewSQLiteMemory(":memory:")
	if err != nil {
		panic(err)
	}

	srv, err := mcp.NewServer(
		mcp.Config{
			Name:      "test-server",
			Version:   "1.0.0",
			Authorize: mcp.AllowAll,
		},
		[]mcp.ToolProvider{testToolProvider{}},
	)
	if err != nil {
		panic(err)
	}

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx mcpContext.Context
		body, _ := io.ReadAll(r.Body)
		respMsg := srv.HandleMessage(&ctx, body)
		w.Header().Set("Content-Type", "application/json")
		var out []byte
		if enc, ok := respMsg.(model.Encodable); ok {
			json.Encode(enc, &out)
		}
		w.Write(out)
	}))

	code := m.Run()
	testServer.Close()
	os.Exit(code)
}
