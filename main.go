package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// UserConfig holds a subscriber's push subscription and reminder preferences.
type UserConfig struct {
	Subscription  webpush.Subscription `json:"subscription"`
	WaterInterval int                  `json:"waterInterval"` // minutes between water reminders
	MedTimes      []string             `json:"medTimes"`      // medication times as "HH:MM"
	LastWater     time.Time            `json:"-"`             // tracks last water notification
	NotifiedMeds  map[string]string    `json:"-"`             // tracks med notifs per day: "YYYY-MM-DD HH:MM"
}

var (
	mu           sync.Mutex
	users        = make(map[string]*UserConfig) // keyed by push endpoint
	vapidPrivate string
	vapidPublic  string
)