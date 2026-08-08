package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	stateFile               = "wellremind-data.json"
	maxWaterIntervalMinutes = 24 * 60
)

// UserConfig holds a subscriber's push subscription and reminder preferences.
type UserConfig struct {
	Subscription     webpush.Subscription `json:"subscription"`
	WaterInterval    int                  `json:"waterInterval"`
	MedTimes         []string             `json:"medTimes"`
	StretchInterval  int                  `json:"stretchInterval"`
	MindfulnessTimes []string             `json:"mindfulnessTimes"`
	ScreenInterval   int                  `json:"screenInterval"`
	GratitudeTimes   []string             `json:"gratitudeTimes"`
	CheckinTime      string               `json:"checkinTime"`
	LastWater        time.Time            `json:"lastWater"`
	LastStretch      time.Time            `json:"lastStretch"`
	LastScreen       time.Time            `json:"lastScreen"`
	NotifiedEvents   map[string]string    `json:"notifiedEvents"`
	NotifiedMeds     map[string]string    `json:"notifiedMeds,omitempty"`
	SymptomHistory   []SymptomEntry       `json:"symptomHistory,omitempty"`
}

type SymptomEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Symptoms  []string  `json:"symptoms"`
}

type persistedState struct {
	VAPIDPrivate  string                 `json:"vapidPrivate"`
	VAPIDPublic   string                 `json:"vapidPublic"`
	Users         map[string]*UserConfig `json:"users"`
	SymptomCounts map[string]int         `json:"symptomCounts,omitempty"`
}

var (
	mu            sync.Mutex
	users         map[string]*UserConfig
	vapidPrivate  string
	vapidPublic   string
	pushSender    = sendPush
	stateSaver    = saveState
	symptomCounts = make(map[string]int)
)

func init() {
	users = make(map[string]*UserConfig)
}

func main() {
	if err := loadState(); err != nil {
		log.Fatal("Failed to load saved state:", err)
	}

	if vapidPrivate == "" || vapidPublic == "" {
		var err error
		vapidPrivate, vapidPublic, err = webpush.GenerateVAPIDKeys()
		if err != nil {
			log.Fatal("Failed to generate VAPID keys:", err)
		}
		if err := saveState(); err != nil {
			log.Fatal("Failed to persist generated VAPID keys:", err)
		}
	}

	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir("static"))
	mux.Handle("/", noCacheStatic(fileServer))
	mux.HandleFunc("/api/vapid-key", handleVapidKey)
	mux.HandleFunc("/api/subscribe", handleSubscribe)
	mux.HandleFunc("/api/unsubscribe", handleUnsubscribe)
	mux.HandleFunc("/api/check-symptoms", handleCheckSymptoms)
	mux.HandleFunc("/symptom-checker", serveSymptomPage)
	mux.HandleFunc("/api/symptom-history", handleSymptomHistory)
	mux.HandleFunc("/api/analytics", handleAnalytics)
	mux.HandleFunc("/healthz", handleHealth)

	go runScheduler()

	fmt.Println("🚀 WellRemind running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/sw.js" {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		next.ServeHTTP(w, r)
	})
}

func loadState() error {
	f, err := os.Open(stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	contents, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(contents) == 0 {
		return nil
	}

	var state persistedState
	if err := json.Unmarshal(contents, &state); err != nil {
		return err
	}

	vapidPrivate = state.VAPIDPrivate
	vapidPublic = state.VAPIDPublic
	if state.Users != nil {
		users = state.Users
	}
	if users == nil {
		users = make(map[string]*UserConfig)
	}

	if state.SymptomCounts != nil {
		symptomCounts = state.SymptomCounts
	}

	for endpoint, user := range users {
		if user == nil || user.Subscription.Endpoint == "" {
			delete(users, endpoint)
			continue
		}
		if user.NotifiedEvents == nil {
			if user.NotifiedMeds != nil {
				user.NotifiedEvents = user.NotifiedMeds
			} else {
				user.NotifiedEvents = make(map[string]string)
			}
		}
		if user.LastWater.IsZero() {
			user.LastWater = time.Now()
		}
		if user.LastStretch.IsZero() {
			user.LastStretch = time.Now()
		}
		if user.LastScreen.IsZero() {
			user.LastScreen = time.Now()
		}
	}

	return nil
}

func saveState() error {
	state := persistedState{
		VAPIDPrivate: vapidPrivate,
		VAPIDPublic:  vapidPublic,
		Users:        users,
	}

	if len(symptomCounts) > 0 {
		state.SymptomCounts = symptomCounts
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(stateFile, data, 0o660)
}

func handleVapidKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"publicKey": vapidPublic})
}

func handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config UserConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if config.Subscription.Endpoint == "" {
		http.Error(w, "Missing push subscription endpoint", http.StatusBadRequest)
		return
	}

	if config.WaterInterval < 0 || config.WaterInterval > maxWaterIntervalMinutes {
		http.Error(w, "Water interval must be between 0 and 1440 minutes", http.StatusBadRequest)
		return
	}
	if config.StretchInterval < 0 || config.StretchInterval > maxWaterIntervalMinutes {
		http.Error(w, "Stretch interval must be between 0 and 1440 minutes", http.StatusBadRequest)
		return
	}
	if config.ScreenInterval < 0 || config.ScreenInterval > maxWaterIntervalMinutes {
		http.Error(w, "Screen break interval must be between 0 and 1440 minutes", http.StatusBadRequest)
		return
	}

	medTimes, err := parseTimeList(config.MedTimes, "medication")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config.MedTimes = medTimes

	mindfulnessTimes, err := parseTimeList(config.MindfulnessTimes, "mindfulness")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config.MindfulnessTimes = mindfulnessTimes

	gratitudeTimes, err := parseTimeList(config.GratitudeTimes, "gratitude")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	config.GratitudeTimes = gratitudeTimes

	config.CheckinTime = strings.TrimSpace(config.CheckinTime)
	if config.CheckinTime != "" {
		if _, err := time.Parse("15:04", config.CheckinTime); err != nil {
			http.Error(w, "Invalid check-in time", http.StatusBadRequest)
			return
		}
	}

	if config.WaterInterval == 0 && len(config.MedTimes) == 0 && config.StretchInterval == 0 && len(config.MindfulnessTimes) == 0 && config.ScreenInterval == 0 && len(config.GratitudeTimes) == 0 && config.CheckinTime == "" {
		http.Error(w, "Please choose at least one reminder type", http.StatusBadRequest)
		return
	}

	config.NotifiedEvents = make(map[string]string)
	config.LastWater = time.Now()
	config.LastStretch = time.Now()
	config.LastScreen = time.Now()

	mu.Lock()
	users[config.Subscription.Endpoint] = &config
	if err := saveState(); err != nil {
		log.Println("Failed to persist subscription state:", err)
	}
	mu.Unlock()

	log.Printf("✅ New subscriber. Water=%d, Stretch=%d, Meds=%v, Mindfulness=%v, Check-in=%q\n",
		config.WaterInterval, config.StretchInterval, config.MedTimes, config.MindfulnessTimes, config.CheckinTime)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "subscribed"})
}

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
	if err := saveState(); err != nil {
		log.Println("Failed to persist unsubscribe state:", err)
	}
	mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unsubscribed"})
}

func serveSymptomPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/symptom.html")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleSymptomHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if user, ok := users[req.Endpoint]; ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(user.SymptomHistory)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]SymptomEntry{})
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	json.NewEncoder(w).Encode(symptomCounts)
}

func handleCheckSymptoms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Endpoint string   `json:"endpoint"`
		Symptoms []string `json:"symptoms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Symptoms) == 0 {
		http.Error(w, "Please select at least one symptom", http.StatusBadRequest)
		return
	}

	info := evaluateSymptoms(req.Symptoms)
	if req.Endpoint != "" {
		mu.Lock()
		if user, ok := users[req.Endpoint]; ok {
			entry := SymptomEntry{Timestamp: time.Now(), Symptoms: info.Symptoms}
			user.SymptomHistory = append(user.SymptomHistory, entry)
			if len(user.SymptomHistory) > 20 {
				user.SymptomHistory = user.SymptomHistory[len(user.SymptomHistory)-20:]
			}
			info.History = user.SymptomHistory
			if err := stateSaver(); err != nil {
				log.Println("Failed to persist symptom history:", err)
			}
		}
		mu.Unlock()
	}

	// record aggregate analytics
	if info.Condition != "" {
		mu.Lock()
		symptomCounts[info.Condition]++
		if err := stateSaver(); err != nil {
			log.Println("Failed to persist symptom counts:", err)
		}
		mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

type symptomCheckResponse struct {
	Condition string         `json:"condition"`
	Advice    string         `json:"advice"`
	Rationale string         `json:"rationale,omitempty"`
	Urgent    bool           `json:"urgent,omitempty"`
	Note      string         `json:"note"`
	Symptoms  []string       `json:"symptoms,omitempty"`
	History   []SymptomEntry `json:"history,omitempty"`
}

func evaluateSymptoms(symptoms []string) symptomCheckResponse {
	symptomSet := make(map[string]bool)
	for _, symptom := range symptoms {
		symptom = strings.TrimSpace(strings.ToLower(symptom))
		if symptom != "" {
			symptomSet[symptom] = true
		}
	}

	resp := symptomCheckResponse{
		Condition: "General health review",
		Advice:    "Rest, hydrate, and monitor symptoms. Seek care if symptoms worsen.",
		Note:      "This is not medical advice. If you are concerned, please consult a healthcare professional.",
		Symptoms:  symptoms,
	}

	// Red flags — urgent
	if (symptomSet["chest pain"] && (symptomSet["shortness of breath"] || symptomSet["difficulty breathing"])) || symptomSet["severe abdominal pain"] {
		resp.Condition = "Possible emergency — urgent attention needed"
		resp.Rationale = "These symptoms are commonly associated with cardiac or respiratory emergencies."
		resp.Advice = "Seek immediate medical care or call emergency services."
		resp.Urgent = true
		return resp
	}

	// Specific clusters
	if symptomSet["fever"] && symptomSet["cough"] && symptomSet["sore throat"] {
		resp.Condition = "Flu-like illness"
		resp.Rationale = "Fever with cough and sore throat are commonly associated with viral respiratory infections."
		resp.Advice = "Rest, stay hydrated, treat fever with appropriate OTC medication, and seek testing or care if high-risk or symptoms worsen."
		return resp
	}

	if symptomSet["dizziness"] && (symptomSet["thirst"] || symptomSet["fatigue"]) {
		resp.Condition = "Possible dehydration"
		resp.Rationale = "Dizziness with thirst/fatigue frequently occurs when fluid intake is insufficient."
		resp.Advice = "Rehydrate with water or oral rehydration solutions, rest, and seek care if symptoms are severe."
		return resp
	}

	if symptomSet["abdominal pain"] && symptomSet["nausea"] && symptomSet["diarrhea"] {
		resp.Condition = "Possible gastrointestinal illness"
		resp.Rationale = "Abdominal pain with nausea/diarrhea often indicates GI infection or food-related illness."
		resp.Advice = "Stay hydrated, avoid solid foods until nausea improves, and seek care if severe pain or high fever."
		return resp
	}

	if symptomSet["headache"] && symptomSet["fatigue"] && symptomSet["nausea"] {
		resp.Condition = "Possible migraine or stress-related headache"
		resp.Rationale = "Headache with fatigue and nausea can be commonly associated with migraine or severe stress."
		resp.Advice = "Rest in a quiet, dark room, hydrate, and consider OTC analgesics. Seek care for persistent or worsening symptoms."
		return resp
	}

	if symptomSet["cough"] && symptomSet["sore throat"] {
		resp.Condition = "Common cold or mild respiratory infection"
		resp.Rationale = "Cough and sore throat are commonly associated with mild upper respiratory infections."
		resp.Advice = "Rest, fluids, and symptomatic care; see a provider if high risk or symptoms worsen."
		return resp
	}

	// Fallback: give symptom-specific tips
	var tips []string
	if symptomSet["fever"] {
		tips = append(tips, "manage fever with rest and fluids")
	}
	if symptomSet["cough"] {
		tips = append(tips, "use warm fluids and lozenges for cough relief")
	}
	if symptomSet["nausea"] {
		tips = append(tips, "try bland foods and small sips of clear fluids")
	}
	if len(tips) > 0 {
		resp.Rationale = "Symptoms are commonly associated with: " + strings.Join(tips, "; ")
		resp.Advice = "Follow the guidance above and consult a healthcare professional if concerned."
	}

	return resp
}

func parseMedTimes(times []string) ([]string, error) {
	return parseTimeList(times, "medication")
}

func runScheduler() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for now := range ticker.C {
		mu.Lock()
		var updated bool
		for _, user := range users {
			if checkWater(user, now) || checkStretch(user, now) || checkScreenBreak(user, now) || checkMedications(user, now) || checkMindfulness(user, now) || checkGratitude(user, now) || checkCheckin(user, now) {
				updated = true
			}
		}
		if updated {
			if err := saveState(); err != nil {
				log.Println("Failed to save state during scheduler run:", err)
			}
		}
		mu.Unlock()
	}
}

func checkWater(user *UserConfig, now time.Time) bool {
	if user.WaterInterval <= 0 {
		return false
	}
	if now.Sub(user.LastWater).Minutes() >= float64(user.WaterInterval) {
		pushSender(user.Subscription, "💧 Time to Hydrate!", "Drink a glass of water — your body will thank you.")
		user.LastWater = now
		return true
	}
	return false
}

func checkStretch(user *UserConfig, now time.Time) bool {
	if user.StretchInterval <= 0 {
		return false
	}
	if now.Sub(user.LastStretch).Minutes() >= float64(user.StretchInterval) {
		pushSender(user.Subscription, "🧘 Time to Stretch!", "Take a moment for a quick stretch break.")
		user.LastStretch = now
		return true
	}
	return false
}

func ensureUserEvents(user *UserConfig) {
	if user.NotifiedEvents == nil {
		user.NotifiedEvents = make(map[string]string)
	}
}

func checkMindfulness(user *UserConfig, now time.Time) bool {
	ensureUserEvents(user)
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

	updated := false
	for _, mindTime := range user.MindfulnessTimes {
		if mindTime != currentTime {
			continue
		}
		key := "mindfulness " + today + " " + mindTime
		if _, alreadySent := user.NotifiedEvents[key]; alreadySent {
			continue
		}
		pushSender(user.Subscription, "🧘 Mindfulness Reminder", fmt.Sprintf("Time for a mindful pause at %s.", mindTime))
		user.NotifiedEvents[key] = "sent"
		updated = true
	}
	return updated
}

func checkScreenBreak(user *UserConfig, now time.Time) bool {
	if user.ScreenInterval <= 0 {
		return false
	}
	if now.Sub(user.LastScreen).Minutes() >= float64(user.ScreenInterval) {
		pushSender(user.Subscription, "🚶 Screen Break Reminder", "Look away, stand up, and move for a short break.")
		user.LastScreen = now
		return true
	}
	return false
}

func checkGratitude(user *UserConfig, now time.Time) bool {
	ensureUserEvents(user)
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

	updated := false
	for _, gratitudeTime := range user.GratitudeTimes {
		if gratitudeTime != currentTime {
			continue
		}
		key := "gratitude " + today + " " + gratitudeTime
		if _, alreadySent := user.NotifiedEvents[key]; alreadySent {
			continue
		}
		pushSender(user.Subscription, "🌟 Gratitude Reminder", fmt.Sprintf("Pause and note something you are grateful for at %s.", gratitudeTime))
		user.NotifiedEvents[key] = "sent"
		updated = true
	}
	return updated
}

func checkCheckin(user *UserConfig, now time.Time) bool {
	if user.CheckinTime == "" {
		return false
	}
	ensureUserEvents(user)
	currentTime := now.Format("15:04")
	if currentTime != user.CheckinTime {
		return false
	}
	today := now.Format("2006-01-02")
	key := "checkin " + today + " " + user.CheckinTime
	if _, alreadySent := user.NotifiedEvents[key]; alreadySent {
		return false
	}
	pushSender(user.Subscription, "📝 Wellness Check-in", fmt.Sprintf("It's time for your daily check-in at %s.", user.CheckinTime))
	user.NotifiedEvents[key] = "sent"
	return true
}

func checkMedications(user *UserConfig, now time.Time) bool {
	ensureUserEvents(user)
	currentTime := now.Format("15:04")
	today := now.Format("2006-01-02")

	updated := false
	for _, medTime := range user.MedTimes {
		if medTime != currentTime {
			continue
		}
		key := "med " + today + " " + medTime
		if _, alreadySent := user.NotifiedEvents[key]; alreadySent {
			continue
		}
		pushSender(user.Subscription, "💊 Medication Reminder", fmt.Sprintf("Time to take your %s medication.", medTime))
		user.NotifiedEvents[key] = "sent"
		updated = true
	}

	return updated
}

func parseTimeList(times []string, label string) ([]string, error) {
	seen := map[string]bool{}
	clean := make([]string, 0, len(times))

	for _, t := range times {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		if _, err := time.Parse("15:04", t); err != nil {
			return nil, fmt.Errorf("Invalid %s time: %q", label, t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}

	return clean, nil
}

// sendPush sends a Web Push notification to a subscriber.
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
