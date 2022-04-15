package migrate

import (
	"ginapp/models"

	"gorm.io/gorm"
)

// drop column age for table users
func User1(db *gorm.DB) error {
	return db.Migrator().DropColumn(&models.User{}, "age")
}
