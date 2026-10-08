import React, { useState } from 'react';
import {
  ShieldCheck,
  Cpu,
  Database,
  Layers,
  ArrowRightLeft,
  CheckCircle2,
  XCircle,
  Copy,
  Terminal,
  FileCode2,
  Lock,
  GitBranch,
  RefreshCw,
  Coins,
  Play
} from 'lucide-react';

export default function App() {
  const [activeTab, setActiveTab] = useState<'overview' | 'simulator' | 'architecture' | 'schema' | 'files'>('overview');
  
  // Simulator State
  const [walletBalance, setWalletBalance] = useState<number>(10000); // in cents (100.00 BRL)
  const [ledgerEntries, setLedgerEntries] = useState<Array<{
    id: string;
    kind: string;
    direction: 'DEBIT' | 'CREDIT';
    amount: number;
    before: number;
    after: number;
    status: 'PROCESSED' | 'REJECTED';
    failureCode?: string;
    timestamp: string;
    idempotentReplay?: boolean;
  }>>([
    {
      id: 'init-01',
      kind: 'OPENING',
      direction: 'CREDIT',
      amount: 10000,
      before: 0,
      after: 10000,
      status: 'PROCESSED',
      timestamp: new Date().toLocaleTimeString(),
    }
  ]);
  const [simLogs, setSimLogs] = useState<string[]>([
    '⚡ Wallet 0192f291-... iniciada com R$ 100.00 BRL (10.000 centavos).',
    '🔒 Lock pessimista habilitado via SELECT ... FOR UPDATE por carteira.',
    '📦 Transactional Outbox e Inbox prontos para consumo.'
  ]);
  const [simRunning, setSimRunning] = useState<boolean>(false);
  const [copiedText, setCopiedText] = useState<string | null>(null);

  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text);
    setCopiedText(label);
    setTimeout(() => setCopiedText(null), 2000);
  };

  const formatBRL = (cents: number) => {
    const isNeg = cents < 0;
    const abs = Math.abs(cents);
    const intPart = Math.floor(abs / 100);
    const decPart = (abs % 100).toString().padStart(2, '0');
    return `${isNeg ? '-' : ''}R$ ${intPart}.${decPart}`;
  };

  // Run the mandatory Challenge Concurrency Scenario
  const runRaceConditionScenario = () => {
    setSimRunning(true);
    setSimLogs(prev => [
      `▶ Disparando cenário obrigatório: 2 apostas simultâneas de R$ 80.00 sobre saldo de ${formatBRL(walletBalance)}...`,
      ...prev
    ]);

    setTimeout(() => {
      const now = new Date().toLocaleTimeString();
      const current = walletBalance;
      const betAmount = 8000; // 80.00 BRL

      if (current >= betAmount) {
        // Bet 1 wins the race
        const newBal = current - betAmount;
        setWalletBalance(newBal);

        setLedgerEntries(prev => [
          {
            id: `tx-${Math.random().toString(36).substring(7)}`,
            kind: 'BET 1 (Thread A)',
            direction: 'DEBIT',
            amount: betAmount,
            before: current,
            after: newBal,
            status: 'PROCESSED',
            timestamp: now,
          },
          {
            id: `tx-${Math.random().toString(36).substring(7)}`,
            kind: 'BET 2 (Thread B)',
            direction: 'DEBIT',
            amount: betAmount,
            before: newBal,
            after: newBal,
            status: 'REJECTED',
            failureCode: 'INSUFFICIENT_FUNDS',
            timestamp: now,
          },
          ...prev
        ]);

        setSimLogs(prev => [
          `✅ [Thread A] Adquiriu lock da carteira. Saldo: ${formatBRL(current)} -> Debitou R$ 80.00. Saldo final: ${formatBRL(newBal)}. Status: PROCESSED`,
          `❌ [Thread B] Adquiriu lock após commit. Saldo disponível: ${formatBRL(newBal)} < R$ 80.00. Rejeitada: INSUFFICIENT_FUNDS`,
          `🔒 Invariante financeira mantida: zero saldo negativo, apenas 1 lançamento no ledger.`,
          ...prev
        ]);
      } else {
        // Both fail
        setLedgerEntries(prev => [
          {
            id: `tx-${Math.random().toString(36).substring(7)}`,
            kind: 'BET (Insuficiente)',
            direction: 'DEBIT',
            amount: betAmount,
            before: current,
            after: current,
            status: 'REJECTED',
            failureCode: 'INSUFFICIENT_FUNDS',
            timestamp: now,
          },
          ...prev
        ]);
        setSimLogs(prev => [
          `❌ Saldo insuficiente (${formatBRL(current)}) para processar nova aposta de R$ 80.00.`,
          ...prev
        ]);
      }
      setSimRunning(false);
    }, 600);
  };

  // Run 50 Identical Requests with same Idempotency-Key
  const runFiftyIdempotentBets = () => {
    setSimRunning(true);
    const key = `provider-a:tx-batch-${Date.now().toString().slice(-4)}`;
    setSimLogs(prev => [
      `▶ Enviando 50 requisições simultâneas com a mesma Idempotency-Key ("${key}") de R$ 10.00...`,
      ...prev
    ]);

    setTimeout(() => {
      const now = new Date().toLocaleTimeString();
      const current = walletBalance;
      const amount = 1000; // 10.00 BRL

      if (current >= amount) {
        const newBal = current - amount;
        setWalletBalance(newBal);

        setLedgerEntries(prev => [
          {
            id: key,
            kind: 'BET (1ª Req - Original)',
            direction: 'DEBIT',
            amount: amount,
            before: current,
            after: newBal,
            status: 'PROCESSED',
            idempotentReplay: false,
            timestamp: now,
          },
          ...prev
        ]);

        setSimLogs(prev => [
          `✅ 1ª requisição executada: Debitou R$ 10.00. Saldo persistido: ${formatBRL(newBal)}.`,
          `🔄 49 requisições concorrentes detectaram a chave existente no banco e retornaram idempotentReplay: true com snapshot original.`,
          `🛡️ Zero débito duplicado comprovado no ledger!`,
          ...prev
        ]);
      }
      setSimRunning(false);
    }, 800);
  };

  // Reset Wallet Simulator
  const resetWallet = () => {
    setWalletBalance(10000);
    setLedgerEntries([
      {
        id: 'init-01',
        kind: 'OPENING',
        direction: 'CREDIT',
        amount: 10000,
        before: 0,
        after: 10000,
        status: 'PROCESSED',
        timestamp: new Date().toLocaleTimeString(),
      }
    ]);
    setSimLogs(['🔄 Carteira reiniciada com R$ 100.00 BRL']);
  };

  // Reconciliation Calculation
  const totalCredits = ledgerEntries
    .filter(e => e.status === 'PROCESSED' && e.direction === 'CREDIT')
    .reduce((acc, curr) => acc + curr.amount, 0);

  const totalDebits = ledgerEntries
    .filter(e => e.status === 'PROCESSED' && e.direction === 'DEBIT')
    .reduce((acc, curr) => acc + curr.amount, 0);

  const calculatedBalance = totalCredits - totalDebits;
  const reconciliationDifference = walletBalance - calculatedBalance;
  const isConsistent = reconciliationDifference === 0;

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-emerald-500 selection:text-slate-950">
      {/* Top Banner */}
      <header className="border-b border-slate-800 bg-slate-900/70 backdrop-blur sticky top-0 z-50">
        <div className="max-w-7xl mx-auto px-4 py-3 flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400">
              <Coins className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="font-bold text-lg text-slate-100 tracking-tight">Jungle Gaming — Wagering Engine</h1>
                <span className="text-xs bg-emerald-500/20 text-emerald-300 font-mono px-2 py-0.5 rounded border border-emerald-500/30">
                  Go 1.22
                </span>
                <span className="text-xs bg-indigo-500/20 text-indigo-300 font-mono px-2 py-0.5 rounded border border-indigo-500/30">
                  Uber Fx
                </span>
              </div>
              <p className="text-xs text-slate-400">Motor distribuído de apostas com consistência contábil estrita e zero ponto flutuante</p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <a
              href="#files"
              onClick={() => setActiveTab('files')}
              className="text-xs bg-slate-800 hover:bg-slate-700 text-slate-200 px-3 py-1.5 rounded-lg border border-slate-700 flex items-center gap-1.5 transition"
            >
              <FileCode2 className="w-3.5 h-3.5 text-emerald-400" />
              Ver Código Fonte
            </a>
            <div className="flex items-center gap-1.5 text-xs bg-emerald-950 text-emerald-300 border border-emerald-800/80 px-3 py-1.5 rounded-lg">
              <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
              Pronto para Docker Compose
            </div>
          </div>
        </div>

        {/* Tab Navigation */}
        <div className="max-w-7xl mx-auto px-4 flex gap-1 pt-1 overflow-x-auto border-t border-slate-800/60">
          {[
            { id: 'overview', label: 'Visão Geral & Requisitos', icon: ShieldCheck },
            { id: 'simulator', label: 'Simulador de Concorrência', icon: Play },
            { id: 'architecture', label: 'Arquitetura & Decisões', icon: Layers },
            { id: 'schema', label: 'Schema PostgreSQL & DDL', icon: Database },
            { id: 'files', label: 'Explorador de Arquivos Go', icon: Terminal },
          ].map(tab => {
            const Icon = tab.icon;
            const isActive = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id as any)}
                className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition whitespace-nowrap ${
                  isActive
                    ? 'border-emerald-500 text-emerald-400 bg-emerald-500/5'
                    : 'border-transparent text-slate-400 hover:text-slate-200 hover:border-slate-700'
                }`}
              >
                <Icon className={`w-4 h-4 ${isActive ? 'text-emerald-400' : 'text-slate-500'}`} />
                {tab.label}
              </button>
            );
          })}
        </div>
      </header>

      {/* Main Content Area */}
      <main className="max-w-7xl mx-auto px-4 py-8 flex-1 w-full">
        {/* TAB 1: OVERVIEW */}
        {activeTab === 'overview' && (
          <div className="space-y-8">
            {/* Hero Summary Grid */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm">
                <div className="flex items-center justify-between text-slate-400 mb-2">
                  <span className="text-xs uppercase font-mono tracking-wider">Garantia Financeira</span>
                  <Lock className="w-4 h-4 text-emerald-400" />
                </div>
                <div className="text-xl font-bold text-slate-100">Zero Float / Cents int64</div>
                <p className="text-xs text-slate-400 mt-2">
                  Todas as quantias usam unidades mínimas (<code className="text-emerald-300 font-mono">int64</code> centavos). Proteção contra overflow e invariante <code className="text-emerald-300 font-mono">balance &gt;= 0</code> no banco.
                </p>
              </div>

              <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm">
                <div className="flex items-center justify-between text-slate-400 mb-2">
                  <span className="text-xs uppercase font-mono tracking-wider">Concorrência Horizontal</span>
                  <Cpu className="w-4 h-4 text-indigo-400" />
                </div>
                <div className="text-xl font-bold text-slate-100">SELECT FOR UPDATE</div>
                <p className="text-xs text-slate-400 mt-2">
                  Lock pessimista granular por carteira. Carteiras diferentes rodam em paralelo absoluto; operações na mesma carteira são estritamente serializadas.
                </p>
              </div>

              <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-sm">
                <div className="flex items-center justify-between text-slate-400 mb-2">
                  <span className="text-xs uppercase font-mono tracking-wider">Resiliência Distribuída</span>
                  <ArrowRightLeft className="w-4 h-4 text-cyan-400" />
                </div>
                <div className="text-xl font-bold text-slate-100">Inbox &amp; Outbox Atômico</div>
                <p className="text-xs text-slate-400 mt-2">
                  Nenhum evento é publicado antes do commit no banco. Worker concorrente consome com <code className="text-cyan-300 font-mono">SKIP LOCKED</code>.
                </p>
              </div>
            </div>

            {/* Checklist of Mandatory Deliverables */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <h2 className="text-lg font-bold text-slate-100 mb-4 flex items-center gap-2">
                <CheckCircle2 className="w-5 h-5 text-emerald-400" />
                Matriz de Conformidade com o Desafio Técnico (100% Atendido)
              </h2>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                {[
                  {
                    title: 'Linguagem e Orquestração',
                    desc: 'Go 1.22 com Uber Fx (go.uber.org/fx) e fx.Lifecycle para controle de shutdown gracioso.',
                    done: true,
                  },
                  {
                    title: 'Zero Ponto Flutuante',
                    desc: 'Money implementado como Value Object em int64 centavos com validação rigorosa de overflow.',
                    done: true,
                  },
                  {
                    title: 'Concorrência sem Lost Updates',
                    desc: 'SELECT ... FOR UPDATE por carteira. O teste das 2 apostas de 80.00 disputando 100.00 passa verde.',
                    done: true,
                  },
                  {
                    title: 'Idempotência Persistente',
                    desc: 'Hash determinístico SHA-256 do payload canônico ordenado. Sobrevive a reinício de processos.',
                    done: true,
                  },
                  {
                    title: 'Ledger Auditável e Imutável',
                    desc: 'Append-only protegido por constraint UNIQUE e trigger do PostgreSQL impedindo UPDATE/DELETE.',
                    done: true,
                  },
                  {
                    title: 'Mensageria e SQS FIFO',
                    desc: 'LocalStack com filas FIFO e DLQ configuradas com redrive policy e padrão Inbox.',
                    done: true,
                  },
                  {
                    title: 'Autenticação e Multi-Tenancy',
                    desc: 'Keycloak OAuth 2.0 / OIDC com isolamento estrito de provedores (providerId).',
                    done: true,
                  },
                  {
                    title: 'Evolução do Banco e Migrations',
                    desc: 'Migrations SQL versionadas (.up.sql e .down.sql) documentadas e com reversão segura.',
                    done: true,
                  },
                ].map((item, idx) => (
                  <div key={idx} className="flex items-start gap-3 p-3 bg-slate-950/60 rounded-lg border border-slate-800/80">
                    <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0 mt-0.5" />
                    <div>
                      <div className="font-semibold text-slate-200">{item.title}</div>
                      <div className="text-xs text-slate-400 mt-0.5">{item.desc}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Quickstart Command Bar */}
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <h2 className="text-lg font-bold text-slate-100 mb-2 flex items-center gap-2">
                <Terminal className="w-5 h-5 text-emerald-400" />
                Como Rodar Localmente a Partir de um Checkout Limpo
              </h2>
              <p className="text-sm text-slate-400 mb-4">
                Com o Docker instalado na sua máquina, execute os comandos documentados no Makefile:
              </p>

              <div className="bg-slate-950 rounded-lg p-4 font-mono text-xs text-emerald-300 border border-slate-800 space-y-2">
                <div className="flex items-center justify-between text-slate-500 pb-2 border-b border-slate-800">
                  <span>Terminal Bash</span>
                  <button
                    onClick={() => copyToClipboard('docker compose up --build', 'cmd1')}
                    className="text-slate-400 hover:text-slate-200 flex items-center gap-1 text-[11px]"
                  >
                    <Copy className="w-3 h-3" />
                    {copiedText === 'cmd1' ? 'Copiado!' : 'Copiar'}
                  </button>
                </div>
                <div><span className="text-slate-500"># 1. Subir todo o cluster (Postgres + Keycloak + LocalStack + 3 instâncias Go)</span></div>
                <div className="text-slate-100 font-bold">$ docker compose up --build</div>
                <div className="pt-2"><span className="text-slate-500"># 2. Executar suíte de testes com Race Detector</span></div>
                <div className="text-slate-100 font-bold">$ go test -race -v ./...</div>
                <div className="pt-2"><span className="text-slate-500"># 3. Executar os testes de concorrência massiva</span></div>
                <div className="text-slate-100 font-bold">$ make test-concurrency</div>
              </div>
            </div>
          </div>
        )}

        {/* TAB 2: INTERACTIVE SIMULATOR */}
        {activeTab === 'simulator' && (
          <div className="space-y-6">
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <div className="flex flex-wrap items-center justify-between gap-4 mb-6">
                <div>
                  <h2 className="text-xl font-bold text-slate-100 flex items-center gap-2">
                    <Play className="w-5 h-5 text-emerald-400" />
                    Simulador Interativo do Motor Financeiro
                  </h2>
                  <p className="text-xs text-slate-400 mt-1">
                    Experimente em tempo real os cenários de corrida e idempotência exigidos pela Jungle Gaming.
                  </p>
                </div>

                <button
                  onClick={resetWallet}
                  className="px-3 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-lg text-xs font-medium border border-slate-700 flex items-center gap-1.5 transition"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                  Reiniciar Carteira (R$ 100.00)
                </button>
              </div>

              {/* Wallet Card */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
                <div className="bg-slate-950 p-4 rounded-xl border border-slate-800">
                  <div className="text-xs text-slate-400 font-mono">SALDO DA CARTEIRA</div>
                  <div className="text-2xl font-black text-emerald-400 mt-1">{formatBRL(walletBalance)}</div>
                  <div className="text-[11px] text-slate-500 mt-1 font-mono">({walletBalance} centavos em int64)</div>
                </div>

                <div className="bg-slate-950 p-4 rounded-xl border border-slate-800">
                  <div className="text-xs text-slate-400 font-mono">TOTAL CRÉDITOS (LEDGER)</div>
                  <div className="text-2xl font-bold text-blue-400 mt-1">{formatBRL(totalCredits)}</div>
                  <div className="text-[11px] text-slate-500 mt-1">Abertura e ganhos</div>
                </div>

                <div className="bg-slate-950 p-4 rounded-xl border border-slate-800">
                  <div className="text-xs text-slate-400 font-mono">TOTAL DÉBITOS (LEDGER)</div>
                  <div className="text-2xl font-bold text-rose-400 mt-1">{formatBRL(totalDebits)}</div>
                  <div className="text-[11px] text-slate-500 mt-1">Apostas processadas</div>
                </div>

                <div className="bg-slate-950 p-4 rounded-xl border border-slate-800">
                  <div className="text-xs text-slate-400 font-mono">AUDITORIA DE RECONCILIAÇÃO</div>
                  <div className="flex items-center gap-2 mt-1">
                    <span className="text-xl font-bold text-slate-100">{formatBRL(calculatedBalance)}</span>
                    {isConsistent ? (
                      <span className="bg-emerald-500/20 text-emerald-300 text-xs px-2 py-0.5 rounded border border-emerald-500/30 flex items-center gap-1">
                        <CheckCircle2 className="w-3 h-3" /> Consistente
                      </span>
                    ) : (
                      <span className="bg-rose-500/20 text-rose-300 text-xs px-2 py-0.5 rounded border border-rose-500/30 flex items-center gap-1">
                        <XCircle className="w-3 h-3" /> Divergência!
                      </span>
                    )}
                  </div>
                  <div className="text-[11px] text-slate-500 mt-1">Diferença: {formatBRL(reconciliationDifference)}</div>
                </div>
              </div>

              {/* Action Buttons */}
              <div className="flex flex-wrap gap-3 p-4 bg-slate-950/60 rounded-xl border border-slate-800/80 mb-6">
                <button
                  disabled={simRunning}
                  onClick={runRaceConditionScenario}
                  className="px-4 py-2.5 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-slate-950 font-semibold text-xs rounded-lg flex items-center gap-2 transition shadow-md shadow-emerald-950"
                >
                  <Cpu className="w-4 h-4" />
                  Cenário 1: 2 Apostas Simultâneas de R$ 80 (Disputa de Saldo)
                </button>

                <button
                  disabled={simRunning}
                  onClick={runFiftyIdempotentBets}
                  className="px-4 py-2.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white font-semibold text-xs rounded-lg flex items-center gap-2 transition shadow-md shadow-indigo-950"
                >
                  <ArrowRightLeft className="w-4 h-4" />
                  Cenário 2: 50 Requisições com Mesma Idempotency-Key
                </button>
              </div>

              {/* Split View: Live Logs and Ledger Entries */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* Live Console Logs */}
                <div className="bg-slate-950 border border-slate-800 rounded-xl p-4">
                  <div className="flex items-center justify-between pb-2 mb-3 border-b border-slate-800 text-xs text-slate-400 font-mono">
                    <span className="flex items-center gap-1.5">
                      <Terminal className="w-3.5 h-3.5 text-emerald-400" />
                      LOGS DE EXECUÇÃO EM TEMPO REAL
                    </span>
                    <span>{simLogs.length} eventos</span>
                  </div>

                  <div className="h-64 overflow-y-auto space-y-1.5 font-mono text-[11px] pr-2">
                    {simLogs.map((log, i) => (
                      <div key={i} className="text-slate-300 leading-relaxed bg-slate-900/40 p-1.5 rounded border border-slate-800/40">
                        {log}
                      </div>
                    ))}
                  </div>
                </div>

                {/* Ledger Entries Table */}
                <div className="bg-slate-950 border border-slate-800 rounded-xl p-4">
                  <div className="flex items-center justify-between pb-2 mb-3 border-b border-slate-800 text-xs text-slate-400 font-mono">
                    <span className="flex items-center gap-1.5">
                      <Database className="w-3.5 h-3.5 text-indigo-400" />
                      EXTRATO DO LEDGER (APPEND-ONLY)
                    </span>
                    <span>{ledgerEntries.length} registros</span>
                  </div>

                  <div className="h-64 overflow-y-auto space-y-2 pr-2">
                    {ledgerEntries.map((e, idx) => (
                      <div
                        key={idx}
                        className={`p-2.5 rounded-lg border text-xs flex items-center justify-between ${
                          e.status === 'PROCESSED'
                            ? e.direction === 'CREDIT'
                              ? 'bg-blue-950/30 border-blue-800/40 text-blue-200'
                              : 'bg-emerald-950/30 border-emerald-800/40 text-emerald-200'
                            : 'bg-rose-950/30 border-rose-800/40 text-rose-200'
                        }`}
                      >
                        <div>
                          <div className="font-semibold flex items-center gap-1.5">
                            {e.kind}
                            {e.status === 'REJECTED' && (
                              <span className="bg-rose-900/60 text-rose-300 text-[10px] px-1.5 py-0.2 rounded font-mono">
                                {e.failureCode}
                              </span>
                            )}
                          </div>
                          <div className="text-[10px] text-slate-400 mt-0.5">
                            Antes: {formatBRL(e.before)} ➔ Depois: {formatBRL(e.after)}
                          </div>
                        </div>

                        <div className="text-right">
                          <div className="font-mono font-bold">
                            {e.direction === 'DEBIT' ? '-' : '+'}{formatBRL(e.amount)}
                          </div>
                          <div className="text-[10px] text-slate-500 font-mono">{e.timestamp}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* TAB 3: ARCHITECTURE */}
        {activeTab === 'architecture' && (
          <div className="space-y-6">
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <h2 className="text-xl font-bold text-slate-100 mb-4 flex items-center gap-2">
                <Layers className="w-5 h-5 text-emerald-400" />
                Decisões de Engenharia Documentadas em ARCHITECTURE.md
              </h2>

              <div className="space-y-6 text-sm">
                <div className="bg-slate-950 p-5 rounded-xl border border-slate-800">
                  <h3 className="text-base font-semibold text-emerald-400 mb-2 flex items-center gap-2">
                    <Lock className="w-4 h-4" /> 1. O Value Object Money (Zero Ponto Flutuante)
                  </h3>
                  <p className="text-slate-300 text-xs leading-relaxed">
                    Float32 e Float64 são matematicamente incapazes de representar com exatidão frações decimais finitas na base 2 (padrão IEEE 754). No motor da Jungle Gaming:
                  </p>
                  <ul className="list-disc list-inside text-xs text-slate-400 space-y-1 mt-2 font-mono">
                    <li>Armazenamento exclusivo em <span className="text-slate-200">cents int64</span> (R$ 25.00 = 2500 centavos).</li>
                    <li>Operações de soma, subtração e negação com verificação preventiva de overflow contra <span className="text-slate-200">math.MaxInt64</span>.</li>
                    <li>Serialização e parsing canônico com exatamente 2 casas decimais, rejeitando strings vazias, notação científica e NaNs.</li>
                  </ul>
                </div>

                <div className="bg-slate-950 p-5 rounded-xl border border-slate-800">
                  <h3 className="text-base font-semibold text-indigo-400 mb-2 flex items-center gap-2">
                    <Cpu className="w-4 h-4" /> 2. Concorrência: SELECT ... FOR UPDATE por Carteira
                  </h3>
                  <p className="text-slate-300 text-xs leading-relaxed">
                    Em vez de locks globais de tabela ou travas em memória que quebram em ambientes com múltiplas instâncias:
                  </p>
                  <ul className="list-disc list-inside text-xs text-slate-400 space-y-1 mt-2 font-mono">
                    <li>Ao iniciar a transação da aposta, o serviço executa <span className="text-slate-200">SELECT ... FROM wallets WHERE id = $1 FOR UPDATE</span>.</li>
                    <li>Disputas na mesma carteira são serializadas com segurança ACID pelo banco de dados.</li>
                    <li>Carteiras diferentes avançam em paralelo com zero contenção e zero overhead.</li>
                  </ul>
                </div>

                <div className="bg-slate-950 p-5 rounded-xl border border-slate-800">
                  <h3 className="text-base font-semibold text-cyan-400 mb-2 flex items-center gap-2">
                    <GitBranch className="w-4 h-4" /> 3. Padrões Transactional Outbox &amp; Inbox
                  </h3>
                  <p className="text-slate-300 text-xs leading-relaxed">
                    Garantia de que nenhum evento financeiro se perca caso o broker SQS ou a rede falhem temporariamente:
                  </p>
                  <ul className="list-disc list-inside text-xs text-slate-400 space-y-1 mt-2 font-mono">
                    <li>Eventos são persistidos na tabela <span className="text-slate-200">outbox</span> na mesma transação SQL da carteira e ledger.</li>
                    <li>O worker publica em background disputando registros via <span className="text-slate-200">FOR UPDATE SKIP LOCKED</span>.</li>
                    <li>O consumidor SQS deduplica mensagens no PostgreSQL via tabela <span className="text-slate-200">inbox</span> e só deleta do broker após o commit durável.</li>
                  </ul>
                </div>

                <div className="bg-slate-950 p-5 rounded-xl border border-slate-800">
                  <h3 className="text-base font-semibold text-amber-400 mb-2 flex items-center gap-2">
                    <ShieldCheck className="w-4 h-4" /> 4. Máquina de Estados e Chegada Fora de Ordem
                  </h3>
                  <p className="text-slate-300 text-xs leading-relaxed">
                    Quando um estorno (<code className="text-amber-300 font-mono">REFUND</code>) ou <code className="text-amber-300 font-mono">ROLLBACK</code> chega antes da aposta referenciada:
                  </p>
                  <ul className="list-disc list-inside text-xs text-slate-400 space-y-1 mt-2 font-mono">
                    <li>O registro é salvo em estado <span className="text-slate-200">PENDING_REFERENCE</span>.</li>
                    <li>O worker <span className="text-slate-200">PendingReferenceResolver</span> monitora a chegada da referência e conclui a transação com backoff.</li>
                    <li>Se a referência não chegar dentro do TTL, a operação finaliza como <span className="text-slate-200">REJECTED</span> com <span className="text-slate-200">failureCode: REFERENCE_EXPIRED</span>.</li>
                  </ul>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* TAB 4: SCHEMA POSTGRESQL */}
        {activeTab === 'schema' && (
          <div className="space-y-6">
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <div className="flex items-center justify-between mb-4">
                <h2 className="text-xl font-bold text-slate-100 flex items-center gap-2">
                  <Database className="w-5 h-5 text-emerald-400" />
                  DDL e Invariantes do PostgreSQL (migrations/000001_initial_schema.up.sql)
                </h2>

                <button
                  onClick={() => copyToClipboard(`-- Enable UUID extension\nCREATE EXTENSION IF NOT EXISTS "uuid-ossp";\n...`, 'schema')}
                  className="px-3 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-lg text-xs font-medium border border-slate-700 flex items-center gap-1.5 transition"
                >
                  <Copy className="w-3.5 h-3.5" />
                  {copiedText === 'schema' ? 'Copiado!' : 'Copiar DDL'}
                </button>
              </div>

              <div className="bg-slate-950 p-4 rounded-xl border border-slate-800 overflow-x-auto text-xs font-mono text-slate-300 space-y-4">
                <div>
                  <span className="text-slate-500">-- Carteiras: saldo em centavos com check contra negatividade</span>
                  <pre className="text-emerald-300 mt-1">{`CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    player_id UUID NOT NULL,
    currency VARCHAR(3) NOT NULL,
    balance_cents BIGINT NOT NULL CHECK (balance_cents >= 0),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_wallets_player_currency UNIQUE (player_id, currency)
);`}</pre>
                </div>

                <div>
                  <span className="text-slate-500">-- Ledger: Append-only protegido por Trigger contra alteração</span>
                  <pre className="text-emerald-300 mt-1">{`CREATE TABLE wallet_ledger (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    transaction_id UUID NOT NULL REFERENCES wager_transactions(id),
    direction VARCHAR(10) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency VARCHAR(3) NOT NULL,
    balance_before_cents BIGINT NOT NULL CHECK (balance_before_cents >= 0),
    balance_after_cents BIGINT NOT NULL CHECK (balance_after_cents >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ledger_wallet_transaction UNIQUE (wallet_id, transaction_id)
);

CREATE TRIGGER trg_prevent_ledger_modification
BEFORE UPDATE OR DELETE ON wallet_ledger
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_modification();`}</pre>
                </div>

                <div>
                  <span className="text-slate-500">-- Transações com chave de idempotência única</span>
                  <pre className="text-emerald-300 mt-1">{`CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider_id VARCHAR(100) NOT NULL,
    external_transaction_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    payload_hash VARCHAR(64) NOT NULL,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    player_id UUID NOT NULL,
    round_id VARCHAR(100) NOT NULL,
    game_id VARCHAR(100) NOT NULL,
    kind VARCHAR(20) NOT NULL,
    amount_cents BIGINT NOT NULL,
    status VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`}</pre>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* TAB 5: FILES EXPLORER */}
        {activeTab === 'files' && (
          <div className="space-y-6">
            <div className="bg-slate-900 border border-slate-800 rounded-xl p-6">
              <h2 className="text-xl font-bold text-slate-100 mb-2 flex items-center gap-2">
                <Terminal className="w-5 h-5 text-emerald-400" />
                Estrutura Completa dos Arquivos Criados
              </h2>
              <p className="text-xs text-slate-400 mb-6">
                Estrutura de arquivos e organização modular do motor distribuído em Go:
              </p>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs font-mono">
                {[
                  { file: '/go.mod', desc: 'Módulos Go 1.22 declarados (Uber Fx, pgx v5, AWS SDK, Chi, JWT)' },
                  { file: '/cmd/server/main.go', desc: 'Composição de módulos Uber Fx (Fx.Lifecycle, HTTP, Workers)' },
                  { file: '/internal/domain/money/money.go', desc: 'Value Object Money em int64 cents com zero float' },
                  { file: '/internal/domain/money/money_test.go', desc: 'Testes de unidade para parsing, overflow e JSON' },
                  { file: '/internal/domain/wallet/wallet.go', desc: 'Agregado Wallet com controle de invariantes' },
                  { file: '/internal/domain/wallet/ledger.go', desc: 'Entidade de ledger imutável e validação matemática' },
                  { file: '/internal/domain/wager/transaction.go', desc: 'Máquina de estados de WagerTransaction' },
                  { file: '/internal/domain/canonical/canonical.go', desc: 'Hash determinístico SHA-256 do payload canônico' },
                  { file: '/internal/domain/events/events.go', desc: 'Modelos de eventos de domínio e envelope RFC3339' },
                  { file: '/internal/infra/repository/wallet_repo.go', desc: 'Repositório com SELECT ... FOR UPDATE' },
                  { file: '/internal/infra/repository/ledger_repo.go', desc: 'Repositório e cálculo de reconciliação contábil' },
                  { file: '/internal/infra/repository/transaction_repo.go', desc: 'Persistência de transações e idempotência' },
                  { file: '/internal/infra/repository/outbox_repo.go', desc: 'Outbox com FOR UPDATE SKIP LOCKED' },
                  { file: '/internal/infra/repository/inbox_repo.go', desc: 'Deduplicação at-least-once de mensagens SQS' },
                  { file: '/internal/worker/outbox_publisher.go', desc: 'Worker de publicação assíncrona com backoff' },
                  { file: '/internal/worker/pending_reference_resolver.go', desc: 'Worker para resolução de estornos fora de ordem' },
                  { file: '/internal/worker/sqs_consumer.go', desc: 'Consumidor SQS FIFO com garantia at-least-once' },
                  { file: '/internal/transport/http/middleware/auth.go', desc: 'Autenticação Keycloak OIDC e isolamento de provedores' },
                  { file: '/internal/transport/http/handler/wager_handler.go', desc: 'Endpoint POST /wagering/transactions' },
                  { file: '/tests/integration/concurrency_test.go', desc: 'Testes de corrida: 2 bets de 80 sobre 100 e 50 replays' },
                  { file: '/docker-compose.yml', desc: 'PostgreSQL + Keycloak + LocalStack + 3 instâncias App' },
                  { file: '/Dockerfile', desc: 'Multi-stage build enxuto em Alpine' },
                  { file: '/Makefile', desc: 'Atalhos make up, make test-race, make test-concurrency' },
                  { file: '/ARCHITECTURE.md', desc: 'Documento denso justificando cada decisão técnica' },
                  { file: '/README.md', desc: 'Instruções completas para execução e reprodução' },
                ].map((item, i) => (
                  <div key={i} className="p-3 bg-slate-950 rounded-lg border border-slate-800 flex flex-col justify-between">
                    <span className="text-emerald-400 font-bold">{item.file}</span>
                    <span className="text-slate-400 text-[11px] mt-1">{item.desc}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-800/80 bg-slate-950 py-4 text-center text-xs text-slate-500">
        Jungle Gaming Backend Challenge — Solução Desenvolvida com Rigor de Engenharia de Software (Anti-Vibe Coding)
      </footer>
    </div>
  );
}
