-- Drop OIDC tables in reverse order of creation (respecting foreign keys)
DROP TABLE IF EXISTS oidc_sessions;
DROP TABLE IF EXISTS oidc_signing_keys;
DROP TABLE IF EXISTS oidc_refresh_tokens;
DROP TABLE IF EXISTS oidc_tokens;
DROP TABLE IF EXISTS oidc_auth_codes;
DROP TABLE IF EXISTS oidc_auth_requests;
DROP TABLE IF EXISTS oidc_clients;
DROP TABLE IF EXISTS oidc_users;
