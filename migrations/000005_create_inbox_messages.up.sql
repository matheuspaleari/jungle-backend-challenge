CREATE TABLE inbox_messages (
    id UUID PRIMARY KEY,

    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,

    payload_hash TEXT NOT NULL,

    received_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,

    CONSTRAINT inbox_messages_consumer_name_not_empty
        CHECK (
            btrim(consumer_name) <> ''
        ),

    CONSTRAINT inbox_messages_message_id_not_empty
        CHECK (
            btrim(message_id) <> ''
        ),

    CONSTRAINT inbox_messages_payload_hash_not_empty
        CHECK (
            btrim(payload_hash) <> ''
        ),

    /*
     * Uma mensagem concluida nunca pode ter sido
     * concluida antes de ter sido recebida.
     */
    CONSTRAINT inbox_messages_completion_time_valid
        CHECK (
            completed_at IS NULL
            OR completed_at >= received_at
        ),

    /*
     * Identidade duravel da mensagem para cada consumidor.
     *
     * Uma redelivery da mesma messageId para o mesmo
     * consumer nao cria outro registro.
     */
    CONSTRAINT inbox_messages_consumer_message_unique
        UNIQUE (
            consumer_name,
            message_id
        )
);


/*
 * Facilita localizar mensagens que foram recebidas,
 * mas cujo processamento ainda nao foi concluido.
 */
CREATE INDEX inbox_messages_pending_idx
    ON inbox_messages (
        received_at
    )
    WHERE completed_at IS NULL;


/*
 * Consultas por data de conclusao.
 */
CREATE INDEX inbox_messages_completed_idx
    ON inbox_messages (
        completed_at
    )
    WHERE completed_at IS NOT NULL;