package httpapi_test

import (
	"testing"
)

func TestPurchaseSimulationOpenAPIContractStructurallyMatchesRuntime(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.13.0" {
		t.Fatalf("info.version = %q, want 0.13.0", got)
	}
	paths := contractObject(t, contractAt(t, document, "paths"), "paths")
	const path = "/v1/purchase-simulations"
	assertHTTPMethods(t, paths, path, []string{"post"})
	operation := contractOperation(t, paths, path, "post")
	if got := contractString(t, operation["operationId"], "operationId"); got != "simulatePurchase" {
		t.Fatalf("operationId = %q, want simulatePurchase", got)
	}
	if _, exists := operation["security"]; exists {
		t.Fatal("purchase simulation must use the server-side owner context")
	}
	body := contractObject(t, operation["requestBody"], "purchase simulation request body")
	if !contractBool(t, body["required"], "purchase simulation request body.required") {
		t.Fatal("purchase simulation request body must be required")
	}
	assertRef(t, contractAt(t, body, "content", "application/json", "schema"), "schemas", "PurchaseSimulationRequest")
	responses := contractObject(t, operation["responses"], "purchase simulation responses")
	assertExactStrings(t, mapKeys(responses), []string{"200", "400", "404", "405", "500"}, "purchase simulation statuses")
	success := contractObject(t, responses["200"], "purchase simulation success")
	assertRef(t, contractAt(t, success, "content", "application/json", "schema"), "schemas", "PurchaseSimulationResponse")
	assertResponseHeaders(t, success, []string{"Cache-Control", "X-Content-Type-Options"})
	assertRef(t, responses["400"], "responses", "InvalidRequest")
	assertRef(t, responses["404"], "responses", "CreditCardNotFound")
	assertRef(t, responses["405"], "responses", "MethodNotAllowed")
	assertRef(t, responses["500"], "responses", "InternalError")

	schemas := contractObject(t, contractAt(t, document, "components", "schemas"), "schemas")
	request := contractObject(t, schemas["PurchaseSimulationRequest"], "PurchaseSimulationRequest")
	assertClosedObject(t, request, "PurchaseSimulationRequest", []string{"creditCardId", "amount", "purchaseOn", "purchaseMode", "periodStart", "periodEnd"})
	requestProperties := contractObject(t, request["properties"], "PurchaseSimulationRequest.properties")
	assertRef(t, requestProperties["creditCardId"], "schemas", "CreditCardId")
	assertRef(t, requestProperties["amount"], "schemas", "Money")
	assertRef(t, requestProperties["purchaseOn"], "schemas", "CivilDate")
	assertRef(t, requestProperties["purchaseMode"], "schemas", "PurchaseSimulationMode")
	assertRef(t, requestProperties["periodStart"], "schemas", "CivilDate")
	assertRef(t, requestProperties["periodEnd"], "schemas", "CivilDate")

	response := contractObject(t, schemas["PurchaseSimulationResponse"], "PurchaseSimulationResponse")
	assertClosedObject(t, response, "PurchaseSimulationResponse", []string{"creditCardId", "purchaseOn", "purchaseMode", "periodStart", "periodEnd", "baseline", "projected", "impact", "hypotheticalCommitments", "assumptions"})
	responseProperties := contractObject(t, response["properties"], "PurchaseSimulationResponse.properties")
	assertRef(t, responseProperties["baseline"], "schemas", "SafeAvailableResponse")
	assertRef(t, responseProperties["projected"], "schemas", "SafeAvailableResponse")
	assertRef(t, responseProperties["impact"], "schemas", "SafeAvailableAmount")
	assertRef(t, contractAt(t, responseProperties, "hypotheticalCommitments", "items"), "schemas", "SafeAvailableBreakdown")
	assertRef(t, contractAt(t, responseProperties, "assumptions", "items"), "schemas", "PurchaseSimulationAssumption")
	assertExactStrings(t, contractStrings(t, schemas["PurchaseSimulationMode"].(map[string]any)["enum"], "PurchaseSimulationMode.enum"), []string{"ONE_TIME", "INSTALLMENT"}, "PurchaseSimulationMode.enum")
	assertExactStrings(t, contractStrings(t, schemas["PurchaseSimulationAssumption"].(map[string]any)["enum"], "PurchaseSimulationAssumption.enum"), []string{"NOT_PERSISTED", "NO_EXPENSE_CREATED"}, "PurchaseSimulationAssumption.enum")
}
