package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"packaging-ai/internal/agent"
	"packaging-ai/internal/auth"
	"packaging-ai/internal/domain"
)

var ErrNotFound = errors.New("not found")

type ProjectRepository interface {
	Create(ctx context.Context, project domain.Project) error
	Get(ctx context.Context, id string) (domain.Project, error)
}

type BriefRepository interface {
	GetLatest(ctx context.Context, projectID string) (domain.Brief, error)
	Upsert(ctx context.Context, brief domain.Brief) error
}

type ProposalRepository interface {
	Create(ctx context.Context, proposal domain.DesignProposal) error
	List(ctx context.Context, projectID string, round int) ([]domain.DesignProposal, error)
	UpdateScore(ctx context.Context, proposalID string, score int) error
}

type FeedbackRepository interface {
	Create(ctx context.Context, feedback domain.Feedback) error
}

type MySQLRepository struct{ db *sql.DB }

func NewMySQL(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

func NewID() string { return newID() }

func (r *MySQLRepository) EnsureAuthSchema(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
		id CHAR(36) NOT NULL, customer_id CHAR(36) NOT NULL, email VARCHAR(320) NOT NULL,
		password_hash VARCHAR(255) NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		PRIMARY KEY (id), UNIQUE KEY uq_users_email (email), KEY idx_users_customer (customer_id),
		CONSTRAINT fk_users_customer FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return err
	}
	// Image providers may return base64 data URLs larger than TEXT's 64 KiB
	// limit. Keep the asset URL column compatible with both remote URLs and
	// embedded image data returned by OpenAI-compatible providers.
	_, err = r.db.ExecContext(ctx, `ALTER TABLE assets MODIFY storage_url LONGTEXT NOT NULL`)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `ALTER TABLE design_proposals ADD COLUMN customer_score TINYINT NULL`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err = r.db.ExecContext(ctx, `ALTER TABLE design_proposals ADD COLUMN product_name VARCHAR(255) NULL`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS design_fusions (
		id CHAR(36) NOT NULL, project_id CHAR(36) NOT NULL, name VARCHAR(255) NOT NULL,
		status VARCHAR(64) NOT NULL DEFAULT 'draft', source_proposal_ids JSON NOT NULL,
		source_features JSON NOT NULL, feature_selections JSON NOT NULL, weights JSON NOT NULL,
		constraints_json JSON NOT NULL, synthesis_brief JSON NOT NULL, output_proposal_ids JSON NOT NULL,
		version INT NOT NULL DEFAULT 1, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		PRIMARY KEY (id), KEY idx_fusions_project (project_id, created_at),
		CONSTRAINT fk_fusions_project FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS intake_submissions (
		id CHAR(36) NOT NULL, project_id CHAR(36) NOT NULL, task_id CHAR(36) NOT NULL,
		message LONGTEXT NOT NULL, document_names JSON NOT NULL, brand_file_names JSON NOT NULL,
		created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), PRIMARY KEY (id),
		KEY idx_intake_submissions_project (project_id, created_at),
		CONSTRAINT fk_intake_submissions_project FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `ALTER TABLE design_proposals ADD COLUMN fusion_id CHAR(36) NULL, ADD KEY idx_proposals_fusion (fusion_id)`)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		// MySQL reports duplicate key when the column already exists as well as
		// when the index is already present. Existing installations are safe.
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return err
		}
	}
	return nil
}

func (r *MySQLRepository) CreateIntakeSubmission(ctx context.Context, submission domain.IntakeSubmission) error {
	documents, err := json.Marshal(submission.DocumentNames)
	if err != nil {
		return err
	}
	brands, err := json.Marshal(submission.BrandFileNames)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO intake_submissions (id, project_id, task_id, message, document_names, brand_file_names) VALUES (?, ?, ?, ?, ?, ?)`, submission.ID, submission.ProjectID, submission.TaskID, submission.Message, documents, brands)
	return err
}

func (r *MySQLRepository) ListIntakeSubmissions(ctx context.Context, projectID string) ([]domain.IntakeSubmission, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, project_id, task_id, message, document_names, brand_file_names, created_at FROM intake_submissions WHERE project_id = ? ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.IntakeSubmission, 0)
	for rows.Next() {
		var submission domain.IntakeSubmission
		var documents, brands []byte
		if err := rows.Scan(&submission.ID, &submission.ProjectID, &submission.TaskID, &submission.Message, &documents, &brands, &submission.CreatedAt); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(documents, &submission.DocumentNames); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(brands, &submission.BrandFileNames); err != nil {
			return nil, err
		}
		result = append(result, submission)
	}
	return result, rows.Err()
}

func (r *MySQLRepository) CreateUser(ctx context.Context, email, passwordHash string) (auth.User, error) {
	customerID, userID := newID(), newID()
	if _, err := r.db.ExecContext(ctx, `INSERT INTO customers (id, name, email) VALUES (?, ?, ?)`, customerID, email, email); err != nil {
		return auth.User{}, err
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO users (id, customer_id, email, password_hash) VALUES (?, ?, ?, ?)`, userID, customerID, email, passwordHash); err != nil {
		return auth.User{}, err
	}
	return auth.User{ID: userID, CustomerID: customerID, Email: email}, nil
}

func (r *MySQLRepository) FindUserByEmail(ctx context.Context, email string) (auth.User, string, error) {
	var user auth.User
	var hash string
	err := r.db.QueryRowContext(ctx, `SELECT id, customer_id, email, password_hash FROM users WHERE email = ?`, email).Scan(&user.ID, &user.CustomerID, &user.Email, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, "", ErrNotFound
	}
	return user, hash, err
}

func (r *MySQLRepository) Create(ctx context.Context, p domain.Project) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO projects
		(id, customer_id, name, status, current_round, created_at, updated_at)
		VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?)`, p.ID, p.CustomerID, p.Name, p.Status, p.CurrentRound, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *MySQLRepository) Get(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	var customerID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT id, customer_id, name, status, current_round, created_at, updated_at
		FROM projects WHERE id = ?`, id).Scan(&p.ID, &customerID, &p.Name, &p.Status, &p.CurrentRound, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, ErrNotFound
	}
	if err != nil {
		return domain.Project{}, err
	}
	p.CustomerID = customerID.String
	return p, nil
}

func (r *MySQLRepository) ListProjects(ctx context.Context, customerID string) ([]domain.Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, customer_id, name, status, current_round, created_at, updated_at FROM projects WHERE customer_id = ? ORDER BY updated_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]domain.Project, 0)
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.Name, &p.Status, &p.CurrentRound, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (r *MySQLRepository) GetForCustomer(ctx context.Context, id, customerID string) (domain.Project, error) {
	var p domain.Project
	err := r.db.QueryRowContext(ctx, `SELECT id, customer_id, name, status, current_round, created_at, updated_at FROM projects WHERE id = ? AND customer_id = ?`, id, customerID).Scan(&p.ID, &p.CustomerID, &p.Name, &p.Status, &p.CurrentRound, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, ErrNotFound
	}
	return p, err
}

func (r *MySQLRepository) UpdateStatus(ctx context.Context, id string, status domain.ProjectStatus) error {
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET status = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) UpdateRoundAndStatus(ctx context.Context, id string, round int, status domain.ProjectStatus) error {
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET current_round = ?, status = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, round, status, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) CreateAgentRun(ctx context.Context, projectID string, run agent.RunOutput) error {
	output, err := marshalJSON(run.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO agent_runs
		(id, project_id, agent_type, input_json, output_json, status, provider, model, input_tokens, output_tokens, duration_ms)
		VALUES (?, ?, ?, ?, ?, 'running', ?, ?, ?, ?, ?)`,
		newID(), projectID, run.AgentName, []byte(`{}`), output, run.Usage.Provider, run.Usage.Model,
		run.Usage.InputTokens, run.Usage.OutputTokens, run.Usage.DurationMillis)
	return err
}

func (r *MySQLRepository) CompleteAgentRun(ctx context.Context, projectID, agentName string, payload map[string]any) error {
	output, err := marshalJSON(payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE agent_runs SET output_json = ?, status = 'succeeded', completed_at = CURRENT_TIMESTAMP(3)
		WHERE project_id = ? AND agent_type = ? AND status = 'running' ORDER BY created_at DESC LIMIT 1`, output, projectID, agentName)
	return err
}

func (r *MySQLRepository) GetLatest(ctx context.Context, projectID string) (domain.Brief, error) {
	var b domain.Brief
	var productInfo, brandInfo, packageSpec, requiredText, constraints []byte
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, product_info, brand_info, audience, market,
		package_spec, material, printing_process, required_text, constraints_json, completion_score, version, created_at, updated_at
		FROM briefs WHERE project_id = ? ORDER BY version DESC LIMIT 1`, projectID).Scan(
		&b.ID, &b.ProjectID, &productInfo, &brandInfo, &b.Audience, &b.Market, &packageSpec,
		&b.Material, &b.PrintingProcess, &requiredText, &constraints, &b.CompletionScore, &b.Version, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Brief{}, ErrNotFound
	}
	if err != nil {
		return domain.Brief{}, err
	}
	if err := unmarshalJSON(productInfo, &b.ProductInfo); err != nil {
		return domain.Brief{}, err
	}
	if err := unmarshalJSON(brandInfo, &b.BrandInfo); err != nil {
		return domain.Brief{}, err
	}
	if err := unmarshalJSON(packageSpec, &b.PackageSpec); err != nil {
		return domain.Brief{}, err
	}
	if err := unmarshalJSON(requiredText, &b.RequiredText); err != nil {
		return domain.Brief{}, err
	}
	if err := unmarshalJSON(constraints, &b.Constraints); err != nil {
		return domain.Brief{}, err
	}
	return b, nil
}

// ReconcileLatestBrief upgrades older or Agent-produced rich product_info into
// the stable fields used by scoring and the UI, then persists a new snapshot
// when the normalized values change.
func (r *MySQLRepository) ReconcileLatestBrief(ctx context.Context, projectID string) (domain.Brief, error) {
	brief, err := r.GetLatest(ctx, projectID)
	if err != nil {
		return domain.Brief{}, err
	}
	if !normalizeBriefSnapshot(&brief) {
		return brief, nil
	}
	brief.ID = newID()
	brief.Version++
	if err := r.Upsert(ctx, brief); err != nil {
		return domain.Brief{}, err
	}
	return brief, nil
}

func normalizeBriefSnapshot(brief *domain.Brief) bool {
	changed := false
	if brief.ProductInfo == nil {
		brief.ProductInfo = map[string]any{}
	}
	// Agent/document intake may use the human-readable keys below. Copy them
	// into the stable fields consumed by the UI, scorer and design workflow.
	if briefString(brief.ProductInfo["product_category"]) == "" {
		if value := briefString(brief.ProductInfo["category"]); value != "" {
			brief.ProductInfo["product_category"], changed = value, true
		}
	}
	if brief.Audience == "" {
		for _, key := range []string{"target_customers", "target_customer"} {
			value := briefString(brief.ProductInfo[key])
			if value == "" {
				continue
			}
			brief.Audience, changed = value, true
			break
		}
	}
	if brief.Market == "" {
		for _, key := range []string{"market", "gifting_scenario", "usage_scenarios"} {
			if value := briefString(brief.ProductInfo[key]); value != "" {
				brief.Market, changed = value, true
				break
			}
		}
	}
	if value := brief.ProductInfo["packaging_type"]; value != nil && brief.ProductInfo["package_type"] == nil {
		brief.ProductInfo["package_type"] = value
		changed = true
	}
	if len(brief.RequiredText) == 0 {
		for _, key := range []string{"required_packaging_text", "mandatory_packaging_text", "mandatory_text"} {
			if values := briefTextValues(brief.ProductInfo[key]); len(values) > 0 {
				brief.RequiredText, changed = values, true
				break
			}
		}
	}
	if strings.TrimSpace(brief.Material) == "" {
		for _, key := range []string{"material_requirement", "material"} {
			if value := briefString(brief.ProductInfo[key]); value != "" {
				brief.Material, changed = value, true
				break
			}
		}
	}
	if len(brief.Constraints) == 0 {
		if values := briefTextValues(brief.ProductInfo["regulatory_requirements"]); len(values) > 0 {
			brief.Constraints, changed = values, true
		}
	}
	if len(brief.PackageSpec) == 0 {
		if value, ok := brief.ProductInfo["package_spec"].(map[string]any); ok && len(value) > 0 {
			brief.PackageSpec, changed = value, true
		}
	}
	score := briefScore(brief)
	if score != brief.CompletionScore {
		brief.CompletionScore, changed = score, true
	}
	return changed
}

func briefScore(brief *domain.Brief) int {
	filled := 0
	if briefString(brief.ProductInfo["product_category"]) != "" {
		filled++
	}
	if briefString(brief.ProductInfo["product_name"]) != "" {
		filled++
	}
	if strings.TrimSpace(brief.Audience) != "" {
		filled++
	}
	if briefString(brief.ProductInfo["package_type"]) != "" {
		filled++
	}
	if len(brief.RequiredText) > 0 {
		filled++
	}
	if strings.TrimSpace(brief.Material) != "" {
		filled++
	}
	if len(brief.PackageSpec) > 0 {
		filled++
	}
	return filled * 100 / 7
}

func briefString(value any) string {
	if value == nil {
		return ""
	}
	if values, ok := value.([]any); ok {
		parts := make([]string, 0, len(values))
		for _, item := range values {
			if text := briefString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "、")
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func briefTextValues(value any) []string {
	if value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		values := make([]string, 0, len(items))
		for _, item := range items {
			if text := briefString(item); text != "" {
				values = append(values, text)
			}
		}
		return values
	}
	if items, ok := value.(map[string]any); ok {
		values := make([]string, 0, len(items))
		for key, item := range items {
			text := briefString(item)
			if text == "" {
				text = key
			} else {
				text = key + "：" + text
			}
			values = append(values, text)
		}
		return values
	}
	if text := briefString(value); text != "" {
		return []string{text}
	}
	return nil
}

func (r *MySQLRepository) Upsert(ctx context.Context, b domain.Brief) error {
	productInfo, err := marshalJSON(b.ProductInfo)
	if err != nil {
		return err
	}
	brandInfo, err := marshalJSON(b.BrandInfo)
	if err != nil {
		return err
	}
	packageSpec, err := marshalJSON(b.PackageSpec)
	if err != nil {
		return err
	}
	requiredText, err := marshalJSON(b.RequiredText)
	if err != nil {
		return err
	}
	constraints, err := marshalJSON(b.Constraints)
	if err != nil {
		return err
	}
	if b.ID == "" {
		b.ID = newID()
	}
	if b.Version == 0 {
		b.Version = 1
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO briefs
		(id, project_id, product_info, brand_info, audience, market, package_spec, material, printing_process,
		required_text, constraints_json, completion_score, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3))`,
		b.ID, b.ProjectID, productInfo, brandInfo, b.Audience, b.Market, packageSpec, b.Material, b.PrintingProcess,
		requiredText, constraints, b.CompletionScore, b.Version)
	return err
}

func (r *MySQLRepository) CreateProposal(ctx context.Context, p domain.DesignProposal) error {
	strategy, err := marshalJSON(p.DesignStrategy)
	if err != nil {
		return err
	}
	palette, err := marshalJSON(p.ColorPalette)
	if err != nil {
		return err
	}
	if p.ID == "" {
		p.ID = newID()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO design_proposals
		(id, project_id, round_no, product_name, title, concept, design_strategy, color_palette, typography, difference_summary, status, fusion_id, created_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), CURRENT_TIMESTAMP(3))`, p.ID, p.ProjectID, p.Round, p.ProductName, p.Title, p.Concept, strategy, palette, p.Typography, p.Difference, p.Status, p.FusionID)
	return err
}

func (r *MySQLRepository) CreateAsset(ctx context.Context, asset domain.Asset) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, storage_url, width, height, prompt, provider, model, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, asset.ID, asset.ProjectID, asset.AssetType, asset.StorageURL, asset.Width, asset.Height, asset.Prompt, asset.Provider, asset.Model, asset.Version)
	return err
}

func (r *MySQLRepository) LinkProposalAsset(ctx context.Context, proposalID, assetID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO proposal_assets (proposal_id, asset_id) VALUES (?, ?)`, proposalID, assetID)
	return err
}

func (r *MySQLRepository) GetAsset(ctx context.Context, id string) (domain.Asset, error) {
	var asset domain.Asset
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, asset_type, storage_url, width, height, prompt, provider, model, version, created_at FROM assets WHERE id = ?`, id).
		Scan(&asset.ID, &asset.ProjectID, &asset.AssetType, &asset.StorageURL, &asset.Width, &asset.Height, &asset.Prompt, &asset.Provider, &asset.Model, &asset.Version, &asset.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Asset{}, ErrNotFound
	}
	if err != nil {
		return domain.Asset{}, err
	}
	return asset, nil
}

func (r *MySQLRepository) GetAssets(ctx context.Context, ids []string) ([]domain.Asset, error) {
	if len(ids) == 0 {
		return []domain.Asset{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, project_id, asset_type, storage_url, width, height, prompt, provider, model, version, created_at FROM assets WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	assets := make([]domain.Asset, 0)
	for rows.Next() {
		var asset domain.Asset
		if err := rows.Scan(&asset.ID, &asset.ProjectID, &asset.AssetType, &asset.StorageURL, &asset.Width, &asset.Height, &asset.Prompt, &asset.Provider, &asset.Model, &asset.Version, &asset.CreatedAt); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (r *MySQLRepository) ListProposals(ctx context.Context, projectID string, round int) ([]domain.DesignProposal, error) {
	query := `SELECT id, project_id, round_no, COALESCE(product_name, ''), title, concept, design_strategy, color_palette, typography, difference_summary, status, customer_score, COALESCE(fusion_id, ''), created_at FROM design_proposals WHERE project_id = ?`
	args := []any{projectID}
	if round > 0 {
		query += " AND round_no = ?"
		args = append(args, round)
	}
	query += " ORDER BY round_no DESC, created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.DesignProposal, 0)
	for rows.Next() {
		var p domain.DesignProposal
		var strategy, palette []byte
		var score sql.NullInt64
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Round, &p.ProductName, &p.Title, &p.Concept, &strategy, &palette, &p.Typography, &p.Difference, &p.Status, &score, &p.FusionID, &p.CreatedAt); err != nil {
			return nil, err
		}
		if score.Valid {
			value := int(score.Int64)
			p.CustomerScore = &value
		}
		if err := unmarshalJSON(strategy, &p.DesignStrategy); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(palette, &p.ColorPalette); err != nil {
			return nil, err
		}
		assets, err := r.listProposalAssets(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		p.Assets = assets
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *MySQLRepository) GetProposal(ctx context.Context, id string) (domain.DesignProposal, error) {
	var p domain.DesignProposal
	var strategy, palette []byte
	var score sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, round_no, COALESCE(product_name, ''), title, concept, design_strategy, color_palette, typography, difference_summary, status, customer_score, COALESCE(fusion_id, ''), created_at
		FROM design_proposals WHERE id = ?`, id).Scan(&p.ID, &p.ProjectID, &p.Round, &p.ProductName, &p.Title, &p.Concept, &strategy, &palette, &p.Typography, &p.Difference, &p.Status, &score, &p.FusionID, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DesignProposal{}, ErrNotFound
	}
	if err != nil {
		return domain.DesignProposal{}, err
	}
	if score.Valid {
		value := int(score.Int64)
		p.CustomerScore = &value
	}
	if err := unmarshalJSON(strategy, &p.DesignStrategy); err != nil {
		return domain.DesignProposal{}, err
	}
	if err := unmarshalJSON(palette, &p.ColorPalette); err != nil {
		return domain.DesignProposal{}, err
	}
	p.Assets, err = r.listProposalAssets(ctx, p.ID)
	if err != nil {
		return domain.DesignProposal{}, err
	}
	return p, nil
}

func (r *MySQLRepository) UpdateProposalScore(ctx context.Context, proposalID string, score int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE design_proposals SET customer_score = ? WHERE id = ?`, score, proposalID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) listProposalAssets(ctx context.Context, proposalID string) ([]domain.Asset, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT a.id, a.project_id, a.asset_type, a.storage_url, a.width, a.height, a.prompt, a.provider, a.model, a.version, a.created_at
		FROM assets a JOIN proposal_assets pa ON pa.asset_id = a.id WHERE pa.proposal_id = ? ORDER BY a.created_at`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []domain.Asset
	for rows.Next() {
		var asset domain.Asset
		if err := rows.Scan(&asset.ID, &asset.ProjectID, &asset.AssetType, &asset.StorageURL, &asset.Width, &asset.Height, &asset.Prompt, &asset.Provider, &asset.Model, &asset.Version, &asset.CreatedAt); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (r *MySQLRepository) CreateFeedback(ctx context.Context, f domain.Feedback) error {
	liked, err := marshalJSON(f.LikedItems)
	if err != nil {
		return err
	}
	disliked, err := marshalJSON(f.DislikedItems)
	if err != nil {
		return err
	}
	must, err := marshalJSON(f.MustChange)
	if err != nil {
		return err
	}
	optional, err := marshalJSON(f.OptionalChange)
	if err != nil {
		return err
	}
	if f.ID == "" {
		f.ID = newID()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO feedback
		(id, project_id, proposal_id, rating, comment, liked_items, disliked_items, must_change, optional_change)
		VALUES (?, ?, NULLIF(?, ''), NULLIF(?, 0), ?, ?, ?, ?, ?)`, f.ID, f.ProjectID, f.ProposalID, f.Rating, f.Comment, liked, disliked, must, optional)
	return err
}

func (r *MySQLRepository) CreateFusion(ctx context.Context, f domain.DesignFusion) error {
	sourceIDs, err := marshalJSON(f.SourceProposalIDs)
	if err != nil {
		return err
	}
	sourceFeatures, err := marshalJSON(f.SourceFeatures)
	if err != nil {
		return err
	}
	selections, err := marshalJSON(f.FeatureSelections)
	if err != nil {
		return err
	}
	weights, err := marshalJSON(f.Weights)
	if err != nil {
		return err
	}
	constraints, err := marshalJSON(f.Constraints)
	if err != nil {
		return err
	}
	brief, err := marshalJSON(f.SynthesisBrief)
	if err != nil {
		return err
	}
	outputs, err := marshalJSON(f.OutputProposalIDs)
	if err != nil {
		return err
	}
	if f.ID == "" {
		f.ID = newID()
	}
	if f.Version == 0 {
		f.Version = 1
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO design_fusions
		(id, project_id, name, status, source_proposal_ids, source_features, feature_selections, weights, constraints_json, synthesis_brief, output_proposal_ids, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.ID, f.ProjectID, f.Name, f.Status, sourceIDs, sourceFeatures, selections, weights, constraints, brief, outputs, f.Version)
	return err
}

func (r *MySQLRepository) GetFusion(ctx context.Context, id string) (domain.DesignFusion, error) {
	var f domain.DesignFusion
	var sourceIDs, sourceFeatures, selections, weights, constraints, brief, outputs []byte
	err := r.db.QueryRowContext(ctx, `SELECT id, project_id, name, status, source_proposal_ids, source_features, feature_selections, weights, constraints_json, synthesis_brief, output_proposal_ids, version, created_at, updated_at FROM design_fusions WHERE id = ?`, id).
		Scan(&f.ID, &f.ProjectID, &f.Name, &f.Status, &sourceIDs, &sourceFeatures, &selections, &weights, &constraints, &brief, &outputs, &f.Version, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DesignFusion{}, ErrNotFound
	}
	if err != nil {
		return domain.DesignFusion{}, err
	}
	if err := unmarshalJSON(sourceIDs, &f.SourceProposalIDs); err != nil {
		return f, err
	}
	if err := unmarshalJSON(sourceFeatures, &f.SourceFeatures); err != nil {
		return f, err
	}
	if err := unmarshalJSON(selections, &f.FeatureSelections); err != nil {
		return f, err
	}
	if err := unmarshalJSON(weights, &f.Weights); err != nil {
		return f, err
	}
	if err := unmarshalJSON(constraints, &f.Constraints); err != nil {
		return f, err
	}
	if err := unmarshalJSON(brief, &f.SynthesisBrief); err != nil {
		return f, err
	}
	if err := unmarshalJSON(outputs, &f.OutputProposalIDs); err != nil {
		return f, err
	}
	return f, nil
}

func (r *MySQLRepository) ListFusions(ctx context.Context, projectID string) ([]domain.DesignFusion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM design_fusions WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	result := make([]domain.DesignFusion, 0, len(ids))
	for _, id := range ids {
		fusion, err := r.GetFusion(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, fusion)
	}
	return result, rows.Err()
}

func (r *MySQLRepository) UpdateFusion(ctx context.Context, f domain.DesignFusion) error {
	sourceIDs, err := marshalJSON(f.SourceProposalIDs)
	if err != nil {
		return err
	}
	sourceFeatures, err := marshalJSON(f.SourceFeatures)
	if err != nil {
		return err
	}
	selections, err := marshalJSON(f.FeatureSelections)
	if err != nil {
		return err
	}
	weights, err := marshalJSON(f.Weights)
	if err != nil {
		return err
	}
	constraints, err := marshalJSON(f.Constraints)
	if err != nil {
		return err
	}
	brief, err := marshalJSON(f.SynthesisBrief)
	if err != nil {
		return err
	}
	outputs, err := marshalJSON(f.OutputProposalIDs)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE design_fusions SET name=?, status=?, source_proposal_ids=?, source_features=?, feature_selections=?, weights=?, constraints_json=?, synthesis_brief=?, output_proposal_ids=?, version=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=?`, f.Name, f.Status, sourceIDs, sourceFeatures, selections, weights, constraints, brief, outputs, f.Version, f.ID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func marshalJSON(value any) ([]byte, error) { return json.Marshal(value) }

func unmarshalJSON(data []byte, target any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("generate id: %w", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b)
	return strings.Join([]string{encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]}, "-")
}
