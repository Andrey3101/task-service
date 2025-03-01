package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/andre/task-service/config"

	"github.com/go-redis/redis/v8"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type GoogleCalendarService struct {
	Config *oauth2.Config
	Client *calendar.Service
	Redis  *redis.Client
}

func NewGoogleCalendarService(cfg config.Config, redisClient *redis.Client) *GoogleCalendarService {
	config := &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURI,
		Scopes:       []string{calendar.CalendarScope},
		Endpoint:     google.Endpoint,
	}
	return &GoogleCalendarService{Config: config, Redis: redisClient}
}

func (s *GoogleCalendarService) GetAuthURL() string {
	return s.Config.AuthCodeURL("state", oauth2.AccessTypeOffline)
}

func (s *GoogleCalendarService) Authenticate(ctx context.Context, code string) error {
	token, err := s.Config.Exchange(ctx, code)
	if err != nil {
		return err
	}
	client, err := calendar.NewService(ctx, option.WithTokenSource(s.Config.TokenSource(ctx, token)))
	if err != nil {
		return err
	}
	s.Client = client
	tokenJSON, _ := json.Marshal(token)
	s.Redis.Set(ctx, "google_calendar_token", tokenJSON, 0)
	return nil
}

func (s *GoogleCalendarService) LoadToken(ctx context.Context) error {
	tokenJSON, err := s.Redis.Get(ctx, "google_calendar_token").Bytes()
	if err != nil {
		return err
	}
	var token oauth2.Token
	if err := json.Unmarshal(tokenJSON, &token); err != nil {
		return err
	}
	if !token.Valid() {
		newToken, err := s.Config.TokenSource(ctx, &token).Token()
		if err != nil {
			return err
		}
		tokenJSON, _ = json.Marshal(newToken)
		s.Redis.Set(ctx, "google_calendar_token", tokenJSON, 0)
		token = *newToken
	}
	client, err := calendar.NewService(ctx, option.WithTokenSource(s.Config.TokenSource(ctx, &token)))
	s.Client = client
	return err
}

func (s *GoogleCalendarService) CreateEvent(ctx context.Context, taskID, title, description string, dueDate time.Time) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", err
		}
	}
	event := &calendar.Event{
		Summary:     title,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: dueDate.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: dueDate.Add(time.Hour).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Reminders: &calendar.EventReminders{
			UseDefault: false,
			Overrides:  []*calendar.EventReminder{{Method: "email", Minutes: 15}},
		},
	}
	createdEvent, err := s.Client.Events.Insert("primary", event).Do()
	if err != nil {
		return "", err
	}
	s.Redis.Set(ctx, "calendar_event:"+taskID, createdEvent.Id, 0)
	return createdEvent.Id, nil
}

func (s *GoogleCalendarService) UpdateEvent(ctx context.Context, taskID, eventID, title, description string, dueDate time.Time) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return err
		}
	}
	event, err := s.Client.Events.Get("primary", eventID).Do()
	if err != nil {
		return err
	}
	event.Summary = title
	event.Description = description
	event.Start = &calendar.EventDateTime{DateTime: dueDate.Format(time.RFC3339), TimeZone: "UTC"}
	event.End = &calendar.EventDateTime{DateTime: dueDate.Add(time.Hour).Format(time.RFC3339), TimeZone: "UTC"}
	_, err = s.Client.Events.Update("primary", eventID, event).Do()
	return err
}

func (s *GoogleCalendarService) DeleteEvent(ctx context.Context, taskID, eventID string) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return err
		}
	}
	err := s.Client.Events.Delete("primary", eventID).Do()
	if err == nil {
		s.Redis.Del(ctx, "calendar_event:"+taskID)
	}
	return err
}

func (s *GoogleCalendarService) GetCalendarEvents(ctx context.Context) ([]*calendar.Event, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return nil, fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	events, err := s.Client.Events.List("primary").TimeMin(time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339)).TimeMax(time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events: %v", err)
	}
	return events.Items, nil
}

func (s *GoogleCalendarService) CreateCalendarEvent(ctx context.Context, summary, description, startTime string) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}
	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Start: &calendar.EventDateTime{
			DateTime: start.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: &calendar.EventDateTime{
			DateTime: start.Add(time.Hour).Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Reminders: &calendar.EventReminders{
			UseDefault: false,
			Overrides:  []*calendar.EventReminder{{Method: "email", Minutes: 15}},
		},
	}
	createdEvent, err := s.Client.Events.Insert("primary", event).Do()
	if err != nil {
		return "", fmt.Errorf("failed to create event: %v", err)
	}
	return createdEvent.Id, nil
}

func (s *GoogleCalendarService) UpdateCalendarEvent(ctx context.Context, eventID, summary, description, startTime string) (string, error) {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return "", fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	start, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		return "", fmt.Errorf("invalid start time format: %v", err)
	}
	event, err := s.Client.Events.Get("primary", eventID).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get event: %v", err)
	}
	event.Summary = summary
	event.Description = description
	event.Start = &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: "UTC"}
	event.End = &calendar.EventDateTime{DateTime: start.Add(time.Hour).Format(time.RFC3339), TimeZone: "UTC"}
	updatedEvent, err := s.Client.Events.Update("primary", eventID, event).Do()
	if err != nil {
		return "", fmt.Errorf("failed to update event: %v", err)
	}
	return updatedEvent.Id, nil
}

func (s *GoogleCalendarService) DeleteCalendarEvent(ctx context.Context, eventID string) error {
	if s.Client == nil {
		if err := s.LoadToken(ctx); err != nil {
			return fmt.Errorf("failed to load Google Calendar token: %v", err)
		}
	}
	err := s.Client.Events.Delete("primary", eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to delete event: %v", err)
	}
	return nil
}
