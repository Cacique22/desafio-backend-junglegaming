DROP TRIGGER IF EXISTS trg_prevent_ledger_modification ON wallet_ledger;
DROP FUNCTION IF EXISTS prevent_ledger_modification();
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS inbox;
DROP TABLE IF EXISTS wallet_ledger;
DROP TABLE IF EXISTS wager_transactions;
DROP TABLE IF EXISTS wallets;
