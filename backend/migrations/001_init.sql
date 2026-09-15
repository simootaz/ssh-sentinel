-- Initial schema, docs/architecture.md section 4. PostgreSQL 14 or newer,
-- standard features only.

-- Servers running the agent. One row per server, one bearer token per row.
CREATE TABLE servers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,              -- hostname or friendly label, shown in the app
    token_hash    TEXT NOT NULL UNIQUE,              -- sha256 hex of the bearer token, never the token
    os            TEXT NOT NULL CHECK (os IN ('linux', 'windows', 'darwin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ                        -- updated on every authenticated call
);

-- Phones that receive pushes. Every pending request is pushed to all of them.
CREATE TABLE devices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fcm_token     TEXT NOT NULL UNIQUE,
    platform      TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    label         TEXT,                              -- "Pixel 8", free text, shown in history
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ
);

-- One row per ssh login or sudo invocation that reached the backend.
CREATE TABLE requests (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id         UUID NOT NULL REFERENCES servers(id),
    context           TEXT NOT NULL DEFAULT 'ssh' CHECK (context IN ('ssh', 'sudo')),
    username          TEXT NOT NULL,
    source_ip         INET,                            -- NULL when unknown (sudo)
    hostname          TEXT NOT NULL,                   -- as reported by the agent, display only
    tty               TEXT,
    command           TEXT,                            -- sudo only, best effort
    geo               JSONB,                           -- {"country": "FR", "city": "Paris", "asn": "AS12876"}, may be NULL
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'approved', 'denied', 'timeout',
                                        'whitelisted', 'blocked_ip', 'blocked_geo', 'notified')),
    decided_by        TEXT                             -- NULL while pending, and for notified
                      CHECK (decided_by IN ('admin', 'whitelist', 'timeout', 'autoblock', 'georule')),
    decided_by_device UUID REFERENCES devices(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,            -- created_at + 30 s
    decided_at        TIMESTAMPTZ
);

CREATE INDEX requests_pending_idx   ON requests (status, expires_at) WHERE status = 'pending';
CREATE INDEX requests_history_idx   ON requests (created_at DESC);
CREATE INDEX requests_server_idx    ON requests (server_id, created_at DESC);
CREATE INDEX requests_denied_ip_idx ON requests (source_ip, decided_at) WHERE status = 'denied';

-- Always-allow list, per context. server_id NULL means every server. expires_at NULL means permanent.
CREATE TABLE whitelist (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username          TEXT NOT NULL,
    context           TEXT NOT NULL DEFAULT 'ssh' CHECK (context IN ('ssh', 'sudo')),
    server_id         UUID REFERENCES servers(id) ON DELETE CASCADE,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_from      UUID REFERENCES requests(id),    -- the request approved with approve_always, if any
    created_by_device UUID REFERENCES devices(id) ON DELETE SET NULL
);

-- Two indexes instead of one UNIQUE (username, context, server_id): in PostgreSQL two NULLs
-- are not equal, so a plain UNIQUE would allow duplicate global entries.
CREATE UNIQUE INDEX whitelist_user_ctx_server_idx ON whitelist (username, context, server_id) WHERE server_id IS NOT NULL;
CREATE UNIQUE INDEX whitelist_user_ctx_global_idx ON whitelist (username, context) WHERE server_id IS NULL;

-- IPs blocked by the backend after repeated denials. Requests from them are denied without a push.
CREATE TABLE blocked_ips (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ip              INET NOT NULL UNIQUE,
    reason          TEXT NOT NULL DEFAULT 'autoblock',
    denial_count    INTEGER NOT NULL,                  -- denials that triggered the block
    first_denied_at TIMESTAMPTZ NOT NULL,
    last_denied_at  TIMESTAMPTZ NOT NULL,
    hit_count       INTEGER NOT NULL DEFAULT 0,        -- requests auto-denied since the block
    last_hit_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ                        -- NULL = until unblocked from the app
);

-- Country blocklist. Requests whose geo country matches are denied without a push.
CREATE TABLE geo_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    country     CHAR(2) NOT NULL UNIQUE,               -- ISO 3166-1 alpha-2, upper case
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
