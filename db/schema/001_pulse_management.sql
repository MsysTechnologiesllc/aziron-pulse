-- Schema for aziron-pulse pod management
CREATE SCHEMA IF NOT EXISTS pulse;

-- Table for tracking user quotas
CREATE TABLE IF NOT EXISTS pulse.quotas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    tenant_id UUID,
    max_pods INTEGER NOT NULL DEFAULT 3,
    max_cpu_per_pod DECIMAL(4,2) NOT NULL DEFAULT 2.0,
    max_memory_mb_per_pod INTEGER NOT NULL DEFAULT 4096,
    max_storage_gb_per_pod INTEGER NOT NULL DEFAULT 10,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, tenant_id)
);

-- Table for tracking provisioned pods
CREATE TABLE IF NOT EXISTS pulse.pods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pulse_id VARCHAR(255) NOT NULL UNIQUE,
    user_id UUID NOT NULL,
    tenant_id UUID,
    namespace VARCHAR(255) NOT NULL,
    pod_name VARCHAR(255) NOT NULL,
    service_name VARCHAR(255) NOT NULL,
    pvc_name VARCHAR(255) NOT NULL,
    node_port INTEGER,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    base_image VARCHAR(500) NOT NULL,
    cpu_limit DECIMAL(4,2) NOT NULL,
    memory_limit_mb INTEGER NOT NULL,
    storage_gb INTEGER NOT NULL,
    workspace_path VARCHAR(500) NOT NULL,
    last_activity_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ttl_minutes INTEGER NOT NULL DEFAULT 120,
    expires_at TIMESTAMP WITH TIME ZONE,
    trace_id VARCHAR(32),
    span_id VARCHAR(16),
    user_email VARCHAR(255),
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_user FOREIGN KEY (user_id, tenant_id) 
        REFERENCES pulse.quotas(user_id, tenant_id) ON DELETE CASCADE
);

-- Index for efficient user_email queries
CREATE INDEX IF NOT EXISTS idx_pods_user_email ON pulse.pods(user_email) WHERE deleted_at IS NULL;

-- Table for pod activity logs
CREATE TABLE IF NOT EXISTS pulse.activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pulse_id UUID NOT NULL,
    activity_type VARCHAR(50) NOT NULL,
    description TEXT,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_pulse FOREIGN KEY (pulse_id) 
        REFERENCES pulse.pods(id) ON DELETE CASCADE
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_pods_user_id ON pulse.pods(user_id);
CREATE INDEX IF NOT EXISTS idx_pods_tenant_id ON pulse.pods(tenant_id);
CREATE INDEX IF NOT EXISTS idx_pods_status ON pulse.pods(status);
CREATE INDEX IF NOT EXISTS idx_pods_expires_at ON pulse.pods(expires_at);
CREATE INDEX IF NOT EXISTS idx_pods_last_activity ON pulse.pods(last_activity_at);
CREATE INDEX IF NOT EXISTS idx_activities_pulse_id ON pulse.activities(pulse_id);
CREATE INDEX IF NOT EXISTS idx_activities_created_at ON pulse.activities(created_at);

-- Insert default quota for system
INSERT INTO pulse.quotas (user_id, tenant_id, max_pods, max_cpu_per_pod, max_memory_mb_per_pod, max_storage_gb_per_pod)
VALUES ('00000000-0000-0000-0000-000000000000', NULL, 3, 2.0, 4096, 10)
ON CONFLICT (user_id, tenant_id) DO NOTHING;
