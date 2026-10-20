package phpconfig

// Workers divides an account's estimated PHP budget between its FPM masters.
// Actual RSS depends on the app; the account cgroup remains the hard RAM limit.
func Workers(memoryMB, pools int) int {
	if pools < 1 {
		pools = 1
	}
	if memoryMB <= 0 {
		return max(1, 10/pools)
	}
	// Allow 64MB per master/OPcache, then estimate 96MB per busy PHP worker.
	budget := max(0, memoryMB-64*pools)
	return max(1, min(50, budget/96)/pools)
}
