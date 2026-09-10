package httpapi_test

import (
	"fmt"
	"testing"
)

func TestFinancialGoalsOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.15.0" {
		t.Fatalf("info.version = %q, want 0.15.0", got)
	}
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	assertHTTPMethods(t, paths, "/v1/financial-goals", []string{"get"})
	assertHTTPMethods(t, paths, "/v1/financial-goals/{goalId}", []string{"put"})
	assertHTTPMethods(t, paths, "/v1/protected-values/{valueId}", []string{"put"})

	list := contractOperation(t, paths, "/v1/financial-goals", "get")
	if _, exists := list["requestBody"]; exists {
		t.Fatal("financial goals GET must not declare requestBody")
	}
	if _, exists := list["security"]; exists {
		t.Fatal("financial goals GET must not define client security")
	}
	assertExactStrings(t, mapKeys(contractObject(t, list["responses"], "financial goals GET responses")), []string{"200", "400", "405", "500"}, "financial goals GET statuses")
	assertRef(t, contractAt(t, list, "responses", "200", "content", "application/json", "schema"), "schemas", "FinancialGoalsResponse")

	for _, testCase := range []struct {
		path      string
		parameter string
		schema    string
		request   string
		response  string
	}{
		{path: "/v1/financial-goals/{goalId}", parameter: "goalId", schema: "FinancialGoalId", request: "FinancialGoalRequest", response: "FinancialGoal"},
		{path: "/v1/protected-values/{valueId}", parameter: "valueId", schema: "ProtectedValueId", request: "ProtectedValueRequest", response: "ProtectedValue"},
	} {
		operation := contractOperation(t, paths, testCase.path, "put")
		refs, inline := operationParameters(t, operation)
		assertExactStrings(t, refs, nil, testCase.path+" parameter refs")
		if len(inline) != 1 || contractString(t, inline[0]["name"], testCase.path+" parameter name") != testCase.parameter || contractString(t, inline[0]["in"], testCase.path+" parameter in") != "path" || !contractBool(t, inline[0]["required"], testCase.path+" parameter required") {
			t.Fatalf("invalid path parameter for %s: %#v", testCase.path, inline)
		}
		assertRef(t, inline[0]["schema"], "schemas", testCase.schema)
		body := contractObject(t, operation["requestBody"], testCase.path+" request body")
		if !contractBool(t, body["required"], testCase.path+" request body required") {
			t.Fatal("declaration PUT request body must be required")
		}
		assertRef(t, contractAt(t, body, "content", "application/json", "schema"), "schemas", testCase.request)
		responses := contractObject(t, operation["responses"], testCase.path+" responses")
		assertExactStrings(t, mapKeys(responses), []string{"200", "400", "405", "500"}, testCase.path+" statuses")
		assertRef(t, contractAt(t, responses, "200", "content", "application/json", "schema"), "schemas", testCase.response)
	}

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	assertClosedObject(t, schemas["FinancialGoal"].(map[string]any), "FinancialGoal", []string{"id", "title", "targetAmount"})
	assertClosedObject(t, schemas["ProtectedValue"].(map[string]any), "ProtectedValue", []string{"id", "label", "amount"})
	assertClosedObject(t, schemas["FinancialGoalsResponse"].(map[string]any), "FinancialGoalsResponse", []string{"goals", "protectedValues"})
	assertClosedObject(t, schemas["FinancialGoalRequest"].(map[string]any), "FinancialGoalRequest", []string{"title", "targetAmount"})
	assertClosedObject(t, schemas["ProtectedValueRequest"].(map[string]any), "ProtectedValueRequest", []string{"label", "amount"})
	assertRef(t, contractAt(t, schemas["FinancialGoal"].(map[string]any), "properties", "targetAmount"), "schemas", "FinancialGoalAmount")
	assertRef(t, contractAt(t, schemas["ProtectedValue"].(map[string]any), "properties", "amount"), "schemas", "ProtectedValueAmount")
	assertRef(t, contractAt(t, schemas["FinancialGoalRequest"].(map[string]any), "properties", "targetAmount"), "schemas", "FinancialGoalAmount")
	assertRef(t, contractAt(t, schemas["ProtectedValueRequest"].(map[string]any), "properties", "amount"), "schemas", "ProtectedValueAmount")
	assertRef(t, contractAt(t, schemas["FinancialGoalsResponse"].(map[string]any), "properties", "goals", "items"), "schemas", "FinancialGoal")
	assertRef(t, contractAt(t, schemas["FinancialGoalsResponse"].(map[string]any), "properties", "protectedValues", "items"), "schemas", "ProtectedValue")

	for _, name := range []string{"FinancialGoalAmount", "ProtectedValueAmount"} {
		amount := schemas[name].(map[string]any)
		assertClosedObject(t, amount, name, []string{"minor", "currency"})
		assertConst(t, contractAt(t, amount, "properties", "currency"), "BRL")
	}
	if got := schemas["FinancialGoalAmount"].(map[string]any)["properties"].(map[string]any)["minor"].(map[string]any)["minimum"]; fmt.Sprint(got) != "1" {
		t.Fatalf("FinancialGoalAmount.minor.minimum = %#v, want 1", got)
	}
	if got := schemas["ProtectedValueAmount"].(map[string]any)["properties"].(map[string]any)["minor"].(map[string]any)["minimum"]; fmt.Sprint(got) != "0" {
		t.Fatalf("ProtectedValueAmount.minor.minimum = %#v, want 0", got)
	}
}
