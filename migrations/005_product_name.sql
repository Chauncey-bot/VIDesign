-- Optional product name selected for a generated proposal.
ALTER TABLE design_proposals ADD COLUMN product_name VARCHAR(255) NULL AFTER round_no;
