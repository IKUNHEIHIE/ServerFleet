// 测试专用：显式 nodeID → 逻辑绝对根的受控注册表映射器。
//
// 交接说明要求“动态登记只允许测试程序内部受控来源，不能暴露无认证 HTTP 配置”。
// 这里只读取 fixturehost 启动参数指定的私有 0600 JSON 文件（例如
// {"node-a":"/home/user/work/fixtures/src"}），fixturehost 不提供任何修改该文件的
// HTTP 端点；每次映射都重新校验文件形态并把内容交给正式的
// files.NewRootPathMapper 做 nodeID/绝对目录/根非 "/" 校验。文件缺失、权限放宽、
// 内容为空或非法都返回 files.ErrPathMappingUnavailable（失败关闭）。
package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime"

	"serverfleet.local/serverfleet/internal/files"
)

const (
	maxFileRootsBytes = 64 << 10
	maxFileRoots      = 1000
)

type registryPathMapper struct{ path string }

func (m registryPathMapper) readRoots() (files.PathMapper, error) {
	info, err := os.Lstat(m.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxFileRootsBytes {
		return nil, files.ErrPathMappingUnavailable
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, files.ErrPathMappingUnavailable
	}
	raw, err := os.ReadFile(m.path)
	if err != nil || len(raw) == 0 || len(raw) > maxFileRootsBytes {
		return nil, files.ErrPathMappingUnavailable
	}
	var roots map[string]string
	if json.Unmarshal(raw, &roots) != nil || len(roots) == 0 || len(roots) > maxFileRoots {
		return nil, files.ErrPathMappingUnavailable
	}
	mapper, err := files.NewRootPathMapper(roots)
	if err != nil {
		return nil, files.ErrPathMappingUnavailable
	}
	return mapper, nil
}

func (m registryPathMapper) RelativePath(ctx context.Context, node, absolute string) (string, error) {
	mapper, err := m.readRoots()
	if err != nil {
		return "", err
	}
	return mapper.RelativePath(ctx, node, absolute)
}

func (m registryPathMapper) AbsolutePath(ctx context.Context, node, relative string) (string, error) {
	mapper, err := m.readRoots()
	if err != nil {
		return "", err
	}
	return mapper.AbsolutePath(ctx, node, relative)
}
