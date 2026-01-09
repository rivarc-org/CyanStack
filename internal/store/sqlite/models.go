package sqlite

import (
	"time"
)

// Project represents a CyanStack project
type Project struct {
	ID                string    `gorm:"primaryKey;size:36"`
	Name              string    `gorm:"size:255;not null"`
	CurrentRevisionID *string   `gorm:"size:36"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`

	// Relations
	Revisions []Revision `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Secrets   []Secret   `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for Project
func (Project) TableName() string {
	return "projects"
}

// Revision represents a versioned snapshot of project state
type Revision struct {
	ID            string    `gorm:"primaryKey;size:36"`
	ProjectID     string    `gorm:"size:36;not null;index"`
	VersionNumber int       `gorm:"not null"`
	GraphState    string    `gorm:"type:text;not null"` // JSON-serialized node graph
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	// Relations
	Project Project `gorm:"foreignKey:ProjectID"`
}

// TableName specifies the table name for Revision
func (Revision) TableName() string {
	return "revisions"
}

// Secret represents an encrypted project secret
type Secret struct {
	ProjectID      string    `gorm:"size:36;not null;primaryKey"`
	PluginID       string    `gorm:"size:255;not null;primaryKey"`
	KeyName        string    `gorm:"size:255;not null;primaryKey"`
	EncryptedValue string    `gorm:"type:text;not null"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

// TableName specifies the table name for Secret
func (Secret) TableName() string {
	return "project_secrets"
}
