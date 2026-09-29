CREATE TABLE IF NOT EXISTS feedback (
    id              BIGSERIAL PRIMARY KEY,
    feedback_no     VARCHAR(32) NOT NULL UNIQUE,
    request_id      VARCHAR(64) NOT NULL UNIQUE,
    client_id       VARCHAR(64) NOT NULL,
    user_id         BIGINT,
    content         TEXT NOT NULL CHECK (char_length(content) >= 1 AND char_length(content) <= 2000),
    contact         VARCHAR(128) NOT NULL CHECK (char_length(contact) >= 1 AND char_length(contact) <= 128),
    image_count     SMALLINT NOT NULL DEFAULT 0,
    extra           JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT check_extra_size CHECK (octet_length(extra::text) <= 8192)
);
CREATE INDEX IF NOT EXISTS idx_feedback_client ON feedback (client_id);
CREATE INDEX IF NOT EXISTS idx_feedback_created ON feedback (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_contact ON feedback (contact);
CREATE INDEX IF NOT EXISTS idx_feedback_extra ON feedback USING GIN (extra jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_feedback_client_time ON feedback (client_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feedback_attachment (
    id              BIGSERIAL PRIMARY KEY,
    feedback_id     BIGINT REFERENCES feedback(id) ON DELETE SET NULL,
    client_id       VARCHAR(64) NOT NULL,
    file_key        VARCHAR(255) NOT NULL UNIQUE,
    original_name   VARCHAR(255) NOT NULL DEFAULT '',
    mime_type       VARCHAR(64) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'unconfirmed',
    size_bytes      BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 5242880),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_attachment_feedback ON feedback_attachment (feedback_id);
CREATE INDEX IF NOT EXISTS idx_attachment_client ON feedback_attachment (client_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_attachment_orphan ON feedback_attachment (created_at) WHERE feedback_id IS NULL OR status != 'confirmed';

CREATE TABLE IF NOT EXISTS feedback_center (
    id              BIGSERIAL PRIMARY KEY,
    center_id       VARCHAR(64) NOT NULL UNIQUE,
    name            VARCHAR(128) NOT NULL,
    app_id          VARCHAR(64) NOT NULL,
    env             VARCHAR(16) NOT NULL DEFAULT 'prod',
    secret_cipher   VARCHAR(256) NOT NULL,
    secret_version  INTEGER NOT NULL DEFAULT 1 CHECK (secret_version > 0),
    sn_mode         VARCHAR(32) NOT NULL CHECK (sn_mode IN ('whitelist', 'prefix', 'any')),
    status          VARCHAR(16) NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    expire_at       TIMESTAMPTZ,
    contact         VARCHAR(128) NOT NULL DEFAULT '',
    remark          VARCHAR(255) NOT NULL DEFAULT '',
    created_by      VARCHAR(64) NOT NULL DEFAULT '',
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_feedback_center_status ON feedback_center (status);
CREATE INDEX IF NOT EXISTS idx_feedback_center_app ON feedback_center (app_id);
CREATE INDEX IF NOT EXISTS idx_feedback_center_created ON feedback_center (created_at DESC);

CREATE TABLE IF NOT EXISTS feedback_center_sn (
    id              BIGSERIAL PRIMARY KEY,
    center_id       VARCHAR(64) NOT NULL REFERENCES feedback_center(center_id) ON DELETE CASCADE,
    sn              VARCHAR(128) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    expire_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (center_id, sn)
);
CREATE INDEX IF NOT EXISTS idx_feedback_center_sn_center ON feedback_center_sn (center_id);

CREATE TABLE IF NOT EXISTS feedback_center_audit (
    id              BIGSERIAL PRIMARY KEY,
    center_id       VARCHAR(64) NOT NULL REFERENCES feedback_center(center_id) ON DELETE CASCADE,
    action          VARCHAR(64) NOT NULL,
    operator        VARCHAR(64) NOT NULL DEFAULT '',
    detail          JSONB NOT NULL DEFAULT '{}',
    ip              VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_feedback_center_audit_center ON feedback_center_audit (center_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_center_audit_action ON feedback_center_audit (action, created_at DESC);
