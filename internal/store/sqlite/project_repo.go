package sqlite

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ProjectRepository handles project persistence
type ProjectRepository struct {
	db *gorm.DB
}

// NewProjectRepository creates a new project repository
func NewProjectRepository(db *DB) *ProjectRepository {
	return &ProjectRepository{db: db.DB}
}

// Create inserts a new project
func (r *ProjectRepository) Create(ctx context.Context, project *Project) error {
	if err := r.db.WithContext(ctx).Create(project).Error; err != nil {
		return fmt.Errorf("creating project: %w", err)
	}
	return nil
}

// GetByID retrieves a project by ID
func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).First(&project, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting project: %w", err)
	}
	return &project, nil
}

// GetByName retrieves a project by name
func (r *ProjectRepository) GetByName(ctx context.Context, name string) (*Project, error) {
	var project Project
	if err := r.db.WithContext(ctx).First(&project, "name = ?", name).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting project by name: %w", err)
	}
	return &project, nil
}

// Update updates an existing project
func (r *ProjectRepository) Update(ctx context.Context, project *Project) error {
	if err := r.db.WithContext(ctx).Save(project).Error; err != nil {
		return fmt.Errorf("updating project: %w", err)
	}
	return nil
}

// Delete removes a project by ID
func (r *ProjectRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&Project{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	return nil
}

// List returns all projects
func (r *ProjectRepository) List(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := r.db.WithContext(ctx).Find(&projects).Error; err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	return projects, nil
}

// SetCurrentRevision updates the current revision for a project
func (r *ProjectRepository) SetCurrentRevision(ctx context.Context, projectID, revisionID string) error {
	if err := r.db.WithContext(ctx).Model(&Project{}).
		Where("id = ?", projectID).
		Update("current_revision_id", revisionID).Error; err != nil {
		return fmt.Errorf("setting current revision: %w", err)
	}
	return nil
}
