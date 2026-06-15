package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Session struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	EventID           string     `json:"event_id"`
	Status            string     `json:"status"`
	Position          int64      `json:"position"`
	QueuedAt          time.Time  `json:"queued_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	UserAgent         string     `json:"user_agent"`
	IPAddress         string     `json:"ip_address"`
	CheckoutToken     string     `json:"checkout_token,omitempty"`
	CheckoutExpiresAt *time.Time `json:"checkout_expires_at,omitempty"`
	AdmittedAt        *time.Time `json:"admitted_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

var (
	redisURL        = flag.String("redis", "localhost:6379", "Redis URL (host:port)")
	redisPass       = flag.String("password", "", "Redis password")
	eventID         = flag.String("event", "", "Event ID (required)")
	numUsers        = flag.Int("users", 300, "Number of users to create (200-500)")
	exitRate        = flag.Float64("exit-rate", 0.1, "Probability of user leaving per minute (0.0-1.0)")
	joinRate        = flag.Duration("join-rate", 10*time.Millisecond, "Time between user joins (set to 0 for maximum speed)")
	batchSize       = flag.Int("batch-size", 20, "Number of users to create per batch (higher = faster)")
	simulate        = flag.Bool("simulate", false, "Enable continuous simulation with random exits")
	checkoutRate    = flag.Float64("checkout-rate", 0.3, "Probability of checkout completion per check (0.0-1.0)")
	checkoutInterval = flag.Duration("checkout-interval", 15*time.Second, "Interval to simulate checkout completions")
)

func main() {
	flag.Parse()

	if *eventID == "" {
		fmt.Println("Error: --event flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if *numUsers < 200 || *numUsers > 500 {
		fmt.Println("Warning: Recommended user count is 200-500, got:", *numUsers)
	}

	ctx := context.Background()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     *redisURL,
		Password: *redisPass,
		DB:       0,
	})

	// Test connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("Failed to connect to Redis: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ Connected to Redis at %s\n", *redisURL)

	// Create and enqueue users
	sessionIDs := createAndEnqueueUsers(ctx, rdb, *eventID, *numUsers)

	fmt.Printf("\n✅ Created %d users in queue for event %s\n", len(sessionIDs), *eventID)
	fmt.Printf("📊 Queue length: %d\n", getQueueLength(ctx, rdb, *eventID))
	fmt.Printf("🎯 Active events: %v\n", getActiveEvents(ctx, rdb))

	if *simulate {
		fmt.Printf("\n🎬 Starting continuous simulation...\n")
		fmt.Printf("   Queue exit rate: %.1f%% per minute\n", *exitRate*100)
		fmt.Printf("   Checkout completion rate: %.1f%% every %v\n", *checkoutRate*100, *checkoutInterval)
		fmt.Printf("   Press Ctrl+C to stop\n\n")
		runSimulation(ctx, rdb, *eventID, sessionIDs)
	} else {
		fmt.Println("\n💡 Tip: Use --simulate flag to enable random exits and checkout completions")
	}
}

func createAndEnqueueUsers(ctx context.Context, rdb *redis.Client, eventID string, numUsers int) []string {
	sessionIDs := make([]string, 0, numUsers)

	fmt.Printf("\n🚀 Creating %d users in batches of %d...\n", numUsers, *batchSize)
	startTime := time.Now()

	queueKey := fmt.Sprintf("waitroom:%s:queue", eventID)

	// Process users in batches
	for batchStart := 0; batchStart < numUsers; batchStart += *batchSize {
		batchEnd := batchStart + *batchSize
		if batchEnd > numUsers {
			batchEnd = numUsers
		}

		pipe := rdb.Pipeline()
		batchSessions := make([]string, 0, *batchSize)

		// Create all users in this batch
		for i := batchStart; i < batchEnd; i++ {
			sessionID := uuid.New().String()
			userID := fmt.Sprintf("demo-user-%d", i+1)

			// Create session with slight time offset to maintain queue order
			baseTime := time.Now()
			queuedAt := baseTime.Add(time.Duration(i-batchStart) * time.Millisecond)

			session := Session{
				ID:        sessionID,
				UserID:    userID,
				EventID:   eventID,
				Status:    "queued",
				Position:  0,
				QueuedAt:  queuedAt,
				ExpiresAt: baseTime.Add(2 * time.Hour),
				UpdatedAt: baseTime,
				UserAgent: "Demo-Simulation/1.0",
				IPAddress: fmt.Sprintf("192.168.1.%d", rand.Intn(255)),
			}

			// Save session to Redis
			sessionKey := fmt.Sprintf("waitroom:session:%s", sessionID)
			sessionJSON, _ := json.Marshal(session)

			// 1. Save session
			pipe.Set(ctx, sessionKey, sessionJSON, 2*time.Hour)

			// 2. Add to queue (score = timestamp in seconds, matching GetQueueScore())
			score := float64(session.QueuedAt.Unix())
			pipe.ZAdd(ctx, queueKey, redis.Z{
				Score:  score,
				Member: sessionID,
			})

			// 3. Add user-event index
			userEventKey := fmt.Sprintf("waitroom:user_session:%s:%s", userID, eventID)
			pipe.Set(ctx, userEventKey, sessionID, 2*time.Hour)

			batchSessions = append(batchSessions, sessionID)
		}

		// 4. Mark event as active (once per batch)
		pipe.SAdd(ctx, "waitroom:active_events", eventID)

		// Execute batch
		if _, err := pipe.Exec(ctx); err != nil {
			fmt.Printf("❌ Failed to create batch %d-%d: %v\n", batchStart+1, batchEnd, err)
		} else {
			sessionIDs = append(sessionIDs, batchSessions...)
		}

		// Progress indicator
		if batchEnd%50 == 0 || batchEnd == numUsers {
			fmt.Printf("   Progress: %d/%d users created\n", batchEnd, numUsers)
		}

		// Rate limiting between batches (much faster than per-user)
		if batchEnd < numUsers && *joinRate > 0 {
			time.Sleep(*joinRate)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("⏱️  Completed in %v (%.0f users/sec)\n", elapsed, float64(numUsers)/elapsed.Seconds())

	return sessionIDs
}

func runSimulation(ctx context.Context, rdb *redis.Client, eventID string, sessionIDs []string) {
	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var mu sync.Mutex
	activeSessions := make(map[string]bool)
	for _, id := range sessionIDs {
		activeSessions[id] = true
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				queueLen := getQueueLength(ctx, rdb, eventID)
				processingLen := getProcessingCount(ctx, rdb, eventID)
				mu.Unlock()

				fmt.Printf("[%s] Queue: %d | Processing: %d | Active: %d\n",
					time.Now().Format("15:04:05"),
					queueLen,
					processingLen,
					len(activeSessions),
				)
			}
		}
	}()

	// Simulate random queue exits
	exitTicker := time.NewTicker(1 * time.Minute)
	defer exitTicker.Stop()

	// Simulate checkout completions
	checkoutTicker := time.NewTicker(*checkoutInterval)
	defer checkoutTicker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n\n🛑 Simulation stopped")
			printFinalStats(ctx, rdb, eventID)
			return

		case <-exitTicker.C:
			mu.Lock()
			// Calculate how many users should exit from queue
			numToExit := int(float64(len(activeSessions)) * (*exitRate))
			if numToExit > 0 {
				exited := simulateRandomExits(ctx, rdb, eventID, activeSessions, numToExit)
				if exited > 0 {
					fmt.Printf("👋 %d users left the queue (%.1f%% exit rate)\n", exited, *exitRate*100)
				}
			}
			mu.Unlock()

		case <-checkoutTicker.C:
			// Simulate checkout completions/failures
			completed := simulateCheckoutCompletions(ctx, rdb, eventID)
			if completed > 0 {
				fmt.Printf("✅ %d users completed checkout (%.1f%% completion rate)\n", completed, *checkoutRate*100)
			}
		}
	}
}

func simulateRandomExits(ctx context.Context, rdb *redis.Client, eventID string, activeSessions map[string]bool, numToExit int) int {
	if len(activeSessions) == 0 {
		return 0
	}

	// Get random sessions to remove
	sessionList := make([]string, 0, len(activeSessions))
	for sessionID := range activeSessions {
		sessionList = append(sessionList, sessionID)
	}

	rand.Shuffle(len(sessionList), func(i, j int) {
		sessionList[i], sessionList[j] = sessionList[j], sessionList[i]
	})

	exited := 0
	for i := 0; i < numToExit && i < len(sessionList); i++ {
		sessionID := sessionList[i]

		// Remove from queue
		queueKey := fmt.Sprintf("waitroom:%s:queue", eventID)
		if err := rdb.ZRem(ctx, queueKey, sessionID).Err(); err != nil {
			continue
		}

		// Update session status
		sessionKey := fmt.Sprintf("waitroom:session:%s", sessionID)
		var session Session
		data, _ := rdb.Get(ctx, sessionKey).Bytes()
		json.Unmarshal(data, &session)
		session.Status = "abandoned"
		session.UpdatedAt = time.Now()
		sessionJSON, _ := json.Marshal(session)
		rdb.Set(ctx, sessionKey, sessionJSON, 2*time.Hour)

		delete(activeSessions, sessionID)
		exited++
	}

	// Check if queue is empty and remove from active events
	queueLen := getQueueLength(ctx, rdb, eventID)
	if queueLen == 0 {
		rdb.SRem(ctx, "waitroom:active_events", eventID)
		fmt.Printf("🏁 Queue is now empty, event removed from active set\n")
	}

	return exited
}

func simulateCheckoutCompletions(ctx context.Context, rdb *redis.Client, eventID string) int {
	processingKey := fmt.Sprintf("waitroom:%s:processing", eventID)

	// Get all sessions in processing
	processingMembers, err := rdb.SMembers(ctx, processingKey).Result()
	if err != nil || len(processingMembers) == 0 {
		return 0
	}

	// Calculate how many should complete checkout
	numToComplete := int(float64(len(processingMembers)) * (*checkoutRate))
	if numToComplete == 0 && len(processingMembers) > 0 {
		numToComplete = 1 // At least complete one if there are any
	}

	// Shuffle and pick random sessions
	rand.Shuffle(len(processingMembers), func(i, j int) {
		processingMembers[i], processingMembers[j] = processingMembers[j], processingMembers[i]
	})

	completed := 0
	for i := 0; i < numToComplete && i < len(processingMembers); i++ {
		sessionID := processingMembers[i]

		// Remove from processing set
		if err := rdb.SRem(ctx, processingKey, sessionID).Err(); err != nil {
			continue
		}

		// Update session status to completed (80% success, 20% failed)
		sessionKey := fmt.Sprintf("waitroom:session:%s", sessionID)
		var session Session
		data, _ := rdb.Get(ctx, sessionKey).Bytes()
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		now := time.Now()
		if rand.Float64() < 0.8 {
			session.Status = "completed"
			session.CompletedAt = &now
		} else {
			session.Status = "failed"
		}
		session.UpdatedAt = now

		sessionJSON, _ := json.Marshal(session)
		rdb.Set(ctx, sessionKey, sessionJSON, 2*time.Hour)

		completed++
	}

	return completed
}

func getQueueLength(ctx context.Context, rdb *redis.Client, eventID string) int64 {
	queueKey := fmt.Sprintf("waitroom:%s:queue", eventID)
	count, _ := rdb.ZCard(ctx, queueKey).Result()
	return count
}

func getProcessingCount(ctx context.Context, rdb *redis.Client, eventID string) int64 {
	processingKey := fmt.Sprintf("waitroom:%s:processing", eventID)
	count, _ := rdb.SCard(ctx, processingKey).Result()
	return count
}

func getActiveEvents(ctx context.Context, rdb *redis.Client) []string {
	events, _ := rdb.SMembers(ctx, "waitroom:active_events").Result()
	return events
}

func printFinalStats(ctx context.Context, rdb *redis.Client, eventID string) {
	queueLen := getQueueLength(ctx, rdb, eventID)
	processingLen := getProcessingCount(ctx, rdb, eventID)

	fmt.Println("\n📊 Final Statistics:")
	fmt.Printf("   Queue Length: %d\n", queueLen)
	fmt.Printf("   Processing: %d\n", processingLen)
	fmt.Printf("   Total Active: %d\n", queueLen+processingLen)
}
