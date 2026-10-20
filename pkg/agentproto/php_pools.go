package agentproto

// PoolCount includes legacy per-version pools and modern per-site masters.
func PHPPoolCount(sites []WebSite) int {
	legacy := map[string]bool{}
	n := 0
	for _, site := range sites {
		if site.AppType == "php" || site.AppType == "laravel" {
			n++
		} else if site.AppType == "" && site.PHPVersion != "" {
			legacy[site.PHPVersion] = true
		}
	}
	return n + len(legacy)
}
