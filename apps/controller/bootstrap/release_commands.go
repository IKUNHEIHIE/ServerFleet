package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/recovery"
	"serverfleet.local/serverfleet/internal/transport"
	"serverfleet.local/serverfleet/internal/updates"
	"strings"
)

func RunReleaseCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !provisionOwnerSupported() {
		return errProvision
	}
	flags := flag.NewFlagSet("controller release "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	directory := flags.String("directory", "", "new private offline key directory")
	keyID := flags.String("key-id", "", "explicit release key ID")
	keyFile := flags.String("key", "", "private offline Ed25519 key file")
	manifestFile := flags.String("manifest", "", "canonical build-generated ReleaseManifest JSON")
	artifactFile := flags.String("artifact", "", "matching executable artifact")
	output := flags.String("output", "", "new private VERSION directory")
	offline := flags.Bool("offline-ack", false, "confirm this action runs on the offline release-signing host")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if flags.NArg() != 0 || !*offline || !transport.FileID(*keyID) || ctx == nil || ctx.Err() != nil {
		return errProvision
	}
	switch args[0] {
	case "keygen":
		root, e := newProvisionDirectory(*directory)
		if e != nil {
			return e
		}
		defer root.Close()
		public, private, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		defer clear(private)
		secret := []byte(base64.StdEncoding.EncodeToString(private))
		defer clear(secret)
		if e = provisionWrite(root, "release-private.key", secret, 0600); e != nil {
			return e
		}
		raw, e := json.Marshal(map[string]string{*keyID: base64.StdEncoding.EncodeToString(public)})
		if e != nil {
			return e
		}
		if e = provisionWrite(root, "release-public.json", raw, 0600); e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "OFFLINE_RELEASE_KEY_CREATED", "keyId": *keyID, "publicKeyFile": filepath.Join(*directory, "release-public.json"), "privateKeyExported": false})
	case "sign":
		raw, e := recovery.ReadPrivateFile(*manifestFile, 16<<10)
		if e != nil {
			return e
		}
		var m updates.ReleaseManifest
		if strictProvisionJSON(raw, &m) != nil || filepath.Base(*output) != m.Version {
			return errProvision
		}
		encoded, e := recovery.ReadPrivateFile(*keyFile, 4096)
		if e != nil {
			return e
		}
		defer clear(encoded)
		private, e := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
		if e != nil || len(private) != ed25519.PrivateKeySize {
			return errProvision
		}
		defer clear(private)
		key := ed25519.NewKeyFromSeed(private[:ed25519.SeedSize])
		defer clear(key)
		if !bytes.Equal(private, key) {
			return errProvision
		}
		public := key.Public().(ed25519.PublicKey)
		signature := ed25519.Sign(key, append([]byte("serverfleet.release.v1\x00"), raw...))
		policy := updates.ReleasePolicy{Component: m.Component, Architecture: m.Architecture, MaxBytes: 256 << 20, TrustedKeys: map[string]ed25519.PublicKey{*keyID: public}}
		if _, e = updates.VerifyManifest(ctx, raw, signature, *keyID, policy); e != nil {
			return e
		}
		artifact, e := openProvisionProgram(*artifactFile)
		if e != nil {
			return e
		}
		defer artifact.Close()
		root, e := newProvisionDirectory(*output)
		if e != nil {
			return e
		}
		defer root.Close()
		if e = copyProvisionProgram(root, "artifact", artifact, m.SHA256, m.Bytes); e != nil {
			return e
		}
		if e = provisionWrite(root, "signature.bin", signature, 0600); e != nil {
			return e
		}
		// Last publish: incomplete output cannot masquerade as a complete release.
		if e = provisionWrite(root, "manifest.json", raw, 0600); e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(map[string]any{"state": "SIGNED_NOT_INSTALLED", "component": m.Component, "version": m.Version, "sequence": m.Sequence, "sha256": m.SHA256, "keyId": *keyID, "directory": *output})
	default:
		return errProvision
	}
}
