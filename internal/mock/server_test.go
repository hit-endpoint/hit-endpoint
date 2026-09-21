package mock

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMockServerEndpoints(t *testing.T) {
	s := NewServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 1. Health
	res, err := http.Get(ts.URL + "/health")
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("health check failed: status=%v, err=%v", res.StatusCode, err)
	}

	// 2. Unauthed pets
	res, err = http.Get(ts.URL + "/pets")
	if err != nil || res.StatusCode != 401 {
		t.Fatalf("expected 401 unauthed, got %v", res.StatusCode)
	}

	// 3. Login
	loginBody := bytes.NewBufferString(`{"username":"admin","password":"hunter2"}`)
	res, err = http.Post(ts.URL+"/auth/login", "application/json", loginBody)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("login failed: %v", res.StatusCode)
	}
	var loginResp map[string]any
	_ = json.NewDecoder(res.Body).Decode(&loginResp)
	token, _ := loginResp["access_token"].(string)
	if token != Token {
		t.Fatalf("expected token %s, got %s", Token, token)
	}

	// 4. Authed GET /pets
	client := &http.Client{}
	req, _ := http.NewRequest("GET", ts.URL+"/pets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("authed get /pets failed: %v", res.StatusCode)
	}
	var listResp struct {
		Items []Pet `json:"items"`
		Total int   `json:"total"`
	}
	_ = json.NewDecoder(res.Body).Decode(&listResp)
	if listResp.Total != 2 {
		t.Fatalf("expected 2 pets, got %d", listResp.Total)
	}

	// 5. POST /pets (create)
	newPetJSON := bytes.NewBufferString(`{"name":"Buddy","kind":"dog","age":2}`)
	req, _ = http.NewRequest("POST", ts.URL+"/pets", newPetJSON)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("create pet failed: %v", res.StatusCode)
	}
	var created Pet
	_ = json.NewDecoder(res.Body).Decode(&created)
	if created.ID != 3 || created.Name != "Buddy" {
		t.Fatalf("unexpected created pet: %+v", created)
	}

	// 6. GET /pets/3
	req, _ = http.NewRequest("GET", ts.URL+"/pets/3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("get pet 3 failed: %v", res.StatusCode)
	}

	// 7. DELETE /pets/3
	req, _ = http.NewRequest("DELETE", ts.URL+"/pets/3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil || res.StatusCode != 204 {
		t.Fatalf("delete pet 3 failed: %v", res.StatusCode)
	}

	// 8. GET /pets/3 after delete -> 404
	req, _ = http.NewRequest("GET", ts.URL+"/pets/3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = client.Do(req)
	if err != nil || res.StatusCode != 404 {
		t.Fatalf("expected 404 for deleted pet, got %v", res.StatusCode)
	}
}
