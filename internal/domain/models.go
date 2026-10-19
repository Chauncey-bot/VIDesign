package domain

import "time"

type ProjectStatus string

const (
	ProjectDraft         ProjectStatus = "draft"
	ProjectCollecting    ProjectStatus = "collecting_requirements"
	ProjectBriefReady    ProjectStatus = "brief_ready"
	ProjectGenerating    ProjectStatus = "generating_proposals"
	ProjectAwaitFeedback ProjectStatus = "awaiting_feedback"
	ProjectRevising      ProjectStatus = "revising"
	ProjectApproved      ProjectStatus = "approved"
	ProjectExporting     ProjectStatus = "exporting"
	ProjectCompleted     ProjectStatus = "completed"
)

type Project struct {
	ID           string        `json:"id"`
	CustomerID   string        `json:"customer_id"`
	Name         string        `json:"name"`
	Status       ProjectStatus `json:"status"`
	CurrentRound int           `json:"current_round"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type Brief struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"project_id"`
	ProductInfo     map[string]any `json:"product_info"`
	BrandInfo       map[string]any `json:"brand_info"`
	Audience        string         `json:"audience"`
	Market          string         `json:"market"`
	PackageSpec     map[string]any `json:"package_spec"`
	Material        string         `json:"material"`
	PrintingProcess string         `json:"printing_process"`
	RequiredText    []string       `json:"required_text"`
	Constraints     []string       `json:"constraints"`
	CompletionScore int            `json:"completion_score"`
	Version         int            `json:"version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type IntakeSubmission struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	TaskID         string    `json:"task_id"`
	Message        string    `json:"message"`
	DocumentNames  []string  `json:"document_names,omitempty"`
	BrandFileNames []string  `json:"brand_file_names,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type DesignProposal struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"project_id"`
	Round          int            `json:"round"`
	ProductName    string         `json:"product_name,omitempty"`
	Title          string         `json:"title"`
	Concept        string         `json:"concept"`
	DesignStrategy map[string]any `json:"design_strategy"`
	ColorPalette   []string       `json:"color_palette"`
	Typography     string         `json:"typography"`
	ImageAssetIDs  []string       `json:"image_asset_ids"`
	Difference     string         `json:"difference_summary"`
	Status         string         `json:"status"`
	FusionID       string         `json:"fusion_id,omitempty"`
	CustomerScore  *int           `json:"customer_score,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	Assets         []Asset        `json:"assets,omitempty"`
}

// FusionFeature is a customer-confirmed design decision extracted from one
// of the source proposals. Keeping it structured makes the synthesis auditable.
type FusionFeature struct {
	Dimension        string `json:"dimension"`
	Value            string `json:"value"`
	SourceProposalID string `json:"source_proposal_id"`
	Decision         string `json:"decision"` // keep, adapt, exclude
	Weight           int    `json:"weight"`   // 0-100
}

type DesignFusion struct {
	ID                string             `json:"id"`
	ProjectID         string             `json:"project_id"`
	Name              string             `json:"name"`
	Status            string             `json:"status"`
	SourceProposalIDs []string           `json:"source_proposal_ids"`
	SourceFeatures    map[string]any     `json:"source_features"`
	FeatureSelections []FusionFeature    `json:"feature_selections"`
	Weights           map[string]float64 `json:"weights"`
	Constraints       []string           `json:"constraints"`
	SynthesisBrief    map[string]any     `json:"synthesis_brief"`
	OutputProposalIDs []string           `json:"output_proposal_ids"`
	Version           int                `json:"version"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type Asset struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	AssetType  string    `json:"asset_type"`
	StorageURL string    `json:"storage_url"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Prompt     string    `json:"prompt"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
}

type Feedback struct {
	ID             string   `json:"id"`
	ProjectID      string   `json:"project_id"`
	ProposalID     string   `json:"proposal_id"`
	Rating         int      `json:"rating"`
	Comment        string   `json:"comment"`
	LikedItems     []string `json:"liked_items"`
	DislikedItems  []string `json:"disliked_items"`
	MustChange     []string `json:"must_change"`
	OptionalChange []string `json:"optional_change"`
}
