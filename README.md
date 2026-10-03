# Jungle Backend Challenge — Processamento Distribuído de Apostas em Go

Backend financeiro em Go para processamento de apostas via HTTP e SQS, com foco em **correção financeira, idempotência persistente, concorrência distribuída, ledger append-only, Inbox/Outbox, OAuth2/OIDC e recuperação após falhas**.

## Stack

- Go 1.27
- Uber Fx
- PostgreSQL 17
- pgx/v5
- AWS SQS via LocalStack
- Keycloak 26.8 / OAuth2 + OIDC
- Docker Compose
- `slog` em JSON
- métricas no formato Prometheus

## Arquitetura

```mermaid
flowchart LR
    P[Provider] -->|OAuth2 Bearer| HTTP[HTTP API]
    P -->|Wager message| SQSIN[SQS wager-transactions.fifo]

    HTTP --> APP[Application / WagerProcessor]
    SQSIN --> CONSUMER[SQS Consumer]
    CONSUMER --> INBOX[Inbox]
    CONSUMER --> APP

    APP --> DB[(PostgreSQL)]
    DB --> WALLET[Wallet]
    DB --> WAGER[Wager Transactions]
    DB --> LEDGER[Append-only Ledger]
    DB --> OUTBOX[Transactional Outbox]

    OUTBOX --> PUB[Outbox Publisher]
    PUB --> SQSOUT[SQS wager-events.fifo]

    KC[Keycloak] --> HTTP
```

As decisões e invariantes estão detalhadas em [ARCHITECTURE.md](ARCHITECTURE.md). A cobertura requisito a requisito está em [docs/REQUIREMENTS_MATRIX.md](docs/REQUIREMENTS_MATRIX.md).

## Garantias financeiras

O domínio não usa `float` para dinheiro. `Money` converte valores decimais com exatamente duas casas para `int64` em minor units e mantém a moeda ISO de três letras.

Cada movimentação financeira é executada em transação PostgreSQL. A wallet é bloqueada no banco durante a alteração, evitando lost updates entre processos diferentes. O ledger registra `balanceBefore` e `balanceAfter` e não é usado como cache mutável.

Operações suportadas:

| Operação | Efeito |
|---|---|
| `OPENING` | crédito inicial; criada apenas quando o saldo inicial é positivo |
| `BET` | débito positivo; exige saldo suficiente |
| `WIN` | crédito positivo |
| `LOSS` | valor exatamente `0.00`; sem movimentação |
| `REFUND` | reversão integral de uma `BET` processada |
| `ROLLBACK` | movimento oposto de uma operação processada elegível |

Reversões validam provider, player, wallet, moeda, round, valor e referência. Reversão parcial não é aceita.

## Idempotência

`POST /wagering/transactions` exige `Idempotency-Key`.

O hash de negócio é compartilhado entre HTTP e SQS. O payload é normalizado antes do SHA-256:

- espaços externos são removidos;
- `kind` e `currency` são convertidos para uppercase;
- o JSON usa a estrutura tipada e ordem determinística dos campos;
- `Idempotency-Key` e metadados de transporte não fazem parte do hash;
- `referenceExternalTransactionId` participa quando aplicável.

Comportamento:

- mesma chave + mesmo conteúdo: retorna o resultado persistido com `idempotentReplay=true`;
- mesma chave + conteúdo diferente: conflito;
- mesmo `(providerId, externalTransactionId)` com outra chave: conflito;
- replay de uma transação concluída retorna o saldo registrado no processamento original, não o saldo atual da wallet.

## Concorrência distribuída

A coordenação financeira é por wallet no PostgreSQL, não por mutex em memória. Isso permite múltiplas instâncias da aplicação sem serializar wallets independentes.

O teste distribuído `scripts/test_12_distributed.ps1` inicia **3 processos reais** da aplicação e valida, entre outros cenários:

- 50 requests idênticos concorrentes: 1 processamento + 49 replays;
- duas `BET` distintas de `80.00` sobre saldo `100.00`: uma processada, uma rejeitada, saldo final `20.00`;
- apenas um débito financeiro no ledger;
- wallets independentes continuam processando;
- restart de uma instância preserva a idempotência durável.

## Inbox / SQS

Entrada assíncrona:

- queue: `wager-transactions.fifo`;
- DLQ: `wager-transactions-dlq.fifo`;
- `maxReceiveCount=5`;
- deduplicação de negócio é persistente no PostgreSQL.

No consumo SQS, **Inbox receive, alterações de domínio, wallet, ledger, outbox e Inbox complete compartilham a mesma transação SQL**. O `DeleteMessage` ocorre somente depois do commit.

Uma `PENDING_REFERENCE` já persistida conclui duravelmente a mensagem de entrada; a continuidade passa a ser responsabilidade do fluxo de referências pendentes.

## Transactional Outbox

Eventos são gravados na mesma transação das alterações financeiras e publicados depois por um worker separado.

Queue de saída:

- `wager-events.fifo`;
- DLQ: `wager-events-dlq.fifo`.

O publisher suporta:

- múltiplos workers;
- claim com lease;
- `FOR UPDATE SKIP LOCKED`;
- backoff;
- recuperação de lease abandonado;
- republicação com o **mesmo `eventId`** quando a publicação ocorreu, mas a confirmação no banco falhou.

Eventos:

- `WagerTransactionProcessed`
- `WagerTransactionRejected`
- `WagerTransactionPendingReference`
- `WalletBalanceChanged`

Envelope:

```json
{
  "eventId": "uuid",
  "eventType": "WalletBalanceChanged",
  "aggregateId": "wallet-or-transaction-id",
  "correlationId": "correlation-id",
  "causationId": "optional-id",
  "occurredAt": "RFC3339 UTC",
  "version": 1,
  "data": {}
}
```

## OAuth2/OIDC e isolamento de provider

O ambiente local usa Keycloak no realm `jungle`.

Clientes de desenvolvimento:

- `provider-a` / `provider-a-secret`
- `provider-b` / `provider-b-secret`
- audience da API: `jungle-api`

As credenciais acima são **somente para o ambiente local do desafio**.

As rotas de negócio exigem Bearer Token. O token é validado por issuer, assinatura/JWKS e audience, e deve conter `provider_id`. A API impede que um provider opere ou consulte wallets/transações pertencentes a outro provider.

## API

### Health e métricas — públicos

```text
GET /health/live
GET /health/ready
GET /metrics
```

Readiness verifica PostgreSQL e SQS.

### Wallets — autenticadas

```text
POST /wallets
GET  /wallets/:walletId
GET  /wallets/:walletId/ledger?cursor=...&limit=50
POST /wallets/:walletId/reconciliation
```

Exemplo:

```json
POST /wallets
{
  "playerId": "player-123",
  "initialBalance": {
    "amount": "100.00",
    "currency": "BRL"
  }
}
```

### Wagering — autenticada

```text
POST /wagering/transactions
Idempotency-Key: provider-a:transaction-123
```

```json
{
  "providerId": "provider-a",
  "externalTransactionId": "transaction-123",
  "playerId": "player-123",
  "walletId": "WALLET_ID",
  "roundId": "round-987",
  "gameId": "fortune-chimp",
  "kind": "BET",
  "money": {
    "amount": "25.00",
    "currency": "BRL"
  }
}
```

Leituras:

```text
GET /wagering/transactions/:transactionId
GET /providers/:providerId/wagering/transactions/:externalTransactionId
```

`REFUND` e `ROLLBACK` adicionam:

```json
{
  "referenceExternalTransactionId": "original-external-transaction-id"
}
```

## Status HTTP relevantes

| Situação | Status |
|---|---:|
| criação de wallet | `201` |
| operação processada/rejeição de negócio persistida | `200` |
| referência pendente persistida | `202` |
| payload inválido | `400` |
| token ausente/inválido | `401` |
| provider sem acesso | `403` |
| recurso não encontrado | `404` |
| conflito de idempotência | `409` |
| falha transitória de processamento | `503` |

Rejeições de negócio são resultados duráveis da transação e expõem `status=REJECTED` e `failureCode`.

## Subindo o ambiente

Pré-requisitos:

- Go 1.27+
- Docker Desktop / Docker Compose
- PowerShell

Na raiz:

```powershell
docker compose up -d
```

Aplique as migrations:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\migrate-up.ps1
```

Configure a aplicação:

```powershell
$env:DATABASE_URL="postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
$env:OIDC_ISSUER_URL="http://localhost:8081/realms/jungle"
$env:OIDC_JWKS_URL="http://localhost:8081/realms/jungle/protocol/openid-connect/certs"
$env:OIDC_AUDIENCE="jungle-api"
$env:SQS_ENDPOINT="http://localhost:4566"
$env:AWS_REGION="us-east-1"
```

Execute:

```powershell
go run .\cmd\api
```

Serviços locais:

| Serviço | Endereço |
|---|---|
| API | `http://localhost:8080` |
| Keycloak | `http://localhost:8081` |
| LocalStack | `http://localhost:4566` |
| PostgreSQL | `localhost:5432` |

## Obtendo token local

PowerShell:

```powershell
$tokenResponse = Invoke-RestMethod `
  -Method POST `
  -Uri "http://localhost:8081/realms/jungle/protocol/openid-connect/token" `
  -ContentType "application/x-www-form-urlencoded" `
  -Body @{
    grant_type    = "client_credentials"
    client_id     = "provider-a"
    client_secret = "provider-a-secret"
  }

$token = $tokenResponse.access_token
```

Exemplo de headers:

```powershell
$headers = @{
  Authorization     = "Bearer $token"
  "Idempotency-Key" = "provider-a:transaction-123"
  "X-Correlation-ID" = "manual-test-001"
}
```

## Testes e validação

Testes gerais:

```powershell
$env:TEST_DATABASE_URL="postgres://jungle:jungle@localhost:5432/jungle?sslmode=disable"
go test ./... -count=1
go vet ./...
go build ./cmd/api
```

Validação do bloco distribuído:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\validate_12.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\test_12_distributed.ps1
```

A execução validada do #12 terminou com:

```text
Falhas encontradas: 0
12_DISTRIBUTED_E2E_RESULT=PASS
```

## Observabilidade

Logs são JSON via `slog`. Conforme disponíveis, incluem:

- `correlationId`
- `messageId`
- `transactionId`
- `walletId`
- `providerId`

Métricas expostas em `/metrics`:

- `jungle_wager_results_total{status=...}`
- `jungle_duplicates_total`
- `jungle_retries_total`
- `jungle_dlq_total`
- `jungle_concurrency_conflicts_total`
- `jungle_reconciliation_divergences_total`
- `jungle_processing_latency_seconds_*`
- `jungle_outbox_lag_seconds_*`

O lag da outbox mede o intervalo entre `occurredAt` do evento e a tentativa de publicação bem-sucedida.

## Estrutura

```text
cmd/api                         entrypoint
internal/domain                 entidades, Money e invariantes
internal/application            casos de uso e processamento financeiro
internal/infrastructure/postgres repositórios e transações
internal/infrastructure/sqsconsumer consumer + Inbox
internal/infrastructure/outboxpublisher publisher da Outbox
internal/infrastructure/oidc    validação OIDC
internal/transport/httpapi      contrato HTTP
internal/observability          logs, métricas e health
internal/platform               configuração e composição Fx
migrations                      migrations PostgreSQL
localstack/init                 provisionamento das filas
keycloak                        realm local
scripts                         execução, testes e validações
docs                            documentação complementar
```

## Auditoria final

O #14 ligou o `PendingReferenceWorker` ao lifecycle Fx e a migration `000011` removeu a unicidade global legada de `(player_id, currency)`, mantendo a identidade de wallet por `(provider_id, player_id, currency)` para wallets autenticadas.

Antes do commit final ainda devem ser concluídas as verificações de entrega:

1. executar `go test -race` em ambiente/toolchain compatível com o race detector;
2. remover do diretório de trabalho binários, `.tmp-*`, logs e relatórios locais (o `.gitignore` já impede novo versionamento desses artefatos);
3. repetir o smoke test a partir de ambiente limpo.

A documentação evita afirmar garantias que não tenham sido verificadas pela auditoria.
