package sqlite

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// DefaultDBName is the default database filename
	DefaultDBName = "cyanstack.db"
	// DefaultConfigDir is the default config directory name
	DefaultConfigDir = ".cyanstack"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	path string
}

// Open opens or creates the SQLite database at ~/.cyanstack/cyanstack.db
func Open() (*DB, error) {
	return OpenAt("")
}

// OpenAt opens or creates the SQLite database at the specified path.
// If path is empty, uses the default location ~/.cyanstack/cyanstack.db
func OpenAt(path string) (*DB, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting current working directory: %w", err)
		}
		path = filepath.Join(cwd, DefaultConfigDir, DefaultDBName)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	// Open database with SQLite-specific settings
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable foreign keys (disabled by default in SQLite)
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting underlying db: %w", err)
	}

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	return &DB{DB: db, path: path}, nil
}

// OpenInMemory opens an in-memory SQLite database (useful for testing)
func OpenInMemory() (*DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening in-memory database: %w", err)
	}

	// Enable foreign keys
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting underlying db: %w", err)
	}

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	return &DB{DB: db, path: ":memory:"}, nil
}

// Path returns the database file path
func (d *DB) Path() string {
	return d.path
}

// Close closes the database connection
func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return fmt.Errorf("getting underlying db: %w", err)
	}
	return sqlDB.Close()
}
