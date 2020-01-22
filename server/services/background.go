package services

import (
	"log"
	"time"

	"github.com/gophertuts/reminders-cli/server/models"
)

type saver interface {
	save() error
}

// BackgroundSaver represents the reminder background saver
type BackgroundSaver struct {
	ticker  *time.Ticker
	service saver
}

// NewSaver creates a new instance of BackgroundSaver
func NewSaver(service saver) *BackgroundSaver {
	ticker := time.NewTicker(30 * time.Second)
	return &BackgroundSaver{
		ticker:  ticker,
		service: service,
	}
}

// Start starts the created Watcher
func (s *BackgroundSaver) Start() {
	log.Println("background saver started")
	for {
		select {
		case <-s.ticker.C:
			err := s.service.save()
			if err != nil {
				log.Printf("could not save records in background: %v", err)
			}
		}
	}
}

// Stop stops the created Watcher
func (s *BackgroundSaver) Stop() error {
	s.ticker.Stop()
	err := s.service.save()
	if err != nil {
		return err
	}
	log.Println("background saver stopped")
	return nil
}

// HTTPNotifierClient represents the HTTP client for communicating with the notifier server
type HTTPNotifierClient interface {
	Notify(reminder models.Reminder) (time.Duration, error)
}

type snapshotManager interface {
	snapshot() Snapshot
	snapshotGrooming(notifiedReminders ...models.Reminder)
	retry(reminder models.Reminder, duration time.Duration)
}

// BackgroundNotifier represents the reminder background saver
type BackgroundNotifier struct {
	ticker   *time.Ticker
	service  snapshotManager
	notified chan models.Reminder
	Client   HTTPNotifierClient
}

// NewNotifier creates a new instance of BackgroundNotifier
func NewNotifier(notifierURI string, service snapshotManager) *BackgroundNotifier {
	ticker := time.NewTicker(1 * time.Second)
	httpClient := NewHTTPClient(notifierURI)
	return &BackgroundNotifier{
		ticker:   ticker,
		service:  service,
		notified: make(chan models.Reminder),
		Client:   httpClient,
	}
}

// Start starts the created Watcher
func (s *BackgroundNotifier) Start() {
	log.Println("background notifier started")
	for {
		select {
		case <-s.ticker.C:
			snapshot := s.service.snapshot()
			for id := range snapshot.UnCompleted {
				_, reminder := snapshot.UnCompleted.flatten(id)
				reminderTick := reminder.ModifiedAt.Add(reminder.Duration).UnixNano()
				nowTick := time.Now().UnixNano()
				deltaTick := time.Now().Add(time.Second).UnixNano()
				if reminderTick > nowTick && reminderTick < deltaTick {
					go func() {
						err := s.notify(reminder)
						if err != nil {
							log.Printf("could not notify reminder with id %d", reminder.ID)
						}
					}()
				}
			}
		case r := <-s.notified:
			log.Printf("notified reminder with with: %d\n", r.ID)
		}
	}
}

// notify notifies a reminder via the HTTP client
func (s *BackgroundNotifier) notify(r models.Reminder) error {
	retryAfter, err := s.Client.Notify(r)
	if err != nil {
		return models.WrapError("could not notify reminder", err)
	}

	if retryAfter != 0 {
		s.service.retry(r, retryAfter)
	} else {
		s.service.snapshotGrooming(r)
		s.notified <- r
	}
	return nil
}

// Stop stops the created Watcher
func (s *BackgroundNotifier) Stop() error {
	s.ticker.Stop()
	log.Println("background notifier stopped")
	return nil
}
