package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
	"github.com/10kkyvl/studioforge/internal/platform"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/catalog"
	"github.com/10kkyvl/studioforge/internal/providers/openrouter/credential"
)

const testModelsResponse = `{"data":[
	{"id":"poolside/laguna-m.1:free","name":"Laguna","context_length":262144,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools","structured_outputs"]},
	{"id":"vendor/paid-vision","name":"Paid Vision","context_length":128000,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"},"supported_parameters":["tools"]},
	{"id":"vendor/chat-only","name":"Chat Only","context_length":8000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0.0001","completion":"0.0002"},"supported_parameters":[]}
]}`

func withOpenRouter(t *testing.T, a *testAPI, keyTestServer *httptest.Server, modelsServer *httptest.Server) {
	t.Helper()
	creds := credential.NewManager(credential.Config{
		Service: "StudioForge-Test",
		Account: "openrouter",
		Secure:  platform.NewMemorySecretStore(),
		BaseURL: keyTestServer.URL,
		GetState: func(ctx context.Context) (string, error) {
			v, _, _ := a.store.Setting(ctx, "openrouter_key_state")
			return v, nil
		},
		SetState: func(ctx context.Context, s string) error {
			return a.store.SetSetting(ctx, "openrouter_key_state", s)
		},
	})
	cat := catalog.NewService(catalog.Config{HTTPClient: modelsServer.Client(), BaseURL: modelsServer.URL, Cache: a.store, TTL: time.Hour})
	a.server.orCreds = creds
	a.server.orCatalog = cat
}

func doRequest(t *testing.T, a *testAPI, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, "http://127.0.0.1:1234"+path, reader)
	request.Header.Set("Origin", "http://127.0.0.1:1234")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	return recorder
}

func TestOpenRouterUnconfiguredReturns503(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	for _, path := range []string{"/api/v1/openrouter/status", "/api/v1/openrouter/models", "/api/v1/openrouter/capabilities?model=x"} {
		recorder := doRequest(t, a, cookie, "GET", path, "")
		if recorder.Code != 503 {
			t.Fatalf("GET %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestOpenRouterKeyLifecycleNeverLeaksKey(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	const secretKey = "sk-or-do-not-leak-me-12345"

	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), secretKey) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testModelsResponse))
	}))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	statusRecorder := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/status", "")
	assertNoKeyLeak(t, statusRecorder, secretKey)
	var status map[string]any
	_ = json.Unmarshal(statusRecorder.Body.Bytes(), &status)
	if status["state"] != string(credential.StateNotConfigured) {
		t.Fatalf("initial status=%v", status)
	}

	blank := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key", `{"key":""}`)
	if blank.Code != 400 {
		t.Fatalf("blank key status=%d body=%s", blank.Code, blank.Body.String())
	}

	saveRecorder := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key", `{"key":"`+secretKey+`"}`)
	assertNoKeyLeak(t, saveRecorder, secretKey)
	if saveRecorder.Code != 200 {
		t.Fatalf("save status=%d body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}
	var saved map[string]any
	_ = json.Unmarshal(saveRecorder.Body.Bytes(), &saved)
	if saved["state"] != string(credential.StateUnverified) {
		t.Fatalf("save status=%v", saved)
	}

	testRecorder := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key/test", "")
	assertNoKeyLeak(t, testRecorder, secretKey)
	if testRecorder.Code != 200 {
		t.Fatalf("test status=%d body=%s", testRecorder.Code, testRecorder.Body.String())
	}
	var tested map[string]any
	_ = json.Unmarshal(testRecorder.Body.Bytes(), &tested)
	if tested["state"] != string(credential.StateConfigured) || tested["ok"] != true {
		t.Fatalf("test result=%v", tested)
	}

	deleteRecorder := doRequest(t, a, cookie, "DELETE", "/api/v1/openrouter/key", "")
	assertNoKeyLeak(t, deleteRecorder, secretKey)
	if deleteRecorder.Code != 200 {
		t.Fatalf("delete status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	afterDelete := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/status", "")
	var afterStatus map[string]any
	_ = json.Unmarshal(afterDelete.Body.Bytes(), &afterStatus)
	if afterStatus["state"] != string(credential.StateNotConfigured) {
		t.Fatalf("status after delete=%v", afterStatus)
	}
}

func TestOpenRouterKeyTestUnexpectedUpstreamReturns502(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testModelsResponse))
	}))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key", `{"key":"sk-or-whatever"}`)
	recorder := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key/test", "")
	if recorder.Code != 502 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "500") {
		t.Fatalf("response exposed upstream status: %s", recorder.Body.String())
	}
}

func TestOpenRouterSaveKeyNormalizesWhitespace(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	var auth string
	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testModelsResponse)) }))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	recorder := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key", `{"key":"  sk-or-normalized\n "}`)
	if recorder.Code != 200 {
		t.Fatalf("save status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key/test", "")
	if recorder.Code != 200 || auth != "Bearer sk-or-normalized" {
		t.Fatalf("test status=%d auth=%q body=%s", recorder.Code, auth, recorder.Body.String())
	}
}

func TestOpenRouterKeyTestMissingReturns409(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testModelsResponse)) }))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	recorder := doRequest(t, a, cookie, "POST", "/api/v1/openrouter/key/test", "")
	if recorder.Code != 409 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestOpenRouterModelsFiltersAndMarksCuratedAvailability(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testModelsResponse))
	}))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	recorder := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/models", "")
	if recorder.Code != 200 {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Source     string           `json:"source"`
		Models     []map[string]any `json:"models"`
		Curated    []map[string]any `json:"curated"`
		Categories []string         `json:"categories"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Source != "live" {
		t.Fatalf("source=%q", body.Source)
	}
	if len(body.Models) != 3 {
		t.Fatalf("expected all 3 text models, got %d: %+v", len(body.Models), body.Models)
	}
	foundNonTool := false
	for _, m := range body.Models {
		if m["id"] == "vendor/chat-only" {
			foundNonTool = true
			if m["tools"] != false {
				t.Fatalf("non-tool model reported tool support: %+v", m)
			}
		}
	}
	if !foundNonTool {
		t.Fatal("known non-tool model was missing from capability display")
	}
	if len(body.Categories) == 0 {
		t.Fatal("expected non-empty categories")
	}
	available := map[string]bool{}
	for _, c := range body.Curated {
		available[c["id"].(string)] = c["available"].(bool)
	}
	if !available["poolside/laguna-m.1:free"] {
		t.Fatalf("expected poolside/laguna-m.1:free to be available, curated=%+v", body.Curated)
	}
	if !available["openrouter/free"] {
		t.Fatal("expected openrouter/free to always be available")
	}
	if available["cohere/north-mini-code:free"] {
		t.Fatal("expected a curated model absent from the catalog to be unavailable")
	}
}

func TestOpenRouterCapabilities(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer keyTestServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testModelsResponse))
	}))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyTestServer, modelsServer)

	known := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/capabilities?model=vendor/paid-vision", "")
	var knownBody map[string]any
	_ = json.Unmarshal(known.Body.Bytes(), &knownBody)
	if knownBody["known"] != true || knownBody["vision"] != true || knownBody["tools"] != true || knownBody["free"] != false {
		t.Fatalf("known capabilities=%v", knownBody)
	}

	free := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/capabilities?model=openrouter/free", "")
	var freeBody map[string]any
	_ = json.Unmarshal(free.Body.Bytes(), &freeBody)
	if freeBody["known"] != false || freeBody["tools"] != false || freeBody["vision"] != false || freeBody["free"] != true || freeBody["verified"] != false {
		t.Fatalf("openrouter/free capabilities=%v", freeBody)
	}

	unknown := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/capabilities?model=vendor/does-not-exist", "")
	var unknownBody map[string]any
	_ = json.Unmarshal(unknown.Body.Bytes(), &unknownBody)
	if unknownBody["known"] != false {
		t.Fatalf("unknown capabilities=%v", unknownBody)
	}
}

func TestOpenRouterCapabilitiesMarksCachedModelUnverified(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	if err := a.store.SetModelCache(context.Background(), []byte(`[{"id":"vendor/cached","architecture":{"output_modalities":["text"]},"supported_parameters":["tools"]}]`), time.Now()); err != nil {
		t.Fatal(err)
	}
	keyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer keyServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyServer, modelsServer)

	recorder := doRequest(t, a, cookie, "GET", "/api/v1/openrouter/capabilities?model=vendor/cached", "")
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["known"] != true || body["tools"] != true || body["verified"] != false {
		t.Fatalf("cached capabilities=%v", body)
	}
}

func TestValidateOpenRouterModelCompatibility(t *testing.T) {
	catalogModels := []catalog.Model{
		{ID: "vendor/tools", Architecture: catalog.Architecture{OutputModalities: []string{"text"}}, SupportedParameters: []string{"tools"}},
		{ID: "vendor/chat", Architecture: catalog.Architecture{OutputModalities: []string{"text"}}},
	}
	tests := []struct {
		name    string
		agent   models.Agent
		source  catalog.Source
		wantErr bool
	}{
		{"tool capable", models.Agent{Provider: "openrouter", ModelAlias: "vendor/tools"}, catalog.SourceLive, false},
		{"known without tools", models.Agent{Provider: "openrouter", ModelAlias: "vendor/chat", AllowUnverifiedModel: true}, catalog.SourceLive, true},
		{"unknown without confirmation", models.Agent{Provider: "openrouter", ModelAlias: "vendor/missing"}, catalog.SourceLive, true},
		{"unknown confirmed", models.Agent{Provider: "openrouter", ModelAlias: "vendor/missing", AllowUnverifiedModel: true}, catalog.SourceLive, false},
		{"stale cache", models.Agent{Provider: "openrouter", ModelAlias: "vendor/tools"}, catalog.SourceCache, true},
		{"stale cache confirmed", models.Agent{Provider: "openrouter", ModelAlias: "vendor/tools", AllowUnverifiedModel: true}, catalog.SourceCache, false},
		{"free router without confirmation", models.Agent{Provider: "openrouter", ModelAlias: "openrouter/free"}, catalog.SourceLive, true},
		{"free router confirmed", models.Agent{Provider: "openrouter", ModelAlias: "openrouter/free", AllowUnverifiedModel: true}, catalog.SourceLive, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOpenRouterModel(&test.agent, catalogModels, test.source)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestOpenRouterAgentModelValidation(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer keyServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testModelsResponse)) }))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyServer, modelsServer)

	request := func(name, model string, allow bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{
			"name": name, "role": "Engineer", "provider": "openrouter", "modelAlias": model,
			"allowUnverifiedModel": allow, "effort": "medium", "permission": "workspace-write", "concurrency": 1, "budget": 1,
		})
		return doRequest(t, a, cookie, "POST", "/api/v1/projects/demo-obby/agents", string(body))
	}

	if recorder := request("Tools", "vendor/paid-vision", false); recorder.Code != http.StatusCreated {
		t.Fatalf("tool model status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("Chat", "vendor/chat-only", true); recorder.Code != http.StatusBadRequest {
		t.Fatalf("non-tool model status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("Unknown", "vendor/missing", false); recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown model status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("Unknown confirmed", "vendor/missing", true); recorder.Code != http.StatusCreated {
		t.Fatalf("confirmed unknown status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("Free router", "openrouter/free", true); recorder.Code != http.StatusCreated {
		t.Fatalf("confirmed free router status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDefaultOpenRouterModelImmediatelyRepointsExistingAgents(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	keyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer keyServer.Close()
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(testModelsResponse)) }))
	defer modelsServer.Close()
	withOpenRouter(t, a, keyServer, modelsServer)

	agent, err := a.store.CreateAgent(context.Background(), models.Agent{ProjectID: "demo-obby", Name: "OpenRouter default", Role: "Engineer", Provider: "openrouter", ModelAlias: "vendor/old-model", AllowUnverifiedModel: true, Effort: "medium", Permission: "workspace-write", Concurrency: 1, Budget: 1})
	if err != nil {
		t.Fatal(err)
	}
	recorder := doRequest(t, a, cookie, "POST", "/api/v1/settings", `{"default_provider":"openrouter","default_model":"vendor/paid-vision"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	agents, err := a.store.ListAgents(context.Background(), "demo-obby")
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range agents {
		if current.ID == agent.ID {
			if current.ModelAlias != "vendor/paid-vision" || current.AllowUnverifiedModel {
				t.Fatalf("agent=%+v", current)
			}
			return
		}
	}
	t.Fatal("OpenRouter agent not found")
}

func TestOpenRouterRunValidationDetectsDisappearedModel(t *testing.T) {
	var present = true
	modelsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if present {
			_, _ = w.Write([]byte(testModelsResponse))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer modelsServer.Close()
	a := newTestAPI(t)
	a.server.orCatalog = catalog.NewService(catalog.Config{HTTPClient: modelsServer.Client(), BaseURL: modelsServer.URL, Cache: a.store, TTL: time.Hour})
	agent := models.Agent{Provider: "openrouter", ModelAlias: "vendor/paid-vision"}
	if err := a.server.validateOpenRouterAgent(context.Background(), &agent, false); err != nil {
		t.Fatalf("initial validation: %v", err)
	}
	present = false
	if err := a.server.validateOpenRouterAgent(context.Background(), &agent, true); err == nil {
		t.Fatal("model disappearance was not detected before run")
	}
}

func TestOpenRouterRoutingSettingsValidation(t *testing.T) {
	a := newTestAPI(t)
	cookie := bootstrapCookie(t, a)
	cases := []struct {
		body       string
		wantStatus int
	}{
		{`{"openrouter_data_collection":"allow"}`, 200},
		{`{"openrouter_data_collection":"deny"}`, 200},
		{`{"openrouter_data_collection":"nope"}`, 400},
		{`{"openrouter_zdr":"true"}`, 200},
		{`{"openrouter_zdr":"maybe"}`, 400},
		{`{"openrouter_allow_fallbacks":"false"}`, 200},
		{`{"openrouter_allow_fallbacks":"nope"}`, 400},
	}
	for _, c := range cases {
		recorder := doRequest(t, a, cookie, "POST", "/api/v1/settings", c.body)
		if recorder.Code != c.wantStatus {
			t.Fatalf("body=%s status=%d want=%d resp=%s", c.body, recorder.Code, c.wantStatus, recorder.Body.String())
		}
	}
}

func assertNoKeyLeak(t *testing.T, recorder *httptest.ResponseRecorder, key string) {
	t.Helper()
	if bytes.Contains(recorder.Body.Bytes(), []byte(key)) {
		t.Fatalf("response leaked the API key: %s", recorder.Body.String())
	}
	for name, values := range recorder.Header() {
		for _, v := range values {
			if strings.Contains(v, key) {
				t.Fatalf("header %s leaked the API key: %s", name, v)
			}
		}
	}
}
