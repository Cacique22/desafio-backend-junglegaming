# Jungle Gaming — Motor Distribuído de Apostas em Go

Serviço de alta disponibilidade e consistência contábil estrita para processamento de transações financeiras de apostas em ambientes distribuídos.

Construído com **Go**, **Uber Fx**, **PostgreSQL**, **Keycloak (OAuth 2.0 / OIDC)** e **AWS SQS FIFO (LocalStack)**.

---

## 1. Pré-Requisitos

* **Docker & Docker Compose** (versão 24+)
* **Go** 1.22+ (para executar os testes localmente)
* **Make** (opcional, para atalhos)

---

## 2. Início Rápido (Executando tudo com Docker Compose)

Para subir o ecossistema completo (PostgreSQL + Keycloak + LocalStack + 3 instâncias do serviço Go em cluster):

```bash
docker compose up --build
```

O compose subirá automaticamente:
* **PostgreSQL 16:** na porta `5432` com migrations aplicadas.
* **LocalStack:** na porta `4566` com as filas FIFO segregadas (`wager-transactions.fifo` e `wager-events.fifo`) e suas respectivas DLQs.
* **Keycloak:** na porta `8085` com o realm `jungle` e clientes pré-configurados.
* **App 1:** na porta `8080`.
* **App 2:** na porta `8081` (para testes de cluster e concorrência).
* **App 3:** na porta `8082` (para recuperação de falhas).

---

## 3. Execução de Testes e Race Detector

Para executar os testes unitários e de integração:

```bash
# Rodar todos os testes
go test ./...

# Rodar com detecção de concorrência (Race Detector)
go test -race ./...

# Rodar especificamente os testes de concorrência massiva
go test -v -race ./tests/integration/...
```

Ou através do Makefile:
```bash
make test
make test-race
make test-concurrency
```

---

## 4. Exemplos de Uso da API (via cURL)

### 4.1. Health Check
```bash
curl -s http://localhost:8080/health/ready | jq
```

### 4.2. Criar uma Carteira (Serviço Interno)
```bash
curl -X POST http://localhost:8080/wallets \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer internal-service-token" \
  -d '{
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "initialBalance": { "amount": "100.00", "currency": "BRL" }
  }' | jq
```
*Guarde o `id` da carteira retornado na resposta para os passos seguintes.*

### 4.3. Realizar uma Aposta (`BET`)
```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer provider-provider-a" \
  -H "Idempotency-Key: provider-a:tx-101" \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "tx-101",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "<WALLET_ID>",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }' | jq
```

### 4.4. Testar Replay Idempotente (Executar o comando anterior novamente)
Ao repetir a chamada com o mesmo `Idempotency-Key` e mesmo payload:
```json
{
  "transactionId": "...",
  "status": "PROCESSED",
  "balance": { "amount": "75.00", "currency": "BRL" },
  "idempotentReplay": true
}
```

### 4.5. Consultar Extrato (Ledger Auditável)
```bash
curl -X GET "http://localhost:8080/wallets/<WALLET_ID>/ledger" \
  -H "Authorization: Bearer internal-service-token" | jq
```

### 4.6. Reconciliar Carteira (Auditoria de Consistência)
```bash
curl -X POST "http://localhost:8080/wallets/<WALLET_ID>/reconciliation" \
  -H "Authorization: Bearer internal-service-token" | jq
```
Retorna a conferência entre o saldo armazenado e a soma dos lançamentos do ledger:
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

## 5. Variáveis de Ambiente

Consulte o arquivo `.env.example` para visualizar a lista completa de configurações de portas, banco e credenciais.

---

## 6. Decisões Arquiteturais

Para detalhes aprofundados sobre a modelagem sem ponto flutuante, locks com `SELECT FOR UPDATE`, padrões Inbox/Outbox e isolamento de provedores, consulte o arquivo [ARCHITECTURE.md](./ARCHITECTURE.md).
