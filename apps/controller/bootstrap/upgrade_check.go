package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"serverfleet.local/serverfleet/apps/controller/server"
)

func RunUpgradeCheckCommand(ctx context.Context, args []string, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("upgrade-check", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	database := flags.String("db", "", "existing SQLite database; opened read-only")
	keys := flags.String("credential-key-dir", "", "original private credential key directory")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if *database == "" || flags.NArg() != 0 {
		return fmt.Errorf("upgrade-check requires -db and the original credential key directory")
	}
	report, e := server.CheckUpgrade(ctx, *database, *keys)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(struct {
		Ready    bool `json:"readyForMigration"`
		Identity any  `json:"identity"`
	}{true, report})
}
