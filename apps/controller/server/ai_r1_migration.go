package server

import (
	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/storage"
)

// Integration owner allocates a NEW immutable version; never edit old migrations.
// No automatic application or migration number is assigned by the module author.
func aiR1Migration(version string) storage.Migration {
	return storage.Migration{Version: version, Name: "AI persistent budgets and typed model audit", Statements: append(ai.GenerationBudgetSchemaStatements(), ai.ModelSettingsAuditSchemaStatements()...)}
}
