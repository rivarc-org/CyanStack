-- OIDC Users table (hardcoded for now, extensible later)
CREATE TABLE IF NOT EXISTS oidc_users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oidc_users_email ON oidc_users(email);

-- OIDC Clients table (dynamically registered plugins)
CREATE TABLE IF NOT EXISTS oidc_clients (
    id TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    plugin_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    redirect_uris TEXT NOT NULL,
    grant_types TEXT NOT NULL DEFAULT '["authorization_code","refresh_token"]',
    response_types TEXT NOT NULL DEFAULT '["code"]',
    scopes TEXT NOT NULL DEFAULT '["openid","profile","email"]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oidc_clients_project_id ON oidc_clients(project_id);
CREATE INDEX IF NOT EXISTS idx_oidc_clients_node_id ON oidc_clients(node_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_oidc_clients_project_node ON oidc_clients(project_id, node_id);

-- OIDC Authorization Requests (pending auth requests)
CREATE TABLE IF NOT EXISTS oidc_auth_requests (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    scopes TEXT NOT NULL,
    state TEXT,
    nonce TEXT,
    response_type TEXT NOT NULL,
    code_challenge TEXT,
    code_challenge_method TEXT,
    user_id TEXT,
    auth_time DATETIME,
    done INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oidc_auth_requests_client_id ON oidc_auth_requests(client_id);

-- OIDC Authorization Codes (short-lived, for code exchange)
CREATE TABLE IF NOT EXISTS oidc_auth_codes (
    id TEXT PRIMARY KEY,
    auth_request_id TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (auth_request_id) REFERENCES oidc_auth_requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oidc_auth_codes_expires ON oidc_auth_codes(expires_at);

-- OIDC Tokens (access and refresh tokens)
CREATE TABLE IF NOT EXISTS oidc_tokens (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    audience TEXT NOT NULL,
    scopes TEXT NOT NULL,
    auth_method_references TEXT,
    auth_time DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oidc_tokens_subject ON oidc_tokens(subject);
CREATE INDEX IF NOT EXISTS idx_oidc_tokens_expires ON oidc_tokens(expires_at);

-- OIDC Refresh Tokens
CREATE TABLE IF NOT EXISTS oidc_refresh_tokens (
    id TEXT PRIMARY KEY,
    token_id TEXT NOT NULL,
    auth_request_id TEXT NOT NULL,
    auth_time DATETIME NOT NULL,
    amr TEXT,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (auth_request_id) REFERENCES oidc_auth_requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oidc_refresh_tokens_expires ON oidc_refresh_tokens(expires_at);

-- RSA Signing Keys (for RS256)
CREATE TABLE IF NOT EXISTS oidc_signing_keys (
    id TEXT PRIMARY KEY,
    private_key_pem TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    algorithm TEXT NOT NULL DEFAULT 'RS256',
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Root Sessions (Super-Admin login to cyan-ui)
CREATE TABLE IF NOT EXISTS oidc_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES oidc_users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_oidc_sessions_user_id ON oidc_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_oidc_sessions_expires ON oidc_sessions(expires_at);
