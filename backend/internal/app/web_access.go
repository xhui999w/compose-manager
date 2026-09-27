package app

import (
	"context"
	"errors"
	"net"

	"github.com/compose-manager/compose-manager/backend/internal/access"
	"github.com/compose-manager/compose-manager/backend/internal/model"
)

func (s *Service) webContainers(ctx context.Context, key string) ([]model.Container, error) {
	if key == "" || len(key) > 256 {
		return nil, errors.New("项目标识无效")
	}
	if net.ParseIP(s.config.NASIP) == nil {
		return nil, errors.New("请先在设置中填写 NAS 内网 IP 地址")
	}
	var containers []model.Container
	if s.config.DemoMode {
		for _, p := range demoProjects(s.config.NASIP) {
			containers = append(containers, p.Containers...)
		}
	} else {
		var err error
		containers, err = s.docker.Containers(ctx)
		if err != nil {
			return nil, err
		}
	}
	result := []model.Container{}
	for _, c := range containers {
		if c.Project == key {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *Service) ProjectWebAccess(ctx context.Context, key string) (access.WebResult, error) {
	if !s.webScanMu.TryLock() {
		return access.WebResult{}, errors.New("另一个网页检测正在进行，请稍后重试")
	}
	defer s.webScanMu.Unlock()
	containers, err := s.webContainers(ctx, key)
	if err != nil {
		return access.WebResult{}, err
	}
	configs, err := s.store.WebAccess(ctx)
	if err != nil {
		return access.WebResult{}, err
	}
	var saved *access.WebConfig
	if cfg, ok := configs[key]; ok {
		saved = &cfg
	}
	candidates := access.Candidates(containers, s.config.NASIP, s.config.DefaultScheme)
	if s.config.DemoMode {
		return access.WebResult{Candidates: candidates, Saved: saved}, nil
	}
	return access.Detect(ctx, candidates, s.config.NASIP, saved), nil
}

func (s *Service) SaveProjectWebAccess(ctx context.Context, key string, cfg access.WebConfig) error {
	if s.config.DemoMode {
		return errors.New("演示模式不能保存设置")
	}
	if err := access.ValidateWebConfig(cfg); err != nil {
		return err
	}
	containers, err := s.webContainers(ctx, key)
	if err != nil {
		return err
	}
	if access.ResolveSaved(containers, s.config.NASIP, cfg) == "" {
		return errors.New("端口映射已变化，请重新检测并选择该项目的 TCP 映射端口")
	}
	if err := s.store.PutWebAccess(ctx, key, cfg); err != nil {
		return err
	}
	return s.store.Audit(ctx, "compose.web-access", "project", key, "success", "默认网页入口已保存")
}
