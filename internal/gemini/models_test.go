package gemini_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"geminiplexer/internal/config"
	"geminiplexer/internal/gemini"
)

func TestFetchModelsRPC(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("rpcids") != "otAQ7b" {
			t.Errorf("expected rpcids otAQ7b, got %s", r.URL.Query().Get("rpcids"))
		}
		// Returns batchexecute response with model info
		w.Write([]byte(`[[["wrb.fr","otAQ7b","[null,null,null,[[null,null,[[null,\"gemini-auto\",null,null,null,1,12,null,null,null,null,null,\"auto_model_id\"]]]]]"]]]`))
	}))
	defer ts.Close()

	sess := gemini.NewAccountSession(config.AccountConfig{ID: "acc1", Cookies: "__Secure-1PSID=sid; __Secure-1PSIDTS=ts"})
	sess.SetBatchExecEndpointOverride(ts.URL)
	sess.SetToken("fake_token")

	models, err := sess.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("FetchModels failed: %v", err)
	}
	if len(models) == 0 || models[0].ID != "gemini-auto" {
		t.Errorf("unexpected models: %+v", models)
	}
}
