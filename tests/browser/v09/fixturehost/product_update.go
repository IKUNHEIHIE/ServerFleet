// 测试专用：把正式的 server.Config.UpdateProduct（Agent 组件本地签名更新）
// 注入组合宿主。只有 fixturehost 启动参数给出一个普通用户私有的 0600 JSON 时
// 才启用；fixturehost 不提供任何 HTTP 端点读取或修改它。
//
// 与生产装配的边界：本文件只负责把“可信程序配置”从私有文件搬进
// updates.ProductDeploymentOptions，随后由正式的 server.NewOwnedProductUpdates
// 绑定 DB/Audit/CheckExecution/管理员授权/RuntimeGate/当前连接，再由正式的
// internal/updates 完成签名校验、冻结计划、任务审批、槽位安装、自有进程监督与
// 健康就绪判定。宿主不判断版本、不写 installed 状态、不伪造 health.json。
//
// SchemaFingerprint 必须与线上 Controller 数据库真实匹配，因此不能由测试脚本
// 猜测：main 先用同一 DB 组合一次正式 Server（完成正式迁移），读出 fingerprints
// 后关闭，再以该真实指纹重新组合。用户提供任何 fingerprint 都会被忽略。
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"serverfleet.local/serverfleet/internal/updates"
)

const maxProductUpdateConfigBytes = 64 << 10

// productUpdateFile is the private on-disk shape. Runtime adapters (DB, Audit,
// Current, Administrator, RuntimeCurrent, NodeCurrent) are deliberately absent:
// they can only come from the composed Server, never from a file.
type productUpdateFile struct {
	Directory        string                  `json:"directory"`
	Inbox            string                  `json:"inbox"`
	StageDir         string                  `json:"stageDir"`
	KeyID            string                  `json:"keyId"`
	NodeID           string                  `json:"nodeId"`
	IdentityDir      string                  `json:"identityDir"`
	ControllerOrigin string                  `json:"controllerOrigin"`
	RootCertificate  string                  `json:"rootCertificate"`
	CandidateVersion string                  `json:"candidateVersion"`
	SchemaVersion    int                     `json:"schemaVersion"`
	Initial          updates.ReleaseManifest `json:"initial"`
	PolicyComponent  string                  `json:"policyComponent"`
	Architecture     string                  `json:"architecture"`
	MinimumSequence  uint64                  `json:"minimumSequence"`
	MaxBytes         int64                   `json:"maxBytes"`
	TrustedKeys      map[string]string       `json:"trustedKeys"`
}

func loadProductUpdateFile(path string) (*productUpdateFile, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxProductUpdateConfigBytes {
		return nil, errors.New("product update config must be a private regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("product update config must not be group/world accessible")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maxProductUpdateConfigBytes {
		return nil, errors.New("product update config is unreadable")
	}
	var file productUpdateFile
	if err = json.Unmarshal(raw, &file); err != nil {
		return nil, errors.New("product update config is not valid JSON")
	}
	if !filepath.IsAbs(file.Directory) || !filepath.IsAbs(file.Inbox) || !filepath.IsAbs(file.StageDir) ||
		!filepath.IsAbs(file.IdentityDir) || !filepath.IsAbs(file.RootCertificate) {
		return nil, errors.New("product update paths must be absolute")
	}
	if strings.TrimSpace(file.KeyID) == "" || strings.TrimSpace(file.NodeID) == "" || strings.TrimSpace(file.ControllerOrigin) == "" {
		return nil, errors.New("product update keyId/nodeId/controllerOrigin are required")
	}
	if !strings.HasPrefix(file.ControllerOrigin, "https://") {
		return nil, errors.New("product update controllerOrigin must be https")
	}
	if file.PolicyComponent != "agent" {
		return nil, errors.New("only the agent component is an accepted local product update")
	}
	if file.PolicyComponent != file.Initial.Component {
		return nil, errors.New("policy component must match the initial manifest")
	}
	if file.Architecture == "" || file.Architecture != file.Initial.Architecture {
		return nil, errors.New("architecture must match the initial manifest")
	}
	if file.MaxBytes <= 0 || file.MaxBytes > 256<<20 {
		return nil, errors.New("maxBytes is out of range")
	}
	if len(file.TrustedKeys) == 0 {
		return nil, errors.New("at least one trusted release key is required")
	}
	return &file, nil
}

// options builds the trusted program configuration. fingerprint is always the
// observed live database fingerprint, never a caller-supplied claim.
func (f *productUpdateFile) options(fingerprint string) (updates.ProductDeploymentOptions, error) {
	keys := make(map[string]ed25519.PublicKey, len(f.TrustedKeys))
	for id, encoded := range f.TrustedKeys {
		if strings.TrimSpace(id) == "" {
			return updates.ProductDeploymentOptions{}, errors.New("trusted key id is empty")
		}
		decoded, err := hex.DecodeString(strings.TrimSpace(encoded))
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return updates.ProductDeploymentOptions{}, errors.New("trusted key must be 32 hex-encoded bytes")
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	if _, ok := keys[f.KeyID]; !ok {
		return updates.ProductDeploymentOptions{}, errors.New("keyId is not present in trustedKeys")
	}
	return updates.ProductDeploymentOptions{
		Directory:         f.Directory,
		Inbox:             f.Inbox,
		StageDir:          f.StageDir,
		KeyID:             f.KeyID,
		NodeID:            f.NodeID,
		IdentityDir:       f.IdentityDir,
		ControllerOrigin:  f.ControllerOrigin,
		RootCertificate:   f.RootCertificate,
		CandidateVersion:  f.CandidateVersion,
		SchemaVersion:     f.SchemaVersion,
		SchemaFingerprint: fingerprint,
		Initial:           f.Initial,
		Policy: updates.ReleasePolicy{
			Component:       f.PolicyComponent,
			Architecture:    f.Architecture,
			MinimumSequence: f.MinimumSequence,
			MaxBytes:        f.MaxBytes,
			TrustedKeys:     keys,
		},
	}, nil
}
