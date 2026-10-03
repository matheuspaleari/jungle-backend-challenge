CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,

    event_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,

    correlation_id TEXT NOT NULL,
    causation_id TEXT,

    occurred_at TIMESTAMPTZ NOT NULL,

    event_version BIGINT NOT NULL,

    payload JSONB NOT NULL,

    retry_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,

    published_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT outbox_events_event_type_not_empty
        CHECK (
            btrim(event_type) <> ''
        ),

    CONSTRAINT outbox_events_aggregate_id_not_empty
        CHECK (
            btrim(aggregate_id) <> ''
        ),

    CONSTRAINT outbox_events_correlation_id_not_empty
        CHECK (
            btrim(correlation_id) <> ''
        ),

    CONSTRAINT outbox_events_causation_id_not_empty
        CHECK (
            causation_id IS NULL
            OR btrim(causation_id) <> ''
        ),

    CONSTRAINT outbox_events_version_positive
        CHECK (
            event_version >= 1
        ),

    CONSTRAINT outbox_events_payload_object
        CHECK (
            jsonb_typeof(payload) = 'object'
        ),

    CONSTRAINT outbox_events_retry_count_non_negative
        CHECK (
            retry_count >= 0
        ),

    CONSTRAINT outbox_events_next_attempt_valid
        CHECK (
            next_attempt_at >= occurred_at
        ),

    CONSTRAINT outbox_events_created_at_valid
        CHECK (
            created_at >= occurred_at
        ),

    CONSTRAINT outbox_events_published_at_valid
        CHECK (
            published_at IS NULL
            OR published_at >= occurred_at
        )
);


/*
 * Principal indice utilizado pelo Outbox Publisher.
 *
 * Localiza eventos ainda nao publicados que ja podem
 * sofrer uma tentativa de publicacao.
 *
 * Posteriormente o worker usara coordenacao no PostgreSQL
 * para permitir multiplos publishers concorrentes.
 */
CREATE INDEX outbox_events_pending_idx
    ON outbox_events (
        next_attempt_at,
        occurred_at,
        id
    )
    WHERE published_at IS NULL;


/*
 * Facilita consultas operacionais dos eventos publicados.
 */
CREATE INDEX outbox_events_published_idx
    ON outbox_events (
        published_at
    )
    WHERE published_at IS NOT NULL;


/*
 * Permite localizar eventos relacionados a um aggregate.
 */
CREATE INDEX outbox_events_aggregate_idx
    ON outbox_events (
        aggregate_id,
        occurred_at DESC
    );