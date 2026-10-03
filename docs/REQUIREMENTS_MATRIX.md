# Matriz de requisitos e evidências

Legenda: **OK** = implementado e coberto; **AUDITAR** = existe implementação/teste, mas há um ponto de entrega/runtime para fechar no #14.

| Área | Implementação / evidência | Estado |
|---|---|---|
| Go + Fx | `cmd/api`, `internal/platform/app/module.go` | OK |
| PostgreSQL / pgx | repositories + `TransactionManager` | OK |
| Dinheiro sem float | `internal/domain/money.go` | OK |
| Idempotência persistente | wager repository + processor + integration tests | OK |
| Hash HTTP/SQS equivalente | `internal/application/wager_payload.go` | OK |
| `(providerId, externalTransactionId)` único | schema/repository/processor | OK |
| Ledger append-only | ledger domain/repository + constraints/tests | OK |
| Estado + wallet + ledger + eventos atômicos | `WagerProcessor` em SQL transaction | OK |
| Concorrência por wallet | PostgreSQL row locking | OK |
| 2x BET 80 sobre 100 | `scripts/test_12_distributed.ps1` | OK |
| >=3 processos reais | teste distribuído inicia 3 PIDs | OK |
| Wallets independentes | teste distribuído | OK |
| Replay após restart | teste distribuído | OK |
| HTTP contract | `internal/transport/httpapi/handler.go` | OK |
| Cursor de ledger | cursor opaco Base64 + ordem estável no repository | OK |
| Reconciliação read-only | handler/repository + métricas | OK |
| OAuth2/OIDC | Keycloak + `internal/infrastructure/oidc` | OK |
| Isolamento por provider | middleware + ownership persistido + E2E #10B | OK |
| SQS input + DLQ | LocalStack init + consumer | OK |
| Inbox durável | `InboxRepository` | OK |
| Inbox + domínio na mesma SQL tx | `Consumer.handle` + `inbox_atomicity_integration_test.go` | OK |
| Outbox transacional | domain events + outbox repository | OK |
| Dois publishers / claim | lease + `SKIP LOCKED` + integration test | OK |
| Retry/backoff Outbox | publisher + recovery tests | OK |
| publish→confirm failure | `publisher_recovery_test.go`, mesmo `eventId` | OK |
| Eventos obrigatórios | quatro `EventType` em `domain/event.go` | OK |
| Pending reference | worker + integration tests + Fx lifecycle | OK |
| Retry/TTL de referência | retry fields + runtime worker, max attempts=8 | OK |
| Logs JSON | `internal/observability` | OK |
| Métricas | `/metrics` | OK |
| Outbox lag real | `occurredAt` → publish | OK |
| Health live/ready | `/health/live`, `/health/ready` | OK |
| Migrations | `migrations/000000..000010` + scripts | OK |
| `go test ./...` | #12 validation | OK |
| `go vet ./...` | #12 validation | OK |
| `go build ./cmd/api` | #12 validation | OK |
| `go test -race` | depende de toolchain/CGO no ambiente Windows atual | AUDITAR |
| Provider-scoped wallet uniqueness | migration `000011` remove regra global; índice provider-scoped permanece | OK |

## Evidência do #12

A validação integrada executou:

```text
go test ./...
Inbox atomicity integration test
Outbox claim/lease recovery integration test
Outbox publish/confirm recovery tests
Three independent DB clients integration test
go vet ./...
go build ./cmd/api
#12 validation PASS
```

O E2E distribuído confirmou:

```text
3 processos de aplicação independentes
50 requests concorrentes = 1 original + 49 replays
saldo 90.00 / version 2 / um único debit
2 BETs de 80 sobre 100 = 1 processada + 1 rejeitada
saldo final 20.00
wallets independentes processadas
restart com novo PID
replay idempotente após restart
Falhas encontradas: 0
12_DISTRIBUTED_E2E_RESULT=PASS
```

## Pontos obrigatórios do #14

1. Executar race detector em ambiente compatível ou documentar precisamente a limitação de toolchain.
2. Limpar artefatos locais (`api.exe`, `.tmp-12`, logs/relatórios) antes do commit.
3. Confirmar que as credenciais versionadas são apenas fixtures locais do desafio.
4. Subir o ambiente do zero e repetir migrations + testes + E2E.
