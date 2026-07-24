package dnsimple

// The Fake must drift toward production, not toward our gateways'
// assumptions (the /dns_analytics envelope bug encoded exactly that
// drift). Every grey-box test in this package therefore serves the Fake
// through a wrapper that validates each response against DNSimple's
// published OpenAPI spec (testdata/openapi.yml, from
// dnsimple/dnsimple-developer). The Fake is deliberately minimal, so the
// spec's `required` lists are stripped before validation: the Fake may say
// less than production, but never something production could not say.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"go.yaml.in/yaml/v4"
)

var (
	contractOnce      sync.Once
	contractValidator validator.Validator
	contractLoadError error
)

// stripRequired removes every schema-level `required` list (sequences only:
// the boolean `required` on parameters must survive).
func stripRequired(node any) any {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if key == "required" {
				if _, isList := value.([]any); isList {
					delete(typed, key)
					continue
				}
			}
			typed[key] = stripRequired(value)
		}
		return typed
	case []any:
		for index, value := range typed {
			typed[index] = stripRequired(value)
		}
		return typed
	default:
		return node
	}
}

func loadContractValidator() (validator.Validator, error) {
	raw, err := os.ReadFile("testdata/openapi.yml")
	if err != nil {
		return nil, err
	}
	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	relaxed, err := yaml.Marshal(stripRequired(tree))
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocument(relaxed)
	if err != nil {
		return nil, err
	}
	built, errs := validator.NewValidator(document)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return built, nil
}

// specExempt lists request-path prefixes the published spec does not cover;
// every entry is a documented gap, not an escape hatch.
var specExempt = []string{
	"/oauth/", // the OAuth token exchange lives outside the v2 API spec
}

// specBroken lists path fragments where the published spec contradicts
// observed production behavior; each entry cites the evidence.
var specBroken = []string{
	// The spec types dns_analytics rows' items as scalars while its own
	// example (and production, verified live in v0.18.1) sends each row as
	// an array of values.
	"/dns_analytics",
}

// newContractServer serves the Fake with per-exchange spec validation, so
// every grey-box test in this package doubles as a contract test.
func newContractServer(test *testing.T, fake *Fake) *httptest.Server {
	test.Helper()
	contractOnce.Do(func() {
		contractValidator, contractLoadError = loadContractValidator()
	})
	if contractLoadError != nil {
		test.Fatalf("loading the OpenAPI contract: %v", contractLoadError)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		recorder := httptest.NewRecorder()
		fake.ServeHTTP(recorder, request)
		for _, prefix := range specExempt {
			if strings.HasPrefix(request.URL.Path, prefix) {
				copyRecorded(response, recorder)
				return
			}
		}
		for _, fragment := range specBroken {
			if strings.Contains(request.URL.Path, fragment) {
				copyRecorded(response, recorder)
				return
			}
		}
		// The spec's server URL carries the /v2 prefix our client omits;
		// validation needs the request shaped as production would see it.
		specRequest := request.Clone(request.Context())
		specRequest.URL = &url.URL{
			Scheme: "https", Host: "api.dnsimple.com",
			Path: "/v2" + request.URL.Path, RawQuery: request.URL.RawQuery,
		}
		specRequest.Host = "api.dnsimple.com"
		// 429 and 5xx are infrastructure responses the spec does not define
		// per operation; the client suite injects them deliberately.
		if recorder.Code == http.StatusTooManyRequests || recorder.Code >= http.StatusInternalServerError {
			copyRecorded(response, recorder)
			return
		}
		valid, validationErrors := contractValidator.ValidateHttpResponse(specRequest, recorder.Result())
		if !valid {
			for _, validationError := range validationErrors {
				detail := ""
				for _, schemaError := range validationError.SchemaValidationErrors {
					detail += "\n    " + schemaError.FieldPath + ": " + schemaError.Reason
				}
				test.Errorf("contract violation on %s %s (status %d): %s: %s%s",
					request.Method, request.URL.Path, recorder.Code,
					validationError.Message, validationError.Reason, detail)
			}
		}
		copyRecorded(response, recorder)
	}))
	test.Cleanup(server.Close)
	return server
}

func copyRecorded(response http.ResponseWriter, recorder *httptest.ResponseRecorder) {
	for key, values := range recorder.Header() {
		for _, value := range values {
			response.Header().Add(key, value)
		}
	}
	response.WriteHeader(recorder.Code)
	_, _ = response.Write(recorder.Body.Bytes())
}
