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
			tasks.POST("/:id/comments", taskHandler.AddComment)
			tasks.GET("/:id/comments", taskHandler.GetTaskComments)

			// Специальные маршруты для задач
			tasks.POST("/create", taskHandler.CreateTask)
			tasks.POST("/create/predictive", taskHandler.CreateTaskWithPrediction)
			tasks.POST("/recurring", taskHandler.CreateRecurringTask)
			tasks.POST("/bulk_assign", taskHandler.BulkAssignTasks)
			tasks.GET("/my", taskHandler.GetMyTasks)
			tasks.GET("/suggestions", taskHandler.GetTaskSuggestions)
			tasks.GET("/stats", taskHandler.GetTaskStats)

			// Отчеты и аналитика
			tasks.POST("/report", taskHandler.GenerateReport)
			tasks.POST("/report/quick", taskHandler.GenerateQuickReport)
			tasks.GET("/reports", taskHandler.GetReports)
			tasks.GET("/dashboard", taskHandler.GetDashboard)
			tasks.GET("/productivity", taskHandler.GetProductivityReport)
			tasks.GET("/sales-analytics", taskHandler.GetSalesAnalytics)

			// Специальные функции
			tasks.POST("/onboarding", taskHandler.CreateOnboardingChecklist)
			tasks.POST("/tax-reminders", taskHandler.CreateTaxReminders)
			tasks.POST("/leave/request", taskHandler.CreateLeaveRequest)
			tasks.POST("/follow-up", taskHandler.CreateFollowUpTask)

			// Отслеживание времени
			tasks.POST("/:task_id/time/start", taskHandler.StartTimeTracking)
			tasks.POST("/time/stop", taskHandler.StopTimeTracking)
			tasks.GET("/time/active", taskHandler.GetActiveTimeEntries)
			tasks.GET("/time/total", taskHandler.GetTotalActiveTime)
			tasks.GET("/users/:user_id/time-report", taskHandler.GetTimeReport)

			// Зарплата
			tasks.POST("/payroll/calculate", taskHandler.CalculatePayroll)
			tasks.GET("/payroll/aggregate", taskHandler.GetAggregatePayroll)
		}

		// Маршруты для комментариев
		comments := v1.Group("/tasks/:taskId/comments")
		{
			comments.GET("/:commentId", taskHandler.GetComment)
			comments.PUT("/:commentId", taskHandler.UpdateComment)
			comments.DELETE("/:commentId", taskHandler.DeleteComment)
		}
	}
}
