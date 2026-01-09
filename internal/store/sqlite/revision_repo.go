package sqlite

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RevisionRepository handles revision persistence
type RevisionRepository struct {
	db *gorm.DB
}

// NewRevisionRepository creates a new revision repository
func NewRevisionRepository(db *DB) *RevisionRepository {
	return &RevisionRepository{db: db.DB}
}

// Create inserts a new revision
func (r *RevisionRepository) Create(ctx context.Context, revision *Revision) error {
	if err := r.db.WithContext(ctx).Create(revision).Error; err != nil {
		return fmt.Errorf("creating revision: %w", err)
	}
	return nil
}

// GetByID retrieves a revision by ID
func (r *RevisionRepository) GetByID(ctx context.Context, id string) (*Revision, error) {
	var revision Revision
	if err := r.db.WithContext(ctx).First(&revision, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting revision: %w", err)
	}
	return &revision, nil
}

// GetLatest retrieves the latest revision for a project
func (r *RevisionRepository) GetLatest(ctx context.Context, projectID string) (*Revision, error) {
	var revision Revision
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("version_number DESC").
		First(&revision).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting latest revision: %w", err)
	}
	return &revision, nil
}

// GetByVersion retrieves a specific version for a project
func (r *RevisionRepository) GetByVersion(ctx context.Context, projectID string, version int) (*Revision, error) {
	var revision Revision
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND version_number = ?", projectID, version).
		First(&revision).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("getting revision by version: %w", err)
	}
	return &revision, nil
}

// ListByProject returns all revisions for a project, ordered by version descending
func (r *RevisionRepository) ListByProject(ctx context.Context, projectID string) ([]Revision, error) {
	var revisions []Revision
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("version_number DESC").
		Find(&revisions).Error; err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	return revisions, nil
}

// GetNextVersionNumber returns the next version number for a project
func (r *RevisionRepository) GetNextVersionNumber(ctx context.Context, projectID string) (int, error) {
	var maxVersion *int
	if err := r.db.WithContext(ctx).
		Model(&Revision{}).
		Where("project_id = ?", projectID).
		Select("MAX(version_number)").
		Scan(&maxVersion).Error; err != nil {
		return 0, fmt.Errorf("getting max version: %w", err)
	}
	if maxVersion == nil {
		return 1, nil
	}
	return *maxVersion + 1, nil
}

// Delete removes a revision by ID
func (r *RevisionRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&Revision{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("deleting revision: %w", err)
	}
	return nil
}
