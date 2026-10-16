ALTER TABLE design_proposals
    ADD COLUMN customer_score TINYINT NULL,
    ADD CONSTRAINT chk_design_proposal_customer_score CHECK (customer_score IS NULL OR customer_score BETWEEN 0 AND 10);
