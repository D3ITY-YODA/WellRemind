package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func TestParseMedTimes(t *testing.T) {
	cases := []struct {
		name     string
		input    []string
		expected []string
		wantErr  bool
	}{
		{name: "valid times", input: []string{"08:00", "12:30", "20:15"}, expected: []string{"08:00", "12:30", "20:15"}},
		{name: "duplicate times", input: []string{"08:00", "08:00", "12:00"}, expected: []string{"08:00", "12:00"}},
		{name: "invalid time", input: []string{"08:00", "25:00"}, wantErr: true},
		{name: "empty entries", input: []string{"08:00", ""}, expected: []string{"08:00"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMedTimes(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseMedTimes() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d times, got %d", len(tc.expected), len(got))
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("expected %q, got %q", tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestCheckWater(t *testing.T) {
	user := &UserConfig{Subscription: webpush.Subscription{Endpoint: "https://example.com"}, WaterInterval: 30, LastWater: time.Now().Add(-35 * time.Minute)}
	called := false
	pushSender = func(sub webpush.Subscription, title, body string) {
		called = true
		if title == "" || body == "" {
			t.Fatal("push payload should contain title and body")
		}
	}
	defer func() { pushSender = sendPush }()

	if !checkWater(user, time.Now()) {
		t.Fatal("expected checkWater to return true")
	}
	if !called {
		t.Fatal("expected pushSender to be called")
	}
}

func TestCheckMedications(t *testing.T) {
	now := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	user := &UserConfig{Subscription: webpush.Subscription{Endpoint: "https://example.com"}, MedTimes: []string{"09:00"}, NotifiedEvents: make(map[string]string)}
	called := false
	pushSender = func(sub webpush.Subscription, title, body string) {
		called = true
	}
	defer func() { pushSender = sendPush }()

	if !checkMedications(user, now) {
		t.Fatal("expected checkMedications to return true")
	}
	if !called {
		t.Fatal("expected pushSender to be called")
	}
	if _, ok := user.NotifiedEvents["med 2026-08-08 09:00"]; !ok {
		t.Fatal("expected notification record to be stored")
	}
}

func TestEvaluateSymptoms(t *testing.T) {
	response := evaluateSymptoms([]string{"fever", "cough", "sore throat"})
	if response.Condition != "Flu-like illness" {
		t.Fatalf("expected flu-like illness, got %q", response.Condition)
	}
	if response.Advice == "" {
		t.Fatal("expected advice text")
	}
}

func TestHandleCheckSymptoms(t *testing.T) {
	requestBody, _ := json.Marshal(map[string]any{
		"endpoint": "https://example.com/1",
		"symptoms": []string{"dizziness", "thirst"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/check-symptoms", bytes.NewReader(requestBody))
	rw := httptest.NewRecorder()

	users = map[string]*UserConfig{
		"https://example.com/1": {Subscription: webpush.Subscription{Endpoint: "https://example.com/1"}, NotifiedEvents: make(map[string]string)},
	}

	handleCheckSymptoms(rw, req)
	res := rw.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var body struct {
		Condition string         `json:"condition"`
		Advice    string         `json:"advice"`
		Note      string         `json:"note"`
		History   []SymptomEntry `json:"history"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Condition != "Possible dehydration" {
		t.Fatalf("expected dehydration, got %q", body.Condition)
	}
	if len(body.History) != 1 {
		t.Fatalf("expected history length 1, got %d", len(body.History))
	}
}

func TestCheckCheckin(t *testing.T) {
	now := time.Date(2026, 8, 8, 11, 30, 0, 0, time.UTC)
	user := &UserConfig{Subscription: webpush.Subscription{Endpoint: "https://example.com"}, CheckinTime: "11:30", NotifiedEvents: make(map[string]string)}
	called := false
	pushSender = func(sub webpush.Subscription, title, body string) {
		called = true
		if title == "" || body == "" {
			t.Fatal("push payload should contain title and body")
		}
	}
	defer func() { pushSender = sendPush }()

	if !checkCheckin(user, now) {
		t.Fatal("expected checkCheckin to return true")
	}
	if !called {
		t.Fatal("expected pushSender to be called")
	}
	if _, ok := user.NotifiedEvents["checkin 2026-08-08 11:30"]; !ok {
		t.Fatal("expected checkin notification record to be stored")
	}
}
