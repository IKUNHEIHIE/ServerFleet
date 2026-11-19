package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

func (c *certificateCSRCapability) ManagedWebsiteAction(endpoint agentLinkEndpoint, client *http.Client) agentAction {
	return agentAction{
		Validate: func(raw []byte) error {
			if !c.current() || c.roots == nil {
				return errExecutionRejected
			}
			_, err := transport.ParseWebsiteCertificatePlan(raw)
			return err
		},
		Run: func(ctx context.Context, raw []byte) executionResult {
			unknown := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
			failed := executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
			p, err := transport.ParseWebsiteCertificatePlan(raw)
			offer, ok := ctx.Value(agentOfferKey{}).(*agentv1.TaskOffer)
			if err != nil || !ok {
				return failed
			}
			var bundle transport.WebsiteCertificateBundle
			// The Controller records the dispatch before serving the
			// private bundle on the same pinned mTLS authority.
			err = waitCommandAdmission(ctx, func(ctx context.Context, _ string) error {
				var err error
				bundle, err = c.fetchManagedWebsite(ctx, endpoint, client, offer)
				return err
			})
			if err != nil {
				return failed
			}
			defer clear(bundle.KeyDER)
			if transport.VerifyWebsiteCertificate(p, bundle, c.roots, time.Now()) != nil {
				return failed
			}
			if err = c.installManagedWebsite(ctx, p, bundle); err != nil {
				return unknown
			}
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
		},
	}
}

func (c *certificateCSRCapability) fetchManagedWebsite(ctx context.Context, endpoint agentLinkEndpoint, client *http.Client, offer *agentv1.TaskOffer) (transport.WebsiteCertificateBundle, error) {
	var bundle transport.WebsiteCertificateBundle
	payload, _ := json.Marshal(map[string]string{"taskId": offer.TaskId, "planHash": offer.PlanDigestSha256, "nodeId": c.nodeID, "executionId": offer.ExecutionId})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+endpoint.authority+transport.WebsiteCertificateBundlePath, bytes.NewReader(payload))
	if err != nil {
		return bundle, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return bundle, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return bundle, errExecutionRejected
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, transport.MaxWebsiteCertificateBundle+1))
	defer clear(raw)
	if err != nil || len(raw) > transport.MaxWebsiteCertificateBundle || json.Unmarshal(raw, &bundle) != nil {
		clear(bundle.KeyDER)
		return transport.WebsiteCertificateBundle{}, errExecutionRejected
	}
	return bundle, nil
}

func (c *certificateCSRCapability) installManagedWebsite(ctx context.Context, plan transport.WebsiteCertificatePlan, bundle transport.WebsiteCertificateBundle) error {
	c.managedMu.Lock()
	defer c.managedMu.Unlock()
	if ctx.Err() != nil || !c.current() {
		return errExecutionRejected
	}
	digest := sha256.Sum256([]byte(plan.Domain))
	site := "managed/" + hex.EncodeToString(digest[:])
	if err := c.root.MkdirAll(site, 0700); err != nil {
		return err
	}
	for _, parent := range []string{"managed", site} {
		info, err := c.root.Lstat(parent)
		if err != nil || !csrPrivateInfo(info, true) {
			return errExecutionRejected
		}
	}
	generation := strconv.FormatUint(plan.Generation, 10) + "-" + plan.OrderID
	old, err := c.root.Readlink(site + "/current")
	if err == nil {
		oldGen, _, found := strings.Cut(old, "-")
		number, parseErr := strconv.ParseUint(oldGen, 10, 64)
		if !found || parseErr != nil || strings.ContainsAny(old, "/\\") || number > plan.Generation || number == plan.Generation && old != generation {
			return errExecutionRejected
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	destination := site + "/" + generation
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: bundle.KeyDER})
	defer clear(key)
	if info, err := c.root.Lstat(destination); err == nil {
		if !csrPrivateInfo(info, true) {
			return errExecutionRejected
		}
		existingKey, e := c.readPrivate(destination+"/privkey.pem", 8<<10)
		defer clear(existingKey)
		existingChain, chainErr := c.readPrivate(destination+"/fullchain.pem", 8<<10)
		if e != nil || chainErr != nil || !bytes.Equal(existingKey, key) || !bytes.Equal(existingChain, bundle.ChainPEM) {
			return errExecutionRejected
		}
	} else if os.IsNotExist(err) {
		stage := site + "/.stage-" + hex.EncodeToString(randomWebsiteSuffix())
		if err := c.root.Mkdir(stage, 0700); err != nil {
			return err
		}
		defer c.root.RemoveAll(stage)
		if err := c.writeWebsiteFile(stage+"/privkey.pem", key); err != nil {
			return err
		}
		if err := c.writeWebsiteFile(stage+"/fullchain.pem", bundle.ChainPEM); err != nil {
			return err
		}
		if err := c.syncWebsiteDirectory(stage); err != nil {
			return err
		}
		if err := c.root.Rename(stage, destination); err != nil {
			return err
		}
	} else {
		return err
	}
	if ctx.Err() != nil || !c.current() {
		return errExecutionRejected
	}
	link := site + "/.current-" + hex.EncodeToString(randomWebsiteSuffix())
	if err := c.root.Symlink(generation, link); err != nil {
		return err
	}
	defer c.root.Remove(link)
	if err := c.root.Rename(link, site+"/current"); err != nil {
		return err
	}
	for _, dir := range []string{site, "managed", "."} {
		if err := c.syncWebsiteDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}

func randomWebsiteSuffix() []byte {
	suffix := make([]byte, 16)
	rand.Read(suffix)
	return suffix
}

func (c *certificateCSRCapability) syncWebsiteDirectory(path string) error {
	dir, err := c.root.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (c *certificateCSRCapability) writeWebsiteFile(path string, data []byte) error {
	f, err := c.root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
