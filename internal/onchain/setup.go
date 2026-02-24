package onchain

// SetupAllowances sets USDC.approve(MAX_INT) for all three Polymarket
// exchange contracts via Safe execTransaction.
//
// One-time setup required for each new Gnosis Safe wallet.
// Python reference: setup_safe_allowances.py
//
//   safe    — initialized Safe wrapper
//   check   — if true, only print current state without sending txs
func SetupAllowances(safe *Safe, checkOnly bool) error {
	panic("not implemented")
}
