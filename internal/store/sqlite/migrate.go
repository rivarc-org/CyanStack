package sqlite

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// schemaVersion tracks applied migrations
type schemaVersion struct {
	Version int  `gorm:"primaryKey"`
	Dirty   bool `gorm:"not null;default:false"`
}

func (schemaVersion) TableName() string {
	return "schema_migrations"
}

// RunMigrations applies all pending database migrations
func RunMigrations(db *DB) error {
	// Ensure schema_migrations table exists
	if err := db.AutoMigrate(&schemaVersion{}); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Get current version
	var current schemaVersion
	db.First(&current)

	// Read and apply migrations
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		// Extract version from filename (e.g., "001_initial_schema.up.sql" -> 1)
		var version int
		_, err := fmt.Sscanf(entry.Name(), "%d_", &version)
		if err != nil {
			continue
		}

		// Skip if already applied
		if version <= current.Version {
			continue
		}

		// Read migration file
		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}

		// Mark as dirty before applying
		db.Save(&schemaVersion{Version: version, Dirty: true})

		// Execute migration (split by semicolons to handle multiple statements)
		statements := strings.Split(string(content), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("executing migration %s: %w", entry.Name(), err)
			}
		}

		// Mark as clean
		db.Save(&schemaVersion{Version: version, Dirty: false})
		current.Version = version
	}

	return nil
}

// MigrationVersion returns the current migration version
func MigrationVersion(db *DB) (uint, bool, error) {
	// Ensure schema_migrations table exists
	if err := db.AutoMigrate(&schemaVersion{}); err != nil {
		return 0, false, fmt.Errorf("creating schema_migrations table: %w", err)
	}

	var current schemaVersion
	if err := db.First(&current).Error; err != nil {
		// No migrations applied yet
		return 0, false, nil
	}

	return uint(current.Version), current.Dirty, nil
}
