package bootstrap

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/updates"
)

func RunBootstrapTokenCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controller bootstrap-token", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "existing private Controller supervisor JSON")
	output := flags.String("output", "", "new absolute private setup-token file")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || !provisionOwnerSupported() || !filepath.IsAbs(*output) || filepath.Clean(*output) != *output {
		return errProvision
	}
	c, e := updates.LoadResidentSupervisor(*config)
	if e != nil || c.Component != "controller" || !provisionExistingDatabase(c.Database) {
		return errProvision
	}
	root, e := openProvisionPrivateDirectory(filepath.Dir(*output))
	if e != nil {
		return e
	}
	defer root.Close()
	// Reserve before rotating a token. Never overwrite another token or a DB.
	f, e := root.OpenFile(filepath.Base(*output), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	token, e := server.PrepareLocalBootstrap(ctx, c.Database)
	if e != nil {
		return e
	}
	n, e := io.WriteString(f, token+"\n")
	if e != nil || n != len(token)+1 || f.Sync() != nil {
		return errProvision
	}
	if e = syncProvisionDirectory(root, "."); e != nil {
		return e
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"state": "SETUP_TOKEN_PREPARED", "tokenFile": *output, "expiresInSeconds": 600, "administratorCreated": false})
}
