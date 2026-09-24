-- SPDX-License-Identifier: Apache-2.0
-- Admin login sessions behind the ls_admin cookie. Only a hash of the cookie
-- value is stored, so a copy of the database can't be used to log in.

CREATE TABLE admin_sessions (
    token_hash TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL, -- unix nanoseconds
    expires_at INTEGER NOT NULL  -- unix nanoseconds
);
CREATE INDEX admin_sessions_by_expiry ON admin_sessions (expires_at);
