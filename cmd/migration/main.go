package main

import (
	"fmt"
	"log"

	"kyc-service/internal/config"
	"kyc-service/internal/models"
	"kyc-service/internal/storage"
	"kyc-service/pkg/utils"
)

func main() {
	// 1. Load config
	cfg := config.Load("config.yaml")

	// 2. Connect to DB
	db, err := storage.InitDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	fmt.Println("Starting data migration for OAuth 1:N architecture...")

	// 3. Find all users that still have 'provider' set in the users table
	// Note: since we removed Provider from the Go struct, we must use raw SQL
	type LegacyUser struct {
		ID         string
		Email      string
		Provider   string
		ProviderID string
	}

	var legacyUsers []LegacyUser
	// Query users where provider is not null, not empty, and not 'local'
	err = db.Raw("SELECT id, email, provider, provider_id FROM users WHERE provider IS NOT NULL AND provider != '' AND provider != 'local'").Scan(&legacyUsers).Error
	if err != nil {
		log.Fatalf("Failed to query legacy users: %v", err)
	}

	if len(legacyUsers) == 0 {
		fmt.Println("No legacy OAuth users found to migrate. Migration complete.")
		return
	}

	fmt.Printf("Found %d legacy OAuth users to migrate.\n", len(legacyUsers))

	tx := db.Begin()
	migratedCount := 0

	for _, lu := range legacyUsers {
		// Check if a connection already exists to prevent duplicates
		var count int64
		tx.Model(&models.UserOAuthConnection{}).Where("user_id = ? AND provider = ?", lu.ID, lu.Provider).Count(&count)

		if count == 0 {
			// Create the connection record
			conn := models.UserOAuthConnection{
				ID:                utils.GenerateID(),
				UserID:            lu.ID,
				Provider:          lu.Provider,
				ProviderAccountID: lu.ProviderID,
				ProviderEmail:     lu.Email, // We assume the business email is the same as provider email for legacy users
			}

			if err := tx.Create(&conn).Error; err != nil {
				tx.Rollback()
				log.Fatalf("Failed to create connection for user %s: %v", lu.ID, err)
			}
			migratedCount++
			fmt.Printf("Migrated user %s (%s)\n", lu.Email, lu.Provider)
		}
	}

	// 4. (Optional but recommended) Nullify the legacy columns to prevent confusion
	// We don't drop them immediately in case rollback is needed, but we clear them.
	if migratedCount > 0 {
		if err := tx.Exec("UPDATE users SET provider = 'local', provider_id = NULL WHERE provider != 'local'").Error; err != nil {
			tx.Rollback()
			log.Fatalf("Failed to nullify legacy columns: %v", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Fatalf("Transaction commit failed: %v", err)
	}

	fmt.Printf("Migration successful! %d records created in user_oauth_connections.\n", migratedCount)
}
