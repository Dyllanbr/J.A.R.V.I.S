package httpapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type recurrenceOperationContract struct {
	path             string
	method           string
	operationID      string
	requestSchema    string
	successStatus    string
	responseSchema   string
	successHeaders   []string
	errorResponses   map[string]string
	idempotencyKey   bool
	recurrenceIDPath bool
}

func TestRecurrenceOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.6.0" {
		t.Fatalf("info.version = %q, want 0.6.0", got)
	}

	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	assertHTTPMethods(t, paths, "/v1/recurrences/preview", []string{"post"})
	assertHTTPMethods(t, paths, "/v1/recurrences", []string{"get", "post"})
	assertHTTPMethods(t, paths, "/v1/recurrences/{recurrenceId}/cancel", []string{"post"})

	operations := []recurrenceOperationContract{
		{
			path:           "/v1/recurrences/preview",
			method:         "post",
			operationID:    "previewRecurrence",
			requestSchema:  "RecurrenceRequest",
			successStatus:  "200",
			responseSchema: "RecurrencePreview",
			successHeaders: []string{"Cache-Control", "X-Content-Type-Options"},
			errorResponses: map[string]string{
				"400": "RecurrencePreviewBadRequest",
				"405": "MethodNotAllowed",
				"500": "InternalError",
			},
		},
		{
			path:           "/v1/recurrences",
			method:         "post",
			operationID:    "recordRecurrence",
			requestSchema:  "RecurrenceRequest",
			successStatus:  "201",
			responseSchema: "Recurrence",
			successHeaders: []string{"Cache-Control", "Idempotency-Replayed", "X-Content-Type-Options"},
			errorResponses: map[string]string{
				"400": "RecurrenceCreateBadRequest",
				"405": "MethodNotAllowed",
				"409": "IdempotencyConflict",
				"500": "InternalError",
			},
			idempotencyKey: true,
		},
		{
			path:           "/v1/recurrences",
			method:         "get",
			operationID:    "listRecurrences",
			successStatus:  "200",
			responseSchema: "RecurrenceList",
			successHeaders: []string{"Cache-Control", "X-Content-Type-Options"},
			errorResponses: map[string]string{
				"400": "InvalidRequest",
				"405": "MethodNotAllowed",
				"500": "InternalError",
			},
		},
		{
			path:           "/v1/recurrences/{recurrenceId}/cancel",
			method:         "post",
			operationID:    "cancelRecurrence",
			successStatus:  "200",
			responseSchema: "Recurrence",
			successHeaders: []string{"Cache-Control", "Idempotency-Replayed", "X-Content-Type-Options"},
			errorResponses: map[string]string{
				"400": "RecurrenceCancelBadRequest",
				"404": "RecurrenceNotFound",
				"405": "MethodNotAllowed",
				"409": "RecurrenceCancelConflict",
				"500": "InternalError",
			},
			idempotencyKey:   true,
			recurrenceIDPath: true,
		},
	}

	for _, expectation := range operations {
		expectation := expectation
		t.Run(expectation.operationID, func(t *testing.T) {
			operation := contractOperation(t, paths, expectation.path, expectation.method)
			assertRecurrenceOperation(t, operation, expectation)
		})
	}

	assertRecurrenceSchemas(t, document)
	assertNarrowErrorSchemas(t, document)
	assertLegacyOperationsUseNarrowErrors(t, paths)
}

func assertRecurrenceOperation(t *testing.T, operation map[string]any, expectation recurrenceOperationContract) {
	t.Helper()
	if got := contractString(t, operation["operationId"], "operationId"); got != expectation.operationID {
		t.Fatalf("operationId = %q, want %q", got, expectation.operationID)
	}

	requestBody, hasRequestBody := operation["requestBody"]
	if expectation.requestSchema == "" {
		if hasRequestBody {
			t.Fatal("operation unexpectedly declares requestBody")
		}
	} else {
		body := contractObject(t, requestBody, "requestBody")
		if !contractBool(t, body["required"], "requestBody.required") {
			t.Fatal("requestBody.required = false, want true")
		}
		assertRef(t, contractAt(t, body, "content", "application/json", "schema"), "schemas", expectation.requestSchema)
	}

	parameterRefs, inlineParameters := operationParameters(t, operation)
	if expectation.idempotencyKey {
		assertExactStrings(t, parameterRefs, []string{"#/components/parameters/IdempotencyKey"}, "parameter refs")
	} else if len(parameterRefs) != 0 {
		t.Fatalf("parameter refs = %v, want none", parameterRefs)
	}
	if expectation.recurrenceIDPath {
		if len(inlineParameters) != 1 {
			t.Fatalf("inline parameters = %d, want recurrenceId only", len(inlineParameters))
		}
		parameter := inlineParameters[0]
		if contractString(t, parameter["name"], "parameter.name") != "recurrenceId" ||
			contractString(t, parameter["in"], "parameter.in") != "path" ||
			!contractBool(t, parameter["required"], "parameter.required") {
			t.Fatalf("recurrenceId path parameter is not required and path-scoped: %#v", parameter)
		}
		assertRef(t, parameter["schema"], "schemas", "RecurrenceId")
	} else if len(inlineParameters) != 0 {
		t.Fatalf("inline parameters = %d, want none", len(inlineParameters))
	}

	responses := contractObject(t, operation["responses"], "responses")
	wantedStatuses := []string{expectation.successStatus}
	for status := range expectation.errorResponses {
		wantedStatuses = append(wantedStatuses, status)
	}
	assertExactStrings(t, mapKeys(responses), wantedStatuses, "response statuses")

	success := contractObject(t, responses[expectation.successStatus], "success response")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", expectation.responseSchema)
	headers := contractObject(t, success["headers"], "success headers")
	assertExactStrings(t, mapKeys(headers), expectation.successHeaders, "success headers")
	for header := range headers {
		assertRef(t, headers[header], "headers", headerComponentName(header))
	}

	for status, component := range expectation.errorResponses {
		assertRef(t, responses[status], "responses", component)
	}
}

func assertRecurrenceSchemas(t *testing.T, document map[string]any) {
	t.Helper()
	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "components.schemas")

	for _, name := range []string{"RecurrenceRequest", "RecurrencePreview"} {
		schema := contractObject(t, schemas[name], name)
		assertClosedObject(t, schema, name, []string{"type", "description", "expectedAmount", "frequency", "startsOn"})
		properties := contractObject(t, schema["properties"], name+".properties")
		assertConst(t, properties["type"], "EXPENSE")
		assertRef(t, properties["expectedAmount"], "schemas", "Money")
		assertConst(t, properties["frequency"], "MONTHLY")
		assertRef(t, properties["startsOn"], "schemas", "CivilDate")
	}

	recurrence := contractObject(t, schemas["Recurrence"], "Recurrence")
	assertClosedObject(t, recurrence, "Recurrence", []string{
		"id", "type", "description", "expectedAmount", "frequency", "startsOn", "status", "createdAt",
	})
	properties := contractObject(t, recurrence["properties"], "Recurrence.properties")
	assertRef(t, properties["id"], "schemas", "RecurrenceId")
	assertConst(t, properties["type"], "EXPENSE")
	assertRef(t, properties["expectedAmount"], "schemas", "Money")
	assertConst(t, properties["frequency"], "MONTHLY")
	assertRef(t, properties["startsOn"], "schemas", "CivilDate")
	assertExactStrings(t, contractStrings(t, contractAt(t, properties, "status", "enum"), "Recurrence.status.enum"), []string{"ACTIVE", "CANCELLED"}, "Recurrence.status.enum")
	if slices.Contains(contractStrings(t, recurrence["required"], "Recurrence.required"), "cancelledAt") {
		t.Fatal("Recurrence.cancelledAt must remain optional")
	}

	list := contractObject(t, schemas["RecurrenceList"], "RecurrenceList")
	assertClosedObject(t, list, "RecurrenceList", []string{"items"})
	assertRef(t, contractAt(t, list, "properties", "items", "items"), "schemas", "Recurrence")

	money := contractObject(t, schemas["Money"], "Money")
	assertClosedObject(t, money, "Money", []string{"minor", "currency"})
	assertConst(t, contractAt(t, money, "properties", "currency"), "BRL")
	if contractString(t, contractAt(t, schemas, "CivilDate", "format"), "CivilDate.format") != "date" {
		t.Fatal("CivilDate.format must be date")
	}

	parameter := contractObject(t, contractAt(t, document, "components", "parameters", "IdempotencyKey"), "IdempotencyKey")
	if contractString(t, parameter["name"], "IdempotencyKey.name") != "Idempotency-Key" ||
		contractString(t, parameter["in"], "IdempotencyKey.in") != "header" ||
		!contractBool(t, parameter["required"], "IdempotencyKey.required") {
		t.Fatalf("IdempotencyKey component does not match runtime: %#v", parameter)
	}
}

func assertNarrowErrorSchemas(t *testing.T, document map[string]any) {
	t.Helper()
	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "components.schemas")
	if _, broadEnvelope := schemas["ErrorEnvelope"]; broadEnvelope {
		t.Fatal("a global cross-operation ErrorEnvelope must not be part of the public contract")
	}

	wantedCodes := map[string][]string{
		"InvalidRequestError":               {"INVALID_REQUEST"},
		"TransactionPreviewBadRequestError": {"INVALID_REQUEST", "UNSUPPORTED_TRANSACTION_TYPE"},
		"TransactionCreateBadRequestError":  {"IDEMPOTENCY_KEY_INVALID", "IDEMPOTENCY_KEY_REQUIRED", "INVALID_REQUEST", "UNSUPPORTED_TRANSACTION_TYPE"},
		"RecurrencePreviewBadRequestError":  {"INVALID_REQUEST", "UNSUPPORTED_RECURRENCE_TYPE"},
		"RecurrenceCreateBadRequestError":   {"IDEMPOTENCY_KEY_INVALID", "IDEMPOTENCY_KEY_REQUIRED", "INVALID_REQUEST", "UNSUPPORTED_RECURRENCE_TYPE"},
		"RecurrenceCancelBadRequestError":   {"IDEMPOTENCY_KEY_INVALID", "IDEMPOTENCY_KEY_REQUIRED", "INVALID_REQUEST"},
		"IdempotencyConflictError":          {"IDEMPOTENCY_KEY_REUSED"},
		"RecurrenceCancelConflictError":     {"IDEMPOTENCY_KEY_REUSED", "RECURRENCE_ALREADY_CANCELLED"},
		"RecurrenceNotFoundError":           {"RECURRENCE_NOT_FOUND"},
		"MethodNotAllowedError":             {"METHOD_NOT_ALLOWED"},
		"InternalError":                     {"INTERNAL_ERROR"},
	}
	for schemaName, codes := range wantedCodes {
		schema := contractObject(t, schemas[schemaName], schemaName)
		assertClosedObject(t, schema, schemaName, []string{"error"})
		errorBody := contractObject(t, contractAt(t, schema, "properties", "error"), schemaName+".error")
		assertClosedObject(t, errorBody, schemaName+".error", []string{"code", "message"})
		got := contractStrings(t, contractAt(t, errorBody, "properties", "code", "enum"), schemaName+" code enum")
		assertExactStrings(t, got, codes, schemaName+" code enum")
	}

	responses := contractObject(t, contractAt(t, document, "components", "responses"), "components.responses")
	responseSchemas := map[string]string{
		"InvalidRequest":               "InvalidRequestError",
		"TransactionPreviewBadRequest": "TransactionPreviewBadRequestError",
		"TransactionCreateBadRequest":  "TransactionCreateBadRequestError",
		"RecurrencePreviewBadRequest":  "RecurrencePreviewBadRequestError",
		"RecurrenceCreateBadRequest":   "RecurrenceCreateBadRequestError",
		"RecurrenceCancelBadRequest":   "RecurrenceCancelBadRequestError",
		"IdempotencyConflict":          "IdempotencyConflictError",
		"RecurrenceCancelConflict":     "RecurrenceCancelConflictError",
		"RecurrenceNotFound":           "RecurrenceNotFoundError",
		"MethodNotAllowed":             "MethodNotAllowedError",
		"InternalError":                "InternalError",
	}
	for responseName, schemaName := range responseSchemas {
		response := contractObject(t, responses[responseName], responseName)
		assertRef(t, contractAt(t, response, "content", "application/json", "schema"), "schemas", schemaName)
		headers := contractObject(t, response["headers"], responseName+".headers")
		assertExactStrings(t, mapKeys(headers), []string{"Cache-Control", "X-Content-Type-Options"}, responseName+" headers")
	}
}

func assertLegacyOperationsUseNarrowErrors(t *testing.T, paths map[string]any) {
	t.Helper()
	expectations := []struct {
		path, method, status, response string
	}{
		{"/v1/transactions/preview", "post", "400", "TransactionPreviewBadRequest"},
		{"/v1/transactions", "post", "400", "TransactionCreateBadRequest"},
		{"/v1/transactions", "post", "409", "IdempotencyConflict"},
		{"/v1/transactions", "get", "400", "InvalidRequest"},
	}
	for _, expectation := range expectations {
		operation := contractOperation(t, paths, expectation.path, expectation.method)
		response := contractAt(t, operation, "responses", expectation.status)
		assertRef(t, response, "responses", expectation.response)
	}
}

func loadOpenAPIContract(t *testing.T) map[string]any {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate recurrence contract test source")
	}
	contractPath := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../../contracts/openapi/openapi.yaml"))
	contract, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("reading OpenAPI contract: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(contract, &document); err != nil {
		t.Fatalf("parsing OpenAPI contract structurally: %v", err)
	}
	return document
}

func contractOperation(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	pathItem := contractObject(t, paths[path], path)
	return contractObject(t, pathItem[method], path+" "+method)
}

func assertHTTPMethods(t *testing.T, paths map[string]any, path string, wanted []string) {
	t.Helper()
	pathItem := contractObject(t, paths[path], path)
	methods := make([]string, 0)
	for _, method := range []string{"get", "post", "put", "patch", "delete", "options", "head", "trace"} {
		if _, exists := pathItem[method]; exists {
			methods = append(methods, method)
		}
	}
	assertExactStrings(t, methods, wanted, path+" methods")
}

func operationParameters(t *testing.T, operation map[string]any) ([]string, []map[string]any) {
	t.Helper()
	raw, exists := operation["parameters"]
	if !exists {
		return nil, nil
	}
	values := contractArray(t, raw, "parameters")
	refs := make([]string, 0)
	inline := make([]map[string]any, 0)
	for index, value := range values {
		parameter := contractObject(t, value, "parameter")
		if ref, exists := parameter["$ref"]; exists {
			refs = append(refs, contractString(t, ref, "parameter.$ref"))
			continue
		}
		if parameter == nil {
			t.Fatalf("parameter %d is nil", index)
		}
		inline = append(inline, parameter)
	}
	return refs, inline
}

func assertClosedObject(t *testing.T, schema map[string]any, name string, required []string) {
	t.Helper()
	if contractString(t, schema["type"], name+".type") != "object" {
		t.Fatalf("%s.type is not object", name)
	}
	if contractBool(t, schema["additionalProperties"], name+".additionalProperties") {
		t.Fatalf("%s.additionalProperties = true, want false", name)
	}
	assertExactStrings(t, contractStrings(t, schema["required"], name+".required"), required, name+".required")
}

func assertConst(t *testing.T, value any, wanted string) {
	t.Helper()
	object := contractObject(t, value, "const schema")
	if got := contractString(t, object["const"], "const"); got != wanted {
		t.Fatalf("const = %q, want %q", got, wanted)
	}
}

func assertRef(t *testing.T, value any, componentType, componentName string) {
	t.Helper()
	object := contractObject(t, value, "$ref object")
	wanted := "#/components/" + componentType + "/" + componentName
	if got := contractString(t, object["$ref"], "$ref"); got != wanted {
		t.Fatalf("$ref = %q, want %q", got, wanted)
	}
}

func headerComponentName(header string) string {
	switch header {
	case "Cache-Control":
		return "CacheControl"
	case "X-Content-Type-Options":
		return "ContentTypeOptions"
	case "Idempotency-Replayed":
		return "IdempotencyReplayed"
	default:
		return header
	}
}

func contractAt(t *testing.T, root map[string]any, path ...string) any {
	t.Helper()
	var current any = root
	for _, part := range path {
		object := contractObject(t, current, part)
		value, exists := object[part]
		if !exists {
			t.Fatalf("contract node %q is missing", part)
		}
		current = value
	}
	return current
}

func contractObject(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s has type %T, want object", label, value)
	}
	return object
}

func contractArray(t *testing.T, value any, label string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s has type %T, want array", label, value)
	}
	return array
}

func contractString(t *testing.T, value any, label string) string {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("%s has type %T, want string", label, value)
	}
	return text
}

func contractBool(t *testing.T, value any, label string) bool {
	t.Helper()
	result, ok := value.(bool)
	if !ok {
		t.Fatalf("%s has type %T, want bool", label, value)
	}
	return result
}

func contractStrings(t *testing.T, value any, label string) []string {
	t.Helper()
	array := contractArray(t, value, label)
	result := make([]string, 0, len(array))
	for _, item := range array {
		result = append(result, contractString(t, item, label))
	}
	return result
}

func assertExactStrings(t *testing.T, got, wanted []string, label string) {
	t.Helper()
	got = append([]string(nil), got...)
	wanted = append([]string(nil), wanted...)
	slices.Sort(got)
	slices.Sort(wanted)
	if !slices.Equal(got, wanted) {
		t.Fatalf("%s = %v, want %v", label, got, wanted)
	}
}

func mapKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}
