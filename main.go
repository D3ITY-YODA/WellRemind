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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "subscribed"})
}

// handleUnsubscribe removes a user's subscription.
func handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	mu.Lock()
	delete(users, body.Endpoint)
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unsubscribed"})
}

// runScheduler ticks every minute and sends reminders as needed.
func runScheduler() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for now := range ticker.C {
		mu.Lock()
		for _, user := range users {
			checkWater(user, now)
			checkMedications(user, now)
		}
		mu.Unlock()
	}
}

func checkWater(user *UserConfig, now time.Time) {
	if user.WaterInterval <= 0 {
		return
	}
	if now.Sub(user.LastWater).Minutes() >= float64(user.WaterInterval) {
		sendPush(user.Subscription, "💧 Time to Hydrate!", "Drink a glass of water — your body will thank you.")
		user.LastWater = now
	}
}

func checkMedications(user *UserConfig, now time.Time) {
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

	for _, medTime := range user.MedTimes {
		if medTime != currentTime {
			continue
		}
		key := today + " " + medTime
		if _, alreadySent := user.NotifiedMeds[key]; alreadySent {
			continue
		}
		sendPush(user.Subscription, "💊 Medication Reminder", fmt.Sprintf("Time to take your %s medication.", medTime))
		user.NotifiedMeds[key] = "sent"
	}
}

/ sendPush sends a Web Push notification to a subscriber.
func sendPush(sub webpush.Subscription, title, body string) {
	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
	})
	if err != nil {
		log.Println("Failed to marshal push payload:", err)
		return
	}

	resp, err := webpush.SendNotification(payload, &sub, &webpush.Options{
		VAPIDPublicKey:  vapidPublic,
		VAPIDPrivateKey: vapidPrivate,
		Subscriber:      "mailto:wellremind@example.com",
		TTL:             60,
	})
	if err != nil {
		log.Println("Push failed:", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("📬 Sent [%s] → HTTP %d\n", title, resp.StatusCode)
}



