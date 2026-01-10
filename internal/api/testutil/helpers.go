package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// SetupTestRouter creates a Gin router in test mode
func SetupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// MakeRequest performs an HTTP request and returns the response recorder
func MakeRequest(router *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(jsonBytes)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// DecodeResponse decodes JSON response into target struct
func DecodeResponse(t *testing.T, w *httptest.ResponseRecorder, target interface{}) {
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), target))
}

// CreateTestDB creates an in-memory SQLite database with migrations applied
// Returns the database and a cleanup function
func CreateTestDB(t *testing.T) (*sqlite.DB, func()) {
	db, err := sqlite.OpenInMemory()
	require.NoError(t, err)

	err = sqlite.RunMigrations(db)
	require.NoError(t, err)

	return db, func() {
		db.Close()
	}
}

// CreateTestCrypto creates a test AESCrypto instance with a fixed key
func CreateTestCrypto(t *testing.T) *security.AESCrypto {
	// Fixed 32-byte key for deterministic testing (exactly 32 bytes)
	key := []byte("12345678901234567890123456789012")
	crypto, err := security.NewAESCrypto(key)
	require.NoError(t, err)
	return crypto
}

// CreateTestProject creates a project fixture in the database
func CreateTestProject(t *testing.T, db *sqlite.DB, id, name string) *sqlite.Project {
	project := &sqlite.Project{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now(),
	}
	repo := sqlite.NewProjectRepository(db)
	err := repo.Create(context.Background(), project)
	require.NoError(t, err)
	return project
}

// CreateTestProjectWithRevision creates a project with a current revision
func CreateTestProjectWithRevision(t *testing.T, db *sqlite.DB, projectID, projectName, revisionID string) (*sqlite.Project, *sqlite.Revision) {
	projectRepo := sqlite.NewProjectRepository(db)
	revisionRepo := sqlite.NewRevisionRepository(db)

	// Create project first (foreign key requires project to exist before revision)
	project := &sqlite.Project{
		ID:        projectID,
		Name:      projectName,
		CreatedAt: time.Now(),
	}
	err := projectRepo.Create(context.Background(), project)
	require.NoError(t, err)

	// Create revision
	revision := &sqlite.Revision{
		ID:            revisionID,
		ProjectID:     projectID,
		VersionNumber: 1,
		GraphState:    `{"nodes":[]}`,
		CreatedAt:     time.Now(),
	}
	err = revisionRepo.Create(context.Background(), revision)
	require.NoError(t, err)

	// Update project to set current revision
	err = projectRepo.SetCurrentRevision(context.Background(), projectID, revisionID)
	require.NoError(t, err)

	// Refetch project to get updated state
	project, err = projectRepo.GetByID(context.Background(), projectID)
	require.NoError(t, err)

	return project, revision
}

// CreateTestRevision creates a revision fixture in the database
func CreateTestRevision(t *testing.T, db *sqlite.DB, id, projectID string, version int, graphState string) *sqlite.Revision {
	revision := &sqlite.Revision{
		ID:            id,
		ProjectID:     projectID,
		VersionNumber: version,
		GraphState:    graphState,
		CreatedAt:     time.Now(),
	}
	repo := sqlite.NewRevisionRepository(db)
	err := repo.Create(context.Background(), revision)
	require.NoError(t, err)
	return revision
}

// AssertStatusCode asserts that the response has the expected status code
func AssertStatusCode(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int) {
	if w.Code != expectedStatus {
		t.Errorf("expected status %d, got %d. Body: %s", expectedStatus, w.Code, w.Body.String())
	}
}
