package httpapi_test

import (
	"strings"
	"testing"
)

func TestCardStatementOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.10.0" {
		t.Fatalf("info.version = %q, want 0.10.0", got)
	}

	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	const path = "/v1/credit-cards/{creditCardId}/statements/{statementDueOn}"
	assertHTTPMethods(t, paths, path, []string{"get"})
	for candidate := range paths {
		if candidate != path && (candidate == "/v1/credit-cards" || strings.HasPrefix(candidate, "/v1/credit-cards/")) {
			t.Fatalf("unexpected statement path %s", candidate)
		}
	}

	operation := contractOperation(t, paths, path, "get")
	if got := contractString(t, operation["operationId"], "operationId"); got != "getCreditCardStatement" {
		t.Fatalf("operationId = %q, want getCreditCardStatement", got)
	}
	if _, exists := operation["requestBody"]; exists {
		t.Fatal("statement GET must not declare requestBody")
	}
	refs, inline := operationParameters(t, operation)
	assertExactStrings(t, refs, nil, "statement parameter refs")
	if len(inline) != 2 {
		t.Fatalf("inline parameters = %d, want creditCardId and statementDueOn", len(inline))
	}
	parameters := make(map[string]map[string]any, len(inline))
	for _, parameter := range inline {
		name := contractString(t, parameter["name"], "statement parameter name")
		parameters[name] = parameter
		if contractString(t, parameter["in"], name+".in") != "path" || !contractBool(t, parameter["required"], name+".required") {
			t.Fatalf("%s is not a required path parameter: %#v", name, parameter)
		}
	}
	if _, ok := parameters["creditCardId"]; !ok {
		t.Fatal("creditCardId path parameter is missing")
	}
	if _, ok := parameters["statementDueOn"]; !ok {
		t.Fatal("statementDueOn path parameter is missing")
	}
	assertRef(t, parameters["creditCardId"]["schema"], "schemas", "CreditCardId")
	assertRef(t, parameters["statementDueOn"]["schema"], "schemas", "CivilDate")

	responses := contractObject(t, operation["responses"], "statement responses")
	assertExactStrings(t, mapKeys(responses), []string{"200", "400", "404", "405", "500"}, "statement statuses")
	success := contractObject(t, responses["200"], "statement success")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", "CardStatement")
	assertResponseHeaders(t, success, []string{"Cache-Control", "X-Content-Type-Options"})
	assertRef(t, responses["400"], "responses", "InvalidRequest")
	assertRef(t, responses["404"], "responses", "CreditCardNotFound")
	assertRef(t, responses["405"], "responses", "MethodNotAllowed")
	assertRef(t, responses["500"], "responses", "InternalError")

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	statement := contractObject(t, schemas["CardStatement"], "CardStatement")
	assertClosedObject(t, statement, "CardStatement", []string{"creditCardId", "statementDueOn", "totalAmount", "lines"})
	assertExactStrings(t, mapKeys(contractObject(t, statement["properties"], "CardStatement.properties")), []string{"creditCardId", "statementDueOn", "totalAmount", "lines"}, "CardStatement properties")
	statementProperties := contractObject(t, statement["properties"], "CardStatement.properties")
	assertRef(t, statementProperties["creditCardId"], "schemas", "CreditCardId")
	assertRef(t, statementProperties["statementDueOn"], "schemas", "CivilDate")
	assertRef(t, statementProperties["totalAmount"], "schemas", "CardStatementTotalAmount")
	assertRef(t, contractAt(t, statementProperties, "lines", "items"), "schemas", "CardStatementLine")

	totalAmount := contractObject(t, schemas["CardStatementTotalAmount"], "CardStatementTotalAmount")
	assertClosedObject(t, totalAmount, "CardStatementTotalAmount", []string{"minor", "currency"})
	totalAmountProperties := contractObject(t, totalAmount["properties"], "CardStatementTotalAmount.properties")
	assertExactStrings(t, mapKeys(totalAmountProperties), []string{"minor", "currency"}, "CardStatementTotalAmount properties")
	if contractString(t, contractAt(t, totalAmountProperties, "minor", "type"), "CardStatementTotalAmount.minor.type") != "integer" {
		t.Fatal("CardStatementTotalAmount.minor must be an integer")
	}
	if contractString(t, contractAt(t, totalAmountProperties, "minor", "format"), "CardStatementTotalAmount.minor.format") != "int64" {
		t.Fatal("CardStatementTotalAmount.minor must use int64")
	}
	if minimum := contractAt(t, totalAmountProperties, "minor", "minimum"); minimum != 0 {
		t.Fatalf("CardStatementTotalAmount.minor.minimum = %#v, want 0", minimum)
	}
	if contractString(t, contractAt(t, totalAmountProperties, "currency", "type"), "CardStatementTotalAmount.currency.type") != "string" {
		t.Fatal("CardStatementTotalAmount.currency must be a string")
	}
	assertConst(t, totalAmountProperties["currency"], "BRL")

	money := contractObject(t, schemas["Money"], "Money")
	assertClosedObject(t, money, "Money", []string{"minor", "currency"})
	moneyProperties := contractObject(t, money["properties"], "Money.properties")
	assertExactStrings(t, mapKeys(moneyProperties), []string{"minor", "currency"}, "Money properties")
	if minimum := contractAt(t, moneyProperties, "minor", "minimum"); minimum != 1 {
		t.Fatalf("Money.minor.minimum = %#v, want 1", minimum)
	}
	assertConst(t, moneyProperties["currency"], "BRL")

	line := contractObject(t, schemas["CardStatementLine"], "CardStatementLine")
	assertClosedObject(t, line, "CardStatementLine", []string{"expenseId", "description", "amount", "occurredAt", "purchaseMode"})
	assertExactStrings(t, mapKeys(contractObject(t, line["properties"], "CardStatementLine.properties")), []string{"expenseId", "description", "amount", "occurredAt", "purchaseMode", "installmentNumber", "installmentCount"}, "CardStatementLine properties")
	lineProperties := contractObject(t, line["properties"], "CardStatementLine.properties")
	assertRef(t, lineProperties["amount"], "schemas", "Money")
	assertRef(t, lineProperties["occurredAt"], "schemas", "CivilDate")
	assertRef(t, lineProperties["purchaseMode"], "schemas", "CardPurchaseMode")
	if contractString(t, contractAt(t, lineProperties, "expenseId", "type"), "CardStatementLine.expenseId.type") != "string" {
		t.Fatal("CardStatementLine.expenseId must be a string")
	}
	for _, name := range []string{"installmentNumber", "installmentCount"} {
		if contractString(t, contractAt(t, lineProperties, name, "type"), "CardStatementLine."+name+".type") != "integer" {
			t.Fatalf("CardStatementLine.%s must be an integer", name)
		}
	}
}
