package httpapi_test

import (
	"slices"
	"testing"
)

type recurrenceSuggestionOperationContract struct {
	path             string
	method           string
	operationID      string
	suggestionIDPath bool
	responses        []recurrenceSuggestionResponseContract
}

type recurrenceSuggestionResponseContract struct {
	status    string
	component string
	schema    string
	headers   []string
	content   bool
}

func TestRecurrenceSuggestionOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	secureHeaders := []string{"Cache-Control", "X-Content-Type-Options"}
	operations := []recurrenceSuggestionOperationContract{
		{
			path:        "/v1/recurrence-suggestions",
			method:      "get",
			operationID: "listRecurrenceSuggestions",
			responses: []recurrenceSuggestionResponseContract{
				{status: "200", schema: "RecurrenceSuggestionList", headers: secureHeaders, content: true},
				{status: "400", component: "InvalidRequest", schema: "InvalidRequestError", headers: secureHeaders, content: true},
				{status: "405", component: "MethodNotAllowed", schema: "MethodNotAllowedError", headers: secureHeaders, content: true},
				{status: "500", component: "InternalError", schema: "InternalError", headers: secureHeaders, content: true},
			},
		},
		{
			path:             "/v1/recurrence-suggestions/{suggestionId}/dismiss",
			method:           "post",
			operationID:      "dismissRecurrenceSuggestion",
			suggestionIDPath: true,
			responses: []recurrenceSuggestionResponseContract{
				{status: "204", headers: []string{"Cache-Control", "Idempotency-Replayed", "X-Content-Type-Options"}},
				{status: "400", component: "InvalidRequest", schema: "InvalidRequestError", headers: secureHeaders, content: true},
				{status: "404", component: "RecurrenceSuggestionNotFound", schema: "RecurrenceSuggestionNotFoundError", headers: secureHeaders, content: true},
				{status: "405", component: "MethodNotAllowed", schema: "MethodNotAllowedError", headers: secureHeaders, content: true},
				{status: "500", component: "InternalError", schema: "InternalError", headers: secureHeaders, content: true},
			},
		},
		{
			path:             "/v1/recurrence-suggestions/{suggestionId}/preview",
			method:           "post",
			operationID:      "previewSuggestedRecurrence",
			suggestionIDPath: true,
			responses: []recurrenceSuggestionResponseContract{
				{status: "200", schema: "RecurrencePreview", headers: secureHeaders, content: true},
				{status: "400", component: "InvalidRequest", schema: "InvalidRequestError", headers: secureHeaders, content: true},
				{status: "404", component: "RecurrenceSuggestionNotFound", schema: "RecurrenceSuggestionNotFoundError", headers: secureHeaders, content: true},
				{status: "405", component: "MethodNotAllowed", schema: "MethodNotAllowedError", headers: secureHeaders, content: true},
				{status: "409", component: "RecurrenceSuggestionSuppressed", schema: "RecurrenceSuggestionSuppressedError", headers: secureHeaders, content: true},
				{status: "500", component: "InternalError", schema: "InternalError", headers: secureHeaders, content: true},
			},
		},
	}

	for _, expectation := range operations {
		expectation := expectation
		t.Run(expectation.operationID, func(t *testing.T) {
			assertHTTPMethods(t, paths, expectation.path, []string{expectation.method})
			operation := contractOperation(t, paths, expectation.path, expectation.method)
			assertSuggestionOperationID(t, operation, expectation.operationID)
			assertNoRequestBody(t, operation)
			if expectation.suggestionIDPath {
				assertSuggestionIDParameter(t, operation)
			} else {
				assertNoParameters(t, operation)
			}
			assertSuggestionResponseMatrix(t, document, operation, expectation.responses)
		})
	}

	assertRecurrenceSuggestionSchemas(t, document)
}

func assertSuggestionResponseMatrix(
	t *testing.T,
	document map[string]any,
	operation map[string]any,
	expectations []recurrenceSuggestionResponseContract,
) {
	t.Helper()
	responses := contractObject(t, operation["responses"], "responses")
	statuses := make([]string, 0, len(expectations))
	for _, expectation := range expectations {
		statuses = append(statuses, expectation.status)
	}
	assertExactStrings(t, mapKeys(responses), statuses, "response statuses")

	components := contractObject(t, contractAt(t, document, "components", "responses"), "components.responses")
	for _, expectation := range expectations {
		response := contractObject(t, responses[expectation.status], "response "+expectation.status)
		if expectation.component != "" {
			assertRef(t, response, "responses", expectation.component)
			response = contractObject(t, components[expectation.component], expectation.component)
		}
		headers := contractObject(t, response["headers"], "response "+expectation.status+" headers")
		assertExactStrings(t, mapKeys(headers), expectation.headers, "response "+expectation.status+" headers")
		for header := range headers {
			assertRef(t, headers[header], "headers", headerComponentName(header))
		}

		content, hasContent := response["content"]
		if hasContent != expectation.content {
			t.Fatalf("response %s content presence = %t, want %t", expectation.status, hasContent, expectation.content)
		}
		if expectation.content {
			mediaTypes := contractObject(t, content, "response "+expectation.status+" content")
			assertExactStrings(t, mapKeys(mediaTypes), []string{"application/json"}, "response "+expectation.status+" media types")
			assertRef(t, contractAt(t, mediaTypes, "application/json", "schema"), "schemas", expectation.schema)
		}
	}
}

func assertRecurrenceSuggestionSchemas(t *testing.T, document map[string]any) {
	t.Helper()
	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	id := contractObject(t, schemas["RecurrenceSuggestionId"], "RecurrenceSuggestionId")
	if contractString(t, id["pattern"], "RecurrenceSuggestionId.pattern") != "^rsg_[0-9a-f]{64}$" {
		t.Fatal("RecurrenceSuggestionId pattern differs from Application validation")
	}

	suggestion := contractObject(t, schemas["RecurrenceSuggestion"], "RecurrenceSuggestion")
	wanted := []string{"anchorDay", "description", "evidenceCount", "expectedAmount", "id", "observedDates", "proposedStartsOn"}
	assertClosedObject(t, suggestion, "RecurrenceSuggestion", wanted)
	properties := contractObject(t, suggestion["properties"], "RecurrenceSuggestion.properties")
	assertExactStrings(t, mapKeys(properties), wanted, "RecurrenceSuggestion properties")
	for _, forbidden := range []string{"fingerprint", "normalizedDescription", "ownerId", "expenseIds", "ruleVersion", "suppressed"} {
		if slices.Contains(mapKeys(properties), forbidden) {
			t.Fatalf("internal field %q is public", forbidden)
		}
	}
	assertRef(t, properties["id"], "schemas", "RecurrenceSuggestionId")
	assertRef(t, properties["expectedAmount"], "schemas", "Money")
	assertRef(t, properties["proposedStartsOn"], "schemas", "CivilDate")
	assertRef(t, contractAt(t, properties, "observedDates", "items"), "schemas", "CivilDate")

	list := contractObject(t, schemas["RecurrenceSuggestionList"], "RecurrenceSuggestionList")
	assertClosedObject(t, list, "RecurrenceSuggestionList", []string{"items"})
	assertRef(t, contractAt(t, list, "properties", "items", "items"), "schemas", "RecurrenceSuggestion")

	assertSuggestionErrorCode(t, schemas, "RecurrenceSuggestionNotFoundError", "RECURRENCE_SUGGESTION_NOT_FOUND")
	assertSuggestionErrorCode(t, schemas, "RecurrenceSuggestionSuppressedError", "RECURRENCE_SUGGESTION_SUPPRESSED")
}

func assertSuggestionErrorCode(t *testing.T, schemas map[string]any, schemaName, code string) {
	t.Helper()
	schema := contractObject(t, schemas[schemaName], schemaName)
	assertClosedObject(t, schema, schemaName, []string{"error"})
	errorBody := contractObject(t, contractAt(t, schema, "properties", "error"), schemaName+".error")
	assertClosedObject(t, errorBody, schemaName+".error", []string{"code", "message"})
	assertExactStrings(t, contractStrings(t, contractAt(t, errorBody, "properties", "code", "enum"), schemaName+".codes"), []string{code}, schemaName+" codes")
}

func assertSuggestionOperationID(t *testing.T, operation map[string]any, wanted string) {
	t.Helper()
	if got := contractString(t, operation["operationId"], "operationId"); got != wanted {
		t.Fatalf("operationId = %q, want %q", got, wanted)
	}
}

func assertNoRequestBody(t *testing.T, operation map[string]any) {
	t.Helper()
	if _, exists := operation["requestBody"]; exists {
		t.Fatal("operation must not declare requestBody")
	}
}

func assertNoParameters(t *testing.T, operation map[string]any) {
	t.Helper()
	refs, inline := operationParameters(t, operation)
	if len(refs) != 0 || len(inline) != 0 {
		t.Fatalf("operation parameters = refs %v inline %v, want none", refs, inline)
	}
}

func assertSuggestionIDParameter(t *testing.T, operation map[string]any) {
	t.Helper()
	refs, inline := operationParameters(t, operation)
	if len(refs) != 0 || len(inline) != 1 {
		t.Fatalf("suggestion parameters = refs %v inline %v", refs, inline)
	}
	parameter := inline[0]
	if contractString(t, parameter["name"], "parameter.name") != "suggestionId" ||
		contractString(t, parameter["in"], "parameter.in") != "path" ||
		!contractBool(t, parameter["required"], "parameter.required") {
		t.Fatalf("suggestionId parameter = %#v", parameter)
	}
	assertRef(t, parameter["schema"], "schemas", "RecurrenceSuggestionId")
}
