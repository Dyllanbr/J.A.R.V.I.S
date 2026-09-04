package httpapi_test

import (
	"strings"
	"testing"
)

func TestScheduledCommitmentsOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.10.0" {
		t.Fatalf("info.version = %q, want 0.10.0", got)
	}
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	assertHTTPMethods(t, paths, "/v1/scheduled-commitments", []string{"get"})
	for path := range paths {
		if path != "/v1/scheduled-commitments" && strings.HasPrefix(path, "/v1/scheduled-commitments") {
			t.Fatalf("unexpected scheduled commitments path %s", path)
		}
	}

	operation := contractOperation(t, paths, "/v1/scheduled-commitments", "get")
	if got := contractString(t, operation["operationId"], "operationId"); got != "listScheduledCommitments" {
		t.Fatalf("operationId = %q, want listScheduledCommitments", got)
	}
	if _, exists := operation["requestBody"]; exists {
		t.Fatal("scheduled commitments GET must not declare requestBody")
	}
	refs, inline := operationParameters(t, operation)
	assertExactStrings(t, refs, nil, "parameter refs")
	if len(inline) != 1 {
		t.Fatalf("inline parameters = %d, want one evaluationDate", len(inline))
	}
	parameter := inline[0]
	if contractString(t, parameter["name"], "evaluationDate.name") != "evaluationDate" ||
		contractString(t, parameter["in"], "evaluationDate.in") != "query" ||
		!contractBool(t, parameter["required"], "evaluationDate.required") {
		t.Fatalf("invalid evaluationDate parameter: %#v", parameter)
	}
	assertRef(t, parameter["schema"], "schemas", "CivilDate")

	responses := contractObject(t, operation["responses"], "responses")
	assertExactStrings(t, mapKeys(responses), []string{"200", "400", "405", "500"}, "scheduled commitment statuses")
	success := contractObject(t, responses["200"], "scheduled commitment success")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", "ScheduledCommitmentListResponse")
	assertResponseHeaders(t, success, []string{"Cache-Control", "X-Content-Type-Options"})
	assertRef(t, responses["400"], "responses", "InvalidRequest")
	assertRef(t, responses["405"], "responses", "MethodNotAllowed")
	assertRef(t, responses["500"], "responses", "InternalError")

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	assertScheduledClosedObject(t, schemas, "ScheduledCommitmentListResponse", []string{"items"}, []string{"items"})
	assertScheduledClosedObject(t, schemas, "ScheduledCommitment", []string{"source", "sourceId", "sequence", "dueOn", "amount"}, []string{"amount", "dueOn", "sequence", "source", "sourceId"})
	list := contractObject(t, schemas["ScheduledCommitmentListResponse"], "ScheduledCommitmentListResponse")
	assertRef(t, contractAt(t, list, "properties", "items", "items"), "schemas", "ScheduledCommitment")
	item := contractObject(t, schemas["ScheduledCommitment"], "ScheduledCommitment")
	properties := contractObject(t, item["properties"], "ScheduledCommitment.properties")
	assertRef(t, properties["source"], "schemas", "ScheduledCommitmentSource")
	assertRef(t, properties["dueOn"], "schemas", "CivilDate")
	assertRef(t, properties["amount"], "schemas", "Money")
	if contractString(t, contractAt(t, properties, "sourceId", "type"), "sourceId.type") != "string" {
		t.Fatal("sourceId must be a string")
	}
	if contractString(t, contractAt(t, properties, "sequence", "type"), "sequence.type") != "integer" {
		t.Fatal("sequence must be an integer")
	}
	if minimum := contractAt(t, properties, "sequence", "minimum"); minimum != 1 {
		t.Fatalf("sequence.minimum = %#v, want 1", minimum)
	}
	source := contractObject(t, schemas["ScheduledCommitmentSource"], "ScheduledCommitmentSource")
	assertExactStrings(t, contractStrings(t, source["enum"], "ScheduledCommitmentSource.enum"), []string{"INSTALLMENT_PLAN", "RECURRENCE"}, "ScheduledCommitmentSource.enum")
}

func assertScheduledClosedObject(t *testing.T, schemas map[string]any, name string, properties, required []string) {
	t.Helper()
	schema := contractObject(t, schemas[name], name)
	if contractString(t, schema["type"], name+".type") != "object" {
		t.Fatalf("%s.type is not object", name)
	}
	additional, exists := schema["additionalProperties"]
	if !exists || additional != false {
		t.Fatalf("%s.additionalProperties = %#v, want explicit false", name, additional)
	}
	assertExactStrings(t, mapKeys(contractObject(t, schema["properties"], name+".properties")), properties, name+".properties")
	assertExactStrings(t, contractStrings(t, schema["required"], name+".required"), required, name+".required")
}

func assertResponseHeaders(t *testing.T, response map[string]any, wanted []string) {
	t.Helper()
	headers := contractObject(t, response["headers"], "response.headers")
	assertExactStrings(t, mapKeys(headers), wanted, "response.headers")
	for name, value := range headers {
		assertRef(t, value, "headers", headerComponentName(name))
	}
}
