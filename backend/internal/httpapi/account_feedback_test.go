package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
)

type testUserRepository struct {
	users map[string]model.User
}

func newTestUserRepository() *testUserRepository {
	return &testUserRepository{users: make(map[string]model.User)}
}

func (repository *testUserRepository) FindUserByPhone(_ context.Context, phone string) (model.User, error) {
	user, ok := repository.users[phone]
	if !ok {
		return model.User{}, fault.NotFound()
	}
	return user, nil
}

func (repository *testUserRepository) FindUserByBusinessID(_ context.Context, id uuid.UUID) (model.User, error) {
	for _, user := range repository.users {
		if user.BusinessID == id {
			return user, nil
		}
	}
	return model.User{}, fault.NotFound()
}

func (repository *testUserRepository) FindUserPasswordHash(_ context.Context, id uuid.UUID) (model.User, error) {
	user, err := repository.FindUserByBusinessID(context.Background(), id)
	if err != nil {
		return model.User{}, err
	}
	return model.User{BaseModel: model.BaseModel{BusinessID: id}, PasswordHash: user.PasswordHash}, nil
}

func (repository *testUserRepository) CreateUser(_ context.Context, user *model.User) error {
	if _, exists := repository.users[user.Phone]; exists {
		return fault.Conflict("该手机号已注册")
	}
	now := time.Now().UTC()
	user.CreatedAt, user.UpdatedAt = now, now
	repository.users[user.Phone] = *user
	return nil
}

func (repository *testUserRepository) UpdateUserProfile(_ context.Context, id uuid.UUID, updates map[string]any) (model.User, error) {
	user, err := repository.FindUserByBusinessID(context.Background(), id)
	if err != nil {
		return model.User{}, err
	}
	if value, ok := updates["nickname"].(string); ok {
		user.Nickname = value
	}
	if value, ok := updates["avatar_key"].(*string); ok {
		user.AvatarKey = value
	}
	return user, nil
}

func (repository *testUserRepository) UpdateUserPasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	for phone, user := range repository.users {
		if user.BusinessID == id {
			user.PasswordHash = hash
			repository.users[phone] = user
			return nil
		}
	}
	return fault.NotFound()
}

type testTokenStore struct {
	userID uuid.UUID
}

func (tokens *testTokenStore) ValidateAccess(_ context.Context, raw string) (store.TokenClaims, error) {
	if raw != "valid" {
		return store.TokenClaims{}, fault.Unauthorized()
	}
	return store.TokenClaims{UserBusinessID: tokens.userID, SessionID: "session-current"}, nil
}

func (*testTokenStore) Issue(context.Context, uuid.UUID) (store.TokenPair, error) {
	return store.TokenPair{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer"}, nil
}

func (*testTokenStore) Rotate(context.Context, string) (store.TokenPair, store.TokenClaims, error) {
	return store.TokenPair{}, store.TokenClaims{}, fault.Unauthorized()
}

func (*testTokenStore) RevokeSession(context.Context, string) error                  { return nil }
func (*testTokenStore) RevokeOtherSessions(context.Context, uuid.UUID, string) error { return nil }
func (*testTokenStore) AcquireUserLock(context.Context, uuid.UUID) (func(), error) {
	return func() {}, nil
}

type testFileStorage struct {
	mu      sync.Mutex
	files   map[string][]byte
	deleted int
}

func newTestFileStorage() *testFileStorage { return &testFileStorage{files: make(map[string][]byte)} }

func (storage *testFileStorage) Store(_ context.Context, _, _ string, reader io.Reader, maximum int64) (string, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return "", err
	}
	if int64(len(content)) > maximum {
		return "", fault.InvalidRequest("too large")
	}
	key := "local_20261015_" + uuid.NewString() + ".png"
	storage.mu.Lock()
	storage.files[key] = content
	storage.mu.Unlock()
	return key, nil
}

func (storage *testFileStorage) Open(key string) (io.ReadCloser, int64, string, error) {
	storage.mu.Lock()
	content, ok := storage.files[key]
	storage.mu.Unlock()
	if !ok {
		return nil, 0, "", fault.NotFound()
	}
	return io.NopCloser(bytes.NewReader(content)), int64(len(content)), "image/png", nil
}

func (storage *testFileStorage) Delete(_ context.Context, key string) error {
	storage.mu.Lock()
	delete(storage.files, key)
	storage.deleted++
	storage.mu.Unlock()
	return nil
}

type testFeedbackRepository struct {
	records []model.Feedback
}

func (repository *testFeedbackRepository) CreateFeedback(_ context.Context, record *model.Feedback) error {
	repository.records = append(repository.records, *record)
	return nil
}

type testFeedbackLimiter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (limiter *testFeedbackLimiter) CheckFeedbackLimit(_ context.Context, ip string, limit int) (bool, error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.counts == nil {
		limiter.counts = make(map[string]int)
	}
	limiter.counts[ip]++
	return limiter.counts[ip] <= limit, nil
}

func testRouter(dependencies Dependencies) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return Router(dependencies)
}

func testRegistrationRouter(maxUpload int64) (*gin.Engine, *testUserRepository, *testFileStorage) {
	repository := newTestUserRepository()
	tokens := &testTokenStore{}
	passwords, err := security.NewPasswordManager()
	if err != nil {
		panic(err)
	}
	files := newTestFileStorage()
	router := testRouter(Dependencies{
		Auth: service.NewAuthService(repository, tokens, passwords), Tokens: tokens, Files: files,
		MaxBodyBytes: 1 << 20, MaxUploadBytes: maxUpload,
		CurrentFileStoreType: func(context.Context) (string, error) { return "local", nil },
	})
	return router, repository, files
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 240, G: 120, B: 90, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func registrationRequest(t *testing.T, router *gin.Engine, avatar []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("phone", "13800138000")
	_ = writer.WriteField("password", "password-123")
	_ = writer.WriteField("nickname", "测试用户")
	if avatar != nil {
		part, err := writer.CreateFormFile("avatar", "avatar.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(avatar)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v2/auth/register", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func feedbackRequest(t *testing.T, router *gin.Engine, content string, imageCount int, token string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("content", content)
	data := pngBytes(t)
	for index := 0; index < imageCount; index++ {
		part, err := writer.CreateFormFile("images", "feedback.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.RemoteAddr = "192.0.2.44:43000"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRegistrationRequiresV2AvatarAndValidImage(t *testing.T) {
	router, repository, files := testRegistrationRouter(64)
	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString("{}")))
	if legacy.Code != http.StatusUpgradeRequired {
		t.Fatalf("legacy registration status = %d, want 426", legacy.Code)
	}
	if response := registrationRequest(t, router, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing avatar status = %d, want 422", response.Code)
	}
	if response := registrationRequest(t, router, []byte("not an image")); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid image status = %d, want 400", response.Code)
	}
	large := bytes.Repeat([]byte("x"), 65)
	if response := registrationRequest(t, router, large); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize avatar status = %d, want 413", response.Code)
	}
	if len(repository.users) != 0 || len(files.files) != 0 {
		t.Fatal("invalid registration should not create an account or leave an uploaded file")
	}
}

func TestRegistrationStoresAvatarAndRejectsDuplicatePhone(t *testing.T) {
	router, repository, files := testRegistrationRouter(1 << 20)
	data := pngBytes(t)
	response := registrationRequest(t, router, data)
	if response.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want 201: %s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	dataObject := envelope["data"].(map[string]any)
	userObject := dataObject["user"].(map[string]any)
	if userObject["avatar_key"] == nil || userObject["avatar_key"] == "" {
		t.Fatal("registered user is missing the stored avatar key")
	}
	if _, exists := repository.users["13800138000"]; !exists {
		t.Fatal("normalized 11-digit phone was not persisted")
	}
	duplicate := registrationRequest(t, router, data)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate phone status = %d, want 409", duplicate.Code)
	}
	if len(files.files) != 1 || files.deleted != 1 {
		t.Fatalf("duplicate registration should clean its uploaded avatar: files=%d deleted=%d", len(files.files), files.deleted)
	}
}

func TestFeedbackTextImageAndRateBoundaries(t *testing.T) {
	userID := uuid.New()
	repository := &testFeedbackRepository{}
	limiter := &testFeedbackLimiter{}
	files := newTestFileStorage()
	tokens := &testTokenStore{userID: userID}
	feedback := service.NewFeedbackService(repository, limiter, 2000, 5, 5)
	router := testRouter(Dependencies{
		Tokens: tokens, Feedback: feedback, Files: files, MaxUploadBytes: 1 << 20,
		CurrentFileStoreType: func(context.Context) (string, error) { return "local", nil },
	})
	if response := feedbackRequest(t, router, string(bytes.Repeat([]byte("a"), 2000)), 0, ""); response.Code != http.StatusCreated {
		t.Fatalf("2000-character feedback status = %d, want 201", response.Code)
	}
	if response := feedbackRequest(t, router, string(bytes.Repeat([]byte("a"), 2001)), 0, ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("2001-character feedback status = %d, want 422", response.Code)
	}
	if response := feedbackRequest(t, router, "", 5, "valid"); response.Code != http.StatusCreated {
		t.Fatalf("five-image signed-in feedback status = %d, want 201", response.Code)
	}
	if response := feedbackRequest(t, router, "", 6, ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("six-image feedback status = %d, want 422", response.Code)
	}
	if response := feedbackRequest(t, router, "guest", 0, ""); response.Code != http.StatusCreated {
		t.Fatalf("guest feedback status = %d, want 201", response.Code)
	}
	if len(repository.records) != 3 {
		t.Fatalf("persisted feedback count = %d, want 3", len(repository.records))
	}
	if repository.records[1].UserID == nil || *repository.records[1].UserID != userID {
		t.Fatal("signed-in feedback must use the validated token user")
	}
	if repository.records[0].UserID != nil || repository.records[2].UserID != nil {
		t.Fatal("guest feedback must remain unassociated")
	}

	rateRepository := &testFeedbackRepository{}
	rateRouter := testRouter(Dependencies{
		Feedback: service.NewFeedbackService(rateRepository, &testFeedbackLimiter{}, 2000, 5, 5),
		Files:    newTestFileStorage(), MaxUploadBytes: 1 << 20,
		CurrentFileStoreType: func(context.Context) (string, error) { return "local", nil },
	})
	for index := 0; index < 5; index++ {
		if response := feedbackRequest(t, rateRouter, "rate check", 0, ""); response.Code != http.StatusCreated {
			t.Fatalf("rate-limit request %d status = %d, want 201", index+1, response.Code)
		}
	}
	if response := feedbackRequest(t, rateRouter, "sixth", 0, ""); response.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth request status = %d, want 429", response.Code)
	}
}
