-- Multi-proposal synthesis sessions. The server also creates this schema for
-- existing installations through EnsureAuthSchema.
CREATE TABLE IF NOT EXISTS design_fusions (
    id CHAR(36) NOT NULL,
    project_id CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL DEFAULT 'draft',
    source_proposal_ids JSON NOT NULL,
    source_features JSON NOT NULL,
    feature_selections JSON NOT NULL,
    weights JSON NOT NULL,
    constraints_json JSON NOT NULL,
    synthesis_brief JSON NOT NULL,
    output_proposal_ids JSON NOT NULL,
    version INT NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), KEY idx_fusions_project (project_id, created_at),
    CONSTRAINT fk_fusions_project FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE design_proposals ADD COLUMN fusion_id CHAR(36) NULL;
ALTER TABLE design_proposals ADD KEY idx_proposals_fusion (fusion_id);
