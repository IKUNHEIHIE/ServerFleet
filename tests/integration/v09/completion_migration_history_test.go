package v09_test

// Versions introduced only by the concentrated completion branch, after the
// accepted 29-version baseline. Historical SQL/checksums are left unchanged.
func completionOnlyMigration(v string) bool {
	switch v {
	case "cx/0011_encrypted_backups", "cx/0012_recovery_revocations", "cx/0013_recovery_reconciliation", "cx/0014_release_acquisition", "cx/0015_agent_certificate_csr", "cx/0016_node_certificate_chains", "cx/0017_node_certificate_sets", "cx/0018_public_certificate_orders", "cx/0019_controller_supervision", "cx/0020_public_certificate_renewals", "cx/0021_backup_maintenance", "cx/0022_operator_incidents", "ds/0082_publishing_dns_tasks":
		return true
	}
	return false
}
