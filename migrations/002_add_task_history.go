package migrations

import (
	"log"
	"tasks-service/models"

	"gorm.io/gorm"
)

func RunMigrations(db *gorm.DB) {
	if err := db.AutoMigrate(&models.Task{}, &models.Assignee{}, &models.Comment{}, &models.Report{}, &models.TaskHistory{}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	db.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_company ON tasks (created_by)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_task_history_task_id ON task_history (task_id)")
}
