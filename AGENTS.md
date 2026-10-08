# AGENTS.md — Regras de Desenvolvimento e Diretrizes para Agentes de IA

Este documento define as diretrizes estritas, invariantes de domínio e decisões arquiteturais que **todos os agentes de IA devem seguir obrigatoriamente** ao trabalhar neste repositório.

---

## 1. Contexto do Projeto

* **Repositório:** `desafio-backend` (Jungle Gaming — Motor Distribuído de Apostas).
* **Stack Tecnológica:** Go (1.22+), Uber Fx, PostgreSQL 16 (`pgx/v5`), AWS SQS FIFO (LocalStack), Keycloak 25 (OAuth 2.0 / OIDC), Docker & Docker Compose.
* **Frontend Auxiliar:** React 19 + TypeScript + Vite + TailwindCSS (Dashboard / Simulador de Concorrência em `src/`).

---

## 2. Decisões Arquiteturais Estritas (Baseado em ARCHITECTURE.md)

Qualquer alteração ou novo código deve respeitar rigorosamente as decisões do [ARCHITECTURE.md](file:///c:/Users/Usuario/Documents/GitHub/desafio-backend/ARCHITECTURE.md):

### 2.1. Precisão Financeira (`money.Money`)
1. **Proibição Absoluta de Float:** Nunca utilizar `float32` ou `float64` para valores monetários, taxas ou saldos.
2. **Representação por Centavos:** Todos os valores financeiros são armazenados e computados como `int64` centavos (`cents`).
3. **Imutabilidade:** O struct `money.Money` é estritamente imutável. Operações como `Add`, `Sub` e `Neg` sempre retornam uma nova instância.
4. **Proteção contra Overflow:** Operações aritméticas validam limites de estouro de inteiros (`math.MaxInt64` e `math.MinInt64`).
5. **Formatação de Moeda:** Moeda no padrão ISO 4217 de 3 letras (ex: `"BRL"`, `"USD"`).

### 2.2. Concorrência e Isolamento de Carteira
1. **Pessimistic Row-Level Lock:** Transações financeiras em uma carteira exigem `SELECT ... FOR UPDATE` na linha correspondente na tabela `wallets`.
2. **Serialização por Carteira:** Operações na mesma carteira são serializadas pelo banco de dados. Nunca permita operações concorrentes não serializadas na mesma carteira.
3. **Paralelismo Horizontal:** Operações em carteiras diferentes devem executar em paralelo sem bloqueio mútuo.
4. **Cenário de Disputa Simultânea:** Se duas apostas concorrentes de R$ 80 disputam um saldo de R$ 100, a primeira deve ser processada (`PROCESSED`, saldo restante R$ 20) e a segunda rejeitada (`REJECTED` com `INSUFFICIENT_FUNDS`), com commit da rejeição sem debitar saldo.

### 2.3. Idempotência Persistente e Hash Canônico
1. **Header Obrigatório:** Requisições financeiras externas utilizam o header `Idempotency-Key`.
2. **Hash Canônico Determinístico:** O SHA-256 é calculado com chaves JSON estritamente ordenadas alfabeticamente (`amount`, `currency`, `externalTransactionId`, `gameId`, `kind`, `playerId`, `providerId`, `roundId`, `walletId`, `referenceExternalTransactionId`).
3. **Resolução de Conflitos:**
   - Mesma chave + mesmo payload: Retorna snapshot original persistido com `"idempotentReplay": true`.
   - Mesma chave + payload divergente: Retorna `409 Conflict`.
   - Mesma tupla `(providerId, externalTransactionId)` com chave diferente: Retorna `409 Conflict`.

### 2.4. Ledger Imutável e Append-Only
1. **Tabela `wallet_ledger`:** Somente operações de crédito/débito aprovadas geram lançamentos. Lançamentos nunca são atualizados ou apagados (`UPDATE`/`DELETE` proibidos por trigger SQL).
2. **Sem Ledger para LOSS ou Rejeições:** Operações do tipo `LOSS` e transações rejeitadas não geram registro no ledger nem incrementam a versão da carteira.
3. **Reconciliação Contábil:** O endpoint `/wallets/:walletId/reconciliation` valida se $\sum \text{Créditos} - \sum \text{Débitos} = \text{Saldo Atual}$.

### 2.5. Máquina de Estados e Resolução de Referências Fora de Ordem
1. **Estados:** `PENDING`, `PROCESSED`, `PENDING_REFERENCE`, `REJECTED`.
2. **Estorno Fora de Ordem:** Se um `REFUND` ou `ROLLBACK` chegar antes da aposta original (`BET`), o status deve ser gravado como `PENDING_REFERENCE`.
3. **Worker em Background:** `PendingReferenceResolver` busca periodicamente transações `PENDING_REFERENCE` e as processa quando a aposta original estiver confirmada, respeitando o TTL de 24 horas.
4. **Prevenção a Duplo Estorno:** Uma transação nunca pode receber dois estornos do mesmo tipo (`HasExistingReversal`).

### 2.6. Mensageria: Padrões Outbox & Inbox
1. **Transactional Outbox:** Eventos de domínio são persistidos na tabela `outbox` na mesma transação atômica do PostgreSQL.
2. **Publicação com Lock Concorrente:** `OutboxPublisher` consome a fila com `SELECT ... FOR UPDATE SKIP LOCKED`.
3. **Inbox Pattern:** Consumo de mensagens SQS registra no `inbox` antes de processar, garantindo idempotência e commit antes do `DeleteMessage`.

### 2.7. Autenticação e Multi-Tenancy
1. **OIDC/Keycloak:** Provedores autenticados via JWT Bearer Token.
2. **Isolamento de Provedores:** Operações validam `provider_id` do token contra o payload; retorno `403 Forbidden` se divergente.
3. **Rotas Internas:** Criação de carteira exige claim `is_internal: true`.

---

## 3. Padrões de Código e Comandos de Operação

* **Compilação e Verificação:**
  ```bash
  go build ./...
  go vet ./...
  ```
* **Testes Unitários:**
  ```bash
  go test -v ./...
  ```
* **Testes com Race Detector:**
  ```bash
  go test -race ./...
  ```
* **Ambiente Completo:**
  ```bash
  docker compose up --build
  ```
