package httpapi_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestSafeAvailableOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.11.0" {
		t.Fatalf("info.version = %q, want 0.11.0", got)
	}
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	const path = "/v1/safe-available"
	assertHTTPMethods(t, paths, path, []string{"get"})
	for candidate := range paths {
		if candidate != path && strings.HasPrefix(candidate, "/v1/safe-available") {
			t.Fatalf("unexpected safe available path %s", candidate)
		}
	}
	operation := contractOperation(t, paths, path, "get")
	if got := contractString(t, operation["operationId"], "operationId"); got != "getSafeAvailable" {
		t.Fatalf("operationId = %q, want getSafeAvailable", got)
	}
	if _, exists := operation["requestBody"]; exists {
		t.Fatal("safe available GET must not declare requestBody")
	}
	refs, inline := operationParameters(t, operation)
	assertExactStrings(t, refs, nil, "safe available parameter refs")
	if len(inline) != 2 {
		t.Fatalf("inline parameters = %d, want periodStart and periodEnd", len(inline))
	}
	parameterNames := make(map[string]struct{}, len(inline))
	for _, parameter := range inline {
		name := contractString(t, parameter["name"], "safe available parameter name")
		parameterNames[name] = struct{}{}
		if contractString(t, parameter["in"], name+".in") != "query" || !contractBool(t, parameter["required"], name+".required") {
			t.Fatalf("invalid %s parameter: %#v", name, parameter)
		}
		assertRef(t, parameter["schema"], "schemas", "CivilDate")
	}
	for _, name := range []string{"periodStart", "periodEnd"} {
		if _, ok := parameterNames[name]; !ok {
			t.Fatalf("missing %s query parameter", name)
		}
	}

	responses := contractObject(t, operation["responses"], "safe available responses")
	assertExactStrings(t, mapKeys(responses), []string{"200", "400", "405", "500"}, "safe available statuses")
	success := contractObject(t, responses["200"], "safe available success")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", "SafeAvailableResponse")
	assertResponseHeaders(t, success, []string{"Cache-Control", "X-Content-Type-Options"})
	assertRef(t, responses["400"], "responses", "InvalidRequest")
	assertRef(t, responses["405"], "responses", "MethodNotAllowed")
	assertRef(t, responses["500"], "responses", "InternalError")

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	assertClosedObject(t, schemas["SafeAvailableResponse"].(map[string]any), "SafeAvailableResponse", []string{
		"periodStart", "periodEnd", "availableBalance", "totalConfirmedIncome", "totalConfirmedExpense", "totalConfirmedCommitments", "finalAmount", "breakdown", "missingData",
	})
	responseProperties := contractObject(t, contractAt(t, schemas["SafeAvailableResponse"].(map[string]any), "properties"), "SafeAvailableResponse.properties")
	assertExactStrings(t, mapKeys(responseProperties), []string{
		"periodStart", "periodEnd", "availableBalance", "totalConfirmedIncome", "totalConfirmedExpense", "totalConfirmedCommitments", "finalAmount", "breakdown", "missingData",
	}, "SafeAvailableResponse properties")
	assertRef(t, responseProperties["periodStart"], "schemas", "CivilDate")
	assertRef(t, responseProperties["periodEnd"], "schemas", "CivilDate")
	for _, name := range []string{"availableBalance", "totalConfirmedIncome", "totalConfirmedExpense", "totalConfirmedCommitments", "finalAmount"} {
		assertRef(t, responseProperties[name], "schemas", "SafeAvailableAmount")
	}
	assertRef(t, contractAt(t, responseProperties, "breakdown", "items"), "schemas", "SafeAvailableBreakdown")
	assertRef(t, contractAt(t, responseProperties, "missingData", "items"), "schemas", "SafeAvailableMissingData")

	amount := schemas["SafeAvailableAmount"].(map[string]any)
	assertClosedObject(t, amount, "SafeAvailableAmount", []string{"minor", "currency"})
	amountProperties := contractObject(t, amount["properties"], "SafeAvailableAmount.properties")
	assertExactStrings(t, mapKeys(amountProperties), []string{"minor", "currency"}, "SafeAvailableAmount properties")
	if contractString(t, contractAt(t, amountProperties, "minor", "type"), "SafeAvailableAmount.minor.type") != "integer" || contractString(t, contractAt(t, amountProperties, "minor", "format"), "SafeAvailableAmount.minor.format") != "int64" {
		t.Fatal("SafeAvailableAmount.minor must be int64")
	}
	if got := fmt.Sprint(amountProperties["minor"].(map[string]any)["minimum"]); got != fmt.Sprint(int64(math.MinInt64)) {
		t.Fatalf("SafeAvailableAmount.minor.minimum = %s, want %d", got, math.MinInt64)
	}
	assertConst(t, amountProperties["currency"], "BRL")

	breakdown := schemas["SafeAvailableBreakdown"].(map[string]any)
	assertClosedObject(t, breakdown, "SafeAvailableBreakdown", []string{"kind", "sourceId", "sequence", "dueOn", "amount"})
	breakdownProperties := contractObject(t, breakdown["properties"], "SafeAvailableBreakdown.properties")
	assertRef(t, breakdownProperties["kind"], "schemas", "SafeAvailableBreakdownKind")
	assertRef(t, breakdownProperties["dueOn"], "schemas", "CivilDate")
	assertRef(t, breakdownProperties["amount"], "schemas", "SafeAvailableAmount")
	if contractString(t, contractAt(t, breakdownProperties, "sourceId", "type"), "sourceId.type") != "string" || contractString(t, contractAt(t, breakdownProperties, "sequence", "type"), "sequence.type") != "integer" {
		t.Fatal("SafeAvailableBreakdown sourceId/sequence types are invalid")
	}
	kind := schemas["SafeAvailableBreakdownKind"].(map[string]any)
	assertExactStrings(t, contractStrings(t, kind["enum"], "SafeAvailableBreakdownKind.enum"), []string{"AVAILABLE_BALANCE", "INCOME", "EXPENSE", "COMMITMENT"}, "SafeAvailableBreakdownKind.enum")
	missing := schemas["SafeAvailableMissingData"].(map[string]any)
	assertExactStrings(t, contractStrings(t, missing["enum"], "SafeAvailableMissingData.enum"), []string{"BUDGET", "CONFIRMED_INCOME", "CONFIRMED_EXPENSE", "COMMITMENTS"}, "SafeAvailableMissingData.enum")
}
