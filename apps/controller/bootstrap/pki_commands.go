package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"

	"serverfleet.local/serverfleet/apps/controller/localpki"
)

// RunPKICommand performs only explicit local provisioning. It starts no HTTP,
// Agent, cloud connection or background process and never prints private keys.
func RunPKICommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if ctx == nil || ctx.Err() != nil || len(args) == 0 {
		return errors.New("pki: explicit command required")
	}
	flags := flag.NewFlagSet("controller pki "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "new private local/offline directory")
	rootDirectory := flags.String("root-directory", "", "OFFLINE Root directory; never an online Controller directory")
	requestDirectory := flags.String("request-directory", "", "private directory containing only the public issuer CSR")
	bundleDirectory := flags.String("bundle-directory", "", "private directory containing the offline-signed public bundle")
	authority := flags.String("authority-id", "", "stable unique Controller authority ID")
	name := flags.String("name", "Server Fleet offline Root", "offline Root common name")
	rootPin := flags.String("root-pin", "", "independently verified Root SPKI SHA-256")
	generation := flags.Uint64("generation", 0, "explicit offline-authorized trust generation")
	requestFile := flags.String("request-file", "", "private recovery authority request JSON")
	outputFile := flags.String("output-file", "", "new signed public recovery authorization file")
	offline := flags.Bool("offline-ack", false, "confirm this explicit signing operation is on the offline Root host")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("pki: unexpected positional arguments")
	}
	switch args[0] {
	case "init-acme-account":
		fingerprint, err := localpki.CreateACMEAccountKey(*directory)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "acme_account_key_created", "spkiSha256": fingerprint, "privateKeyExported": false})
	case "sign-recovery":
		if !*offline {
			return errors.New("pki: offline Root operation requires -offline-ack")
		}
		if err := localpki.SignRecoveryAuthority(*rootDirectory, *requestFile, *outputFile); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "recovery_authority_signed", "privateKeyExported": false, "fencingProven": false})
	case "init-offline":
		if !*offline {
			return errors.New("pki: offline Root operation requires -offline-ack")
		}
		pin, err := localpki.CreateOfflineRoot(*directory, *name)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "offline_root_created", "rootSpkiSha256": pin, "privateKeyExported": false})
	case "request-intermediate":
		if err := localpki.RequestIntermediate(*directory, *authority); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "csr_created", "transferOnly": "issuer.csr.pem", "privateKeyExported": false})
	case "sign-intermediate":
		if !*offline {
			return errors.New("pki: offline Root operation requires -offline-ack")
		}
		if err := localpki.SignIntermediate(*rootDirectory, *requestDirectory, *directory, *authority, *generation); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "public_bundle_signed", "trustGeneration": *generation, "privateKeyExported": false})
	case "install-intermediate":
		material, err := localpki.InstallIntermediate(*directory, *bundleDirectory, *rootPin)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "provisioned", "authorityId": material.AuthorityID, "trustGeneration": material.TrustGeneration, "rootSpkiSha256": material.RootSPKI})
	default:
		return errors.New("pki: unsupported explicit command")
	}
}
