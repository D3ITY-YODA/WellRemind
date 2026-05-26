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

func main() {
	// Generate VAPID keys (used to authenticate push notifications)
	var err error
	vapidPrivate, vapidPublic, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatal("Failed to generate VAPID keys:", err)
	}

	mux := http.NewServeMux()

	// Serve static files (HTML, JS, service worker)
	mux.Handle("/", http.FileServer(http.Dir("static")))

	// API routes
	mux.HandleFunc("/api/vapid-key", handleVapidKey)
	mux.HandleFunc("/api/subscribe", handleSubscribe)
	mux.HandleFunc("/api/unsubscribe", handleUnsubscribe)

	// Start the background reminder scheduler
	go runScheduler()

	fmt.Println("🚀 WellRemind running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

//handleVapidKey returns the server's VAPID public key to the browser.
//The browser needs this to set up a push subscription.
func handleVapidKey(w http.ResponseWriter, r *http.Request){
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"publicKey": vapidPublic})
}

//handleSUbscribe registers a user's push subscription and reminder settings.
func handleSubscribe(w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config UserConfig
	if err :=json.NewDecoder(r.Body).Decode(&config); err != nil{
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if config.Subscription.Endpoint == ""{
		http.Error(w, "Missing push subscription endpoint", http.StatusBadRequest)
		return
	}

	mu.Lock()
	config.LastWater = time.Now()
	config.NotifiedMeds = make(map[string]string)
	users[config.Subscription.Endpoint] = &config
	mu.Unlock()

	log.Printf("✅ New subscriber. Water every %d min. Meds at: %v\n",
		config.WaterInterval, config.MedTimes)


}
