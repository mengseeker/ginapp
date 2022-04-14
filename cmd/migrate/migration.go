package migrate

import "ginapp/models"

func Migrate() error {
	return AutoMigrate()
}

func AutoMigrate() error {
	return models.DB.Debug().AutoMigrate(
		&models.User{},
	)
}
