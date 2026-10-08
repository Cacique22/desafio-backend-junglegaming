# Jungle Gaming — Motor Distribuído de Apostas em Go

Serviço de alta disponibilidade e consistência contábil estrita para processamento de transações financeiras de apostas em ambientes distribuídos.

Construído com **Go 1.22+**, **Uber Fx**, **PostgreSQL 16**, **Keycloak 25 (OAuth 2.0 / OIDC)** e **AWS SQS FIFO (LocalStack 3.7)**.

---

## 1. Pré-Requisitos

* **Docker & Docker Compose** (versão 24+)
* **Go** 1.22+ (para compilação e testes locais)
* **Make** (opcional, para atalhos)
* **curl** e **jq** (opcionais, para testar endpoints manualmente)

---

## 2. Início Rápido (Execução via Docker Compose)

Para subir o cluster completo com um único comando a partir de um checkout limpo:

```bash
docker compose up --build
```

O compose provisiona e inicializa de forma automatizada:
* **PostgreSQL 16:** porta `5432` com migrations aplicadas e triggers de immutabilidade ativos.
* **LocalStack 3.7:** porta `4566` com filas FIFO segregadas (`wager-transactions.fifo` e `wager-events.fifo`) e suas respectivas DLQs.
* **Keycloak 25:** porta `8085` com o realm `jungle` provisionado automaticamente com identidades de teste.
* **Cluster da Aplicação Go:** 3 nós em paralelo:
  * `wager-app-1`: porta `8080`
  * `wager-app-2`: porta `8081` (cluster e concorrência)
  * `wager-app-3`: porta `8082` (tolerância a falhas e failover)

---

## 3. Banco de Dados e Migrations

As migrations SQL gerenciam o schema relacional, constraints de integridade (`balance_cents >= 0`) e triggers de bloqueio de mutação do ledger.

### 3.1. Aplicação Automática e Manual
* **Automática:** Ao iniciar o contêiner `wager-postgres`, o script `migrations/000001_initial_schema.up.sql` é executado através do diretório `/docker-entrypoint-initdb.d/`.
* **Manual (se necessário):**
  ```bash
  docker exec -i wager-postgres psql -U postgres -d wager_db < migrations/000001_initial_schema.up.sql
  ```

### 3.2. Reversão de Migrations (Rollback)
Para reverter completamente o schema e remover todas as tabelas, triggers e funções:
```bash
docker exec -i wager-postgres psql -U postgres -d wager_db < migrations/000001_initial_schema.down.sql
```

---

## 4. Inicialização das Filas SQS FIFO

As filas de mensagens são provisionadas de forma segregada para evitar *poison pills* e garantir ordenação estrita por entidade:
1. `wager-transactions.fifo` (e DLQ `wager-transactions-dlq.fifo`): Comandos de transações externas.
2. `wager-events.fifo` (e DLQ `wager-events-dlq.fifo`): Eventos de domínio despachados pela Transactional Outbox.

O script `scripts/init-localstack.sh` é executado automaticamente pelo hook de inicialização do LocalStack (`/etc/localstack/init/ready.d/`). Para recriar manualmente via CLI:
```bash
# Filas de transações
docker exec -i wager-localstack awslocal sqs create-queue --queue-name wager-transactions-dlq.fifo --attributes FifoQueue=true,ContentBasedDeduplication=false
docker exec -i wager-localstack awslocal sqs create-queue --queue-name wager-transactions.fifo --attributes FifoQueue=true,ContentBasedDeduplication=false

# Filas de eventos da outbox
docker exec -i wager-localstack awslocal sqs create-queue --queue-name wager-events-dlq.fifo --attributes FifoQueue=true,ContentBasedDeduplication=false
docker exec -i wager-localstack awslocal sqs create-queue --queue-name wager-events.fifo --attributes FifoQueue=true,ContentBasedDeduplication=false
```

---

## 5. Provisionamento do IdP (Keycloak) e Autenticação

O Keycloak é provisionado no boot importando o arquivo `keycloak/realm-export.json`.

### 5.1. Identidades Pré-Configuradas

| Client ID | Client Secret | Claims Injetadas | Uso / Permissão |
|---|---|---|---|
| `provider-a` | `provider-a-secret` | `provider_id: "provider-a"` | Provedor externo A (apenas suas transações) |
| `provider-b` | `provider-b-secret` | `provider_id: "provider-b"` | Provedor externo B (isolado do provedor A) |
| `internal-service` | `internal-secret` | `is_internal: true` | Operações administrativas (abertura e conciliação de carteira) |

### 5.2. Obtenção de Token OAuth 2.0 / OIDC (Client Credentials)
```bash
# Token para Provider A
curl -s -X POST http://localhost:8085/realms/jungle/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret" | jq -r .access_token

# Token Interno
curl -s -X POST http://localhost:8085/realms/jungle/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=internal-service&client_secret=internal-secret" | jq -r .access_token
```

*Nota para testes locais:* Para agilidade em desenvolvimento e testes automatizados, o middleware aceita também os mock tokens quando `ALLOW_DEV_TOKENS=true`:
* `Authorization: Bearer provider-provider-a`
* `Authorization: Bearer internal-service-token`

---

## 6. Comandos de Operação e Testes

Os comandos abaixo atendem integralmente aos requisitos de validação técnica:

### 6.1. Comandos Principais
```bash
# 1. Subir ambiente completo
docker compose up --build

# 2. Executar toda a suite de testes
go test ./...

# 3. Executar testes com detector de race conditions
go test -race ./...

# 4. Executar análise estática de código
go vet ./...
```

### 6.2. Preparação de Dependências e Testes de Integração
Para executar os testes de integração a partir da sua máquina host:
1. Certifique-se de que o ambiente está ativo: `docker compose up -d`
2. Execute a suíte de integração:
   ```bash
   go test -v ./tests/integration/...
   ```

### 6.3. Testes de Múltiplas Instâncias e Concorrência Massiva
```bash
# Teste de disputa concorrente (2 apostas de R$ 80 disputando R$ 100)
go test -v -run TestTwoSimultaneousBetsDispute ./tests/integration/...

# Teste de 50 apostas simultâneas idênticas (replay idempotente)
go test -v -run TestFiftySimultaneousIdenticalBets ./tests/integration/...

# Teste de ciclo de vida completo distribuído entre app1, app2 e app3
go test -v -run TestFullLifecycleAndMultiInstanceE2E ./tests/integration/...

# Teste de resolução de reversão fora de ordem (PendingReferenceResolver)
go test -v -run TestOutOfOrderRefundResolution ./tests/integration/...

# Teste de prevenção contra reversão dupla cruzada (REFUND + ROLLBACK)
go test -v -run TestCombinedRefundAndRollbackReversalPrevention ./tests/integration/...
```

### 6.4. Simulações de Falha e Resiliência
* **Tolerância à Queda de Instância (Failover de Nó):**
  1. Derrube a instância 3: `docker stop wager-app-3`
  2. Execute `go test -v -run TestTwoSimultaneousBetsDispute ./tests/integration/...`
  3. Verifique que `app1:8080` e `app2:8081` continuam atendendo normalmente.
  4. Reinicie a instância: `docker start wager-app-3`
* **Queda Temporária de Mensageria (Transactional Outbox Resiliency):**
  1. Se o LocalStack for interrompido momentaneamente, as transações financeiras no PostgreSQL continuam sendo commitadas sem falha.
  2. Os eventos permanecem na tabela `outbox` como `PENDING`.
  3. Ao reestabelecer o SQS, o `OutboxPublisher` consome a fila com backoff e publica todos os eventos pendentes sem perda.

---

## 7. Exemplos Práticos de Uso da API (cURL)

### 7.1. Health Check
```bash
curl -s http://localhost:8080/health/ready | jq
```

### 7.2. Criar Carteira (Operação Administrativa)
```bash
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer internal-service-token" \
  -d '{
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "initialBalance": { "amount": "100.00", "currency": "BRL" }
  }' | jq
```

### 7.3. Submeter uma Aposta (`BET`)
```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer provider-provider-a" \
  -H "Idempotency-Key: provider-a:bet-101" \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "bet-101",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "<WALLET_ID>",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }' | jq
```

### 7.4. Replay Idempotente (Mesma Chave e Payload)
Repita a chamada anterior:
```json
{
  "transactionId": "...",
  "status": "PROCESSED",
  "balance": { "amount": "75.00", "currency": "BRL" },
  "idempotentReplay": true
}
```

### 7.5. Reconciliação Contábil do Ledger
```bash
curl -X POST http://localhost:8080/wallets/<WALLET_ID>/reconciliation \
  -H "Authorization: Bearer internal-service-token" | jq
```
Retorno:
```json
{
  "walletId": "<WALLET_ID>",
  "storedBalance": { "amount": "75.00", "currency": "BRL" },
  "calculatedBalance": { "amount": "75.00", "currency": "BRL" },
  "difference": { "amount": "0.00", "currency": "BRL" },
  "consistent": true,
  "checkedEntries": 2
}
```

---

## 8. Testes de Carga e Performance (Diferencial Opcional)

O repositório inclui um benchmark de carga automatizado e 100% reproduzível via Go, projetado para estressar o cluster distribuído contra as instâncias ativas no Docker Compose.

### 8.1. Comando Reproduzível
```bash
# Via Makefile
make test-load

# Ou diretamente via Go
go test -v -run TestLoadBenchmark ./tests/integration/...
```

### 8.2. Ambiente e Metodologia
* **Ambiente:** Cluster composto por 3 nós Go (`app1:8080`, `app2:8081`, `app3:8082`), PostgreSQL 16 com triggers e locks ativos, e LocalStack AWS SQS FIFO (`wager-transactions.fifo` e `wager-events.fifo`).
* **Metodologia:**
  1. Criação de $N$ carteiras independentes com saldo inicial de R$ 1.000,00.
  2. Disparo de 200 operações concorrentes distribuídas em 15 workers paralelos via HTTP round-robin entre os 3 nós.
  3. Carga mista composta por 70% `BET`, 20% `WIN`, e 10% de injeção deliberada de replays idempotentes e conflitos de chave/payload (HTTP 409).
  4. Coleta de latência individual por requisição, contagem de status HTTP e medição do atraso de publicação da Transactional Outbox diretamente no PostgreSQL.
  5. Reconciliação contábil do ledger após o término da carga em todas as carteiras.

### 8.3. Métricas Obtidas (Execução Típica em Ambiente Local)
* **Throughput:** ~175 req/s (RPS)
* **Latência HTTP:**
  * **Min:** 1.7 ms
  * **p50 (Mediana):** ~70 ms
  * **p95:** ~205 ms
  * **p99:** ~268 ms
  * **Max:** ~285 ms
* **Distribuição de Respostas:**
  * **200 OK (Processados/Replays):** 100% das operações válidas processadas com sucesso
  * **409 Conflict:** 100% dos conflitos injetados interceptados e resolvidos sem erro de servidor
  * **5xx / Erros:** 0 falhas inesperadas
* **Atraso da Outbox (Lag):** Média de ~1.200 ms e p95 de ~1.690 ms (tempo entre a inserção na transação do banco e a publicação no SQS FIFO pelos workers).
* **Consistência Contábil:** 100% das carteiras com $\Delta = 0.00$ na reconciliação pós-carga.

---

## 9. Decisões Arquiteturais

Para detalhes aprofundados sobre a modelagem financeira sem ponto flutuante, locks com `SELECT FOR UPDATE`, padrões Inbox/Outbox, isolamento de provedores e limitações, consulte o arquivo [ARCHITECTURE.md](./ARCHITECTURE.md).
