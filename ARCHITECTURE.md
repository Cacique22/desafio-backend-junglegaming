# ARCHITECTURE.md — Distributed Wagering Engine

Este documento detalha as decisões de engenharia, garantias matemáticas, modelos de concorrência, tolerância a falhas e arquitetura de software implementadas no motor distribuído de apostas da **Jungle Gaming**.

---

## 1. Precisão Monetária e Representação de Dinheiro (`Money`)

### 1.1. Proibição Absoluta de Ponto Flutuante
Valores em `float32` ou `float64` sofrem de imprecisão de representação binária inerente ao padrão IEEE 754 (por exemplo, `0.1 + 0.2 = 0.30000000000000004`). Em sistemas financeiros e de jogos com alto volume, acumular erros de arredondamento em floats corrompe a integridade contábil e inviabiliza auditorias.

### 1.2. Modelagem por Unidades Mínimas (`int64` centavos)
* O Value Object `money.Money` armazena a quantia como um número inteiro de centavos (`cents int64`) e a moeda como uma string ISO 4217 de 3 letras (`currency string`).
* Exemplo: `R$ 25.00` é representado internamente e persistido no PostgreSQL como `2500` na coluna `BIGINT` (`balance_cents`).
* **Proteção contra Overflow:** Todas as operações aritméticas (`Add`, `Sub`, `Neg` e parsing) implementam validação preventiva de estouro de limites (`math.MaxInt64` e `math.MinInt64`).
* **Imutabilidade:** O struct `Money` é estritamente imutável. Qualquer operação gera uma nova instância.
* **Validação de Entrada:**
  * O parser rejeita strings vazias, notação científica (`1e5`), `NaN`, `Infinity`, caracteres não numéricos e escalas excedentes (mais de 2 casas decimais).
  * Valores negativos são rejeitados em entradas externas. São permitidos apenas para diferenças internas e reconciliação.

---

## 2. Controle de Concorrência e Isolamento de Carteiras

### 2.1. Bloqueio Pessimista por Carteira (`SELECT ... FOR UPDATE`)
Para coordenar concorrência sem risco de *lost updates* (atualizações perdidas) ou saldos negativos:
1. Quando uma requisição financeira chega para `wallet_id`, a transação SQL adquire um lock pessimista exclusivo na linha da carteira:
   ```sql
   SELECT id, player_id, currency, balance_cents, version, created_at, updated_at
   FROM wallets
   WHERE id = $1
   FOR UPDATE;
   ```
2. **Paralelismo Horizontal:** Transações de carteiras diferentes (`wallet_A` e `wallet_B`) adquirem locks em linhas distintas e executam em paralelo absoluto, sem contenção e sem qualquer lock global de tabela.
3. **Serialização por Carteira:** Operações simultâneas na **mesma** carteira são estritamente enfileiradas pelo motor de MVCC/locks do PostgreSQL.
4. **Resolução do Teste Obrigatório (2 apostas de R$ 80 disputando R$ 100):**
   * A primeira aposta adquire o lock, debita R$ 80.00, atualiza o saldo para R$ 20.00 e comita.
   * A segunda aposta é liberada do lock, lê o saldo atualizado de R$ 20.00, falha na invariante de domínio (`balance >= amount`), é rejeitada com `INSUFFICIENT_FUNDS` e comitada como rejeição de negócio (sem débito e sem corrupção do saldo).

---

## 3. Idempotência Persistente e Hash Canônico

### 3.1. Chave e Hash Determinístico
O sistema recebe a chave via header `Idempotency-Key` e calcula o hash SHA-256 dos campos de negócio canônicos:
* **JSON Canônico:** Ordenação estrita das chaves alfabeticamente (`amount`, `currency`, `externalTransactionId`, `gameId`, `kind`, `playerId`, `providerId`, `roundId`, `walletId`, `referenceExternalTransactionId`).
* **Metadados de Transporte Excluídos:** Headers HTTP, timestamps e identificadores de envelope não afetam o hash.

### 3.2. Regras de Resolução
1. **Mesma Chave + Mesmo Payload:** O sistema retorna imediatamente o resultado original persistido no banco com `"idempotentReplay": true`. O saldo retornado é o snapshot do momento do processamento original.
2. **Mesma Chave + Payload Diferente:** Retorna `409 Conflict`.
3. **Mesma Operação `(providerId, externalTransactionId)` com Chave Diferente:** Retorna `409 Conflict`. Nenhuma operação pode ser reaplicada sob outra chave.

---

## 4. Ledger Imutável e Append-Only

### 4.1. Garantias no Banco de Dados
* Cada movimentação financeira aprovada insere um registro na tabela `wallet_ledger`.
* **Constraint de Unicidade:** `UNIQUE (wallet_id, transaction_id)` garante que uma transação nunca produza mais de um lançamento na mesma carteira.
* **Proteção Ativa:** Trigger `trg_prevent_ledger_modification` no PostgreSQL intercepta e aborta qualquer tentativa de `UPDATE` ou `DELETE` na tabela `wallet_ledger`.
* Operações do tipo `LOSS` e transações rejeitadas **não** geram lançamento no ledger nem alteram a versão da carteira.

### 4.2. Reconciliação
O endpoint `POST /wallets/:walletId/reconciliation` executa uma query somando todos os créditos e subtraindo todos os débitos históricos:
$$\text{CalculatedBalance} = \sum \text{Credits} - \sum \text{Debits}$$
A consistência é garantida quando $\text{StoredBalance} - \text{CalculatedBalance} = 0.00$.

---

## 5. Máquina de Estados e Resolução de Referências

### 5.1. Estados da Transação
```
              ┌──────────────────────────┐
              │         PENDING          │
              └─────────────┬────────────┘
                            │
       ┌────────────────────┼────────────────────┐
       ▼                    ▼                    ▼
┌──────────────┐   ┌─────────────────┐   ┌──────────────┐
│  PROCESSED   │   │PENDING_REFERENCE│   │   REJECTED   │
│  (Terminal)  │   └────────┬────────┘   │  (Terminal)  │
└──────────────┘            │            └──────────────┘
                            ▼ (Worker TTL)
                   ┌─────────────────┐
                   │    REJECTED     │
                   └─────────────────┘
```

### 5.2. Chegada Fora de Ordem (`REFUND` / `ROLLBACK` antes da `BET`)
* Se um estorno ou rollback chegar antes da aposta original, ele é gravado no banco como `PENDING_REFERENCE`.
* Um worker em background (`PendingReferenceResolver`) consulta periodicamente as transações pendentes. Quando a aposta original é processada, o estorno é aplicado e transiciona para `PROCESSED`.
* Caso a referência não chegue antes do TTL (24h), a transação é finalizada como `REJECTED` com `failureCode: "REFERENCE_EXPIRED"`.
* **Proteção contra Reversão Dupla e Combinações Cruzadas:** A tabela é consultada (`HasExistingReversalTx` com filtro `kind IN ('REFUND', 'ROLLBACK')` e status `PROCESSED`) sob o lock transacional da carteira (`SELECT ... FOR UPDATE`). Isso garante que nenhuma transação referenciada (`BET`) receba devolução duplicada de fundos sob **qualquer combinação** (`BET` -> `REFUND` -> `ROLLBACK`, `BET` -> `ROLLBACK` -> `REFUND` ou reversões do mesmo tipo). Caso já exista qualquer estorno processado para a referência, qualquer reversão subsequente é rejeitada de forma atômica e imediatamente com código de falha `DOUBLE_REVERSAL` (HTTP 422), preservando a coerência financeira e impedindo crédito duplicado.

---

## 6. Mensageria: Padrões Inbox & Transactional Outbox

### 6.1. Transactional Outbox (Zero Perda de Eventos)
* Quando uma operação financeira é concluída, os eventos de domínio (`WagerTransactionProcessed`, `WalletBalanceChanged`) são serializados e inseridos na tabela `outbox` **dentro da mesma transação SQL** que atualizou a carteira e o ledger.
* Se a aplicação cair antes do commit, nada é persistido e nenhum evento é publicado.
* Se o commit ocorrer, os eventos estão gravados de forma durável.
* **Publicador Concorrente:** O worker `OutboxPublisher` consome a outbox usando `SELECT ... FOR UPDATE SKIP LOCKED`. Múltiplas instâncias do serviço podem publicar em paralelo sem conflitos ou duplicações.

### 6.2. Inbox Pattern (Consumidor SQS FIFO)
* O consumidor SQS registra o `messageId` na tabela `inbox` com lock exclusivo.
* Se a mensagem já estiver marcada como concluída, o consumidor apenas a remove do SQS.
* A mensagem só é deletada da fila SQS **após** o commit com sucesso de todas as alterações no PostgreSQL.

---

## 7. Autenticação e Multi-Tenancy (Keycloak OIDC)

* A autenticação é baseada em tokens JWT no padrão OAuth 2.0 / OIDC (`client_credentials`).
* O middleware `AuthMiddleware` valida a assinatura e extrai o `provider_id` do token.
* **Isolamento de Provedores:** O provedor autenticado só pode submeter operações e consultar transações cujo `providerId` seja idêntico à sua identidade. Tentativas de acessar dados de outro provedor retornam `403 Forbidden`.
* Operações internas como abertura de carteira (`POST /wallets`) exigem a claim `is_internal: true`.

---

## 8. Composição e Ciclo de Vida com Uber Fx

* Toda a aplicação é composta por módulos Fx desacoplados: `ConfigModule`, `DatabaseModule`, `SQSModule`, `RepositoryModule`, `UseCaseModule`, `AuthModule`, `HTTPModule` e `WorkerModule`.
* **Graceful Shutdown (`fx.Lifecycle`):**
  1. Em `SIGTERM`, o servidor HTTP para de aceitar novas requisições.
  2. Os workers de SQS e Outbox encerram o processamento das mensagens atuais e liberam recursos.
  3. O pool de conexões do PostgreSQL (`pgxpool`) é drenado e fechado de forma limpa.

---

## 9. Limitações, Interpretações Adotadas e Trabalho Futuro

### 9.1. Interpretações Adotadas
1. **Reversões Cruzadas e Combinações de `REFUND` e `ROLLBACK`:**
   Adotou-se a regra de que uma aposta (`BET`) original só pode sofrer estorno financeiro **uma única vez**, independentemente de o provedor submeter um `REFUND` ou um `ROLLBACK`. Qualquer tentativa de reversão subsequente para a mesma referência sob qualquer combinação (`BET` -> `REFUND` -> `ROLLBACK` ou `BET` -> `ROLLBACK` -> `REFUND`) é rejeitada com código `DOUBLE_REVERSAL` (HTTP 422), preservando a integridade contábil estrita da carteira.
2. **Chegada Fora de Ordem e TTL:**
   Quando uma reversão chega antes da aposta original, ela é persistida como `PENDING_REFERENCE`. O worker `PendingReferenceResolver` busca periodicamente essas pendências. Adotou-se um TTL de 24 horas; caso a aposta referenciada nunca chegue nesse período, a transação transiciona para `REJECTED` com `failureCode: "REFERENCE_EXPIRED"`.
3. **Escopo do Ledger Contábil:**
   Transações com movimentação líquida zero (como `LOSS` de saldo R$ 0,00) ou transações rejeitadas (`REJECTED`) não geram lançamentos na tabela `wallet_ledger`, pois o ledger reflete estritamente lançamentos de crédito e débito efetivos.
4. **Isolamento de Provedores:**
   O `provider_id` contido no token JWT deve ser idêntico ao campo `providerId` no corpo JSON da requisição. Tentativas de submissão cruzada ou consulta de transações de terceiros resultam em `403 Forbidden`.

### 9.2. Limitações Conhecidas
1. **Serialização por Carteira (Pessimistic Row-Level Lock):**
   A garantia estrita de consistência financeira imediata serializa operações na mesma carteira via `SELECT ... FOR UPDATE` no PostgreSQL. Essa abordagem elimina race conditions e saldo negativo, mas impõe um limite físico de throughput por carteira individual (jogadores distintos executam em paralelo sem bloqueio mútuo).
2. **LocalStack FIFO vs AWS SQS Produção:**
   No ambiente local, o LocalStack 3.7 simula as filas FIFO. O agrupamento por entidade é feito via `MessageGroupId: wallet_id`, garantindo ordenação estrita por jogador em produção.

### 9.3. Trabalho Não Concluído / Extensões Futuras (Diferenciais Opcionais)
1. **Partidas Dobradas Globais (Double-Entry Bookkeeping Completo):**
   Atualmente, o ledger audita detalhadamente a carteira do apostador ($\sum \text{Créditos} - \sum \text{Débitos} = \text{Saldo}$). Uma evolução natural seria modelar as contas de contrapartida da casa (GGR, contas de retenção de bônus e contas de custódia de provedor).
2. **Distributed Tracing (OpenTelemetry):**
   Instrumentação com propagação de trace contexts (W3C TraceContext) entre as requisições HTTP, filas SQS FIFO e transações do PostgreSQL via spans do OpenTelemetry.

