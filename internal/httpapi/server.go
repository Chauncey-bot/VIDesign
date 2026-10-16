package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"packaging-ai/internal/auth"
	"packaging-ai/internal/docx"
	"packaging-ai/internal/domain"
	"packaging-ai/internal/export"
	"packaging-ai/internal/repository"
	"packaging-ai/internal/taskqueue"
	"packaging-ai/internal/workflow"
)

type Server struct {
	db    *sql.DB
	repo  *repository.MySQLRepository
	queue *taskqueue.Queue
	auth  *auth.Manager
}

func NewServer(db *sql.DB, queue *taskqueue.Queue, authManager *auth.Manager) *Server {
	return &Server{db: db, repo: repository.NewMySQL(db), queue: queue, auth: authManager}
}

func (s *Server) Close() {
	if s.queue != nil {
		s.queue.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/v1/auth/register", s.register)
	mux.HandleFunc("/api/v1/auth/login", s.login)
	mux.Handle("/api/v1/projects", s.requireAuth(http.HandlerFunc(s.projects)))
	mux.Handle("/api/v1/projects/", s.requireAuth(http.HandlerFunc(s.projectRoutes)))
	mux.Handle("/api/v1/proposals/", s.requireAuth(http.HandlerFunc(s.proposalRoutes)))
	mux.Handle("/api/v1/tasks/", s.requireAuth(http.HandlerFunc(s.taskRoutes)))
	mux.Handle("/api/v1/assets/", s.requireAuth(http.HandlerFunc(s.assetRoutes)))
	mux.Handle("/", http.FileServer(http.Dir("web")))
	return logging(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if s.db != nil && s.db.PingContext(r.Context()) != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if r.Method == http.MethodGet {
		projects, err := s.repo.ListProjects(r.Context(), user.CustomerID)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, projects)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	var req struct {
		Name       string `json:"name"`
		CustomerID string `json:"customer_id"`
	}
	if err := decodeBody(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	now := time.Now().UTC()
	project := domain.Project{ID: repository.NewID(), CustomerID: user.CustomerID, Name: req.Name, Status: domain.ProjectDraft, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(r.Context(), project); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) projectRoutes(w http.ResponseWriter, r *http.Request) {
	parts := pathParts(r.URL.Path, "/api/v1/projects/")
	if len(parts) == 0 {
		writeError(w, http.StatusNotFound, "route_not_found")
		return
	}
	projectID := parts[0]
	if _, err := s.repo.GetForCustomer(r.Context(), projectID, currentUser(r).CustomerID); err != nil {
		writeDBError(w, err)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		project, err := s.repo.Get(r.Context(), projectID)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
		return
	}
	if len(parts) == 2 && parts[1] == "chat" && r.Method == http.MethodPost {
		message, documentText, assetIDs, brandAssetIDs, err := s.parseChatRequest(r, projectID)
		if err != nil || strings.TrimSpace(message) == "" {
			writeError(w, http.StatusBadRequest, "message is required")
			return
		}
		payload := map[string]any{"message": message}
		if documentText != "" {
			payload["document_text"] = documentText
		}
		if len(assetIDs) > 0 {
			payload["document_asset_ids"] = assetIDs
		}
		if len(brandAssetIDs) > 0 {
			payload["brand_asset_ids"] = brandAssetIDs
		}
		task := taskqueue.Task{ID: repository.NewID(), Kind: workflow.ChatTask, ProjectID: projectID, Payload: payload}
		if err := s.queue.Submit(r.Context(), task); err != nil {
			writeError(w, http.StatusServiceUnavailable, "task_queue_unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": task.ID, "status": taskqueue.Queued, "project_id": projectID})
		return
	}
	if len(parts) == 2 && parts[1] == "generate" && r.Method == http.MethodPost {
		var req struct {
			ProposalCount int `json:"proposal_count"`
		}
		if err := decodeBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid generate request")
			return
		}
		if req.ProposalCount == 0 {
			req.ProposalCount = 9
		}
		task := taskqueue.Task{ID: repository.NewID(), Kind: workflow.GenerateTask, ProjectID: projectID, Payload: map[string]any{"proposal_count": req.ProposalCount}}
		if err := s.queue.Submit(r.Context(), task); err != nil {
			writeError(w, http.StatusServiceUnavailable, "task_queue_unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": task.ID, "status": taskqueue.Queued, "project_id": projectID})
		return
	}
	if len(parts) == 2 && parts[1] == "revision" && r.Method == http.MethodPost {
		var req struct {
			SelectedProposalID string         `json:"selected_proposal_id"`
			Feedback           map[string]any `json:"feedback"`
			Preserve           []string       `json:"preserve"`
		}
		if err := decodeBody(r, &req); err != nil || strings.TrimSpace(req.SelectedProposalID) == "" {
			writeError(w, http.StatusBadRequest, "selected_proposal_id is required")
			return
		}
		task := taskqueue.Task{ID: repository.NewID(), Kind: workflow.RevisionTask, ProjectID: projectID, Payload: map[string]any{"selected_proposal_id": req.SelectedProposalID, "feedback": req.Feedback, "preserve": req.Preserve}}
		if err := s.queue.Submit(r.Context(), task); err != nil {
			writeError(w, http.StatusServiceUnavailable, "task_queue_unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": task.ID, "status": taskqueue.Queued, "project_id": projectID})
		return
	}
	if len(parts) == 2 && parts[1] == "export" {
		s.exportProject(w, r, projectID)
		return
	}
	if len(parts) == 2 && parts[1] == "brief" {
		s.brief(w, r, projectID)
		return
	}
	if len(parts) == 2 && parts[1] == "proposals" && r.Method == http.MethodGet {
		round, _ := strconv.Atoi(r.URL.Query().Get("round"))
		proposals, err := s.repo.ListProposals(r.Context(), projectID, round)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, proposals)
		return
	}
	writeError(w, http.StatusNotFound, "route_not_found")
}

func (s *Server) parseChatRequest(r *http.Request, projectID string) (string, string, []string, []string, error) {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		var req struct {
			Message string `json:"message"`
		}
		if err := decodeBody(r, &req); err != nil {
			return "", "", nil, nil, err
		}
		return req.Message, "", nil, nil, nil
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		return "", "", nil, nil, err
	}
	message := r.FormValue("message")
	var documentText string
	assetIDs := []string{}
	documentFiles := r.MultipartForm.File["document"]
	for index, header := range documentFiles {
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".docx") {
			return "", "", nil, nil, fmt.Errorf("only .docx is supported in document field")
		}
		file, err := header.Open()
		if err != nil {
			return "", "", nil, nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 20<<20))
		_ = file.Close()
		if readErr != nil {
			return "", "", nil, nil, readErr
		}
		document, err := docx.Parse(data)
		if err != nil {
			return "", "", nil, nil, err
		}
		if text := strings.TrimSpace(document.Text); text != "" {
			if documentText != "" {
				documentText += "\n\n"
			}
			documentText += fmt.Sprintf("【需求文档 %d：%s】\n%s", index+1, header.Filename, text)
		}
		for _, image := range document.Images {
			asset := domain.Asset{ID: repository.NewID(), ProjectID: projectID, AssetType: "brief_reference_image", StorageURL: image.DataURL, Prompt: image.Name, Provider: "docx-upload", Version: 1}
			if err := s.repo.CreateAsset(r.Context(), asset); err != nil {
				return "", "", nil, nil, err
			}
			assetIDs = append(assetIDs, asset.ID)
		}
	}
	brandAssetIDs, err := s.parseBrandAsset(r, projectID)
	if err != nil {
		return "", "", nil, nil, err
	}
	return message, documentText, assetIDs, brandAssetIDs, nil
}

func (s *Server) parseBrandAsset(r *http.Request, projectID string) ([]string, error) {
	brandFiles := r.MultipartForm.File["brand_file"]
	if len(brandFiles) == 0 {
		return nil, nil
	}
	assetIDs := make([]string, 0, len(brandFiles))
	for _, header := range brandFiles {
		name := strings.TrimSpace(header.Filename)
		dot := strings.LastIndex(name, ".")
		if dot < 0 {
			return nil, fmt.Errorf("brand_file must be .ai, .eps or .svg")
		}
		ext := strings.ToLower(name[dot:])
		if ext != ".ai" && ext != ".eps" && ext != ".svg" {
			return nil, fmt.Errorf("brand_file must be .ai, .eps or .svg")
		}
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 20<<20))
		_ = file.Close()
		if readErr != nil {
			return nil, readErr
		}
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		asset := domain.Asset{ID: repository.NewID(), ProjectID: projectID, AssetType: "brand_source_file", StorageURL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), Prompt: name, Provider: "customer-upload", Model: ext, Version: 1}
		if err := s.repo.CreateAsset(r.Context(), asset); err != nil {
			return nil, err
		}
		assetIDs = append(assetIDs, asset.ID)
	}
	return assetIDs, nil
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil || strings.TrimSpace(req.Email) == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password (minimum 8 characters) are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_hash_failed")
		return
	}
	user, err := s.repo.CreateUser(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)), string(hash))
	if err != nil {
		writeError(w, http.StatusConflict, "email_already_exists")
		return
	}
	token, err := s.auth.Issue(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_issue_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": userResponse(user)})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_login")
		return
	}
	user, hash, err := s.repo.FindUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	token, err := s.auth.Issue(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_issue_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": userResponse(user)})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "authorization_required")
			return
		}
		user, err := s.auth.Parse(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

type userContextKey struct{}

func currentUser(r *http.Request) auth.User {
	user, _ := r.Context().Value(userContextKey{}).(auth.User)
	return user
}
func userResponse(user auth.User) map[string]string {
	return map[string]string{"id": user.ID, "customer_id": user.CustomerID, "email": user.Email}
}

func (s *Server) taskRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	parts := pathParts(r.URL.Path, "/api/v1/tasks/")
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "route_not_found")
		return
	}
	record, ok := s.queue.Get(parts[0])
	if !ok {
		writeError(w, http.StatusNotFound, "task_not_found")
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) brief(w http.ResponseWriter, r *http.Request, projectID string) {
	switch r.Method {
	case http.MethodGet:
		brief, err := s.repo.GetLatest(r.Context(), projectID)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, brief)
	case http.MethodPatch:
		var brief domain.Brief
		if err := decodeBody(r, &brief); err != nil {
			writeError(w, http.StatusBadRequest, "invalid brief")
			return
		}
		brief.ProjectID = projectID
		if brief.ID == "" {
			brief.ID = repository.NewID()
		}
		if err := s.repo.Upsert(r.Context(), brief); err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, brief)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (s *Server) proposalRoutes(w http.ResponseWriter, r *http.Request) {
	parts := pathParts(r.URL.Path, "/api/v1/proposals/")
	if len(parts) != 2 || parts[1] != "feedback" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "route_not_found")
		return
	}
	var feedback domain.Feedback
	if err := decodeBody(r, &feedback); err != nil {
		writeError(w, http.StatusBadRequest, "invalid feedback")
		return
	}
	feedback.ID = repository.NewID()
	feedback.ProposalID = parts[0]
	if feedback.ProjectID == "" {
		proposal, err := s.repo.GetProposal(r.Context(), feedback.ProposalID)
		if err != nil {
			writeDBError(w, err)
			return
		}
		feedback.ProjectID = proposal.ProjectID
	}
	if err := s.repo.CreateFeedback(r.Context(), feedback); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, feedback)
}

func (s *Server) assetRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	parts := pathParts(r.URL.Path, "/api/v1/assets/")
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "route_not_found")
		return
	}
	asset, err := s.repo.GetAsset(r.Context(), parts[0])
	if err != nil {
		writeDBError(w, err)
		return
	}
	if strings.HasPrefix(asset.StorageURL, "http://") || strings.HasPrefix(asset.StorageURL, "https://") {
		http.Redirect(w, r, asset.StorageURL, http.StatusTemporaryRedirect)
		return
	}
	if !strings.HasPrefix(asset.StorageURL, "data:") {
		writeError(w, http.StatusNotFound, "asset_not_downloadable")
		return
	}
	header, body, err := decodeDataURL(asset.StorageURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_decode_failed")
		return
	}
	w.Header().Set("Content-Type", header)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=asset-%s", asset.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request, projectID string) {
	data, err := s.reportData(r.Context(), projectID)
	if err != nil {
		writeDBError(w, err)
		return
	}
	if r.Method == http.MethodPost {
		writeJSON(w, http.StatusOK, map[string]any{"project_id": projectID, "downloads": []map[string]string{{"format": "pdf", "url": "/api/v1/projects/" + projectID + "/export?format=pdf"}, {"format": "markdown", "url": "/api/v1/projects/" + projectID + "/export?format=markdown"}}})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	switch r.URL.Query().Get("format") {
	case "markdown", "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=packaging-report.md")
		_, _ = w.Write(export.Markdown(data))
	case "pdf", "":
		content, err := export.PDF(data)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "pdf_generation_failed")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=packaging-report.pdf")
		_, _ = w.Write(content)
	default:
		writeError(w, http.StatusBadRequest, "unsupported_export_format")
	}
}

func (s *Server) reportData(ctx context.Context, projectID string) (export.ReportData, error) {
	project, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return export.ReportData{}, err
	}
	brief, err := s.repo.GetLatest(ctx, projectID)
	if err != nil {
		return export.ReportData{}, err
	}
	proposals, err := s.repo.ListProposals(ctx, projectID, 0)
	if err != nil {
		return export.ReportData{}, err
	}
	return export.ReportData{Project: project, Brief: brief, Proposals: proposals}, nil
}

func decodeDataURL(value string) (string, []byte, error) {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data URL")
	}
	header := strings.TrimPrefix(parts[0], "data:")
	payload := parts[1]
	if strings.Contains(header, ";base64") {
		header = strings.TrimSuffix(header, ";base64")
		data, err := base64.StdEncoding.DecodeString(payload)
		return header, data, err
	}
	data, err := url.PathUnescape(payload)
	return header, []byte(data), err
}

func pathParts(path, prefix string) []string {
	path = strings.TrimPrefix(path, prefix)
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func decodeBody(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeDBError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource_not_found")
		return
	}
	writeError(w, http.StatusInternalServerError, "database_error")
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
