-- Migration 002: Add IDE-specific fields to pulse.pods
-- repo_url: the git repository cloned on startup
-- resource_tier: human-readable tier label ("small"|"medium"|"large")

ALTER TABLE pulse.pods
    ADD COLUMN IF NOT EXISTS repo_url      VARCHAR(500),
    ADD COLUMN IF NOT EXISTS resource_tier VARCHAR(20) DEFAULT 'medium';

-- Index for quickly finding pods associated with a given repository
CREATE INDEX IF NOT EXISTS idx_pods_repo_url
    ON pulse.pods(repo_url)
    WHERE deleted_at IS NULL;

COMMENT ON COLUMN pulse.pods.repo_url IS 'Git repository URL cloned into /home/coder/workspace on pod startup';
COMMENT ON COLUMN pulse.pods.resource_tier IS 'Named resource tier: small (1 CPU/2 GB), medium (2 CPU/4 GB), large (4 CPU/8 GB)';
