package services

import (
	"context"
	"encoding/json"
	"task-service/config"
	"time"

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
