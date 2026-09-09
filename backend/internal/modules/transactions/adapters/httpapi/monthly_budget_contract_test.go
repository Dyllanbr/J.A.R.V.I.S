package httpapi_test

import (
	"fmt"
	"testing"
)

func TestMonthlyBudgetOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.13.0" {
		t.Fatalf("info.version = %q, want 0.13.0", got)
	}
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	const path = "/v1/monthly-budgets/{month}"
	assertHTTPMethods(t, paths, path, []string{"get", "put"})
	for _, method := range []string{"get", "put"} {
		operation := contractOperation(t, paths, path, method)
		refs, inline := operationParameters(t, operation)
		assertExactStrings(t, refs, nil, method+" parameter refs")
		if len(inline) != 1 {
			t.Fatalf("%s parameters = %d, want one path parameter", method, len(inline))
		}
		parameter := inline[0]
		if contractString(t, parameter["name"], method+" parameter name") != "month" || contractString(t, parameter["in"], method+" parameter in") != "path" || !contractBool(t, parameter["required"], method+" parameter required") {
			t.Fatalf("%s month parameter = %#v", method, parameter)
		}
		assertRef(t, parameter["schema"], "schemas", "CivilMonth")
		if _, exists := operation["security"]; exists {
			t.Fatalf("%s must not define client security", method)
		}
		if method == "get" {
			if _, exists := operation["requestBody"]; exists {
				t.Fatal("GET monthly budget must not declare requestBody")
			}
		} else {
			body := contractObject(t, operation["requestBody"], method+" requestBody")
			if !contractBool(t, body["required"], method+" requestBody.required") {
				t.Fatal("PUT monthly budget body must be required")
			}
			assertRef(t, contractAt(t, body, "content", "application/json", "schema"), "schemas", "MonthlyBudgetRequest")
		}
		responses := contractObject(t, operation["responses"], method+" responses")
		wanted := []string{"200", "400", "405", "500"}
		if method == "get" {
			wanted = []string{"200", "400", "404", "405", "500"}
		}
		assertExactStrings(t, mapKeys(responses), wanted, method+" statuses")
		success := contractObject(t, responses["200"], method+" success")
		assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", "MonthlyBudget")
		assertResponseHeaders(t, success, []string{"Cache-Control", "X-Content-Type-Options"})
		assertRef(t, responses["400"], "responses", "InvalidRequest")
		assertRef(t, responses["405"], "responses", "MethodNotAllowed")
		assertRef(t, responses["500"], "responses", "InternalError")
		if method == "get" {
			assertRef(t, responses["404"], "responses", "MonthlyBudgetNotFound")
		}
	}

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	civilMonth := schemas["CivilMonth"].(map[string]any)
	if contractString(t, civilMonth["type"], "CivilMonth.type") != "string" || contractString(t, civilMonth["pattern"], "CivilMonth.pattern") != "^(?!0000-)[0-9]{4}-(0[1-9]|1[0-2])$" {
		t.Fatal("CivilMonth pattern is not strict YYYY-MM")
	}
	assertClosedObject(t, schemas["MonthlyBudgetAmount"].(map[string]any), "MonthlyBudgetAmount", []string{"minor", "currency"})
	amountProperties := contractObject(t, schemas["MonthlyBudgetAmount"].(map[string]any)["properties"], "MonthlyBudgetAmount.properties")
	if contractString(t, contractAt(t, amountProperties, "minor", "type"), "MonthlyBudgetAmount.minor.type") != "integer" || contractString(t, contractAt(t, amountProperties, "minor", "format"), "MonthlyBudgetAmount.minor.format") != "int64" || fmt.Sprint(amountProperties["minor"].(map[string]any)["minimum"]) != "0" {
		t.Fatal("MonthlyBudgetAmount.minor must be int64 with minimum 0")
	}
	assertConst(t, amountProperties["currency"], "BRL")
	assertClosedObject(t, schemas["MonthlyBudgetRequest"].(map[string]any), "MonthlyBudgetRequest", []string{"amount"})
	assertRef(t, contractAt(t, schemas["MonthlyBudgetRequest"].(map[string]any), "properties", "amount"), "schemas", "MonthlyBudgetAmount")
	assertClosedObject(t, schemas["MonthlyBudget"].(map[string]any), "MonthlyBudget", []string{"month", "amount"})
	assertRef(t, contractAt(t, schemas["MonthlyBudget"].(map[string]any), "properties", "month"), "schemas", "CivilMonth")
	assertRef(t, contractAt(t, schemas["MonthlyBudget"].(map[string]any), "properties", "amount"), "schemas", "MonthlyBudgetAmount")
	assertClosedObject(t, schemas["MonthlyBudgetNotFoundError"].(map[string]any), "MonthlyBudgetNotFoundError", []string{"error"})
}
