package v1

import (
	"github.com/andre/task-service/handlers"
	"github.com/gin-gonic/gin"
)

// SetupRoutes настраивает маршруты для API v1
func SetupRoutes(router *gin.Engine, taskHandler *handlers.TaskHandler) {
	// API v1 группа
	v1 := router.Group("/api/v1")
	{
		// Маршруты для задач
		tasks := v1.Group("/tasks")
		{
			tasks.POST("/", taskHandler.CreateTask)
			tasks.GET("/", taskHandler.GetTasks)
			tasks.GET("/:id", taskHandler.GetTask)
			tasks.PUT("/:id", taskHandler.UpdateTask)
			tasks.DELETE("/:id", taskHandler.DeleteTask)
			tasks.POST("/:id/complete", taskHandler.CompleteTask)
			tasks.POST("/:id/assign", taskHandler.AssignTask)
			tasks.GET("/:id/comments", taskHandler.GetTaskComments)
			tasks.POST("/:id/comments", taskHandler.AddTaskComment)
		}

		// Маршруты для встреч
		meetings := v1.Group("/meetings")
		{
			meetings.POST("/", taskHandler.CreateMeeting)
			meetings.GET("/", taskHandler.GetMeetings)
			meetings.GET("/:id", taskHandler.GetMeeting)
			meetings.PUT("/:id", taskHandler.UpdateMeeting)
			meetings.DELETE("/:id", taskHandler.DeleteMeeting)
			meetings.POST("/:id/status", taskHandler.ChangeMeetingStatus)
			meetings.GET("/upcoming", taskHandler.GetUpcomingMeetings)
		}

		// Маршруты для важных дат
		importantDates := v1.Group("/important-dates")
		{
			importantDates.POST("/", taskHandler.CreateImportantDate)
			importantDates.GET("/", taskHandler.GetImportantDates)
			importantDates.GET("/:id", taskHandler.GetImportantDate)
			importantDates.PUT("/:id", taskHandler.UpdateImportantDate)
			importantDates.DELETE("/:id", taskHandler.DeleteImportantDate)
			importantDates.POST("/:id/complete", taskHandler.MarkDateCompleted)
			importantDates.GET("/overdue", taskHandler.GetOverdueDates)
		}

		// Маршруты для интеграций
		integrations := v1.Group("/integrations")
		{
			integrations.POST("/", taskHandler.CreateExternalIntegration)
			integrations.GET("/", taskHandler.GetExternalIntegrations)
			integrations.GET("/:id", taskHandler.GetExternalIntegration)
			integrations.PUT("/:id", taskHandler.UpdateExternalIntegration)
			integrations.DELETE("/:id", taskHandler.DeleteExternalIntegration)
			integrations.POST("/:id/sync", taskHandler.SyncExternalData)
			integrations.POST("/:id/test", taskHandler.TestIntegrationConnection)
		}

		// Маршруты для контактов
		contacts := v1.Group("/contacts")
		{
			contacts.POST("/", taskHandler.CreateContact)
			contacts.GET("/", taskHandler.GetContacts)
			contacts.GET("/:id", taskHandler.GetContact)
			contacts.PUT("/:id", taskHandler.UpdateContact)
			contacts.DELETE("/:id", taskHandler.DeleteContact)
			contacts.GET("/search", taskHandler.SearchContacts)
			contacts.GET("/stats", taskHandler.GetContactStats)
			contacts.POST("/:id/interactions", taskHandler.CreateContactInteraction)
			contacts.GET("/:id/interactions", taskHandler.GetContactInteractions)
		}

		// Маршруты для комментариев
		comments := v1.Group("/comments")
		{
			comments.GET("/:id", taskHandler.GetComment)
			comments.PUT("/:id", taskHandler.UpdateComment)
			comments.DELETE("/:id", taskHandler.DeleteComment)
		}

		// Маршруты для подписок
		subscriptions := v1.Group("/subscriptions")
		{
			subscriptions.POST("/", taskHandler.CreateSubscription)
			subscriptions.GET("/", taskHandler.GetSubscriptions)
			subscriptions.GET("/:id", taskHandler.GetSubscription)
			subscriptions.PUT("/:id", taskHandler.UpdateSubscription)
			subscriptions.DELETE("/:id", taskHandler.DeleteSubscription)
		}

		// Маршруты для Google Calendar
		googleCalendar := v1.Group("/google-calendar")
		{
			googleCalendar.GET("/auth", taskHandler.GoogleCalendarAuth)
			googleCalendar.GET("/callback", taskHandler.GoogleCalendarCallback)
			googleCalendar.GET("/events", taskHandler.GetGoogleCalendarEvents)
			googleCalendar.POST("/events", taskHandler.CreateGoogleCalendarEvent)
			googleCalendar.PUT("/events/:id", taskHandler.UpdateGoogleCalendarEvent)
			googleCalendar.DELETE("/events/:id", taskHandler.DeleteGoogleCalendarEvent)
		}

		// Маршруты для WebSocket
		websocket := v1.Group("/ws")
		{
			websocket.GET("/", taskHandler.HandleWebSocket)
		}

		// Маршруты для аналитики
		analytics := v1.Group("/analytics")
		{
			analytics.GET("/tasks", taskHandler.GetTaskAnalytics)
			analytics.GET("/meetings", taskHandler.GetMeetingAnalytics)
			analytics.GET("/contacts", taskHandler.GetContactAnalytics)
			analytics.GET("/productivity", taskHandler.GetProductivityAnalytics)
		}

		// Маршруты для экспорта
		export := v1.Group("/export")
		{
			export.GET("/tasks", taskHandler.ExportTasks)
			export.GET("/meetings", taskHandler.ExportMeetings)
			export.GET("/contacts", taskHandler.ExportContacts)
			export.GET("/reports", taskHandler.ExportReports)
		}
	}
}
