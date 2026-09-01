package httpapi_test

import (
	"slices"
	"testing"
)

type creditCardOperationContract struct {
	path           string
	method         string
	operationID    string
	requestSchema  string
	successStatus  string
	responseSchema string
	headers        []string
	errors         map[string]string
	idempotency    bool
	pathID         bool
}

func TestCreditCardOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	assertHTTPMethods(t, paths, "/v1/cards/preview", []string{"post"})
	assertHTTPMethods(t, paths, "/v1/cards", []string{"get", "post"})
	assertHTTPMethods(t, paths, "/v1/cards/{cardId}", []string{"get"})
	assertHTTPMethods(t, paths, "/v1/cards/{cardId}/archive", []string{"post"})

	operations := []creditCardOperationContract{
		{
			path: "/v1/cards/preview", method: "post", operationID: "previewCreditCard",
			requestSchema: "CreditCardCreateRequest", successStatus: "200", responseSchema: "CreditCardPreview",
			headers: []string{"Cache-Control", "X-Content-Type-Options"},
			errors:  map[string]string{"400": "InvalidRequest", "405": "MethodNotAllowed", "500": "InternalError"},
		},
		{
			path: "/v1/cards", method: "post", operationID: "recordCreditCard",
			requestSchema: "CreditCardCreateRequest", successStatus: "201", responseSchema: "CreditCard", idempotency: true,
			headers: []string{"Cache-Control", "Idempotency-Replayed", "X-Content-Type-Options"},
			errors:  map[string]string{"400": "CreditCardCreateBadRequest", "405": "MethodNotAllowed", "409": "IdempotencyConflict", "500": "InternalError"},
		},
		{
			path: "/v1/cards", method: "get", operationID: "listCreditCards",
			successStatus: "200", responseSchema: "CreditCardListResponse",
			headers: []string{"Cache-Control", "X-Content-Type-Options"},
			errors:  map[string]string{"400": "InvalidRequest", "405": "MethodNotAllowed", "500": "InternalError"},
		},
		{
			path: "/v1/cards/{cardId}", method: "get", operationID: "getCreditCard", pathID: true,
			successStatus: "200", responseSchema: "CreditCard",
			headers: []string{"Cache-Control", "X-Content-Type-Options"},
			errors:  map[string]string{"400": "InvalidRequest", "404": "CreditCardNotFound", "405": "MethodNotAllowed", "500": "InternalError"},
		},
		{
			path: "/v1/cards/{cardId}/archive", method: "post", operationID: "archiveCreditCard", pathID: true, idempotency: true,
			successStatus: "200", responseSchema: "CreditCard",
			headers: []string{"Cache-Control", "Idempotency-Replayed", "X-Content-Type-Options"},
			errors:  map[string]string{"400": "CreditCardArchiveBadRequest", "404": "CreditCardNotFound", "405": "MethodNotAllowed", "409": "CreditCardArchiveConflict", "500": "InternalError"},
		},
	}

	for _, expectation := range operations {
		t.Run(expectation.operationID, func(t *testing.T) {
			assertCreditCardOperation(t, contractOperation(t, paths, expectation.path, expectation.method), expectation)
		})
	}
	assertCreditCardSchemas(t, document)
	assertCreditCardErrors(t, document)
}

func assertCreditCardOperation(t *testing.T, operation map[string]any, expectation creditCardOperationContract) {
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
			t.Fatal("requestBody must be required")
		}
		assertRef(t, contractAt(t, body, "content", "application/json", "schema"), "schemas", expectation.requestSchema)
	}

	refs, inline := operationParameters(t, operation)
	wantedRefs := []string{}
	if expectation.idempotency {
		wantedRefs = append(wantedRefs, "#/components/parameters/IdempotencyKey")
	}
	assertExactStrings(t, refs, wantedRefs, "parameter refs")
	if expectation.pathID {
		if len(inline) != 1 {
			t.Fatalf("inline parameters = %d, want cardId", len(inline))
		}
		parameter := inline[0]
		if contractString(t, parameter["name"], "cardId.name") != "cardId" ||
			contractString(t, parameter["in"], "cardId.in") != "path" ||
			!contractBool(t, parameter["required"], "cardId.required") {
			t.Fatalf("invalid cardId parameter: %#v", parameter)
		}
		assertRef(t, parameter["schema"], "schemas", "CreditCardId")
	} else if len(inline) != 0 {
		t.Fatalf("inline parameters = %d, want none", len(inline))
	}

	responses := contractObject(t, operation["responses"], "responses")
	statuses := []string{expectation.successStatus}
	for status := range expectation.errors {
		statuses = append(statuses, status)
	}
	assertExactStrings(t, mapKeys(responses), statuses, "response statuses")
	success := contractObject(t, responses[expectation.successStatus], "success")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", expectation.responseSchema)
	headers := contractObject(t, success["headers"], "success.headers")
	assertExactStrings(t, mapKeys(headers), expectation.headers, "success headers")
	for name := range headers {
		assertRef(t, headers[name], "headers", headerComponentName(name))
	}
	for status, component := range expectation.errors {
		assertRef(t, responses[status], "responses", component)
	}
}

func assertCreditCardSchemas(t *testing.T, document map[string]any) {
	t.Helper()
	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	for _, name := range []string{"CreditCardCreateRequest", "CreditCardPreview"} {
		schema := contractObject(t, schemas[name], name)
		assertClosedObject(t, schema, name, []string{"name", "closingDay", "dueDay"})
		properties := contractObject(t, schema["properties"], name+".properties")
		assertExactStrings(t, mapKeys(properties), []string{"brand", "closingDay", "creditLimit", "dueDay", "lastFour", "name"}, name+" properties")
		assertRef(t, properties["brand"], "schemas", "CreditCardBrand")
		assertRef(t, properties["creditLimit"], "schemas", "Money")
	}
	cardID := contractObject(t, schemas["CreditCardId"], "CreditCardId")
	if pattern := contractString(t, cardID["pattern"], "CreditCardId.pattern"); pattern != "^card_[0-9a-f]{32}$" {
		t.Fatalf("CreditCardId.pattern = %q", pattern)
	}
	brand := contractObject(t, schemas["CreditCardBrand"], "CreditCardBrand")
	assertExactStrings(t, contractStrings(t, brand["enum"], "CreditCardBrand.enum"),
		[]string{"VISA", "MASTERCARD", "ELO", "AMERICAN_EXPRESS", "HIPERCARD", "OTHER"}, "CreditCardBrand.enum")
	status := contractObject(t, schemas["CreditCardStatus"], "CreditCardStatus")
	assertExactStrings(t, contractStrings(t, status["enum"], "CreditCardStatus.enum"),
		[]string{"ACTIVE", "ARCHIVED"}, "CreditCardStatus.enum")

	card := contractObject(t, schemas["CreditCard"], "CreditCard")
	assertClosedObject(t, card, "CreditCard", []string{"id", "name", "closingDay", "dueDay", "status", "createdAt"})
	properties := contractObject(t, card["properties"], "CreditCard.properties")
	assertExactStrings(t, mapKeys(properties), []string{
		"archivedAt", "brand", "closingDay", "createdAt", "creditLimit", "dueDay", "id", "lastFour", "name", "status",
	}, "CreditCard properties")
	for _, forbidden := range []string{"owner", "userId", "number", "pan", "cvv", "paymentToken", "availableLimit", "balance", "invoice"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("CreditCard exposes forbidden property %s", forbidden)
		}
	}
	assertRef(t, properties["id"], "schemas", "CreditCardId")
	assertRef(t, properties["status"], "schemas", "CreditCardStatus")
	if slices.Contains(contractStrings(t, card["required"], "CreditCard.required"), "archivedAt") {
		t.Fatal("archivedAt must remain optional")
	}
	list := contractObject(t, schemas["CreditCardListResponse"], "CreditCardListResponse")
	assertClosedObject(t, list, "CreditCardListResponse", []string{"items"})
	assertRef(t, contractAt(t, list, "properties", "items", "items"), "schemas", "CreditCard")
}

func assertCreditCardErrors(t *testing.T, document map[string]any) {
	t.Helper()
	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	wanted := map[string][]string{
		"CreditCardCreateBadRequestError":  {"IDEMPOTENCY_KEY_INVALID", "IDEMPOTENCY_KEY_REQUIRED", "INVALID_REQUEST"},
		"CreditCardArchiveBadRequestError": {"IDEMPOTENCY_KEY_INVALID", "IDEMPOTENCY_KEY_REQUIRED", "INVALID_REQUEST"},
		"CreditCardNotFoundError":          {"CREDIT_CARD_NOT_FOUND"},
		"CreditCardArchiveConflictError":   {"CREDIT_CARD_ALREADY_ARCHIVED", "IDEMPOTENCY_KEY_REUSED"},
	}
	for name, codes := range wanted {
		schema := contractObject(t, schemas[name], name)
		assertClosedObject(t, schema, name, []string{"error"})
		errorBody := contractObject(t, contractAt(t, schema, "properties", "error"), name+".error")
		assertClosedObject(t, errorBody, name+".error", []string{"code", "message"})
		assertExactStrings(t, contractStrings(t, contractAt(t, errorBody, "properties", "code", "enum"), name+".codes"), codes, name+" codes")
	}
	responses := contractObject(t, contractAt(t, document, "components", "responses"), "responses")
	for response, schema := range map[string]string{
		"CreditCardCreateBadRequest":  "CreditCardCreateBadRequestError",
		"CreditCardArchiveBadRequest": "CreditCardArchiveBadRequestError",
		"CreditCardNotFound":          "CreditCardNotFoundError",
		"CreditCardArchiveConflict":   "CreditCardArchiveConflictError",
	} {
		value := contractObject(t, responses[response], response)
		assertRef(t, contractAt(t, value, "content", "application/json", "schema"), "schemas", schema)
		assertExactStrings(t, mapKeys(contractObject(t, value["headers"], response+".headers")),
			[]string{"Cache-Control", "X-Content-Type-Options"}, response+" headers")
	}
}
