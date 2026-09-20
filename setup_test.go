package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"webtyp.com/context"
	"webtyp.com/fmt"
	webtypjson "webtyp.com/json"
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
			Description: "Calculates sum",
			Access:      model.AccessPublic,
			Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
				var args struct {
					A float64 `json:"a"`
					B float64 `json:"b"`
				}
				if err := json.Unmarshal([]byte(req.Params.Arguments), &args); err != nil {
					return nil, err
				}
				return mcp.Text(fmt.Sprintf("%d", int(args.A)+int(args.B))), nil
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
		mcp.Config{Name: "test", Version: "1.0.0", Authorize: mcp.AllowAll},
		[]mcp.ToolProvider{testToolProvider{}},
	)
	if err != nil {
		panic(err)
	}

	testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		body, _ := io.ReadAll(r.Body)
		resp := srv.HandleMessage(&ctx, body)
		w.Header().Set("Content-Type", "application/json")
		var out []byte
		if enc, ok := resp.(model.Encodable); ok {
			webtypjson.Encode(enc, &out)
		}
		w.Write(out)
	}))

	code := m.Run()

	testServer.Close()
	os.Exit(code)
}
