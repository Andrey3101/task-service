package migrations

import (
	"log"
	"tasks-service/models"

	"gorm.io/gorm"
)

func RunMigrations(db *gorm.DB) {
	if err := db.AutoMigrate(&models.Task{}, &models.Assignee{}, &models.Comment{}, &models.Report{}); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	db.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_company ON tasks (created_by)")
}
