package httpapi_test

import "testing"

func TestAuthenticationSessionOpenAPIContract(t *testing.T) {
	document := loadOpenAPIContract(t)
	if got := contractString(t, contractAt(t, document, "info", "version"), "info.version"); got != "0.15.0" {
		t.Fatalf("info.version = %q, want 0.15.0", got)
	}

	issue := contractObject(t, contractAt(t, document, "paths", "/v1/auth/sessions", "post"), "paths./v1/auth/sessions.post")
	if got := contractString(t, issue["operationId"], "operationId"); got != "issueAuthenticationSession" {
		t.Fatalf("operationId = %q", got)
	}
	parameters := contractArray(t, issue["parameters"], "parameters")
	if len(parameters) != 1 || contractString(t, contractObject(t, parameters[0], "bootstrap parameter")["$ref"], "parameter ref") != "#/components/parameters/SessionBootstrapToken" {
		t.Fatalf("bootstrap parameters = %#v", parameters)
	}
	assertRef(t, contractAt(t, issue, "requestBody", "content", "application/json", "schema"), "schemas", "SessionIssueRequest")
	assertRef(t, contractAt(t, issue, "responses", "201", "content", "application/json", "schema"), "schemas", "SessionIssueResponse")
	assertRef(t, contractAt(t, issue, "responses", "401"), "responses", "Unauthenticated")

	revoke := contractObject(t, contractAt(t, document, "paths", "/v1/auth/sessions/{sessionID}", "delete"), "paths./v1/auth/sessions/{sessionID}.delete")
	if got := contractString(t, contractAt(t, revoke, "operationId"), "operationId"); got != "revokeAuthenticationSession" {
		t.Fatalf("operationId = %q", got)
	}
	assertRef(t, contractAt(t, revoke, "responses", "204", "headers", "Cache-Control"), "headers", "CacheControl")
	assertRef(t, contractAt(t, revoke, "responses", "404"), "responses", "SessionNotFound")
	security := contractArray(t, revoke["security"], "security")
	if len(security) != 1 {
		t.Fatalf("security = %#v", security)
	}

	bootstrap := contractObject(t, contractAt(t, document, "components", "parameters", "SessionBootstrapToken"), "SessionBootstrapToken")
	if contractString(t, bootstrap["name"], "bootstrap name") != "X-JARVIS-Session-Bootstrap" || contractString(t, bootstrap["in"], "bootstrap location") != "header" {
		t.Fatalf("bootstrap parameter = %#v", bootstrap)
	}
	assertClosedSchema(t, contractAt(t, document, "components", "schemas", "SessionIssueRequest"), "SessionIssueRequest")
	assertClosedSchema(t, contractAt(t, document, "components", "schemas", "SessionIssueResponse"), "SessionIssueResponse")
}

func assertClosedSchema(t *testing.T, value any, name string) {
	t.Helper()
	schema := contractObject(t, value, name)
	closed, ok := schema["additionalProperties"].(bool)
	if !ok || closed {
		t.Fatalf("%s is not closed", name)
	}
}
